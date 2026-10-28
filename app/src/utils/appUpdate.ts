// 应用内更新纯逻辑：GitHub Releases 清单解析、versionCode 换算、代理列表与竞速、
// 强更与「稍后」记忆。零 Tauri / DOM 依赖（fetch 为 Node 18+ / 浏览器原生，可被 node:test 直接测）。
// 设计见客户端设计「应用内更新」。
// .ts 后缀：node:test（type stripping）要求相对导入带显式后缀，bundler/vite 两者兼容
import { KEYS } from './storage.ts'

export const GH_REPO = 'bookandmusic/love-life'

export interface UpdateRelease {
  versionName: string
  versionCode: number
  /** APK 成品文件名（缓存目录与冷启动恢复用） */
  fileName: string
  /** GitHub release asset 的 browser_download_url（可能被代理前缀包装后交给下载器） */
  downloadUrl: string
  /** GitHub asset digest（形如 sha256:xxx，可能为空——为空时仅 TLS + 签名一致性兜底） */
  sha256: string
  notes: string[]
  minVersionCode: number
  sizeBytes: number
}

// ---------- 版本换算 ----------

/** 解析 release tag：'v0.1.1' → '0.1.1'；非 v 前缀三段式返回 null */
export function parseTagName(tag: string): string | null {
  const m = /^v?(\d+)\.(\d+)\.(\d+)$/.exec(tag.trim())
  return m ? `${m[1]}.${m[2]}.${m[3]}` : null
}

/**
 * version 与 Android versionCode 的换算规则，与 tauri-cli 生成一致：
 * major*10^6 + minor*10^3 + patch（v0.1.1 产物 AXML 实测 = 1001）。
 * tauri.conf.json 的 bundle.android.versionCode 显式配置会覆盖公式（本项目未配置）。
 */
export function tagToVersionCode(version: string): number | null {
  const m = /^(\d+)\.(\d+)\.(\d+)$/.exec(version.trim())
  if (!m) return null
  const [major, minor, patch] = m.slice(1).map(Number)
  return major * 1_000_000 + minor * 1_000 + patch
}

// ---------- GitHub release 解析 ----------

/** release body 里约定行：min-version-code: N（缺省 0 = 不强更）；其余非空行作 notes */
export function bodyToMeta(body: string): { notes: string[]; minVersionCode: number } {
  const notes: string[] = []
  let minVersionCode = 0
  for (const line of (body ?? '').split('\n')) {
    const m = /^min-version-code:\s*(\d+)\s*$/i.exec(line.trim())
    if (m) {
      minVersionCode = Number(m[1])
    } else if (line.trim()) {
      notes.push(line.trim())
    }
  }
  return { notes, minVersionCode }
}

interface AssetLike {
  name?: unknown
  browser_download_url?: unknown
  size?: unknown
  digest?: unknown
}

/** 按 `love-life-<version>-` 前缀 + `.apk` 后缀挑出 APK asset（Actions 现有命名；排除 .aab） */
export function pickApkAsset(
  assets: unknown,
  versionName: string
): { name: string; url: string; size: number; digest: string } | null {
  if (!Array.isArray(assets)) return null
  const prefix = `love-life-${versionName}-`
  const apk = (assets as AssetLike[]).filter((a) => {
    if (typeof a !== 'object' || a === null) return false
    const n = a.name
    return typeof n === 'string' && n.startsWith(prefix) && n.endsWith('.apk')
  })
  // 发布约定只有一个 universal APK；真出现多个时取第一个，避免静默选错版本
  const first = apk[0]
  if (!first || typeof first.browser_download_url !== 'string') return null
  return {
    name: first.name as string,
    url: first.browser_download_url as string,
    size: typeof first.size === 'number' ? first.size : 0,
    digest: typeof first.digest === 'string' ? first.digest : '',
  }
}

/** GitHub API releases/latest 响应 → UpdateRelease；形状不符 / 缺 APK 时抛明确错误 */
export function parseRelease(apiJson: unknown): UpdateRelease {
  if (typeof apiJson !== 'object' || apiJson === null) {
    throw new Error('更新清单响应不是对象')
  }
  const d = apiJson as Record<string, unknown>
  const tagName = typeof d.tag_name === 'string' ? d.tag_name : ''
  const versionName = parseTagName(tagName)
  if (!versionName) throw new Error(`更新清单 tag 无法解析: ${tagName || '（缺失）'}`)
  const versionCode = tagToVersionCode(versionName)
  if (versionCode === null) throw new Error('版本号换算失败')
  const asset = pickApkAsset(d.assets, versionName)
  if (!asset) throw new Error(`更新清单缺少 APK（v${versionName}）`)
  const { notes, minVersionCode } = bodyToMeta(typeof d.body === 'string' ? d.body : '')
  return {
    versionName,
    versionCode,
    fileName: asset.name,
    downloadUrl: asset.url,
    sha256: asset.digest,
    notes,
    minVersionCode,
    sizeBytes: asset.size,
  }
}

// ---------- 代理列表与竞速（设计见客户端设计「应用内更新 · 代理与威胁模型」） ----------

/** 内置加速代理：准入 = 实测 API 200 + Range 206 且存续核验通过（2026-10-28 实测仅此一家全合格） */
export const BUILTIN_PROXIES = ['https://gh-proxy.com']

/** 去尾斜杠（空串原样） */
function normalizeProxy(prefix: string): string {
  return prefix.trim().replace(/\/+$/, '')
}

/** releases/latest 的 API URL；代理非空时按 `<proxy>/<原 URL>` 前缀拼接 */
export function buildApiUrl(proxyPrefix: string, repo: string = GH_REPO): string {
  const p = normalizeProxy(proxyPrefix)
  const raw = `https://api.github.com/repos/${repo}/releases/latest`
  return p ? `${p}/${raw}` : raw
}

/** asset 下载 URL（https://github.com/...）按同样前缀拼接 */
export function buildDownloadUrl(assetUrl: string, proxyPrefix: string): string {
  const p = normalizeProxy(proxyPrefix)
  return p ? `${p}/${assetUrl}` : assetUrl
}

function dedupeProxies(list: string[]): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const raw of list) {
    const p = normalizeProxy(raw)
    if (!p || seen.has(p)) continue
    seen.add(p)
    out.push(p)
  }
  return out
}

/** 当前生效代理列表：内置恒在首位，自定义按添加序追加，整体归一化去重 */
export function getProxies(): string[] {
  let custom: string[] = []
  try {
    const parsed: unknown = JSON.parse(localStorage.getItem(KEYS.GH_PROXIES) ?? 'null')
    if (Array.isArray(parsed)) custom = parsed.filter((x): x is string => typeof x === 'string')
  } catch {
    /* 存储不可读 / 坏 JSON：退回纯内置 */
  }
  return dedupeProxies([...BUILTIN_PROXIES, ...custom])
}

/** 添加自定义代理（空值或与已有项同值则忽略） */
export function addProxy(prefix: string): void {
  const p = normalizeProxy(prefix)
  if (!p || getProxies().includes(p)) return
  try {
    const custom = getProxies().filter((x) => !BUILTIN_PROXIES.includes(x))
    localStorage.setItem(KEYS.GH_PROXIES, JSON.stringify([...custom, p]))
  } catch {
    /* 隐私模式等存储不可写：不持久化 */
  }
}

/** 移除自定义代理；内置项不可删（静默 no-op） */
export function removeProxy(prefix: string): void {
  if (isBuiltinProxy(prefix)) return
  try {
    const rest = getProxies().filter(
      (x) => x !== normalizeProxy(prefix) && !BUILTIN_PROXIES.includes(x)
    )
    localStorage.setItem(KEYS.GH_PROXIES, JSON.stringify(rest))
  } catch {
    /* 同上 */
  }
}

export function isBuiltinProxy(prefix: string): boolean {
  return BUILTIN_PROXIES.includes(normalizeProxy(prefix))
}

/** 并发竞速取最新清单：最快「完整拿到且解析通过」的一路胜出并取消其余；全败抛错。
 *  成功判定到解析层而非响应头——响应快但清单坏的代理不该赢。 */
export async function raceLatestRelease(
  proxies: string[],
  timeoutMs: number
): Promise<{ release: UpdateRelease; proxy: string }> {
  const controllers = proxies.map(() => new AbortController())
  try {
    const attempts = proxies.map(async (proxy, i) => {
      const timer = setTimeout(() => controllers[i].abort(), timeoutMs)
      try {
        const res = await fetch(buildApiUrl(proxy), { signal: controllers[i].signal })
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        return { release: parseRelease(await res.json()), proxy }
      } finally {
        clearTimeout(timer)
      }
    })
    return await Promise.any(attempts)
  } catch {
    throw new Error('全部更新通道不可达')
  } finally {
    for (const c of controllers) c.abort() // 取消落败者（含未归的超时路）；胜者此时已完成，无害
  }
}

// ---------- 版本对比与「稍后」记忆 ----------

export function isUpdateAvailable(localCode: number, latestCode: number): boolean {
  return latestCode > localCode
}

/** 强更 = 本机低于最低可用版本；提示级强更（断网可绕过，见客户端设计「应用内更新」） */
export function isForceUpdate(localCode: number, minVersionCode: number): boolean {
  return minVersionCode > 0 && localCode < minVersionCode
}

function safeGetLocal(key: string): string | null {
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}

export function getDismissedVersionCode(): number {
  const v = safeGetLocal(KEYS.DISMISSED_UPDATE)
  const n = v === null ? NaN : Number(v)
  return Number.isInteger(n) && n >= 0 ? n : 0
}

export function setDismissedVersionCode(code: number): void {
  try {
    localStorage.setItem(KEYS.DISMISSED_UPDATE, String(code))
  } catch {
    /* 同上 */
  }
}

/** 体积人性化（下载体积提示用） */
export function humanBytes(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return ''
  const units = ['B', 'KB', 'MB', 'GB']
  let v = n
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v >= 100 || i === 0 ? Math.round(v) : v.toFixed(1)} ${units[i]}`
}
