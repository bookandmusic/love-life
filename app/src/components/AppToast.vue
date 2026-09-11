<script setup lang="ts">
// 浮层 toast：此前 8 个视图各写一份「Transition + .toast + setTimeout」，这里收敛成一处。
// text 一变即重新计时；duration=0 表示不自动消失（错误提示靠点击关闭）。
import { onBeforeUnmount, watch } from 'vue'

const props = withDefaults(
  defineProps<{ text?: string; duration?: number; bottom?: string; pill?: boolean }>(),
  { text: '', duration: 1800, bottom: 'calc(16px + env(safe-area-inset-bottom))', pill: false },
)
const emit = defineEmits<{ (e: 'done'): void }>()

let timer: ReturnType<typeof setTimeout> | null = null
watch(
  () => props.text,
  (t) => {
    if (timer) clearTimeout(timer)
    timer = t && props.duration > 0 ? setTimeout(() => emit('done'), props.duration) : null
  },
  { immediate: true },
)
onBeforeUnmount(() => {
  if (timer) clearTimeout(timer)
})
</script>

<template>
  <Transition name="fade">
    <div v-if="text" class="toast" :class="{ pill }" :style="{ bottom }" @click="emit('done')">{{ text }}</div>
  </Transition>
</template>

<style scoped>
.toast {
  position: fixed;
  left: 50%;
  transform: translateX(-50%);
  background: rgba(0, 0, 0, 0.78);
  color: #fff;
  font-size: 14px;
  padding: 10px 20px;
  border-radius: 12px;
  z-index: 2000;
  max-width: 90vw;
  text-align: center;
  cursor: pointer;
}
.toast.pill {
  padding: 8px 16px;
  border-radius: 999px;
  font-size: 13px;
}
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.2s ease;
}
.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
</style>
