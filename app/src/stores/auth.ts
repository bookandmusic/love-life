import { ref } from 'vue'
import { defineStore } from 'pinia'
import { api, clearAuth, getToken, setBase, setTokens } from '../api/client'

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

  async function login(base: string, username: string, password: string) {
    setBase(base)
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
