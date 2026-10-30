import { KEYS } from '../utils/storage'

const TOKEN_KEY = KEYS.TOKEN
const REFRESH_KEY = KEYS.REFRESH

// 构建期内置地址（CI 注入 APP_DEFAULT_BASE，见 DEVELOPMENT「地址注入」）。
// 未注入时的兜底：dev 回落本机后端，打包产物留空（此时只能靠登录后下发的绿联通道）。
const BUILTIN_BASE: string = __DEFAULT_BASE__ || (import.meta.env.DEV ? 'http://localhost:8080' : '')

// 绿联通道下发的地址（登录后由 utils/channel.ts 写入；未启用时为空串）
let channelBase = ''

/** 设置当前绿联通道地址（空串 = 清除，回落内置地址） */
export function setChannelBase(base: string) {
  channelBase = base ? base.replace(/\/+$/, '') : ''
}

/** 当前生效地址：绿联通道优先，其次构建期内置；两处都无则空串（无法发请求） */
export function getBase(): string {
  return channelBase || BUILTIN_BASE
}

/** 构建期内置地址（诊断展示用；不含通道） */
export function getBuiltinBase(): string {
  return BUILTIN_BASE
}

// 通道层注入点：client 不直接依赖 utils/channel（否则循环依赖），
// 由 channel.ts 在初始化时把自己的实现注册进来。
let channelCookieGetter: () => string = () => ''
let channelFailureHandler: () => Promise<boolean> = async () => false
let channelBeforeRequest: () => Promise<void> = async () => {}
/** 注册通道层回调（channel.ts 调用一次） */
export function registerChannelHooks(hooks: {
  cookie: () => string
  onFailed: () => Promise<boolean>
  beforeRequest: () => Promise<void>
}) {
  channelCookieGetter = hooks.cookie
  channelFailureHandler = hooks.onFailed
  channelBeforeRequest = hooks.beforeRequest
}

function getChannelCookie(): string {
  return channelCookieGetter()
}

/** 请求失败时询问通道层能否恢复（换通道后重试） */
async function onChannelRequestFailed(): Promise<boolean> {
  try {
    return await channelFailureHandler()
  } catch {
    return false
  }
}

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY)
}

/** 当前 refresh token（登出时交给服务端吊销用；正常请求流程不直接读它） */
export function getRefreshToken(): string | null {
  return localStorage.getItem(REFRESH_KEY)
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
  /** 响应体的结构化字段（如分片上传 409 的 received = 服务端已收字节，用于重同步） */
  details?: Record<string, unknown>
  constructor(status: number, message: string, details?: Record<string, unknown>) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.details = details
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
  // 绿联凭据接近到期时先续期（到期前 60 秒）；其它通道此调用立即返回
  await channelBeforeRequest()

  // 绿联通道需要附带服务端换发的会话 cookie（其它通道为空）
  const channelCookie = getChannelCookie()
  if (channelCookie) headers.set('Cookie', channelCookie)

  let res: Response
  try {
    res = await fetch(getBase() + path, { ...options, headers })
  } catch (err) {
    // 网络层失败（连接不上 / 超时）：可能是通道失效，标记并重取一次后重试
    if (retry && !path.startsWith('/api/v1/auth/')) {
      const recovered = await onChannelRequestFailed()
      if (recovered) return api<T>(path, options, false)
    }
    throw err
  }

  // 登录/刷新自身的 401 是「凭据不对」，不是「会话过期」：不走重试分支，
  // 直接把服务端文案透出（否则登录页输错密码会显示「登录已过期，请重新登录」）
  const isAuthEntry = path.startsWith('/api/v1/auth/login') || path.startsWith('/api/v1/auth/refresh')
  // 绿联通道的 cookie 失效时，绿联中转会 302 到登录页（浏览器会跟随，最终可能落在登录 HTML）。
  // 判据：非 2xx 且当前通道是绿联 → 让通道层重取一次（服务端会重新登录）后重试。
  // 注意与 401 区分：通道失效不代表登录态失效，绝不能 clearAuth。
  if (!res.ok && res.status !== 401 && retry && !isAuthEntry) {
    const recovered = await onChannelRequestFailed()
    if (recovered) return api<T>(path, options, false)
  }
  if (res.status === 401 && retry && !isAuthEntry) {
    if (await refreshOnce()) {
      return api<T>(path, options, false)
    }
    clearAuth()
    throw new ApiError(401, '登录已过期，请重新登录')
  }
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { error?: string }
    // 整个响应体作为 details 带上（分片上传 409 的 received 在里面，调用方据此重同步）
    throw new ApiError(res.status, body.error ?? `HTTP ${res.status}`, body as Record<string, unknown>)
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
