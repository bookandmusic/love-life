package com.bookandmusic.lovelife.appnative.designsystem.atoms

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.unit.dp

@Immutable
data class AppNavItem(val label: String, val icon: ImageVector)

/**
 * iOS 26 式悬浮胶囊玻璃导航条：脱离屏幕边缘，项均分，
 * 选中 = 主色 12% 胶囊（14dp 圆角）；尾部可选角色按钮（细分隔线隔出，如「发布」），禁用触摇滚动。
 */
@Composable
fun AppNavBar(
    items: List<AppNavItem>,
    selected: Int,
    onSelect: (Int) -> Unit,
    modifier: Modifier = Modifier,
) {
    Row(
        modifier
            // 左右边距 16（原 24）：胶囊本体更宽，四项之间的留白更舒展
            .padding(start = 16.dp, end = 16.dp)
            .padding(bottom = WindowInsets.navigationBars.asPaddingValues().calculateBottomPadding() + 8.dp)
            .fillMaxWidth()
            .appChrome(AppChromeSurface.Bar, RoundedCornerShape(31.dp))
            .consumeTouches()
            .heightIn(min = 58.dp)
            .padding(horizontal = 6.dp, vertical = 5.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        items.forEachIndexed { index, item ->
            NavItem(item, selected == index, { onSelect(index) }, Modifier.weight(1f))
        }
    }
}

@Composable
private fun NavItem(
    item: AppNavItem,
    selected: Boolean,
    onSelect: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val tint = if (selected) {
        MaterialTheme.colorScheme.primary
    } else {
        MaterialTheme.colorScheme.onSurfaceVariant
    }
    Box(modifier.fillMaxWidth(), contentAlignment = Alignment.Center) {
        Column(
            Modifier
                // 选中底衬撑满条目宽度 + 固定高 → 横向圆角矩形（宽度 >> 高度）
                // 贴内容包裹时「图标 + 标签」竖向堆叠，底衬会变成竖长条
                .fillMaxWidth()
                .height(48.dp)
                .padding(horizontal = 4.dp)
                .clip(RoundedCornerShape(14.dp))
                .background(if (selected) MaterialTheme.colorScheme.primary.copy(alpha = 0.12f) else Color.Transparent)
                .clickable { onSelect() },
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.Center,
        ) {
            Icon(item.icon, contentDescription = item.label, tint = tint, modifier = Modifier.size(24.dp))
            Text(
                item.label,
                style = MaterialTheme.typography.labelMedium,
                color = tint,
                // 单行不换行：权重分到的宽度有限，换行会把「时光」拆成两行（内容被挤压）
                maxLines = 1,
                softWrap = false,
                modifier = Modifier.padding(top = 2.dp),
            )
        }
    }
}
