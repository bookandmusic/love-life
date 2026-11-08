package com.bookandmusic.lovelife.appnative.designsystem.atoms

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
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
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import com.bookandmusic.lovelife.appnative.designsystem.theme.LocalAppColors
import com.bookandmusic.lovelife.appnative.designsystem.theme.LocalAppSpacing

/**
 * 全站空态：主色图标（55% 不透明）+ 15/600 标题 + 13 副文案 + 主色胶囊 CTA。
 * 图标用主色而非灰：空态不显得「坏了」，而是「还没开始」。
 */
@Composable
fun AppEmptyState(
    title: String,
    modifier: Modifier = Modifier,
    hint: String? = null,
    icon: ImageVector? = null,
    cta: String? = null,
    onCta: (() -> Unit)? = null,
    compact: Boolean = false,
) {
    val s = LocalAppSpacing.current
    Column(
        modifier
            .fillMaxWidth()
            .padding(horizontal = s.xxl, vertical = if (compact) s.lg else s.xxl),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        icon?.let {
            Icon(
                it,
                contentDescription = null,
                modifier = Modifier.size(if (compact) 26.dp else 34.dp),
                tint = MaterialTheme.colorScheme.primary.copy(alpha = 0.55f),
            )
        }
        Text(
            title,
            style = MaterialTheme.typography.titleMedium,
            color = MaterialTheme.colorScheme.onSurface,
            textAlign = TextAlign.Center,
            modifier = Modifier.padding(top = if (icon != null) s.md else 0.dp),
        )
        if (hint != null) {
            Text(
                hint,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                textAlign = TextAlign.Center,
                modifier = Modifier.padding(top = 6.dp),
            )
        }
        if (cta != null) {
            AppPillButton(cta, onClick = { onCta?.invoke() }, modifier = Modifier.padding(top = 18.dp))
        }
    }
}

/** 胶囊按钮：主色底 + 白字，高 36dp。 */
@Composable
fun AppPillButton(
    text: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    background: Color = MaterialTheme.colorScheme.primary,
    contentColor: Color = LocalAppColors.current.onSolid,
    enabled: Boolean = true,
) {
    Row(
        modifier
            .clip(RoundedCornerShape(999.dp))
            .background(if (enabled) background else background.copy(alpha = 0.4f))
            .clickable(enabled = enabled) { onClick() }
            .padding(horizontal = 22.dp, vertical = 9.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(text, style = MaterialTheme.typography.bodyMedium, color = contentColor)
    }
}
