import { ref } from 'vue'
import { defineStore } from 'pinia'
import { api } from '../api/client'

// 共享未读数 store（阶段5.2 B14：NavActions 每次挂载拉 unread-count 收敛到单一 store）
export const useUnread = defineStore('unread', () => {
  const count = ref(0)

  async function refresh() {
    try {
      const res = await api<{ count: number }>('/api/v1/notifications/unread-count')
      count.value = res.count
    } catch {
      /* 红点失败静默 */
    }
  }

  function reset() {
    count.value = 0
  }

  return { count, refresh, reset }
})