<script setup lang="ts">
/**
 * 日志 · 操作日志：回收站流水（admin 恢复 + 系统超期永久删除，均带操作者与摘要）。
 * 轻量文本行形态：事件流水不是表格数据。
 */
import { onMounted, ref } from 'vue'
import { recycleApi } from '@/api'
import type { RestoreLogEntry } from '@/api/recycle'
import { errMsg } from '@/lib/http'
import { toast } from '@/lib/toast'

const restoreLog = ref<RestoreLogEntry[]>([])
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    restoreLog.value = (await recycleApi.listRestoreLog()).items
  } catch (e) {
    toast.error(errMsg(e, '操作日志加载失败'))
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="view">
    <div class="stream-card">
      <p v-if="restoreLog.length === 0" class="stream-empty">
        {{ loading ? '加载中…' : '暂无操作记录' }}
      </p>
      <div v-else class="log-stream">
        <div v-for="(l, i) in restoreLog" :key="i" class="log-line">
          <span class="log-time">{{ l.time }}</span>
          <span class="log-operator">{{ l.operator }}</span>
          <span class="log-msg">{{ l.summary }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.view {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  height: 100%;
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
  font-size: var(--text-base);
  line-height: var(--leading-relaxed);
}

.log-line {
  display: flex;
  gap: var(--space-3);
  padding: var(--space-2) 0;
  border-bottom: 1px dashed var(--color-border);
}

.log-time {
  flex-shrink: 0;
  font-family: var(--font-mono);
  font-size: var(--text-sm);
  color: var(--color-ink-tertiary);
}

.log-operator {
  flex-shrink: 0;
  width: 48px;
  font-weight: 500;
  color: var(--color-ink);
}

.log-msg {
  color: var(--color-ink-secondary);
  word-break: break-all;
}
</style>
