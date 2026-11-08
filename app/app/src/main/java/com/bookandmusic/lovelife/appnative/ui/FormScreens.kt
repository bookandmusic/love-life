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
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material.icons.filled.Star
import androidx.compose.material.icons.outlined.Cake
import androidx.compose.material.icons.outlined.ChatBubbleOutline
import androidx.compose.material.icons.outlined.Notifications
import androidx.compose.material.icons.outlined.PhotoLibrary
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppIconBadge
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppIconBadgeSize
import com.bookandmusic.lovelife.appnative.util.TimeFmt
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.MusicNote
import androidx.compose.material.icons.outlined.Place
import androidx.compose.material.icons.outlined.Search
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
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import coil.compose.AsyncImage
import com.bookandmusic.lovelife.appnative.data.AlbumsRepository
import com.bookandmusic.lovelife.appnative.data.AnniversariesRepository
import com.bookandmusic.lovelife.appnative.data.GeocodeCandidate
import com.bookandmusic.lovelife.appnative.data.MediaRepository
import com.bookandmusic.lovelife.appnative.data.PointsRepository
import com.bookandmusic.lovelife.appnative.data.PostsRepository
import com.bookandmusic.lovelife.appnative.data.errText
import com.bookandmusic.lovelife.appnative.util.formatCoord
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppButton
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppButtonVariant
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppEmptyState
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppChip
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppRowDivider
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppSegmentedControl
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppFormGap
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppFormGroup
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppFormRow
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppFormTapRow
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppFormRowDivider
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppFormTextBlock
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppDatePickerSheet
import com.bookandmusic.lovelife.appnative.designsystem.atoms.LunarEngine
import com.bookandmusic.lovelife.appnative.designsystem.atoms.LunarMonth
import com.bookandmusic.lovelife.appnative.designsystem.atoms.PickedDate
import com.bookandmusic.lovelife.appnative.util.LunarCalendar
import com.bookandmusic.lovelife.appnative.util.lunarDayName
import com.bookandmusic.lovelife.appnative.util.lunarMonthName
import java.time.LocalDate
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppToast
import com.bookandmusic.lovelife.appnative.designsystem.theme.LocalAppColors
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/** 媒体类型判定：优先 contentResolver 的 MIME 前缀，无类型时按扩展名兜底（与服务端分类一致）。 */
private fun android.content.Context.pickedMediaKind(uri: android.net.Uri): String {
    val type = contentResolver.getType(uri).orEmpty().lowercase()
    return when {
        type.startsWith("image") -> "photo"
        type.startsWith("video") -> "video"
        type.startsWith("audio") -> "audio"
        else -> {
            val name = contentResolver.query(uri, null, null, null, null)?.use { cursor ->
                val idx = cursor.getColumnIndex(android.provider.OpenableColumns.DISPLAY_NAME)
                if (idx >= 0 && cursor.moveToFirst()) cursor.getString(idx) else null
            }.orEmpty()
            when (name.substringAfterLast('.', "").lowercase()) {
                "jpg", "jpeg", "png", "webp", "gif", "heic" -> "photo"
                "mp4", "mov", "webm" -> "video"
                "m4a", "mp3", "wav", "aac" -> "audio"
                else -> "photo"
            }
        }
    }
}

/** 已选媒体（本机 uri + 上传态；kind: photo/video/audio，与服务端媒体分类一致）。 */
private data class PickedMedia(
    val uri: android.net.Uri,
    val kind: String,
    /** 以下三项只经 copy() 更新（列表项整体替换），保持不可变 */
    val uploading: Boolean = false,
    val token: String? = null,
    val failed: Boolean = false,
)

/**
 * 发布动态：正文 textarea + 心情胶囊 + 媒体网格（≤9，末格「+」）+
 * 地点选择；提交时才上传媒体，逐文件进度，个别失败可重传。
 */
@Composable
fun PublishScreen(
    contentPadding: PaddingValues,
    onBack: () -> Unit,
    onPublished: () -> Unit,
) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var content by remember { mutableStateOf("") }
    var mood by remember { mutableStateOf("") }
    var customMood by remember { mutableStateOf("") }
    var locationName by remember { mutableStateOf("") }
    var locationLat by remember { mutableStateOf<Double?>(null) }
    var locationLng by remember { mutableStateOf<Double?>(null) }
    var media by remember { mutableStateOf<List<PickedMedia>>(emptyList()) }
    var submitting by remember { mutableStateOf(false) }
    var toast by remember { mutableStateOf<String?>(null) }
    var showLocation by remember { mutableStateOf(false) }
    var showMood by remember { mutableStateOf(false) }
    var geoQuery by remember { mutableStateOf("") }
    var candidates by remember { mutableStateOf<List<GeocodeCandidate>>(emptyList()) }
    var searching by remember { mutableStateOf(false) }
    var geoError by remember { mutableStateOf("") }

    // 边输边搜：停顿 350ms 自动查（中文输入法的组合态在 IME 提交后才进 query，不会打断）
    LaunchedEffect(geoQuery) {
        val q = geoQuery.trim()
        if (q.isEmpty()) {
            candidates = emptyList(); geoError = ""; searching = false
            return@LaunchedEffect
        }
        delay(350)
        searching = true
        runCatching { PointsRepository.geocode(q) }
            .onSuccess { candidates = it; geoError = "" }
            .onFailure { candidates = emptyList(); geoError = errText(it, "搜索失败") }
        searching = false
    }

    // 系统文件选择器三类通选（图片/视频/音频）——Photo Picker 只覆盖可视媒体，音频进不了列表
    val picker = rememberLauncherForActivityResult(
        contract = ActivityResultContracts.OpenMultipleDocuments()
    ) { uris ->
        if (uris.isNotEmpty()) {
            val added = uris.map { uri ->
                PickedMedia(uri = uri, kind = context.pickedMediaKind(uri))
            }
            media = (media + added).take(9)
        }
    }

    val effectiveMood = customMood.ifEmpty { mood }
    val canSubmit = !submitting && (content.isNotBlank() || media.isNotEmpty() || effectiveMood.isNotEmpty() || locationName.isNotEmpty())

    Box(Modifier.fillMaxSize()) {
        Column(
            Modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .padding(top = contentPadding.calculateTopPadding())
                .padding(bottom = contentPadding.calculateBottomPadding())
        ) {
            // 发布按钮挂到顶栏右侧
            TopBarAction(
                text = if (submitting) "发布中…" else "发布",
                enabled = canSubmit,
                onClick = {
                        submitting = true
                        scope.launch {
                            try {
                                val tokens = mutableListOf<String>()
                                media.forEachIndexed { index, item ->
                                    if (item.token != null) {
                                        tokens.add(item.token)
                                        return@forEachIndexed
                                    }
                                    media = media.toMutableList().also { it[index] = item.copy(uploading = true) }
                                    runCatching { MediaRepository.upload(context, item.uri) }
                                        .onSuccess { up ->
                                            tokens.add(up.media_token)
                                            media = media.toMutableList().also {
                                                it[index] = item.copy(uploading = false, token = up.media_token)
                                            }
                                        }
                                        .onFailure {
                                            media = media.toMutableList().also {
                                                it[index] = item.copy(uploading = false, failed = true)
                                            }
                                            throw it
                                        }
                                }
                                PostsRepository.create(
                                    content = content.trim(),
                                    mood = effectiveMood,
                                    locationName = locationName,
                                    mediaTokens = tokens,
                                    // 选了坐标就传 location 对象（服务端据此同步点位）；纯文本地名走 location_name
                                    location = if (locationLat != null && locationLng != null) {
                                        com.bookandmusic.lovelife.appnative.data.PostLocation(
                                            name = locationName,
                                            lat = locationLat,
                                            lng = locationLng,
                                        )
                                    } else null,
                                )
                                onPublished()
                            } catch (e: Exception) {
                                toast = errText(e, "发布失败")
                            } finally {
                                submitting = false
                            }
                        }
                },
            )

            // ===== 微信式分组：正文白块（含媒体九宫格，同块连续贴边） =====
            AppFormTextBlock(
                value = content,
                onValueChange = { content = it },
                placeholder = "这一刻的想法…（可留空，直接传照片也行）",
                minHeight = 120.dp,
            )
            // 媒体九宫格：与正文同一白块（空媒体时只渲染末格「+」）
            Column(
                    Modifier
                        .fillMaxWidth()
                        .background(MaterialTheme.colorScheme.surface)
                        .padding(horizontal = 16.dp)
                        .padding(bottom = 12.dp),
                ) {
                    media.chunked(3).forEach { rowItems ->
                        Row(
                            Modifier.fillMaxWidth().padding(bottom = 3.dp),
                            horizontalArrangement = Arrangement.spacedBy(3.dp),
                        ) {
                            rowItems.forEach { item ->
                                Box(Modifier.weight(1f).aspectRatio(1f)) {
                                    AsyncImage(
                                        model = item.uri,
                                        contentDescription = null,
                                        contentScale = ContentScale.Crop,
                                        modifier = Modifier
                                            .fillMaxSize()
                                            .clip(RoundedCornerShape(2.dp)),
                                    )
                                    if (item.kind == "audio") {
                                        // 音频无缩略图：深色底 + 白音符（对齐 AppMediaGrid 的 audio 角标语言）
                                        Box(
                                            Modifier
                                                .fillMaxSize()
                                                .background(Color.Black.copy(alpha = 0.75f)),
                                            contentAlignment = Alignment.Center,
                                        ) {
                                            Icon(
                                                Icons.Outlined.MusicNote,
                                                contentDescription = "录音",
                                                tint = Color.White,
                                                modifier = Modifier.size(24.dp),
                                            )
                                        }
                                    }
                                    if (item.kind == "video") {
                                        Icon(
                                            Icons.Filled.PlayArrow,
                                            contentDescription = null,
                                            tint = Color.White,
                                            modifier = Modifier.align(Alignment.Center).size(24.dp),
                                        )
                                    }
                                    if (item.uploading) {
                                        Box(
                                            Modifier
                                                .fillMaxSize()
                                                .background(Color.Black.copy(alpha = 0.35f)),
                                            contentAlignment = Alignment.Center,
                                        ) {
                                            Text(
                                                "上传中…",
                                                style = MaterialTheme.typography.labelMedium,
                                                color = Color.White,
                                            )
                                        }
                                    }
                                    if (item.failed) {
                                        Box(
                                            Modifier
                                                .fillMaxSize()
                                                .background(MaterialTheme.colorScheme.error.copy(alpha = 0.25f)),
                                            contentAlignment = Alignment.Center,
                                        ) {
                                            Icon(
                                                Icons.Outlined.Close,
                                                contentDescription = "上传失败",
                                                tint = Color.White,
                                                modifier = Modifier.size(22.dp),
                                            )
                                        }
                                    }
                                    Box(
                                        Modifier
                                            .align(Alignment.TopEnd)
                                            .padding(4.dp)
                                            .size(20.dp)
                                            .clip(RoundedCornerShape(999.dp))
                                            .background(Color.Black.copy(alpha = 0.55f))
                                            .clickable { media = media - item },
                                        contentAlignment = Alignment.Center,
                                    ) {
                                        Icon(
                                            Icons.Outlined.Close,
                                            contentDescription = "移除",
                                            tint = Color.White,
                                            modifier = Modifier.size(13.dp),
                                        )
                                    }
                                }
                            }
                            repeat(3 - rowItems.size) { Spacer(Modifier.weight(1f)) }
                        }
                    }
                    // 末格「添加 +」：微信式灰底方格
                    if (media.size < 9) {
                        Row(
                            Modifier.fillMaxWidth().padding(top = 3.dp),
                            horizontalArrangement = Arrangement.spacedBy(3.dp),
                        ) {
                            Box(
                                Modifier
                                    .weight(1f)
                                    .aspectRatio(1f)
                                    .clip(RoundedCornerShape(2.dp))
                                    .background(MaterialTheme.colorScheme.surfaceContainerHigh)
                                    .clickable {
                                        picker.launch(
                                            arrayOf("image/*", "video/*", "audio/*")
                                        )
                                    },
                                contentAlignment = Alignment.Center,
                            ) {
                                Icon(
                                    Icons.Outlined.Add,
                                    contentDescription = "添加",
                                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                                    modifier = Modifier.size(28.dp),
                                )
                            }
                            if (media.size % 3 == 0) {
                                // 整行占满时补两个空格，保持网格节奏
                            }
                            Spacer(Modifier.weight(1f))
                            Spacer(Modifier.weight(1f))
                        }
                    }
                    if (media.isNotEmpty()) {
                        Text(
                            "共 ${media.size} 个（最多 9 个）",
                            style = MaterialTheme.typography.labelMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            modifier = Modifier.padding(top = 6.dp),
                        )
                    }
                }

            AppFormGap()

            // ===== 地点 / 心情：微信式分组行 =====
            AppFormGroup {
                AppFormTapRow(
                    label = "所在位置",
                    value = locationName,
                    placeholder = "搜索地点（可选）",
                    onClick = {
                        geoQuery = ""
                        candidates = emptyList()
                        showLocation = true
                    },
                )
                AppFormRowDivider()
                AppFormTapRow(
                    label = "心情",
                    value = effectiveMood,
                    placeholder = "选一个（可选）",
                    onClick = { showMood = true },
                )
            }

            if (toast != null) {
                Text(
                    toast.orEmpty(),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.error,
                    modifier = Modifier.padding(horizontal = 16.dp, vertical = 10.dp),
                )
            }
            Spacer(Modifier.height(24.dp))
        }

        // 地点选择弹层（搜索候选 + 手动填写）
        if (showLocation) {
            LocationSheet(
                query = geoQuery,
                onQueryChange = { geoQuery = it },
                candidates = candidates,
                searching = searching,
                error = geoError,
                onSearch = {
                    val q = geoQuery.trim()
                    if (q.isEmpty()) return@LocationSheet
                    searching = true
                    scope.launch {
                        runCatching { PointsRepository.geocode(q) }
                            .onSuccess { candidates = it; geoError = "" }
                            .onFailure { candidates = emptyList(); geoError = errText(it, "搜索失败") }
                        searching = false
                    }
                },
                onPick = {
                    locationName = it.name
                    locationLat = it.lat
                    locationLng = it.lng
                    showLocation = false
                },
                onConfirm = { pickedName, pickedLat, pickedLng ->
                    locationName = pickedName
                    locationLat = pickedLat
                    locationLng = pickedLng
                    showLocation = false
                },
                onDismiss = { showLocation = false },
            )
        }

        // 心情选择弹层：4 预设 + 自定义输入 + 清除
        if (showMood) {
            MoodSheet(
                selected = effectiveMood,
                onPick = {
                    mood = it
                    customMood = ""
                    showMood = false
                },
                onCustom = {
                    customMood = it
                    mood = ""
                    showMood = false
                },
                onClear = {
                    mood = ""
                    customMood = ""
                    showMood = false
                },
                onDismiss = { showMood = false },
            )
        }

        AppToast(text = toast, onDismiss = { toast = null })
    }
}

/** 心情选择 sheet：预设胶囊 + 自定义输入 + 清除（微信式分组行点开）。 */
@Composable
private fun MoodSheet(
    selected: String,
    onPick: (String) -> Unit,
    onCustom: (String) -> Unit,
    onClear: () -> Unit,
    onDismiss: () -> Unit,
) {
    val presets = listOf("开心", "平静", "心动", "感动")
    var custom by remember { mutableStateOf(if (selected !in presets) selected else "") }
    val colors = LocalAppColors.current
    com.bookandmusic.lovelife.appnative.designsystem.atoms.AppSheet(
        visible = true,
        onDismiss = onDismiss,
        title = "心情",
    ) {
        Row(
            Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            presets.forEach { m ->
                Box(Modifier.weight(1f)) {
                    AppChip(
                        text = m,
                        background = if (selected == m) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.surfaceContainerHigh,
                        contentColor = if (selected == m) colors.onSolid else MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    Box(
                        Modifier
                            .matchParentSize()
                            .clip(RoundedCornerShape(999.dp))
                            .clickable { onPick(m) },
                    )
                }
            }
        }
        Row(
            Modifier
                .fillMaxWidth()
                .padding(top = 12.dp)
                .clip(RoundedCornerShape(12.dp))
                .background(MaterialTheme.colorScheme.surfaceContainerHigh)
                .padding(horizontal = 12.dp, vertical = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Box(Modifier.weight(1f)) {
                if (custom.isEmpty()) {
                    Text(
                        "自定义心情（两个字以内更配胶囊）",
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                BasicTextField(
                    value = custom,
                    onValueChange = { custom = it.take(8) },
                    singleLine = true,
                    textStyle = MaterialTheme.typography.bodyMedium.copy(color = MaterialTheme.colorScheme.onSurface),
                    cursorBrush = SolidColor(MaterialTheme.colorScheme.primary),
                    modifier = Modifier.fillMaxWidth(),
                )
            }
            Text(
                "用这个",
                style = MaterialTheme.typography.labelLarge,
                color = colors.tint,
                modifier = Modifier
                    .clip(RoundedCornerShape(999.dp))
                    .clickable(enabled = custom.isNotBlank()) { onCustom(custom.trim()) }
                    .padding(horizontal = 8.dp, vertical = 4.dp),
            )
        }
        Text(
            "清除心情",
            style = MaterialTheme.typography.labelLarge,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            textAlign = androidx.compose.ui.text.style.TextAlign.Center,
            modifier = Modifier
                .fillMaxWidth()
                .padding(top = 10.dp)
                .clip(RoundedCornerShape(999.dp))
                .clickable { onClear() }
                .padding(vertical = 8.dp),
        )
    }
}

@Composable
private fun LocationSheet(
    query: String,
    onQueryChange: (String) -> Unit,
    candidates: List<GeocodeCandidate>,
    searching: Boolean,
    error: String,
    onSearch: () -> Unit,
    onPick: (GeocodeCandidate) -> Unit,
    /** 确认地点：name 必填；lat/lng 为地图选点结果（未选点 = 纯文本地名） */
    onConfirm: (name: String, lat: Double?, lng: Double?) -> Unit,
    onDismiss: () -> Unit,
) {
    val colors = LocalAppColors.current
    // 地图选点：搜不到时打开全屏选点页（与「添加地点」页同一交互）
    var showMapPicker by remember { mutableStateOf(false) }
    var pickedLat by remember { mutableStateOf<Double?>(null) }
    var pickedLng by remember { mutableStateOf<Double?>(null) }

    com.bookandmusic.lovelife.appnative.designsystem.atoms.AppSheet(
        visible = true,
        onDismiss = onDismiss,
        title = "选择地点",
    ) {
        Row(
            Modifier
                .fillMaxWidth()
                .clip(RoundedCornerShape(12.dp))
                .background(MaterialTheme.colorScheme.surfaceContainerHigh)
                .padding(horizontal = 12.dp, vertical = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Box(Modifier.weight(1f)) {
                if (query.isEmpty()) {
                    Text(
                        "如：杭州 / 厦门 · 鼓浪屿",
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                BasicTextField(
                    value = query,
                    onValueChange = onQueryChange,
                    singleLine = true,
                    textStyle = MaterialTheme.typography.bodyMedium.copy(color = MaterialTheme.colorScheme.onSurface),
                    cursorBrush = SolidColor(MaterialTheme.colorScheme.primary),
                    modifier = Modifier.fillMaxWidth(),
                )
            }
            Box(
                Modifier
                    .size(30.dp)
                    .clip(RoundedCornerShape(8.dp))
                    .background(MaterialTheme.colorScheme.primary)
                    .clickable(enabled = !searching) { onSearch() },
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    Icons.Outlined.Search,
                    contentDescription = "搜索",
                    tint = colors.onSolid,
                    modifier = Modifier.size(16.dp),
                )
            }
        }
        if (candidates.isNotEmpty()) {
            Column(Modifier.padding(top = 8.dp)) {
                candidates.take(5).forEach { c ->
                    Row(
                        Modifier
                            .fillMaxWidth()
                            .clip(RoundedCornerShape(12.dp))
                            .clickable { onPick(c) }
                            .padding(horizontal = 12.dp, vertical = 9.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        Icon(
                            Icons.Outlined.Place,
                            contentDescription = null,
                            tint = colors.semGreen,
                            modifier = Modifier.size(16.dp),
                        )
                        Column {
                            Text(c.name, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurface)
                            Text(
                                "${c.display} · ${formatCoord(c.lat, 3)},${formatCoord(c.lng, 3)}",
                                style = MaterialTheme.typography.labelMedium,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    }
                }
            }
        } else if (!searching && error.isNotEmpty()) {
            Text(
                error,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.error,
                modifier = Modifier.padding(top = 10.dp),
            )
        }

        // ===== 地图选点入口：搜不到时打开全屏选点页（不在弹层内嵌小地图） =====
        Text(
            if (pickedLat != null) {
                "已选点：${formatCoord(pickedLat!!, 5)}, ${formatCoord(pickedLng!!, 5)}"
            } else {
                "没找到？在地图上选点"
            },
            style = MaterialTheme.typography.labelMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(top = 12.dp, bottom = 6.dp),
        )
        Row(
            Modifier
                .fillMaxWidth()
                .clip(RoundedCornerShape(12.dp))
                .background(MaterialTheme.colorScheme.surfaceContainerHigh)
                .clickable { showMapPicker = true }
                .padding(horizontal = 12.dp, vertical = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Icon(
                Icons.Outlined.Place,
                contentDescription = null,
                tint = colors.semGreen,
                modifier = Modifier.size(18.dp),
            )
            Text(
                "打开地图选点",
                style = MaterialTheme.typography.bodyLarge,
                color = MaterialTheme.colorScheme.onSurface,
                modifier = Modifier.weight(1f),
            )
            Icon(
                Icons.AutoMirrored.Filled.KeyboardArrowRight,
                contentDescription = null,
                tint = MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.4f),
                modifier = Modifier.size(18.dp),
            )
        }

        AppButton(
            if (query.isBlank()) "使用地图选点" else "用「${query.trim()}」",
            onClick = {
                onConfirm(
                    query.trim(),
                    pickedLat,
                    pickedLng,
                )
            },
            enabled = query.isNotBlank() || pickedLat != null,
            modifier = Modifier.padding(top = 12.dp),
        )
    }

    if (showMapPicker) {
        MapPickerScreen(
            onCancel = { showMapPicker = false },
            onPicked = { pLat, pLng ->
                pickedLat = pLat
                pickedLng = pLng
                showMapPicker = false
            },
        )
    }
}

/** 添加纪念日：名称 + 阳历/农历 + 日期 + 提前提醒。 */
@Composable
fun AnniversaryFormScreen(
    contentPadding: PaddingValues,
    onBack: () -> Unit,
    onSaved: () -> Unit,
) {
    val scope = rememberCoroutineScope()
    var name by remember { mutableStateOf("") }
    var type by remember { mutableIntStateOf(0) } // 0 阳历 / 1 农历
    var solar by remember { mutableStateOf<LocalDate?>(null) }   // 阳历模式：选中公历日；农历模式：仅存选中日的公历投影（显示用）
    var isLeapMonth by remember { mutableStateOf(false) }         // 农历模式下选中的是否闰月
    // 农历语义（服务端契约：type=lunar 时 date = 农历年月日 YYYY-MM-DD）
    var lunarY by remember { mutableStateOf<Int?>(null) }
    var lunarM by remember { mutableStateOf<Int?>(null) }
    var lunarD by remember { mutableStateOf<Int?>(null) }
    var showCalendar by remember { mutableStateOf(false) }
    var remind by remember { mutableIntStateOf(1) }
    var saving by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf("") }
    val canSave = name.isNotBlank() && !saving &&
        (if (type == 1) (lunarY != null && lunarM != null && lunarD != null) else solar != null)

    // 服务端契约：date 一律 YYYY-MM-DD——lunar 时为农历语义年月日，solar 时为公历
    val lunarEngine = remember { AppLunarEngine() }

    Column(
        Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(top = contentPadding.calculateTopPadding())
            .padding(bottom = contentPadding.calculateBottomPadding())
    ) {
        TopBarAction(
            text = if (saving) "保存中…" else "保存",
            enabled = canSave,
            onClick = {
                saving = true
                scope.launch {
                    runCatching {
                        AnniversariesRepository.create(
                                name = name.trim(),
                                // 服务端契约：lunar 的 date = 农历年月日（如中秋 2026-08-15），
                                // solar 的 date = 公历。农历语义来自日历选择的 lunar* 字段。
                                date = if (type == 1) {
                                    "%04d-%02d-%02d".format(lunarY!!, lunarM!!, lunarD!!)
                                } else solar.toString(),
                                type = if (type == 1) "lunar" else "solar",
                                isLeap = isLeapMonth,
                                remindDays = remind.coerceIn(0, 90),
                        )
                    }.onSuccess { onSaved() }
                        .onFailure {
                            error = errText(it, "保存失败")
                            saving = false
                        }
                }
            },
        )
        // ===== 微信式分组 1：基本信息 =====
        AppFormGroup {
            AppFormRow("名称", name, { name = it }, placeholder = "如：中秋")
        }
        AppFormGap()
        // ===== 微信式分组 2：类型 + 日期（日历选择） =====
        AppFormGroup {
            Row(
                Modifier
                    .fillMaxWidth()
                    .heightIn(min = 48.dp)
                    .padding(horizontal = 16.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text("类型", style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurface)
                Spacer(Modifier.weight(1f))
                AppSegmentedControl(
                    options = listOf("阳历", "农历"),
                    selected = type,
                    onSelect = {
                        type = it
                        // 切换历法清两套日期状态，避免「阳历日存成农历语义」的错位
                        solar = null
                        isLeapMonth = false
                        lunarY = null; lunarM = null; lunarD = null
                    },
                    modifier = Modifier.width(150.dp),
                )
            }
            AppFormRowDivider()
            AppFormTapRow(
                label = "日期",
                value = run {
                    if (type == 1 && lunarY != null && lunarM != null && lunarD != null) {
                        val leapTxt = if (isLeapMonth) "闰" else ""
                        // 农历语义 + 公历投影（用户可读阳历）
                        "$leapTxt${lunarMonthName(lunarM!!)}${lunarDayName(lunarD!!)}" +
                            (solar?.let { "（$it）" } ?: "")
                    } else solar?.toString() ?: ""
                },
                placeholder = if (type == 1) "点选农历日期" else "点选日期",
                onClick = { showCalendar = true },
            )
        }
        AppFormGap()
        // ===== 微信式分组 3：提醒 =====
        AppFormGroup {
            AppFormRow("提前提醒", remind.toString(), { remind = it.filter { c -> c.isDigit() }.toIntOrNull() ?: 0 }, placeholder = "天")
        }
        Text(
            if (type == 1) "按农历年月日保存，每年农历循环；括号内为对应阳历" else "提醒天数 0–90；0 = 不提前提醒",
            style = MaterialTheme.typography.labelMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(horizontal = 16.dp, vertical = 10.dp),
        )
        if (error.isNotEmpty()) {
            Text(
                error,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.error,
                modifier = Modifier.padding(horizontal = 16.dp),
            )
        }
        Spacer(Modifier.height(24.dp))
    }

    if (showCalendar) {
        AppDatePickerSheet(
            visible = true,
            onDismiss = { showCalendar = false },
            onPick = { picked ->
                solar = picked.date
                if (type == 1) {
                    lunarY = picked.lunarYear
                    lunarM = picked.lunarMonth
                    lunarD = picked.lunarDay
                    isLeapMonth = picked.lunarLeap
                }
                showCalendar = false
            },
            lunarMode = type == 1,
            initial = solar,
            lunarEngine = if (type == 1) lunarEngine else null,
        )
    }
}

/** 农历引擎：把 LunarCalendar 适配给 AppDatePickerSheet（月寻址一律按序位）。 */
private class AppLunarEngine : LunarEngine {

    /** 该农历年的月序列（含闰月：平 m 与闰 m 各占一序位，闰在后） */
    private fun monthSequence(y: Int): List<Pair<Int, Boolean>> {
        val lm = LunarCalendar.leapMonth(y)
        val seq = mutableListOf<Pair<Int, Boolean>>()
        for (m in 1..12) {
            seq.add(m to false)
            if (m == lm) seq.add(m to true)
        }
        return seq
    }

    override fun monthCount(y: Int): Int = monthSequence(y).size

    override fun monthAt(y: Int, pos: Int): LunarMonth {
        val (m, leap) = monthSequence(y)[pos.coerceIn(0, monthSequence(y).size - 1)]
        return LunarMonth(m, leap)
    }

    override fun positionOf(y: Int, month: Int, isLeap: Boolean): Int =
        monthSequence(y).indexOfFirst { it.first == month && it.second == isLeap }

    private fun seqAt(y: Int, pos: Int): Pair<Int, Boolean> = monthSequence(y)[pos.coerceIn(0, monthCount(y) - 1)]

    override fun monthStart(y: Int, pos: Int): LocalDate {
        val (m, leap) = seqAt(y, pos)
        // 农历表缺该年/该月时换算返回 null：退回正月初一，不让日历整页崩掉
        return LunarCalendar.lunarToSolar(y, m, 1, leap)
            ?: LunarCalendar.lunarToSolar(y, 1, 1, false)
            ?: LocalDate.of(y, 1, 1)
    }

    override fun monthLength(y: Int, pos: Int): Int {
        val (m, leap) = seqAt(y, pos)
        return if (leap) LunarCalendar.leapMonthDays(y) else LunarCalendar.monthDays(y, m)
    }

    override fun lunarToSolar(y: Int, pos: Int, d: Int): LocalDate? {
        val (m, leap) = seqAt(y, pos)
        return LunarCalendar.lunarToSolar(y, m, d, leap)
    }

    override fun solarToLunar(date: LocalDate): PickedDate? {
        val l = LunarCalendar.solarToLunar(date) ?: return null
        return PickedDate(date, l.year, l.month, l.day, l.isLeap)
    }

    override fun dayLabel(y: Int, pos: Int, d: Int): String {
        val (m, leap) = seqAt(y, pos)
        return if (d == 1) (if (leap) "闰" else "") + lunarMonthName(m) else lunarDayName(d)
    }

    override fun yearGanZhi(y: Int): String = LunarCalendar.yearGanZhi(y)

    override fun monthLabel(y: Int, pos: Int): String {
        val (m, leap) = seqAt(y, pos)
        return (if (leap) "闰" else "") + lunarMonthName(m)
    }
}

/** 新建相册（微信式表单页）：名称行 + 描述多行白块。 */
@Composable
fun AlbumFormScreen(
    contentPadding: PaddingValues,
    onBack: () -> Unit,
    onSaved: (Long, String) -> Unit,
) {
    val scope = rememberCoroutineScope()
    var name by remember { mutableStateOf("") }
    var description by remember { mutableStateOf("") }
    var saving by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf("") }

    Column(
        Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(top = contentPadding.calculateTopPadding())
            .padding(bottom = contentPadding.calculateBottomPadding())
    ) {
        TopBarAction(
            text = if (saving) "创建中…" else "创建",
            enabled = name.isNotBlank() && !saving,
            onClick = {
                saving = true
                scope.launch {
                    runCatching { AlbumsRepository.create(name.trim(), description.trim()) }
                        .onSuccess { id -> onSaved(id, name.trim()) }
                        .onFailure {
                            error = errText(it, "创建失败")
                            saving = false
                        }
                }
            },
        )
        AppFormGroup {
            AppFormRow("名称", name, { name = it }, placeholder = "如：厦门旅行")
        }
        AppFormGap()
        AppFormTextBlock(
            value = description,
            onValueChange = { description = it },
            placeholder = "描述（可选）",
            minHeight = 88.dp,
        )
        if (error.isNotEmpty()) {
            Text(
                error,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.error,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 10.dp),
            )
        }
        Spacer(Modifier.height(24.dp))
    }
}

/** 添加地点（微信式表单页）：搜索优先（边输边搜回填经纬度）→ 搜不到地图点选 → 手动经纬度兜底。 */
@Composable
fun PointFormScreen(
    contentPadding: PaddingValues,
    onBack: () -> Unit,
    onSaved: () -> Unit,
) {
    val scope = rememberCoroutineScope()
    val colors = LocalAppColors.current
    var name by remember { mutableStateOf("") }
    var lat by remember { mutableStateOf("") }
    var lng by remember { mutableStateOf("") }
    var visitDate by remember { mutableStateOf<LocalDate?>(null) }
    var description by remember { mutableStateOf("") }
    var candidates by remember { mutableStateOf<List<GeocodeCandidate>>(emptyList()) }
    var searching by remember { mutableStateOf(false) }
    var geoError by remember { mutableStateOf("") }
    var saving by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf("") }
    var showCalendar by remember { mutableStateOf(false) }

    // 地图选点状态（搜不到时的回填路径）
    var showMapPicker by remember { mutableStateOf(false) }
    var pickedLat by remember { mutableStateOf<Double?>(null) }
    var pickedLng by remember { mutableStateOf<Double?>(null) }

    val canSave = name.isNotBlank() && lat.toDoubleOrNull() != null && lng.toDoubleOrNull() != null && !saving

    // 边输边搜：350ms 防抖 + 失败与空结果分开（搜索优先策略）
    LaunchedEffect(name) {
        val q = name.trim()
        if (q.isEmpty()) {
            candidates = emptyList(); geoError = ""; searching = false
            return@LaunchedEffect
        }
        delay(350)
        searching = true
        runCatching { PointsRepository.geocode(q) }
            .onSuccess { candidates = it; geoError = "" }
            .onFailure { candidates = emptyList(); geoError = errText(it, "搜索失败") }
        searching = false
    }

    Column(
        Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(top = contentPadding.calculateTopPadding())
            .padding(bottom = contentPadding.calculateBottomPadding())
    ) {
        TopBarAction(
            text = if (saving) "保存中…" else "保存",
            enabled = canSave,
            onClick = {
                saving = true
                scope.launch {
                    runCatching {
                        PointsRepository.create(
                                name = name.trim(),
                                lat = lat.toDouble(),
                                lng = lng.toDouble(),
                                visitDate = visitDate?.toString() ?: "",
                            description = description,
                        )
                    }.onSuccess { onSaved() }
                        .onFailure {
                            error = errText(it, "保存失败")
                            saving = false
                        }
                }
            },
        )
        // ===== 分组 1：名称（边输边搜） =====
        AppFormGroup {
            AppFormRow("名称", name, { name = it }, placeholder = "如：厦门·鼓浪屿")
        }

        // ===== 搜索候选（独立白组，点击回填） =====
        if (searching) {
            Text(
                "搜索中…",
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 10.dp),
            )
        } else if (candidates.isNotEmpty()) {
            AppFormGap()
            AppFormGroup {
                candidates.take(5).forEachIndexed { i, c ->
                    if (i > 0) AppFormRowDivider()
                    Row(
                        Modifier
                            .fillMaxWidth()
                            .clickable {
                                name = c.name
                                lat = formatCoord(c.lat)
                                lng = formatCoord(c.lng)
                                candidates = emptyList()
                            }
                            .padding(horizontal = 16.dp, vertical = 11.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(10.dp),
                    ) {
                        Icon(
                            Icons.Outlined.Place,
                            contentDescription = null,
                            tint = colors.semGreen,
                            modifier = Modifier.size(18.dp),
                        )
                        Column(Modifier.weight(1f)) {
                            Text(c.name, style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurface)
                            Text(
                                "${c.display} · ${formatCoord(c.lat, 3)},${formatCoord(c.lng, 3)}",
                                style = MaterialTheme.typography.labelMedium,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    }
                }
            }
        } else if (geoError.isNotEmpty()) {
            Text(
                geoError,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.error,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 10.dp),
            )
        }

        AppFormGap()

        // ===== 分组 2：地图选点（全屏页，微信「选择位置」同形态） =====
        // 不在表单内嵌小地图：内嵌块与滚动手势打架，且瓦片绝对定位需宿主裁剪才不溢出
        AppFormGroup {
            AppFormTapRow(
                label = "地图选点",
                value = if (pickedLat != null && pickedLng != null) {
                    "已选 ${formatCoord(pickedLat!!, 5)}, ${formatCoord(pickedLng!!, 5)}"
                } else "",
                placeholder = "点按打开地图选点",
                onClick = { showMapPicker = true },
            )
        }

        AppFormGap()

        // ===== 分组 3：经纬度 + 日期 + 描述 =====
        AppFormGroup {
            AppFormRow("经度", lng, { lng = it; pickedLng = null }, placeholder = "如 118.06")
            AppFormRowDivider()
            AppFormRow("纬度", lat, { lat = it; pickedLat = null }, placeholder = "如 24.44")
            AppFormRowDivider()
            AppFormTapRow(
                label = "到访日期",
                value = visitDate?.toString() ?: "",
                placeholder = "可选",
                onClick = { showCalendar = true },
            )
            AppFormRowDivider()
            AppFormRow("描述", description, { description = it }, placeholder = "可选")
        }
        if (error.isNotEmpty()) {
            Text(
                error,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.error,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 10.dp),
            )
        }
        Spacer(Modifier.height(24.dp))
    }

    if (showCalendar) {
        AppDatePickerSheet(
            visible = true,
            onDismiss = { showCalendar = false },
            onPick = {
                visitDate = it.date
                showCalendar = false
            },
            initial = visitDate,
        )
    }

    if (showMapPicker) {
        MapPickerScreen(
            onCancel = { showMapPicker = false },
            onPicked = { pLat, pLng ->
                pickedLat = pLat
                pickedLng = pLng
                lat = formatCoord(pLat)
                lng = formatCoord(pLng)
                showMapPicker = false
            },
            initialLat = pickedLat ?: lat.toDoubleOrNull(),
            initialLng = pickedLng ?: lng.toDoubleOrNull(),
        )
    }
}

/** 通知中心：列表按时间倒序，点击即删；顶部「清空」。 */
@Composable
fun NotificationsScreen(
    contentPadding: PaddingValues,
    onBack: () -> Unit,
    /** 点通知 → 跳对应内容（动态定位高亮 / 地图点位 / 纪念日 / 相册详情） */
    onOpenTarget: (targetType: String, targetId: Long, snapshot: String) -> Unit = { _, _, _ -> },
) {
    var items by remember { mutableStateOf<List<com.bookandmusic.lovelife.appnative.data.NotificationDto>?>(null) }
    var toast by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val colors = LocalAppColors.current

    var loadError by remember { mutableStateOf("") }
    suspend fun reload() {
        // 不静默吞异常：失败要能看见（否则表现为「通知中心空的」）
        // 通知按页拉全：只取第一页会漏掉更早的事件
        runCatching { com.bookandmusic.lovelife.appnative.data.NotificationsRepository.listAll() }
            .onSuccess {
                items = it
                loadError = ""
            }
            .onFailure {
                items = emptyList()
                loadError = it.message ?: "加载失败"
            }
    }
    LaunchedEffect(Unit) { reload() }

    Box(Modifier.fillMaxSize()) {
        LazyColumn(
            Modifier.fillMaxSize(),
            contentPadding = contentPadding,
        ) {
            item {
                Row(
                    Modifier.fillMaxWidth().padding(horizontal = 16.dp).padding(bottom = 12.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(
                        "共 ${items?.size ?: 0} 条",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.weight(1f),
                    )
                    if (!items.isNullOrEmpty()) {
                        Text(
                            "清空",
                            style = MaterialTheme.typography.bodyMedium,
                            color = colors.tint,
                            modifier = Modifier
                                .clip(RoundedCornerShape(8.dp))
                                .clickable {
                                    scope.launch {
                                        runCatching { com.bookandmusic.lovelife.appnative.data.NotificationsRepository.clear() }
                                            .onSuccess {
                                                items = emptyList()
                                                toast = "已清空"
                                            }
                                            .onFailure { toast = "清空失败：${errText(it, "")}" }
                                    }
                                }
                                .padding(horizontal = 8.dp, vertical = 4.dp),
                        )
                    }
                }
            }
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
                                title = if (loadError.isNotEmpty()) "通知加载失败" else "还没有通知",
                                hint = if (loadError.isNotEmpty()) loadError else "对方发动态、点赞、评论时会出现在这里。",
                                icon = Icons.Outlined.Notifications,
                            )
                        }
                    }
                    items(list, key = { it.id }) { n ->
                        // 通知行：去卡片，行 + 细线分隔
                        Column(Modifier.fillMaxWidth()) {
                                Row(
                                    Modifier
                                        .fillMaxWidth()
                                        .clickable {
                                            // 通知是临时消费品：点击即删除。
                                            // 顺序固定：先删再跳——跳转会触发壳层重算未读（红点 = 未消费条数），
                                            // 先跳再删会让那次重算读到删除前的旧值，返回主壳后红点多一个。
                                            scope.launch {
                                                runCatching {
                                                    com.bookandmusic.lovelife.appnative.data.NotificationsRepository.consume(n.id)
                                                }
                                                    .onSuccess { items = items?.filter { it.id != n.id } }
                                                    .onFailure { toast = "通知未删除：${errText(it, "")}" }
                                                // 跳转照旧：用户点的是「去看看」，消费只是副作用
                                                onOpenTarget(n.target_type, n.target_id, n.snapshot)
                                            }
                                        }
                                        .padding(horizontal = 14.dp, vertical = 12.dp),
                                    verticalAlignment = Alignment.CenterVertically,
                                    horizontalArrangement = Arrangement.spacedBy(10.dp),
                                ) {
                                    AppIconBadge(
                                        icon = notificationIcon(n.event_type),
                                        tint = notificationTint(n.event_type, colors, MaterialTheme.colorScheme.onSurfaceVariant),
                                        background = notificationTint(
                                            n.event_type,
                                            colors,
                                            MaterialTheme.colorScheme.onSurfaceVariant,
                                        ).copy(alpha = 0.14f),
                                        size = AppIconBadgeSize.Md,
                                        corner = 8.dp,
                                    )
                                    Column(Modifier.weight(1f)) {
                                        Text(
                                            notificationText(n),
                                            style = MaterialTheme.typography.bodyMedium,
                                            color = MaterialTheme.colorScheme.onSurface,
                                            maxLines = 2,
                                            overflow = TextOverflow.Ellipsis,
                                        )
                                        Text(
                                            TimeFmt.dateTime(n.created_at),
                                            style = MaterialTheme.typography.labelMedium,
                                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                                            modifier = Modifier.padding(top = 2.dp),
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
        AppToast(text = toast, onDismiss = { toast = null })
    }
}

private fun notificationIcon(event: String): androidx.compose.ui.graphics.vector.ImageVector = when (event) {
    "post_liked" -> Icons.Filled.Favorite
    "post_commented" -> Icons.Outlined.ChatBubbleOutline
    "album_created" -> Icons.Outlined.PhotoLibrary
    "anniversary_reminder" -> Icons.Outlined.Cake
    "map_point_created" -> Icons.Outlined.Place
    else -> Icons.Outlined.Notifications
}

private fun notificationTint(
    event: String,
    colors: com.bookandmusic.lovelife.appnative.designsystem.theme.AppColors,
    fallback: androidx.compose.ui.graphics.Color,
): androidx.compose.ui.graphics.Color = when (event) {
    "post_liked" -> colors.primaryDeep
    "anniversary_reminder" -> colors.semGold
    "post_commented", "album_created" -> colors.semBlue
    "map_point_created" -> colors.semGreen
    else -> fallback
}

/** 事件文案：把通知类型翻成一句人话。 */
private fun notificationText(n: com.bookandmusic.lovelife.appnative.data.NotificationDto): String {
    val snap = runCatching {
        kotlinx.serialization.json.Json.parseToJsonElement(n.snapshot)
    }.getOrNull()
    fun field(key: String): String {
        val prim = (snap as? kotlinx.serialization.json.JsonObject)?.get(key)
            as? kotlinx.serialization.json.JsonPrimitive
        return prim?.content ?: ""
    }
    val operator = field("operator").ifEmpty { "对方" }
    return when (n.event_type) {
        "post_created" -> "$operator 发布了新动态：${field("content")}"
        "post_liked" -> "$operator 赞了你的动态"
        "post_commented" -> "$operator 评论了你的动态：${field("content")}"
        "post_deleted" -> "${field("deleted_by").ifEmpty { "对方" }} 删除了你发布的动态"
        "comment_deleted" -> "${field("deleted_by").ifEmpty { "对方" }} 删除了你的评论"
        "album_created" -> "$operator 创建了相册「${field("name")}」"
        "album_deleted" -> "${field("deleted_by").ifEmpty { "对方" }} 删除了你的相册「${field("name")}」"
        "anniversary_reminder" -> "纪念日「${field("name")}」快到了"
        "anniversary_deleted" -> "${field("deleted_by").ifEmpty { "对方" }} 删除了你的纪念日「${field("name")}」"
        "map_point_created" -> "$operator 添加了足迹「${field("name")}」"
        "map_point_deleted" -> "${field("deleted_by").ifEmpty { "对方" }} 删除了你的足迹「${field("name")}」"
        else -> "系统通知"
    }
}
