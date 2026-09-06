<script setup lang="ts">
import AppIcon from './AppIcon.vue'

withDefaults(
  defineProps<{
    variant?: 'info' | 'warning' | 'error' | 'success'
    icon?: string
  }>(),
  { variant: 'info', icon: 'info' },
)
</script>

<template>
  <div class="alert" :class="`alert-${variant}`">
    <AppIcon :name="icon" :size="18" />
    <div class="alert-content">
      <slot />
    </div>
  </div>
</template>
