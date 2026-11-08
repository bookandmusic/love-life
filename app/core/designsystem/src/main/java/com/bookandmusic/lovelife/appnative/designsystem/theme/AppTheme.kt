package com.bookandmusic.lovelife.appnative.designsystem.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp

@Immutable
data class AppSpacing(
    val md: Dp = 12.dp,
    val lg: Dp = 16.dp,
    val xxl: Dp = 32.dp,
)

val LocalAppSpacing = staticCompositionLocalOf { AppSpacing() }

/**
 * 主题入口：画布 = iOS 中性色（浅 #F2F2F7 / 深 #000000），卡面 = 纯白 / #1C1C1E。
 * 不用全屏氛围渐变——温度只来自主色与头卡渐变。
 */
@Composable
fun AppTheme(
    darkTheme: Boolean = isSystemInDarkTheme(),
    /** 标题字体（品牌字体由 :app 注入；null = 系统字体） */
    titleFont: androidx.compose.ui.text.font.FontFamily? = null,
    /** 强调色（个人偏好，null = 默认暖粉）。只替换 primary 家族，其余语义色不动 */
    accent: androidx.compose.ui.graphics.Color? = null,
    content: @Composable () -> Unit,
) {
    var scheme = if (darkTheme) AppPalette.Dark else AppPalette.Light
    if (accent != null) {
        scheme = scheme.copy(
            primary = accent,
            onPrimary = androidx.compose.ui.graphics.Color.White,
            primaryContainer = accent.copy(alpha = 0.22f),
            onPrimaryContainer = scheme.onPrimaryContainer,
        )
    }
    val colors = if (darkTheme) DarkAppColors else LightAppColors
    CompositionLocalProvider(
        LocalAppSpacing provides AppSpacing(),
        LocalAppColors provides colors,
        LocalAppTitleFont provides titleFont,
        LocalAppDark provides darkTheme,
    ) {
        MaterialTheme(
            colorScheme = scheme,
            typography = AppTypography,
            shapes = AppShapes,
            content = content,
        )
    }
}
