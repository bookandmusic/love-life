package com.bookandmusic.lovelife.appnative.update

import android.app.NotificationManager
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import com.bookandmusic.lovelife.appnative.data.UpdateRelease
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch

/**
 * 下载保活前台服务（`dataSync` 类型）：持有进程重要性，避免下载被系统缓存冻结打断，
 * 同时把进度推给状态栏通知与 [UpdateController]（同进程，无需跨进程通道）。
 *
 * 完成后摘掉前台通知、另发一条完成通知——点击拉回 APP 由用户点「立即安装」交接，
 * 不用安装 Intent 直拉（绕过「未知来源」检查的路径不可控）。
 */
class UpdateService : Service() {

    companion object {
        private const val EXTRA_PATH = "download_path"
        private const val EXTRA_NAME = "asset_name"
        private const val EXTRA_VERSION = "version_name"
        private const val EXTRA_CODE = "version_code"
        private const val EXTRA_SIZE = "asset_size"
        private const val EXTRA_MIN = "min_version_code"
        private const val EXTRA_NOTES = "notes"

        fun start(context: Context, release: UpdateRelease) {
            val intent = Intent(context, UpdateService::class.java)
                .putExtra(EXTRA_PATH, release.downloadPath)
                .putExtra(EXTRA_NAME, release.fileName)
                .putExtra(EXTRA_VERSION, release.versionName)
                .putExtra(EXTRA_CODE, release.versionCode)
                .putExtra(EXTRA_SIZE, release.sizeBytes)
                .putExtra(EXTRA_MIN, release.minVersionCode)
                .putExtra(EXTRA_NOTES, release.notes.joinToString("\n"))
            ContextCompat.startForegroundService(context, intent)
        }

        private fun releaseOf(intent: Intent) = UpdateRelease(
            versionName = intent.getStringExtra(EXTRA_VERSION) ?: "",
            versionCode = intent.getIntExtra(EXTRA_CODE, 0),
            fileName = intent.getStringExtra(EXTRA_NAME) ?: "",
            downloadPath = intent.getStringExtra(EXTRA_PATH) ?: "",
            notes = (intent.getStringExtra(EXTRA_NOTES) ?: "").split("\n").filter { it.isNotEmpty() },
            minVersionCode = intent.getIntExtra(EXTRA_MIN, 0),
            sizeBytes = intent.getLongExtra(EXTRA_SIZE, 0),
        )
    }

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private var job: Job? = null

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent == null) {
            stopSelf()
            return START_NOT_STICKY
        }
        // 单飞：下载线程存活期间忽略重复启动（UI 标志在进程重启后会丢，这里兜底）
        if (job?.isActive == true) return START_NOT_STICKY

        val release = releaseOf(intent)
        UpdateNotifications.ensureChannels(this)
        ServiceCompat.startForeground(
            this,
            UpdateNotifications.ID_PROGRESS,
            UpdateNotifications.progress(this, release.versionName, 0L, release.sizeBytes.takeIf { it > 0 }),
            // 类型常量是 API 29 才有的：低版本 ServiceCompat 会忽略该参数，显式给 0 免得读 29 的字段
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
                ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC
            } else {
                0
            },
        )

        job = scope.launch {
            val result = UpdateDownloader.download(cacheDir, release) { received, total ->
                UpdateController.onProgress(received, total)
                // 进度回调本身已按 ≥200ms / 整数百分比节流，通知直接用
                val nm = getSystemService(NOTIFICATION_SERVICE) as NotificationManager
                nm.notify(
                    UpdateNotifications.ID_PROGRESS,
                    UpdateNotifications.progress(this@UpdateService, release.versionName, received, total),
                )
            }
            UpdateController.onFinished(result)
            val nm = getSystemService(NOTIFICATION_SERVICE) as NotificationManager
            if (result.ok) {
                ServiceCompat.stopForeground(this@UpdateService, ServiceCompat.STOP_FOREGROUND_REMOVE)
                nm.notify(UpdateNotifications.ID_DONE, UpdateNotifications.done(this@UpdateService, release.versionName))
            } else {
                nm.cancel(UpdateNotifications.ID_PROGRESS)
            }
            stopSelf()
        }
        // 服务被销毁而进程还活着时，协程会被 cancel 吞掉结果（onFinished 不执行），
        // 界面就永远停在「下载中」——完成回调里补一次落态与通知清理。
        job?.invokeOnCompletion { cause ->
            if (cause != null) {
                UpdateDownloader.requestCancel() // 保留 .part，下次点「立即更新」自动续传
                UpdateController.onFinished(
                    UpdateDownloader.Result(ok = false, error = "下载已中断", resumable = true),
                )
                (getSystemService(NOTIFICATION_SERVICE) as NotificationManager)
                    .cancel(UpdateNotifications.ID_PROGRESS)
            }
        }
        return START_NOT_STICKY
    }

    override fun onDestroy() {
        scope.cancel()
        super.onDestroy()
    }
}
