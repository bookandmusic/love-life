package com.bookandmusic.lovelife.appnative.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.imePadding
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import coil.compose.AsyncImage
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material.icons.filled.Star
import androidx.compose.material.icons.outlined.FavoriteBorder
import androidx.compose.material.icons.outlined.ChatBubbleOutline
import androidx.compose.material.icons.outlined.Place
import androidx.compose.material.icons.outlined.StarBorder
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.withStyle
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.bookandmusic.lovelife.appnative.data.CommentDto
import com.bookandmusic.lovelife.appnative.data.CommentsRepository
import com.bookandmusic.lovelife.appnative.data.PostMediaUi
import com.bookandmusic.lovelife.appnative.data.PostUi
import com.bookandmusic.lovelife.appnative.data.PostsRepository
import com.bookandmusic.lovelife.appnative.data.errText
import com.bookandmusic.lovelife.appnative.designsystem.atoms.appClickAndLongPress
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppActionSheet
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppMenuItem
import com.bookandmusic.lovelife.appnative.designsystem.atoms.onAppLongPress
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Download
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppAvatar
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppCard
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppChip
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppConfirmSheet
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppEmptyState
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppMediaGrid
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppRowDivider
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppMediaItem
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppToast
import com.bookandmusic.lovelife.appnative.designsystem.theme.AppCardBodyStyle
import com.bookandmusic.lovelife.appnative.designsystem.theme.LocalAppColors
import com.bookandmusic.lovelife.appnative.util.TimeFmt
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.launch

/**
 * 时光 = 动态流：整页滚动，每条动态 = 白色卡
 * （头像块 + 昵称 + 时间 + 心情胶囊 → 正文 → 媒体网格 → 地点 → 赞/藏/评 → 展开评论）。
 * 触底自动分页；媒体点击进全屏查看器；本人动态长按可删。
 */
@Composable
fun TimelineScreen(
    contentPadding: PaddingValues,
    onOpenPublish: () -> Unit,
    /** 目标动态 id（首页回忆卡 / 通知跳转）：自动加载到该条并滚动定位 + 临时高亮 */
    focusPostId: Long? = null,
    /** 定位完成后回调，壳层据此清空目标，避免再次进入时重复定位 */
    onFocusConsumed: () -> Unit = {},
) {
    var posts by remember { mutableStateOf<List<PostUi>?>(null) }
    var total by remember { mutableIntStateOf(0) }
    var page by remember { mutableIntStateOf(1) }
    var hasMore by remember { mutableStateOf(true) }
    var loadingMore by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf("") }
    var toast by remember { mutableStateOf<String?>(null) }
    var viewer by remember { mutableStateOf<Pair<List<PostMediaUi>, Int>?>(null) }
    // 查看器当前展示的动态：查看器内长按媒体要复用卡片的媒体动作单（保存 / 作者删除）
    var viewerPost by remember { mutableStateOf<PostUi?>(null) }
    var pendingDelete by remember { mutableStateOf<PostUi?>(null) }
    var menuTarget by remember { mutableStateOf<PostUi?>(null) }
    var mediaMenu by remember { mutableStateOf<Pair<PostUi, Int>?>(null) }
    // 待确认的单媒体删除（动态 + 目标媒体）
    var pendingMediaDelete by remember { mutableStateOf<Pair<PostUi, PostMediaUi>?>(null) }
    var myId by remember { mutableStateOf<Long?>(null) }
    // 我的昵称：点赞后本地合并「xxx 赞了」名单用（服务端不回传名单，客户端自己算）
    var myName by remember { mutableStateOf("") }
    var roleById by remember { mutableStateOf<Map<Long, String>>(emptyMap()) }
    val scope = rememberCoroutineScope()
    val listState = rememberLazyListState()
    // 媒体「保存到本机」共享启动器（屏幕级声明：不在条件分支里反复挂载）
    val saveMedia = rememberMediaSaver({ toast = it }) { mediaMenu = null }
    // 定位目标：滚动到该条后短暂高亮
    var highlightId by remember { mutableStateOf<Long?>(null) }

    suspend fun loadFirst() {
        runCatching { PostsRepository.posts(1) }
            .onSuccess { res ->
                posts = res.items
                total = res.total
                page = 1
                hasMore = res.items.size >= 20
                error = ""
            }
            .onFailure {
                error = errText(it, "加载失败")
                if (posts == null) posts = emptyList()
            }
    }

    LaunchedEffect(Unit) {
        runCatching { com.bookandmusic.lovelife.appnative.data.AuthRepository.restore() }.getOrNull()?.let {
            myId = it.id
            myName = it.nickname
        }
        roleById = runCatching {
            com.bookandmusic.lovelife.appnative.data.SpaceRepository.info().members.associate { it.id to it.role }
        }.getOrDefault(emptyMap())
        loadFirst()
    }

    // 分页：滚动到接近底部时加载下一页。
    // 用 snapshotFlow 而非 LaunchedEffect(posts)：后者把 posts 当 key，每次追加都会重启 effect，
    // 在「仍处于底部」时反复触发同一页请求，表现为不加载或重复加载。
    LaunchedEffect(listState) {
        snapshotFlow {
            val info = listState.layoutInfo
            val last = info.visibleItemsInfo.lastOrNull()?.index ?: -1
            if (info.totalItemsCount == 0) false else last >= info.totalItemsCount - 3
        }
            .distinctUntilChanged()
            .collect { atBottom ->
                if (!atBottom) return@collect
                if (loadingMore || !hasMore || posts == null) return@collect
                loadingMore = true
                runCatching { PostsRepository.posts(page + 1) }
                    .onSuccess { res ->
                        if (res.items.isEmpty()) {
                            hasMore = false
                        } else {
                            // 按 id 去重：并发/重试时不会把同一条塞两次
                            val existing = (posts ?: emptyList()).map { it.id }.toSet()
                            posts = (posts ?: emptyList()) + res.items.filter { it.id !in existing }
                            page += 1
                            total = res.total
                            if (res.items.size < 20) hasMore = false
                        }
                    }
                    .onFailure { toast = it.message ?: "加载失败" }
                loadingMore = false
            }
    }

    // 定位目标动态：逐页加载直到包含它，再滚动到该条并高亮 2 秒
    LaunchedEffect(focusPostId, posts != null) {
        val target = focusPostId ?: return@LaunchedEffect
        // 必须等首屏数据到位（posts != null），否则会在空列表上从第 2 页开始翻、漏掉第 1 页
        if (posts == null) return@LaunchedEffect
        // 已在列表里：直接定位
        var index = posts.orEmpty().indexOfFirst { it.id == target }
        // 不在列表里：继续翻页直到找到或没有更多
        while (index < 0 && hasMore) {
            val next = runCatching { PostsRepository.posts(page + 1) }.getOrNull() ?: break
            if (next.items.isEmpty()) {
                hasMore = false
                break
            }
            val existing = (posts ?: emptyList()).map { it.id }.toSet()
            posts = (posts ?: emptyList()) + next.items.filter { it.id !in existing }
            page += 1
            total = next.total
            if (next.items.size < 20) hasMore = false
            index = posts.orEmpty().indexOfFirst { it.id == target }
        }
        if (index >= 0) {
            // +1 是因为列表首项是 SectionSpacer
            listState.animateScrollToItem(index + 1)
            highlightId = target
            kotlinx.coroutines.delay(2000)
            highlightId = null
        }
        onFocusConsumed()
    }

    Box(Modifier.fillMaxSize()) {
        LazyColumn(
            state = listState,
            modifier = Modifier.fillMaxSize(),
            contentPadding = contentPadding,
        ) {
            item {
                SectionSpacer()
            }
            if (error.isNotEmpty() && posts.isNullOrEmpty()) {
                item {
                    AppCard(modifier = Modifier.padding(horizontal = 16.dp)) {
                        Text(error, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.error)
                    }
                }
            }
            if (posts != null && posts!!.isEmpty() && error.isEmpty()) {
                item {
                    AppEmptyState(
                        modifier = Modifier.padding(horizontal = 16.dp),
                        title = "动态圈还空着",
                        hint = "发一条动态，记录此刻的心情和日常。",
                        icon = Icons.Outlined.ChatBubbleOutline,
                        cta = "发动态",
                        onCta = onOpenPublish,
                    )
                }
            }
            items(posts.orEmpty(), key = { it.id }) { post ->
                // 微信式：整页只有一层左右 16dp，动态之间用细分隔线（无卡片容器）
                Column(
                    Modifier
                        .fillMaxWidth()
                        .background(
                            if (highlightId == post.id) {
                                MaterialTheme.colorScheme.primary.copy(alpha = 0.08f)
                            } else {
                                androidx.compose.ui.graphics.Color.Transparent
                            }
                        )
                ) {
                    AppRowDivider(modifier = Modifier.padding(horizontal = 16.dp))
                    Box(Modifier.padding(horizontal = 16.dp)) {
                    val mine = myId != null && myId == post.authorId
                    PostCard(
                            post = post,
                            role = roleById[post.authorId],
                            onToggleLike = {
                                scope.launch {
                                    val liked = runCatching { PostsRepository.toggleLike(post.id) }.getOrElse {
                                        toast = "操作失败：${errText(it, "")}"
                                        return@launch
                                    }
                                    posts = posts?.map {
                                        if (it.id == post.id) {
                                            // 名单与数字同步更新，否则「xxx 赞了」要等整页重拉才变
                                            val likers = if (myName.isNotEmpty()) {
                                                if (liked) (it.likers + myName).distinct()
                                                else it.likers.filter { n -> n != myName }
                                            } else {
                                                it.likers
                                            }
                                            it.copy(
                                                liked = liked,
                                                likeCount = it.likeCount + if (liked) 1 else -1,
                                                likers = likers,
                                            )
                                        } else {
                                            it
                                        }
                                    }
                                }
                            },
                            onOpenMedia = { index -> viewerPost = post; viewer = post.media to index },
                            onToast = { toast = it },
                            onDelete = if (mine) ({ pendingDelete = post }) else null,
                            onMediaLongPress = { p, i -> mediaMenu = p to i },
                            onContentLongPress = { p -> menuTarget = p },
                        )
                    }
                }
            }
            if (loadingMore) {
                item { FootNote("加载中…") }
            } else if (!hasMore && (posts?.size ?: 0) >= 20) {
                item { FootNote("没有更多了") }
            }
            item { Spacer(Modifier.size(8.dp)) }
        }

        viewer?.let { (media, index) ->
            MediaViewer(
                media = media,
                initialIndex = index,
                onDismiss = { viewer = null; viewerPost = null },
                onLongPress = { m ->
                    // 与动态卡媒体长按同一动作单：保存到本机 / 作者移入回收站
                    val idx = media.indexOf(m)
                    if (idx >= 0) viewerPost?.let { vp -> mediaMenu = vp to idx }
                },
            )
        }

        // 长按动作单：动态卡的上下文操作
        // 长按动作单：只列**当前用户真的能做的事**。
        // 别人的动态没有可执行项时不弹（弹一个只有「取消」的空菜单是 bug）。
        menuTarget?.let { target ->
            val mine = myId != null && myId == target.authorId
            val context = androidx.compose.ui.platform.LocalContext.current
            val items = buildList {
                // 通用：有正文时可复制（别人的动态也有可执行项，长按不会「点了没反应」）
                if (target.content.isNotEmpty()) {
                    add(
                        AppMenuItem(label = "复制文字", icon = Icons.Outlined.ContentCopy) {
                            val cm = context.getSystemService(android.content.Context.CLIPBOARD_SERVICE)
                                as android.content.ClipboardManager
                            cm.setPrimaryClip(android.content.ClipData.newPlainText("动态", target.content))
                            toast = "已复制"
                        }
                    )
                }
                // 仅作者：移入回收站
                if (mine) {
                    add(
                        AppMenuItem(
                            label = "移入回收站",
                            icon = Icons.Outlined.Delete,
                            danger = true,
                        ) { pendingDelete = target }
                    )
                }
            }
            if (items.isEmpty()) {
                menuTarget = null
            } else {
                AppActionSheet(
                    visible = true,
                    title = target.author,
                    items = items,
                    onDismiss = { menuTarget = null },
                )
            }
        }

        // 长按整卡动作单（作者：删除；非作者：无可操作 → 不弹）
        // 已在上方 menuTarget 处处理

        mediaMenu?.let { (target, index) ->
            val media = target.media.getOrNull(index)
            val mine = myId != null && myId == target.authorId
            AppActionSheet(
                visible = true,
                title = when (media?.kind) {
                    "video" -> "视频"
                    "audio" -> "录音"
                    else -> "照片"
                },
                items = buildList {
                    add(
                        AppMenuItem(label = "保存到本机", icon = Icons.Outlined.Download) {
                            if (media != null) saveMedia(media)
                        }
                    )
                    // 仅作者：单独删除这个媒体文件（若删后动态无媒体且无正文，动态一并删除）
                    if (mine && media != null) {
                        add(
                            AppMenuItem(label = "删除", icon = Icons.Outlined.Delete, danger = true) {
                                pendingMediaDelete = target to media
                            }
                        )
                    }
                },
                onDismiss = { mediaMenu = null },
            )
        }

        // 删除单个媒体：确认文案要如实说明「会不会连动态一起没」
        pendingMediaDelete?.let { (target, media) ->
            val willVanish = target.media.size == 1 && target.content.isBlank()
            AppConfirmSheet(
                visible = true,
                title = "删除这个${when (media.kind) { "video" -> "视频"; "audio" -> "录音"; else -> "照片" }}",
                message = if (willVanish) {
                    "这是这条动态里唯一的媒体，且没有文字，删除后整条动态都会移入回收站。仅后台可恢复。"
                } else {
                    "只删除这个文件，动态的其他内容保留。仅后台可恢复。"
                },
                confirmText = "删除",
                onConfirm = {
                    scope.launch {
                        runCatching { PostsRepository.deleteMedia(media.id) }
                            .onSuccess { postGone ->
                                if (postGone) {
                                    posts = posts?.filter { it.id != target.id }
                                    total = (total - 1).coerceAtLeast(0)
                                    viewer = null
                                    viewerPost = null
                                } else {
                                    posts = posts?.map { p ->
                                        if (p.id == target.id) p.copy(media = p.media.filter { it.id != media.id }) else p
                                    }
                                    // 查看器持打开时的列表快照：不同步的话被删照片在全屏里还活着
                                    viewer?.let { (vMedia, vIdx) ->
                                        if (viewerPost?.id == target.id) {
                                            val next = vMedia.filter { it.id != media.id }
                                            if (next.isEmpty()) {
                                                viewer = null
                                                viewerPost = null
                                            } else {
                                                viewer = next to vIdx.coerceIn(0, next.size - 1)
                                                viewerPost = viewerPost?.copy(media = next)
                                            }
                                        }
                                    }
                                }
                                toast = "已移入回收站"
                            }
                            .onFailure { toast = "删除失败：${errText(it, "")}" }
                        pendingMediaDelete = null
                    }
                },
                onDismiss = { pendingMediaDelete = null },
            )
        }

        pendingDelete?.let { target ->
            AppConfirmSheet(
                visible = true,
                title = "移入回收站",
                message = "这条动态将移入回收站，媒体一并删除。仅后台可恢复。",
                confirmText = "移入回收站",
                onConfirm = {
                    scope.launch {
                        runCatching { PostsRepository.delete(target.id) }
                            .onSuccess {
                                posts = posts?.filter { it.id != target.id }
                                total = (total - 1).coerceAtLeast(0)
                                toast = "已移入回收站"
                                // 删除可能来自查看器内的媒体长按：动态没了，查看器一并关闭
                                viewer = null
                                viewerPost = null
                            }
                            .onFailure { toast = "删除失败：${errText(it, "")}" }
                        pendingDelete = null
                    }
                },
                onDismiss = { pendingDelete = null },
            )
        }

        AppToast(text = toast, onDismiss = { toast = null })
    }
}

/** 卡片顶部留白（区块与顶栏之间）。 */
@Composable
private fun SectionSpacer() {
    Spacer(Modifier.size(4.dp))
}

@Composable
private fun FootNote(text: String) {
    Text(
        text,
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.fillMaxWidth().padding(vertical = 14.dp),
        textAlign = androidx.compose.ui.text.style.TextAlign.Center,
    )
}

/**
 * 一条动态（微信朋友圈式）：**无卡片容器**，只靠页面左右 16dp + 动态之间的细分隔线分隔。
 * 结构：头像 44 方形 ｜ 昵称 → 正文 15/24 → 媒体 → 「时间 · 地点 · ••」→ 点赞行 → 评论。
 */
@Composable
private fun PostCard(
    post: PostUi,
    role: String?,
    onToggleLike: () -> Unit,
    onOpenMedia: (Int) -> Unit,
    onToast: (String) -> Unit,
    /** 作者可删；null = 不可删（别人的动态） */
    onDelete: (() -> Unit)? = null,
    /** 长按媒体格（保存到本机 / 删除） */
    onMediaLongPress: ((PostUi, Int) -> Unit)? = null,
    /** 长按正文（复制 / 本人：删除动作单） */
    onContentLongPress: ((PostUi) -> Unit)? = null,
) {
    val colors = LocalAppColors.current
    var showActions by remember(post.id) { mutableStateOf(false) }
    var commentTarget by remember(post.id) { mutableStateOf(false) }

    Column(Modifier.fillMaxWidth().padding(vertical = 12.dp)) {
        Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            // 左列：方形头像（微信式，贴顶）；有头像图时覆盖首字色块
            AppAvatar(
                post.author,
                role,
                size = 44.dp,
                square = true,
                image = if (post.authorAvatar.isNotEmpty()) {
                    {
                        AsyncImage(
                            model = post.authorAvatar,
                            contentDescription = post.author,
                            contentScale = ContentScale.Crop,
                            modifier = Modifier.fillMaxSize(),
                        )
                    }
                } else null,
            )
            Column(Modifier.weight(1f)) {
                // 昵称
                Text(
                    post.author,
                    style = MaterialTheme.typography.titleMedium,
                    color = colors.authorName,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                // 正文 15/24；长按 = 复制/删除动作单（热区只在本体，不含头像/时间行/评论区）
                if (post.content.isNotEmpty()) {
                    Text(
                        post.content,
                        style = MaterialTheme.typography.bodyLarge.copy(lineHeight = MaterialTheme.typography.bodyLarge.fontSize * 1.6.toFloat()),
                        color = MaterialTheme.colorScheme.onSurface,
                        modifier = Modifier
                            .padding(top = 6.dp)
                            .appClickAndLongPress(
                                onClick = {},
                                onLongPress = { onContentLongPress?.invoke(post) },
                            ),
                    )
                }
                // 媒体
                if (post.media.isNotEmpty()) {
                    AppMediaGrid(
                        items = post.media.map { it.toGridItem() },
                        onOpen = onOpenMedia,
                        modifier = Modifier.padding(top = 8.dp).fillMaxWidth(),
                        onLongPress = { index -> onMediaLongPress?.invoke(post, index) },
                    )
                }
                // 时间 · 地点 · ••（同一行）
                Row(
                    Modifier.padding(top = 10.dp).fillMaxWidth(),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(
                        TimeFmt.dateTime(post.createdAt),
                        style = MaterialTheme.typography.labelMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = 1,
                    )
                    post.locationName?.let { loc ->
                        // 不设 weight：地点只占自身宽度，剩余空间由下面的 Spacer 独占，
                        // 否则 weight(1f, fill=false) 会和 Spacer 抢空间，把「••」挤到中间。
                        Text(
                            "  $loc",
                            style = MaterialTheme.typography.labelMedium,
                            color = colors.authorName,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
                    Spacer(Modifier.weight(1f))
                    // 「••」：折叠点赞与评论入口（微信式）
                    Box(
                        Modifier
                            .clip(RoundedCornerShape(8.dp))
                            .background(MaterialTheme.colorScheme.surfaceContainerHigh)
                            .clickable { showActions = !showActions }
                            .padding(horizontal = 10.dp, vertical = 5.dp),
                    ) {
                        Text(
                            "••",
                            style = MaterialTheme.typography.labelLarge,
                            color = colors.authorName,
                        )
                    }
                }
                // 折叠操作：赞 / 评论
                if (showActions) {
                    Row(
                        Modifier.padding(top = 8.dp),
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        ActionPill(
                            icon = if (post.liked) Icons.Filled.Favorite else Icons.Outlined.FavoriteBorder,
                            label = if (post.liked) "取消" else "赞",
                            active = post.liked,
                            onClick = onToggleLike,
                        )
                        ActionPill(
                            icon = Icons.Outlined.ChatBubbleOutline,
                            label = "评论",
                            active = false,
                            onClick = { commentTarget = true },
                        )
                    }
                }
                // 点赞人（「张三、李四 赞了」）+ 评论，同一个灰底块
                CommentLikeBlock(
                    post = post,
                    onToast = onToast,
                    openComposer = commentTarget,
                    onComposerClosed = { commentTarget = false },
                )
            }
        }
    }
}

/** 折叠操作胶囊（赞 / 评论）。 */
@Composable
private fun ActionPill(
    icon: androidx.compose.ui.graphics.vector.ImageVector,
    label: String,
    active: Boolean,
    onClick: () -> Unit,
) {
    val colors = LocalAppColors.current
    val tint = if (active) colors.primaryDeep else MaterialTheme.colorScheme.onSurfaceVariant
    Row(
        Modifier
            .clip(RoundedCornerShape(999.dp))
            .background(MaterialTheme.colorScheme.surfaceContainerHigh)
            .clickable { onClick() }
            .padding(horizontal = 12.dp, vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(5.dp),
    ) {
        Icon(icon, contentDescription = null, tint = tint, modifier = Modifier.size(14.dp))
        Text(label, style = MaterialTheme.typography.labelMedium, color = tint)
    }
}

/**
 * 点赞行 + 评论区（微信朋友圈式）：同一个浅灰圆角块。
 * 结构：♥ 张三、李四 → 细线 → 评论列表（全部显示）。输入框不常驻，点「评论」从底部弹出。
 */
@Composable
private fun CommentLikeBlock(
    post: PostUi,
    onToast: (String) -> Unit,
    openComposer: Boolean,
    onComposerClosed: () -> Unit,
) {
    var comments by remember(post.id) { mutableStateOf<List<CommentDto>?>(null) }
    var pending by remember(post.id) { mutableStateOf<CommentDto?>(null) }
    var pendingDelete by remember(post.id) { mutableStateOf<CommentDto?>(null) }
    var myId by remember { mutableStateOf<Long?>(null) }
    val scope = rememberCoroutineScope()
    val colors = LocalAppColors.current

    LaunchedEffect(post.id) {
        myId = runCatching { com.bookandmusic.lovelife.appnative.data.AuthRepository.restore()?.id }.getOrNull()
        comments = runCatching { CommentsRepository.list(post.id) }.getOrNull()
    }

    val hasLikes = post.likers.isNotEmpty()
    val hasComments = !comments.isNullOrEmpty()

    // 空态（无赞无评）只免渲染灰底内容块；弹层与动作单照常渲染——
    // 否则「点评论弹输入框」在空态动态上永远不出现（曾表现为「先点赞才能评论」的 bug）
    if (hasLikes || hasComments) Column(
        Modifier
            .padding(top = 10.dp)
            .fillMaxWidth()
            .clip(RoundedCornerShape(8.dp))
            .background(MaterialTheme.colorScheme.surfaceContainerHigh)
            .padding(horizontal = 12.dp, vertical = 10.dp),
    ) {
        // 点赞行：「张三、李四 赞了」
        if (hasLikes) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Icon(
                    Icons.Filled.Favorite,
                    contentDescription = null,
                    tint = colors.authorName,
                    modifier = Modifier.size(14.dp),
                )
                Text(
                    post.likers.joinToString("、") + " 赞了",
                    style = MaterialTheme.typography.bodySmall,
                    color = colors.authorName,
                    modifier = Modifier.padding(start = 6.dp),
                )
            }
        }
        if (hasLikes && hasComments) {
            AppRowDivider(modifier = Modifier.padding(vertical = 8.dp))
        }
        // 评论：全部显示（微信式）；长按 = 复制 / 本人：删除（单探测器，与外层手势构造性互斥）
        comments.orEmpty().forEach { c ->
            val mine = myId != null && myId == c.author_id
            CommentRow(
                c,
                modifier = Modifier.appClickAndLongPress(
                    onClick = {},
                    onLongPress = { pending = c },
                ),
            )
        }
    }

    // 底部弹出输入框（点「评论」触发）
    if (openComposer) {
        CommentComposer(
            postId = post.id,
            onDismiss = onComposerClosed,
            onPosted = { created ->
                comments = comments.orEmpty() + created
                onComposerClosed()
            },
            onToast = onToast,
        )
    }

    // 长按评论的动作单：复制（人人可用）；删除仅作者
    pending?.let { target ->
        val mine = myId != null && myId == target.author_id
        val ctx = androidx.compose.ui.platform.LocalContext.current
        AppActionSheet(
            visible = true,
            title = target.author,
            items = buildList {
                add(
                    AppMenuItem(label = "复制文字", icon = Icons.Outlined.ContentCopy) {
                        val cm = ctx.getSystemService(android.content.Context.CLIPBOARD_SERVICE)
                            as android.content.ClipboardManager
                        cm.setPrimaryClip(android.content.ClipData.newPlainText("评论", target.content))
                        onToast("已复制")
                        pending = null
                    }
                )
                if (mine) {
                    add(
                        AppMenuItem(label = "删除", icon = Icons.Outlined.Delete, danger = true) {
                            pendingDelete = target
                            pending = null
                        }
                    )
                }
            },
            onDismiss = { pending = null },
        )
    }

    // 删除确认（从动作单进入）
    pendingDelete?.let { target ->
        AppConfirmSheet(
            visible = true,
            title = "移入回收站",
            message = "这条评论将移入回收站，仅后台可恢复。",
            confirmText = "移入回收站",
            onConfirm = {
                scope.launch {
                    // 成功才本地移除：失败也移除的话，重进页面评论会「复活」
                    runCatching { CommentsRepository.delete(target.id) }
                        .onSuccess {
                            comments = comments?.filter { it.id != target.id }
                            pendingDelete = null
                        }
                        .onFailure {
                            onToast("删除失败：${errText(it, "")}")
                            pendingDelete = null
                        }
                }
            },
            onDismiss = { pendingDelete = null },
        )
    }
}

/**
 * 评论输入框：底部固定，跟随键盘（`imePadding`）。这是微信朋友圈的形态——
 * 点「评论」后输入框从底部升起、键盘同时弹出，发送或点空白处即收起。
 */
@Composable
private fun CommentComposer(
    postId: Long,
    onDismiss: () -> Unit,
    onPosted: (CommentDto) -> Unit,
    onToast: (String) -> Unit,
) {
    var text by remember { mutableStateOf("") }
    var sending by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val colors = LocalAppColors.current
    val focus = remember { FocusRequester() }
    LaunchedEffect(Unit) { focus.requestFocus() }

    androidx.compose.ui.window.Dialog(
        onDismissRequest = onDismiss,
        properties = androidx.compose.ui.window.DialogProperties(usePlatformDefaultWidth = false),
    ) {
        Box(
            Modifier
                .fillMaxSize()
                .padding(bottom = 0.dp)
                .imePadding(),
            contentAlignment = Alignment.BottomCenter,
        ) {
            // 点空白处收起
            Box(
                Modifier
                    .fillMaxSize()
                    .clickable(
                        interactionSource = remember { androidx.compose.foundation.interaction.MutableInteractionSource() },
                        indication = null,
                    ) { onDismiss() }
            )
            Row(
                Modifier
                    .fillMaxWidth()
                    .background(MaterialTheme.colorScheme.surface)
                    .padding(horizontal = 12.dp, vertical = 10.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Box(
                    Modifier
                        .weight(1f)
                        .clip(RoundedCornerShape(999.dp))
                        .background(MaterialTheme.colorScheme.surfaceContainerHigh)
                        .padding(horizontal = 14.dp, vertical = 10.dp),
                ) {
                    if (text.isEmpty()) {
                        Text(
                            "说点什么…",
                            style = MaterialTheme.typography.bodyMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    BasicTextField(
                        value = text,
                        onValueChange = { text = it },
                        textStyle = MaterialTheme.typography.bodyMedium.copy(color = MaterialTheme.colorScheme.onSurface),
                        cursorBrush = SolidColor(MaterialTheme.colorScheme.primary),
                        modifier = Modifier.fillMaxWidth().focusRequester(focus),
                    )
                }
                Box(
                    Modifier
                        .clip(RoundedCornerShape(999.dp))
                        .background(
                            if (text.isBlank() || sending) {
                                MaterialTheme.colorScheme.primary.copy(alpha = 0.4f)
                            } else {
                                MaterialTheme.colorScheme.primary
                            }
                        )
                        .clickable(enabled = text.isNotBlank() && !sending) {
                            val content = text.trim()
                            if (content.isEmpty()) return@clickable
                            sending = true
                            scope.launch {
                                runCatching { CommentsRepository.create(postId, content) }
                                    .onSuccess { onPosted(it) }
                                    .onFailure { onToast(errText(it, "评论失败")) }
                                sending = false
                            }
                        }
                        .padding(horizontal = 16.dp, vertical = 10.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    Text("发送", style = MaterialTheme.typography.bodyMedium, color = colors.onSolid)
                }
            }
        }
    }
}

@Composable
private fun CommentRow(c: CommentDto, modifier: Modifier = Modifier) {
    val colors = LocalAppColors.current
    // 昵称嵌在正文前（微信朋友圈式），不用固定宽列把内容挤到右边
    Text(
        buildAnnotatedString {
            withStyle(SpanStyle(color = colors.authorName, fontWeight = FontWeight.Medium)) {
                append(c.author)
            }
            append("：")
            withStyle(SpanStyle(color = MaterialTheme.colorScheme.onSurface)) {
                append(c.content)
            }
        },
        style = MaterialTheme.typography.bodySmall,
        modifier = modifier.fillMaxWidth().padding(vertical = 4.dp),
    )
}

internal fun com.bookandmusic.lovelife.appnative.data.PostMediaUi.toGridItem(): AppMediaItem =
    AppMediaItem(kind = kind, thumbUrl = thumbUrl, durationText = durationText)

