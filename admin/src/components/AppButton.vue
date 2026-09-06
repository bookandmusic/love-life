<script setup lang="ts">
import AppIcon from './AppIcon.vue'

withDefaults(
  defineProps<{
    variant?: 'primary' | 'secondary' | 'danger'
    size?: 'md' | 'sm'
    icon?: string
    iconOnly?: boolean
    disabled?: boolean
    type?: 'button' | 'submit'
  }>(),
  { variant: 'secondary', size: 'md', iconOnly: false, disabled: false, type: 'button' },
)
</script>

<template>
  <button
    :type="type"
    :disabled="disabled"
    class="btn"
    :class="[
      `btn-${variant}`,
      size === 'sm' && 'btn-sm',
      iconOnly && 'btn-icon',
    ]"
  >
    <AppIcon v-if="icon" :name="icon" :size="size === 'sm' ? 14 : 16" />
    <slot />
  </button>
</template>
