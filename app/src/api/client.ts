import { KEYS } from '../utils/storage'

const BASE_LEGACY_KEY = KEYS.BASE
const TOKEN_KEY = KEYS.TOKEN
const REFRESH_KEY = KEYS.REFRESH

// 旧单地址迁移（AP1）：历史版本只有一个 base，视作公网槽初值；双地址槽已用时跳过
function migrateLegacyBase() {
  const legacy = localStorage.getItem(BASE_LEGACY_KEY)
  if (legacy && !localStorage.getItem(KEYS.BASE_LAN) && !localStorage.getItem(KEYS.BASE_WAN)) {
    localStorage.setItem(KEYS.BASE_WAN, legacy)
  }
}
migrateLegacyBase()

export function getBase(): string {
  return localStorage.getItem(BASE_LEGACY_KEY) ?? ''
}

// normalizeBase 统一去尾斜杠；空串原样返回（未配置槽位）
function normalizeBase(base: string): string {
  return base.replace(/\/+$/, '')
}

export function setBase(base: string) {
  localStorage.setItem(BASE_LEGACY_KEY, normalizeBase(base))
}

export function getLanBase(): string {
  return localStorage.getItem(KEYS.BASE_LAN) ?? ''
}

export function getWanBase(): string {
  return localStorage.getItem(KEYS.BASE_WAN) ?? ''
}

export function setDualBase(lan: string, wan: string) {
  localStorage.setItem(KEYS.BASE_LAN, normalizeBase(lan))
  localStorage.setItem(KEYS.BASE_WAN, normalizeBase(wan))
}

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY)
}

export function setTokens(token: string, refreshToken: string) {
  localStorage.setItem(TOKEN_KEY, token)
  localStorage.setItem(REFRESH_KEY, refreshToken)
}

// 认证代际：clearAuth() 递增，使在途 refresh 的结果作废（登出后不得把 token 写回）
let authEpoch = 0

export function clearAuth() {
  authEpoch++
  refreshInFlight = null
  localStorage.removeItem(TOKEN_KEY)
  localStorage.removeItem(REFRESH_KEY)
}

// 单飞：并发 401 共享同一次 refresh。rotation 后端下多发必有一发失败，会误判「登录已过期」
let refreshInFlight: Promise<boolean> | null = null

function refreshOnce(): Promise<boolean> {
  refreshInFlight ??= doRefresh().finally(() => {
    refreshInFlight = null
  })
  return refreshInFlight
}

async function doRefresh(): Promise<boolean> {
  const rt = localStorage.getItem(REFRESH_KEY)
  if (!rt) return false
  const epoch = authEpoch
  try {
    const res = await fetch(getBase() + '/api/v1/auth/refresh', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ refresh_token: rt }),
    })
    if (!res.ok) return false
    const data = (await res.json()) as { token: string; refresh_token: string }
    if (epoch !== authEpoch) return false // 期间已登出：结果作废
    setTokens(data.token, data.refresh_token)
    return true
  } catch {
    return false
  }
}

/** 请求失败的错误对象：message 供展示，status 供调用处按语义分流（如 409/410 = 卡片已过期） */
export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

/** 统一错误文案：api() 恒抛 Error（含 ApiError），这里只做「空消息」兜底，调用处不再各写一遍三元 */
// 只透出服务端文案（ApiError）；网络层异常等内部错误给调用方的人话兜底，
// 避免 "Failed to execute 'json' on 'Response'" 这类英文内部信息出现在 toast 上
export function errText(e: unknown, fallback: string): string {
  return e instanceof ApiError && e.message ? e.message : fallback
}

export async function api<T>(path: string, options: RequestInit = {}, retry = true): Promise<T> {
  const headers = new Headers(options.headers)
  if (!(options.body instanceof FormData)) {
    headers.set('Content-Type', 'application/json')
  }
  const token = getToken()
  if (token) headers.set('Authorization', `Bearer ${token}`)

  const res = await fetch(getBase() + path, { ...options, headers })

  // 登录/刷新自身的 401 是「凭据不对」，不是「会话过期」：不走重试分支，
  // 直接把服务端文案透出（否则登录页输错密码会显示「登录已过期，请重新登录」）
  const isAuthEntry = path.startsWith('/api/v1/auth/login') || path.startsWith('/api/v1/auth/refresh')
  if (res.status === 401 && retry && !isAuthEntry) {
    if (await refreshOnce()) {
      return api<T>(path, options, false)
    }
    clearAuth()
    throw new ApiError(401, '登录已过期，请重新登录')
  }
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { error?: string }
    throw new ApiError(res.status, body.error ?? `HTTP ${res.status}`)
  }
  // 204 / 空响应体（DELETE 走 noContent()，mock 与真实服务端一致）不能按 JSON 解析，
  // 否则每次删除都会抛 "Unexpected end of JSON input"
  if (res.status === 204) return undefined as T
  const text = await res.text()
  return (text ? (JSON.parse(text) as T) : (undefined as T))
}

// 媒体签名 URL 是相对路径（/api/v1/...），<img> 加载时拼上服务端地址
export function fullUrl(u: string): string {
  return getBase() + u
}

// 日期展示：ISO（T 分隔）与空格分隔都可能出现，统一为 "YYYY-MM-DD · HH:mm"（按本地时区）
export function fmtDateTime(s: string): string {
  const d = new Date(s.replace(' ', 'T'))
  if (Number.isNaN(d.getTime())) return s.slice(0, 16).replace(/[T ]/, ' · ')
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} · ${p(d.getHours())}:${p(d.getMinutes())}`
}
