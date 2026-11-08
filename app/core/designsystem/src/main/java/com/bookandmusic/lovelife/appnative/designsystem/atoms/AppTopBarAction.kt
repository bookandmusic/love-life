package com.bookandmusic.lovelife.appnative.designsystem.atoms

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.padding
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
import androidx.compose.ui.unit.dp
import com.bookandmusic.lovelife.appnative.designsystem.theme.LocalAppColors

/**
 * 顶栏动作位：44dp 圆形命中区 + 21dp 图标，图标用主色——
 * 顶栏的铃铛与齿轮一律用主色，这是全站最容易辨认的特征之一。
 * 命中区取 44dp 是触控目标下限：标题行同为 44dp，正好撑满不越界。
 */
@Composable
fun AppTopBarAction(
    icon: ImageVector,
    contentDescription: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    tint: Color = MaterialTheme.colorScheme.primary,
    badgeCount: Int = 0,
) {
    Box(
        modifier
            .size(44.dp)
            .clip(CircleShape)
            .clickable { onClick() },
        contentAlignment = Alignment.Center,
    ) {
        Icon(icon, contentDescription = contentDescription, tint = tint, modifier = Modifier.size(21.dp))
        if (badgeCount > 0) {
            Text(
                if (badgeCount > 99) "99+" else badgeCount.toString(),
                style = MaterialTheme.typography.labelMedium,
                color = LocalAppColors.current.onSolid,
                modifier = Modifier
                    .align(Alignment.TopEnd)
                    .padding(top = 2.dp, end = 1.dp)
                    .clip(RoundedCornerShape(999.dp))
                    .background(MaterialTheme.colorScheme.error)
                    .padding(horizontal = 4.dp, vertical = 1.dp),
            )
        }
    }
}

/** 顶栏文字动作：tint 蓝 17sp，禁用时 40% 透明。 */
@Composable
fun AppTopBarText(
    text: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
) {
    val colors = LocalAppColors.current
    Box(
        modifier
            .clip(CircleShape)
            .clickable(enabled = enabled) { onClick() }
            .padding(horizontal = 8.dp, vertical = 8.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text,
            style = MaterialTheme.typography.titleLarge,
            color = if (enabled) colors.tint else colors.tint.copy(alpha = 0.4f),
        )
    }
}
