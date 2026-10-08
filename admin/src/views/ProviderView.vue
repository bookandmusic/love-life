<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { providersApi } from '../api'
import { THINKING_OPTIONS, type AiProvider, type ApiProtocol, type ModelKind, type ModelRow, type ThinkingLevel } from '../api/providers'
import { API_PROTOCOLS, protocolLabel } from '../api/providers'
import AppCard from '../components/AppCard.vue'
import AppButton from '../components/AppButton.vue'
import AppBadge from '../components/AppBadge.vue'
import AppModal from '../components/AppModal.vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import { errMsg } from '../lib/http'
import { toast } from '../lib/toast'
import { useConfirmAction } from '../lib/confirm'
import AppAlert from '../components/AppAlert.vue'

const providers = ref<AiProvider[]>([])
const chains = ref<Record<ModelKind, ModelRow[]>>({ chat: [], embedding: [] })

async function load() {
  try {
    providers.value = await providersApi.listProviders()
    chains.value = await providersApi.listModels()
  } catch (e) {
    toast.error(errMsg(e, '模型配置加载失败'))
  }
}
onMounted(load)

function providerName(id: number): string {
  return providers.value.find((p) => p.id === id)?.name ?? `供应商 #${id}`
}

// ---------- 供应商 添加/编辑弹窗（仅身份与凭据；模型条目在「模型与链路」管理） ----------
const editingId = ref<number | null>(null)
const showForm = ref(false)
const fApiType = ref<ApiProtocol>('openai-chat')
const fName = ref('')
const fBaseUrl = ref('')
const fKey = ref('')
const fetching = ref(false)

const protocolHint = computed(
  () => API_PROTOCOLS.find((p) => p.value === fApiType.value)?.hint ?? '',
)
const baseUrlPlaceholder = computed(
  () =>
    ({
      'openai-chat': 'https://api.openai.com/v1',
      'openai-responses': 'https://api.openai.com/v1',
      anthropic: 'https://api.anthropic.com',
      gemini: 'https://generativelanguage.googleapis.com',
    })[fApiType.value] ?? 'https://…',
)

function openCreate() {
  editingId.value = null
  fApiType.value = 'openai-chat'
  fName.value = ''
  fBaseUrl.value = ''
  fKey.value = ''
  showForm.value = true
}

function openEdit(p: AiProvider) {
  editingId.value = p.id
  fApiType.value = p.api_type
  fName.value = p.name
  fBaseUrl.value = p.base_url
  fKey.value = ''
  showForm.value = true
}

async function submitForm() {
  if (!fBaseUrl.value.trim()) {
    toast.error('服务地址必填')
    return
  }
  if (editingId.value === null && !fKey.value.trim()) {
    toast.error('API Key 必填')
    return
  }
  try {
    const payload = {
      api_type: fApiType.value,
      name: fName.value.trim() || undefined,
      base_url: fBaseUrl.value.trim(),
    }
    if (editingId.value === null) {
      await providersApi.createProvider({ ...payload, api_key: fKey.value.trim() })
    } else if (fKey.value.trim()) {
      await providersApi.updateProvider(editingId.value, payload)
      await providersApi.setProviderKey(editingId.value, fKey.value.trim())
    } else {
      await providersApi.updateProvider(editingId.value, payload)
    }
    showForm.value = false
    toast.success(editingId.value === null ? '供应商已创建' : '供应商已保存')
    await load()
  } catch (e) {
    toast.error(errMsg(e, '保存失败'))
  }
}

// 删除走确认弹窗（不用原生 confirm）；结果用全局 toast 反馈
const remove = useConfirmAction<AiProvider>('删除失败')

async function runRemoveProvider(p: AiProvider) {
  await providersApi.deleteProvider(p.id)
  toast.success(`已删除供应商「${p.name}」`)
  await load()
}

// ---------- 模型与链路（chat / embedding 两条链） ----------
const showModelForm = ref(false)
const editingModelId = ref<number | null>(null)
const mKind = ref<ModelKind>('chat')
const mProviderId = ref<number | null>(null)
const mModelId = ref('')
const mMaxTokens = ref<number | null>(null)
const mThinking = ref<ThinkingLevel>('off')
/** 该模型支持的思考档位（多选）：档位取值是模型属性，故由这里声明，不在代码里判模型名。 */
const mThinkingLevels = ref<ThinkingLevel[]>(['off'])
const mContextWindow = ref<number | null>(null)
const mSaving = ref(false)

const kindLabel: Record<ModelKind, string> = { chat: '对话模型链', embedding: '嵌入模型链' }

// 嵌入走 OpenAI 兼容 /embeddings，仅 OpenAI Chat 协议供应商可用（服务端同口径校验）
const providerOptions = computed(() =>
  providers.value
    .filter((p) => (mKind.value === 'embedding' ? p.api_type === 'openai-chat' : true))
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

// 获取模型列表：以所选供应商的地址与已存秘钥为准
async function fetchModelList() {
  const p = providers.value.find((x) => x.id === mProviderId.value)
  if (!p) {
    toast.error('请先选择供应商')
    return
  }
  fetching.value = true
  try {
    const res = await providersApi.fetchModels({ api_type: p.api_type, base_url: p.base_url, provider_id: p.id })
    const hit = res.models.find((id) => id === mModelId.value.trim())
    if (!hit && res.models.length > 0) mModelId.value = res.models[0]
    toast.success('模型列表已获取，请从常用名中选用')
  } catch (e) {
    toast.error(errMsg(e, '获取模型列表失败'))
  } finally {
    fetching.value = false
  }
}

function openModelCreate(kind: ModelKind) {
  editingModelId.value = null
  mKind.value = kind
  mProviderId.value = providerOptions.value[0]?.id ?? null
  mModelId.value = ''
  mMaxTokens.value = null
  mThinking.value = 'off'
  mThinkingLevels.value = ['off']
  mContextWindow.value = null
  showModelForm.value = true
}

function openModelEdit(row: ModelRow) {
  editingModelId.value = row.id
  mKind.value = row.kind
  mProviderId.value = row.provider_id
  mModelId.value = row.model_id
  mMaxTokens.value = row.max_tokens
  mThinking.value = row.thinking
  mThinkingLevels.value = row.thinking_levels.split(',').filter(Boolean) as ThinkingLevel[]
  mContextWindow.value = row.context_window
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
      chains.value = await providersApi.createModel({
        kind: mKind.value,
        provider_id: mProviderId.value!,
        model_id: mModelId.value.trim(),
        max_tokens: maxTok,
        thinking: mThinking.value,
        thinking_levels: levels,
        context_window: ctxWin,
      })
    } else {
      chains.value = await providersApi.updateModel(editingModelId.value, {
        model_id: mModelId.value.trim(),
        max_tokens: maxTok,
        thinking: mThinking.value,
        thinking_levels: levels,
        context_window: ctxWin,
      })
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
  chains.value = await providersApi.listModels()
}

// 链内排序：乐观更新 + 服务端持久化，失败回滚重载
async function moveModel(kind: ModelKind, idx: number, dir: -1 | 1) {
  const list = chains.value[kind]
  const target = idx + dir
  if (target < 0 || target >= list.length) return
  const next = [...list]
  ;[next[idx], next[target]] = [next[target], next[idx]]
  chains.value = { ...chains.value, [kind]: next }
  try {
    chains.value = await providersApi.reorderModels(kind, next.map((r) => r.id))
  } catch (e) {
    toast.error(errMsg(e, '排序保存失败'))
    chains.value = await providersApi.listModels()
  }
}
</script>

<template>
  <div class="tab-panel">
    <AppAlert variant="info" icon="info">
      模型条目按类型分两条链：对话与语义检索各自独立，调用从链首开始、失败依次向下切换（链内排序即故障切换策略）。
    </AppAlert>

    <div class="page-section-header">
      <h2 class="page-section-title">AI 供应商（{{ providers.length }}）</h2>
      <AppButton variant="primary" icon="plus" @click="openCreate">添加供应商</AppButton>
    </div>
    <AppCard class="card-fill">
      <div v-if="providers.length === 0" class="empty-state">
        <div class="empty-state-title">还没有配置供应商，点击右上角「添加供应商」</div>
      </div>
      <div v-else class="table-wrapper">
        <table>
          <thead>
            <tr><th>名称</th><th>协议</th><th>地址</th><th>Key</th><th></th></tr>
          </thead>
          <tbody>
            <tr v-for="p in providers" :key="p.id">
              <td>{{ p.name }}</td>
              <td><code>{{ protocolLabel(p.api_type) }}</code></td>
              <td class="text-secondary" style="word-break: break-all; max-width: 240px">{{ p.base_url }}</td>
              <td>
                <AppBadge :variant="p.has_key ? 'success' : 'neutral'">{{ p.has_key ? '已配置' : '未配置' }}</AppBadge>
              </td>
              <td class="text-right">
                <div class="flex gap-2" style="justify-content: flex-end">
                  <AppButton variant="secondary" size="sm" icon="edit" @click="openEdit(p)">编辑</AppButton>
                  <AppButton variant="danger" size="sm" icon="trash" @click="remove.target.value = p">删除</AppButton>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </AppCard>

    <template v-for="kind in (['chat', 'embedding'] as const)" :key="kind">
      <div class="page-section-header">
        <h2 class="page-section-title">{{ kindLabel[kind] }}（{{ chains[kind].length }}）</h2>
        <div class="flex gap-2 items-center">
          <AppBadge v-if="kind === 'embedding'" :variant="chains[kind].length ? 'success' : 'neutral'">
            {{ chains[kind].length ? '语义检索已启用' : '未启用（回落关键词）' }}
          </AppBadge>
          <AppButton variant="primary" icon="plus" @click="openModelCreate(kind)">添加模型条目</AppButton>
        </div>
      </div>
      <AppCard class="card-fill">
        <div v-if="chains[kind].length === 0" class="empty-state">
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
              <tr v-for="(row, idx) in chains[kind]" :key="row.id">
                <td>
                  <span class="order-num">{{ idx + 1 }}</span>
                  <button class="order-btn" type="button" title="上移" :disabled="idx === 0" @click="moveModel(kind, idx, -1)">↑</button>
                  <button class="order-btn" type="button" title="下移" :disabled="idx === chains[kind].length - 1" @click="moveModel(kind, idx, 1)">↓</button>
                </td>
                <td>{{ row.provider_name }}</td>
                <td><code>{{ protocolLabel(row.api_type) }}</code></td>
                <td><code>{{ row.model_id }}</code></td>
                <td>{{ thinkingLabel(row.thinking) }}</td>
                <td><code>{{ row.thinking_levels }}</code></td>
                <td>{{ row.context_window ?? '—' }}</td>
                <td>{{ row.max_tokens ?? '—' }}</td>
                <td class="text-right">
                  <div class="flex gap-2" style="justify-content: flex-end">
                    <AppButton variant="secondary" size="sm" icon="edit" @click="openModelEdit(row)">编辑</AppButton>
                    <AppButton variant="danger" size="sm" icon="trash" @click="modelRemove.target.value = row">移除</AppButton>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <AppAlert v-if="kind === 'embedding'" variant="warning" icon="alert-triangle" style="margin-top: 14px">
          嵌入由链上供应商完成：动态正文会发送到该地址，向量结果只存在本机。如需零出站，可指向本机 OpenAI 兼容服务，例如
          <code>http://127.0.0.1:11434/v1</code> + <code>bge-m3</code>。链为空或全部失败时，AI 搜索自动回落关键词匹配。
        </AppAlert>
      </AppCard>
    </template>

    <AppModal
      v-if="showForm"
      :title="editingId === null ? '添加 AI 供应商' : '编辑供应商'"
      :width="560"
      @close="showForm = false"
    >
      <form class="flex" style="flex-direction: column; gap: 14px" @submit.prevent="submitForm">
        <div class="flex gap-3">
          <div class="form-group" style="flex: 1; margin: 0">
            <label class="form-label">API 类型（协议）</label>
            <select v-model="fApiType" class="form-select">
              <option v-for="p in API_PROTOCOLS" :key="p.value" :value="p.value">{{ p.label }}</option>
            </select>
          </div>
          <div class="form-group" style="flex: 1; margin: 0">
            <label class="form-label">名称（可选）</label>
            <input v-model="fName" class="form-input" placeholder="默认取协议名，如 DeepSeek" />
          </div>
        </div>
        <p class="form-hint" style="margin: -6px 0 0">{{ protocolHint }}</p>
        <div class="form-group" style="margin: 0">
          <label class="form-label">服务地址</label>
          <input v-model="fBaseUrl" class="form-input" :placeholder="baseUrlPlaceholder" required />
        </div>
        <div class="form-group" style="margin: 0">
          <label class="form-label">API Key{{ editingId === null ? '' : '（留空 = 不修改）' }}</label>
          <input
            v-model="fKey"
            class="form-input"
            type="password"
            :placeholder="editingId === null ? '粘贴服务商提供的密钥' : '••••••••'"
            :required="editingId === null"
          />
          <p class="form-hint">秘钥加密存储于服务端，保存后不再回显。模型条目在下方「模型与链路」中按类型添加与排序。</p>
        </div>
        <div class="flex gap-2" style="justify-content: flex-end">
          <AppButton variant="secondary" icon="x" type="button" @click="showForm = false">取消</AppButton>
          <AppButton variant="primary" icon="save" type="submit">保存</AppButton>
        </div>
      </form>
    </AppModal>

    <AppModal
      v-if="showModelForm"
      :title="(editingModelId === null ? '添加' : '编辑') + kindLabel[mKind] + '条目'"
      :width="520"
      @close="showModelForm = false"
    >
      <form class="flex" style="flex-direction: column; gap: 14px" @submit.prevent="submitModelForm">
        <div class="form-group" style="margin: 0">
          <label class="form-label">供应商</label>
          <select v-model.number="mProviderId" class="form-select" :disabled="editingModelId !== null">
            <option v-for="p in providerOptions" :key="p.id" :value="p.id">{{ p.name }}（{{ protocolLabel(p.api_type) }}）</option>
          </select>
          <p v-if="mKind === 'embedding' && providerOptions.length === 0" class="form-hint">
            嵌入只允许 OpenAI Chat 协议且已配 Key 的供应商；请先在上方添加。
          </p>
        </div>
        <div class="form-group" style="margin: 0">
          <label class="form-label">模型名</label>
          <div class="flex gap-2">
            <input v-model="mModelId" class="form-input" placeholder="如 gpt-5.5 / claude-sonnet-5-5 / bge-m3" required />
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
          <p class="form-hint">「取候选」用所选供应商的地址与已存秘钥请求其模型列表，取回后填入第一个候选名。</p>
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
        <div class="form-group">
          <label class="form-label">支持的思考档位</label>
          <div class="flex gap-3">
            <label v-for="o in THINKING_OPTIONS" :key="o.value" class="flex" style="align-items: center; gap: 6px">
              <input v-model="mThinkingLevels" type="checkbox" :value="o.value" :disabled="thinkingDisabled && o.value !== 'off'" />
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
          <select v-model="mThinking" class="form-select" :disabled="thinkingDisabled">
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
      v-if="remove.target.value"
      title="删除供应商"
      :message="`删除供应商「${remove.target.value.name}」（${remove.target.value.base_url}）？其名下模型条目一并移除，链序自动收敛；使用该供应商的模型调用将失败。`"
      confirm-label="删除"
      :busy="remove.busy.value"
      @close="remove.target.value = null"
      @confirm="remove.confirm(runRemoveProvider)"
    />

    <ConfirmDialog
      v-if="modelRemove.target.value"
      title="移除模型条目"
      :message="`从${kindLabel[modelRemove.target.value.kind]}移除「${modelRemove.target.value.model_id}」（${modelRemove.target.value.provider_name}）？`"
      confirm-label="移除"
      :busy="modelRemove.busy.value"
      @close="modelRemove.target.value = null"
      @confirm="modelRemove.confirm(runRemoveModel)"
    />
  </div>
</template>

<style scoped>
.order-num {
  display: inline-block;
  min-width: 18px;
  font-weight: 600;
  color: var(--color-text-secondary);
}
.order-btn {
  width: 22px;
  height: 22px;
  margin-left: 2px;
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
