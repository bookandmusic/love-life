// 媒体另存到本机（APP 全屏查看器的「保存」按钮，照片/视频/音频同一入口）。
//
// 手机优先（Android APK / iOS / 桌面 Tauri）：走系统「另存为」（Android 侧是 SAF
// ACTION_CREATE_DOCUMENT）再写入 —— 零存储权限、用户自选去处（相册/下载/文件管理器都能选），
// 且按块流式写，大视频不进内存。插件 API 走 window.__TAURI__.dialog / .fs（tauri.conf.json
// 开了 withGlobalTauri，因此前端不需要额外 npm 依赖；Rust 侧注册了 dialog/fs 两个插件）。
//
// 浏览器 / 无 Tauri 环境降级：先探测服务端是否带 Content-Disposition（GET + Range: bytes=0-0，
// 只取 1 字节），带则把带 download=1 的直链交给浏览器下载管理器（流式、可续传、文件名由服务端给），
// 否则退回 fetch→blob→<a download>。
import { fullUrl } from '../api/client'
import { mediaExtFromUrl, mediaKindExt, mediaKindLabel } from './mediaKind'

export type SaveStatus = 'saved' | 'cancelled' | 'failed'

export interface SaveOutcome {
  status: SaveStatus
  message?: string
}

export interface SaveTarget {
  id: number | string
  kind: string
  /** 当前签名地址（mediaUrl(m).raw），15 分钟 TTL */
  url: string
  /** 拍摄时间（无则用今天；服务端取不到名称时客户端按它拼） */
  taken_at?: string | null
  /** 动态内序号（只有「查看器 items 就是该动态媒体」的场景能确定；其余留空，兜底用媒体 id） */
  seq?: number
  onProgress?: (loaded: number, total: number) => void
  /** 签名临期时换新地址（POST /media/reurl），仅 401/403 时调用一次 */
  refresh?: () => Promise<string>
}

interface TauriDialogApi {
  save?: (opts: { defaultPath?: string }) => Promise<string | null>
}

interface TauriFileHandle {
  write: (data: Uint8Array) => Promise<void>
  close: () => Promise<void>
}

interface TauriFsApi {
  writeFile?: (path: string, data: Uint8Array) => Promise<void>
  open?: (path: string, opts: Record<string, unknown>) => Promise<TauriFileHandle>
}

function tauriDialog(): TauriDialogApi | undefined {
  const w = window as unknown as { __TAURI__?: { dialog?: TauriDialogApi } }
  return w.__TAURI__?.dialog
}

function tauriFs(): TauriFsApi | undefined {
  const w = window as unknown as { __TAURI__?: { fs?: TauriFsApi } }
  return w.__TAURI__?.fs
}

// mediaFileName 与服务端 downloadName 同规则：{日期}_{照片|视频|录音}_{序号}.{ext}
export function mediaFileName(kind: string, date: string | null | undefined, seq: number, ext?: string): string {
  const d = (date ?? '').slice(0, 10) || new Date().toISOString().slice(0, 10)
  return d + '_' + mediaKindLabel(kind) + '_' + seq + '.' + (ext ?? mediaKindExt(kind))
}

// parseServerFileName 解析 Content-Disposition 里的文件名（RFC 5987 filename* 优先）
export function parseServerFileName(header: string | null): string | null {
  if (!header) return null
  const star = /filename\*=UTF-8''([^;]+)/i.exec(header)
  if (star) {
    try {
      return decodeURIComponent(star[1].trim())
    } catch {
      /* 编码异常时退回 ASCII 名 */
    }
  }
  const plain = /filename="([^"]+)"/i.exec(header)
  return plain ? plain[1] : null
}

function withDownload(url: string): string {
  return fullUrl(url) + (url.includes('?') ? '&' : '?') + 'download=1'
}

// probeServerName 试探服务端文件名（Range 0-0 只取 1 字节）。返回 null 表示
// 拿不到（无 Tauri 又无 Content-Disposition）——此时浏览器路径只能用 blob 兜底。
async function probeServerName(url: string): Promise<string | null> {
  try {
    const res = await fetch(withDownload(url), { headers: { Range: 'bytes=0-0' } })
    if (!res.ok && res.status !== 206) return null
    return parseServerFileName(res.headers.get('content-disposition'))
  } catch {
    return null
  }
}

// fetchWithRetry 取字节；签名过期（401/403）时换新签名重试一次
async function fetchWithRetry(url: string, refresh?: () => Promise<string>): Promise<Response> {
  let target = url
  let res = await fetch(fullUrl(target))
  if ((res.status === 401 || res.status === 403) && refresh) {
    target = await refresh()
    res = await fetch(fullUrl(target))
  }
  if (!res.ok) throw new Error('HTTP ' + res.status)
  return res
}

async function writeStream(fs: TauriFsApi, path: string, res: Response, onProgress?: (loaded: number, total: number) => void): Promise<void> {
  const total = Number(res.headers.get('content-length') ?? 0)
  // 注意：getReader() 会锁定 body，只有真要流式写时才取（否则后面 arrayBuffer() 会报「已读」）
  if (fs.open && res.body) {
    const reader = res.body.getReader()
    const handle = await fs.open(path, { write: true, create: true, truncate: true })
    let loaded = 0
    try {
      for (;;) {
        const { done, value } = await reader.read()
        if (done) break
        if (value) {
          await handle.write(value)
          loaded += value.byteLength
          onProgress?.(loaded, total)
        }
      }
    } finally {
      await handle.close()
    }
    return
  }
  const buf = new Uint8Array(await res.arrayBuffer())
  if (!fs.writeFile) throw new Error('fs 插件未就绪')
  await fs.writeFile(path, buf)
  onProgress?.(buf.byteLength, total || buf.byteLength)
}

function fallbackName(t: SaveTarget): string {
  // 序号：只有调用方明确知道「动态内序号」时才用；否则用媒体 id，绝不拿查看器下标冒充
  const seq = t.seq ?? (Number(t.id) || 1)
  // 扩展名：优先 URL 上的 ?ext=（mock 的 webm/wav、生产的 mp4/m4a 都能对上真实字节），没有才用词表兜底
  return mediaFileName(t.kind, t.taken_at, seq, mediaExtFromUrl(t.url))
}

// saveMedia 保存单条媒体到本机。返回 saved / cancelled（用户取消另存为）/ failed。
export async function saveMedia(t: SaveTarget): Promise<SaveOutcome> {
  const dialog = tauriDialog()
  const fs = tauriFs()
  try {
    if (dialog?.save && fs) {
      // 先弹系统另存为（取消＝零成本），再下载并流式写入
      const name = (await probeServerName(t.url)) ?? fallbackName(t)
      const path = await dialog.save({ defaultPath: name })
      if (path === null || path === undefined) return { status: 'cancelled' }
      const res = await fetchWithRetry(t.url, t.refresh)
      await writeStream(fs, path, res, t.onProgress)
      return { status: 'saved' }
    }

    // 浏览器：能拿到 Content-Disposition 就交给浏览器下载管理器（流式、文件名服务端给）
    const serverName = await probeServerName(t.url)
    if (serverName) {
      const a = document.createElement('a')
      a.href = withDownload(t.url)
      a.rel = 'noopener'
      document.body.appendChild(a)
      a.click()
      a.remove()
      return { status: 'saved' }
    }

    const res = await fetchWithRetry(t.url, t.refresh)
    const blob = await res.blob()
    const href = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = href
    a.download = fallbackName(t)
    document.body.appendChild(a)
    a.click()
    a.remove()
    window.setTimeout(() => URL.revokeObjectURL(href), 5000)
    return { status: 'saved' }
  } catch (e) {
    console.warn('[saveMedia] 保存失败', e)
    return { status: 'failed', message: '保存失败，请检查网络后重试' }
  }
}