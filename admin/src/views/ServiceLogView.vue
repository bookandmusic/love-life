<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { logsApi } from '../api'
import type { LogEntry } from '../api/logs'
import AppCard from '../components/AppCard.vue'
import AppBadge from '../components/AppBadge.vue'
import AppButton from '../components/AppButton.vue'
import AppIcon from '../components/AppIcon.vue'
import { errMsg } from '../lib/http'
import { toast } from '../lib/toast'

// 服务端日志缓冲是有限的（环形，满则覆盖最旧），故这里是「最近的 N 条」而非完整历史。
const entries = ref<LogEntry[]>([])
const level = ref('全部')
const keyword = ref('')
const following = ref(true)
const loading = ref(false)

// 级别取 slog 的四个语义级别里**实际会出现**的：服务端 handler 从 INFO 起、且全仓无
// slog.Debug，故不列 DEBUG（列了是一个永远筛不出东西的选项）。
const LEVELS = ['全部', 'INFO', 'WARN', 'ERROR'] as const

const LEVEL_VARIANT: Record<string, 'neutral' | 'info' | 'warning' | 'error'> = {
  DEBUG: 'neutral',
  INFO: 'info',
  WARN: 'warning',
  ERROR: 'error',
}

/** 轮询间隔：日志不是逐帧数据，2s 足够「实时」，又不至于把后台刷成请求风暴。 */
const POLL_MS = 2000
let timer: number | undefined
/** 上次拿到的最大 seq，用作增量游标（不带就每次全量拉，白耗带宽） */
let cursor = 0

async function load(initial = false) {
  if (loading.value) return
  loading.value = true
  try {
    const page = await logsApi.listLogs(initial ? 0 : cursor)
    if (initial) {
      entries.value = page.items
    } else if (page.items.length > 0) {
      // 缓冲满时服务端会丢弃最旧的：超出保留量的本地条目也一并裁掉，保持「最近 N 条」语义
      entries.value = [...entries.value, ...page.items].slice(-2000)
    }
    cursor = page.latestSeq
  } catch (e) {
    // 轮询失败不弹 toast（否则会每 2s 弹一次）；仅首次加载时提示
    if (initial) toast.error(errMsg(e, '服务日志加载失败'))
    following.value = false
  } finally {
    loading.value = false
  }
}

function startPolling() {
  if (timer !== undefined) return
  timer = window.setInterval(() => void load(), POLL_MS)
}

function stopPolling() {
  if (timer !== undefined) {
    window.clearInterval(timer)
    timer = undefined
  }
}

function toggleFollow() {
  following.value = !following.value
  if (following.value) {
    void load()
    startPolling()
  } else {
    stopPolling()
  }
}

function clearView() {
  // 只清空当前视图，不动服务端缓冲（缓冲是运行期共享状态，页面无权清）
  entries.value = []
}

onMounted(async () => {
  await load(true)
  startPolling()
})
onUnmounted(stopPolling)

const filtered = computed(() =>
  entries.value.filter((e) => {
    if (level.value !== '全部' && e.level !== level.value) return false
    if (!keyword.value.trim()) return true
    const k = keyword.value.trim().toLowerCase()
    return (
      e.msg.toLowerCase().includes(k) ||
      Object.entries(e.attrs ?? {}).some(([key, v]) => key.toLowerCase().includes(k) || v.toLowerCase().includes(k))
    )
  }),
)

/** attrs 摊平成单行，供表格展示与搜索（值的脱敏已在服务端完成） */
function attrsLine(e: LogEntry): string {
  const entries = Object.entries(e.attrs ?? {})
  if (entries.length === 0) return ''
  return entries.map(([k, v]) => `${k}=${v}`).join('  ')
}

/** 按**当前筛选结果**计数：与下方表格同口径，否则标题的数字会和表里的行数打架 */
const errorCount = computed(() => filtered.value.filter((e) => e.level === 'ERROR').length)
</script>

<template>
  <div class="page">
    <div class="page-section-header">
      <h2 class="page-section-title">
        服务日志
        <!-- 筛选生效时同时给出「命中/总数」：只报总数会与下方表格的条数对不上 -->
        <span class="text-tertiary text-sm">
          （{{ filtered.length === entries.length ? `最近 ${entries.length} 条` : `命中 ${filtered.length} / ${entries.length} 条` }}{{ errorCount ? `，其中 ${errorCount} 条错误` : '' }}）
        </span>
      </h2>
      <div class="flex gap-3 items-center">
        <select v-model="level" class="form-select" style="width: auto">
          <option v-for="l in LEVELS" :key="l" :value="l">{{ l === '全部' ? '全部级别' : l }}</option>
        </select>
        <div class="search-input" style="width: 200px">
          <AppIcon name="search" :size="16" />
          <input v-model="keyword" class="form-input" type="text" placeholder="搜索消息或字段…" />
        </div>
        <AppButton
          :variant="following ? 'primary' : 'secondary'"
          size="sm"
          :icon="following ? 'check' : 'refresh-cw'"
          @click="toggleFollow"
        >
          {{ following ? '实时跟随中' : '已暂停' }}
        </AppButton>
        <AppButton variant="secondary" size="sm" icon="trash" @click="clearView">清空视图</AppButton>
      </div>
    </div>

    <AppCard class="card-fill">
      <div v-if="filtered.length === 0" class="empty-state">
        <AppIcon name="file-text" :size="48" />
        <div class="empty-state-title">
          {{ entries.length === 0 ? '暂无日志' : '没有符合筛选条件的日志' }}
        </div>
      </div>
      <!-- 包装层必须是 <table> 外的一层：<table> 自身不能作滚动容器（表头 sticky 无从参照） -->
      <div v-else class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th style="width: 160px">时间</th>
              <th style="width: 80px">级别</th>
              <th style="width: 240px">消息</th>
              <th>字段</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="e in filtered" :key="e.seq">
              <td class="text-secondary">{{ e.time }}</td>
              <td>
                <AppBadge :variant="LEVEL_VARIANT[e.level] ?? 'neutral'">{{ e.level }}</AppBadge>
              </td>
              <td>{{ e.msg }}</td>
              <td class="text-secondary log-attrs">{{ attrsLine(e) || '—' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </AppCard>

    <p class="form-hint">
      日志来自服务端进程内缓冲（重启后清空，完整历史见服务终端或 <code>make logs</code>）；密钥类字段已打码。
    </p>
  </div>
</template>

<style scoped>
.log-attrs {
  font-family: var(--font-mono);
  font-size: 12px;
  word-break: break-all;
}
</style>
