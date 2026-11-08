package com.bookandmusic.lovelife.appnative.designsystem.atoms

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.ui.input.pointer.PointerEventPass
import kotlinx.coroutines.withTimeoutOrNull
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.unit.dp

/**
 * 长按菜单项（全站唯一的上下文操作入口）。
 *
 * 为什么全站用长按而不是左滑：长按对**任何高度的条目**语义一致（动态卡 600dp 与评论行 20dp
 * 用同一手势），左滑是隐形手势、需要额外教学，且在占满屏宽的卡片上拖动手感很差。
 */
data class AppMenuItem(
    val label: String,
    val icon: ImageVector,
    val danger: Boolean = false,
    val enabled: Boolean = true,
    val onClick: () -> Unit,
)

/**
 * 长按动作单：底部升起，每项一行（图标 + 文字 + 右箭头），底部单独一行取消。
 * 危险项用错误色。全部项目居左，与 iOS action sheet 一致。
 */
@Composable
fun AppActionSheet(
    visible: Boolean,
    items: List<AppMenuItem>,
    onDismiss: () -> Unit,
    title: String? = null,
) {
    if (!visible) return
    AppSheet(
        visible = true,
        onDismiss = onDismiss,
        title = title,
        hideCancel = true,
        content = {
            Column(
                Modifier
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(12.dp))
                    .background(MaterialTheme.colorScheme.surfaceContainerHigh),
            ) {
                items.forEachIndexed { index, item ->
                    if (index > 0) {
                        AppRowDivider(startIndent = 52.dp)
                    }
                    Row(
                        Modifier
                            .fillMaxWidth()
                            .clickable(enabled = item.enabled) {
                                // 先执行动作再关闭：调用点的 onClick 常读「当前操作对象」这个状态，
                                // 先 onDismiss() 会先把该状态清空，动作里的 ?.let 就此静默不执行。
                                item.onClick()
                                onDismiss()
                            }
                            .padding(horizontal = 16.dp, vertical = 14.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(12.dp),
                    ) {
                        val tint = when {
                            !item.enabled -> MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.4f)
                            item.danger -> MaterialTheme.colorScheme.error
                            else -> MaterialTheme.colorScheme.onSurface
                        }
                        Icon(item.icon, contentDescription = null, tint = tint, modifier = Modifier.size(20.dp))
                        Text(
                            item.label,
                            style = MaterialTheme.typography.bodyLarge,
                            color = tint,
                            modifier = Modifier.weight(1f),
                        )
                    }
                }
            }
            // 取消单独一行（与项目组分离）
            Box(
                Modifier
                    .padding(top = 8.dp)
                    .fillMaxWidth()
                    .height(50.dp)
                    .clip(RoundedCornerShape(12.dp))
                    .background(MaterialTheme.colorScheme.surface)
                    .clickable { onDismiss() },
                contentAlignment = Alignment.Center,
            ) {
                Text(
                    "取消",
                    style = MaterialTheme.typography.bodyLarge,
                    color = MaterialTheme.colorScheme.onSurface,
                )
            }
        },
    )
}

/**
 * **叠加层专用**长按手势：按住 500ms 触发 [onLongPress]，必须赢过子元素手势处理器的场景才用它。
 *
 * 两个关键点：
 * 1. 在 **Initial pass** 观察指针（父级先于子元素看到事件）——媒体查看器页的长按必须赢过
 *    子元素 `ZoomableImage` 的双击/缩放、HorizontalPager 的滑动判定。
 * 2. 移动超过 touchSlop 取消（否则滚动列表时误触）。
 *
 * **同一元素上「点击 + 长按」并存时禁止用本修饰符**（叠加内层 clickable 会两个都触发），
 * 必须用 [appClickAndLongPress] 单探测器。平台层面「消费 up 拦不住已挂起的点击」：
 * `waitForUpOrCancellation` 先判 `changedToUp()` 再判 `isConsumed`，up 即使被消费，
 * 内层 clickable 照样触发 onTap——所以互斥只能靠单探测器，不能靠消费时机。
 *
 * 触发后的尾循环把**所有**后续事件（含最后的 up）全部消费：对同树其它观察者是卫生的
 * （如 pager 停止翻页），尽管救不了已挂起的 tap。
 */
fun Modifier.onAppLongPress(
    enabled: Boolean = true,
    onLongPress: () -> Unit,
): Modifier = if (!enabled) this else this.pointerInput(Unit) {
    awaitEachGesture {
        val down = awaitFirstDown(requireUnconsumed = false, pass = PointerEventPass.Initial)
        val downPosition = down.position
        val slop = viewConfiguration.touchSlop
        var moved = false
        var longPressed = false

        val held = withTimeoutOrNull(viewConfiguration.longPressTimeoutMillis) {
            while (true) {
                // 两个 pass 都要看：Initial 用于抢在子元素前判断，
                // Main 用于确认手势是否已被子元素消费（如列表滚动）
                val event = awaitPointerEvent(PointerEventPass.Initial)
                val change = event.changes.firstOrNull { it.id == down.id } ?: return@withTimeoutOrNull false
                if (!change.pressed) return@withTimeoutOrNull false
                if ((change.position - downPosition).getDistance() > slop) {
                    moved = true
                    return@withTimeoutOrNull false
                }
            }
            @Suppress("UNREACHABLE_CODE") false
        }

        if (held == null && !moved) {
            longPressed = true
            down.consume()
            onLongPress()
        }

        // 持续消费到抬手（含 up）：阻止外层 pager 等收到后续事件
        if (longPressed) {
            var up = false
            while (!up) {
                val event = awaitPointerEvent(PointerEventPass.Initial)
                event.changes.forEach {
                    it.consume()
                    if (!it.pressed) up = true
                }
            }
        }
    }
}

/**
 * 点击 + 长按（**单一手势处理器**，构造性互斥：按住超时 → 长按；按时抬起 → 点击，二者不会都触发）。
 *
 * 内核是平台的 `detectTapGestures`（`combinedClickable` 同款实现）：
 * - 长按用 `withTimeout` 判定——**静止手指没有中间事件也准点触发**（事件轮询式判定的老实现会漏）。
 * - down 当场消费（Main pass 子优先仲裁）：本元素收到 down 后，**父级的手势探测器会跳过整个手势**
 *  （KDoc：If the first down event is consumed somewhere else, the entire gesture will be skipped）。
 *  嵌套场景（动态卡包着媒体格）里「长按图片只弹媒体菜单、长按空白才弹动态菜单」由平台直接保证。
 * - 长按成立后 `consumeUntilUp()` 连 up 一起全收，onTap 不执行。
 *
 * lambda 经 `rememberUpdatedState` 持有：`pointerInput(Unit)` 的闭包只捕获首次组合的值，
 * 不桥接的话重组后的新 lambda 永远不会被调用。
 *
 * 无 ripple / indication（与全站 `Modifier.appClick` 一致，无视觉回归）。
 */
@Composable
fun Modifier.appClickAndLongPress(
    enabled: Boolean = true,
    onClick: () -> Unit,
    onLongPress: (() -> Unit)? = null,
): Modifier {
    val currentOnClick by rememberUpdatedState(onClick)
    val currentOnLongPress by rememberUpdatedState(onLongPress)
    return if (!enabled) this else this.pointerInput(Unit) {
        detectTapGestures(
            onTap = { currentOnClick() },
            onLongPress = onLongPress?.let { { currentOnLongPress?.invoke() } },
        )
    }
}
