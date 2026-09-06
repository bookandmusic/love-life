<script setup lang="ts">
import AppIcon from './AppIcon.vue'

withDefaults(
  defineProps<{
    label: string
    value: string
    change: string
    trend?: 'up' | 'neutral'
    icon: string
  }>(),
  { trend: 'up' },
)
</script>

<template>
  <div class="stat-card">
    <div class="stat-header">
      <span class="stat-label">{{ label }}</span>
      <div class="stat-icon"><AppIcon :name="icon" :size="16" /></div>
    </div>
    <div class="stat-value">{{ value }}</div>
    <div class="stat-change" :class="{ 'text-tertiary': trend === 'neutral' }">
      <AppIcon v-if="trend === 'up'" name="trending-up" :size="12" />
      <span>{{ change }}</span>
    </div>
  </div>
</template>
