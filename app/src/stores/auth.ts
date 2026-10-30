import { ref } from 'vue'
import { defineStore } from 'pinia'
import { api, clearAuth, getToken, getRefreshToken, setTokens } from '../api/client'
import { clearCredentials, loadCredentials, saveCredentials } from '../utils/credentials'

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

  /** 记住登录：把凭据写入系统安全存储（失败不阻断登录，返回是否成功） */
  async function rememberCredentials(username: string, password: string): Promise<boolean> {
    return saveCredentials(username, password)
  }

  /** 登录页预填：读取已存凭据；无则 null */
  async function loadSavedCredentials() {
    return loadCredentials()
  }

  /** 取消「记住登录」或退出登录时清除凭据 */
  async function forgetCredentials(): Promise<void> {
    await clearCredentials()
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
   * 启动恢复（BootView 用）：区分「凭据失效」与「网络失败」。
   *
   * - 成功：ok
   * - 401 且 refresh 已失败：若有已存凭据则静默重登一次；仍失败判 expired
   * - 网络层失败：判 network（**不**清本地凭据，让用户重试而不是重输密码）
   */
  async function restoreWithRetry(): Promise<{ ok: boolean; reason?: 'expired' | 'network' }> {
    if (!getToken()) return { ok: false, reason: 'expired' }
    try {
      user.value = await api<UserInfo>('/api/v1/auth/me')
      return { ok: true }
    } catch (e) {
      const status = e instanceof Error && 'status' in e ? (e as { status: number }).status : 0
      // 非 401：网络/服务端异常，保留凭据让用户重试
      if (status !== 401) return { ok: false, reason: 'network' }

      // 401：access 与 refresh 都已失效，用已存凭据静默重登一次。
      // 不做多次重试：密码错误重试无意义，网络问题重试会拖慢启动——
      // 失败即判失效，由 BootView 区分「回登录页」还是「网络重试」。
      const saved = await loadCredentials()
      if (!saved) return { ok: false, reason: 'expired' }
      try {
        await login(saved.username, saved.password)
        return { ok: true }
      } catch (e) {
        // 登录失败区分网络问题：网络层失败应让用户重试，而不是回登录页
        const status = e instanceof Error && 'status' in e ? (e as { status: number }).status : 0
        return { ok: false, reason: status === 0 ? 'network' : 'expired' }
      }
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
      await clearCredentials()
    }
  }

  return { user, login, restore, restoreWithRetry, logout, rememberCredentials, loadSavedCredentials, forgetCredentials }
})
