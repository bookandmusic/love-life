<script setup lang="ts">
import AppIcon from './AppIcon.vue'

withDefaults(
  defineProps<{
    variant?: 'success' | 'warning' | 'error' | 'info' | 'neutral'
    icon?: string
  }>(),
  { variant: 'neutral' },
)
</script>

<template>
  <span class="badge" :class="`badge-${variant}`">
    <AppIcon v-if="icon" :name="icon" :size="12" />
    <slot />
  </span>
</template>
