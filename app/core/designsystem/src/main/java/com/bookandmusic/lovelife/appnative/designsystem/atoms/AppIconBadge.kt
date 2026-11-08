package com.bookandmusic.lovelife.appnative.designsystem.atoms

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import com.bookandmusic.lovelife.appnative.designsystem.theme.LocalAppColors

/** 图标块尺寸档（设置行 28 / 相册/动态头像 36 / 纪念日/地点 40 / 地图点位 44）。 */
enum class AppIconBadgeSize(val box: Dp, val icon: Dp) {
    Sm(28.dp, 16.dp),
    Md(36.dp, 18.dp),
    Lg(40.dp, 18.dp),
    Xl(44.dp, 20.dp),
}

/**
 * 圆角图标块：
 * soft 底衬 + 同色图标，是全站最显眼的「彩色标识」语言。
 */
@Composable
fun AppIconBadge(
    icon: ImageVector,
    tint: Color,
    background: Color,
    modifier: Modifier = Modifier,
    size: AppIconBadgeSize = AppIconBadgeSize.Lg,
    corner: Dp = 12.dp,
    contentDescription: String? = null,
) {
    Box(
        modifier
            .size(size.box)
            .clip(RoundedCornerShape(corner))
            .background(background),
        contentAlignment = Alignment.Center,
    ) {
        Icon(icon, contentDescription = contentDescription, tint = tint, modifier = Modifier.size(size.icon))
    }
}

/**
 * 头像块：圆形实色底 + 首字；有图时由调用方传 [image] 覆盖。
 * role 决定底色：her = 主色，him = 语义蓝，other = 中性灰。
 */
@Composable
fun AppAvatar(
    name: String,
    role: String?,
    modifier: Modifier = Modifier,
    size: Dp = 36.dp,
    /** 方形圆角（love-girl 风格：信息流头像为方形，比圆形更规整） */
    square: Boolean = false,
    image: (@Composable () -> Unit)? = null,
) {
    val colors = LocalAppColors.current
    val bg = when (role) {
        "girl" -> MaterialTheme.colorScheme.primary
        "boy" -> colors.semBlue
        else -> MaterialTheme.colorScheme.outline
    }
    val shape = if (square) RoundedCornerShape(8.dp) else CircleShape
    Box(
        modifier
            .size(size)
            .clip(shape)
            .background(bg),
        contentAlignment = Alignment.Center,
    ) {
        if (image != null) {
            image()
        } else {
            Text(
                name.take(1).ifEmpty { "·" },
                style = if (size >= 44.dp) MaterialTheme.typography.titleMedium else MaterialTheme.typography.bodySmall,
                color = colors.onSolid,
            )
        }
    }
}
