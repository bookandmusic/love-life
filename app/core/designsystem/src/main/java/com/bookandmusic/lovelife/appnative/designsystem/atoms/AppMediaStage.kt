package com.bookandmusic.lovelife.appnative.designsystem.atoms

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp

/**
 * 深色舞台（全屏媒体查看器）加载态原子：环形指示 + 次要文字。
 * 舞台恒走深色 token（黑舞台不跟随页面主题），故文字用 onSurfaceVariant；
 * 无容器底色——黑舞台上黑底胶囊不可见（黑叠黑），文案直排。
 */
@Composable
fun AppMediaLoading(
    text: String = "加载中…",
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        CircularProgressIndicator(
            color = MaterialTheme.colorScheme.primary,
            strokeWidth = 2.5.dp,
            modifier = Modifier.size(36.dp),
        )
        Text(
            text,
            style = MaterialTheme.typography.labelMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(top = 10.dp),
        )
    }
}

/** 深色舞台中央状态层：占满舞台让调用方对齐锚点（居中）。 */
@Composable
fun AppMediaStageState(
    modifier: Modifier = Modifier,
    content: @Composable () -> Unit,
) {
    Box(modifier = modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        content()
    }
}
