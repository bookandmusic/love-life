package com.bookandmusic.lovelife.appnative.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.outlined.AccessTime
import androidx.compose.material.icons.outlined.Image
import androidx.compose.material.icons.outlined.LocationOn
import androidx.compose.material.icons.outlined.MusicNote
import androidx.compose.material.icons.outlined.Place
import androidx.compose.material3.Icon
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableDoubleStateOf
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import coil.compose.AsyncImage
import com.bookandmusic.lovelife.appnative.data.ChatRepository
import com.bookandmusic.lovelife.appnative.data.ChatState
import com.bookandmusic.lovelife.appnative.data.ChatUi
import com.bookandmusic.lovelife.appnative.data.MapConfigDto
import com.bookandmusic.lovelife.appnative.data.MapRepository
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppPillButton
import com.bookandmusic.lovelife.appnative.designsystem.theme.LocalAppColors
import kotlinx.coroutines.delay

/**
 * AI 卡片体系（两类，视觉上一眼可分）：
 * - **展示卡**（只读）：服务端下发已验证的 view，list / grid / stats / detail / kpi / timeline / map 七分支，白底细边
 * - **交互卡**（要你决策）：确认卡，主色描边 + 目标形态预览（与结果卡同一套 [ChatUi.PreviewView]）+ 取消/确认
 *
 * 展示卡**只认服务端校验过的形态**：`when(view)` 七分支、无 else、不按数据推形态——
 * 画不出来的围栏在 UiFence 就落成一行人话，不会走到这里。
 * 一切强调色取 `MaterialTheme.colorScheme.primary`（跟随设置里的主题色），不写死任何色值。
 */

/** 引用条目（随消息提交；服务端按 ref + ids 展开） */
data class UiQuoted(
    val ref: String,
    val record: ChatUi.UiRecord,
)

private val CARD_RADIUS = 16.dp

// ==================== 展示卡 ====================

/**
 * 展示卡：标题行 + 形态主体 + 页脚。
 * 无外层阴影、无嵌套容器——它就长在正文流里，和 DeepSeek 的表格/代码块一个层级。
 */
@Composable
fun DisplayCard(
    view: String,
    data: ChatUi.ResultData,
    /** 只画这些项（stats / kpi）：模型在大数据里点名的那几项 */
    only: List<String> = emptyList(),
    /** 列表最多画几条（list / grid）：0 = 数据默认条数 */
    limit: Int = 0,
    onMediaClick: (index: Int, records: List<ChatUi.UiRecord>) -> Unit,
    onMore: () -> Unit = {},
    onToggleQuote: ((UiQuoted) -> Unit)? = null,
    quotedIds: Set<Long> = emptySet(),
) {
    // 收窄：limit 只裁条目，only 只裁统计项（两者服务端都校验过形态与取值）
    val shown = if (limit > 0 && (view == "list" || view == "grid" || view == "timeline")) {
        data.copy(records = data.records.take(limit))
    } else {
        data
    }
    val shownData = if (only.isEmpty()) {
        shown
    } else {
        shown.copy(
            stats = shown.stats.filter { it.label in only },
            series = shown.series.filter { it.title in only },
            summaries = shown.summaries.filter { it.label in only },
        )
    }
    Column(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(CARD_RADIUS))
            .background(MaterialTheme.colorScheme.surface)
            .border(0.5.dp, LocalAppColors.current.cardEdge, RoundedCornerShape(CARD_RADIUS))
            .padding(14.dp),
    ) {
        if (shownData.title.isNotBlank()) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    shownData.title,
                    style = MaterialTheme.typography.titleSmall,
                    color = MaterialTheme.colorScheme.onSurface,
                    modifier = Modifier.weight(1f),
                )
                if (shownData.total > 0) {
                    Text(
                        "共 ${shownData.total} 条",
                        style = MaterialTheme.typography.labelMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
            Spacer(Modifier.height(8.dp))
        }
        // 七分支，无 else：view 由服务端 validated 保证（UiFence 已挡掉未知形态）
        when (view) {
            "list" -> ListBody(shownData, onToggleQuote, quotedIds, onMediaClick)
            "grid" -> GridBody(shownData.records, onMediaClick)
            "stats" -> StatsBody(shownData)
            "kpi" -> KpiBody(shownData)
            "detail" -> DetailBody(shownData, onMediaClick)
            "timeline" -> TimelineBody(shownData.records)
            "map" -> MapBody(shownData.records)
        }        // 余量 = 真实命中总数 − 本屏条目数（分页入口；统计/详情/地图卡没有列表，不显示）
        val remaining = if (view == "list" || view == "grid" || view == "timeline") {
            shownData.total - shownData.records.size
        } else {
            0
        }
        if (remaining > 0) {
            Row(
                Modifier.fillMaxWidth().padding(top = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    "还有 $remaining 条",
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.weight(1f),
                )
                // 命中区下限 44dp：文字本身只有 20dp 高，直接点文字很难点中
                Box(
                    Modifier
                        .heightIn(min = 44.dp)
                        .clip(RoundedCornerShape(8.dp))
                        .clickable { onMore() }
                        .padding(horizontal = 6.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    Text(
                        "再看几条",
                        style = MaterialTheme.typography.labelLarge,
                        color = MaterialTheme.colorScheme.primary,
                    )
                }
            }
        }
    }
}

// ---------- 列表 ----------

/** 记录是否带可看的媒体（列表行据此决定能不能点开查看器） */
private val ChatUi.UiRecord.hasMedia: Boolean
    get() = thumb_url.isNotBlank() || kind in setOf("photo", "video", "audio", "media")

@Composable
private fun ListBody(
    data: ChatUi.ResultData,
    onToggleQuote: ((UiQuoted) -> Unit)?,
    quotedIds: Set<Long>,
    onMediaClick: (index: Int, records: List<ChatUi.UiRecord>) -> Unit,
) {
    val shown = data.records.take(6)
    // 查看器只吃带媒体的记录：把无媒体的行也传进去会渲染成空舞台
    val mediaOnly = shown.filter { it.hasMedia }
    shown.forEachIndexed { i, r ->
        if (i > 0) Spacer(Modifier.height(2.dp))
        val rowModifier = if (r.hasMedia) {
            Modifier.fillMaxWidth().clickable { onMediaClick(mediaOnly.indexOf(r), mediaOnly) }
        } else {
            Modifier.fillMaxWidth()
        }
        Row(rowModifier.padding(vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
            if (data.selectable && onToggleQuote != null) {
                QuoteDot(checked = r.id in quotedIds) { onToggleQuote(UiQuoted(data.ref, r)) }
                Spacer(Modifier.width(8.dp))
            }
            Thumb(r)
            Column(Modifier.weight(1f).padding(start = 10.dp)) {
                Text(
                    r.text,
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurface,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
                val sub = listOf(r.date, r.badge).filter { it.isNotBlank() }.joinToString(" · ")
                if (sub.isNotBlank()) {
                    Text(
                        sub,
                        style = MaterialTheme.typography.labelMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
            }
        }
    }
}

/** 勾选圈（selectable 时出现；实心 = 已勾选引用） */
@Composable
private fun QuoteDot(checked: Boolean, onClick: () -> Unit) {
    Box(
        Modifier
            .size(22.dp)
            .clip(CircleShape)
            .border(
                1.dp,
                if (checked) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.outlineVariant,
                CircleShape,
            )
            .background(if (checked) MaterialTheme.colorScheme.primary else Color.Transparent)
            .clickable { onClick() },
        contentAlignment = Alignment.Center,
    ) {
        if (checked) {
            Icon(Icons.Filled.Check, null, tint = Color.White, modifier = Modifier.size(14.dp))
        }
    }
}

@Composable
private fun Thumb(r: ChatUi.UiRecord) {
    if (r.thumb_url.isNotBlank()) {
        AsyncImage(
            model = com.bookandmusic.lovelife.appnative.data.Channel.absolute(r.thumb_url),
            contentDescription = null,
            contentScale = androidx.compose.ui.layout.ContentScale.Crop,
            modifier = Modifier.size(44.dp).clip(RoundedCornerShape(12.dp)),
        )
    } else {
        Box(
            Modifier
                .size(44.dp)
                .clip(RoundedCornerShape(12.dp))
                .background(MaterialTheme.colorScheme.surfaceContainerHigh),
            contentAlignment = Alignment.Center,
        ) {
            Icon(kindIcon(r.kind), null, tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.size(20.dp))
        }
    }
}

private fun kindIcon(kind: String) = when (kind) {
    "place" -> Icons.Outlined.Place
    "album" -> Icons.Outlined.Image
    "anniversary" -> Icons.Outlined.AccessTime
    "media" -> Icons.Outlined.Image
    else -> Icons.Outlined.LocationOn
}

// ---------- 网格 ----------

@Composable
private fun GridBody(records: List<ChatUi.UiRecord>, onMediaClick: (Int, List<ChatUi.UiRecord>) -> Unit) {
    val shown = records.take(9)
    shown.chunked(3).forEach { row ->
        Row(Modifier.fillMaxWidth().padding(vertical = 2.dp)) {
            row.forEach { r ->
                Box(
                    Modifier
                        .weight(1f)
                        .aspectRatio(1f)
                        .padding(2.dp)
                        .clip(RoundedCornerShape(12.dp))
                        .background(MaterialTheme.colorScheme.surfaceContainerHigh)
                        .clickable { onMediaClick(shown.indexOf(r), shown) },
                ) {
                    if (r.thumb_url.isNotBlank()) {
                        AsyncImage(
                            model = com.bookandmusic.lovelife.appnative.data.Channel.absolute(r.thumb_url),
                            contentDescription = r.text,
                            contentScale = androidx.compose.ui.layout.ContentScale.Crop,
                            modifier = Modifier.fillMaxSize(),
                        )
                    } else {
                        Icon(
                            kindIcon(r.kind), null,
                            tint = MaterialTheme.colorScheme.onSurfaceVariant,
                            modifier = Modifier.align(Alignment.Center).size(22.dp),
                        )
                    }
                    if (r.kind == "audio") {
                        Icon(
                            Icons.Outlined.MusicNote, null, tint = Color.White,
                            modifier = Modifier
                                .align(Alignment.Center)
                                .size(20.dp)
                                .clip(CircleShape)
                                .background(Color.Black.copy(alpha = 0.35f))
                                .padding(3.dp),
                        )
                    }
                    val badge = r.badge.ifBlank { if (r.kind == "video") "视频" else "" }
                    if (badge.isNotBlank()) {
                        Text(
                            badge,
                            style = MaterialTheme.typography.labelSmall,
                            color = Color.White,
                            modifier = Modifier
                                .align(Alignment.BottomEnd)
                                .padding(4.dp)
                                .clip(RoundedCornerShape(8.dp))
                                .background(Color.Black.copy(alpha = 0.45f))
                                .padding(horizontal = 4.dp, vertical = 1.dp),
                        )
                    }
                }
            }
            repeat(3 - row.size) { Spacer(Modifier.weight(1f)) }
        }
    }
}

// ---------- 统计 ----------

/** 单值卡：一个大数字 + 一句说明（回答「多少」时用，比 stats 大卡克制） */
@Composable
private fun KpiBody(data: ChatUi.ResultData) {
    val stat = data.stats.firstOrNull() ?: return
    Column(Modifier.fillMaxWidth().padding(vertical = 6.dp)) {
        Text(
            stat.value,
            style = MaterialTheme.typography.headlineMedium,
            color = MaterialTheme.colorScheme.onSurface,
            fontWeight = FontWeight.Bold,
        )
        Text(
            stat.label,
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(top = 2.dp),
        )
        if (stat.hint.isNotBlank()) {
            Text(
                stat.hint,
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(top = 2.dp),
            )
        }
    }
}

@Composable
private fun StatsBody(data: ChatUi.ResultData) {
    data.stats.chunked(2).forEach { row ->
        Row(Modifier.fillMaxWidth().padding(vertical = 3.dp)) {
            row.forEachIndexed { idx, s ->
                Column(
                    Modifier
                        .weight(1f)
                        .clip(RoundedCornerShape(12.dp))
                        .background(MaterialTheme.colorScheme.surfaceContainerHigh)
                        .padding(horizontal = 12.dp, vertical = 10.dp),
                ) {
                    Text(
                        s.value,
                        style = MaterialTheme.typography.titleMedium,
                        color = MaterialTheme.colorScheme.onSurface,
                        fontWeight = FontWeight.Bold,
                    )
                    Text(
                        s.label,
                        style = MaterialTheme.typography.labelMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    // 口径说明（「今年 3 · 本月 1」「只数照片，不含视频与录音」）：
                    // kpi 卡与统计卡都要画它，用户才能看出每个数字是按什么口径算的
                    if (s.hint.isNotBlank()) {
                        Text(
                            s.hint,
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            modifier = Modifier.padding(top = 1.dp),
                        )
                    }
                }
                if (idx != row.lastIndex) Spacer(Modifier.width(6.dp))
            }
            if (row.size == 1) Spacer(Modifier.weight(1f))
        }
    }
    data.series.forEach { ser ->
        if (ser.title.isNotBlank()) {
            Text(
                ser.title,
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(top = 8.dp, bottom = 2.dp),
            )
        }
        val maxV = (ser.points.maxOfOrNull { it.count } ?: 0).coerceAtLeast(1)
        // 全部画出：服务端本来就只发到上限（城市 Top 最多 10 点），再在客户端截一次
        // 会让第 7 名之后的城市无声消失，用户以为只去过 6 个城市
        ser.points.forEach { pt ->
            Row(
                Modifier.fillMaxWidth().padding(vertical = 2.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    pt.label,
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.width(72.dp),
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Box(
                    Modifier
                        .padding(horizontal = 6.dp)
                        .weight(1f)
                        .height(8.dp)
                        .clip(RoundedCornerShape(999.dp))
                        .background(MaterialTheme.colorScheme.surfaceContainerHighest),
                ) {
                    Box(
                        Modifier
                            .fillMaxWidth(pt.count.toFloat() / maxV)
                            .fillMaxHeight()
                            .clip(RoundedCornerShape(999.dp))
                            .background(MaterialTheme.colorScheme.primary),
                    )
                }
                Text(
                    "${pt.count}",
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.onSurface,
                )
            }
        }
    }
    // 分布摘要：一行一条（「按年 · 2024–2026 · 3 年共 498 条」），不平铺明细
    data.summaries.forEach { s ->
        Row(Modifier.fillMaxWidth().padding(vertical = 2.dp)) {
            Text(
                s.label,
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(end = 8.dp),
            )
            Text(
                s.text,
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.onSurface,
            )
        }
    }
}

// ---------- 时间线 ----------

/**
 * 时间线卡：按日期倒序、按月分组的竖向轴（左轴点 + 日期，右内容）。
 * 与列表的差别只在「先后」：一条发生在哪个月、彼此相隔多久，读轴就看得出来，不用自己比对日期。
 */
@Composable
private fun TimelineBody(records: List<ChatUi.UiRecord>) {
    records.sortedByDescending { it.date }
        .groupBy { it.date.take(7) }
        .forEach { (month, items) ->
            Text(
                monthLabel(month),
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(top = 6.dp, bottom = 2.dp),
            )
            items.forEach { r -> TimelineRow(r) }
        }
}

@Composable
private fun TimelineRow(r: ChatUi.UiRecord) {
    val lineColor = MaterialTheme.colorScheme.outlineVariant
    Row(
        Modifier
            .fillMaxWidth()
            // 轴画在行背景上：行高由右侧内容决定，不用 IntrinsicSize 猜
            .drawBehind {
                val x = 7.dp.toPx()
                drawLine(lineColor, Offset(x, 0f), Offset(x, size.height), strokeWidth = 1.dp.toPx())
            },
    ) {
        Box(Modifier.width(15.dp), contentAlignment = Alignment.TopCenter) {
            Box(
                Modifier
                    .padding(top = 6.dp)
                    .size(7.dp)
                    .clip(CircleShape)
                    .background(MaterialTheme.colorScheme.primary),
            )
        }
        Column(Modifier.weight(1f).padding(start = 6.dp, top = 2.dp, bottom = 6.dp)) {
            Text(
                r.text,
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurface,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
            val sub = listOf(dayLabel(r.date), r.badge).filter { it.isNotBlank() }.joinToString(" · ")
            if (sub.isNotBlank()) {
                Text(
                    sub,
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
    }
}

/** 2026-02 → 「2026 年 2 月」；没有日期的条目归到「未标日期」 */
private fun monthLabel(ym: String): String {
    if (ym.length < 7) return "未标日期"
    return ym.substring(0, 4) + " 年 " + ym.substring(5, 7).trimStart('0') + " 月"
}

/** 2026-02-13 → 「2 月 13 日」（同月分组下不重复写年） */
private fun dayLabel(date: String): String {
    if (date.length < 10) return date
    return date.substring(5, 7).trimStart('0') + " 月 " + date.substring(8, 10).trimStart('0') + " 日"
}

// ---------- 地图 ----------

/**
 * 地图卡：把命中地点画成标记（只有带经纬度的数据能走到这里，服务端已按 map_needs_coords 挡过）。
 * 瓦片配置按需拉取（与足迹页同一接口）；没配瓦片源时给一句说明，不留空白框。
 * 点标记在卡下方回显该地点的名称与日期——不跳页，不打断对话。
 */
@Composable
private fun MapBody(records: List<ChatUi.UiRecord>) {
    val pinned = records.filter { it.lat != 0.0 || it.lng != 0.0 }
    if (pinned.isEmpty()) return
    var cfg by remember { mutableStateOf<MapConfigDto?>(null) }
    var picked by remember { mutableStateOf<ChatUi.UiRecord?>(null) }
    // 视野按点位包络取一档能全看到的缩放（点位跨省就拉远，同城就贴近）
    val centerLat = pinned.map { it.lat }.average()
    val centerLng = pinned.map { it.lng }.average()
    val span = maxOf(pinned.maxOf { it.lat } - pinned.minOf { it.lat }, pinned.maxOf { it.lng } - pinned.minOf { it.lng })
    val fitZoom = when {
        span > 20 -> 3f
        span > 8 -> 5f
        span > 3 -> 7f
        span > 1 -> 9f
        span > 0.3 -> 11f
        else -> 13f
    }
    var zoom by remember(cfg) { mutableFloatStateOf(fitZoom) }
    var viewLat by remember(cfg) { mutableDoubleStateOf(centerLat) }
    var viewLng by remember(cfg) { mutableDoubleStateOf(centerLng) }
    // 瓦片签名按 exp 换签：只拉一次会因首拉失败永久停在「地图加载中…」，
    // 长驻超过签名 TTL 后新瓦片会 401
    LaunchedEffect(Unit) {
        while (true) {
            val next = runCatching { MapRepository.config() }.getOrNull()
            if (next != null) cfg = next
            kotlinx.coroutines.delay(MapRepository.nextConfigDelayMs(cfg != null, next?.exp ?: 0))
        }
    }

    Box(
        Modifier
            .fillMaxWidth()
            .height(200.dp)
            .clip(RoundedCornerShape(12.dp))
            .background(MaterialTheme.colorScheme.surfaceContainerHigh),
    ) {
        val c = cfg
        when {
            c == null -> MapHintText("地图加载中…")
            c.provider.isEmpty() -> MapHintText("未启用瓦片源（后台「地图」启用后自动生效）")
            else -> MapCanvas(
                cfg = c,
                pins = pinned.map {
                    MapPin(id = it.id, lat = it.lat, lng = it.lng, name = it.text, highlight = it.id == picked?.id)
                },
                zoomF = zoom.coerceAtMost(maxZoomFor(c)),
                centerLat = viewLat,
                centerLon = viewLng,
                onGesture = { z, lat, lon ->
                    zoom = z; viewLat = lat; viewLng = lon
                },
                onZoomButton = { delta -> zoom = (zoom + delta).coerceIn(3f, maxZoomFor(c)) },
                modifier = Modifier.fillMaxSize(),
                onPinClick = { pin -> picked = pinned.firstOrNull { it.id == pin.id } },
            )
        }
    }
    picked?.let { p ->
        Text(
            listOf(p.text, p.date, p.badge).filter { it.isNotBlank() }.joinToString(" · "),
            style = MaterialTheme.typography.labelMedium,
            color = MaterialTheme.colorScheme.onSurface,
            modifier = Modifier.padding(top = 6.dp),
        )
    }
}

@Composable
private fun BoxScope.MapHintText(text: String) {
    Text(
        text,
        style = MaterialTheme.typography.labelMedium,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.align(Alignment.Center),
    )
}

// ---------- 详情 ----------

@Composable
private fun DetailBody(data: ChatUi.ResultData, onMediaClick: (Int, List<ChatUi.UiRecord>) -> Unit) {
    // detail 卡的数据保证有单实体（服务端校验 detail_needs_entity）；没有就不画主体
    val e = data.entity ?: return
    if (e.text.isNotBlank()) {
        Text(
            e.text,
            style = MaterialTheme.typography.bodyMedium.copy(
                lineHeight = MaterialTheme.typography.bodyMedium.fontSize * 1.55f,
            ),
            color = MaterialTheme.colorScheme.onSurface,
            maxLines = 14,
            overflow = TextOverflow.Ellipsis,
        )
    }
    if (e.media.isNotEmpty()) {
        Spacer(Modifier.height(8.dp))
        GridBody(e.media, onMediaClick)
    }
    val meta = buildList {
        if (e.date.isNotBlank()) add(e.date)
        if (e.place.isNotBlank()) add(e.place)
        if (e.mood.isNotBlank()) add(e.mood)
        e.meta.forEach { m -> if (m.value.isNotBlank()) add("${m.label} ${m.value}") }
    }
    if (meta.isNotEmpty()) {
        Text(
            meta.joinToString(" · "),
            style = MaterialTheme.typography.labelMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(top = 8.dp),
        )
    }
}

// ==================== 交互卡（确认卡） ====================

/**
 * 确认卡：待你决策的那件事，用**目标形态**摆出来——发动态就长得像那条动态，
 * 不是参数表。pending 时主色描边 + 两个按钮；裁决后收成一行回执。
 */
@Composable
fun ConfirmCard(
    card: ChatRepository.CardSnapshot,
    onResolve: (decision: String) -> Unit,
    /** 到期时刻（本地绝对时刻；0 = 无倒计时） */
    expireAt: Long = 0,
) {
    var now by remember(expireAt) { mutableLongStateOf(System.currentTimeMillis()) }
    LaunchedEffect(expireAt) {
        if (expireAt <= 0) return@LaunchedEffect
        while (System.currentTimeMillis() < expireAt) {
            delay(1000)
            now = System.currentTimeMillis()
        }
        now = System.currentTimeMillis()
    }
    val status = ChatState.effectiveStatus(card.status, expireAt, now)
    val remaining = ChatState.remainingSeconds(expireAt, now)
    var expanded by remember(status) { mutableStateOf(false) }
    val preview = ChatUi.parsePreview(card.preview)
    val pending = status == "pending"
    val working = status == "executing"

    Column(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(CARD_RADIUS))
            .background(MaterialTheme.colorScheme.surface)
            .border(
                if (pending) 1.dp else 0.5.dp,
                if (pending) MaterialTheme.colorScheme.primary.copy(alpha = 0.55f) else LocalAppColors.current.cardEdge,
                RoundedCornerShape(CARD_RADIUS),
            )
            .padding(14.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                ChatUi.intentLabel(card.intent),
                style = MaterialTheme.typography.titleSmall,
                color = MaterialTheme.colorScheme.onSurface,
                modifier = Modifier.weight(1f),
            )
            StatusChip(status, remaining)
        }
        if (card.summary.isNotBlank()) {
            Text(
                card.summary,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(top = 4.dp),
                maxLines = if (expanded) 4 else 2,
                overflow = TextOverflow.Ellipsis,
            )
        }
        Spacer(Modifier.height(10.dp))
        PreviewBody(preview, expanded, onExpand = { expanded = !expanded })

        card.warnings.forEach { w ->
            Text(
                "· $w",
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.error,
                modifier = Modifier.padding(top = 6.dp),
            )
        }

        if (working) {
            Row(
                Modifier.fillMaxWidth().padding(top = 12.dp),
                horizontalArrangement = Arrangement.End,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                CircularProgressIndicator(strokeWidth = 1.6.dp, modifier = Modifier.size(14.dp))
                // 确认执行时先上传附件：进度直接写在卡上（服务端回执到达后覆盖成结果）
                Text(
                    "  " + card.result.orEmpty().ifBlank { "处理中…" },
                    style = MaterialTheme.typography.labelLarge,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        } else if (pending) {
            Row(
                Modifier.fillMaxWidth().padding(top = 12.dp),
                horizontalArrangement = Arrangement.End,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                // 命中区下限 44dp：胶囊本身约 35dp 高，外扩点击区、视觉不变
                Box(
                    Modifier
                        .heightIn(min = 44.dp)
                        .clip(RoundedCornerShape(999.dp))
                        .clickable { onResolve("cancel") },
                    contentAlignment = Alignment.Center,
                ) {
                    Text(
                        "取消",
                        style = MaterialTheme.typography.labelLarge,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier
                            .clip(RoundedCornerShape(999.dp))
                            .background(MaterialTheme.colorScheme.surfaceContainerHigh)
                            .padding(horizontal = 18.dp, vertical = 9.dp),
                    )
                }
                Spacer(Modifier.width(10.dp))
                AppPillButton("确认", onClick = { onResolve("approve") })
            }
        } else {
            val receipt = when (status) {
                "executed", "confirmed" -> "已执行"
                "cancelled" -> "已取消"
                "expired" -> "已过期，未执行"
                else -> "没能完成"
            }
            val detail = card.result.orEmpty()
            Text(
                receipt,
                style = MaterialTheme.typography.bodySmall,
                color = if (status == "expired" || status == "failed")
                    MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(top = 10.dp),
            )
            // 执行结果直接摊在回执行下：不藏进「详情」，一眼看到成没成、成了什么
            if (detail.isNotBlank()) {
                Text(
                    detail,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurface,
                    maxLines = 3,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.padding(top = 2.dp),
                )
            }
        }
    }
}

@Composable
private fun StatusChip(status: String, remainingSeconds: Int) {
    val (label, color, bg) = when (status) {
        "pending" -> Triple(
            "待确认 · ${remainingText(remainingSeconds)}",
            MaterialTheme.colorScheme.primary,
            MaterialTheme.colorScheme.primary.copy(alpha = 0.12f),
        )
        "executed", "confirmed" -> Triple("已执行", MaterialTheme.colorScheme.onSurfaceVariant, MaterialTheme.colorScheme.surfaceContainerHigh)
        "cancelled" -> Triple("已取消", MaterialTheme.colorScheme.onSurfaceVariant, MaterialTheme.colorScheme.surfaceContainerHigh)
        "executing" -> Triple("处理中", MaterialTheme.colorScheme.primary, MaterialTheme.colorScheme.primary.copy(alpha = 0.12f))
        "expired" -> Triple("已过期", MaterialTheme.colorScheme.error, MaterialTheme.colorScheme.error.copy(alpha = 0.10f))
        else -> Triple("失败", MaterialTheme.colorScheme.error, MaterialTheme.colorScheme.error.copy(alpha = 0.10f))
    }
    Text(
        label,
        style = MaterialTheme.typography.labelMedium,
        color = color,
        modifier = Modifier
            .clip(RoundedCornerShape(999.dp))
            .background(bg)
            .padding(horizontal = 8.dp, vertical = 2.dp),
    )
}

/**
 * 目标形态预览：与展示卡同一套 [ChatUi.PreviewView] 形态词汇——
 * 正文（要发的东西）→ 媒体 → 元信息 → 变更 → 条目，按段序渲染。
 */
@Composable
private fun PreviewBody(p: ChatUi.CardPreview, expanded: Boolean, onExpand: () -> Unit) {
    p.views.forEach { v ->
        when (v) {
            is ChatUi.PreviewView.Text -> Text(
                v.text,
                style = MaterialTheme.typography.bodyMedium.copy(
                    lineHeight = MaterialTheme.typography.bodyMedium.fontSize * 1.5f,
                ),
                color = MaterialTheme.colorScheme.onSurface,
                maxLines = if (expanded) 20 else 4,
                overflow = TextOverflow.Ellipsis,
            )
            is ChatUi.PreviewView.Media -> {
                if (v.media.isNotEmpty()) {
                    Spacer(Modifier.height(8.dp))
                    v.media.take(6).chunked(3).forEach { row ->
                        Row(Modifier.fillMaxWidth().padding(vertical = 2.dp)) {
                            row.forEach { r ->
                                AsyncImage(
                                    model = com.bookandmusic.lovelife.appnative.data.Channel.absolute(r.thumb_url),
                                    contentDescription = null,
                                    contentScale = androidx.compose.ui.layout.ContentScale.Crop,
                                    modifier = Modifier
                                        .weight(1f)
                                        .aspectRatio(1f)
                                        .padding(2.dp)
                                        .clip(RoundedCornerShape(12.dp))
                                        .background(MaterialTheme.colorScheme.surfaceContainerHigh),
                                )
                            }
                            repeat(3 - row.size) { Spacer(Modifier.weight(1f)) }
                        }
                    }
                } else if (v.count > 0) {
                    Text(
                        "附 ${v.count} 个媒体",
                        style = MaterialTheme.typography.labelMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(top = 6.dp),
                    )
                }
            }
            is ChatUi.PreviewView.Lines -> v.lines.forEach { line ->
                Row(Modifier.fillMaxWidth().padding(top = 4.dp)) {
                    Text(
                        line.label,
                        style = MaterialTheme.typography.labelMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.width(48.dp),
                    )
                    Text(
                        line.value,
                        style = MaterialTheme.typography.labelMedium,
                        color = MaterialTheme.colorScheme.onSurface,
                        maxLines = if (expanded) 4 else 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f),
                    )
                }
            }
            is ChatUi.PreviewView.Change -> Row(
                Modifier.fillMaxWidth().padding(top = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    v.label,
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.width(48.dp),
                )
                Text(
                    v.from,
                    style = MaterialTheme.typography.labelMedium.copy(textDecoration = TextDecoration.LineThrough),
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Text(
                    "  →  ",
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Text(
                    v.to,
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.primary,
                )
            }
            is ChatUi.PreviewView.Items -> {
                Spacer(Modifier.height(6.dp))
                v.items.take(if (expanded) 10 else 3).forEach { it ->
                    Text(
                        "· ${it.text.ifBlank { "#${it.id}" }}",
                        style = MaterialTheme.typography.labelMedium,
                        color = MaterialTheme.colorScheme.onSurface,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.padding(top = 2.dp),
                    )
                }
                if (v.count > minOf(v.items.size, if (expanded) 10 else 3)) {
                    Text(
                        "等 ${v.count} 条",
                        style = MaterialTheme.typography.labelMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(top = 2.dp),
                    )
                }
            }
        }
    }
    if (expandable(p.views)) {
        Text(
            if (expanded) "收起" else "展开",
            style = MaterialTheme.typography.labelMedium,
            color = MaterialTheme.colorScheme.primary,
            modifier = Modifier
                .padding(top = 6.dp)
                .clip(RoundedCornerShape(8.dp))
                .clickable { onExpand() }
                .padding(vertical = 2.dp),
        )
    }
}

/** 预览有没有长到需要「展开」 */
private fun expandable(views: List<ChatUi.PreviewView>): Boolean = views.any { v ->
    when (v) {
        is ChatUi.PreviewView.Text -> v.text.length > 60
        is ChatUi.PreviewView.Lines -> v.lines.size > 2
        is ChatUi.PreviewView.Media -> v.media.size > 6
        is ChatUi.PreviewView.Items -> v.items.size > 3
        is ChatUi.PreviewView.Change -> false
    }
}

/** 剩余秒数 → 徽标文案（分档：<60 秒显示秒，其余显示分钟；无倒计时显示空） */
internal fun remainingText(seconds: Int): String = when {
    seconds <= 0 -> "已过期"
    seconds < 60 -> "${seconds}秒"
    else -> "${(seconds + 59) / 60}分"
}
