package com.bookandmusic.lovelife.appnative.designsystem.theme

import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Color

/**
 * iOS 中性画布 + 单一暖粉强调色：
 * 画布 #F2F2F7 / 卡面纯白 / 正文 #1C1C1E，温度只来自主色与头卡渐变。
 */
internal object AppPalette {

    val Light = lightColorScheme(
        primary = Color(0xFFFF8FAB),
        onPrimary = Color.White,
        primaryContainer = Color(0xFFFFE3EA),
        onPrimaryContainer = Color(0xFF5C1A2B),
        secondary = Color(0xFF5AA7D4),
        onSecondary = Color.White,
        secondaryContainer = Color(0xFFE3F1F9),
        onSecondaryContainer = Color(0xFF123A52),
        tertiary = Color(0xFFE8A800),
        onTertiary = Color.White,
        tertiaryContainer = Color(0xFFFFF1CC),
        onTertiaryContainer = Color(0xFF4A3600),
        error = Color(0xFFFF3B30),
        onError = Color.White,
        errorContainer = Color(0xFFFFE1DE),
        onErrorContainer = Color(0xFF5C0F0B),
        background = Color(0xFFF2F2F7),
        onBackground = Color(0xFF1C1C1E),
        surface = Color(0xFFFFFFFF),
        onSurface = Color(0xFF1C1C1E),
        surfaceVariant = Color(0xFFF2F2F7),
        onSurfaceVariant = Color(0xFF6C6C70),
        surfaceContainer = Color(0xFFF2F2F7),
        surfaceContainerHigh = Color(0xFFE9E9EE),
        surfaceContainerLow = Color(0xFFFFFFFF),
        outline = Color(0xFFC7C7CC),
        outlineVariant = Color(0xFFE5E5EA),
        scrim = Color(0xFF000000),
    )

    val Dark = darkColorScheme(
        // 深色用深一档主色：纯黑底上 #FF8FAB 大面积填充过亮，深色下压到 #E86A8C
        primary = Color(0xFFE86A8C),
        onPrimary = Color.White,
        primaryContainer = Color(0xFF5C2136),
        onPrimaryContainer = Color(0xFFFFD9E1),
        secondary = Color(0xFF6DB9E8),
        onSecondary = Color(0xFF04283C),
        secondaryContainer = Color(0xFF14405A),
        onSecondaryContainer = Color(0xFFCFE9F8),
        tertiary = Color(0xFFFFD166),
        onTertiary = Color(0xFF3D2E00),
        tertiaryContainer = Color(0xFF4A3A00),
        onTertiaryContainer = Color(0xFFFFEDB8),
        error = Color(0xFFFF453A),
        onError = Color.White,
        errorContainer = Color(0xFF5C1310),
        onErrorContainer = Color(0xFFFFDAD6),
        background = Color(0xFF000000),
        onBackground = Color(0xFFF2F2F7),
        surface = Color(0xFF1C1C1E),
        onSurface = Color(0xFFF2F2F7),
        surfaceVariant = Color(0xFF1C1C1E),
        onSurfaceVariant = Color(0xFF98989F),
        surfaceContainer = Color(0xFF1C1C1E),
        surfaceContainerHigh = Color(0xFF2C2C2E),
        surfaceContainerLow = Color(0xFF1C1C1E),
        outline = Color(0xFF545458),
        outlineVariant = Color(0xFF3A3A3C),
        scrim = Color(0xFF000000),
    )
}

/**
 * 色板之外的语义色（tint / sem-* / primaryDeep / rule）：
 * 塞进 ColorScheme 会丢语义，故单列一份，经 [LocalAppColors] 取用。
 */
@Immutable
data class AppColors(
    /** 深档主色：可点文字、图标、选中态文字 */
    val primaryDeep: Color,
    /** 操作色：文字/图标型可点控件（导航按钮、保存、文字动作） */
    val tint: Color,
    /** 操作色 10% 底衬 */
    val tintWash: Color,
    /** 0.5dp 分隔线 */
    val rule: Color,
    /** 卡面细边 */
    val cardEdge: Color,
    /** 作者名（信息蓝，与主色区分「身份」与「强调」） */
    val authorName: Color,
    val semBlue: Color,
    val semGreen: Color,
    val semGold: Color,
    val semGoldSoft: Color,
    val semGreenSoft: Color,
    /** 实心色块（头像 / 图标块）上的文字色 */
    val onSolid: Color,
)

internal val LightAppColors = AppColors(
    primaryDeep = Color(0xFFF06292),
    tint = Color(0xFF007AFF),
    tintWash = Color(0x1A007AFF),
    rule = Color(0x2E3C3C43),
    cardEdge = Color(0x143C3C43),
    authorName = Color(0xFF576B95),
    semBlue = Color(0xFF5AA7D4),
    semGreen = Color(0xFF34C759),
    semGold = Color(0xFFE8A800),
    semGoldSoft = Color(0x38FFD166),
    semGreenSoft = Color(0x2434C759),
    onSolid = Color.White,
)

internal val DarkAppColors = AppColors(
    primaryDeep = Color(0xFFFFB4C2),
    tint = Color(0xFF0A84FF),
    tintWash = Color(0x290A84FF),
    rule = Color(0xA6545458),
    cardEdge = Color(0x1FFFFFFF),
    authorName = Color(0xFF7A8FB8),
    semBlue = Color(0xFF6DB9E8),
    semGreen = Color(0xFF30D158),
    semGold = Color(0xFFFFD166),
    semGoldSoft = Color(0x29FFD166),
    semGreenSoft = Color(0x2930D158),
    onSolid = Color.White,
)

/**
 * 强调色预设（暖色系）：只做个人偏好，本地保存，不跨设备同步。
 * id 用于持久化，name 用于设置页展示，色值是浅色模式的 primary；深色模式沿用同一色。
 */
enum class AppAccent(val id: String, val label: String, val color: Color) {
    Pink("pink", "樱花粉", Color(0xFFFF8FAB)),
    Rose("rose", "玫瑰", Color(0xFFE86A8C)),
    Coral("coral", "珊瑚", Color(0xFFFF9A8B)),
    Peach("peach", "蜜桃", Color(0xFFFFB07C)),
    Apricot("apricot", "杏色", Color(0xFFFFC48C)),
    ;

    companion object {
        fun byId(id: String?): AppAccent = entries.firstOrNull { it.id == id } ?: Pink
    }
}

val LocalAppColors = staticCompositionLocalOf { LightAppColors }

/**
 * 当前是否深色主题（唯一判据）。
 * 不能用 `isSystemInDarkTheme()`：APP 的深浅由用户手动切换（顶栏月亮），
 * 与系统设置不同步，用它会导致图片/插画类资源不跟随切换。
 */
val LocalAppDark = staticCompositionLocalOf { false }
