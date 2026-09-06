<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { providersApi } from '../api'
import type { AiProvider, ApiProtocol, ModelEntry } from '../api/providers'
import { API_PROTOCOLS, protocolLabel } from '../api/providers'
import AppCard from '../components/AppCard.vue'
import AppButton from '../components/AppButton.vue'
import AppBadge from '../components/AppBadge.vue'
import AppModal from '../components/AppModal.vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import { toast } from '../lib/toast'
import AppAlert from '../components/AppAlert.vue'

const providers = ref<AiProvider[]>([])

async function load() {
  try {
    providers.value = await providersApi.listProviders()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '供应商列表加载失败')
  }
}
onMounted(load)

// ---------- 供应商 添加/编辑弹窗 ----------
const editingId = ref<number | null>(null)
const showForm = ref(false)
const fApiType = ref<ApiProtocol>('openai-chat')
const fName = ref('')
const fBaseUrl = ref('')
const fKey = ref('')
const fModels = ref<ModelEntry[]>([])
const fDefaultModel = ref('')
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
  fModels.value = []
  fDefaultModel.value = ''
  showForm.value = true
}

function openEdit(p: AiProvider) {
  editingId.value = p.id
  fApiType.value = p.api_type
  fName.value = p.name
  fBaseUrl.value = p.base_url
  fKey.value = ''
  fModels.value = p.models.map((m) => ({ ...m }))
  fDefaultModel.value = p.default_model
  showForm.value = true
}

// 获取模型列表：保留已填参数（按模型 id 对应），新模型参数默认不设置
async function fetchModelList() {
  if (!fBaseUrl.value.trim()) {
    toast.error('请先填写服务地址')
    return
  }
  fetching.value = true
  try {
    const res = await providersApi.fetchModels({
      api_type: fApiType.value,
      base_url: fBaseUrl.value.trim(),
      api_key: fKey.value.trim() || undefined,
      provider_id: editingId.value ?? undefined,
    })
    const prev = new Map(fModels.value.filter((m) => m.id.trim()).map((m) => [m.id, m]))
    fModels.value = res.models.map((id) => prev.get(id) ?? { id, temperature: null, max_tokens: null })
    if (!fModels.value.some((m) => m.id === fDefaultModel.value)) {
      fDefaultModel.value = fModels.value[0]?.id ?? ''
    }
    toast.success('模型列表已获取')
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '获取模型列表失败')
  } finally {
    fetching.value = false
  }
}

function addManualModel() {
  const last = fModels.value[fModels.value.length - 1]
  if (last && !last.id.trim()) return
  fModels.value.push({ id: '', temperature: null, max_tokens: null })
}

function removeModelRow(i: number) {
  const removed = fModels.value[i]
  fModels.value.splice(i, 1)
  if (removed.id === fDefaultModel.value) {
    fDefaultModel.value = fModels.value[0]?.id ?? ''
  }
}

// 空输入框（''）归一化为 null = 不传参
function normalizeModels(): ModelEntry[] {
  return fModels.value
    .filter((m) => m.id.trim())
    .map((m) => ({
      id: m.id.trim(),
      temperature: m.temperature === null || (m.temperature as unknown as string) === '' ? null : Number(m.temperature),
      max_tokens: m.max_tokens === null || (m.max_tokens as unknown as string) === '' ? null : Number(m.max_tokens),
    }))
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
  const models = normalizeModels()
  if (models.length > 0 && !models.some((m) => m.id === fDefaultModel.value)) {
    toast.error('请在模型列表中勾选一个默认模型')
    return
  }
  try {
    const payload = {
      api_type: fApiType.value,
      name: fName.value.trim() || undefined,
      base_url: fBaseUrl.value.trim(),
      models,
      default_model: fDefaultModel.value,
    }
    if (editingId.value === null) {
      await providersApi.createProvider({ ...payload, api_key: fKey.value.trim() })
    } else {
      const updated = await providersApi.updateProvider(editingId.value, payload)
      const i = providers.value.findIndex((p) => p.id === updated.id)
      if (i >= 0) providers.value[i] = updated
      if (fKey.value.trim()) await providersApi.setProviderKey(editingId.value, fKey.value.trim())
    }
    showForm.value = false
    toast.success(editingId.value === null ? '供应商已创建' : '供应商已保存')
    await load()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '保存失败')
  }
}

// 删除走确认弹窗（不用原生 confirm）；结果用全局 toast 反馈
const confirmRemove = ref<AiProvider | null>(null)
const removing = ref(false)

async function doRemoveProvider() {
  const p = confirmRemove.value
  if (!p) return
  removing.value = true
  try {
    await providersApi.deleteProvider(p.id)
    toast.success(`已删除供应商「${p.name}」`)
    confirmRemove.value = null
    await load()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '删除失败')
  } finally {
    removing.value = false
  }
}

// 排序即故障切换链：乐观更新 + 服务端持久化，失败回滚重载
async function move(idx: number, dir: -1 | 1) {
  const target = idx + dir
  if (target < 0 || target >= providers.value.length) return
  const next = [...providers.value]
  ;[next[idx], next[target]] = [next[target], next[idx]]
  providers.value = next
  try {
    providers.value = await providersApi.reorderProviders(next.map((p) => p.id))
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '排序保存失败')
    await load()
  }
}
</script>

<template>
  <div class="tab-panel">
    <AppAlert variant="info" icon="info">
      供应商按列表顺序使用：从第一个开始，失败依次向下切换（排序即故障切换策略）。
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
            <tr><th style="width: 44px">顺序</th><th>名称</th><th>协议</th><th>地址</th><th>默认模型</th><th>Key</th><th></th></tr>
          </thead>
          <tbody>
            <tr v-for="(p, idx) in providers" :key="p.id">
              <td>
                <span class="order-num">{{ idx + 1 }}</span>
                <button class="order-btn" type="button" title="上移" :disabled="idx === 0" @click="move(idx, -1)">↑</button>
                <button class="order-btn" type="button" title="下移" :disabled="idx === providers.length - 1" @click="move(idx, 1)">↓</button>
              </td>
              <td>{{ p.name }}</td>
              <td><code>{{ protocolLabel(p.api_type) }}</code></td>
              <td class="text-secondary" style="word-break: break-all; max-width: 240px">{{ p.base_url }}</td>
              <td><code>{{ p.default_model || '—' }}</code></td>
              <td>
                <AppBadge :variant="p.has_key ? 'success' : 'neutral'">{{ p.has_key ? '已配置' : '未配置' }}</AppBadge>
              </td>
              <td class="text-right">
                <div class="flex gap-2" style="justify-content: flex-end">
                  <AppButton variant="secondary" size="sm" icon="edit" @click="openEdit(p)">编辑</AppButton>
                  <AppButton variant="danger" size="sm" icon="trash" @click="confirmRemove = p">删除</AppButton>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </AppCard>

    <AppModal
      v-if="showForm"
      :title="editingId === null ? '添加 AI 供应商' : '编辑供应商'"
      :width="640"
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
          <p class="form-hint">秘钥加密存储于服务端，保存后不再回显。</p>
        </div>
        <div class="form-group" style="margin: 0">
          <label class="form-label">模型列表与参数</label>
          <div class="flex gap-2" style="margin-bottom: 8px">
            <AppButton variant="secondary" icon="refresh-cw" type="button" :disabled="fetching" @click="fetchModelList">
              {{ fetching ? '获取中…' : '获取模型列表' }}
            </AppButton>
            <AppButton variant="secondary" icon="plus" type="button" @click="addManualModel">手动添加模型</AppButton>
          </div>
          <table v-if="fModels.length > 0" class="model-table">
            <thead>
              <tr><th>默认</th><th>模型</th><th>Temperature</th><th>Max Tokens</th><th></th></tr>
            </thead>
            <tbody>
              <tr v-for="(m, i) in fModels" :key="i">
                <td>
                  <input
                    type="radio"
                    name="default-model"
                    :checked="fDefaultModel === m.id"
                    @change="fDefaultModel = m.id"
                  />
                </td>
                <td><input v-model="m.id" class="form-input" placeholder="模型名" /></td>
                <td>
                  <input v-model.number="m.temperature" class="form-input" type="number" min="0" max="2" step="0.1" placeholder="不传" />
                </td>
                <td>
                  <input v-model.number="m.max_tokens" class="form-input" type="number" min="256" max="32768" step="256" placeholder="不传" />
                </td>
                <td>
                  <button class="order-btn" type="button" title="移除该模型" @click="removeModelRow(i)">✕</button>
                </td>
              </tr>
            </tbody>
          </table>
          <p v-else class="form-hint">
            填好地址与秘钥后点击「获取模型列表」；也可手动添加模型。参数留空 = 调用时不传，使用供应商默认值。
          </p>
        </div>
        <div class="flex gap-2" style="justify-content: flex-end">
          <AppButton variant="secondary" icon="x" type="button" @click="showForm = false">取消</AppButton>
          <AppButton variant="primary" icon="save" type="submit">保存</AppButton>
        </div>
      </form>
    </AppModal>

    <ConfirmDialog
      v-if="confirmRemove"
      title="删除供应商"
      :message="`删除供应商「${confirmRemove.name}」（${confirmRemove.base_url}）？删除后使用该供应商的模型调用将失败。`"
      confirm-label="删除"
      :busy="removing"
      @close="confirmRemove = null"
      @confirm="doRemoveProvider"
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
.model-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}
.model-table th,
.model-table td {
  text-align: left;
  padding: 6px 8px;
  border-bottom: 1px solid var(--color-border);
  vertical-align: middle;
}
.model-table th {
  color: var(--color-text-tertiary);
  font-weight: 500;
}
.model-table .form-input {
  width: 100%;
  min-width: 90px;
}
</style>
