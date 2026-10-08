<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { providersApi } from '../api'
import { THINKING_OPTIONS, protocolLabel, type AiProvider, type ModelKind, type ModelRow, type ThinkingLevel } from '../api/providers'
import AppCard from '../components/AppCard.vue'
import AppButton from '../components/AppButton.vue'
import AppBadge from '../components/AppBadge.vue'
import AppModal from '../components/AppModal.vue'
import AppAlert from '../components/AppAlert.vue'
import AppIcon from '../components/AppIcon.vue'
import AppCombobox from '../components/AppCombobox.vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import { errMsg } from '../lib/http'
import { toast } from '../lib/toast'
import { useConfirmAction } from '../lib/confirm'

// 两条链共用本组件，链别由路由决定（/ai/chat-chain、/ai/embedding-chain）
const route = useRoute()
const kind = computed(() => route.meta.kind as ModelKind)

const kindLabel: Record<ModelKind, string> = { chat: '对话模型链', embedding: '嵌入模型链' }

const providers = ref<AiProvider[]>([])
const list = ref<ModelRow[]>([])

/** 取回两条链、只留本条（各写一遍 `chains[kind]` 迟早漏掉一处）。 */
async function reloadList() {
  list.value = (await providersApi.listModels())[kind.value]
}

async function load() {
  try {
    providers.value = await providersApi.listProviders()
    await reloadList()
  } catch (e) {
    toast.error(errMsg(e, '模型配置加载失败'))
  }
}
onMounted(load)

// ---------- 模型条目 添加/编辑弹窗 ----------
const showModelForm = ref(false)
const editingModelId = ref<number | null>(null)
const mProviderId = ref<number | null>(null)
const mModelId = ref('')
const mMaxTokens = ref<number | null>(null)
const mThinking = ref<ThinkingLevel>('off')
/** 该模型支持的思考档位（多选）：档位取值是模型属性，故由这里声明，不在代码里判模型名。 */
const mThinkingLevels = ref<ThinkingLevel[]>(['off'])
const mContextWindow = ref<number | null>(null)
const mSaving = ref(false)

// 嵌入走 OpenAI 兼容 /embeddings，仅 OpenAI Chat 协议供应商可用（服务端同口径校验）
const providerOptions = computed(() =>
  providers.value
    .filter((p) => (kind.value === 'embedding' ? p.api_type === 'openai-chat' : true))
    .filter((p) => p.has_key || editingModelId.value !== null),
)

// 所选供应商的协议：openai-chat 没有统一的思考参数（是否回思考由供应商自定），故该项置灰。
// 服务端有同口径校验（checkThinkingAllowed）——服务端才是权威，这里只是别让人白填一遍。
const thinkingDisabled = computed(
  () => providers.value.find((x) => x.id === mProviderId.value)?.api_type === 'openai-chat',
)

// 声明集恒含「关」（关是任何模型的合法选择）：下拉选项、回落判断、提交串三处共用，
// 各写一遍迟早漂移（漏一处就会出现「界面上没有关、提交时又带了关」这类不一致）。
const withOff = (levels: ThinkingLevel[]): ThinkingLevel[] => [...new Set([...levels, 'off' as ThinkingLevel])]

// 「思考」下拉的选项 = 该模型声明的支持档位（含恒有的「关」）。
// 声明集外选不出来：档位取值是模型属性（如 Gemini 3 Pro 只接受低/高），
// 在配置面声明即可，避免在代码里解析模型名（别名会让名称匹配落空）。
const thinkingChoices = computed(() => {
  const allowed = withOff(mThinkingLevels.value)
  return THINKING_OPTIONS.filter((o) => allowed.includes(o.value))
})

// 取消勾选当前选中的档位时，把「思考」回落到「关」。
// 不回落就会留下「下拉空着、内部仍是旧档位」的状态：提交时送出的档位不在声明集内，
// 被服务端以「thinking 需在 thinking_levels 声明的档位内」拒绝（400），
// 而界面上看不出哪里不对。watch 而非在 change 里判断，是为了两处入口都覆盖到。
watch(mThinkingLevels, (levels) => {
  if (!withOff(levels).includes(mThinking.value)) mThinking.value = 'off'
})

// openai-chat 无统一思考参数，声明集只能是 off：选中该协议就把多选与档位都落回「关」。
// 只置灰不落值会留下「勾选框灰着但仍勾着中/高」的状态——提交必被服务端拒（400），
// 而界面上看不出哪里可改（新建时供应商可切换，故该路径可达）。
watch(thinkingDisabled, (disabled) => {
  if (disabled) {
    mThinkingLevels.value = ['off']
    mThinking.value = 'off'
  }
})

/** 档位的显示名（列表用；取值面只在 THINKING_OPTIONS 一处）。 */
function thinkingLabel(level: ThinkingLevel): string {
  return THINKING_OPTIONS.find((o) => o.value === level)?.label ?? level
}

const fetching = ref(false)
/** 「取候选」取回的模型名候选（来自当前所选供应商），交给 AppCombobox 作下拉选项。 */
const modelCandidates = ref<string[]>([])

// 获取模型列表：以所选供应商的地址与已存秘钥为准。
// 取回的是**候选**而非结论——全部交给下拉，不替用户挑一个（原来只填 models[0]，
// 用户配 deepseek 时拿到的是列表首位的 bge-m3，得自己删掉重打）。
async function fetchModelList() {
  const p = providers.value.find((x) => x.id === mProviderId.value)
  if (!p) {
    toast.error('请先选择供应商')
    return
  }
  fetching.value = true
  try {
    const res = await providersApi.fetchModels({ api_type: p.api_type, base_url: p.base_url, provider_id: p.id })
    modelCandidates.value = res.models
    if (res.models.length === 0) {
      // 供应商可能没开 /models：不覆盖已输入值，手填仍是唯一路径
      toast.error('该供应商没有返回模型列表，请直接手填模型名')
      return
    }
    // 已输入且命中候选时不必多言；不命中要说明，否则用户会以为取候选没生效
    const typed = mModelId.value.trim()
    const miss = typed && !res.models.includes(typed) ? '，当前输入名不在其中' : ''
    toast.success(`已取回 ${res.models.length} 个候选${miss}，可下拉选择或直接输入`)
  } catch (e) {
    toast.error(errMsg(e, '获取模型列表失败'))
  } finally {
    fetching.value = false
  }
}

// 新建传 null、编辑传该行：两侧赋值一一对应，分开写迟早漏改一处
function openModelForm(row: ModelRow | null) {
  editingModelId.value = row?.id ?? null
  mProviderId.value = row?.provider_id ?? providerOptions.value[0]?.id ?? null
  mModelId.value = row?.model_id ?? ''
  mMaxTokens.value = row?.max_tokens ?? null
  mThinking.value = row?.thinking ?? 'off'
  mThinkingLevels.value = row
    ? (row.thinking_levels.split(',').filter(Boolean) as ThinkingLevel[])
    : ['off']
  mContextWindow.value = row?.context_window ?? null
  // 候选取自「供应商 + 地址」：每次打开都从空开始，不带着上次的残留
  modelCandidates.value = []
  showModelForm.value = true
}

function normalizeParam(v: number | null): number | null {
  return v === null || (v as unknown as string) === '' ? null : Number(v)
}

async function submitModelForm() {
  if (mProviderId.value === null && editingModelId.value === null) {
    toast.error('请选择供应商')
    return
  }
  if (!mModelId.value.trim()) {
    toast.error('模型名必填')
    return
  }
  mSaving.value = true
  try {
    const maxTok = normalizeParam(mMaxTokens.value)
    const ctxWin = normalizeParam(mContextWindow.value)
    // 声明集恒含「关」（关是任何模型的合法选择），顺序交给服务端归一
    const levels = withOff(mThinkingLevels.value).join(',')
    if (editingModelId.value === null) {
      list.value = (
        await providersApi.createModel({
          kind: kind.value,
          provider_id: mProviderId.value!,
          model_id: mModelId.value.trim(),
          max_tokens: maxTok,
          thinking: mThinking.value,
          thinking_levels: levels,
          context_window: ctxWin,
        })
      )[kind.value]
    } else {
      list.value = (
        await providersApi.updateModel(editingModelId.value, {
          model_id: mModelId.value.trim(),
          max_tokens: maxTok,
          thinking: mThinking.value,
          thinking_levels: levels,
          context_window: ctxWin,
        })
      )[kind.value]
    }
    showModelForm.value = false
    toast.success(editingModelId.value === null ? '模型条目已添加' : '模型条目已保存')
  } catch (e) {
    toast.error(errMsg(e, '保存失败'))
  } finally {
    mSaving.value = false
  }
}

const modelRemove = useConfirmAction<ModelRow>('删除失败')

async function runRemoveModel(row: ModelRow) {
  await providersApi.deleteModel(row.id)
  toast.success(`已移除模型「${row.model_id}」`)
  await reloadList()
}

// 链内排序：乐观更新 + 服务端持久化，失败回滚重载
async function moveModel(idx: number, dir: -1 | 1) {
  const target = idx + dir
  if (target < 0 || target >= list.value.length) return
  const next = [...list.value]
  ;[next[idx], next[target]] = [next[target], next[idx]]
  list.value = next
  try {
    list.value = (await providersApi.reorderModels(kind.value, next.map((r) => r.id)))[kind.value]
  } catch (e) {
    toast.error(errMsg(e, '排序保存失败'))
    await reloadList()
  }
}

// 两条链是同组件不同路由：实例被复用，onMounted 不会再跑，靠 watch 换链重载。
// 换链同时关掉弹窗——标题随 kind 变，留着它就成了「在对话链打开、按嵌入链提交」。
watch(kind, () => {
  showModelForm.value = false
  load()
})

// 候选取自「供应商 + 地址」：改了供应商，旧候选不再成立
watch(mProviderId, () => {
  modelCandidates.value = []
})
</script>

<template>
  <div class="tab-panel">
    <AppAlert variant="info" icon="info">
      模型条目按类型分两条链：对话与语义检索各自独立，调用从链首开始、失败依次向下切换（链内排序即故障切换策略）。
    </AppAlert>

    <div class="page-section-header">
      <h2 class="page-section-title">{{ kindLabel[kind] }}（{{ list.length }}）</h2>
      <div class="flex gap-2 items-center">
        <AppBadge v-if="kind === 'embedding'" :variant="list.length ? 'success' : 'neutral'">
          {{ list.length ? '语义检索已启用' : '未启用（回落关键词）' }}
        </AppBadge>
        <AppButton variant="primary" icon="plus" @click="openModelForm(null)">添加模型条目</AppButton>
      </div>
    </div>

    <AppCard class="card-fill">
      <div v-if="list.length === 0" class="empty-state">
        <div class="empty-state-title">
          {{ kind === 'chat' ? '对话链为空——AI 模式不可用' : '嵌入链为空——语义检索回落关键词匹配' }}，点右上角添加
        </div>
      </div>
      <div v-else class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th style="width: 44px">链序</th><th>供应商</th><th>协议</th><th>模型</th>
              <th>思考</th><th>支持的思考</th><th>上下文</th><th>最大输出</th><th></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(row, idx) in list" :key="row.id">
              <td class="nowrap">
                <span class="order-num">{{ idx + 1 }}</span>
                <button class="order-btn" type="button" title="上移" :disabled="idx === 0" @click="moveModel(idx, -1)">
                  <AppIcon name="chevron-up" :size="14" />
                </button>
                <button
                  class="order-btn"
                  type="button"
                  title="下移"
                  :disabled="idx === list.length - 1"
                  @click="moveModel(idx, 1)"
                >
                  <AppIcon name="chevron-down" :size="14" />
                </button>
              </td>
              <td>{{ row.provider_name }}</td>
              <td><code>{{ protocolLabel(row.api_type) }}</code></td>
              <td class="nowrap"><code>{{ row.model_id }}</code></td>
              <td>{{ thinkingLabel(row.thinking) }}</td>
              <td><code>{{ row.thinking_levels }}</code></td>
              <td>{{ row.context_window ?? '—' }}</td>
              <td>{{ row.max_tokens ?? '—' }}</td>
              <td class="text-right">
                <div class="flex gap-2" style="justify-content: flex-end">
                  <AppButton variant="secondary" size="sm" icon="edit" @click="openModelForm(row)">编辑</AppButton>
                  <AppButton variant="danger" size="sm" icon="trash" @click="modelRemove.target.value = row">移除</AppButton>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </AppCard>

    <AppAlert v-if="kind === 'embedding'" variant="warning" icon="alert-triangle">
      嵌入由链上供应商完成：动态正文会发送到该地址，向量结果只存在本机。如需零出站，可指向本机 OpenAI 兼容服务，例如
      <code>http://127.0.0.1:11434/v1</code> + <code>bge-m3</code>。链为空或全部失败时，AI 搜索自动回落关键词匹配。
    </AppAlert>

    <AppModal
      v-if="showModelForm"
      :title="(editingModelId === null ? '添加' : '编辑') + kindLabel[kind] + '条目'"
      :width="520"
      @close="showModelForm = false"
    >
      <form class="flex" style="flex-direction: column; gap: 14px" @submit.prevent="submitModelForm">
        <div class="form-group" style="margin: 0">
          <label class="form-label">供应商</label>
          <select v-model.number="mProviderId" class="form-select" :disabled="editingModelId !== null">
            <option v-for="p in providerOptions" :key="p.id" :value="p.id">{{ p.name }}（{{ protocolLabel(p.api_type) }}）</option>
          </select>
          <p v-if="kind === 'embedding' && providerOptions.length === 0" class="form-hint">
            嵌入只允许 OpenAI Chat 协议且已配 Key 的供应商；请先在「供应商」页添加。
          </p>
        </div>
        <div class="form-group" style="margin: 0">
          <label class="form-label" for="model-name">模型名</label>
          <div class="flex gap-2">
            <AppCombobox
              id="model-name"
              v-model="mModelId"
              :options="modelCandidates"
              placeholder="如 gpt-5.5 / claude-sonnet-5-5 / bge-m3"
              required
            />
            <AppButton
              v-if="editingModelId === null"
              variant="secondary"
              icon="refresh-cw"
              type="button"
              :disabled="fetching"
              @click="fetchModelList"
            >
              {{ fetching ? '获取中…' : '取候选' }}
            </AppButton>
          </div>
          <p class="form-hint">
            「取候选」用所选供应商的地址与已存秘钥请求其模型列表，取回后从下拉中选择，也可直接输入。
          </p>
        </div>
        <div class="flex gap-3">
          <div class="form-group" style="flex: 1; margin: 0">
            <label class="form-label">最大输出（可选）</label>
            <input v-model.number="mMaxTokens" class="form-input" type="number" min="256" max="32768" step="1" placeholder="不传" />
          </div>
          <div class="form-group" style="flex: 1; margin: 0">
            <label class="form-label">上下文窗口（可选）</label>
            <input v-model.number="mContextWindow" class="form-input" type="number" min="1024" max="2000000" step="1" placeholder="不传" />
          </div>
        </div>
        <p class="form-hint">上下文窗口是模型的 token 上限，用于裁剪送入模型的历史；不填则用服务端兜底预算。最大输出不得超过它。</p>
        <!-- 不可配置时只读展示，不摆禁用控件：该协议声明集只有「关」，禁用一个没有可选、
             值也改不动的控件，用户只看到「点不动」。如实说出「不可配置」更清楚。 -->
        <div class="form-group">
          <label class="form-label">支持的思考档位</label>
          <p v-if="thinkingDisabled" class="readonly-value">关（该协议不支持配置）</p>
          <div v-else class="flex gap-3">
            <label v-for="o in THINKING_OPTIONS" :key="o.value" class="flex" style="align-items: center; gap: 6px">
              <input v-model="mThinkingLevels" class="form-checkbox" type="checkbox" :value="o.value" />
              <span>{{ o.label }}</span>
            </label>
          </div>
          <p class="form-hint">
            按供应商文档填该模型实际支持的档位（如 Gemini 3 Pro 只有低/高，选「中」会被供应商拒绝）。
            「关」恒在集合内。档位在配置面声明，服务端不解析模型名——供应商改别名也不会失效。
          </p>
        </div>
        <div class="form-group">
          <label class="form-label">思考</label>
          <p v-if="thinkingDisabled" class="readonly-value">关 —— 按该模型的最低档请求</p>
          <select v-else v-model="mThinking" class="form-select">
            <option v-for="o in thinkingChoices" :key="o.value" :value="o.value">{{ o.label }}</option>
          </select>
          <p class="form-hint">
            <template v-if="thinkingDisabled">OpenAI Chat 协议没有统一的思考参数，是否回思考由供应商自定，故不可配置。</template>
            <template v-else>开启后模型会先思考再作答，思考同样消耗 token（按模型计费）。「关」= 该模型的最低档——现役模型无法真正关闭思考，省略参数只会让它留在默认档。</template>
          </p>
        </div>
        <div class="flex gap-2" style="justify-content: flex-end">
          <AppButton variant="secondary" icon="x" type="button" @click="showModelForm = false">取消</AppButton>
          <AppButton variant="primary" icon="save" type="submit" :disabled="mSaving">保存</AppButton>
        </div>
      </form>
    </AppModal>

    <ConfirmDialog
      v-if="modelRemove.target.value"
      title="移除模型条目"
      :message="`从${kindLabel[kind]}移除「${modelRemove.target.value.model_id}」（${modelRemove.target.value.provider_name}）？`"
      confirm-label="移除"
      :busy="modelRemove.busy.value"
      @close="modelRemove.target.value = null"
      @confirm="modelRemove.confirm(runRemoveModel)"
    />
  </div>
</template>

<style scoped>
/* 链序与模型名不折行：这两列一折，行高就从 54px 抬到 89px（序号 + 上下移按钮叠成三行）。
   列窄时宁可让 `.table-wrapper` 横向滚动，也不要把标识符拆断。 */
.nowrap {
  white-space: nowrap;
}
/* 不可配置项的只读展示：一眼看出不是可操作控件，故不再套输入框的边框底纹 */
.readonly-value {
  margin: 0;
  padding: 8px 0;
  font-size: 13px;
  color: var(--color-text-secondary);
}
.order-num {
  display: inline-block;
  min-width: 18px;
  font-weight: 600;
  color: var(--color-text-secondary);
  vertical-align: middle;
}
/* 按钮盒 22×22：必须显式清掉 padding——`<button>` 默认 `padding: 1px 6px`，
   在 border-box 下会把内容盒压到 8px，装不下 14px 的图标。 */
.order-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0;
  width: 22px;
  height: 22px;
  margin-left: 2px;
  vertical-align: middle;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background: var(--color-surface);
  color: var(--color-text-secondary);
  cursor: pointer;
  line-height: 1;
}
.order-btn:hover:not(:disabled) {
  background: var(--color-accent-light);
  color: var(--color-text-primary);
}
.order-btn:disabled {
  opacity: 0.35;
  cursor: not-allowed;
}
</style>
