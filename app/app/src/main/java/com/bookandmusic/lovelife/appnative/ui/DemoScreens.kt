package com.bookandmusic.lovelife.appnative.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Cake
import androidx.compose.material.icons.outlined.Folder
import androidx.compose.material.icons.outlined.FolderOpen
import androidx.compose.material.icons.outlined.PhotoLibrary
import androidx.compose.material.icons.outlined.MusicNote
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import coil.compose.AsyncImage
import com.bookandmusic.lovelife.appnative.data.AlbumDto
import com.bookandmusic.lovelife.appnative.data.AlbumsRepository
import com.bookandmusic.lovelife.appnative.data.PostsRepository
import com.bookandmusic.lovelife.appnative.data.errText
import com.bookandmusic.lovelife.appnative.data.Channel
import com.bookandmusic.lovelife.appnative.data.PostMediaUi
import com.bookandmusic.lovelife.appnative.data.TimelineItemDto
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppCard
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppConfirmSheet
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppEmptyState
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppRowDivider
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppIconBadge
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppIconBadgeSize
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppPillButton
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppSegmentedControl
import com.bookandmusic.lovelife.appnative.designsystem.atoms.appClickAndLongPress
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppActionSheet
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppMenuItem
import com.bookandmusic.lovelife.appnative.designsystem.atoms.onAppLongPress
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Download
import androidx.compose.material.icons.outlined.RemoveCircleOutline
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppToast
import com.bookandmusic.lovelife.appnative.designsystem.theme.LocalAppColors
import com.bookandmusic.lovelife.appnative.util.TimeFmt
import kotlinx.coroutines.launch

/**
 * 相册 = 媒体库：分段控件（相册 / 时间轴）→
 * 相册视图 = 2 列封面卡网格（封面 4:3 + 数量角标 + 名称）；时间轴 = 按月分组 3 列方格 + 触底分页。
 */
@Composable
fun AlbumsScreen(
    contentPadding: PaddingValues,
    onOpenAlbum: (Long, String) -> Unit,
    onOpenAlbumForm: () -> Unit,
    onOpenPublish: () -> Unit,
) {
    var view by remember { mutableIntStateOf(0) }
    var albums by remember { mutableStateOf<List<AlbumDto>?>(null) }
    var timeline by remember { mutableStateOf<List<TimelineItemDto>?>(null) }
    var tlTotal by remember { mutableIntStateOf(0) }
    var tlPage by remember { mutableIntStateOf(1) }
    var tlHasMore by remember { mutableStateOf(true) }
    var tlLoading by remember { mutableStateOf(false) }
    var toast by remember { mutableStateOf<String?>(null) }
    var viewer by remember { mutableStateOf<Pair<List<PostMediaUi>, Int>?>(null) }
    var mediaMenuT by remember { mutableStateOf<PostMediaUi?>(null) }
    var pendingAlbum by remember { mutableStateOf<AlbumDto?>(null) }
    var menuTarget by remember { mutableStateOf<AlbumDto?>(null) }
    val tlState = rememberLazyListState()
    val scope = rememberCoroutineScope()
    val saveMediaT = rememberMediaSaver({ toast = it }) { mediaMenuT = null }
    // 当前用户 id：媒体长按菜单「删除」项仅作者可见（保存不受限）
    var myId by remember { mutableStateOf<Long?>(null) }
    var pendingMediaDeleteT by remember { mutableStateOf<PostMediaUi?>(null) }

    LaunchedEffect(Unit) {
        myId = runCatching { com.bookandmusic.lovelife.appnative.data.AuthRepository.restore()?.id }.getOrNull()
        albums = runCatching { AlbumsRepository.albums() }.getOrNull()
        runCatching { AlbumsRepository.timeline(1) }.onSuccess { res ->
            timeline = res.items
            tlTotal = res.total
            tlHasMore = res.has_more
        }.onFailure { timeline = emptyList() }
    }

    val nearEnd by remember {
        androidx.compose.runtime.derivedStateOf {
            val info = tlState.layoutInfo
            val last = info.visibleItemsInfo.lastOrNull()?.index ?: 0
            last >= info.totalItemsCount - 4
        }
    }
    LaunchedEffect(nearEnd, view, tlHasMore, tlLoading) {
        if (view != 1 || !nearEnd || !tlHasMore || tlLoading) return@LaunchedEffect
        tlLoading = true
        runCatching { AlbumsRepository.timeline(tlPage + 1) }.onSuccess { res ->
            if (res.items.isEmpty()) {
                tlHasMore = false
            } else {
                timeline = (timeline ?: emptyList()) + res.items
                tlPage += 1
                tlTotal = res.total
                tlHasMore = res.has_more
            }
        }
        tlLoading = false
    }

    Box(Modifier.fillMaxSize()) {
        Column(Modifier.fillMaxSize()) {
            // 分段控件固定（不随内容滚动）
            Column(Modifier.padding(top = contentPadding.calculateTopPadding())) {
                AppSegmentedControl(
                    options = listOf("相册", "时间轴"),
                    selected = view,
                    onSelect = { view = it },
                    modifier = Modifier.padding(horizontal = 16.dp).padding(top = 4.dp, bottom = 10.dp),
                )
            }
            LazyColumn(
                state = tlState,
                modifier = Modifier.fillMaxWidth().weight(1f),
                contentPadding = PaddingValues(bottom = contentPadding.calculateBottomPadding()),
            ) {
                if (view == 0) {
                    item { AlbumGrid(albums, onOpenAlbum, onLongPress = { menuTarget = it }, onOpenAlbumForm = onOpenAlbumForm) }
                } else {
                    item {
                        Row(
                            Modifier.fillMaxWidth().padding(horizontal = 16.dp).padding(bottom = 12.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Text(
                                "共 ${tlTotal} 个媒体",
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    }
                    timelineGridItems(
                        timeline = timeline,
                        onOpen = { list, index -> viewer = list to index },
                        onLongPress = { m -> mediaMenuT = m },
                    )
                    if (tlLoading) {
                        item { FootNote("加载中…") }
                    } else if (!tlHasMore && !timeline.isNullOrEmpty()) {
                        item { FootNote("没有更多了") }
                    }
                }
                item { Spacer(Modifier.height(8.dp)) }
            }
        }

        viewer?.let { (media, index) ->
            MediaViewer(
                media = media,
                initialIndex = index,
                onDismiss = { viewer = null },
                // 相册 tab·时间轴网格：长按媒体 = 保存到本机（该视图不带作者信息，删除仍在动态侧）
                onLongPress = { m -> mediaMenuT = m },
            )
        }

        mediaMenuT?.let { m ->
            val mine = myId != null && m.authorId != 0L && myId == m.authorId
            AppActionSheet(
                visible = true,
                title = when (m.kind) {
                    "video" -> "视频"
                    "audio" -> "录音"
                    else -> "照片"
                },
                items = buildList {
                    add(AppMenuItem(label = "保存到本机", icon = Icons.Outlined.Download) { saveMediaT(m) })
                    // 仅作者：删除文件（媒体=动态作者的数据）
                    if (mine) {
                        add(
                            AppMenuItem(label = "删除", icon = Icons.Outlined.Delete, danger = true) {
                                pendingMediaDeleteT = m
                                mediaMenuT = null
                            }
                        )
                    }
                },
                onDismiss = { mediaMenuT = null },
            )
        }

        // 时间轴侧媒体删除确认 + 本地移除（时间轴与查看器共用入口）
        pendingMediaDeleteT?.let { m ->
            AppConfirmSheet(
                visible = true,
                title = "删除这个${when (m.kind) { "video" -> "视频"; "audio" -> "录音"; else -> "照片" }}",
                message = "只删除这个文件；若所属动态已无其他媒体且无文字，整条动态会一并移入回收站。仅后台可恢复。",
                confirmText = "删除",
                onConfirm = {
                    scope.launch {
                        runCatching { PostsRepository.deleteMedia(m.id) }
                            .onSuccess {
                                timeline = timeline?.filter { it.id != m.id }
                                viewer = null
                                toast = "已移入回收站"
                                pendingMediaDeleteT = null
                            }
                            .onFailure {
                                toast = "删除失败：${errText(it, "")}"
                                pendingMediaDeleteT = null
                            }
                    }
                },
                onDismiss = { pendingMediaDeleteT = null },
            )
        }

        menuTarget?.let { target ->
            AppActionSheet(
                visible = true,
                title = target.name,
                items = listOf(
                    AppMenuItem(label = "移入回收站", icon = Icons.Outlined.Delete, danger = true) {
                        pendingAlbum = target
                    }
                ),
                onDismiss = { menuTarget = null },
            )
        }

        pendingAlbum?.let { target ->
            AppConfirmSheet(
                visible = true,
                title = "移入回收站",
                message = "「${target.name}」将移入回收站，相册内媒体回到时间轴（不删媒体）。",
                confirmText = "移入回收站",
                onConfirm = {
                    scope.launch {
                        runCatching { AlbumsRepository.delete(target.id) }
                            .onSuccess {
                                albums = albums?.filter { it.id != target.id }
                                toast = "已移入回收站"
                            }
                            .onFailure { toast = "删除失败：${errText(it, "")}" }
                        pendingAlbum = null
                    }
                },
                onDismiss = { pendingAlbum = null },
            )
        }

        AppToast(text = toast, onDismiss = { toast = null })
    }
}

@Composable
private fun FootNote(text: String) {
    Text(
        text,
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        textAlign = TextAlign.Center,
        modifier = Modifier.fillMaxWidth().padding(vertical = 14.dp),
    )
}

/** 相册视图：2 列封面卡网格。 */
@Composable
private fun AlbumGrid(
    albums: List<AlbumDto>?,
    onOpen: (Long, String) -> Unit,
    onLongPress: (AlbumDto) -> Unit,
    onOpenAlbumForm: () -> Unit,
) {
    when {
        albums == null -> Box(Modifier.fillMaxWidth().padding(24.dp), contentAlignment = Alignment.Center) {
            Text(
                "加载中…",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        albums.isEmpty() -> AppEmptyState(
            title = "相册集合还空着",
            hint = "按旅行、日常或家人建一个，把照片归归类。",
            icon = Icons.Outlined.FolderOpen,
            cta = "新建相册",
            onCta = onOpenAlbumForm,
        )
        else -> Column(Modifier.padding(horizontal = 16.dp)) {
            // 瀑布流：按封面真实宽高比排版（服务端下发 cover_width/height）；缺失时回退 4:3
            // 真瀑布流：两列独立累加高度（按真实封面比例估算），next 项放入当前较矮的一列。
            // 用 Row + 两个 Column 实现（Compose 无内置瀑布流；两列场景 Column 方案最简且不抖）。
            val columns = remember(albums) { splitIntoBalancedColumns(albums) }
            Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                columns.forEach { columnAlbums ->
                    Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                        columnAlbums.forEach { album ->
                            // 点击与长按用**同一个手势处理器**，否则长按后抬手仍会触发点击（进详情）
                            Box(
                                Modifier.appClickAndLongPress(
                                    onClick = { onOpen(album.id, album.name) },
                                    onLongPress = { onLongPress(album) },
                                )
                            ) {
                                AlbumCard(
                                    album = album,
                                    aspect = albumCoverRatio(album),
                                )
                            }
                        }
                    }
                }
            }
        }
    }
}

/**
 * 封面宽高比（宽/高）：服务端下发原始尺寸；缺失回退 4:3。
 * 限制 0.6–1.6，避免极端长图撑破列宽。
 */
private fun albumCoverRatio(album: AlbumDto): Float =
    if (album.cover_width > 0 && album.cover_height > 0) {
        (album.cover_width.toFloat() / album.cover_height).coerceIn(0.6f, 1.6f)
    } else {
        4f / 3f
    }

/**
 * 按估算高度把相册分配到两列（贪心：每次放入当前较矮的列）。
 * 高度估算 = 封面高度（列宽 / 比例）+ 名称行 44dp + 间距 12dp。
 */
private fun splitIntoBalancedColumns(albums: List<AlbumDto>): List<List<AlbumDto>> {
    val heights = FloatArray(2)
    val cols = listOf(mutableListOf<AlbumDto>(), mutableListOf<AlbumDto>())
    albums.forEach { album ->
        val target = if (heights[0] <= heights[1]) 0 else 1
        cols[target].add(album)
        // 列宽约 (屏宽 - 2*16 - 12) / 2 ≈ 165dp
        heights[target] += 165f / albumCoverRatio(album) + 44f + 12f
    }
    return cols.map { it.toList() }
}

@Composable
private fun AlbumCard(album: AlbumDto, aspect: Float = 4f / 3f) {
    val colors = LocalAppColors.current
    // 无卡片容器：封面图就是主体（圆角 8），名称压在图片下方——图片不被「框」住也就不会变小
    Column(Modifier.fillMaxWidth()) {
        Box(
            Modifier
                .fillMaxWidth()
                .aspectRatio(aspect)
                .clip(RoundedCornerShape(8.dp))
        ) {
            if (!album.cover_url.isNullOrEmpty()) {
                AsyncImage(
                    model = Channel.base().trimEnd('/') + album.cover_url,
                    contentDescription = album.name,
                    contentScale = ContentScale.Crop,
                    modifier = Modifier.fillMaxSize(),
                )
            } else {
                Box(
                    Modifier
                        .fillMaxSize()
                        .background(MaterialTheme.colorScheme.primary.copy(alpha = 0.14f)),
                    contentAlignment = Alignment.Center,
                ) {
                    Icon(
                        Icons.Outlined.PhotoLibrary,
                        contentDescription = null,
                        tint = colors.primaryDeep,
                        modifier = Modifier.size(22.dp),
                    )
                }
            }
            Text(
                album.item_count.toString(),
                style = MaterialTheme.typography.labelMedium,
                color = Color.White,
                modifier = Modifier
                    .align(Alignment.BottomEnd)
                    .padding(6.dp)
                    .clip(RoundedCornerShape(8.dp))
                    .background(Color.Black.copy(alpha = 0.5f))
                    .padding(horizontal = 6.dp, vertical = 1.dp),
            )
        }
        Text(
            album.name,
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurface,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.padding(top = 8.dp),
        )
    }
}

/** 时间轴：按月分组，月份标题 + 3 列方格。 */
private fun androidx.compose.foundation.lazy.LazyListScope.timelineGridItems(
    timeline: List<TimelineItemDto>?,
    onOpen: (List<PostMediaUi>, Int) -> Unit,
    onLongPress: (PostMediaUi) -> Unit = {},
) {
    if (timeline == null) {
        item {
            Box(Modifier.fillMaxWidth().padding(24.dp), contentAlignment = Alignment.Center) {
                Text(
                    "加载中…",
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
        return
    }
    if (timeline.isEmpty()) {
        item {
            AppEmptyState(
                title = "相册还空着",
                hint = "发动态时带上照片，就会自动收进这里。",
                icon = Icons.Outlined.PhotoLibrary,
            )
        }
        return
    }
    val mediaUi = timeline.map { it.toMediaUi() }
    val groups = timeline.groupBy { it.month_key ?: (it.taken_at?.take(7) ?: "unknown") }
        .toSortedMap(compareByDescending { it })
    var offset = 0
    groups.forEach { (month, group) ->
        val groupStart = offset
        offset += group.size
        item(key = "h_$month") {
            Text(
                TimeFmt.monthLabel(month),
                style = MaterialTheme.typography.labelLarge,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 6.dp, bottom = 8.dp),
            )
        }
        group.chunked(3).forEachIndexed { rowIndex, rowItems ->
            item(key = "r_${month}_$rowIndex") {
                Row(
                    Modifier.fillMaxWidth().padding(horizontal = 16.dp).padding(bottom = 3.dp),
                    horizontalArrangement = Arrangement.spacedBy(3.dp),
                ) {
                    rowItems.forEachIndexed { colIndex, m ->
                        val globalIndex = groupStart + rowIndex * 3 + colIndex
                        TimelineCell(
                            item = m,
                            modifier = Modifier.weight(1f),
                            onClick = { onOpen(mediaUi, globalIndex) },
                            onLongPress = { onLongPress(mediaUi[globalIndex]) },
                        )
                    }
                    repeat(3 - rowItems.size) { Spacer(Modifier.weight(1f)) }
                }
            }
        }
    }
}

@Composable
private fun TimelineCell(
    item: TimelineItemDto,
    modifier: Modifier,
    onClick: () -> Unit,
    onLongPress: () -> Unit = {},
) {
    val colors = LocalAppColors.current
    val url = item.thumb_url?.let { Channel.base().trimEnd('/') + it }
    Box(
        modifier
            .aspectRatio(1f)
            .clip(RoundedCornerShape(2.dp))
            .background(MaterialTheme.colorScheme.surfaceContainerHigh)
            // 点击 + 长按单一手势处理器（构造性互斥，避免双触发）
            .appClickAndLongPress(onClick = onClick, onLongPress = onLongPress),
        contentAlignment = Alignment.Center,
    ) {
        if (url != null) {
            AsyncImage(
                model = url,
                contentDescription = null,
                contentScale = ContentScale.Crop,
                modifier = Modifier.fillMaxSize(),
            )
        }
        when (item.kind) {
            "video" -> Row(
                Modifier
                    .align(Alignment.BottomEnd)
                    .padding(5.dp)
                    .clip(RoundedCornerShape(8.dp))
                    .background(Color.Black.copy(alpha = 0.55f))
                    .padding(horizontal = 5.dp, vertical = 2.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(3.dp),
            ) {
                Icon(Icons.Filled.PlayArrow, contentDescription = null, tint = colors.onSolid, modifier = Modifier.size(10.dp))
                item.duration?.let {
                    Text(durationText(it), style = MaterialTheme.typography.labelMedium, color = colors.onSolid)
                }
            }
            "audio" -> Box(
                Modifier
                    .align(Alignment.BottomEnd)
                    .padding(5.dp)
                    .size(18.dp)
                    .clip(RoundedCornerShape(999.dp))
                    .background(colors.semBlue.copy(alpha = 0.75f)),
                contentAlignment = Alignment.Center,
            ) {
                Icon(Icons.Outlined.MusicNote, contentDescription = null, tint = colors.onSolid, modifier = Modifier.size(10.dp))
            }
        }
    }
}

private fun durationText(seconds: Double): String {
    val total = seconds.toInt()
    val m = total / 60
    val s = total % 60
    return if (m > 0) "$m:${s.toString().padStart(2, '0')}" else "0:${s.toString().padStart(2, '0')}"
}

private fun TimelineItemDto.toMediaUi(): PostMediaUi = PostMediaUi(
    id = id,
    kind = kind,
    thumbUrl = thumb_url?.let { Channel.base().trimEnd('/') + it },
    url = url?.let { Channel.base().trimEnd('/') + it },
    durationText = duration?.let { durationText(it) },
    authorId = author_id,
)

/** 相册详情（二级页）：3 列方格 + 末位「+」格；点击进全屏查看器。 */
@Composable
fun AlbumDetailScreen(
    albumId: Long,
    albumName: String,
    contentPadding: PaddingValues,
    onBack: () -> Unit,
) {
    var media by remember { mutableStateOf<List<TimelineItemDto>?>(null) }
    var viewerIndex by remember { mutableIntStateOf(-1) }
    var mediaMenuA by remember { mutableStateOf<PostMediaUi?>(null) }
    // 待确认删除的媒体（删除文件本身，与「移出相册」不同）
    var deletingMedia by remember { mutableStateOf<TimelineItemDto?>(null) }
    var toast by remember { mutableStateOf<String?>(null) }
    val saveMediaA = rememberMediaSaver({ toast = it }) { mediaMenuA = null }
    var showPicker by remember { mutableStateOf(false) }
    var removingMedia by remember { mutableStateOf<TimelineItemDto?>(null) }
    var allTimeline by remember { mutableStateOf<List<TimelineItemDto>>(emptyList()) }
    var picked by remember { mutableStateOf<List<Long>>(emptyList()) }
    // 当前用户 id：「删除文件」仅作者可见（保存 / 移出相册不受限）
    var myId by remember { mutableStateOf<Long?>(null) }
    val scope = rememberCoroutineScope()

    // 顶栏「+」= 添加照片（页面自己挂到壳层动作位）
    val addPhotos = {
        showPicker = true
        picked = emptyList()
        scope.launch {
            // 时间轴单页上限 50：按 has_more 翻页拉全，否则选图器只能看到最新一页
            allTimeline = runCatching { AlbumsRepository.timelineAll() }.getOrDefault(emptyList())
        }
        Unit
    }
    TopBarIconAction(Icons.Outlined.Add, "添加照片", onClick = addPhotos)

    suspend fun reload() {
        media = runCatching { AlbumsRepository.albumMedia(albumId).media }.getOrNull()
    }
    LaunchedEffect(albumId) {
        myId = runCatching { com.bookandmusic.lovelife.appnative.data.AuthRepository.restore()?.id }.getOrNull()
        reload()
    }

    val list = media
    Box(Modifier.fillMaxSize()) {
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = contentPadding,
        ) {
            if (list == null) {
                item {
                    Box(Modifier.fillMaxWidth().padding(24.dp), contentAlignment = Alignment.Center) {
                        Text(
                            "加载中…",
                            style = MaterialTheme.typography.bodyMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
            } else if (list.isEmpty()) {
                item {
                    AppEmptyState(
                        title = "这个相册还空着",
                        hint = "从时间轴挑几张照片放进来吧。",
                        icon = Icons.Outlined.PhotoLibrary,
                    )
                }
            } else {
                items(list.chunked(3), key = { row -> row.joinToString("-") { it.id.toString() } }) { rowItems ->
                    Row(
                        Modifier.fillMaxWidth().padding(horizontal = 16.dp).padding(bottom = 3.dp),
                        horizontalArrangement = Arrangement.spacedBy(3.dp),
                    ) {
                        rowItems.forEach { m ->
                            TimelineCell(
                                item = m,
                                modifier = Modifier.weight(1f),
                                onClick = { viewerIndex = list.indexOf(m) },
                            )
                        }
                        repeat(3 - rowItems.size) { Spacer(Modifier.weight(1f)) }
                    }
                }
            }
            item { Spacer(Modifier.height(8.dp)) }
        }

        if (viewerIndex >= 0 && list != null) {
            MediaViewer(
                media = list.map { it.toMediaUi() },
                initialIndex = viewerIndex,
                onDismiss = { viewerIndex = -1 },
                onLongPress = { m -> mediaMenuA = m },
            )
        }
        AppToast(text = toast, onDismiss = { toast = null })

        mediaMenuA?.let { m ->
            val mine = myId != null && m.authorId != 0L && myId == m.authorId
            AppActionSheet(
                visible = true,
                title = when (m.kind) {
                    "video" -> "视频"
                    "audio" -> "录音"
                    else -> "照片"
                },
                items = buildList {
                    add(AppMenuItem(label = "保存到本机", icon = Icons.Outlined.Download) { saveMediaA(m) })
                    // 移出相册：照片回到时间轴，文件不删（相册是共同资产，双方可操作）
                    add(
                        AppMenuItem(label = "移出相册", icon = Icons.Outlined.RemoveCircleOutline) {
                            removingMedia = list?.firstOrNull { it.toMediaUi() == m }
                        }
                    )
                    // 删除文件本身（仅作者；若删后动态无媒体且无正文，动态一并删除）
                    if (mine) {
                        add(
                            AppMenuItem(label = "删除", icon = Icons.Outlined.Delete, danger = true) {
                                // 取 DTO 以拿到 post_id（判断是否该动态最后的媒体）
                                deletingMedia = list?.firstOrNull { it.id == m.id }
                            }
                        )
                    }
                },
                onDismiss = { mediaMenuA = null },
            )
        }

        deletingMedia?.let { m ->
            val samePostCount = list?.count { it.post_id == m.post_id } ?: 1
            AppConfirmSheet(
                visible = true,
                title = "删除这个${when (m.kind) { "video" -> "视频"; "audio" -> "录音"; else -> "照片" }}",
                message = if (samePostCount <= 1) {
                    "这是所属动态里唯一的媒体，若该动态也没有文字，删除后整条动态都会移入回收站。仅后台可恢复。"
                } else {
                    "只删除这个文件，动态的其他内容保留。仅后台可恢复。"
                },
                confirmText = "删除",
                onConfirm = {
                    scope.launch {
                        runCatching { PostsRepository.deleteMedia(m.id) }
                            .onSuccess {
                                deletingMedia = null
                                viewerIndex = -1
                                reload()
                                toast = "已移入回收站"
                            }
                            .onFailure { toast = "删除失败：${errText(it, "")}" }
                    }
                },
                onDismiss = { deletingMedia = null },
            )
        }

        removingMedia?.let { target ->
            AppConfirmSheet(
                visible = true,
                title = "移出相册",
                message = "这张照片会回到时间轴，不会删除。",
                confirmText = "移出相册",
                onConfirm = {
                    scope.launch {
                        runCatching { AlbumsRepository.removePhotos(albumId, listOf(target.id)) }
                            .onSuccess {
                                removingMedia = null
                                reload()
                                toast = "已移出相册"
                            }
                            .onFailure { toast = "移出失败：${it.message}" }
                    }
                },
                onDismiss = { removingMedia = null },
            )
        }

        // 从时间轴添加照片：多选未归属媒体 → 加入相册（已归属的灰显禁选）
        if (showPicker) {
            AddPhotosSheet(
                items = allTimeline,
                currentAlbumId = albumId,
                picked = picked,
                onToggle = { id -> picked = if (picked.contains(id)) picked - id else picked + id },
                onConfirm = {
                    scope.launch {
                        runCatching { AlbumsRepository.addPhotos(albumId, picked) }
                            .onSuccess {
                                showPicker = false
                                picked = emptyList()
                                reload()
                                toast = "已加入相册"
                            }
                            .onFailure { toast = "加入失败：${it.message}" }
                    }
                },
                onDismiss = {
                    showPicker = false
                    picked = emptyList()
                },
            )
        }
    }
}

/** 「从时间轴添加照片」弹层：3 列方格多选，已归属其它相册的媒体灰显。 */
@Composable
private fun AddPhotosSheet(
    items: List<TimelineItemDto>,
    currentAlbumId: Long,
    picked: List<Long>,
    onToggle: (Long) -> Unit,
    onConfirm: () -> Unit,
    onDismiss: () -> Unit,
) {
    val colors = LocalAppColors.current
    com.bookandmusic.lovelife.appnative.designsystem.atoms.AppSheet(
        visible = true,
        onDismiss = onDismiss,
        title = "从时间轴添加",
        confirmText = if (picked.isEmpty()) "加入相册" else "加入相册（${picked.size}）",
        confirmEnabled = picked.isNotEmpty(),
        onConfirm = onConfirm,
    ) {
        androidx.compose.foundation.layout.Box(Modifier.height(360.dp)) {
            LazyColumn(Modifier.fillMaxSize()) {
                items(items.chunked(3), key = { row -> row.joinToString("-") { it.id.toString() } }) { rowItems ->
                    Row(
                        Modifier.fillMaxWidth().padding(bottom = 3.dp),
                        horizontalArrangement = Arrangement.spacedBy(3.dp),
                    ) {
                        rowItems.forEach { m ->
                            val locked = m.album_id != null && m.album_id != currentAlbumId
                            val selected = picked.contains(m.id)
                            Box(
                                Modifier
                                    .weight(1f)
                                    .aspectRatio(1f)
                                    .clip(RoundedCornerShape(2.dp))
                                    .background(MaterialTheme.colorScheme.surfaceContainerHigh)
                                    .clickable(enabled = !locked) { onToggle(m.id) },
                            ) {
                                val url = m.thumb_url?.let { Channel.base().trimEnd('/') + it }
                                if (url != null) {
                                    AsyncImage(
                                        model = url,
                                        contentDescription = null,
                                        contentScale = ContentScale.Crop,
                                        modifier = Modifier.fillMaxSize().then(
                                            if (locked) Modifier.background(Color.Black.copy(alpha = 0.45f)) else Modifier
                                        ),
                                    )
                                }
                                if (locked) {
                                    Text(
                                        m.album_name ?: "已在别的相册",
                                        style = MaterialTheme.typography.labelMedium,
                                        color = Color.White,
                                        maxLines = 1,
                                        overflow = TextOverflow.Ellipsis,
                                        modifier = Modifier
                                            .align(Alignment.BottomCenter)
                                            .fillMaxWidth()
                                            .background(Color.Black.copy(alpha = 0.5f))
                                            .padding(horizontal = 3.dp, vertical = 1.dp),
                                    )
                                }
                                if (selected) {
                                    Box(
                                        Modifier
                                            .align(Alignment.TopEnd)
                                            .padding(5.dp)
                                            .size(20.dp)
                                            .clip(RoundedCornerShape(999.dp))
                                            .background(MaterialTheme.colorScheme.primary),
                                        contentAlignment = Alignment.Center,
                                    ) {
                                        Icon(
                                            Icons.Filled.Check,
                                            contentDescription = null,
                                            tint = colors.onSolid,
                                            modifier = Modifier.size(13.dp),
                                        )
                                    }
                                }
                            }
                        }
                        repeat(3 - rowItems.size) { Spacer(Modifier.weight(1f)) }
                    }
                }
                if (items.isEmpty()) {
                    item {
                        Text(
                            "时间轴还没有可添加的媒体",
                            style = MaterialTheme.typography.bodyMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            modifier = Modifier.fillMaxWidth().padding(20.dp),
                            textAlign = TextAlign.Center,
                        )
                    }
                }
            }
        }
    }
}

/** 纪念日列表页（二级）：全量纪念日，长按删除，行右侧天数。 */
@Composable
fun AnniversaryListScreen(
    contentPadding: PaddingValues,
    onBack: () -> Unit,
    onAdd: () -> Unit,
    /** 作为一级 tab 常驻时不显示返回（二级页形态才显示） */
    inTab: Boolean = false,
) {
    var items by remember { mutableStateOf<List<com.bookandmusic.lovelife.appnative.data.AnniversaryDto>?>(null) }
    var pendingDelete by remember { mutableStateOf<com.bookandmusic.lovelife.appnative.data.AnniversaryDto?>(null) }
    var menuTargetA by remember { mutableStateOf<com.bookandmusic.lovelife.appnative.data.AnniversaryDto?>(null) }
    var toast by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val colors = LocalAppColors.current

    suspend fun reload() {
        items = runCatching { com.bookandmusic.lovelife.appnative.data.AnniversariesRepository.list() }.getOrNull()
    }
    LaunchedEffect(Unit) { reload() }

    Box(Modifier.fillMaxSize()) {
        LazyColumn(Modifier.fillMaxSize(), contentPadding = contentPadding) {
            when (val list = items) {
                null -> item {
                    Box(Modifier.fillMaxWidth().padding(24.dp), contentAlignment = Alignment.Center) {
                        Text(
                            "加载中…",
                            style = MaterialTheme.typography.bodyMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
                else -> {
                    if (list.isEmpty()) {
                        item {
                            AppEmptyState(
                                title = "记下重要日子",
                                hint = "生日、初见、在一起的日子，都会准时提醒你们。",
                                icon = Icons.Outlined.Cake,
                                cta = "记下第一个纪念日",
                                onCta = onAdd,
                            )
                        }
                    }
                    items(list, key = { it.id }) { a ->
                        val meta = com.bookandmusic.lovelife.appnative.util.anniversaryMeta(a)
                        // 去卡片容器：行 + 细线分隔
                        Column(
                            Modifier.fillMaxWidth().appClickAndLongPress(
                                onClick = {},
                                onLongPress = { menuTargetA = a },
                            )
                        ) {
                                Row(
                                    Modifier.padding(horizontal = 16.dp, vertical = 14.dp),
                                        verticalAlignment = Alignment.CenterVertically,
                                        horizontalArrangement = Arrangement.spacedBy(14.dp),
                                    ) {
                                        AppIconBadge(
                                            icon = Icons.Outlined.Cake,
                                            tint = colors.semGold,
                                            background = colors.semGoldSoft,
                                            size = AppIconBadgeSize.Lg,
                                        )
                                        Column(Modifier.weight(1f)) {
                                            Row(
                                                verticalAlignment = Alignment.CenterVertically,
                                                horizontalArrangement = Arrangement.spacedBy(8.dp),
                                            ) {
                                                Text(
                                                    a.name,
                                                    style = MaterialTheme.typography.titleLarge,
                                                    color = MaterialTheme.colorScheme.onSurface,
                                                    maxLines = 1,
                                                    overflow = TextOverflow.Ellipsis,
                                                )
                                                Text(
                                                    if (a.type == "lunar") "农历" else "阳历",
                                                    style = MaterialTheme.typography.labelMedium,
                                                    color = if (a.type == "lunar") {
                                                        colors.semGold
                                                    } else {
                                                        MaterialTheme.colorScheme.onSurfaceVariant
                                                    },
                                                )
                                            }
                                            Text(
                                                meta.anniv,
                                                style = MaterialTheme.typography.bodySmall,
                                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                                                maxLines = 1,
                                                overflow = TextOverflow.Ellipsis,
                                                modifier = Modifier.padding(top = 4.dp),
                                            )
                                            Text(
                                                meta.next,
                                                style = MaterialTheme.typography.labelMedium,
                                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                                                maxLines = 1,
                                                overflow = TextOverflow.Ellipsis,
                                            )
                                        }
                                        Row(verticalAlignment = Alignment.Bottom) {
                                            Text(
                                                if (meta.invalid) "—" else meta.days.toString(),
                                                style = MaterialTheme.typography.headlineSmall,
                                                color = if (meta.hot) colors.semGold else MaterialTheme.colorScheme.onSurfaceVariant,
                                            )
                                            Text(
                                                "天",
                                                style = MaterialTheme.typography.labelMedium,
                                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                                                modifier = Modifier.padding(start = 1.dp, bottom = 3.dp),
                                            )
                                        }
                                    }
                                AppRowDivider(modifier = Modifier.padding(start = 16.dp))
                        }
                    }
                }
            }
            item { Spacer(Modifier.height(8.dp)) }
        }

        menuTargetA?.let { target ->
            AppActionSheet(
                visible = true,
                title = target.name,
                items = listOf(
                    AppMenuItem(label = "移入回收站", icon = Icons.Outlined.Delete, danger = true) {
                        pendingDelete = target
                    }
                ),
                onDismiss = { menuTargetA = null },
            )
        }

        pendingDelete?.let { target ->
            AppConfirmSheet(
                visible = true,
                title = "移入回收站",
                message = "「${target.name}」将移入回收站，提醒一并停止。",
                confirmText = "移入回收站",
                onConfirm = {
                    scope.launch {
                        runCatching { com.bookandmusic.lovelife.appnative.data.AnniversariesRepository.delete(target.id) }
                            .onSuccess {
                                items = items?.filter { it.id != target.id }
                                toast = "已移入回收站"
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
