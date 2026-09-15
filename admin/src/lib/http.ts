import { API_BASE_URL } from './env'

const TOKEN_KEY = 'love-life-admin-token'
const REFRESH_KEY = 'love-life-admin-refresh'
const ROLE_KEY = 'love-life-admin-role'

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY)
}

export function setToken(token: string): void {
  localStorage.setItem(TOKEN_KEY, token)
}

export function getRefreshToken(): string | null {
  return localStorage.getItem(REFRESH_KEY)
}

export function setRefreshToken(token: string): void {
  localStorage.setItem(REFRESH_KEY, token)
}

/** 清空全部本地凭证（token/refresh/role） */
export function clearAuth(): void {
  localStorage.removeItem(TOKEN_KEY)
  localStorage.removeItem(REFRESH_KEY)
  localStorage.removeItem(ROLE_KEY)
}

export function getRole(): string | null {
  return localStorage.getItem(ROLE_KEY)
}

export function setRole(role: string): void {
  localStorage.setItem(ROLE_KEY, role)
}

/**
 * 统一错误文案：仅 ApiError 透出服务端 message，其余（网络层异常等）一律用兜底文案，
 * 避免把 `TypeError: Failed to fetch` 这类内部英文信息显示给用户（与 app 端 errText 同口径）。
 */
export function errMsg(e: unknown, fallback: string): string {
  return e instanceof ApiError && e.message ? e.message : fallback
}

export class ApiError extends Error {
  status: number
  constructor(message: string, status: number) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

function buildUrl(path: string): string {
  if (!API_BASE_URL) return path
  if (/^https?:\/\//.test(path)) return path
  const base = API_BASE_URL.replace(/\/$/, '')
  return base + (path.startsWith('/') ? path : '/' + path)
}

// 会话续期：单飞（并发 401 只发一次 refresh；refresh token 为一次性，重复使用会失败）
let refreshing: Promise<boolean> | null = null

async function doRefresh(): Promise<boolean> {
  const rt = getRefreshToken()
  if (!rt) return false
  try {
    const res = await fetch(buildUrl('/api/v1/auth/refresh'), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ refresh_token: rt }),
    })
    if (!res.ok) return false
    const data = (await res.json()) as { token: string; refresh_token: string }
    setToken(data.token)
    if (data.refresh_token) setRefreshToken(data.refresh_token)
    return true
  } catch {
    return false
  }
}

function tryRefresh(): Promise<boolean> {
  refreshing ??= doRefresh().finally(() => (refreshing = null))
  return refreshing
}

/**
 * 统一请求入口：自动拼接 API base URL、注入 Bearer 令牌、规范化错误。
 * 所有业务 api 模块均经此函数访问后端，组件内不直接调用 fetch。
 * 401 时先用 refresh token 续期并重放一次；仍失败则清凭证回登录页
 * （登录请求除外，避免把密码错误也当成会话失效）。
 */
export async function http<T = unknown>(
  path: string,
  options: RequestInit = {},
): Promise<T> {
  const headers = new Headers(options.headers)
  // FormData 上传：让浏览器自带 boundary，不要手动塞 application/json（会导致服务端解析失败）
  if (!(options.body instanceof FormData) && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }
  const token = getToken()
  if (token) headers.set('Authorization', `Bearer ${token}`)

  let res = await fetch(buildUrl(path), { ...options, headers })

  const authPath = path.includes('/auth/login') || path.includes('/auth/refresh')
  if (res.status === 401 && !authPath && (await tryRefresh())) {
    headers.set('Authorization', `Bearer ${getToken()}`)
    res = await fetch(buildUrl(path), { ...options, headers })
  }

  if (!res.ok) {
    if (res.status === 401 && !authPath) {
      clearAuth()
      window.location.replace(import.meta.env.BASE_URL + 'login')
    }
    const body = (await res.json().catch(() => ({}))) as { error?: string }
    throw new ApiError(body.error ?? `HTTP ${res.status}`, res.status)
  }

  if (res.status === 204) return undefined as T
  return res.json() as Promise<T>
}
