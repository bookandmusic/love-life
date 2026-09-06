<script setup lang="ts">
// 轻量确认 sheet（替代 window.confirm），基于 Sheet 组件。
import Sheet from './Sheet.vue'

defineProps<{
  show: boolean
  title?: string
  message?: string
  confirmText?: string
  cancelText?: string
  confirmDanger?: boolean
}>()
const emit = defineEmits<{
  'update:show': [v: boolean]
  confirm: []
}>()

function onConfirm() { emit('confirm'); emit('update:show', false) }
function onCancel() { emit('update:show', false) }
</script>

<template>
  <Sheet
    :show="show"
    :title="title"
    :confirm-text="confirmText || '确定'"
    :cancel-text="cancelText || '取消'"
    :confirm-danger="confirmDanger"
    @update:show="emit('update:show', $event)"
    @cancel="onCancel"
    @confirm="onConfirm"
  >
    <p class="msg" v-if="message">{{ message }}</p>
    <slot />
  </Sheet>
</template>

<style scoped>
.msg { font-size: 14px; color: var(--muted-ink); text-align: center; margin: 0; }
</style>
