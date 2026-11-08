package com.bookandmusic.lovelife.appnative.designsystem.atoms

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.shape.CornerBasedShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.bookandmusic.lovelife.appnative.designsystem.theme.LocalAppColors

/**
 * chrome 表面（顶栏 / 导航条 / 弹层 / 地图控件）的唯一材质出口。
 *
 * 为什么不用折射玻璃：折射材质会在节点内缩一圈绘制折射区，与铺满节点的底衬叠加后
 * 形成「外框套内框」的双层边界（导航条上表现为胶囊外多一圈白色圆角矩形）。
 * chrome 只需要「近实色 + 细边 + 极轻投影」，这是 iOS 26 工具栏的实际观感，
 * 也保证内容永不会透上来。
 *
 * 三档（唯一允许的三种 chrome 面）：
 * - [AppChromeSurface.Bar]：顶栏 / 导航条（0.97 白 + 0.5dp 边线）
 * - [AppChromeSurface.Floating]：浮在内容上的控件（地图缩放、浮动条）
 * - [AppChromeSurface.Overlay]：弹层（1.0 实色，必须完全遮住下方）
 */
enum class AppChromeSurface { Bar, Floating, Overlay }

@Composable
fun Modifier.appChrome(
    surface: AppChromeSurface = AppChromeSurface.Bar,
    shape: CornerBasedShape = RoundedCornerShape(0.dp),
): Modifier {
    val colors = LocalAppColors.current
    val alpha = when (surface) {
        AppChromeSurface.Bar -> 0.97f
        AppChromeSurface.Floating -> 0.95f
        AppChromeSurface.Overlay -> 1f
    }
    val border = when (surface) {
        AppChromeSurface.Overlay -> 0.dp
        else -> 0.5.dp
    }
    var m = this.background(MaterialTheme.colorScheme.surface.copy(alpha = alpha), shape)
    if (border > 0.dp) m = m.border(border, colors.cardEdge, shape)
    return m
}

/**
 * 吃掉落在本节点内的触摸（chrome 专用）：顶栏 / 导航条浮在滚动内容上，
 * 点在其空隙里必须由 chrome 消化，不能穿透到底下的列表。
 * 用无涟漪的 clickable 而不是 pointerInput 消费事件——子节点的点击先于父处理，
 * 嵌套 clickable 天然让子项优先（条内按钮照常工作）。
 */
@Composable
fun Modifier.consumeTouches(): Modifier {
    val source = remember { MutableInteractionSource() }
    return this.clickable(
        interactionSource = source,
        indication = null,
        onClick = {},
    )
}
