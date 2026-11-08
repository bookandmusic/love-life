package com.bookandmusic.lovelife.appnative.data

import android.content.Context
import androidx.core.content.edit
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * 应用内更新：清单解析、版本比较与「稍后」记忆（纯逻辑，可单测）。
 *
 * 链路：APP 只连自己的服务端（与业务请求同一 base），
 * 服务端从 GitHub 取回并缓存安装包；客户端能看到的只能是**服务端已就绪**的版本——
 * `available:false` 是「暂无已就绪的包」，按无需更新处理，不是错误。
 *
 * 下载与安装（前台服务、断点续传、安装交接）在 `update/` 下，本文件不碰 Android 组件。
 */

@Serializable
data class UpdateLatestDto(
    val available: Boolean = false,
    /** 无就绪包时服务端给的原因（仅展示，不是错误） */
    val reason: String = "",
    @SerialName("version_name") val versionName: String = "",
    @SerialName("version_code") val versionCode: Int = 0,
    val notes: String = "",
    @SerialName("min_version_code") val minVersionCode: Int = 0,
    @SerialName("asset_name") val assetName: String = "",
    @SerialName("asset_size") val assetSize: Long = 0,
    /** 相对路径：调用处拼当前 base，下载因此与业务请求走同一地址（含绿联通道） */
    @SerialName("download_path") val downloadPath: String = "",
)

/** 一个可下载的版本 */
data class UpdateRelease(
    val versionName: String,
    val versionCode: Int,
    /** APK 文件名：缓存文件名与冷启动恢复都按它找成品 */
    val fileName: String,
    val downloadPath: String,
    val notes: List<String>,
    /** 本机 versionCode 低于它即强更（0 = 未标注） */
    val minVersionCode: Int,
    val sizeBytes: Long,
)

object AppUpdate {
    const val LATEST_PATH = "/api/v1/app/update/latest"

    /** 服务端响应 → 可下载版本；`available:false` 返回 null（视为无需更新） */
    fun releaseOf(dto: UpdateLatestDto): UpdateRelease? {
        if (!dto.available) return null
        require(dto.versionName.isNotEmpty() && dto.versionCode > 0) { "更新清单缺少版本信息" }
        require(dto.assetName.isNotEmpty() && dto.downloadPath.isNotEmpty()) { "更新清单缺少下载地址" }
        return UpdateRelease(
            versionName = dto.versionName,
            versionCode = dto.versionCode,
            fileName = dto.assetName,
            downloadPath = dto.downloadPath,
            notes = noteLines(dto.notes),
            minVersionCode = dto.minVersionCode,
            sizeBytes = dto.assetSize,
        )
    }

    /** 版本比较只用 versionCode 整数（与 APK 内写死的值同量纲） */
    fun isUpdateAvailable(localCode: Int, latestCode: Int): Boolean = latestCode > localCode

    /**
     * 强更 = 本机低于最低可用版本。**提示级而非准入级**：断网启动可绕过，
     * 服务端不做准入拦截。
     */
    fun isForceUpdate(localCode: Int, minVersionCode: Int): Boolean =
        minVersionCode > 0 && localCode < minVersionCode

    /**
     * release 说明按行拆成展示用列表。
     * 服务端把 release body 原样下发，`min-version-code:` 是发布约定而非更新说明，要滤掉。
     */
    fun noteLines(notes: String): List<String> =
        notes.split("\n")
            .map { it.trim() }
            .filter { it.isNotEmpty() && !it.lowercase().startsWith("min-version-code:") }

    /** 小数点钉成 `.`：设备语言用逗号分隔（de/fr 等）时体积文案会跳字，单测也会因 locale 飘 */
    fun humanBytes(n: Long): String = when {
        n <= 0 -> "—"
        n < 1024 -> "$n B"
        n < 1024 * 1024 -> "${n / 1024} KB"
        else -> String.format(java.util.Locale.US, "%.1f MB", n / 1024.0 / 1024.0)
    }
}

/**
 * 「稍后」记忆：同版本不自动打扰，手动检查不受限；强更版本不写记忆。
 * 存 SharedPreferences 不存服务端——这是**本机设备状态**（换台设备仍该看到更新提示）。
 */
object DismissedUpdateStore {
    private const val FILE = "lovelife_update"
    private const val KEY_CODE = "dismissed_version_code"

    private fun prefs(context: Context) = context.getSharedPreferences(FILE, Context.MODE_PRIVATE)

    fun code(context: Context): Int = prefs(context).getInt(KEY_CODE, 0)

    fun set(context: Context, code: Int) {
        prefs(context).edit { putInt(KEY_CODE, code) }
    }
}
