import { http, setToken, setRefreshToken, getRefreshToken, setRole, clearAuth, ApiError } from '../lib/http'

export interface LoginResponse {
  token: string
  refresh_token: string
  user: { role: string }
}

export async function login(username: string, password: string): Promise<LoginResponse> {
  const res = await http<LoginResponse>('/api/v1/auth/login', {
    method: 'POST',
    body: JSON.stringify({ username, password, client: 'admin' }),
  })
  // 仅 admin 角色可登录后台（ARCHITECTURE 第 9 章）；其余角色不落 token，直接拒绝
  // 抛 ApiError 而非普通 Error：errMsg 仅透出 ApiError 文案，用普通 Error 会让该业务原因被兜底文案吞掉
  // 403 与后端同口径（客户端 client=admin 时非 admin 角色返回 403）
  if (res.user.role !== 'admin') {
    throw new ApiError('仅管理员可登录后台', 403)
  }
  setToken(res.token)
  if (res.refresh_token) setRefreshToken(res.refresh_token)
  setRole(res.user.role)
  return res
}

/**
 * 退出登录：先吊销服务端 refresh token，再清本地凭证。
 * 服务端调用失败不阻断登出——本地必须清干净，否则用户以为退出了其实还在；
 * 极端情况下（网络断）残留的 refresh token 会在 30 天内自然过期。
 */
export async function logout(): Promise<void> {
  const refreshToken = getRefreshToken()
  try {
    if (refreshToken) {
      await http('/api/v1/auth/logout', {
        method: 'POST',
        body: JSON.stringify({ refresh_token: refreshToken }),
      })
    }
  } catch {
    // 忽略：本地清理在 finally 里保证执行
  } finally {
    clearAuth()
  }
}
