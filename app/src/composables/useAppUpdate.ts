// 应用内更新状态机（全局单例）：检查 → 弹窗 → 后台下载 → 就绪安装 → 交接系统。
// 下载在 Kotlin 前台服务执行，进度经 Channel 回传；本文件只驱动 UI 状态与调用应用命令。
import { Channel, invoke } from '@tauri-apps/api/core'
import { ref } from 'vue'
import {
  buildDownloadUrl,
  getDismissedVersionCode,
  getProxies,
  isForceUpdate,
  isUpdateAvailable,
  raceLatestRelease,
  setDismissedVersionCode,
  type UpdateRelease,
} from '../utils/appUpdate'

export type UpdateState =
  | 'idle'
  | 'checking'
  | 'prompt'
  | 'downloading'
  | 'ready'
  | 'handoff'
  | 'failed'
  | 'noupdate'

type DownloadMsg =
  | { type: 'progress'; received: number; total: number | null; versionName: string }
  | {
      type: 'finished'
      ok: boolean
      path: string | null
      error: string | null
      resumable: boolean
      cancelled: boolean
      versionName: string
    }

const isTauri = typeof window !== 'undefined' && '__TAURI_INTERNALS__' in window

/** 启动静默检查的延迟：给首屏渲染让路 */

const state = ref<UpdateState>('idle')
const release = ref<UpdateRelease | null>(null)
const localVersionCode = ref(0)
const progress = ref({ received: 0, total: 0 })
const errorMsg = ref('')
const manualNotice = ref('')
/** 缺「安装未知应用」授权：用户从系统页回来后自动重试安装 */
const needsPermission = ref(false)
let cachedPath = ''
let downloading = false
/** 竞速胜出的代理：下载 URL 用它构建（下载阶段不再竞速，见客户端设计「应用内更新 · 代理与威胁模型」） */
let winnerProxy = ''

export function useAppUpdate() {
  /** 检查更新：manual=true 时失败给提示、跳过「稍后」记忆（用户主动要看） */
  async function check(manual = false): Promise<void> {
    // 防重入：App.vue 与 LoginView 同挂载各有一个延迟定时器，避免并发双请求
    if (!isTauri || downloading || state.value === 'checking') return
    state.value = 'checking'
    errorMsg.value = ''
    if (manual) manualNotice.value = ''
    try {
      const raced = await raceLatestRelease(getProxies(), 10_000)
      const r = raced.release
      winnerProxy = raced.proxy
      localVersionCode.value = await invoke<number>('get_version_code')
      if (!isUpdateAvailable(localVersionCode.value, r.versionCode)) {
        state.value = 'noupdate'
        release.value = null
        if (manual) manualNotice.value = '已是最新版本'
        return
      }
      release.value = r
      // 冷启动恢复：成品已在缓存 → 直接进就绪态，不重弹确认
      const cached = await invoke<string | null>('has_cached', { fileName: r.fileName })
      if (cached) {
        cachedPath = cached
        state.value = 'ready'
        return
      }
      // 「稍后」记忆：同版本非强更不再打扰；手动检查不受限
      if (
        !manual &&
        !isForceUpdate(localVersionCode.value, r.minVersionCode) &&
        getDismissedVersionCode() === r.versionCode
      ) {
        state.value = 'idle'
        return
      }
      state.value = 'prompt'
    } catch (e) {
      state.value = 'idle'
      const msg = e instanceof Error ? e.message : String(e)
      if (manual) manualNotice.value = `检查失败：${msg}`
    }
  }

  function dismiss() {
    if (release.value && !isForceUpdate(localVersionCode.value, release.value.minVersionCode)) {
      setDismissedVersionCode(release.value.versionCode)
    }
    state.value = 'idle'
  }

  /** 点「立即更新」：Kotlin 前台服务接管下载，本命令 resolve 仅代表服务已启动 */
  async function startDownload(): Promise<void> {
    const r = release.value
    if (!r || downloading) return
    downloading = true
    state.value = 'downloading'
    progress.value = { received: 0, total: r.sizeBytes }
    const ch = new Channel<DownloadMsg>()
    ch.onmessage = (m) => {
      if (m.type === 'progress') {
        progress.value = { received: m.received, total: m.total ?? r.sizeBytes }
      } else {
        downloading = false
        if (m.ok && m.path) {
          cachedPath = m.path
          state.value = 'ready'
        } else if (m.cancelled) {
          state.value = 'idle'
        } else {
          errorMsg.value = m.error ?? '下载失败'
          state.value = 'failed'
        }
      }
    }
    try {
      await invoke('download_apk', {
        url: buildDownloadUrl(r.downloadUrl, winnerProxy || getProxies()[0]),
        expectedSha256: r.sha256,
        fileName: r.fileName,
        versionName: r.versionName,
        onProgress: ch,
      })
    } catch (e) {
      downloading = false
      errorMsg.value = e instanceof Error ? e.message : String(e)
      state.value = 'failed'
    }
  }

  async function cancelDownload(): Promise<void> {
    await invoke('cancel_download').catch(() => {})
    // 实际落态等 finished(cancelled) 消息；这里不抢状态，防竞态误回 idle
  }

  /** 失败重试：重新竞速选代理再续传——胜出代理可能已死，不复用旧 winner（计划决策「重试重新竞速」） */
  async function retryDownload(): Promise<void> {
    try {
      const raced = await raceLatestRelease(getProxies(), 10_000)
      release.value = raced.release
      winnerProxy = raced.proxy
    } catch (e) {
      errorMsg.value = e instanceof Error ? e.message : String(e)
      return // 保持 failed 态，用户稍后可再试
    }
    await startDownload()
  }

  async function install(): Promise<void> {
    if (!cachedPath) return
    try {
      const r = await invoke<{ needsPermission: boolean }>('install_apk', { path: cachedPath })
      if (r.needsPermission) {
        needsPermission.value = true
        return // 保持 ready；visibilitychange 回来自动重试
      }
      state.value = 'handoff'
    } catch (e) {
      errorMsg.value = e instanceof Error ? e.message : String(e)
      state.value = 'failed'
    }
  }

  return {
    state,
    release,
    localVersionCode,
    progress,
    errorMsg,
    manualNotice,
    needsPermission,
    isForce: () =>
      release.value !== null &&
      isForceUpdate(localVersionCode.value, release.value.minVersionCode),
    check,
    dismiss,
    startDownload,
    retryDownload,
    cancelDownload,
    install,
  }
}

// 「安装未知应用」授权页返回（WebView 重新可见）后自动重试交接。
// 全局单例生命周期 = 应用生命周期，无需解绑；web 降级下 isTauri 为 false 不挂载。
if (isTauri) {
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState !== 'visible') return
    const { needsPermission: np, install } = useAppUpdate()
    if (np.value && state.value === 'ready') {
      np.value = false
      void install()
    }
  })
}
