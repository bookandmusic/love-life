package com.bookandmusic.lovelife.appnative.update

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.core.net.toUri
import android.provider.Settings
import androidx.core.content.FileProvider
import java.io.File

/**
 * 安装交接：把下载好的 APK 交给系统安装器。
 *
 * 两道系统把关：①「安装未知应用」授权（Android 8+ 按来源应用授权，缺失时引导到系统页，
 * 用户返回后由 [UpdateController.onActivityResumed] 自动重试）；②安装器签名一致性校验——
 * 同包名异签名无法覆盖安装，这是更新链路的身份防线。
 */
object ApkInstaller {

    sealed interface Result {
        /** 缺授权：已跳系统授权页，等用户回来重试 */
        data object NeedsPermission : Result

        /** 已交给系统安装器 */
        data object HandedOff : Result

        data class Failed(val message: String) : Result
    }

    private const val MIME_APK = "application/vnd.android.package-archive"

    /** 是否已获得「安装未知应用」授权（Android 8+ 按来源应用授权） */
    fun canInstall(context: Context): Boolean = context.packageManager.canRequestPackageInstalls()

    fun install(context: Context, apkPath: String): Result {
        val file = File(apkPath)
        if (!file.exists() || file.length() <= 0) return Result.Failed("安装包不存在，请重新下载")

        if (!canInstall(context)) {
            return try {
                context.startActivity(
                    Intent(
                        Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES,
                        "package:${context.packageName}".toUri(),
                    ).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
                )
                Result.NeedsPermission
            } catch (e: Exception) {
                Result.Failed("无法打开「安装未知应用」设置：${e.message ?: "系统页不可用"}")
            }
        }

        val uri = try {
            FileProvider.getUriForFile(context, "${context.packageName}.fileprovider", file)
        } catch (e: Exception) {
            return Result.Failed("安装包授权失败：${e.message ?: "FileProvider 未配置"}")
        }

        // ACTION_INSTALL_PACKAGE 自 API 29 起标记过时（官方推荐 PackageInstaller 会话），
        // 但系统安装器至今仍处理它，且不必把 49MB 再拷贝一份进会话。
        //
        // 探测手段用「先发再兜」而不是 resolveActivity：后者在 API 30+ 受包可见性限制，
        // 没有 <queries> 声明时恒返回 null——探测永远走不到真分支，等于白写。
        val flags = Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK
        return try {
            context.startActivity(
                Intent(Intent.ACTION_INSTALL_PACKAGE).setDataAndType(uri, MIME_APK).addFlags(flags),
            )
            Result.HandedOff
        } catch (_: ActivityNotFoundException) {
            // 个别 ROM 没有该 action 的处理者：退回等价的 ACTION_VIEW（同一 MIME）
            try {
                context.startActivity(
                    Intent(Intent.ACTION_VIEW).setDataAndType(uri, MIME_APK).addFlags(flags),
                )
                Result.HandedOff
            } catch (e: Exception) {
                Result.Failed("无法打开安装器：${e.message ?: "系统无处理应用"}")
            }
        } catch (e: Exception) {
            Result.Failed("无法打开安装器：${e.message ?: "系统无处理应用"}")
        }
    }
}