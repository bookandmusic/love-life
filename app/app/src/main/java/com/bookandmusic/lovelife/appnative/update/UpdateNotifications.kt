package com.bookandmusic.lovelife.appnative.update

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import androidx.core.app.NotificationCompat

/**
 * 应用更新的两条通知：
 * 下载中（低优先级、常驻、带进度）与完成（默认优先级，点开回 APP 进就绪态）。
 *
 * 权限：Android 13+ 的 `POST_NOTIFICATIONS` 被拒只影响通知展示，不影响下载
 * （前台服务仍保活、APP 内仍有进度）。
 */
object UpdateNotifications {
    const val CHANNEL_PROGRESS = "appupdate_progress"
    const val CHANNEL_DONE = "appupdate_done"

    const val ID_PROGRESS = 4242

    /** 完成通知独立 ID：前台通知被 stopForeground(REMOVE) 撤销时不会连带完成通知 */
    const val ID_DONE = 4243

    fun ensureChannels(context: Context) {
        val nm = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        if (nm.getNotificationChannel(CHANNEL_PROGRESS) == null) {
            nm.createNotificationChannel(
                NotificationChannel(CHANNEL_PROGRESS, "应用更新（下载中）", NotificationManager.IMPORTANCE_LOW),
            )
        }
        if (nm.getNotificationChannel(CHANNEL_DONE) == null) {
            nm.createNotificationChannel(
                NotificationChannel(CHANNEL_DONE, "应用更新（完成）", NotificationManager.IMPORTANCE_DEFAULT),
            )
        }
    }

    fun progress(context: Context, versionName: String, received: Long, total: Long?): Notification =
        NotificationCompat.Builder(context, CHANNEL_PROGRESS)
            .setSmallIcon(android.R.drawable.stat_sys_download)
            .setContentTitle("正在下载更新 v$versionName")
            .setContentText(if (total != null && total > 0) "${percent(received, total)}%" else "准备中…")
            .setProgress(100, percent(received, total), total == null || total <= 0)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setContentIntent(launchIntent(context))
            .build()

    fun done(context: Context, versionName: String): Notification =
        NotificationCompat.Builder(context, CHANNEL_DONE)
            .setSmallIcon(android.R.drawable.stat_sys_download_done)
            .setContentTitle("更新已就绪 v$versionName")
            .setContentText("点击打开应用安装")
            // 不设 ongoing：ongoing 与 autoCancel 语义冲突（常驻却又称可点掉）——
            // 这条就该是普通通知，点一次即消失
            .setAutoCancel(true)
            .setContentIntent(launchIntent(context))
            .build()

    /** 进度通知与完成通知走同一个落点：拉回 APP，由就绪态的「立即安装」做交接 */
    private fun launchIntent(context: Context): PendingIntent? {
        val intent = context.packageManager.getLaunchIntentForPackage(context.packageName) ?: return null
        return PendingIntent.getActivity(
            context, 0, intent,
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
    }

    private fun percent(received: Long, total: Long?): Int =
        if (total != null && total > 0) ((received * 100 / total).coerceIn(0, 100)).toInt() else 0
}
