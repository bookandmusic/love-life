package com.bookandmusic.lovelife.appnative.designsystem.atoms

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import com.bookandmusic.lovelife.appnative.designsystem.theme.LocalAppColors

/**
 * 底部浮条反馈：黑底白字胶囊，浮在导航条之上。
 * **2.5 秒自动消失**（也支持点击即消）——只靠点击消会一直挂在屏幕上。
 */
@Composable
fun AppToast(
    text: String?,
    onDismiss: () -> Unit,
    modifier: Modifier = Modifier,
    bottomPadding: androidx.compose.ui.unit.Dp = 100.dp,
) {
    // 自动消失：文本变化时重新计时
    androidx.compose.runtime.LaunchedEffect(text) {
        if (!text.isNullOrEmpty()) {
            kotlinx.coroutines.delay(2500)
            onDismiss()
        }
    }
    Box(modifier.fillMaxSize()) {
        AnimatedVisibility(
            visible = !text.isNullOrEmpty(),
            enter = fadeIn(),
            exit = fadeOut(),
            modifier = Modifier.align(Alignment.BottomCenter),
        ) {
            Text(
                text.orEmpty(),
                style = MaterialTheme.typography.bodyMedium,
                color = LocalAppColors.current.onSolid,
                textAlign = TextAlign.Center,
                modifier = Modifier
                    .padding(horizontal = 24.dp)
                    .padding(bottom = bottomPadding)
                    .clip(RoundedCornerShape(12.dp))
                    .background(Color.Black.copy(alpha = 0.78f))
                    .clickable { onDismiss() }
                    .padding(horizontal = 20.dp, vertical = 10.dp),
            )
        }
    }
}
