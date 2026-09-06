<script setup lang="ts">
// 统一错误态（可重试）：展示错误信息 + 重试按钮（各页共用）
import { RefreshCw } from 'lucide-vue-next'

defineProps<{
  error: string
  retry?: () => void
}>()
</script>

<template>
  <div class="load-error">
    <p class="err-text">{{ error }}</p>
    <button v-if="retry" class="retry" @click="retry"><RefreshCw :size="14" stroke-width="2" />重试</button>
  </div>
</template>

<style scoped>
.load-error {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 14px;
  padding: 48px 0;
}
.err-text {
  color: var(--danger);
  font-size: 14px;
  text-align: center;
}
.retry {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  border: none;
  background: var(--primary);
  color: #fff;
  border-radius: 999px;
  padding: 9px 22px;
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
}
.retry:active {
  transform: scale(0.96);
}
</style>