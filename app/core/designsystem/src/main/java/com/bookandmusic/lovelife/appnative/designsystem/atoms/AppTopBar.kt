package com.bookandmusic.lovelife.appnative.designsystem.atoms

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBars
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp

/**
 * 玻璃顶栏：标题**居中**（iOS HIG 内联标题）、左侧返回、右侧动作簇；
 * 状态栏 inset + 44dp 标题行。内容从其下滚过（浮层，不随内容滚动）。
 */
@Composable
fun AppTopBar(
    title: String,
    modifier: Modifier = Modifier,
    leading: (@Composable RowScope.() -> Unit)? = null,
    trailing: (@Composable RowScope.() -> Unit)? = null,
) {
    Box(
        modifier
            .fillMaxWidth()
            .appChrome(AppChromeSurface.Bar)
            .consumeTouches()
            .padding(top = WindowInsets.statusBars.asPaddingValues().calculateTopPadding())
    ) {
        // 标题**绝对居中**（叠层，不参与 Row 分配）：左右动作数量变化时标题不会左右漂。
        // 左右动作各占自身宽度并贴边；若两侧都用 weight(1f)，右侧 3 个动作会被等分宽度裁掉。
        Box(Modifier.fillMaxWidth().height(44.dp)) {
            Text(
                title,
                style = com.bookandmusic.lovelife.appnative.designsystem.theme.appTopBarTitleStyle(),
                color = MaterialTheme.colorScheme.onSurface,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier
                    .align(Alignment.Center)
                    // 左右各留 120dp 给动作区，避免长标题压到图标
                    .padding(horizontal = 120.dp),
            )
            Row(
                Modifier.fillMaxWidth().padding(horizontal = 4.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Row(verticalAlignment = Alignment.CenterVertically) { leading?.invoke(this) }
                Spacer(Modifier.weight(1f))
                Row(verticalAlignment = Alignment.CenterVertically) { trailing?.invoke(this) }
            }
        }
    }
}
