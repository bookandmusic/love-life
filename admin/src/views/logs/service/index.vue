<script setup lang="ts">
/**
 * 日志 · 服务日志：服务端进程内环形缓冲的运行日志（slog）。
 * 文本流形态：等宽字体、级别着色、单行一条（运行日志不是表格数据）。
 * 契约：2s 轮询 + seq 增量拉取 + 跟随开关（见 api/logs）。
 */
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { NButton, NInput, NSelect } from 'naive-ui'
import { Check, RefreshCw, Trash2 } from 'lucide-vue-next'
import { logsApi } from '@/api'
import type { LogEntry } from '@/api/logs'
import { errMsg } from '@/lib/http'
import { toast } from '@/lib/toast'

// 服务端日志缓冲是有限的（环形，满则覆盖最旧），故这里是「最近的 N 条」而非完整历史。
const entries = ref<LogEntry[]>([])
const level = ref<string>('全部')
const keyword = ref('')
const following = ref(true)
const loading = ref(false)

// 级别取 slog 的四个语义级别里**实际会出现**的：服务端 handler 从 INFO 起、且全仓无
// slog.Debug，故不列 DEBUG（列了是一个永远筛不出东西的选项）。
const LEVELS = ['全部', 'INFO', 'WARN', 'ERROR'] as const

const levelOptions = LEVELS.map((l) => ({ label: l === '全部' ? '全部级别' : l, value: l }))

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
    // 失败即停表：不再每 2s 打一次失败请求，由用户重新跟随
    stopPolling()
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
  // 首载失败时 load 已把 following 置 false 并停表，这里不能无条件重启轮询
  if (following.value) startPolling()
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
  })
)

/** attrs 摊平成单行（值的脱敏已在服务端完成） */
function attrsLine(e: LogEntry): string {
  const list = Object.entries(e.attrs ?? {})
  if (list.length === 0) return ''
  return list.map(([k, v]) => `${k}=${v}`).join('  ')
}

/** 按**当前筛选结果**计数：与文本流同口径，否则标题数字和行数对不上 */
const errorCount = computed(() => filtered.value.filter((e) => e.level === 'ERROR').length)
</script>

<template>
  <div class="view">
    <div class="toolbar">
      <span class="head-label">服务日志</span>
      <span class="head-count">
        {{ filtered.length === entries.length ? `最近 ${entries.length} 条` : `命中 ${filtered.length} / ${entries.length} 条` }}{{ errorCount ? `，其中 ${errorCount} 条错误` : '' }}
      </span>
      <div class="toolbar-spacer" />
      <NSelect v-model:value="level" :options="levelOptions" class="filter-select" />
      <NInput v-model:value="keyword" placeholder="搜索消息或字段…" class="search-input" clearable />
      <NButton size="small" :type="following ? 'primary' : 'default'" :secondary="following" @click="toggleFollow">
        <template #icon>
          <component :is="following ? Check : RefreshCw" :size="14" :stroke-width="1.8" />
        </template>
        {{ following ? '实时跟随中' : '已暂停' }}
      </NButton>
      <NButton size="small" quaternary @click="clearView">
        <template #icon>
          <Trash2 :size="14" :stroke-width="1.8" />
        </template>
        清空视图
      </NButton>
    </div>

    <div class="stream-card">
      <p v-if="filtered.length === 0" class="stream-empty">
        {{ entries.length === 0 ? '暂无日志' : '没有符合筛选条件的日志' }}
      </p>
      <div v-else class="log-stream">
        <div v-for="e in filtered" :key="e.seq" class="log-line">
          <span class="log-time">{{ e.time }}</span>
          <span class="log-level" :class="'level-' + e.level.toLowerCase()">{{ e.level }}</span>
          <span class="log-msg">
            {{ e.msg }}<span v-if="attrsLine(e)" class="log-attrs">  {{ attrsLine(e) }}</span>
          </span>
        </div>
      </div>
    </div>

    <p class="hint">
      日志来自服务端进程内缓冲（重启后清空，完整历史见服务终端或 <code>make logs</code>）；密钥类字段已打码。
    </p>
  </div>
</template>

<style scoped>
.view {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  height: 100%;
}

.toolbar {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.head-label {
  font-size: var(--text-md);
  font-weight: 600;
  color: var(--color-ink);
}

.head-count {
  font-size: var(--text-sm);
  color: var(--color-ink-tertiary);
}

.toolbar-spacer {
  flex: 1;
}

.filter-select {
  width: var(--control-sm);
}

.search-input {
  width: var(--control-md);
}

/* 文本流卡片：占满剩余高度，内部滚动（页面锁定） */
.stream-card {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  background: var(--color-subtle);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  padding: var(--space-3) var(--space-4);
}

.stream-empty {
  margin: var(--space-6) 0;
  text-align: center;
  font-size: var(--text-base);
  color: var(--color-ink-tertiary);
}

.log-stream {
  font-family: var(--font-mono);
  font-size: var(--text-sm);
  line-height: var(--leading-relaxed);
}

.log-line {
  display: flex;
  gap: var(--space-3);
  padding: var(--space-1) 0;
  border-bottom: 1px dashed var(--color-border);
}

.log-time {
  flex-shrink: 0;
  color: var(--color-ink-tertiary);
}

.log-level {
  flex-shrink: 0;
  width: var(--space-8);
  font-weight: 600;
}

.level-info {
  color: var(--color-info);
}

.level-warn {
  color: var(--color-warning);
}

.level-error {
  color: var(--color-error);
}

.level-debug {
  color: var(--color-ink-tertiary);
}

.log-msg {
  color: var(--color-ink);
  word-break: break-all;
  white-space: pre-wrap;
}

.log-attrs {
  color: var(--color-ink-tertiary);
}

.hint {
  margin: 0;
  font-size: var(--text-sm);
  color: var(--color-ink-tertiary);
}
</style>
