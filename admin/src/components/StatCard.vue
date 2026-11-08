<script setup lang="ts">
/**
 * 统计卡：图标 + 标签 + 大数值（+ 可选趋势行）。
 * 样式值全部来自 CSS 变量令牌，不写裸色值 / 裸字号。
 */
import type { Component } from 'vue'

defineProps<{
  icon: Component
  label: string
  value: string | number
  tone?: 'neutral' | 'success' | 'warning' | 'error'
}>()
</script>

<template>
  <div class="stat-card">
    <div class="stat-icon" :class="tone ? `tone-${tone}` : ''">
      <component :is="icon" :size="16" :stroke-width="1.8" />
    </div>
    <div class="stat-label">{{ label }}</div>
    <div class="stat-value">{{ value }}</div>
    <div v-if="$slots.default" class="stat-extra">
      <slot />
    </div>
  </div>
</template>

<style scoped>
.stat-card {
  background: var(--color-canvas);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  padding: var(--space-4) var(--space-5);
  box-shadow: var(--shadow-sm);
}

.stat-icon {
  width: var(--space-6);
  height: var(--space-6);
  border-radius: var(--radius-md);
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--color-subtle);
  color: var(--color-ink-secondary);
  margin-bottom: var(--space-3);
}

.stat-icon.tone-success {
  color: var(--color-success);
  background: var(--color-success-bg);
}

.stat-icon.tone-warning {
  color: var(--color-warning);
  background: var(--color-warning-bg);
}

.stat-icon.tone-error {
  color: var(--color-error);
  background: var(--color-error-bg);
}

.stat-label {
  font-size: var(--text-sm);
  font-weight: 500;
  color: var(--color-ink-tertiary);
}

.stat-value {
  margin-top: var(--space-1);
  font-size: var(--text-2xl);
  font-weight: 600;
  color: var(--color-ink);
  line-height: var(--leading-tight);
}

.stat-extra {
  margin-top: var(--space-1);
  font-size: var(--text-sm);
  color: var(--color-ink-tertiary);
}
</style>
