import { ref } from 'vue'

// 全局轻提示（后台统一反馈通道：成功/失败/信息，顶部居中自动消失，
// 不在页面内联展示以免布局跳动—— 布局约定）
export interface ToastItem {
  id: number
  message: string
  kind: 'success' | 'error' | 'info'
}

const toasts = ref<ToastItem[]>([])
let seq = 0

function push(message: string, kind: ToastItem['kind'], duration: number): void {
  const id = ++seq
  toasts.value.push({ id, message, kind })
  setTimeout(() => dismiss(id), duration)
}

export function dismiss(id: number): void {
  toasts.value = toasts.value.filter((t) => t.id !== id)
}

export function useToasts() {
  return toasts
}

export const toast = {
  success: (message: string) => push(message, 'success', 2600),
  error: (message: string) => push(message, 'error', 4200),
  info: (message: string) => push(message, 'info', 2600),
}
