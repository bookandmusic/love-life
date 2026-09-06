import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { api } from '../api/client'

export interface SpaceMember {
  id: number
  nickname: string
  role: string
}

export interface SpaceInfo {
  name: string
  signature: string
  love_start_date: string | null
  members: SpaceMember[]
  days_together: number | null
  next_anniversary: { name: string; date: string; days_until: number }
}

// 共享空间信息 store（阶段5.2 B14：Home/Posts 等不再各自重复拉 /space，切页不重复请求）
export const useSpace = defineStore('space', () => {
  const space = ref<SpaceInfo | null>(null)
  const loading = ref(false)
  const error = ref('')

  const members = computed(() => new Map((space.value?.members ?? []).map((m) => [m.id, m])))

  // 归属色 class：girl→her / boy→him / 其他→other（Posts/Detail 共用，单一实现）
  function roleClass(authorId: number): string {
    const mb = members.value.get(authorId)
    if (mb?.role === 'girl') return 'her'
    if (mb?.role === 'boy') return 'him'
    return 'other'
  }

  async function load(force = false) {
    if (space.value && !force) return
    loading.value = true
    error.value = ''
    try {
      space.value = await api<SpaceInfo>('/api/v1/space')
    } catch (e) {
      error.value = e instanceof Error ? e.message : '加载失败'
    } finally {
      loading.value = false
    }
  }

  return { space, loading, error, members, roleClass, load }
})