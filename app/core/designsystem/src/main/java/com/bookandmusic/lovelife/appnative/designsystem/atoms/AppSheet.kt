package com.bookandmusic.lovelife.appnative.designsystem.atoms

import androidx.activity.compose.BackHandler
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.MutableTransitionState
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInVertically
import androidx.compose.animation.slideOutVertically
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp

/**
 * 底部弹层：**实色白面**（不是玻璃）+ 顶部圆角 18
 * + 标题 15/600 居中 + 内容 + 底部按钮行（取消 ghost / 确认主色或危险色）。
 *
 * 不用 ModalBottomSheet：独立 Popup 的 Composition 拿不到主窗口的玻璃背板与主题。
 */
@Composable
fun AppSheet(
    visible: Boolean,
    onDismiss: () -> Unit,
    modifier: Modifier = Modifier,
    title: String? = null,
    confirmText: String? = null,
    cancelText: String? = null,
    confirmDanger: Boolean = false,
    confirmEnabled: Boolean = true,
    hideCancel: Boolean = false,
    /**
     * 不可关闭弹层（强更用）：返回键、点蒙层、Dialog 外部点击**都**不关。
     * 与 `hideCancel` 分工不同——后者只撤掉「取消」按钮，关得住返回键就是假强更。
     */
    closable: Boolean = true,
    onConfirm: (() -> Unit)? = null,
    content: @Composable ColumnScope.() -> Unit,
) {
    val state = remember { MutableTransitionState(false) }
    state.targetState = visible
    BackHandler(enabled = visible && closable) { onDismiss() }
    if (!state.currentState && !state.targetState) return
    // 用 Dialog 承载：弹层必须盖住顶栏与导航条（页面 Box 里的浮层会被后画的 chrome 压住）。
    // 弹层是实色白面，不依赖玻璃背板，独立窗口安全。
    androidx.compose.ui.window.Dialog(
        onDismissRequest = { if (closable) onDismiss() },
        properties = androidx.compose.ui.window.DialogProperties(
            usePlatformDefaultWidth = false,
            dismissOnClickOutside = closable,
        ),
    ) {
    Box(modifier.fillMaxSize()) {
        AnimatedVisibility(
            visibleState = state,
            enter = fadeIn(),
            exit = fadeOut(),
            modifier = Modifier.matchParentSize(),
        ) {
            Box(
                Modifier
                    .fillMaxSize()
                    .background(MaterialTheme.colorScheme.scrim.copy(alpha = 0.32f))
                    .clickable(
                        interactionSource = remember { MutableInteractionSource() },
                        indication = null,
                    ) { if (!hideCancel && closable) onDismiss() }
            )
        }
        AnimatedVisibility(
            visibleState = state,
            enter = slideInVertically(initialOffsetY = { it }),
            exit = slideOutVertically(targetOffsetY = { it }),
            modifier = Modifier.align(Alignment.BottomCenter),
        ) {
            Column(
                Modifier
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(topStart = 18.dp, topEnd = 18.dp))
                    .background(MaterialTheme.colorScheme.surface)
                    .padding(16.dp)
                    .padding(bottom = WindowInsets.navigationBars.asPaddingValues().calculateBottomPadding()),
            ) {
                if (title != null) {
                    Text(
                        title,
                        style = MaterialTheme.typography.titleMedium,
                        color = MaterialTheme.colorScheme.onSurface,
                        textAlign = TextAlign.Center,
                        modifier = Modifier.fillMaxWidth().padding(bottom = 12.dp),
                    )
                }
                content()
                if (confirmText != null || onConfirm != null) {
                    Row(
                        Modifier.fillMaxWidth().padding(top = 12.dp),
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        if (!hideCancel) {
                            AppButton(
                                cancelText ?: "取消",
                                onClick = onDismiss,
                                variant = AppButtonVariant.Ghost,
                                modifier = Modifier.weight(1f),
                            )
                        }
                        AppButton(
                            confirmText ?: "确定",
                            onClick = { onConfirm?.invoke() },
                            variant = if (confirmDanger) AppButtonVariant.Danger else AppButtonVariant.Primary,
                            enabled = confirmEnabled,
                            modifier = Modifier.weight(1f),
                        )
                    }
                }
            }
        }
    }
    }
}

/**
 * 轻量确认弹层：标题 + 一句后果说明 + 取消/危险按钮。
 * 删除链路统一走它。
 */
@Composable
fun AppConfirmSheet(
    visible: Boolean,
    title: String,
    message: String,
    confirmText: String,
    onConfirm: () -> Unit,
    onDismiss: () -> Unit,
    modifier: Modifier = Modifier,
    cancelText: String = "取消",
) {
    AppSheet(
        visible = visible,
        onDismiss = onDismiss,
        modifier = modifier,
        title = title,
        confirmText = confirmText,
        cancelText = cancelText,
        confirmDanger = true,
        onConfirm = {
            onConfirm()
            onDismiss()
        },
    ) {
        Text(
            message,
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurface,
            textAlign = TextAlign.Center,
            modifier = Modifier.fillMaxWidth(),
        )
    }
}
