import { http, setToken, setRefreshToken, setRole, clearAuth } from '../lib/http'

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
  // 仅 admin 角色可登录后台（DESIGN 第 9 章）；其余角色不落 token，直接拒绝
  if (res.user.role !== 'admin') {
    throw new Error('仅管理员可登录后台')
  }
  setToken(res.token)
  if (res.refresh_token) setRefreshToken(res.refresh_token)
  setRole(res.user.role)
  return res
}

/** 退出登录：清空本地全部凭证（服务端 logout 由后端处理，前端仅清理本地态） */
export function logout(): void {
  clearAuth()
}
