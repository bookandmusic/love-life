package com.bookandmusic.lovelife.appnative.ui

import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Favorite
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import com.bookandmusic.lovelife.appnative.data.AuthRepository
import com.bookandmusic.lovelife.appnative.data.Channel
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppButton
import com.bookandmusic.lovelife.appnative.designsystem.atoms.brandTileShape
import kotlinx.coroutines.withTimeoutOrNull

/**
 * 启动加载页：会话恢复与取通道并行，总超时 15s。
 * 网络失败停留本页可重试，不退回登录页；凭据失效才引导重新登录。
 */
@Composable
fun BootScreen(onReady: () -> Unit, onExpired: () -> Unit) {
    var phase by remember { mutableStateOf("restoring") } // restoring | offline | expired
    var detail by remember { mutableStateOf("") }
    var attempt by remember { mutableIntStateOf(0) }

    LaunchedEffect(attempt) {
        phase = "restoring"
        detail = ""
        val result = withTimeoutOrNull(15_000L) {
            when (AuthRepository.restoreWithRetry()) {
                AuthRepository.RestoreResult.Ok -> {
                    Channel.init()
                    "ok"
                }
                AuthRepository.RestoreResult.Expired -> "expired"
                AuthRepository.RestoreResult.Network -> "network"
            }
        }
        when (result) {
            "ok" -> onReady()
            "expired" -> {
                phase = "expired"
                detail = "登录已过期，请重新登录"
            }
            else -> {
                phase = "offline"
                detail = "连接超时，请检查网络后重试"
            }
        }
    }

    Box(
        Modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background),
        contentAlignment = Alignment.Center,
    ) {
        Column(horizontalAlignment = Alignment.CenterHorizontally, modifier = Modifier.padding(horizontal = 32.dp)) {
            androidx.compose.foundation.Image(
                painter = androidx.compose.ui.res.painterResource(com.bookandmusic.lovelife.appnative.R.drawable.brand_icon),
                contentDescription = "情侣生活记录",
                modifier = Modifier.size(96.dp).clip(brandTileShape(96.dp)),
            )
            Text(
                "情侣生活记录",
                style = MaterialTheme.typography.headlineSmall,
                color = MaterialTheme.colorScheme.onSurface,
                modifier = Modifier.padding(top = 16.dp),
            )
            if (phase == "restoring") {
                val transition = rememberInfiniteTransition(label = "boot-breathe")
                val alpha by transition.animateFloat(
                    initialValue = 0.18f,
                    targetValue = 0.55f,
                    animationSpec = infiniteRepeatable(tween(800), RepeatMode.Reverse),
                    label = "boot-breathe-alpha",
                )
                Box(
                    Modifier
                        .padding(top = 18.dp)
                        .height(3.dp)
                        .fillMaxWidth(0.45f)
                        .alpha(alpha)
                        .clip(RoundedCornerShape(999.dp))
                        .background(MaterialTheme.colorScheme.primary)
                )
                Text(
                    "正在恢复登录…",
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(top = 8.dp),
                )
            } else {
                Text(
                    detail,
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    textAlign = TextAlign.Center,
                    modifier = Modifier.padding(top = 8.dp),
                )
                if (phase == "offline") {
                    AppButton("重试", onClick = { attempt++ }, modifier = Modifier.padding(top = 14.dp))
                } else {
                    AppButton("去登录", onClick = onExpired, modifier = Modifier.padding(top = 14.dp))
                }
            }
        }
    }
}
