<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue'
import {
  NButton,
  NCard,
  NDataTable,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NModal,
  NSelect,
  NTag,
  type DataTableColumns,
} from 'naive-ui'
import { Plus } from 'lucide-vue-next'
import { providersApi } from '@/api'
import { API_PROTOCOLS, protocolLabel, type AiProvider, type ApiProtocol } from '@/api/providers'
import SectionHeader from '@/components/SectionHeader.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { errMsg } from '@/lib/http'
import { toast } from '@/lib/toast'
import { useConfirmAction } from '@/lib/confirm'

const providers = ref<AiProvider[]>([])
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    providers.value = await providersApi.listProviders()
  } catch (e) {
    toast.error(errMsg(e, '供应商加载失败'))
  } finally {
    loading.value = false
  }
}
onMounted(load)

// ---------- 添加/编辑弹窗（仅身份与凭据；模型条目在「对话模型链」/「嵌入模型链」管理） ----------
const saving = ref(false)
const editingId = ref<number | null>(null)
const showForm = ref(false)
const fApiType = ref<ApiProtocol>('openai-chat')
const fName = ref('')
const fBaseUrl = ref('')
const headerTimeoutNum = ref<number | null>(null) // null = 默认 30s
const fKey = ref('')

const protocolOptions = API_PROTOCOLS.map((p) => ({ label: p.label, value: p.value }))

const protocolHint = computed(() => API_PROTOCOLS.find((p) => p.value === fApiType.value)?.hint ?? '')

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
  headerTimeoutNum.value = null
  fKey.value = ''
  showForm.value = true
}

function openEdit(p: AiProvider) {
  editingId.value = p.id
  fApiType.value = p.api_type
  fName.value = p.name
  fBaseUrl.value = p.base_url
  headerTimeoutNum.value = p.header_timeout_sec
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
  saving.value = true
  try {
    // 永远显式传超时字段（0 = 默认 30s，服务端归一 NULL）：update 侧「字段缺失 = 不修改」，
    // 不传的话「清空超时」这个操作永远不会生效。
    const payload = {
      api_type: fApiType.value,
      name: fName.value.trim() || undefined,
      base_url: fBaseUrl.value.trim(),
      header_timeout_sec: headerTimeoutNum.value ?? 0,
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
  } finally {
    saving.value = false
  }
}

// 删除走确认弹窗（不用原生 confirm）；结果用全局 toast 反馈
const remove = useConfirmAction<AiProvider>('删除失败')

async function runRemoveProvider(p: AiProvider) {
  await providersApi.deleteProvider(p.id)
  toast.success(`已删除供应商「${p.name}」`)
  await load()
}

const columns: DataTableColumns<AiProvider> = [
  { title: '名称', key: 'name', minWidth: 140 },
  { title: '协议', key: 'api_type', minWidth: 180, render: (row) => h('code', { class: 'cell-code' }, protocolLabel(row.api_type)) },
  { title: '地址', key: 'base_url', minWidth: 220, ellipsis: { tooltip: true } },
  {
    title: 'Key',
    key: 'has_key',
    width: 90,
    render: (row) =>
      h(NTag, { type: row.has_key ? 'success' : 'default', size: 'small', bordered: false }, { default: () => (row.has_key ? '已配置' : '未配置') }),
  },
  {
    title: '',
    key: 'actions',
    width: 130,
    align: 'right',
    render: (row) =>
      h('div', { class: 'ops-cell' }, [
        h(NButton, { size: 'small', quaternary: true, onClick: () => openEdit(row) }, { default: () => '编辑' }),
        h(NButton, { size: 'small', type: 'error', secondary: true, onClick: () => (remove.target.value = row) }, { default: () => '删除' }),
      ]),
  },
]
</script>

<template>
  <div class="view">
    <SectionHeader :title="`AI 供应商（${providers.length}）`" desc="供应商 = 协议 + 地址 + 密钥；模型条目在模型链页按类型管理。">
      <NButton type="primary" @click="openCreate">
        <template #icon>
          <Plus :size="15" :stroke-width="1.8" />
        </template>
        添加供应商
      </NButton>
    </SectionHeader>

    <NCard size="small" class="table-card">
      <NDataTable
        :columns="columns"
        :data="providers"
        :loading="loading"
        flex-height
        style="flex: 1; min-height: 0"
        :scroll-x="760"
        :bordered="false"
        :row-key="(row: AiProvider) => row.id"
      />
    </NCard>

    <NModal
      v-model:show="showForm"
      preset="card"
      :title="editingId === null ? '添加 AI 供应商' : '编辑供应商'"
      :style="{ width: '560px' }"
      :bordered="true"
    >
      <NForm label-placement="top" size="medium" @submit.prevent="submitForm">
        <div class="form-row">
          <NFormItem label="API 类型（协议）">
            <NSelect v-model:value="fApiType" :options="protocolOptions" />
          </NFormItem>
          <NFormItem label="名称（可选）">
            <NInput v-model:value="fName" placeholder="默认取协议名，如 DeepSeek" />
          </NFormItem>
        </div>
        <p class="hint">{{ protocolHint }}</p>
        <NFormItem label="服务地址" required>
          <NInput v-model:value="fBaseUrl" :placeholder="baseUrlPlaceholder" />
        </NFormItem>
        <NFormItem label="响应头超时（秒，可选）">
          <NInputNumber
            v-model:value="headerTimeoutNum"
            :min="1"
            :max="3600"
            placeholder="默认 30"
            clearable
            class="timeout-input"
          />
        </NFormItem>
        <p class="hint">等待模型服务返回首包的上限；上游挂起时按它快速判死并切链上下一个供应商。</p>
        <NFormItem :label="`API Key${editingId === null ? '' : '（留空 = 不修改）'}`" :required="editingId === null">
          <NInput
            v-model:value="fKey"
            type="password"
            show-password-on="click"
            :placeholder="editingId === null ? '粘贴服务商提供的密钥' : '••••••••'"
          />
        </NFormItem>
        <p class="hint">
          密钥加密存储于服务端，保存后不再回显。模型条目在「对话模型链」/「嵌入模型链」中按类型添加与排序。
        </p>
      </NForm>
      <template #footer>
        <div class="modal-actions">
          <NButton @click="showForm = false">取消</NButton>
          <NButton type="primary" :loading="saving" @click="submitForm">保存</NButton>
        </div>
      </template>
    </NModal>

    <ConfirmDialog
      v-if="remove.target.value"
      title="删除供应商"
      :message="`删除供应商「${remove.target.value.name}」（${remove.target.value.base_url}）？其名下模型条目一并移除，链序自动收敛；使用该供应商的模型调用将失败。`"
      confirm-label="删除"
      :busy="remove.busy.value"
      @close="remove.target.value = null"
      @confirm="remove.confirm(runRemoveProvider)"
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

.form-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: var(--space-4);
}

.hint {
  margin: 0 0 var(--space-3);
  font-size: var(--text-sm);
  color: var(--color-ink-tertiary);
}

.timeout-input {
  width: var(--control-md);
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
