package com.bookandmusic.lovelife.appnative.designsystem.theme

import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Shapes
import androidx.compose.material3.Typography
import androidx.compose.ui.text.TextStyle
import androidx.compose.runtime.Composable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

/**
 * 标题字体族：默认系统字体；App 层通过 [AppTheme.titleFont] 注入品牌字体
 * （字体资源属 :app 模块，designsystem 不持有任何字体文件）。
 */
val LocalAppTitleFont = staticCompositionLocalOf<FontFamily?> { null }

/**
 * 字号阶梯：头卡天数 52/800、封面标题 19/700、区块标题 17/700、
 * 行标题 15/600、正文 14–15、说明 13、微 11。行高按 1.4–1.55 取值。
 */
internal val AppTypography = Typography(
    displaySmall = TextStyle(fontSize = 52.sp, fontWeight = FontWeight.ExtraBold, lineHeight = 56.sp, letterSpacing = (-0.03).sp),
    headlineSmall = TextStyle(fontSize = 19.sp, fontWeight = FontWeight.Bold, lineHeight = 26.sp),
    titleLarge = TextStyle(
        fontSize = 17.sp,
        fontWeight = FontWeight.SemiBold,
        lineHeight = 23.sp,
    ),
    titleMedium = TextStyle(fontSize = 15.sp, fontWeight = FontWeight.SemiBold, lineHeight = 21.sp),
    bodyLarge = TextStyle(fontSize = 15.sp, fontWeight = FontWeight.Normal, lineHeight = 23.sp),
    bodyMedium = TextStyle(fontSize = 14.sp, fontWeight = FontWeight.Normal, lineHeight = 21.sp),
    bodySmall = TextStyle(fontSize = 13.sp, fontWeight = FontWeight.Normal, lineHeight = 19.sp),
    labelLarge = TextStyle(fontSize = 13.sp, fontWeight = FontWeight.SemiBold, lineHeight = 18.sp),
    labelMedium = TextStyle(fontSize = 11.sp, fontWeight = FontWeight.Medium, lineHeight = 15.sp),
)

/**
 * 区块标题：17/700，字体由 [LocalAppTitleFont] 提供。
 */
@Composable
fun appSectionTitleStyle(): TextStyle {
    val family = LocalAppTitleFont.current
    val base = TextStyle(fontSize = 17.sp, fontWeight = FontWeight.Bold, lineHeight = 23.sp)
    return if (family != null) base.copy(fontFamily = family) else base
}

/** 顶栏标题：17/600，字体由 [LocalAppTitleFont] 提供。 */
@Composable
fun appTopBarTitleStyle(): TextStyle {
    val family = LocalAppTitleFont.current
    val base = TextStyle(fontSize = 17.sp, fontWeight = FontWeight.SemiBold, lineHeight = 23.sp)
    return if (family != null) base.copy(fontFamily = family) else base
}

/** 卡片正文：14/1.55。 */
val AppCardBodyStyle = TextStyle(fontSize = 14.sp, fontWeight = FontWeight.Normal, lineHeight = 22.sp)

internal val AppShapes = Shapes(
    extraSmall = RoundedCornerShape(6.dp),
    small = RoundedCornerShape(10.dp),
    medium = RoundedCornerShape(16.dp),
    large = RoundedCornerShape(24.dp),
    extraLarge = RoundedCornerShape(28.dp),
)
