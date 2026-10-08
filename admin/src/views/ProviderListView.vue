<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { providersApi } from '../api'
import { API_PROTOCOLS, protocolLabel, type AiProvider, type ApiProtocol } from '../api/providers'
import AppCard from '../components/AppCard.vue'
import AppButton from '../components/AppButton.vue'
import AppBadge from '../components/AppBadge.vue'
import AppModal from '../components/AppModal.vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import { errMsg } from '../lib/http'
import { toast } from '../lib/toast'
import { useConfirmAction } from '../lib/confirm'

const providers = ref<AiProvider[]>([])

async function load() {
  try {
    providers.value = await providersApi.listProviders()
  } catch (e) {
    toast.error(errMsg(e, '供应商加载失败'))
  }
}
onMounted(load)

// ---------- 添加/编辑弹窗（仅身份与凭据；模型条目在「对话模型链」/「嵌入模型链」管理） ----------
const editingId = ref<number | null>(null)
const showForm = ref(false)
const fApiType = ref<ApiProtocol>('openai-chat')
const fName = ref('')
const fBaseUrl = ref('')
const fKey = ref('')

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
</script>

<template>
  <div class="tab-panel">
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
          <p class="form-hint">秘钥加密存储于服务端，保存后不再回显。模型条目在「对话模型链」/「嵌入模型链」中按类型添加与排序。</p>
        </div>
        <div class="flex gap-2" style="justify-content: flex-end">
          <AppButton variant="secondary" icon="x" type="button" @click="showForm = false">取消</AppButton>
          <AppButton variant="primary" icon="save" type="submit">保存</AppButton>
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
  </div>
</template>
