<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue'
import {
  NAlert,
  NButton,
  NCard,
  NDataTable,
  NForm,
  NFormItem,
  NInput,
  NModal,
  NSelect,
  NTag,
  type DataTableColumns,
} from 'naive-ui'
import { Plus } from 'lucide-vue-next'
import { webhooksApi } from '@/api'
import type { WebhookChannel } from '@/api/webhooks'
import SectionHeader from '@/components/SectionHeader.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { errMsg } from '@/lib/http'
import { toast } from '@/lib/toast'
import { useConfirmAction } from '@/lib/confirm'

const { WEBHOOK_EVENTS, WEBHOOK_PRESETS } = webhooksApi

const channels = ref<WebhookChannel[]>([])
const loading = ref(false)

// editingId 为 null = 新增；否则编辑该渠道
const saving = ref(false)
const editingId = ref<number | null>(null)
const showForm = ref(false)

const emptyForm = () => ({
  name: '',
  method: 'POST' as WebhookChannel['method'],
  url: '',
  headersText: '',
  body_template: '',
  events: ['new_post'] as string[],
})
const form = ref(emptyForm())

const methodOptions = ['GET', 'POST'].map((m) => ({ label: m, value: m }))

// headers 以「每行 Key: Value」录入，序列化为对象
function headersToText(headers: Record<string, string>): string {
  return Object.entries(headers ?? {})
    .map(([k, v]) => `${k}: ${v}`)
    .join('\n')
}

function parseHeaders(text: string): Record<string, string> {
  const out: Record<string, string> = {}
  for (const line of text.split('\n')) {
    const i = line.indexOf(':')
    if (i <= 0) continue
    const k = line.slice(0, i).trim()
    const v = line.slice(i + 1).trim()
    if (k) out[k] = v
  }
  return out
}

function eventLabel(key: string): string {
  return WEBHOOK_EVENTS.find((e) => e[0] === key)?.[1] ?? key
}

function applyPreset(name: string) {
  const url = WEBHOOK_PRESETS[name]
  if (url) {
    form.value.url = url
    form.value.method = 'POST'
    form.value.body_template = ''
  }
}

function toggleEvent(key: string) {
  const idx = form.value.events.indexOf(key)
  if (idx >= 0) form.value.events.splice(idx, 1)
  else form.value.events.push(key)
}

async function load() {
  loading.value = true
  try {
    const res = await webhooksApi.listWebhooks()
    channels.value = res.items
  } catch (e) {
    toast.error(errMsg(e, '渠道列表加载失败'))
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editingId.value = null
  form.value = emptyForm()
  showForm.value = true
}

function openEdit(c: WebhookChannel) {
  editingId.value = c.id
  form.value = {
    name: c.name,
    method: c.method,
    url: c.url,
    headersText: headersToText(c.headers),
    body_template: c.body_template,
    events: [...c.events],
  }
  showForm.value = true
}

async function save() {
  saving.value = true
  try {
    const payload = {
    name: form.value.name,
    method: form.value.method,
    url: form.value.url,
    headers: parseHeaders(form.value.headersText),
    body_template: form.value.body_template,
    events: form.value.events,
  }
    if (editingId.value === null) {
      await webhooksApi.createWebhook(payload)
    } else {
      await webhooksApi.updateWebhook(editingId.value, payload)
    }
    showForm.value = false
    toast.success(editingId.value === null ? '渠道已创建' : '渠道已保存')
    await load()
  } catch (e) {
    toast.error(errMsg(e, '保存失败'))
  } finally {
    saving.value = false
  }
}

async function test(c: WebhookChannel) {
  try {
    await webhooksApi.testWebhook(c.id)
    toast.success('已发送测试推送')
  } catch (e) {
    toast.error(errMsg(e, '发送失败'))
  }
}

// 删除走确认弹窗（不用原生 confirm）；结果用全局 toast 反馈
const remove = useConfirmAction<WebhookChannel>('删除失败')

async function runRemove(c: WebhookChannel) {
  await webhooksApi.deleteWebhook(c.id)
  toast.success('渠道已删除')
  await load()
}

onMounted(load)

const columns: DataTableColumns<WebhookChannel> = [
  { title: 'ID', key: 'id', width: 60 },
  {
    title: '名称',
    key: 'name',
    minWidth: 140,
    render: (row) =>
      h('span', { class: 'name-cell' }, [
        row.name || '—',
        Object.keys(row.headers ?? {}).length > 0
          ? h(NTag, { size: 'small', bordered: false }, { default: () => '含请求头' })
          : null,
      ]),
  },
  { title: '地址', key: 'url', minWidth: 240, ellipsis: { tooltip: true }, render: (row) => `${row.method} ${row.url}` },
  { title: '订阅', key: 'events', minWidth: 180, render: (row) => row.events.map(eventLabel).join('、') || '无' },
  {
    title: '',
    key: 'actions',
    width: 170,
    align: 'right',
    render: (row) =>
      h('div', { class: 'ops-cell' }, [
        h(NButton, { size: 'small', quaternary: true, onClick: () => openEdit(row) }, { default: () => '编辑' }),
        h(NButton, { size: 'small', quaternary: true, onClick: () => test(row) }, { default: () => '测试' }),
        h(NButton, { size: 'small', type: 'error', secondary: true, onClick: () => (remove.target.value = row) }, { default: () => '删除' }),
      ]),
  },
]

const presetNames = computed(() => Object.keys(WEBHOOK_PRESETS))
</script>

<template>
  <div class="view">
    <NAlert type="info" :show-icon="false">
      空间级外部推送（Webhook）：订阅事件的渠道在事件发生时收到推送；与模型配置同属服务端集成，仅 admin 可管理。
    </NAlert>

    <SectionHeader title="已配置渠道" desc="事件发生时向订阅渠道推送 HTTP 请求。">
      <NButton type="primary" @click="openCreate">
        <template #icon>
          <Plus :size="15" :stroke-width="1.8" />
        </template>
        添加渠道
      </NButton>
    </SectionHeader>

    <NCard size="small" class="table-card">
      <NDataTable
        :columns="columns"
        :data="channels"
        :loading="loading"
        flex-height
        style="flex: 1; min-height: 0"
        :scroll-x="800"
        :bordered="false"
        :row-key="(row: WebhookChannel) => row.id"
      />
    </NCard>

    <NModal
      v-model:show="showForm"
      preset="card"
      :title="editingId === null ? '添加渠道' : '编辑渠道'"
      :style="{ width: '560px' }"
      :bordered="true"
    >
      <NForm label-placement="top" size="medium">
        <div class="form-row">
          <NFormItem label="渠道名称">
            <NInput v-model:value="form.name" placeholder="如 我的 Bark" />
          </NFormItem>
          <NFormItem label="请求方式" class="method-item">
            <NSelect v-model:value="form.method" :options="methodOptions" />
          </NFormItem>
        </div>
        <NFormItem label="服务地址" required>
          <NInput v-model:value="form.url" placeholder="https://…" />
        </NFormItem>
        <NFormItem label="请求头（可选，每行一条 Key: Value）">
          <NInput v-model:value="form.headersText" type="textarea" :rows="2" placeholder="Authorization: Bearer xxx" />
        </NFormItem>
        <NFormItem label="请求体模板（可选）">
          <NInput
            v-model:value="form.body_template"
            type="textarea"
            :rows="3"
            placeholder="留空=默认 JSON；占位符 \{\{title\}\} \{\{content\}\} \{\{time\}\} \{\{type\}\}"
          />
        </NFormItem>
        <NFormItem label="订阅事件">
          <div class="event-picker">
            <NButton
              v-for="[key, label] in WEBHOOK_EVENTS"
              :key="key"
              size="small"
              :type="form.events.includes(key) ? 'primary' : 'default'"
              :secondary="form.events.includes(key)"
              @click="toggleEvent(key)"
            >
              {{ label }}
            </NButton>
          </div>
        </NFormItem>
        <div class="preset-row">
          <span class="hint-inline">预设：</span>
          <NButton v-for="name in presetNames" :key="name" size="small" quaternary @click="applyPreset(name)">
            {{ name }}
          </NButton>
        </div>
      </NForm>
      <template #footer>
        <div class="modal-actions">
          <NButton @click="showForm = false">取消</NButton>
          <NButton type="primary" :loading="saving" @click="save">保存渠道</NButton>
        </div>
      </template>
    </NModal>

    <ConfirmDialog
      v-if="remove.target.value"
      title="删除通知渠道"
      :message="`删除渠道「${remove.target.value.name || remove.target.value.url}」？删除后该渠道将不再收到推送。`"
      confirm-label="删除"
      :busy="remove.busy.value"
      @close="remove.target.value = null"
      @confirm="remove.confirm(runRemove)"
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
  grid-template-columns: 1fr var(--control-sm);
  gap: var(--space-4);
}

.method-item {
  margin-bottom: 0;
}

.event-picker {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
}

.preset-row {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.hint-inline {
  font-size: var(--text-sm);
  color: var(--color-ink-tertiary);
}

.table-card :deep(.ops-cell) {
  display: inline-flex;
  gap: var(--space-2);
}

.table-card :deep(.name-cell) {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
}
</style>
