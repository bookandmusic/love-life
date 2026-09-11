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
  /* border-box：否则 width 只算内容宽，加上 16px 内边距后弹窗比屏幕/卡片宽出 32px（手机贴边溢出、桌面超出卡片） */
  box-sizing: border-box;
  position: relative; /* 桌面档要压在 ::before 那层暗色之上 */
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
  box-sizing: border-box;
  height: 42px;
  border-radius: 12px;
  border: none;
  font-size: 15px;
  font-weight: 500;
  cursor: pointer;
  /* 只压背景，不压元素透明度：opacity 会连文字一起变淡，看起来像被禁用 */
  background: var(--sk);
  color: var(--ink);
}
.btn:disabled { opacity: 0.4; cursor: not-allowed; }
.btn.primary { background: var(--primary, #ff8fab); color: #fff; }
.btn.danger  { background: var(--danger,  #e0494b); color: #fff; }

/* 桌面档：弹窗从「应用卡片的下边缘」弹出——遮罩留出与卡片相同的外距，
   弹窗宽度对齐卡片宽度、四角同圆角，看起来是卡片内部升起的 sheet（手机/平板仍贴屏幕底） */
@media (min-width: 1280px) {
  .sheet-mask {
    padding-bottom: var(--frame-inset);
  }
  /* 看得见的暗层收进卡片矩形（与卡片同外距、同圆角）；mask 本身仍铺满视口，
     所以「点卡片外空白也能关闭」的手感不变——::before 设了 pointer-events: none，点击穿到 mask 上 */
  .sheet-mask {
    background: transparent;
  }
  .sheet-mask::before {
    content: '';
    position: absolute;
    top: var(--frame-inset);
    bottom: var(--frame-inset);
    left: 50%;
    transform: translateX(-50%);
    width: min(var(--frame-max), calc(100% - 2 * var(--frame-inset)));
    border-radius: var(--frame-radius);
    background: rgba(0, 0, 0, 0.32);
    pointer-events: none;
  }
  .sheet-card {
    width: min(var(--frame-max), calc(100% - 2 * var(--frame-inset)));
    border-radius: var(--frame-radius);
  }
  /* 表面随卡片拉满（所以看起来是从卡片下缘升起），内容仍保持对话框宽度 */
  .sheet-title,
  .sheet-body,
  .sheet-foot {
    width: 100%;
    max-width: 520px;
    margin-inline: auto;
  }
}

.sheet-enter-active, .sheet-leave-active { transition: opacity .18s ease; }
.sheet-enter-active .sheet-card, .sheet-leave-active .sheet-card { transition: transform .22s ease; }
.sheet-enter-from, .sheet-leave-to { opacity: 0; }
.sheet-enter-from .sheet-card, .sheet-leave-to .sheet-card { transform: translateY(16px); }
</style>
