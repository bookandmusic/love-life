package com.bookandmusic.lovelife.appnative.ui

import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.Stable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.staticCompositionLocalOf

/** 顶栏右侧动作（页面自管文案与可用性，壳层负责渲染位置）。 */
@Stable
class TopBarActionHost {
    var text: String? by mutableStateOf(null)
    /** 图标模式：非空时顶栏渲染图标而不是文字（如相册详情的「+」） */
    var icon: androidx.compose.ui.graphics.vector.ImageVector? by mutableStateOf(null)
    var enabled: Boolean by mutableStateOf(true)
    var onClick: (() -> Unit)? by mutableStateOf(null)
}

val LocalTopBarAction = staticCompositionLocalOf { TopBarActionHost() }

/**
 * 页面把「保存 / 发布」挂到顶栏右侧：
 * 页面自己管文案、禁用态与动作，壳层只负责渲染——避免把表单状态提升到壳层。
 */
@Composable
fun TopBarAction(text: String, enabled: Boolean, onClick: () -> Unit) {
    val host = LocalTopBarAction.current
    DisposableEffect(text, enabled, onClick) {
        host.text = text
        host.enabled = enabled
        host.onClick = onClick
        onDispose {
            host.text = null
            host.onClick = null
        }
    }
}

/** 图标动作（顶栏右侧）：与 [TopBarAction] 同一宿主，只是渲染成图标。 */
@Composable
fun TopBarIconAction(
    icon: androidx.compose.ui.graphics.vector.ImageVector,
    contentDescription: String,
    enabled: Boolean = true,
    onClick: () -> Unit,
) {
    val host = LocalTopBarAction.current
    DisposableEffect(icon, enabled, onClick) {
        host.icon = icon
        host.text = null
        host.enabled = enabled
        host.onClick = onClick
        onDispose {
            host.icon = null
            host.onClick = null
        }
    }
}

@Composable
fun rememberTopBarActionHost(): TopBarActionHost = remember { TopBarActionHost() }
