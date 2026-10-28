import { ref } from 'vue'
import { defineStore } from 'pinia'
import { api, clearAuth, getToken, getRefreshToken, setTokens } from '../api/client'

export interface UserInfo {
  id: number
  username: string
  role: string
  nickname: string
  ai_style?: string
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

  /**
   * 退出登录：先吊销服务端 refresh token，再清本地。
   * 服务端调用失败不阻断登出——本地必须清干净，否则用户以为退出了其实还在；
   * 极端情况（断网）下残留 token 会在 30 天内自然过期。
   */
  async function logout() {
    const rt = getRefreshToken()
    try {
      if (rt) {
        await api('/api/v1/auth/logout', {
          method: 'POST',
          body: JSON.stringify({ refresh_token: rt }),
        })
      }
    } catch {
      // 忽略：本地清理在 finally 里保证执行
    } finally {
      clearAuth()
      user.value = null
    }
  }

  return { user, login, restore, logout }
})
