package com.bookandmusic.lovelife

import android.app.Activity
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import androidx.core.app.ActivityCompat
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat
import androidx.core.content.FileProvider
import app.tauri.annotation.Command
import app.tauri.annotation.InvokeArg
import app.tauri.annotation.TauriPlugin
import app.tauri.plugin.Channel
import app.tauri.plugin.Invoke
import app.tauri.plugin.JSObject
import app.tauri.plugin.Plugin
import java.io.File

// 应用内更新的 Kotlin 侧入口：启动下载（extras 进前台服务）、缓存查询、安装器交接、版本码。
// 下载引擎与通知生命周期在 UpdateDownloadService；进度经 Channel 直达前端（不经 ACL）。
@TauriPlugin
class AppUpdatePlugin(private val activity: Activity) : Plugin(activity) {

    @InvokeArg
    class StartDownloadArgs {
        lateinit var url: String
        var expectedSha256: String? = null
        lateinit var fileName: String
        lateinit var versionName: String
        var onProgress: Channel? = null
    }

    @InvokeArg
    class FileNameArgs {
        lateinit var fileName: String
    }

    @InvokeArg
    class PathArgs {
        lateinit var path: String
    }

    companion object {
        const val CHANNEL_PROGRESS = "appupdate_progress"
        const val CHANNEL_DONE = "appupdate_done"
        const val NOTIFICATION_ID = 4242
        /** 完成通知独立 ID：前台通知被 stopForeground(REMOVE) 撤销时不会连带完成通知 */
        const val DONE_NOTIFICATION_ID = 4243
        private const val REQ_POST_NOTIFICATIONS = 4243

        /** 服务与前端之间的进度/结果通道（单一下载会话，服务取走后置空） */
        @Volatile
        var pendingChannel: Channel? = null

        fun launchIntent(context: Context): PendingIntent? {
            val intent = context.packageManager.getLaunchIntentForPackage(context.packageName)
                ?: return null
            return PendingIntent.getActivity(
                context, 0, intent,
                PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
            )
        }

        fun progressNotification(
            context: Context, versionName: String, received: Long, total: Long?
        ): Notification {
            val pct = if (total != null && total > 0) (received * 100 / total).toInt() else 0
            return NotificationCompat.Builder(context, CHANNEL_PROGRESS)
                .setSmallIcon(android.R.drawable.stat_sys_download)
                .setContentTitle("正在下载更新 v$versionName")
                .setContentText(if (total != null && total > 0) "$pct%" else "准备中…")
                .setProgress(100, pct, total == null || total <= 0)
                .setOngoing(true)
                .setOnlyAlertOnce(true)
                .setContentIntent(launchIntent(context))
                .build()
        }

        fun doneNotification(context: Context, versionName: String): Notification =
            NotificationCompat.Builder(context, CHANNEL_DONE)
                .setSmallIcon(android.R.drawable.stat_sys_download_done)
                .setContentTitle("更新已就绪 v$versionName")
                .setContentText("点击打开应用安装")
                .setOngoing(true)
                .setAutoCancel(true)
                .setContentIntent(launchIntent(context))
                .build()

        fun ensureChannels(context: Context) {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                val nm =
                    context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
                if (nm.getNotificationChannel(CHANNEL_PROGRESS) == null) {
                    nm.createNotificationChannel(
                        NotificationChannel(
                            CHANNEL_PROGRESS, "应用更新（下载中）",
                            NotificationManager.IMPORTANCE_LOW
                        )
                    )
                }
                if (nm.getNotificationChannel(CHANNEL_DONE) == null) {
                    nm.createNotificationChannel(
                        NotificationChannel(
                            CHANNEL_DONE, "应用更新（完成）",
                            NotificationManager.IMPORTANCE_DEFAULT
                        )
                    )
                }
            }
        }
    }

    private fun requestNotificationPermissionIfNeeded() {
        if (Build.VERSION.SDK_INT >= 33 &&
            ActivityCompat.checkSelfPermission(
                activity, "android.permission.POST_NOTIFICATIONS"
            ) != PackageManager.PERMISSION_GRANTED
        ) {
            // 拒绝也不阻塞：通知不展示，前台服务照常保活，APP 内仍有进度
            ActivityCompat.requestPermissions(
                activity, arrayOf("android.permission.POST_NOTIFICATIONS"), REQ_POST_NOTIFICATIONS
            )
        }
    }

    @Command
    fun startDownload(invoke: Invoke) {
        val args = invoke.parseArgs(StartDownloadArgs::class.java)
        ensureChannels(activity)
        requestNotificationPermissionIfNeeded()
        pendingChannel = args.onProgress
        val intent = Intent(activity, UpdateDownloadService::class.java)
            .putExtra(UpdateDownloadService.EXTRA_URL, args.url)
            .putExtra(UpdateDownloadService.EXTRA_SHA256, args.expectedSha256 ?: "")
            .putExtra(UpdateDownloadService.EXTRA_FILE_NAME, args.fileName)
            .putExtra(UpdateDownloadService.EXTRA_VERSION, args.versionName)
        ContextCompat.startForegroundService(activity, intent)
        invoke.resolve()
    }

    @Command
    fun cancelDownload(invoke: Invoke) {
        UpdateDownloadService.requestCancel()
        invoke.resolve()
    }

    @Command
    fun hasCached(invoke: Invoke) {
        val args = invoke.parseArgs(FileNameArgs::class.java)
        val f = File(activity.cacheDir, args.fileName)
        val ret = JSObject()
        if (f.exists() && f.length() > 0) ret.put("path", f.absolutePath) else ret.put("path", null)
        invoke.resolve(ret)
    }

    @Command
    fun getVersionCode(invoke: Invoke) {
        val pm = activity.packageManager
        val info = pm.getPackageInfo(activity.packageName, 0)
        val code =
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) info.longVersionCode
            else @Suppress("DEPRECATION") info.versionCode.toLong()
        val ret = JSObject()
        ret.put("versionCode", code)
        invoke.resolve(ret)
    }

    @Command
    fun installApk(invoke: Invoke) {
        try {
            val args = invoke.parseArgs(PathArgs::class.java)
            val file = File(args.path)
            if (!file.exists()) {
                invoke.reject("安装包不存在")
                return
            }
            // Android 8+ 需「安装未知应用」授权；缺授权时引导系统页，前端在用户返回后重试
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O &&
                !activity.packageManager.canRequestPackageInstalls()
            ) {
                activity.startActivity(
                    Intent(
                        android.provider.Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES,
                        Uri.parse("package:${activity.packageName}")
                    )
                )
                val ret = JSObject()
                ret.put("needsPermission", true)
                invoke.resolve(ret)
                return
            }
            val uri = FileProvider.getUriForFile(
                activity, "${activity.packageName}.fileprovider", file
            )
            val intent = Intent(Intent.ACTION_INSTALL_PACKAGE)
                .setDataAndType(uri, "application/vnd.android.package-archive")
                .addFlags(
                    Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK
                )
            activity.startActivity(intent)
            val ret = JSObject()
            ret.put("needsPermission", false)
            invoke.resolve(ret)
        } catch (e: Exception) {
            invoke.reject(e.message ?: "安装交接失败")
        }
    }
}
