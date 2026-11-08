package com.bookandmusic.lovelife.appnative.designsystem.atoms

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import com.bookandmusic.lovelife.appnative.designsystem.theme.LocalAppColors

/**
 * 内容卡：纯白面 + 圆角 + 细边 + 软阴影。
 * 玻璃只留给 chrome（顶栏 / 导航条 / 弹层），内容卡一律实色——滚动时不脱离页面。
 */
@Composable
fun AppCard(
    modifier: Modifier = Modifier,
    corner: Dp = 12.dp,
    padding: Dp = 16.dp,
    background: Color = MaterialTheme.colorScheme.surface,
    border: Boolean = true,
    shadow: Boolean = true,
    content: @Composable ColumnScope.() -> Unit,
) {
    val colors = LocalAppColors.current
    Column(
        modifier
            .then(if (shadow) Modifier.shadow(2.dp, RoundedCornerShape(corner), clip = false) else Modifier)
            .clip(RoundedCornerShape(corner))
            .background(background)
            .then(
                if (border) {
                    Modifier.border(1.dp, colors.cardEdge, RoundedCornerShape(corner))
                } else {
                    Modifier
                }
            )
            .padding(padding),
        content = content,
    )
}

