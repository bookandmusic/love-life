<script setup lang="ts">
/**
 * 应用更新：服务端替 APP 从 GitHub 取回版本信息与安装包并缓存，APP 只从本服务更新。
 * 是否走 HTTP 代理由「使用 HTTP 代理」开关决定（代理本体在「HTTP 代理」页配置）。
 */
import { onMounted, ref } from 'vue'
import { NAlert, NButton, NCard, NSwitch, NTag } from 'naive-ui'
import {
  checkAppUpdate,
  getAppUpdateConfig,
  removeAppUpdate,
  saveAppUpdateConfig,
  startAppUpdateDownload,
  type AppUpdateConfig
} from '@/api/appupdate'
import SectionHeader from '@/components/SectionHeader.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { errMsg } from '@/lib/http'
import { toast } from '@/lib/toast'
import { useConfirmAction } from '@/lib/confirm'

const config = ref<AppUpdateConfig | null>(null)
const loading = ref(true)
const checking = ref(false)
const starting = ref(false)
const savingProxy = ref(false)
// 检查结果：GitHub 上的最新版本与本地是否就绪
const latest = ref<{ version: string; size: number; ready: boolean } | null>(null)

// 清理动作无目标对象，用 true 作哨兵（useConfirmAction 的骨架对 falsy 直接跳过）
const cleanup = useConfirmAction<boolean>('清理失败')

async function load() {
  loading.value = true
  try {
    config.value = await getAppUpdateConfig()
  } catch (e) {
    toast.error(errMsg(e, '加载失败'))
  } finally {
    loading.value = false
  }
}

/** 切换「使用 HTTP 代理」；代理地址等在「HTTP 代理」页维护 */
async function toggleUseProxy(useProxy: boolean) {
  if (!config.value) {
    toast.error('配置未加载，请刷新后重试')
    return
  }
  savingProxy.value = true
  try {
    // 服务端 PUT 是整体替换（仅密码沿用）：只发开关会把 proxy_url / proxy_user 抹空，
    // 必须把已加载的代理字段一并回传
    await saveAppUpdateConfig({
      proxy_url: config.value.proxy_url,
      proxy_user: config.value.proxy_user,
      use_proxy: useProxy
    })
    toast.success(useProxy ? '应用更新将使用 HTTP 代理' : '应用更新改为直连 GitHub')
    await load()
  } catch (e) {
    toast.error(errMsg(e, '保存失败'))
    await load()
  } finally {
    savingProxy.value = false
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
  <div class="view">
    <SectionHeader title="应用更新" desc="服务端替 APP 从 GitHub 取回版本信息与安装包并缓存；APP 只连本服务检查与下载。" />

    <NCard size="small" class="block-card">
      <template #header>
        <div class="card-head">
          <span>HTTP 代理</span>
          <div class="head-right">
            <span class="head-hint">应用更新走「HTTP 代理」页配置的代理</span>
            <NTag v-if="!config?.proxy_url" size="small" :bordered="false">未配置代理</NTag>
            <NSwitch
              size="small"
              :value="config?.use_proxy ?? true"
              :loading="savingProxy"
              @update:value="toggleUseProxy"
            />
          </div>
        </div>
      </template>
      <p class="hint">开关只决定<strong>用不用</strong>；代理地址、账号与密码在「HTTP 代理」页配置。关闭后检查更新与下载安装包都直连 GitHub。</p>
    </NCard>

    <NCard title="安装包" size="small">
      <p v-if="loading" class="empty">加载中…</p>
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

        <NAlert v-if="config?.downloading" type="info" :show-icon="false" class="stack-alert">
          正在后台下载 {{ config.download_of || '安装包' }}…慢速链路下可能需要十几分钟，可稍后刷新本页查看结果。
        </NAlert>
        <NAlert v-if="config?.download_err" type="error" :show-icon="false" class="stack-alert">
          上次下载失败：{{ config.download_err }}
        </NAlert>

        <p class="hint">
          服务端<strong>只下发已下载到本地的安装包</strong>：APP 检查更新时若这里没有就绪版本，会被视为「无需更新」。因此换版后请在这里点「下载安装包」。
        </p>

        <div class="actions">
          <NButton :loading="checking" @click="doCheck">{{ checking ? '检查中…' : '检查最新版本' }}</NButton>
          <NButton type="primary" :disabled="starting || config?.downloading" @click="startDownload">
            {{ config?.downloading ? '下载中…' : '下载安装包' }}
          </NButton>
          <NButton quaternary type="error" :disabled="!config?.cache.version" @click="cleanup.target.value = true">
            删除安装包
          </NButton>
        </div>
      </template>
    </NCard>

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
.view {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.block-card {
  flex-shrink: 0;
}

.card-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.head-right {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.head-hint {
  font-size: var(--text-sm);
  color: var(--color-ink-tertiary);
}

.hint {
  margin: 0;
  font-size: var(--text-base);
  line-height: var(--leading-relaxed);
  color: var(--color-ink-secondary);
}

.actions {
  display: flex;
  gap: var(--space-2);
  margin-top: var(--space-4);
}

.status {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.status-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: var(--text-md);
}

.status-label {
  color: var(--color-ink-secondary);
}

.status-value {
  color: var(--color-ink);
}

.stack-alert {
  margin-top: var(--space-4);
}

.empty {
  padding: var(--space-4) 0;
  font-size: var(--text-base);
  color: var(--color-ink-tertiary);
}
</style>
