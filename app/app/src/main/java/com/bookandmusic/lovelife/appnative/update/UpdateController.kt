package com.bookandmusic.lovelife.appnative.update

import android.content.Context
import com.bookandmusic.lovelife.appnative.BuildConfig
import com.bookandmusic.lovelife.appnative.data.ApiClient
import com.bookandmusic.lovelife.appnative.data.AppUpdate
import com.bookandmusic.lovelife.appnative.data.DismissedUpdateStore
import com.bookandmusic.lovelife.appnative.data.UpdateLatestDto
import com.bookandmusic.lovelife.appnative.data.UpdateRelease
import com.bookandmusic.lovelife.appnative.data.errText
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import java.io.File

/** 更新弹窗的状态：发现新版 / 下载中 / 就绪 / 失败 / 交接 */
enum class UpdateStage { Idle, Checking, Prompt, Downloading, Ready, Handoff, Failed }

/**
 * 应用内更新的全局单例状态。
 *
 * 单一事实来源：检查、下载（前台服务）、安装交接都写这里，UI 只订阅 [state]——
 * 下载进度在同进程内直接写 [state]，不需要跨进程桥。
 */
object UpdateController {

    data class State(
        val stage: UpdateStage = UpdateStage.Idle,
        val release: UpdateRelease? = null,
        val localVersionCode: Int = BuildConfig.VERSION_CODE,
        val received: Long = 0,
        val total: Long = 0,
        val error: String = "",
        /** 手动检查的结果提示（已是最新 / 检查失败原因） */
        val manualNotice: String = "",
        /** 缺「安装未知应用」授权：用户从系统页回来后自动重试交接 */
        val needsPermission: Boolean = false,
        /** 下载中弹窗被收起（后台继续）：设置页点「检查更新」可重新打开 */
        val sheetCollapsed: Boolean = false,
    ) {
        /** 强更：弹窗不可关、无「稍后」 */
        val force: Boolean
            get() = release?.let { AppUpdate.isForceUpdate(localVersionCode, it.minVersionCode) } == true

        val percent: Int
            get() = if (total > 0) ((received * 100 / total).coerceIn(0, 100)).toInt() else 0
    }

    private val _state = MutableStateFlow(State())
    val state: StateFlow<State> = _state.asStateFlow()

    @Volatile private var cachedPath: String = ""

    /**
     * 检查更新。manual=true 时给结果提示、且**跳过「稍后」记忆**（用户主动要看）。
     * 服务端 `available:false` 表示暂无已就绪的包 → 视为无需更新，不是错误。
     */
    /**
     * 检查结果的决策表（纯函数，可单测）。
     *
     * **顺序即语义**：冷启动恢复排在「稍后」记忆之前，
     * 导致「下载完 → 点稍后」之后每次启动都撞见缓存里的成品，又弹「更新已就绪」。
     */
    internal enum class Decision {
        /** 无更新 / 不打扰：停在 Idle */
        Idle,

        /** 成品已在缓存：直接待安装，不重弹确认 */
        Ready,

        /** 发现新版本：弹「发现新版本」 */
        Prompt,
    }

    internal fun decide(
        isUpdateAvailable: Boolean,
        hasCachedPackage: Boolean,
        force: Boolean,
        manual: Boolean,
        dismissedCode: Int,
        releaseCode: Int,
    ): Decision = when {
        !isUpdateAvailable -> Decision.Idle
        // 手动检查：用户主动要看，跳过「稍后」记忆，有成品就直接待安装
        manual -> if (hasCachedPackage) Decision.Ready else Decision.Prompt
        // 非手动：「稍后」记忆优先于冷启动恢复——否则点过稍后也会被成品包反复弹
        !force && dismissedCode == releaseCode -> Decision.Idle
        hasCachedPackage -> Decision.Ready
        else -> Decision.Prompt
    }

    suspend fun check(context: Context, manual: Boolean = false) {
        // 登录页与主壳各挂一次检查：并发进来时只让第一个真正发请求。
        // 用原子占位而非「先读再写」——两边都读到 Idle 时，后写者会用陈旧快照覆盖前者。
        val claimed = synchronized(this) {
            val cur = _state.value
            if (cur.stage == UpdateStage.Checking || cur.stage == UpdateStage.Downloading) false
            else {
                _state.value = cur.copy(stage = UpdateStage.Checking, error = "", manualNotice = "")
                true
            }
        }
        if (!claimed) return
        try {
            val dto = ApiClient.api<UpdateLatestDto>(AppUpdate.LATEST_PATH)
            val release = AppUpdate.releaseOf(dto)
            if (release == null || !AppUpdate.isUpdateAvailable(_state.value.localVersionCode, release.versionCode)) {
                _state.value = _state.value.copy(
                    stage = UpdateStage.Idle,
                    release = null,
                    manualNotice = if (!manual) "" else dto.reason.ifEmpty { "已是最新版本" },
                )
                return
            }
            _state.value = _state.value.copy(release = release)

            val cached = UpdateDownloader.cachedPath(cacheDir(context), release.fileName)
            val force = AppUpdate.isForceUpdate(_state.value.localVersionCode, release.minVersionCode)
            val decision = decide(
                isUpdateAvailable = true,
                hasCachedPackage = cached != null,
                force = force,
                manual = manual,
                dismissedCode = DismissedUpdateStore.code(context),
                releaseCode = release.versionCode,
            )
            // 成品包始终记着：即使这次停在 Idle，设置页也要能给出「待安装」入口
            if (cached != null) cachedPath = cached
            when (decision) {
                Decision.Idle -> _state.value = _state.value.copy(stage = UpdateStage.Idle)
                Decision.Ready -> _state.value = _state.value.copy(
                    stage = UpdateStage.Ready,
                    received = release.sizeBytes,
                    total = release.sizeBytes,
                    sheetCollapsed = false,
                )
                Decision.Prompt -> _state.value = _state.value.copy(stage = UpdateStage.Prompt, sheetCollapsed = false)
            }
        } catch (e: Exception) {
            _state.value = _state.value.copy(
                stage = UpdateStage.Idle,
                manualNotice = if (manual) "检查失败：${errText(e, "网络异常")}" else "",
            )
        }
    }

    /**
     * 「稍后」：记住该版本码，同版本非强更不自动打扰。
     * 有成品包时**不清 release**——包已经下好了，只是用户现在不想装；
     * 清掉会让设置页显示「已是最新」，用户再也找不到那个已经下载好的包。
     */
    fun dismiss(context: Context) {
        val s = _state.value
        if (s.release != null && !s.force) DismissedUpdateStore.set(context, s.release.versionCode)
        // release 不动：包可能已经下好了，只是用户现在不想装（设置页要保留「待安装」入口）
        _state.value = s.copy(stage = UpdateStage.Idle, sheetCollapsed = false)
    }

    /** 成品包是否已在缓存（设置页据此把「已是最新」换成「vX 待安装」） */
    fun hasCachedPackage(): Boolean = cachedPath.isNotEmpty()

    /** 点「立即更新」：前台服务接管下载（本调用只负责起服务） */
    fun startDownload(context: Context) {
        val release = _state.value.release ?: return
        if (_state.value.stage == UpdateStage.Downloading) return
        _state.value = _state.value.copy(
            stage = UpdateStage.Downloading,
            sheetCollapsed = false,
            received = 0,
            total = release.sizeBytes,
            error = "",
        )
        // 复位必须发生在起服务之前（下载器自己不复位，否则窗口期内的取消会被吞掉）
        UpdateDownloader.resetCancel()
        UpdateService.start(context, release)
    }

    fun cancelDownload() {
        // 实际落态等服务回调 finished(cancelled)：这里不抢状态，防竞态误回 Idle
        UpdateDownloader.requestCancel()
    }

    /** 失败重试：重新取一次清单再续传——版本信息与下载地址都可能已变 */
    suspend fun retry(context: Context) {
        try {
            val dto = ApiClient.api<UpdateLatestDto>(AppUpdate.LATEST_PATH)
            val release = AppUpdate.releaseOf(dto)
            if (release == null) {
                _state.value = _state.value.copy(stage = UpdateStage.Failed, error = "服务端暂无已就绪的安装包")
                return
            }
            _state.value = _state.value.copy(release = release)
        } catch (e: Exception) {
            _state.value = _state.value.copy(error = errText(e, "网络异常"))
            return // 保持失败态，用户稍后可再试
        }
        startDownload(context)
    }

    /** 交给系统安装器；缺「未知来源」授权时置 needsPermission，前端在页面回来后重试 */
    fun install(context: Context) {
        val path = cachedPath.ifEmpty {
            _state.value.release?.let { UpdateDownloader.cachedPath(cacheDir(context), it.fileName) } ?: ""
        }
        if (path.isEmpty()) {
            _state.value = _state.value.copy(stage = UpdateStage.Failed, error = "安装包不存在，请重新下载")
            return
        }
        cachedPath = path
        when (val r = ApkInstaller.install(context, path)) {
            ApkInstaller.Result.NeedsPermission -> _state.value = _state.value.copy(needsPermission = true)
            ApkInstaller.Result.HandedOff -> _state.value = _state.value.copy(stage = UpdateStage.Handoff, needsPermission = false)
            is ApkInstaller.Result.Failed -> _state.value = _state.value.copy(stage = UpdateStage.Failed, error = r.message)
        }
    }

    /**
     * 从「安装未知应用」系统页回来：**只在授权已拿到时**自动重试交接。
     * 没拿到就保持就绪态（授权提示仍在），用户再点「立即安装」重走一遍——
     * 否则每次返回都被再弹去系统页，等于出不来。
     */
    fun onActivityResumed(context: Context) {
        val s = _state.value
        if (s.needsPermission && s.stage == UpdateStage.Ready && ApkInstaller.canInstall(context)) {
            _state.value = s.copy(needsPermission = false)
            install(context)
        }
    }

    /** 「后台下载」：只收起弹窗，状态机不动，下载在前台服务里继续 */
    fun collapseSheet() {
        _state.value = _state.value.copy(sheetCollapsed = true)
    }

    /** 设置页点「检查更新」时若已有进行中的会话，重新打开弹窗 */
    fun openSheet() {
        _state.value = _state.value.copy(sheetCollapsed = false)
    }

    fun closeSheet() {
        _state.value = _state.value.copy(stage = UpdateStage.Idle, sheetCollapsed = false)
    }

    fun clearNotice() {
        if (_state.value.manualNotice.isNotEmpty()) _state.value = _state.value.copy(manualNotice = "")
    }

    // ---- 下载服务回报（同进程内直接写状态） ----

    internal fun onProgress(received: Long, total: Long?) {
        // 迟到的进度不写非下载态（取消后仍可能有一帧在途，否则会把 Idle 的进度数字改掉）
        if (_state.value.stage != UpdateStage.Downloading) return
        _state.value = _state.value.copy(received = received, total = total ?: _state.value.total)
    }

    internal fun onFinished(result: UpdateDownloader.Result) {
        val s = _state.value
        _state.value = when {
            result.ok && result.path != null -> {
                cachedPath = result.path
                s.copy(stage = UpdateStage.Ready, received = s.total, error = "", sheetCollapsed = false)
            }
            result.cancelled -> s.copy(stage = UpdateStage.Idle, error = "")
            else -> s.copy(stage = UpdateStage.Failed, error = result.error ?: "下载失败")
        }
    }

    private fun cacheDir(context: Context): File = context.applicationContext.cacheDir
}
