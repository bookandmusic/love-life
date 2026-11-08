package com.bookandmusic.lovelife.appnative.data

import android.content.Context
import androidx.core.content.edit
import com.bookandmusic.lovelife.appnative.designsystem.theme.AppAccent

/**
 * 本地外观偏好（深浅模式 + 强调色）。
 *
 * 存本地不存服务端：这是**个人设备偏好**（同一账号在另一台设备上可以有不同观感），
 * 与「输出风格」这类跨设备生效的个人设置不同（后者走 `/api/v1/me` 的 ai_style）。
 */
enum class AppThemeMode(val id: String, val label: String) {
    System("system", "跟随系统"),
    Light("light", "浅色"),
    Dark("dark", "深色"),
    ;

    companion object {
        fun byId(id: String?): AppThemeMode = entries.firstOrNull { it.id == id } ?: System
    }
}

object ThemePreferences {
    private const val FILE = "lovelife_theme"
    private const val KEY_MODE = "theme_mode"
    private const val KEY_ACCENT = "accent"
    private const val KEY_APP_MODE = "app_mode"

    private fun prefs(context: Context) =
        context.getSharedPreferences(FILE, Context.MODE_PRIVATE)

    fun mode(context: Context): AppThemeMode =
        AppThemeMode.byId(prefs(context).getString(KEY_MODE, null))

    fun setMode(context: Context, mode: AppThemeMode) {
        prefs(context).edit { putString(KEY_MODE, mode.id) }
    }

    /** 界面模式（classic | ai）：与外观同属本机偏好，重启后必须保持用户上次的选择 */
    fun appMode(context: Context): String =
        prefs(context).getString(KEY_APP_MODE, null).takeIf { it == "ai" || it == "classic" } ?: "classic"

    fun setAppMode(context: Context, mode: String) {
        prefs(context).edit { putString(KEY_APP_MODE, mode) }
    }

    fun accent(context: Context): AppAccent =
        AppAccent.byId(prefs(context).getString(KEY_ACCENT, null))

    fun setAccent(context: Context, accent: AppAccent) {
        prefs(context).edit { putString(KEY_ACCENT, accent.id) }
    }
}
