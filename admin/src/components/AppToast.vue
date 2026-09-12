<script setup lang="ts">
import { dismiss, useToasts } from '../lib/toast'
import AppIcon from './AppIcon.vue'

const toasts = useToasts()

const icons: Record<string, string> = {
  success: 'check',
  error: 'alert-circle',
  info: 'info',
}
</script>

<template>
  <Teleport to="body">
    <div class="toast-wrap" aria-live="polite">
      <TransitionGroup name="toast">
        <div v-for="t in toasts" :key="t.id" class="toast" :class="`toast-${t.kind}`" @click="dismiss(t.id)">
          <AppIcon :name="icons[t.kind]" :size="15" />
          <span>{{ t.message }}</span>
        </div>
      </TransitionGroup>
    </div>
  </Teleport>
</template>

<style scoped>
.toast-wrap {
  position: fixed;
  top: 16px;
  left: 50%;
  transform: translateX(-50%);
  z-index: 200;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  pointer-events: none;
}
.toast {
  pointer-events: auto;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 9px 16px;
  border-radius: var(--radius-md);
  background: var(--color-surface);
  box-shadow: 0 6px 24px rgba(0, 0, 0, 0.14);
  border: 1px solid var(--color-border);
  font-size: 13.5px;
  color: var(--color-text-primary);
  cursor: pointer;
  max-width: min(80vw, 480px);
}
.toast-success { border-color: var(--color-success); }
.toast-success :deep(.icon, svg) { color: var(--color-success); }
.toast-error { border-color: var(--color-error); }
.toast-info { border-color: var(--color-accent); }

.toast-enter-active,
.toast-leave-active {
  transition: all 0.25s ease;
}
.toast-enter-from,
.toast-leave-to {
  opacity: 0;
  transform: translateY(-8px);
}
</style>
