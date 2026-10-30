// 服务地址通道：决定每个请求发往哪里。
//
// 三段降级（顺序固定）：
//   1. 绿联通道（服务端换发 cookie，境内最快）
//   2. 备用地址（后台配置的 HTTPS 列表，按 order 探活取第一个可用）
//   3. 构建期内置地址（最后兜底，永远存在）
//
// 绿联链路说明：APP 拿到 {baseURL, cookie} 后**直接请求绿联地址**，
// 请求经绿联中转到达 NAS 上的本服务。服务端是被访问方，看不到 APP 与绿联之间的握手，
// 因此 cookie 失效只能由 APP 自己发现（请求失败）后回头重取通道。
//
// 并发与续期：
//   - 取通道单飞：并发请求共享同一个 Promise，只发一次
//   - 到期前 60 秒主动重取（见 ensureFresh）
//   - 不轮询：只在「启动 / 到期前 / 请求失败」三个时机重取
import { api, getBuiltinBase, registerChannelHooks, setChannelBase } from '../api/client'

export type ChannelKind = 'uglink' | 'backup' | 'builtin'

interface UglinkChannel {
  available: boolean
  reason?: string
  baseURL?: string
  cookie?: string
  expiresAt?: string
}

interface BackupItem {
  id: string
  name: string
  url: string
  enabled: boolean
}

// 当前生效状态
let kind: ChannelKind = 'builtin'
let cookie = '' // 仅绿联通道使用
let expiresAt = 0 // 绿联凭据的建议续期时间戳（ms）
let activeBase = '' // 当前生效的 base（绿联/备用/内置），供诊断展示

// 单飞：并发触发重取时只发一次请求
let inflight: Promise<void> | null = null

/** 当前通道种类（诊断展示用） */
export function currentKind(): ChannelKind {
  return kind
}

/** 当前生效的 base（诊断展示用） */
export function currentBase(): string {
  return activeBase || getBuiltinBase()
}

/**
 * 初始化通道：登录后（或在 BootView 中）调用一次。
 * 依次尝试绿联 → 备用地址 → 内置；任何一步成功即停止。
 */
export async function initChannel(): Promise<void> {
  await resolve()
}

/**
 * 请求前的续期检查：绿联凭据接近到期则重取。
 * 由 api() 在发请求前调用；只在真需要时才发请求，正常路径零开销。
 */
export async function ensureFresh(): Promise<void> {
  if (kind !== 'uglink' || expiresAt === 0) return
  // 到期前 60 秒重取；服务端若判定会话仍有效会直接返回同一份 cookie
  if (Date.now() >= expiresAt - 60_000) await refreshChannel()
}

/** 重取通道（单飞）：并发调用共享同一次请求 */
export function refreshChannel(): Promise<void> {
  inflight ??= resolve().finally(() => {
    inflight = null
  })
  return inflight
}

async function resolve(): Promise<void> {
  if (await tryUglink()) return
  if (await tryBackup()) return
  kind = 'builtin'
  cookie = ''
  expiresAt = 0
  activeBase = getBuiltinBase()
  setChannelBase('')
}

async function tryUglink(): Promise<boolean> {
  try {
    const ch = await api<UglinkChannel>('/api/v1/uglink/channel')
    if (!ch.available || !ch.baseURL) return false
    kind = 'uglink'
    cookie = ch.cookie ?? ''
    expiresAt = ch.expiresAt ? Date.parse(ch.expiresAt) : 0
    activeBase = ch.baseURL
    setChannelBase(ch.baseURL)
    return true
  } catch {
    return false
  }
}

// 并发探活所有启用地址，按实测延迟取最快。
// 不做固定优先级：家里最快的地址出门后可能完全不可达，优先级会立刻过时。
async function tryBackup(): Promise<boolean> {
  let items: BackupItem[] = []
  try {
    items = (await api<{ items: BackupItem[] }>('/api/v1/channels/backup')).items ?? []
  } catch {
    return false
  }
  const enabled = items.filter((i) => i.enabled)
  if (enabled.length === 0) return false

  const results = await Promise.all(
    enabled.map(async (item) => ({ item, ms: await probe(item.url) }))
  )
  // ms 为 Infinity 表示不可达；全部不可达则往下退
  const best = results.filter((r) => Number.isFinite(r.ms)).sort((a, b) => a.ms - b.ms)[0]
  if (!best) return false

  kind = 'backup'
  cookie = ''
  expiresAt = 0
  activeBase = best.item.url
  setChannelBase(best.item.url)
  return true
}

/** 探活：GET {base}/api/v1/health，2 秒超时；返回往返毫秒数，失败返回 Infinity */
async function probe(base: string): Promise<number> {
  const started = Date.now()
  try {
    const res = await fetch(base.replace(/\/+$/, '') + '/api/v1/health', {
      signal: AbortSignal.timeout(2_000),
    })
    if (!res.ok) return Number.POSITIVE_INFINITY
    return Date.now() - started
  } catch {
    return Number.POSITIVE_INFINITY
  }
}

// 定时探活间隔。代价很小：一次 GET /api/v1/health（几百字节），
// 且**仅在前台运行**——切到后台立即停，回来再启动。
const WATCH_INTERVAL_MS = 5 * 60 * 1000

/**
 * 启动通道择优监听。三种信号，都只在前台工作：
 *   1. online 事件：网络恢复
 *   2. visibilitychange：切回前台
 *   3. 定时探活：覆盖「平滑切网不发事件」的情况（WiFi 弱信号自动切蜂窝）
 *
 * 为什么定时器代价可接受：一次健康检查几百字节，5 分钟一次；
 * 真正的开销在「后台也在跑」，所以切后台立刻停表。
 */
export function startChannelWatcher(): void {
  if (typeof window === 'undefined') return

  let lastRefresh = 0
  let timer: number | undefined

  const refreshOnChange = () => {
    // 去抖：online 与 visibilitychange 常成对触发，10 秒内只重取一次
    const now = Date.now()
    if (now - lastRefresh < 10_000) return
    lastRefresh = now
    void refreshChannel()
  }

  const startTimer = () => {
    if (timer !== undefined) return
    timer = window.setInterval(refreshOnChange, WATCH_INTERVAL_MS)
  }
  const stopTimer = () => {
    if (timer === undefined) return
    window.clearInterval(timer)
    timer = undefined
  }

  window.addEventListener('online', refreshOnChange)
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') {
      refreshOnChange()
      startTimer() // 回前台恢复定时探活
    } else {
      stopTimer() // 进后台停止：不需要在用户看不到时探活
    }
  })

  // 首次启动时若在前台则开始计时
  if (document.visibilityState === 'visible') startTimer()
}

// 注册到 api()：带 cookie、请求失败时换通道。
// 内置地址都失败说明是网络问题，换通道无意义，直接返回 false 让上层报错。
registerChannelHooks({
  cookie: () => cookie,
  onFailed: async () => {
    if (kind === 'builtin') return false
    await refreshChannel()
    return true
  },
  beforeRequest: ensureFresh,
})
