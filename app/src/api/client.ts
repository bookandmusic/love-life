import { KEYS, getItem, setItem, removeItem } from '../utils/storage'

const BASE_KEY = KEYS.BASE
const TOKEN_KEY = KEYS.TOKEN
const REFRESH_KEY = KEYS.REFRESH

// 旧单地址迁移（AP1）：历史版本只有一个 base，视作公网槽初值；双地址槽已用时跳过
function migrateLegacyBase() {
  const legacy = getItem(BASE_KEY)
  if (legacy && !getItem(KEYS.BASE_LAN) && !getItem(KEYS.BASE_WAN)) {
    setItem(KEYS.BASE_WAN, legacy)
  }
}
migrateLegacyBase()

export function getBase(): string {
  return getItem(BASE_KEY) ?? ''
}

// normalizeBase 统一去尾斜杠；空串原样返回（未配置槽位）
function normalizeBase(base: string): string {
  return base.replace(/\/+$/, '')
}

export function setBase(base: string) {
  setItem(BASE_KEY, normalizeBase(base))
}

export function getLanBase(): string {
  return getItem(KEYS.BASE_LAN) ?? ''
}

export function getWanBase(): string {
  return getItem(KEYS.BASE_WAN) ?? ''
}

export function setDualBase(lan: string, wan: string) {
  setItem(KEYS.BASE_LAN, normalizeBase(lan))
  setItem(KEYS.BASE_WAN, normalizeBase(wan))
}

export function getToken(): string | null {
  return getItem(TOKEN_KEY)
}

export function setTokens(token: string, refreshToken: string) {
  setItem(TOKEN_KEY, token)
  setItem(REFRESH_KEY, refreshToken)
}

export function clearAuth() {
  removeItem(TOKEN_KEY)
  removeItem(REFRESH_KEY)
}

async function tryRefresh(): Promise<boolean> {
  const rt = getItem(REFRESH_KEY)
  if (!rt) return false
  try {
    const res = await fetch(getBase() + '/api/v1/auth/refresh', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ refresh_token: rt }),
    })
    if (!res.ok) return false
    const data = (await res.json()) as { token: string; refresh_token: string }
    setTokens(data.token, data.refresh_token)
    return true
  } catch {
    return false
  }
}

export async function api<T>(path: string, options: RequestInit = {}, retry = true): Promise<T> {
  const headers = new Headers(options.headers)
  if (!(options.body instanceof FormData)) {
    headers.set('Content-Type', 'application/json')
  }
  const token = getToken()
  if (token) headers.set('Authorization', `Bearer ${token}`)

  const res = await fetch(getBase() + path, { ...options, headers })

  if (res.status === 401 && retry) {
    if (await tryRefresh()) {
      return api<T>(path, options, false)
    }
    clearAuth()
    throw new Error('登录已过期，请重新登录')
  }
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { error?: string }
    throw new Error(body.error ?? `HTTP ${res.status}`)
  }
  return (await res.json()) as T
}

// 媒体签名 URL 是相对路径（/api/v1/...），<img> 加载时拼上服务端地址
export function fullUrl(u: string): string {
  return getBase() + u
}

// 日期展示：ISO（T 分隔）与空格分隔都可能出现，统一为 "YYYY-MM-DD · HH:mm"
export function fmtDateTime(s: string): string {
  return s.slice(0, 16).replace(/[T ]/, ' · ')
}
