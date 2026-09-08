import { ref } from 'vue'
import { defineStore } from 'pinia'
import { api, clearAuth, getToken, setTokens } from '../api/client'

export interface UserInfo {
  id: number
  username: string
  role: string
  nickname: string
  ai_style?: string
  chat_retention_days?: number
}

export const useAuth = defineStore('auth', () => {
  const user = ref<UserInfo | null>(null)

  async function login(username: string, password: string) {
    // 生效地址由登录页双地址探测（probeAndSelect）先行写入，此处不再覆盖
    const res = await api<{ token: string; refresh_token: string; user: UserInfo }>(
      '/api/v1/auth/login',
      {
        method: 'POST',
        body: JSON.stringify({ username, password, client: 'app' }),
      },
    )
    setTokens(res.token, res.refresh_token)
    user.value = res.user
  }

  async function restore() {
    if (!getToken()) return
    try {
      user.value = await api<UserInfo>('/api/v1/auth/me')
    } catch {
      user.value = null
    }
  }

  function logout() {
    clearAuth()
    user.value = null
  }

  return { user, login, restore, logout }
})
