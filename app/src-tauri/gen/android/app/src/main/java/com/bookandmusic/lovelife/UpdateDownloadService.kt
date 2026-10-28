package com.bookandmusic.lovelife

import android.app.NotificationManager
import android.app.Service
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.IBinder
import android.os.StatFs
import androidx.core.app.ServiceCompat
import app.tauri.plugin.Channel
import app.tauri.plugin.JSObject
import java.io.File
import java.io.FileInputStream
import java.io.FileOutputStream
import java.net.HttpURLConnection
import java.net.URL
import java.security.MessageDigest

// 下载保活前台服务 + 下载引擎（HttpURLConnection + Range 断点续传 + MessageDigest sha256）。
// 进度与结果经 Channel 直达前端（不经全局事件）；取消标志静态共享。
class UpdateDownloadService : Service() {

    companion object {
        const val EXTRA_URL = "url"
        const val EXTRA_SHA256 = "sha256"
        const val EXTRA_FILE_NAME = "fileName"
        const val EXTRA_VERSION = "versionName"

        const val EVENT_PROGRESS = "progress"
        const val EVENT_FINISHED = "finished"

        @Volatile
        private var cancelled = false

        /** 单飞防护：下载线程存活期间忽略重复的 startDownload（前端标志在 WebView 重载后会丢） */
        @Volatile
        private var worker: Thread? = null

        fun requestCancel() {
            cancelled = true
        }
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent == null) {
            stopSelf()
            return START_NOT_STICKY
        }
        if (worker?.isAlive == true) {
            return START_NOT_STICKY
        }
        val versionName = intent.getStringExtra(EXTRA_VERSION) ?: ""
        // Android 14+ 要求显式声明 dataSync 类型（manifest 已同步声明）
        ServiceCompat.startForeground(
            this,
            AppUpdatePlugin.NOTIFICATION_ID,
            AppUpdatePlugin.progressNotification(this, versionName, 0L, null),
            ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC
        )
        cancelled = false
        val channel = AppUpdatePlugin.pendingChannel
        AppUpdatePlugin.pendingChannel = null
        worker = Thread { runDownload(intent, channel) }.also { it.start() }
        return START_NOT_STICKY
    }

    private fun sendProgress(channel: Channel?, received: Long, total: Long?, versionName: String) {
        val obj = JSObject()
        obj.put("type", EVENT_PROGRESS)
        obj.put("received", received.toDouble())
        obj.put("total", total?.toDouble())
        obj.put("versionName", versionName)
        try {
            channel?.send(obj)
        } catch (_: Exception) {
            // 进度送达失败不影响下载主流程（通知栏仍有进度；前端下次进度点会继续收到）
        }
    }

    private fun sendFinished(
        channel: Channel?, ok: Boolean, path: String?, error: String?,
        resumable: Boolean, cancelledFlag: Boolean, versionName: String
    ) {
        val obj = JSObject()
        obj.put("type", EVENT_FINISHED)
        obj.put("ok", ok)
        obj.put("path", path)
        obj.put("error", error)
        obj.put("resumable", resumable)
        obj.put("cancelled", cancelledFlag)
        obj.put("versionName", versionName)
        try {
            channel?.send(obj)
        } catch (_: Exception) {
            // 结果送达失败：冷启动 has_cached 会兜底发现成品，前端仍能进就绪态
        }
    }

    private fun updateNotification(versionName: String, received: Long, total: Long?) {
        val nm = getSystemService(NOTIFICATION_SERVICE) as NotificationManager
        nm.notify(
            AppUpdatePlugin.NOTIFICATION_ID,
            AppUpdatePlugin.progressNotification(this, versionName, received, total)
        )
    }

    private fun runDownload(intent: Intent, channel: Channel?) {
        val url = intent.getStringExtra(EXTRA_URL) ?: ""
        val expectedSha = intent.getStringExtra(EXTRA_SHA256) ?: ""
        val fileName = intent.getStringExtra(EXTRA_FILE_NAME) ?: ""
        val versionName = intent.getStringExtra(EXTRA_VERSION) ?: ""
        val nm = getSystemService(NOTIFICATION_SERVICE) as NotificationManager

        try {
            val dir = cacheDir
            val part = File(dir, "$fileName.part")
            val done = File(dir, fileName)

            if (done.exists() && done.length() > 0) {
                sendFinished(channel, true, done.absolutePath, null, false, false, versionName)
                stopSelf()
                return
            }

            val partLen = if (part.exists()) part.length() else 0L

            val conn = URL(url).openConnection() as HttpURLConnection
            conn.connectTimeout = 15_000
            conn.readTimeout = 30_000
            conn.instanceFollowRedirects = true
            if (partLen > 0) conn.setRequestProperty("Range", "bytes=$partLen-")
            conn.connect()
            val code = conn.responseCode
            val resumed = code == 206 && partLen > 0
            if (code !in 200..299) {
                conn.disconnect()
                // 416 = 半成品越界：删掉让用户重试即可恢复
                if (code == 416) part.delete()
                sendFinished(
                    channel, false, null, "HTTP $code",
                    resumable = code == 416, cancelledFlag = false, versionName = versionName
                )
                nm.cancel(AppUpdatePlugin.NOTIFICATION_ID)
                stopSelf()
                return
            }
            val baseLen = if (resumed) partLen else 0L
            if (!resumed && partLen > 0) part.delete()
            val total = conn.contentLengthLong.let { if (it > 0) it + baseLen else null }

            // 磁盘预检：cache 分区剩余空间不足时尽早失败（StatFs 以 cacheDir 所在分区为准）
            if (total != null) {
                val available = StatFs(dir.path).availableBytes
                if (available < total) {
                    conn.disconnect()
                    sendFinished(
                        channel, false, null, "磁盘空间不足",
                        resumable = true, cancelledFlag = false, versionName = versionName
                    )
                    nm.cancel(AppUpdatePlugin.NOTIFICATION_ID)
                    stopSelf()
                    return
                }
            }

            // sha256 覆盖完整文件：续传时把已有字节回灌进 digest（磁盘顺序读一遍）
            val digest = MessageDigest.getInstance("SHA-256")
            if (resumed) {
                FileInputStream(part).use { input ->
                    val buf = ByteArray(1 shl 16)
                    while (true) {
                        val n = input.read(buf)
                        if (n < 0) break
                        digest.update(buf, 0, n)
                    }
                }
            }

            conn.inputStream.use { input ->
                FileOutputStream(part, resumed).use { out ->
                    val buf = ByteArray(1 shl 16)
                    var received = baseLen
                    var lastEmit = 0L
                    var lastPct = -1L
                    while (true) {
                        if (cancelled) {
                            out.flush()
                            part.delete()
                            sendFinished(
                                channel, false, null, "已取消下载",
                                resumable = false, cancelledFlag = true, versionName = versionName
                            )
                            nm.cancel(AppUpdatePlugin.NOTIFICATION_ID)
                            stopSelf()
                            return
                        }
                        val n = input.read(buf)
                        if (n < 0) break
                        digest.update(buf, 0, n)
                        out.write(buf, 0, n)
                        received += n
                        val now = System.currentTimeMillis()
                        val pct = total?.let { received * 100 / it } ?: -1L
                        // 节流：≥200ms 或整数百分比 ≥1 才更新通知与前端
                        if (now - lastEmit >= 200 || pct > lastPct) {
                            updateNotification(versionName, received, total)
                            sendProgress(channel, received, total, versionName)
                            lastEmit = now
                            lastPct = pct
                        }
                    }
                    out.flush()
                }
            }
            conn.disconnect()

            val actual = digest.digest().joinToString("") { "%02x".format(it) }
            val expected = expectedSha.trim()
                .removePrefix("sha256:")
                .trim()
                .lowercase()
            if (expected.isNotEmpty() && actual != expected) {
                part.delete()
                sendFinished(
                    channel, false, null, "文件校验失败（sha256 不符）",
                    resumable = false, cancelledFlag = false, versionName = versionName
                )
                nm.cancel(AppUpdatePlugin.NOTIFICATION_ID)
                stopSelf()
                return
            }

            if (!part.renameTo(done)) {
                // 同目录 rename 失败极罕见（跨 fs 防御）：退化为拷贝删除
                done.outputStream().use { o -> part.inputStream().use { it.copyTo(o) } }
                part.delete()
            }

            // 先摘掉前台通知（REMOVE 会撤销同 ID 的常驻通知），完成通知用独立 ID 免被连带撤销
            ServiceCompat.stopForeground(this, ServiceCompat.STOP_FOREGROUND_REMOVE)
            nm.notify(
                AppUpdatePlugin.DONE_NOTIFICATION_ID,
                AppUpdatePlugin.doneNotification(this, versionName)
            )
            sendFinished(channel, true, done.absolutePath, null, false, false, versionName)
            stopSelf()
        } catch (e: Exception) {
            // 断网等 IO 异常：保留 .part 供续传
            sendFinished(
                channel, false, null, e.message ?: "下载失败",
                resumable = true, cancelledFlag = false, versionName = versionName
            )
            nm.cancel(AppUpdatePlugin.NOTIFICATION_ID)
            stopSelf()
        }
    }
}
