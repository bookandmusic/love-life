<script setup lang="ts">
import { computed, h, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import {
  NAlert,
  NButton,
  NCard,
  NCheckbox,
  NCheckboxGroup,
  NDataTable,
  NForm,
  NFormItem,
  NInputNumber,
  NModal,
  NSelect,
  NTag,
  type DataTableColumns,
} from 'naive-ui'
import { ChevronDown, ChevronUp, Plus, RefreshCw } from 'lucide-vue-next'
import { providersApi } from '@/api'
import { THINKING_OPTIONS, protocolLabel, type AiProvider, type ModelKind, type ModelRow, type ThinkingLevel } from '@/api/providers'
import SectionHeader from '@/components/SectionHeader.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { errMsg } from '@/lib/http'
import { toast } from '@/lib/toast'
import { useConfirmAction } from '@/lib/confirm'

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
    .filter((p) => p.has_key || editingModelId.value !== null)
    .map((p) => ({ label: `${p.name}（${protocolLabel(p.api_type)}）`, value: p.id })),
)

// 声明集恒含「关」（关是任何模型的合法选择）：下拉选项、回落判断、提交串三处共用。
const withOff = (levels: ThinkingLevel[]): ThinkingLevel[] => [...new Set([...levels, 'off' as ThinkingLevel])]

// 「思考」下拉的选项 = 该模型声明的支持档位（含恒有的「关」）。
const thinkingChoices = computed(() => {
  const allowed = withOff(mThinkingLevels.value)
  return THINKING_OPTIONS.filter((o) => allowed.includes(o.value)).map((o) => ({ label: o.label, value: o.value }))
})

// 取消勾选当前选中的档位时，把「思考」回落到「关」：
// 不回落就会提交声明集外的档位，被服务端 400 拒绝而界面上看不出哪里不对。
watch(mThinkingLevels, (levels) => {
  if (!withOff(levels).includes(mThinking.value)) mThinking.value = 'off'
})

/** 档位的显示名（列表用；取值面只在 THINKING_OPTIONS 一处）。 */
function thinkingLabel(level: ThinkingLevel): string {
  return THINKING_OPTIONS.find((o) => o.value === level)?.label ?? level
}

const thinkingLevelOptions = THINKING_OPTIONS.map((o) => ({ label: o.label, value: o.value }))

const fetching = ref(false)
/** 「取候选」取回的模型名候选（来自当前所选供应商），交给下拉选择。 */
const modelCandidates = ref<string[]>([])
/** 下拉选项；tag 模式允许手填任意值，空列表 = 全手填 */
const modelSelectOptions = computed(() =>
  modelCandidates.value.map((m) => ({ label: m, value: m })),
)

// 获取模型列表：以所选供应商的地址与已存秘钥为准。
// 取回的是**候选**而非结论——全部交给下拉，不替用户挑一个。
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
  mProviderId.value = row?.provider_id ?? providerOptions.value[0]?.value ?? null
  mModelId.value = row?.model_id ?? ''
  mMaxTokens.value = row?.max_tokens ?? null
  mThinking.value = row?.thinking ?? 'off'
  mThinkingLevels.value = row ? (row.thinking_levels.split(',').filter(Boolean) as ThinkingLevel[]) : ['off']
  mContextWindow.value = row?.context_window ?? null
  // 候选取自「供应商 + 地址」：每次打开都从空开始，不带着上次的残留
  modelCandidates.value = []
  showModelForm.value = true
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
    // 声明集恒含「关」（关是任何模型的合法选择），顺序交给服务端归一
    const levels = withOff(mThinkingLevels.value).join(',')
    if (editingModelId.value === null) {
      list.value = (
        await providersApi.createModel({
          kind: kind.value,
          provider_id: mProviderId.value!,
          model_id: mModelId.value.trim(),
          max_tokens: mMaxTokens.value,
          thinking: mThinking.value,
          thinking_levels: levels,
          context_window: mContextWindow.value,
        })
      )[kind.value]
    } else {
      list.value = (
        await providersApi.updateModel(editingModelId.value, {
          model_id: mModelId.value.trim(),
          max_tokens: mMaxTokens.value,
          thinking: mThinking.value,
          thinking_levels: levels,
          context_window: mContextWindow.value,
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

// 候选取自「供应商 + 地址」：换供应商后候选失效，需按新供应商重取
watch(mProviderId, () => {
  modelCandidates.value = []
})

const columns: DataTableColumns<ModelRow> = [
  {
    title: '链序',
    key: 'order',
    width: 110,
    render: (_row, idx) =>
      h('span', { class: 'order-cell' }, [
        h('span', { class: 'order-num' }, String(idx + 1)),
        h('button', {
          class: 'order-btn',
          type: 'button',
          title: '上移',
          disabled: idx === 0,
          onClick: () => moveModel(idx, -1),
        }, [h(ChevronUp, { size: 14, 'stroke-width': 1.8 })]),
        h('button', {
          class: 'order-btn',
          type: 'button',
          title: '下移',
          disabled: idx === list.value.length - 1,
          onClick: () => moveModel(idx, 1),
        }, [h(ChevronDown, { size: 14, 'stroke-width': 1.8 })]),
      ]),
  },
  { title: '供应商', key: 'provider_name', minWidth: 120 },
  { title: '协议', key: 'api_type', minWidth: 150, render: (row) => h('code', { class: 'cell-code' }, protocolLabel(row.api_type)) },
  { title: '模型', key: 'model_id', minWidth: 160, render: (row) => h('code', { class: 'cell-code' }, row.model_id) },
  { title: '思考', key: 'thinking', width: 70, render: (row) => thinkingLabel(row.thinking) },
  { title: '支持的思考', key: 'thinking_levels', width: 130, render: (row) => h('code', { class: 'cell-code' }, row.thinking_levels) },
  { title: '上下文', key: 'context_window', width: 90, render: (row) => String(row.context_window ?? '—') },
  { title: '最大输出', key: 'max_tokens', width: 90, render: (row) => String(row.max_tokens ?? '—') },
  {
    title: '',
    key: 'actions',
    width: 130,
    align: 'right',
    render: (row) =>
      h('div', { class: 'ops-cell' }, [
        h(NButton, { size: 'small', quaternary: true, onClick: () => openModelForm(row) }, { default: () => '编辑' }),
        h(NButton, { size: 'small', type: 'error', secondary: true, onClick: () => (modelRemove.target.value = row) }, { default: () => '移除' }),
      ]),
  },
]
</script>

<template>
  <div class="view">
    <NAlert type="info" :show-icon="false">
      模型条目按类型分两条链：对话与语义检索各自独立，调用从链首开始、失败依次向下切换（链内排序即故障切换策略）。
    </NAlert>

    <SectionHeader :title="`${kindLabel[kind]}（${list.length}）`">
      <NTag v-if="kind === 'embedding'" :type="list.length ? 'success' : 'default'" size="small" :bordered="false">
        {{ list.length ? '语义检索已启用' : '未启用（回落关键词）' }}
      </NTag>
      <NButton type="primary" @click="openModelForm(null)">
        <template #icon>
          <Plus :size="15" :stroke-width="1.8" />
        </template>
        添加模型条目
      </NButton>
    </SectionHeader>

    <NCard size="small" class="table-card">
      <NDataTable
        :columns="columns"
        :data="list"
        flex-height
        style="flex: 1; min-height: 0"
        :scroll-x="900"
        :bordered="false"
        :row-key="(row: ModelRow) => row.id"
      />
    </NCard>

    <NAlert v-if="kind === 'embedding'" type="warning" :show-icon="false">
      嵌入由链上供应商完成：动态正文会发送到该地址，向量结果只存在本机。如需零出站，可指向本机 OpenAI 兼容服务，例如
      <code>http://127.0.0.1:11434/v1</code> + <code>bge-m3</code>。链为空或全部失败时，AI 搜索自动回落关键词匹配。
    </NAlert>

    <NModal
      v-model:show="showModelForm"
      preset="card"
      :title="(editingModelId === null ? '添加' : '编辑') + kindLabel[kind] + '条目'"
      :style="{ width: '560px' }"
      :bordered="true"
    >
      <NForm label-placement="top" size="medium" @submit.prevent="submitModelForm">
        <NFormItem label="供应商">
          <NSelect v-model:value="mProviderId" :options="providerOptions" :disabled="editingModelId !== null" placeholder="选择供应商" />
        </NFormItem>
        <p v-if="kind === 'embedding' && providerOptions.length === 0" class="hint-block">
          嵌入只允许 OpenAI Chat 协议且已配 Key 的供应商；请先在「供应商」页添加。
        </p>
        <NFormItem label="模型名" required>
          <div class="model-id-row">
            <!--
 NSelect(filterable+tag)：点开即见全部候选（AutoComplete 必须先输入才显示列表），
                 tag 模式允许手填任意值——「下拉选」与「手填」一个控件两用 
-->
            <NSelect
              v-model:value="mModelId"
              :options="modelSelectOptions"
              filterable
              tag
              clearable
              placeholder="如 gpt-5.5 / claude-sonnet-5-5 / bge-m3"
            />
            <NButton v-if="editingModelId === null" :loading="fetching" @click="fetchModelList">
              <template #icon>
                <RefreshCw :size="14" :stroke-width="1.8" />
              </template>
              {{ fetching ? '获取中…' : '取候选' }}
            </NButton>
          </div>
        </NFormItem>
        <p class="hint-block">「取候选」用所选供应商的地址与已存秘钥请求其模型列表，取回后从下拉中选择，也可直接输入。</p>
        <div class="form-row">
          <NFormItem label="最大输出（可选）">
            <NInputNumber v-model:value="mMaxTokens" :min="256" :max="32768" placeholder="不传" clearable class="num-input" />
          </NFormItem>
          <NFormItem label="上下文窗口（可选）">
            <NInputNumber v-model:value="mContextWindow" :min="1024" :max="2000000" placeholder="不传" clearable class="num-input" />
          </NFormItem>
        </div>
        <p class="hint-block">上下文窗口是模型的 token 上限，用于裁剪送入模型的历史；不填则用服务端兜底预算。最大输出不得超过它。</p>
        <NFormItem label="支持的思考档位">
          <NCheckboxGroup v-model:value="mThinkingLevels">
            <div class="checkbox-row">
              <NCheckbox v-for="o in thinkingLevelOptions" :key="o.value" :value="o.value" :label="o.label" />
            </div>
          </NCheckboxGroup>
        </NFormItem>
        <p class="hint-block">
          按供应商文档填该模型实际支持的档位（如 Gemini 3 Pro 只有低/高，选「中」会被供应商拒绝）。「关」恒在集合内。
        </p>
        <NFormItem label="思考">
          <NSelect v-model:value="mThinking" :options="thinkingChoices" />
        </NFormItem>
        <p class="hint-block">
          开启后模型会先思考再作答，思考同样消耗 token（按模型计费）。「关」= 该模型的最低档——现役模型无法真正关闭思考，省略参数只会让它留在默认档。
        </p>
      </NForm>
      <template #footer>
        <div class="modal-actions">
          <NButton @click="showModelForm = false">取消</NButton>
          <NButton type="primary" :loading="mSaving" @click="submitModelForm">保存</NButton>
        </div>
      </template>
    </NModal>

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
.view {
  display: flex;
  flex-direction: column;
  height: 100%;
  gap: var(--space-5);
}

.table-card {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

/* 链序与模型名不折行：一折行序号 + 上下移按钮就叠成三行，行高被抬到三倍 */
.order-cell {
  white-space: nowrap;
  display: inline-flex;
  align-items: center;
}

.order-num {
  display: inline-block;
  min-width: var(--space-4);
  font-weight: 600;
  color: var(--color-ink-secondary);
}

/* 按钮盒必须显式清掉 padding——`<button>` 默认 padding 会把内容盒压小、装不下图标。 */
.order-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0;
  width: 22px;
  height: 22px;
  margin-left: var(--space-1);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background: var(--color-canvas);
  color: var(--color-ink-secondary);
  cursor: pointer;
  line-height: 1;
  transition: background var(--transition), color var(--transition);
}

.order-btn:hover:not(:disabled) {
  background: var(--color-subtle);
  color: var(--color-ink);
}

.order-btn:disabled {
  opacity: 0.35;
  cursor: not-allowed;
}

.model-id-row {
  display: flex;
  gap: var(--space-2);
  width: 100%;
}

.form-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: var(--space-4);
}

.num-input {
  width: 100%;
}

.checkbox-row {
  display: flex;
  gap: var(--space-4);
  flex-wrap: wrap;
}

.hint-block {
  margin: 0 0 var(--space-3);
  font-size: var(--text-sm);
  color: var(--color-ink-tertiary);
}

.table-card :deep(.cell-code) {
  font-family: var(--font-mono);
  font-size: var(--text-sm);
}

.table-card :deep(.ops-cell) {
  display: inline-flex;
  gap: var(--space-2);
}
</style>
