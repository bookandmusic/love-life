package com.bookandmusic.lovelife.appnative.designsystem.atoms

import androidx.compose.foundation.border
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.outlined.MusicNote
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.unit.dp
import coil.compose.AsyncImage
import com.bookandmusic.lovelife.appnative.designsystem.theme.LocalAppColors

/** 媒体格数据（:app 的 PostMediaUi 直接映射过来，保持 designsystem 不依赖业务模型）。 */
data class AppMediaItem(
    val kind: String?,
    val thumbUrl: String?,
    val durationText: String? = null,
    /** 原始宽高（单图按真实比例；0 = 未知，回退 4:3） */
    val width: Int = 0,
    val height: Int = 0,
)

/**
 * 媒体网格：1 张 4:3、2 张两列、3+ 张三列方格；
 * 视频右下播放角标（图标 + 时长），音频显示音符块。点击进全屏查看器。
 */
@Composable
fun AppMediaGrid(
    items: List<AppMediaItem>,
    onOpen: (Int) -> Unit,
    modifier: Modifier = Modifier,
    /** 长按媒体格 → 动作单（保存到本机 / 删除）；null = 不提供长按 */
    onLongPress: ((Int) -> Unit)? = null,
) {
    if (items.isEmpty()) return
    // 微信九宫格：1 张独立 / 2-3 张单行 / 4 张 2×2 / 5-9 张三列
    val columns = when (items.size) {
        1 -> 1
        2, 3 -> items.size
        4 -> 2
        else -> 3
    }
    val rows = items.chunked(columns)
    androidx.compose.foundation.layout.Column(
        // 贴边铺满：左右不留白，外侧圆角由宿主卡片裁剪
        modifier.padding(top = 10.dp),
        verticalArrangement = Arrangement.spacedBy(3.dp),
    ) {
        rows.forEachIndexed { rowIndex, rowItems ->
            Row(horizontalArrangement = Arrangement.spacedBy(3.dp)) {
                rowItems.forEachIndexed { colIndex, m ->
                    val index = rowIndex * columns + colIndex
                    MediaCell(
                        m = m,
                        single = columns == 1,
                        columns = columns,
                        modifier = Modifier.weight(1f),
                        onClick = { onOpen(index) },
                        onLongPress = onLongPress?.let { { it(index) } },
                    )
                }
                repeat(columns - rowItems.size) {
                    Box(Modifier.weight(1f))
                }
            }
        }
    }
}

@Composable
private fun MediaCell(
    m: AppMediaItem,
    single: Boolean,
    columns: Int,
    modifier: Modifier,
    onClick: () -> Unit,
    onLongPress: (() -> Unit)? = null,
) {
    val colors = LocalAppColors.current
    val isAudio = m.kind == "audio"
    // 单图/多图都不用圆角：外层卡片统一裁切，格子间只靠 3dp 缝隙分隔
    val shape = androidx.compose.foundation.shape.RoundedCornerShape(0.dp)
    Box(
        modifier
            .then(
                if (single) {
                    // 单图：按原图比例，宽度 60% 屏宽（微信式，不裁切、不撑满整行）
                    val ratio = if (m.width > 0 && m.height > 0) {
                        (m.width.toFloat() / m.height).coerceIn(0.6f, 1.6f)
                    } else {
                        4f / 3f
                    }
                    Modifier.fillMaxWidth(0.6f).aspectRatio(ratio)
                } else {
                    Modifier.aspectRatio(1f)
                }
            )
            .clip(shape)
            .background(MaterialTheme.colorScheme.surfaceContainerHigh)
            // 点击与长按必须走单一手势处理器：clickable + onAppLongPress 两个独立处理器
            // 会各自收到同一串指针事件，长按弹菜单的同时还会触发点击（进查看器）
            .appClickAndLongPress(onClick = onClick, onLongPress = onLongPress),
        contentAlignment = Alignment.Center,
    ) {
        when {
            // 视频：首帧封面（服务端 ffmpeg 抽帧）+ 居中白色圆形播放键 + 右下时长
            m.kind == "video" -> {
                if (m.thumbUrl != null) {
                    AsyncImage(
                        model = m.thumbUrl,
                        contentDescription = "视频",
                        contentScale = ContentScale.Crop,
                        modifier = Modifier.fillMaxSize(),
                    )
                }
                // 居中白色圆形播放键（微信式：半透明黑底 + 白描边 + 白三角）
                Box(
                    Modifier
                        .size(if (single) 56.dp else 36.dp)
                        .clip(CircleShape)
                        .background(Color.Black.copy(alpha = 0.35f))
                        .border(2.dp, Color.White, CircleShape),
                    contentAlignment = Alignment.Center,
                ) {
                    Icon(
                        Icons.Filled.PlayArrow,
                        contentDescription = "播放",
                        tint = Color.White,
                        modifier = Modifier.size(if (single) 30.dp else 20.dp),
                    )
                }
                if (!m.durationText.isNullOrEmpty()) {
                    Text(
                        m.durationText,
                        style = MaterialTheme.typography.labelMedium,
                        color = Color.White,
                        modifier = Modifier
                            .align(Alignment.BottomEnd)
                            .padding(5.dp)
                            .clip(CircleShape)
                            .background(Color.Black.copy(alpha = 0.55f))
                            .padding(horizontal = 6.dp, vertical = 2.dp),
                    )
                }
            }
            // 音频：波形封面（服务端 ffmpeg 生成）+ 右下音符角标
            isAudio -> {
                if (m.thumbUrl != null) {
                    AsyncImage(
                        model = m.thumbUrl,
                        contentDescription = "录音",
                        contentScale = ContentScale.Crop,
                        modifier = Modifier.fillMaxSize(),
                    )
                } else {
                    Icon(
                        Icons.Outlined.MusicNote,
                        contentDescription = "录音",
                        tint = MaterialTheme.colorScheme.primary,
                        modifier = Modifier.size(28.dp),
                    )
                }
                Row(
                    Modifier
                        .align(Alignment.BottomEnd)
                        .padding(5.dp)
                        .clip(CircleShape)
                        .background(Color.Black.copy(alpha = 0.55f))
                        .padding(horizontal = 6.dp, vertical = 2.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(3.dp),
                ) {
                    Icon(
                        Icons.Outlined.MusicNote,
                        contentDescription = null,
                        tint = Color.White,
                        modifier = Modifier.size(10.dp),
                    )
                    if (!m.durationText.isNullOrEmpty()) {
                        Text(m.durationText, style = MaterialTheme.typography.labelMedium, color = Color.White)
                    }
                }
            }
            m.thumbUrl != null -> {
                AsyncImage(
                    model = m.thumbUrl,
                    contentDescription = m.kind,
                    contentScale = ContentScale.Crop,
                    modifier = Modifier.fillMaxSize(),
                )
                if (m.kind == "video") {
                    Row(
                        Modifier
                            .align(Alignment.BottomEnd)
                            .padding(5.dp)
                            .clip(CircleShape)
                            .background(Color.Black.copy(alpha = 0.55f))
                            .padding(horizontal = 5.dp, vertical = 2.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(3.dp),
                    ) {
                        Icon(
                            Icons.Filled.PlayArrow,
                            contentDescription = null,
                            tint = colors.onSolid,
                            modifier = Modifier.size(10.dp),
                        )
                        if (!m.durationText.isNullOrEmpty()) {
                            Text(m.durationText, style = MaterialTheme.typography.labelMedium, color = colors.onSolid)
                        }
                    }
                }
            }
        }
    }
}
