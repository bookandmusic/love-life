package com.bookandmusic.lovelife.appnative.designsystem.atoms

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.unit.dp

/**
 * 进度条（应用更新下载用）。轨道用 `surfaceVariant`、填充用主色，
 * 高度 8dp、胶囊圆角——与弹层里其它元素同一档位，不引入新视觉值。
 */
@Composable
fun AppProgressBar(
    percent: Int,
    modifier: Modifier = Modifier,
) {
    val shape = RoundedCornerShape(999.dp)
    val fraction = (percent.coerceIn(0, 100)) / 100f
    Box(
        modifier
            .fillMaxWidth()
            .height(8.dp)
            .clip(shape)
            .background(MaterialTheme.colorScheme.surfaceVariant)
    ) {
        Box(
            Modifier
                .fillMaxHeight()
                .fillMaxWidth(fraction)
                .clip(shape)
                .background(MaterialTheme.colorScheme.primary)
        )
    }
}