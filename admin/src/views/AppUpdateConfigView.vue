<script setup lang="ts">
// 外部集成 · 应用更新：服务端替 APP 从 GitHub 取回更新信息与安装包。
// 代理仅作用于 GitHub 出站——模型 API / 地图瓦片 / 通知 Webhook 各走自己的通道。
import { onMounted, ref } from 'vue'
import {
  checkAppUpdate,
  getAppUpdateConfig,
  removeAppUpdate,
  saveAppUpdateConfig,
  startAppUpdateDownload,
  type AppUpdateConfig,
} from '../api/appupdate'
import { errMsg } from '../lib/http'
import { toast } from '../lib/toast'
import { useConfirmAction } from '../lib/confirm'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import AppCard from '../components/AppCard.vue'
import AppButton from '../components/AppButton.vue'
import AppAlert from '../components/AppAlert.vue'

const config = ref<AppUpdateConfig | null>(null)
const loading = ref(true)

const proxyUrl = ref('')
const proxyUser = ref('')
const proxyPassword = ref('')
const saving = ref(false)
const checking = ref(false)
const starting = ref(false)
// 检查结果：GitHub 上的最新版本与本地是否就绪
const latest = ref<{ version: string; size: number; ready: boolean } | null>(null)

// 清理动作无目标对象，用 true 作哨兵（useConfirmAction 的骨架对 falsy 直接跳过）
const cleanup = useConfirmAction<boolean>('清理失败')

async function load() {
  loading.value = true
  try {
    const cfg = await getAppUpdateConfig()
    config.value = cfg
    proxyUrl.value = cfg.proxy_url
    proxyUser.value = cfg.proxy_user
  } catch (e) {
    toast.error(errMsg(e, '加载失败'))
  } finally {
    loading.value = false
  }
}

async function save() {
  saving.value = true
  try {
    await saveAppUpdateConfig({
      proxy_url: proxyUrl.value.trim(),
      proxy_user: proxyUser.value.trim(),
      proxy_password: proxyPassword.value || undefined,
    })
    proxyPassword.value = ''
    toast.success('已保存')
    await load()
  } catch (e) {
    toast.error(errMsg(e, '保存失败'))
  } finally {
    saving.value = false
  }
}

async function clearPassword() {
  try {
    await saveAppUpdateConfig({
      proxy_url: proxyUrl.value.trim(),
      proxy_user: proxyUser.value.trim(),
      clear_password: true,
    })
    toast.success('代理密码已清除')
    await load()
  } catch (e) {
    toast.error(errMsg(e, '清除失败'))
  }
}

async function doCheck() {
  checking.value = true
  try {
    const res = await checkAppUpdate()
    if (res.ok && res.version_name) {
      latest.value = { version: res.version_name, size: res.asset_size ?? 0, ready: !!res.ready }
      toast.success(`最新版本：${res.version_name}`)
      await load()
    } else {
      toast.error(res.reason || '检查失败')
    }
  } catch (e) {
    toast.error(errMsg(e, '检查失败'))
  } finally {
    checking.value = false
  }
}

async function startDownload() {
  starting.value = true
  try {
    const res = await startAppUpdateDownload()
    if (res.ok) {
      toast.success('已开始后台下载，可稍后刷新查看')
      await load()
    } else {
      toast.error(res.reason || '无法开始下载')
    }
  } catch (e) {
    toast.error(errMsg(e, '无法开始下载'))
  } finally {
    starting.value = false
  }
}

async function runCleanup() {
  await removeAppUpdate()
  toast.success('已删除缓存的安装包')
  latest.value = null
  await load()
}

function humanSize(n: number): string {
  if (!n) return '—'
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(0)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

function fmtTime(iso: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString()
}

onMounted(load)
</script>

<template>
  <div class="page">
    <AppAlert variant="info">
      APP 只连本服务：检查更新与下载安装包都走 APP 当前使用的地址（绿联通道 / 备用地址 / 内置），
      由服务端从 GitHub 取回并缓存。原「更新代理」二级页随之下线。
    </AppAlert>

    <AppCard title="GitHub 代理">
      <p class="hint">
        <strong>仅作用于 GitHub 出站</strong>（版本信息与安装包下载）；模型 API、地图瓦片、通知 Webhook 不走它。
        留空则直连。
      </p>
      <div class="form">
        <label class="form-label">代理地址</label>
        <input v-model="proxyUrl" class="form-input" placeholder="http://192.168.1.10:7890" spellcheck="false" />

        <label class="form-label">用户名（可选）</label>
        <input v-model="proxyUser" class="form-input" autocomplete="off" spellcheck="false" />

        <label class="form-label">
          密码（可选）
          <span v-if="config?.has_password" class="badge-note">已设置，留空表示不修改</span>
        </label>
        <input
          v-model="proxyPassword"
          class="form-input"
          type="password"
          autocomplete="new-password"
          :placeholder="config?.has_password ? '••••••••' : '代理无需认证时留空'"
        />
      </div>
      <div class="actions">
        <AppButton :disabled="saving" @click="save">{{ saving ? '保存中…' : '保存' }}</AppButton>
        <AppButton v-if="config?.has_password" variant="secondary" @click="clearPassword">清除密码</AppButton>
      </div>
    </AppCard>

    <AppCard title="安装包">
      <div v-if="loading" class="empty">加载中…</div>
      <template v-else>
        <div class="status">
          <div class="status-row">
            <span class="status-label">已就绪版本</span>
            <span class="status-value">{{ config?.cache.version || '无' }}</span>
          </div>
          <div class="status-row">
            <span class="status-label">安装包大小</span>
            <span class="status-value">{{ humanSize(config?.cache.size_bytes || 0) }}</span>
          </div>
          <div class="status-row">
            <span class="status-label">就绪时间</span>
            <span class="status-value">{{ fmtTime(config?.cache.fetch_time || '') }}</span>
          </div>
          <div v-if="latest" class="status-row">
            <span class="status-label">GitHub 最新版本</span>
            <span class="status-value">
              {{ latest.version }}（{{ humanSize(latest.size) }}）{{ latest.ready ? ' · 已就绪' : '' }}
            </span>
          </div>
        </div>

        <AppAlert v-if="config?.downloading" variant="info">
          正在后台下载 {{ config.download_of || '安装包' }}…慢速链路下可能需要十几分钟，可稍后刷新本页查看结果。
        </AppAlert>
        <AppAlert v-if="config?.download_err" variant="error">
          上次下载失败：{{ config.download_err }}
        </AppAlert>

        <p class="hint">
          服务端<strong>只下发已下载到本地的安装包</strong>：APP 检查更新时若这里没有就绪版本，会被视为「无需更新」。
          因此换版后请在这里点「下载安装包」。
        </p>

        <div class="actions">
          <AppButton :disabled="checking" @click="doCheck">{{ checking ? '检查中…' : '检查最新版本' }}</AppButton>
          <AppButton :disabled="starting || config?.downloading" @click="startDownload">
            {{ config?.downloading ? '下载中…' : '下载安装包' }}
          </AppButton>
          <AppButton variant="secondary" :disabled="!config?.cache.version" @click="cleanup.target.value = true">
            删除安装包
          </AppButton>
        </div>
      </template>
    </AppCard>

    <ConfirmDialog
      v-if="cleanup.target.value"
      title="删除安装包"
      message="删除已缓存的安装包与版本信息？删除后 APP 将看不到可更新版本，直到重新下载。"
      confirm-label="删除"
      :busy="cleanup.busy.value"
      @close="cleanup.target.value = null"
      @confirm="cleanup.confirm(runCleanup)"
    />
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
.form {
  display: flex;
  flex-direction: column;
  gap: 6px;
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
.empty {
  padding: 16px 0;
  font-size: 13px;
  color: var(--muted-ink);
}
</style>