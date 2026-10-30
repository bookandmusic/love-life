// 应用内更新纯逻辑：服务端返回结构的解析、版本对比、强更与「稍后」记忆。
// 零 Tauri / DOM 依赖（可被 node:test 直接测）。设计见客户端设计「应用内更新」。
// .ts 后缀：node:test（type stripping）要求相对导入带显式后缀，bundler/vite 两者兼容
import { KEYS } from './storage.ts'

export interface UpdateRelease {
  versionName: string
  versionCode: number
  /** APK 成品文件名（缓存目录与冷启动恢复用） */
  fileName: string
  /** 下载地址；相对路径，交给下载器前需拼上当前 base */
  downloadUrl: string
  /** GitHub asset digest；服务端不下发时为空——完整性由服务端侧校验与安装器签名一致性承担 */
  sha256: string
  notes: string[]
  minVersionCode: number
  sizeBytes: number
}

// ---------- 服务端更新清单解析 ----------

/** 服务端 `/api/v1/app/update/latest` 的响应形状 */
interface ServerRelease {
  available?: unknown
  version_name?: unknown
  version_code?: unknown
  notes?: unknown
  min_version_code?: unknown
  asset_name?: unknown
  asset_size?: unknown
  download_path?: unknown
  reason?: unknown
}

/** release 说明按行拆开：`min-version-code: N` 行作强更门槛，其余非空行作说明 */
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

/**
 * 服务端响应 → UpdateRelease。
 * 未配置或取回失败时服务端返回 `{available:false, reason}`，这里转成可读错误。
 */
export function parseRelease(serverJson: unknown): UpdateRelease {
  if (typeof serverJson !== 'object' || serverJson === null) {
    throw new Error('更新清单响应不是对象')
  }
  const d = serverJson as ServerRelease
  if (d.available === false) {
    throw new Error(typeof d.reason === 'string' ? d.reason : '暂时无法获取更新信息')
  }
  const versionName = typeof d.version_name === 'string' ? d.version_name : ''
  const versionCode = typeof d.version_code === 'number' ? d.version_code : NaN
  const assetName = typeof d.asset_name === 'string' ? d.asset_name : ''
  const downloadPath = typeof d.download_path === 'string' ? d.download_path : ''
  if (!versionName || !Number.isFinite(versionCode)) throw new Error('更新清单缺少版本信息')
  if (!assetName || !downloadPath) throw new Error('更新清单缺少下载地址')

  const notes = typeof d.notes === 'string' ? d.notes : ''
  const meta = bodyToMeta(notes)
  const minFromServer = typeof d.min_version_code === 'number' ? d.min_version_code : 0

  return {
    versionName,
    versionCode,
    fileName: assetName,
    // 相对路径：调用处拼上当前 base，下载因此与业务请求走同一地址（含绿联通道）
    downloadUrl: downloadPath,
    sha256: '',
    notes: meta.notes,
    minVersionCode: minFromServer || meta.minVersionCode,
    sizeBytes: typeof d.asset_size === 'number' ? d.asset_size : 0,
  }
}

/** 检查更新的请求路径（相对 base） */
export const UPDATE_LATEST_PATH = '/api/v1/app/update/latest'

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
    /* 隐私模式等存储不可写：本次会话内仍生效，只是重启后会再提示 */
  }
}

export function humanBytes(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return '—'
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(0)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}