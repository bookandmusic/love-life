package com.bookandmusic.lovelife.appnative.update

import android.os.StatFs
import com.bookandmusic.lovelife.appnative.data.ApiClient
import com.bookandmusic.lovelife.appnative.data.Channel
import com.bookandmusic.lovelife.appnative.data.SecureStore
import com.bookandmusic.lovelife.appnative.data.UpdateRelease
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.OkHttpClient
import okhttp3.Request
import java.io.File
import java.io.FileOutputStream
import java.time.Duration

/**
 * 应用更新下载引擎：OkHttp + Range 断点续传，写 `.part`，校验通过才 rename 成成品。
 *
 * 与业务请求同一地址与凭据（[Channel] 通道链 + Bearer 令牌）：服务端更新接口走登录鉴权，
 * 走登录鉴权，下载器必须带头；令牌中途过期时刷新一次再按 Range 续传（慢链路上 46MB 会跨过期点）。
 *
 * 完整性：服务端当前不下发摘要，按「最终字节数 == 服务端声明大小」校验；
 * 安装包身份由系统安装器的签名一致性把关。
 */
object UpdateDownloader {

    /** 下载结果：ok=false 时 [resumable] 表示 `.part` 已保留、重试可续传 */
    data class Result(
        val ok: Boolean,
        val path: String? = null,
        val error: String? = null,
        val resumable: Boolean = false,
        val cancelled: Boolean = false,
    )

    private val http: OkHttpClient by lazy {
        OkHttpClient.Builder()
            .connectTimeout(Duration.ofSeconds(15))
            .readTimeout(Duration.ofSeconds(30))
            .callTimeout(Duration.ZERO) // 长下载：总时长不设上限，靠读写超时判死
            .build()
    }

    /**
     * 取消标志。**不在 download() 开头复位**：起前台服务到协程真正开跑之间有可感窗口，
     * 用户在这窗口点了「取消」，复位会把那次取消吞掉——下载照跑完、还自己弹「已就绪」。
     * 复位只由发起方（UpdateController.startDownload）在起服务前做一次。
     */
    @Volatile
    private var cancelled = false

    fun requestCancel() {
        cancelled = true
    }

    fun resetCancel() {
        cancelled = false
    }

    /** 成品是否已在缓存（冷启动恢复：直接进就绪态，不重弹确认） */
    fun cachedPath(cacheDir: File, fileName: String): String? {
        val f = File(cacheDir, fileName)
        return if (f.exists() && f.length() > 0) f.absolutePath else null
    }

    /**
     * 下载到 `cacheDir/<fileName>`。[onProgress] 已节流（≥200ms 或整数百分比变化），
     * 通知栏与 UI 两个消费方共用同一份节流后的进度。
     */
    suspend fun download(
        cacheDir: File,
        release: UpdateRelease,
        onProgress: (received: Long, total: Long?) -> Unit,
    ): Result = withContext(Dispatchers.IO) {
        cachedPath(cacheDir, release.fileName)?.let { return@withContext Result(ok = true, path = it) }
        val part = File(cacheDir, "${release.fileName}.part")
        val done = File(cacheDir, release.fileName)
        try {
            runDownload(cacheDir, part, done, release, onProgress)
        } catch (e: kotlinx.coroutines.CancellationException) {
            throw e
        } catch (e: Exception) {
            // 断网等 IO 异常保留 .part 供用户点「重试」续传（只有取消与校验失败才删）。
            // 不做自动重连：真机验证表明续传路径本身正常，用户点一下「重试」即从断点继续；
            // 后台静默重连反而会让人不知道下载停过。
            Result(ok = false, error = friendlyError(e), resumable = true)
        }
    }

    /**
     * 续传状态机的纯决策：给定响应码 + 本地已下载长度，决定怎么落。
     * 抽出来是为了可单测——206/200/416 三个分支错任意一个都是「静默坏」（下载出损坏包或缺字节）。
     */
    internal enum class Action {
        /** 按 Range 追加写 */
        APPEND,

        /** 服务端忽略 Range（200）：半成品作废，从头写 */
        RESTART,

        /** 416 半成品越界：删掉重下 */
        RESET_AND_FAIL,

        /** 该状态码不该进到这里（由调用方处理 401/错误码） */
        OTHER,
    }

    /**
     * 异常 → 用户看得懂的中文。
     * **绝不透传 `e.message`**：OkHttp/Java 的原文是 `Software caused connection abort`、
     * `unexpected end of stream` 这类英文，直接出现在弹窗里既看不懂也像崩溃。
     * 原文留给日志（见 UpdateService 的 Log），界面只要一句人话。
     */
    internal fun friendlyError(e: Throwable): String {
        val name = e.javaClass.simpleName.lowercase()
        val msg = (e.message ?: "").lowercase()
        return when {
            e is java.net.UnknownHostException || "unable to resolve host" in msg -> "网络不可用，请检查连接"
            e is java.net.SocketTimeoutException || "timeout" in msg || "timed out" in msg -> "连接超时，请稍后重试"
            "connection abort" in msg || "software caused connection abort" in msg -> "网络连接被中断"
            e is java.io.EOFException || "unexpected end of stream" in msg -> "下载中断，请重试"
            "connection reset" in msg || "broken pipe" in msg || "connection refused" in msg -> "网络连接被中断"
            e is javax.net.ssl.SSLException || "ssl" in name || "certificate" in msg -> "安全连接失败，请检查网络环境"
            e is java.io.IOException -> "网络异常，请重试"
            else -> "下载失败，请重试"
        }
    }

    internal fun decide(code: Int, partLen: Long): Action = when {
        code == 416 -> Action.RESET_AND_FAIL
        code == 206 && partLen > 0 -> Action.APPEND
        code in 200..299 -> Action.RESTART
        else -> Action.OTHER
    }

    private suspend fun runDownload(
        cacheDir: File,
        part: File,
        done: File,
        release: UpdateRelease,
        onProgress: (Long, Long?) -> Unit,
    ): Result {
        var baseLen = if (part.exists()) part.length() else 0L
        var authRetried = false

        while (true) {
            Channel.ensureFresh()
            val request = Request.Builder()
                .url(Channel.base().trimEnd('/') + release.downloadPath)
                .apply {
                    ApiClient.applyAuth(this)
                    if (baseLen > 0) header("Range", "bytes=$baseLen-")
                }
                .build()

            val outcome = http.newCall(request).execute().use { res ->
                when {
                    res.code == 401 && !authRetried -> {
                        val seen = SecureStore.token() ?: ""
                        if (ApiClient.refreshOnce(seen)) {
                            authRetried = true
                            null // 刷新成功：外层 while 重来一次（Range 保留，等于续传）
                        } else {
                            ApiClient.clearAuth()
                            Result(ok = false, error = "登录已过期，请重新登录", resumable = true)
                        }
                    }

                    decide(res.code, baseLen) == Action.RESET_AND_FAIL -> {
                        // 半成品越界（本地比服务端文件还长）：删掉让用户重试即可恢复
                        part.delete()
                        Result(ok = false, error = "续传位置无效，请重试", resumable = false)
                    }

                    // 刷新成功却仍被拒：令牌确实不可用了，按业务口径清会话（.part 保留，重登后可续传）
                    res.code == 401 -> {
                        ApiClient.clearAuth()
                        Result(ok = false, error = "登录已过期，请重新登录", resumable = true)
                    }

                    res.code !in 200..299 ->
                        Result(ok = false, error = "服务端返回错误（${res.code}），请稍后重试", resumable = true)

                    else -> streamToPart(res, part, done, baseLen, release, onProgress)
                }
            }
            if (outcome != null) return outcome
            baseLen = if (part.exists()) part.length() else 0L
        }
    }

    private fun streamToPart(
        res: okhttp3.Response,
        part: File,
        done: File,
        baseLen: Long,
        release: UpdateRelease,
        onProgress: (Long, Long?) -> Unit,
    ): Result {
        val action = decide(res.code, baseLen)
        val resumed = action == Action.APPEND
        var start = baseLen
        if (action == Action.RESTART && start > 0) {
            // 服务端忽略 Range（200 = 全量下发）：半成品作废，从头写
            part.delete()
            start = 0
        }
        // OkHttp 5 的 body 非空（无内容也是空 body），无需兜空
        val body = res.body
        val total = body.contentLength().let { if (it > 0) it + start else null }

        // 磁盘预检：cache 分区放不下就尽早失败，别下到一半才报错。
        // 减去 start：.part 已有字节已经占掉了 available，再按全长要求会重复计一次，
        // 磁盘紧张时续传会被误判「空间不足」。
        val need = (total ?: release.sizeBytes) - start
        if (need > 0 && StatFs(part.parentFile!!.path).availableBytes < need) {
            return Result(ok = false, error = "磁盘空间不足", resumable = true)
        }

        body.byteStream().use { input ->
            FileOutputStream(part, resumed).use { out ->
                val buf = ByteArray(1 shl 16)
                var received = start
                var lastEmit = 0L
                var lastPct = -1L
                while (true) {
                    if (cancelled) {
                        out.flush()
                        part.delete()
                        return Result(ok = false, error = "已取消下载", cancelled = true)
                    }
                    val n = input.read(buf)
                    if (n < 0) break
                    out.write(buf, 0, n)
                    received += n
                    val now = System.currentTimeMillis()
                    val pct = total?.let { received * 100 / it } ?: -1L
                    // 节流：≥200ms 或整数百分比变化才推一次（通知与 UI 共用一个回调）
                    if (now - lastEmit >= 200 || pct > lastPct) {
                        onProgress(received, total ?: release.sizeBytes.takeIf { it > 0 })
                        lastEmit = now
                        lastPct = pct
                    }
                }
                out.flush()
            }
        }
        onProgress(part.length(), total ?: release.sizeBytes.takeIf { it > 0 })

        // 完整性：服务端声明大小与实际字节数必须一致（不等即截断）
        val expected = release.sizeBytes.takeIf { it > 0 } ?: total
        if (expected != null && expected > 0 && part.length() != expected) {
            part.delete()
            return Result(ok = false, error = "安装包不完整，请重试", resumable = false)
        }

        if (!part.renameTo(done)) {
            // 同目录 rename 失败极罕见（跨 fs 防御）：退化为拷贝后删半成品
            done.outputStream().use { o -> part.inputStream().use { it.copyTo(o) } }
            part.delete()
        }
        // 只留当前版本：其他版本的成品与半成品各 46MB，cache 不该攒着
        deleteStaleArtifacts(done.parentFile, release.fileName)
        return Result(ok = true, path = done.absolutePath)
    }

    /**
     * 清掉除 keepFileName 之外的安装包残留（成品与 `.part` 都算）。
     * 只认 release 的命名前缀，别的缓存文件一律不碰。
     */
    private fun deleteStaleArtifacts(dir: File?, keepFileName: String) {
        val files = dir?.listFiles() ?: return
        for (f in files) {
            val n = f.name
            val isApk = n.endsWith(".apk") || n.endsWith(".apk.part")
            if (isApk && n != keepFileName && n != "$keepFileName.part") runCatching { f.delete() }
        }
    }
}
