package com.bookandmusic.lovelife.appnative.designsystem.atoms

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.unit.dp
import com.bookandmusic.lovelife.appnative.designsystem.theme.LocalAppColors

/** Primary = 主色填充；Danger = 危险红填充；Ghost = 灰底深字（取消）。 */
enum class AppButtonVariant { Primary, Danger, Ghost }

/** 按钮：高 42、圆角 12、15/500。 */
@Composable
fun AppButton(
    text: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    variant: AppButtonVariant = AppButtonVariant.Primary,
    enabled: Boolean = true,
) {
    val colors = LocalAppColors.current
    val bg = when (variant) {
        AppButtonVariant.Primary -> MaterialTheme.colorScheme.primary
        AppButtonVariant.Danger -> MaterialTheme.colorScheme.error
        AppButtonVariant.Ghost -> MaterialTheme.colorScheme.surfaceContainerHigh
    }
    val fg = when (variant) {
        AppButtonVariant.Primary, AppButtonVariant.Danger -> colors.onSolid
        AppButtonVariant.Ghost -> MaterialTheme.colorScheme.onSurface
    }
    Box(
        modifier
            .fillMaxWidth()
            .height(42.dp)
            .clip(RoundedCornerShape(12.dp))
            .background(if (enabled) bg else bg.copy(alpha = 0.4f))
            .clickable(enabled = enabled) { onClick() },
        contentAlignment = Alignment.Center,
    ) {
        Text(text, style = MaterialTheme.typography.bodyLarge, color = fg)
    }
}
