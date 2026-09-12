<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue'
import AppIcon from './AppIcon.vue'

defineProps<{ title: string; width?: number }>()
const emit = defineEmits<{ (e: 'close'): void }>()

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close')
}
onMounted(() => window.addEventListener('keydown', onKeydown))
onUnmounted(() => window.removeEventListener('keydown', onKeydown))
</script>

<template>
  <Teleport to="body">
    <div class="modal-overlay" @click.self="emit('close')">
      <div class="modal-panel" :style="{ maxWidth: (width ?? 520) + 'px' }">
        <div class="modal-header">
          <span class="modal-title">{{ title }}</span>
          <button class="modal-close" type="button" aria-label="关闭" @click="emit('close')">
            <AppIcon name="x" :size="16" />
          </button>
        </div>
        <div class="modal-body">
          <slot />
        </div>
        <div v-if="$slots.footer" class="modal-footer">
          <slot name="footer" />
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(26, 26, 26, 0.4);
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding: 80px 16px 16px;
  z-index: 100;
}
.modal-panel {
  background: var(--color-surface);
  border-radius: var(--radius-lg);
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.16);
  width: 100%;
  max-height: calc(100vh - 96px);
  display: flex;
  flex-direction: column;
}
.modal-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 16px 20px;
  border-bottom: 1px solid var(--color-border);
}
.modal-title {
  font-size: 15px;
  font-weight: 600;
  color: var(--color-text-primary);
}
.modal-close {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--color-text-tertiary);
  cursor: pointer;
}
.modal-close:hover {
  background: var(--color-accent-light);
  color: var(--color-text-primary);
}
.modal-body {
  padding: 20px;
  overflow-y: auto;
}
.modal-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding: 14px 20px;
  border-top: 1px solid var(--color-border);
}
</style>
