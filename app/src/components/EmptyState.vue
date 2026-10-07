<script setup lang="ts">
// 全站统一空态：图标 + 标题 +（可选）副文案 +（可选）操作按钮。
// 首页首启、各列表页、AI 未配置/空对话均复用本组件，保证视觉与口吻一致。
import type { Component } from 'vue'

defineProps<{
  icon?: Component
  title: string
  sub?: string
  action?: string
  // 小容器模式（Sheet、地图覆盖层等）：去掉 52vh 最小高度并收紧留白，由父容器负责定位
  compact?: boolean
}>()

defineEmits<{ (e: 'action'): void }>()
</script>

<template>
  <div class="empty-state" :class="{ compact }">
    <span v-if="icon" class="es-ic">
      <component :is="icon" :size="compact ? 26 : 34" stroke-width="1.2" />
    </span>
    <p class="es-title">{{ title }}</p>
    <p v-if="sub" class="es-sub">{{ sub }}</p>
    <button v-if="action" class="es-action" @click="$emit('action')">{{ action }}</button>
  </div>
</template>

<style scoped>
.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  text-align: center;
  min-height: 52vh;
 /* 横向不写 padding：外壳 --side-pad 已承担，叠加会在手机档挤窄 */
  padding: 48px 0;
  color: var(--muted-ink);
}
.empty-state.compact {
  min-height: 0;
  padding: 20px 0;
}
.es-ic {
  display: inline-flex;
  color: var(--primary);
  opacity: 0.55;
  margin-bottom: 12px;
}
.es-title {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
  color: var(--ink);
}
.es-sub {
  margin: 6px 0 0;
  font-size: 13px;
  line-height: 1.5;
  color: var(--muted-ink);
  max-width: 240px;
}
.es-action {
  margin-top: 18px;
  border: none;
  border-radius: 999px;
  background: var(--primary);
  color: var(--on-primary);
  font-size: 14px;
  font-weight: 600;
  padding: 9px 22px;
  cursor: pointer;
}
.es-action:active {
  opacity: 0.85;
}
</style>
