<script setup lang="ts">
// 远程访问 · 绿联通道：服务端持凭据完成握手，APP 登录后取通道信息。
// 这是内置能力（不是「通道列表中的一项」）：只有启用开关，不可删除。
import { onMounted, ref } from 'vue'
import { getUglinkConfig, saveUglinkConfig, testUglink, type UglinkConfig } from '../api/uglink'
import { errMsg } from '../lib/http'
import { toast } from '../lib/toast'
import AppCard from '../components/AppCard.vue'
import AppButton from '../components/AppButton.vue'
import AppToggle from '../components/AppToggle.vue'
import AppAlert from '../components/AppAlert.vue'
import AppBadge from '../components/AppBadge.vue'

const config = ref<UglinkConfig>({ configured: false })
const loading = ref(true)

// 表单（密码只写不读：留空表示沿用已存密码）
const uglinkId = ref('')
const port = ref(8080)
const username = ref('')
const password = ref('')
const enabled = ref(false)

const saving = ref(false)
const testing = ref(false)
const testResult = ref<{ ok: boolean; reason?: string; base_url?: string } | null>(null)

async function load() {
  loading.value = true
  try {
    config.value = await getUglinkConfig()
    if (config.value.configured) {
      uglinkId.value = config.value.uglink_id ?? ''
      port.value = config.value.port ?? 8080
      username.value = config.value.username ?? ''
      enabled.value = config.value.enabled ?? false
    }
  } catch (e) {
    toast.error(errMsg(e, '加载失败'))
  } finally {
    loading.value = false
  }
}

async function save() {
  saving.value = true
  try {
    await saveUglinkConfig({
      uglink_id: uglinkId.value.trim(),
      port: port.value,
      username: username.value.trim(),
      password: password.value || undefined,
      enabled: enabled.value,
    })
    password.value = ''
    toast.success('已保存')
    await load()
  } catch (e) {
    toast.error(errMsg(e, '保存失败'))
  } finally {
    saving.value = false
  }
}

async function test() {
  testing.value = true
  testResult.value = null
  try {
    testResult.value = await testUglink()
    if (testResult.value.ok) toast.success('连接成功')
  } catch (e) {
    toast.error(errMsg(e, '测试失败'))
  } finally {
    testing.value = false
  }
}

function fmtTime(iso?: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString()
}

onMounted(load)
</script>

<template>
  <div class="page">
    <AppCard title="绿联通道">
      <p class="hint">
        服务端用这里的账号登录绿联，为 APP 换发中转凭据。APP 不接触密码。
        未启用时 APP 走「备用地址」或内置地址。
      </p>

      <AppAlert v-if="!loading && !config.configured" variant="info">
        尚未配置。填写下方信息并启用后，APP 在外网可经绿联中转访问本服务（国内通常快于 Cloudflare）。
      </AppAlert>

      <div class="form">
        <label class="form-label">UGREENlink ID</label>
        <input v-model="uglinkId" class="form-input" placeholder="如 mynas123" spellcheck="false" />

        <label class="form-label">服务端口</label>
        <input v-model.number="port" class="form-input" type="number" placeholder="8080" />

        <label class="form-label">绿联账号</label>
        <input v-model="username" class="form-input" autocomplete="off" spellcheck="false" />

        <label class="form-label">
          密码
          <span v-if="config.has_password" class="badge-note">已设置，留空表示不修改</span>
        </label>
        <input
          v-model="password"
          class="form-input"
          type="password"
          autocomplete="new-password"
          :placeholder="config.has_password ? '••••••••' : '首次启用必填'"
        />

        <div class="row-toggle">
          <span class="form-label">启用绿联通道</span>
          <AppToggle v-model="enabled" />
        </div>
      </div>

      <div class="actions">
        <AppButton :disabled="saving" @click="save">{{ saving ? '保存中…' : '保存' }}</AppButton>
        <AppButton variant="secondary" :disabled="testing || !config.configured" @click="test">
          {{ testing ? '测试中…' : '测试连接' }}
        </AppButton>
      </div>

      <AppAlert v-if="testResult" :variant="testResult.ok ? 'success' : 'error'">
        <template v-if="testResult.ok">
          连接成功。中转地址：{{ testResult.base_url }}
        </template>
        <template v-else>
          连接失败：{{ testResult.reason }}
        </template>
      </AppAlert>
    </AppCard>

    <AppCard v-if="config.configured" title="当前状态">
      <div class="status">
        <div class="status-row">
          <span class="status-label">登录状态</span>
          <AppBadge :variant="config.logged_in ? 'success' : 'neutral'">
            {{ config.logged_in ? '已登录' : '未登录' }}
          </AppBadge>
        </div>
        <div v-if="config.expires_at" class="status-row">
          <span class="status-label">凭据有效期至</span>
          <span class="status-value">{{ fmtTime(config.expires_at) }}</span>
        </div>
        <div v-if="config.failure_count" class="status-row">
          <span class="status-label">连续失败次数</span>
          <span class="status-value">{{ config.failure_count }}</span>
        </div>
        <div v-if="config.backoff_until" class="status-row">
          <span class="status-label">退避至</span>
          <span class="status-value">{{ fmtTime(config.backoff_until) }}</span>
        </div>
      </div>
      <p class="hint small">
        失败次数达到 3 次会进入 1 小时退避，避免触发绿联风控。
      </p>
    </AppCard>
  </div>
</template>

<style scoped>
.page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.hint {
  margin: 0 0 12px;
  font-size: 13px;
  line-height: 1.6;
  color: var(--muted-ink);
}
.hint.small {
  margin: 12px 0 0;
  font-size: 12px;
}
.form {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-top: 12px;
}
.form-label {
  font-size: 13px;
  color: var(--muted-ink);
  margin-top: 8px;
}
.badge-note {
  margin-left: 6px;
  font-size: 12px;
  color: var(--muted-ink);
  opacity: 0.8;
}
.form-input {
  width: 100%;
  box-sizing: border-box;
  padding: 9px 12px;
  border: 1px solid var(--rule);
  border-radius: 10px;
  background: var(--bg);
  font-size: 14px;
  color: var(--ink);
}
.form-input:focus {
  outline: none;
  border-color: var(--primary);
}
.row-toggle {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: 12px;
}
.actions {
  display: flex;
  gap: 8px;
  margin-top: 16px;
}
.status {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.status-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 14px;
}
.status-label {
  color: var(--muted-ink);
}
.status-value {
  color: var(--ink);
}
</style>
