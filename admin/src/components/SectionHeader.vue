<script setup lang="ts">
/**
 * 页面区块头：标题 + 右侧动作（页面唯一的一级标题槽位）。
 * 页面统一用它，避免各页重复写标题样式。
 */
defineProps<{ title: string; desc?: string }>()
</script>

<template>
  <header class="section-header">
    <div>
      <h2 class="section-title">{{ title }}</h2>
      <p v-if="desc" class="section-desc">{{ desc }}</p>
    </div>
    <div class="section-actions">
      <slot />
    </div>
  </header>
</template>

<style scoped>
.section-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.section-title {
  font-size: var(--text-md);
  font-weight: 600;
  color: var(--color-ink);
}

.section-desc {
  margin-top: var(--space-1);
  font-size: var(--text-sm);
  color: var(--color-ink-tertiary);
}

.section-actions {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}
</style>
