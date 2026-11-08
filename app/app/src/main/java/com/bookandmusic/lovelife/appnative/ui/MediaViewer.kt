package com.bookandmusic.lovelife.appnative.ui

import android.net.Uri
import androidx.core.net.toUri
import androidx.compose.foundation.layout.height
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.foundation.layout.Column
import androidx.compose.runtime.LaunchedEffect
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.ui.input.pointer.pointerInput
import com.bookandmusic.lovelife.appnative.designsystem.atoms.onAppLongPress
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.gestures.calculateZoom
import androidx.compose.foundation.gestures.calculatePan
import androidx.compose.foundation.gestures.detectTransformGestures
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.pager.HorizontalPager
import androidx.compose.foundation.pager.rememberPagerState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material.icons.outlined.MusicNote
import androidx.compose.material.icons.outlined.Image
import androidx.compose.material.icons.outlined.PlayCircle
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.Download
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.media3.common.MediaItem
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.datasource.DefaultHttpDataSource
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.source.ProgressiveMediaSource
import androidx.media3.extractor.DefaultExtractorsFactory
import androidx.media3.ui.PlayerView
import coil.compose.AsyncImage
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import com.bookandmusic.lovelife.appnative.data.PostMediaUi
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppMediaLoading
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppMediaStageState
import com.bookandmusic.lovelife.appnative.data.PostsRepository

/**
 * 全屏媒体查看器（时光动态 / 相册详情共用）：照片 ContentScale.Fit 浏览，
 * 视频/音频用 Media3 ExoPlayer：渐进式流起播（不等全文件）+ 缓冲/失败反馈，进入即播、退出释放。
 */
@Composable
fun MediaViewer(
    media: List<PostMediaUi>,
    initialIndex: Int,
    onDismiss: () -> Unit,
    /** 长按媒体 → 动作单（保存到本机 / 删除）；由调用方在页面层承载弹层 */
    onLongPress: ((PostMediaUi) -> Unit)? = null,
) {
    if (media.isEmpty()) {
        onDismiss()
        return
    }
    Dialog(
        onDismissRequest = onDismiss,
        properties = DialogProperties(usePlatformDefaultWidth = false),
    ) {
        // 黑舞台恒走深色 token（页面主题切档会让浅色亮粉落纯黑底上过亮）
        com.bookandmusic.lovelife.appnative.designsystem.theme.AppTheme(darkTheme = true) {
        // 系统返回键退出（微信式：查看器里没有关闭按钮）
        BackHandler { onDismiss() }
        val pagerState = rememberPagerState(initialPage = initialIndex.coerceIn(0, media.size - 1)) { media.size }
        val current = media[pagerState.currentPage]
        Box(
            Modifier
                .fillMaxSize()
                .background(Color.Black),
        ) {
            HorizontalPager(state = pagerState, modifier = Modifier.fillMaxSize()) { pageIndex ->
                val m = media[pageIndex]
                Box(
                    Modifier
                        .fillMaxSize()
                        // 长按在 Initial pass 观察：正常滑动/缩放不消费，仍归 pager 与 ZoomableImage
                        .then(
                            if (onLongPress != null) {
                                Modifier.onAppLongPress { onLongPress(m) }
                            } else Modifier
                        )
                ) {
                    when {
                        m.kind == "video" -> VideoPlayerPage(m)
                        m.kind == "audio" -> AudioPlayerPage(m)
                        else -> ZoomableImage(m.url ?: m.thumbUrl)
                    }
                }
            }
            // ===== 顶部 chrome：中央计数 + 类型标识 =====
            Row(
                Modifier
                    .fillMaxWidth()
                    .align(Alignment.TopCenter)
                    .statusBarsPadding()
                    .padding(top = 8.dp, bottom = 8.dp),
                horizontalArrangement = androidx.compose.foundation.layout.Arrangement.Center,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    "${pagerState.currentPage + 1} / ${media.size}",
                    style = MaterialTheme.typography.labelLarge,
                    color = Color.White.copy(alpha = 0.85f),
                )
                Box(
                    Modifier
                        .padding(start = 10.dp)
                        .clip(RoundedCornerShape(999.dp))
                        .background(Color.White.copy(alpha = 0.18f))
                        .padding(horizontal = 8.dp, vertical = 3.dp),
                ) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Icon(
                            when (current.kind) {
                                "video" -> Icons.Outlined.PlayCircle
                                "audio" -> Icons.Outlined.MusicNote
                                else -> Icons.Outlined.Image
                            },
                            contentDescription = null,
                            tint = Color.White.copy(alpha = 0.85f),
                            modifier = Modifier.size(12.dp),
                        )
                        Text(
                            when (current.kind) {
                                "video" -> "视频"
                                "audio" -> "录音"
                                else -> "照片"
                            },
                            style = MaterialTheme.typography.labelMedium,
                            color = Color.White.copy(alpha = 0.85f),
                            modifier = Modifier.padding(start = 4.dp),
                        )
                    }
                }
            }

            // ===== 底部 chrome：一行类型提示（加载/失败态也常驻）=====
            val hint = buildString {
                append(
                    when (current.kind) {
                        "video", "audio" -> "长按可保存或删除 · 点击播放"
                        else -> "长按可保存或删除 · 双击缩放"
                    }
                )
                if (media.size > 1) append(" · 左右滑动切换")
            }
            Text(
                hint,
                style = MaterialTheme.typography.labelMedium,
                color = Color.White.copy(alpha = 0.6f),
                modifier = Modifier
                    .align(Alignment.BottomCenter)
                    .navigationBarsPadding()
                    .padding(bottom = 16.dp),
            )

            // 多图时底部小圆点（微信式：只留页码指示）——上移避让底栏提示
            if (media.size > 1) {
                Row(
                    Modifier
                        .fillMaxWidth()
                        .align(Alignment.BottomCenter)
                        .navigationBarsPadding()
                        .padding(bottom = 44.dp),
                    horizontalArrangement = androidx.compose.foundation.layout.Arrangement.Center,
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    repeat(media.size) { i ->
                        Box(
                            Modifier
                                .padding(horizontal = 3.dp)
                                .size(6.dp)
                                .clip(CircleShape)
                                .background(
                                    if (i == pagerState.currentPage) Color.White
                                    else Color.White.copy(alpha = 0.35f)
                                )
                        )
                    }
                }
            }
        }
        }
    }
}

/**
 * 播放前地址保障：签名 URL 15 分钟 TTL，
 * 临期（<2 分钟）先 POST /media/reurl 换新再播；重签失败用原地址兜底试一次
 * （经中转过期地址会被 302 到登录页 HTML，播放器会给出可见错误而非无声黑屏）。
 */
internal suspend fun playableUrl(m: PostMediaUi): String {
    val url = m.url ?: return ""
    val exp = url.substringAfter("exp=", "").substringBefore('&').toLongOrNull() ?: return url
    val remainSec = exp - System.currentTimeMillis() / 1000
    if (remainSec > 120 || m.id == 0L) return url
    val fresh = runCatching { PostsRepository.reurl(listOf(m.id))[m.id] }.getOrNull()
    val rel = fresh ?: return url
    return com.bookandmusic.lovelife.appnative.data.Channel.base().trimEnd('/') + rel
}

/**
 * 视频页：ExoPlayer 渐进式流播放（缓冲即起播）+ 系统控制器。
 * 缓冲中 → 居中「加载中…」；错误 → 「无法播放」提示（点击重试走重签）。
 */
@Composable
private fun VideoPlayerPage(m: PostMediaUi) {
    var playUrl by remember(m.url) { mutableStateOf<String?>(null) }
    var retryKey by remember(m.url) { mutableIntStateOf(0) }
    LaunchedEffect(m.url, retryKey) {
        playUrl = runCatching { playableUrl(m) }.getOrDefault(m.url)
    }
    playUrl?.let { url ->
        ExoPlayerSurface(url = url, onRetry = { retryKey++ })
    }
}

/** 共用的 ExoPlayer 状态管理：创建、缓冲/错误观察、进度轮询、释放。 */
// media3 的播放器装配 API（ProgressiveMediaSource / DefaultHttpDataSource / DefaultExtractorsFactory）
// 标了 @UnstableApi：官方用法是显式 opt-in 确认——不用则 lint 报 UnsafeOptInUsageError。
@androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)
@Composable
private fun rememberExoPlayerState(url: String): ExoPlayerState {
    val context = LocalContext.current
    var state by remember(url) {
        mutableStateOf(ExoPlayerState(buffering = true))
    }
    var position by remember(url) { mutableIntStateOf(0) }
    var duration by remember(url) { mutableIntStateOf(0) }
    var playing by remember(url) { mutableStateOf(false) }

    val player = remember(url) {
        ExoPlayer.Builder(context).build().apply {
            // VBR 无 Xing 头的 mp3：常量码率寻址把时长当 CBR 估——不必等全文件扫帧（流式起播的关键）
            val extractors = DefaultExtractorsFactory().setConstantBitrateSeekingEnabled(true)
            val source = ProgressiveMediaSource.Factory(
                DefaultHttpDataSource.Factory()
                    .setDefaultRequestProperties(mediaHeaders())
                    .setConnectTimeoutMs(8_000)
                    .setReadTimeoutMs(15_000),
                extractors,
            ).createMediaSource(MediaItem.fromUri(url.toUri()))
            setMediaSource(source)
            prepare()
            playWhenReady = true
        }
    }
    DisposableEffect(url) {
        val listener = object : Player.Listener {
            override fun onPlaybackStateChanged(playbackState: Int) {
                state = when (playbackState) {
                    Player.STATE_BUFFERING -> state.copy(buffering = true, ended = false)
                    Player.STATE_READY -> state.copy(buffering = false, error = null)
                    Player.STATE_ENDED -> state.copy(buffering = false, ended = true)
                    else -> state
                }
                duration = player.duration.toInt()
            }

            override fun onIsPlayingChanged(isPlaying: Boolean) {
                playing = isPlaying
            }

            override fun onPlayerError(error: PlaybackException) {
                state = state.copy(buffering = false, error = "无法播放，点按重试")
            }
        }
        player.addListener(listener)
        onDispose { player.release() }
    }
    // 进度轮询（自绘进度条数据源）
    LaunchedEffect(url, playing) {
        while (playing) {
            position = player.currentPosition.toInt()
            duration = player.duration.toInt()
            kotlinx.coroutines.delay(500)
        }
    }
    return remember(url, state, position, duration, playing) {
        ExoPlayerState(
            player = player,
            buffering = state.buffering,
            error = state.error,
            ended = state.ended,
            positionMs = position,
            durationMs = duration,
            playing = playing,
        )
    }
}

/** ExoPlayer 观察态（player 供调用方操作；其余为渲染数据）。 */
private data class ExoPlayerState(
    val player: ExoPlayer? = null,
    val buffering: Boolean = false,
    val error: String? = null,
    val ended: Boolean = false,
    val positionMs: Int = 0,
    val durationMs: Int = 0,
    val playing: Boolean = false,
)

/** 视频：ExoPlayer + PlayerView（系统控制器，与系统播放器一致）。 */
@androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)
@Composable
private fun ExoPlayerSurface(url: String, onRetry: () -> Unit) {
    val st = rememberExoPlayerState(url)
    Box(Modifier.fillMaxSize()) {
        st.player?.let { p ->
            AndroidView(
                factory = { ctx -> PlayerView(ctx).apply {
                    player = p
                    useController = true
                    // 加载期不自动弹系统控制器：自绘 chrome 常驻，避免双层控制叠加
                    controllerAutoShow = false
                } },
                modifier = Modifier.fillMaxSize(),
            )
        }
        // 状态层统一舞台居中锚点（视频/音频一致）
        AppMediaStageState {
            when {
                st.error != null -> ErrorHint(text = st.error!!, onClick = onRetry)
                st.buffering -> AppMediaLoading()
            }
        }
    }
}

/** 错误提示：黑舞台上文字直排（黑底胶囊不可见），点击重试（重试前会重新走重签）。 */
@Composable
private fun ErrorHint(text: String, onClick: () -> Unit) {
    Column(
        Modifier
            .clickable(onClick = onClick)
            .padding(horizontal = 24.dp, vertical = 12.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Icon(
            Icons.Outlined.Close,
            contentDescription = null,
            tint = MaterialTheme.colorScheme.error,
            modifier = Modifier.size(28.dp),
        )
        Text(
            text,
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurface,
            modifier = Modifier.padding(top = 8.dp),
        )
        Text(
            "文件可能已损坏或格式不受支持",
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(top = 4.dp),
        )
    }
}

/** 媒体请求头：绿联中转 cookie（直连/备用通道为空 map，等价于不带）。 */
internal fun mediaHeaders(): Map<String, String> {
    val cookie = com.bookandmusic.lovelife.appnative.data.Channel.cookieValue()
    return if (cookie.isEmpty()) emptyMap() else mapOf("Cookie" to cookie)
}

/**
 * 音频：ExoPlayer 流式播放（VBR 录音不必等全文件下完才出时长）。
 * 自绘播放/暂停 + 进度条 + 时长（保留原有视觉），缓冲/失败有文字反馈。
 */
@Composable
private fun AudioPlayerPage(m: PostMediaUi) {
    var playUrl by remember(m.url) { mutableStateOf<String?>(null) }
    var retryKey by remember(m.url) { mutableIntStateOf(0) }
    LaunchedEffect(m.url, retryKey) {
        playUrl = runCatching { playableUrl(m) }.getOrDefault(m.url)
    }
    playUrl?.let { url ->
        val st = rememberExoPlayerState(url)
        Column(
            Modifier.fillMaxSize().padding(32.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.Center,
        ) {
            Icon(
                Icons.Outlined.MusicNote,
                contentDescription = null,
                tint = Color.White.copy(alpha = 0.85f),
                modifier = Modifier.size(64.dp),
            )
            Text(
                "录音",
                style = MaterialTheme.typography.titleMedium,
                color = Color.White,
                modifier = Modifier.padding(top = 12.dp),
            )
            // 三态互斥（对齐视频页）：加载态只有转圈（占播放钮同轴槽位，READY 接管不跳布局）；
            // 就绪态才渲染控件组；错误态点按钮重试（重签）。
            when {
                st.buffering -> AppMediaLoading(text = "加载中", modifier = Modifier.padding(top = 36.dp))
                st.error != null -> Column(
                    horizontalAlignment = Alignment.CenterHorizontally,
                    modifier = Modifier.padding(top = 36.dp),
                ) {
                    Text(
                        st.error!!,
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.error,
                    )
                    Text(
                        "文件可能已损坏或格式不受支持",
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(top = 4.dp),
                    )
                }
                else -> {
                    // 进度条（可拖动跳转）；就绪后 duration 已就绪，无需灰显兜底
                    val dur = st.durationMs.coerceAtLeast(1)
                    val fraction = (st.positionMs.toFloat() / dur).coerceIn(0f, 1f)
                    androidx.compose.foundation.layout.Box(
                        Modifier
                            .padding(top = 20.dp)
                            .fillMaxWidth()
                            .height(28.dp)
                            .pointerInput(st.durationMs) {
                                detectTapGestures { offset ->
                                    val f = (offset.x / size.width).coerceIn(0f, 1f)
                                    st.player?.seekTo((dur * f).toLong())
                                }
                            },
                        contentAlignment = Alignment.CenterStart,
                    ) {
                        androidx.compose.foundation.layout.Box(
                            Modifier.fillMaxWidth().height(3.dp)
                                .clip(RoundedCornerShape(2.dp))
                                .background(Color.White.copy(alpha = 0.25f))
                        )
                        androidx.compose.foundation.layout.Box(
                            Modifier.fillMaxWidth(fraction).height(3.dp)
                                .clip(RoundedCornerShape(2.dp))
                                .background(MaterialTheme.colorScheme.primary)
                        )
                    }
                    Row(
                        Modifier.fillMaxWidth().padding(top = 4.dp),
                        horizontalArrangement = Arrangement.SpaceBetween,
                    ) {
                        Text(fmtMs(st.positionMs), style = MaterialTheme.typography.labelMedium, color = Color.White.copy(alpha = 0.7f))
                        Text(fmtMs(dur), style = MaterialTheme.typography.labelMedium, color = Color.White.copy(alpha = 0.7f))
                    }
                    // 播放 / 暂停
                    Box(
                        Modifier
                            .padding(top = 20.dp)
                            .size(64.dp)
                            .clip(CircleShape)
                            .background(MaterialTheme.colorScheme.primary)
                            .clickable {
                                val p = st.player ?: return@clickable
                                if (p.isPlaying) p.pause() else p.play()
                            },
                        contentAlignment = Alignment.Center,
                    ) {
                        Icon(
                            if (st.playing) Icons.Filled.Pause else Icons.Filled.PlayArrow,
                            contentDescription = if (st.playing) "暂停" else "播放",
                            tint = Color.White,
                            modifier = Modifier.size(32.dp),
                        )
                    }
                }
            }
        }
    }
}

/** 毫秒 → mm:ss。 */
private fun fmtMs(ms: Int): String {
    val total = ms / 1000
    return "%d:%02d".format(total / 60, total % 60)
}

/** 可缩放照片：双指缩放（1x–4x）+ 单指平移，双击复位；缩放时不平移页面。 */
@Composable
private fun ZoomableImage(url: String?) {
    var scale by remember { mutableFloatStateOf(1f) }
    var offset by remember { mutableStateOf(androidx.compose.ui.geometry.Offset.Zero) }
    // 手势里要读最新值：pointerInput(Unit) 的闭包只捕获首次组合的值，
    // 用 rememberUpdatedState 才能拿到当前缩放（否则放大后拖动无效、双击也不复位）。
    val scaleState = rememberUpdatedState(scale)
    val offsetState = rememberUpdatedState(offset)

    AsyncImage(
        model = url,
        contentDescription = null,
        contentScale = ContentScale.Fit,
        modifier = Modifier
            .fillMaxSize()
            .pointerInput(Unit) {
                // 未放大时**不消费**：水平滑动必须让给 HorizontalPager，否则翻页失效。
                // 已放大时消费拖动做平移。detectTransformGestures 无法条件消费，
                // 故用 awaitEachGesture 手写：先判断缩放，再决定消费或放行。
                awaitEachGesture {
                    awaitFirstDown(requireUnconsumed = false)
                    do {
                        val event = awaitPointerEvent()
                        val zoomChange = event.calculateZoom()
                        val panChange = event.calculatePan()
                        val zooming = zoomChange != 1f
                        val cur = scaleState.value
                        if (cur > 1f || zooming) {
                            // 放大中或双指：消费
                            val next = (cur * zoomChange).coerceIn(1f, 4f)
                            scale = next
                            offset = if (next > 1f) offsetState.value + panChange else androidx.compose.ui.geometry.Offset.Zero
                            event.changes.forEach { it.consume() }
                        }
                        // 未放大且非双指：不消费，pager 接管水平滑动
                    } while (event.changes.any { it.pressed })
                }
            }
            .graphicsLayer {
                scaleX = scale
                scaleY = scale
                translationX = offset.x
                translationY = offset.y
            }
            .pointerInput(Unit) {
                detectTapGestures(
                    onDoubleTap = {
                        // 双击缩放：1x <-> 2.5x 切换；放大态双击复位
                        if (scale > 1f) {
                            scale = 1f
                            offset = androidx.compose.ui.geometry.Offset.Zero
                        } else {
                            scale = 2.5f
                        }
                    },
                )
            },
    )
}
