<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { webhooksApi } from '../api'
import type { WebhookChannel } from '../api/webhooks'
import AppCard from '../components/AppCard.vue'
import AppButton from '../components/AppButton.vue'
import AppIcon from '../components/AppIcon.vue'
import AppBadge from '../components/AppBadge.vue'
import AppAlert from '../components/AppAlert.vue'
import AppModal from '../components/AppModal.vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import { errMsg } from '../lib/http'
import { toast } from '../lib/toast'
import { useConfirmAction } from '../lib/confirm'

const { WEBHOOK_EVENTS, WEBHOOK_PRESETS } = webhooksApi

const channels = ref<WebhookChannel[]>([])

// editingId 为 null = 新增；否则编辑该渠道
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
  try {
    const res = await webhooksApi.listWebhooks()
    channels.value = res.items
  } catch (e) {
    toast.error(errMsg(e, '渠道列表加载失败'))
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
  const payload = {
    name: form.value.name,
    method: form.value.method,
    url: form.value.url,
    headers: parseHeaders(form.value.headersText),
    body_template: form.value.body_template,
    events: form.value.events,
  }
  try {
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
</script>

<template>
  <div class="tab-panel">
    <AppAlert variant="info" icon="bell">
      空间级外部推送（Webhook）：订阅事件的渠道在事件发生时收到推送；与模型配置同属服务端集成，仅 admin 可管理。
    </AppAlert>

    <div class="page-section-header">
      <h2 class="page-section-title">已配置渠道</h2>
      <AppButton variant="primary" icon="plus" @click="openCreate">添加渠道</AppButton>
    </div>

    <AppCard class="card-fill">
      <div v-if="channels.length === 0" class="empty-state">
        <AppIcon name="bell-off" :size="48" />
        <div class="empty-state-title">还没有配置渠道</div>
      </div>
      <div v-else class="table-wrapper">
        <table>
          <thead>
            <tr><th>ID</th><th>名称</th><th>地址</th><th>订阅</th><th></th></tr>
          </thead>
          <tbody>
            <tr v-for="c in channels" :key="c.id">
              <td>{{ c.id }}</td>
              <td>
                {{ c.name || '—' }}
                <AppBadge v-if="Object.keys(c.headers ?? {}).length > 0" variant="neutral">含请求头</AppBadge>
              </td>
              <td class="text-secondary" style="word-break: break-all; max-width: 280px">
                {{ c.method }} {{ c.url }}
              </td>
              <td>{{ c.events.map(eventLabel).join('、') || '无' }}</td>
              <td class="text-right">
                <div class="flex gap-2" style="justify-content: flex-end">
                  <AppButton variant="secondary" size="sm" icon="edit" @click="openEdit(c)">编辑</AppButton>
                  <AppButton variant="secondary" size="sm" icon="send" @click="test(c)">测试</AppButton>
                  <AppButton variant="danger" size="sm" icon="trash" @click="remove.target.value = c">删除</AppButton>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </AppCard>

    <AppModal
      v-if="showForm"
      :title="editingId === null ? '添加渠道' : '编辑渠道'"
      :width="560"
      @close="showForm = false"
    >
      <form class="flex" style="flex-direction: column; gap: 14px" @submit.prevent="save">
        <div class="flex gap-3" style="flex-wrap: wrap">
          <div class="form-group" style="flex: 1; min-width: 140px; margin: 0">
            <label class="form-label">渠道名称</label>
            <input v-model="form.name" class="form-input" placeholder="如 我的 Bark" />
          </div>
          <div class="form-group" style="width: 110px; margin: 0">
            <label class="form-label">请求方式</label>
            <select v-model="form.method" class="form-select">
              <option>GET</option>
              <option>POST</option>
            </select>
          </div>
        </div>
        <div class="form-group" style="margin: 0">
          <label class="form-label">服务地址</label>
          <input v-model="form.url" class="form-input" placeholder="https://…" required />
        </div>
        <div class="form-group" style="margin: 0">
          <label class="form-label">请求头（可选，每行一条 Key: Value）</label>
          <textarea
            v-model="form.headersText"
            class="form-textarea"
            rows="2"
            placeholder="Authorization: Bearer xxx"
          />
        </div>
        <div class="form-group" style="margin: 0">
          <label class="form-label">请求体模板（可选）</label>
          <textarea
            v-model="form.body_template"
            class="form-textarea"
            rows="3"
            placeholder="留空=默认 JSON；占位符 \{\{title\}\} \{\{content\}\} \{\{time\}\} \{\{type\}\}"
          />
        </div>
        <div class="form-group" style="margin: 0">
          <label class="form-label">订阅事件</label>
          <div class="flex gap-2" style="flex-wrap: wrap">
            <button
              v-for="[key, label] in WEBHOOK_EVENTS"
              :key="key"
              type="button"
              class="btn btn-sm"
              :class="form.events.includes(key) ? 'btn-primary' : 'btn-secondary'"
              @click="toggleEvent(key)"
            >
              {{ label }}
            </button>
          </div>
        </div>
        <div class="flex gap-2" style="align-items: center">
          <span class="text-tertiary text-sm">预设：</span>
          <button
            v-for="(url, name) in WEBHOOK_PRESETS"
            :key="name"
            type="button"
            class="btn btn-sm btn-secondary"
            @click="applyPreset(name)"
          >
            {{ name }}
          </button>
        </div>
        <div class="flex gap-2" style="justify-content: flex-end">
          <AppButton variant="secondary" icon="x" type="button" @click="showForm = false">取消</AppButton>
          <AppButton variant="primary" icon="save" type="submit">保存渠道</AppButton>
        </div>
      </form>
    </AppModal>

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
