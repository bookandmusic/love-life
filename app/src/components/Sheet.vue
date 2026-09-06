<script setup lang="ts">
// 通用底部弹出 sheet（iOS 风格圆角 + 柔阴影），支持标题/正文 slot + 取消/主操作按钮。
// 受控显隐由父组件 v-model:show 控制。
import { watch } from 'vue'

const props = defineProps<{
  show: boolean
  title?: string
  confirmText?: string
  cancelText?: string
  confirmDisabled?: boolean
  confirmDanger?: boolean
}>()
const emit = defineEmits<{
  'update:show': [v: boolean]
  confirm: []
  cancel: []
}>()

function close() { emit('update:show', false) }
function onCancel() { emit('cancel'); close() }
function onConfirm() { emit('confirm') }

watch(() => props.show, (v) => {
  if (typeof document === 'undefined') return
  document.body.style.overflow = v ? 'hidden' : ''
})
</script>

<template>
  <Teleport to="body">
    <Transition name="sheet">
      <div v-if="show" class="sheet-mask" @click.self="onCancel">
        <div class="sheet-card" role="dialog" aria-modal="true">
          <header v-if="title" class="sheet-title">{{ title }}</header>
          <div class="sheet-body">
            <slot />
          </div>
          <footer class="sheet-foot">
            <button class="btn ghost" @click="onCancel">{{ cancelText || '取消' }}</button>
            <button
              class="btn"
              :class="confirmDanger ? 'danger' : 'primary'"
              :disabled="confirmDisabled"
              @click="onConfirm"
            >{{ confirmText || '确定' }}</button>
          </footer>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.sheet-mask {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.32);
  display: flex;
  align-items: flex-end;
  justify-content: center;
  z-index: 1000;
  -webkit-tap-highlight-color: transparent;
}
.sheet-card {
  width: min(420px, 100%);
  background: var(--surface);
  color: var(--ink);
  border-top-left-radius: 18px;
  border-top-right-radius: 18px;
  padding: 16px 16px calc(16px + env(safe-area-inset-bottom)) 16px;
  box-shadow: 0 -8px 24px rgba(0, 0, 0, 0.08);
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.sheet-title {
  font-size: 15px;
  font-weight: 600;
  text-align: center;
  color: var(--ink);
}
.sheet-body { display: flex; flex-direction: column; gap: 8px; }
.sheet-foot {
  display: flex;
  gap: 8px;
  margin-top: 4px;
}
.btn {
  flex: 1;
  height: 42px;
  border-radius: 12px;
  border: none;
  font-size: 15px;
  font-weight: 500;
  cursor: pointer;
  background: var(--muted-ink); opacity: 0.12;
  color: var(--ink);
}
.btn:disabled { opacity: 0.4; cursor: not-allowed; }
.btn.primary { background: var(--primary, #ff8fab); color: #fff; }
.btn.danger  { background: var(--danger,  #e0494b); color: #fff; }

.sheet-enter-active, .sheet-leave-active { transition: opacity .18s ease; }
.sheet-enter-active .sheet-card, .sheet-leave-active .sheet-card { transition: transform .22s ease; }
.sheet-enter-from, .sheet-leave-to { opacity: 0; }
.sheet-enter-from .sheet-card, .sheet-leave-to .sheet-card { transform: translateY(16px); }
</style>
