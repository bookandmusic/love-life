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
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.Place
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Place
import androidx.compose.material.icons.outlined.Remove
import androidx.compose.material.icons.outlined.RestartAlt
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableDoubleStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.bookandmusic.lovelife.appnative.data.MapConfigDto
import com.bookandmusic.lovelife.appnative.data.MapRepository
import com.bookandmusic.lovelife.appnative.data.PointDto
import com.bookandmusic.lovelife.appnative.data.PointsRepository
import com.bookandmusic.lovelife.appnative.data.errText
import com.bookandmusic.lovelife.appnative.data.latLonToTile
import com.bookandmusic.lovelife.appnative.data.tileToLatLon
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppCard
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppConfirmSheet
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppEmptyState
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppRowDivider
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppIconBadge
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppIconBadgeSize
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppSegmentedControl
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppActionSheet
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppMenuItem
import androidx.compose.material.icons.outlined.Delete
import com.bookandmusic.lovelife.appnative.designsystem.atoms.appClickAndLongPress
import com.bookandmusic.lovelife.appnative.designsystem.atoms.onAppLongPress
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppToast
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppChromeSurface
import com.bookandmusic.lovelife.appnative.designsystem.atoms.appChrome
import com.bookandmusic.lovelife.appnative.designsystem.theme.LocalAppColors
import kotlinx.coroutines.launch
import kotlin.math.roundToInt

/**
 * 足迹 = 空间视图：分段控件（地图 / 列表）；
 * 地图 = 圆角容器 + WebMercator 瓦片 + 水滴 pin（带名称白标签）+ 右上玻璃缩放控件；
 * 列表 = 点位卡（绿色图标块 + 名称 + 日期 + 描述 + chevron），点击回地图定位。
 */
@Composable
fun MapScreen(
    contentPadding: PaddingValues,
    onOpenPointForm: () -> Unit,
) {
    var cfg by remember { mutableStateOf<MapConfigDto?>(null) }
    var points by remember { mutableStateOf<List<PointDto>?>(null) }
    var view by remember { mutableIntStateOf(0) }
    var pendingPoint by remember { mutableStateOf<PointDto?>(null) }
    var menuPoint by remember { mutableStateOf<PointDto?>(null) }
    var toast by remember { mutableStateOf<String?>(null) }
    // 默认视图 = 中国全境（缩放 3 恰好容纳全国经度跨度）；不自动定位到首个点位
    var zoomF by remember { mutableFloatStateOf(3f) }
    var centerLat by remember { mutableDoubleStateOf(35.0) }
    var centerLon by remember { mutableDoubleStateOf(104.0) }
    var highlightId by remember { mutableStateOf<Long?>(null) }
    val scope = androidx.compose.runtime.rememberCoroutineScope()

    LaunchedEffect(Unit) {
        points = runCatching { PointsRepository.list() }.getOrNull()
    }
    // 瓦片签名按 exp 定时换：长驻地图页不会在 30 分钟后变成一片空白；
    // 首拉失败走短退避（见 MapRepository.nextConfigDelayMs），不会停在「地图加载中…」等一个换签周期
    LaunchedEffect(Unit) {
        while (true) {
            val next = runCatching { MapRepository.config() }.getOrNull()
            if (next != null) cfg = next
            kotlinx.coroutines.delay(MapRepository.nextConfigDelayMs(cfg != null, next?.exp ?: 0))
        }
    }

    Box(Modifier.fillMaxSize()) {
        Column(Modifier.fillMaxSize().padding(top = contentPadding.calculateTopPadding())) {
            AppSegmentedControl(
                options = listOf("地图", "列表"),
                selected = view,
                onSelect = { view = it },
                modifier = Modifier.padding(horizontal = 16.dp).padding(top = 4.dp, bottom = 8.dp),
            )
            when {
                view == 1 -> PointList(
                    points = points,
                    contentPadding = contentPadding,
                    onMenuRequest = { menuPoint = it },
                    onLocate = { p ->
                        centerLat = p.lat
                        centerLon = p.lng
                        zoomF = 14f
                        highlightId = p.id
                        view = 0
                    },
                    onAdd = onOpenPointForm,
                )
                cfg == null -> MapHint("地图加载中…")
                cfg!!.provider.isEmpty() -> MapHint("未启用瓦片源（后台「地图」启用后自动生效）")
                else -> Box(
                    Modifier
                        .padding(horizontal = 16.dp)
                        .padding(bottom = contentPadding.calculateBottomPadding())
                        .fillMaxSize()
                        .clip(RoundedCornerShape(12.dp)),
                ) {
                    MapCanvas(
                        cfg = cfg!!,
                        pins = points.orEmpty().map {
                            MapPin(id = it.id, lat = it.lat, lng = it.lng, name = it.name, highlight = it.id == highlightId)
                        },
                        zoomF = zoomF,
                        centerLat = centerLat,
                        centerLon = centerLon,
                        onGesture = { z, nLat, nLon ->
                            zoomF = z; centerLat = nLat; centerLon = nLon
                        },
                        onZoomButton = { delta ->
                            zoomF = (zoomF + delta).coerceIn(3f, maxZoomFor(cfg!!))
                        },
                        onReset = {
                            centerLat = 35.0
                            centerLon = 104.0
                            zoomF = 3f
                            highlightId = null
                        },
                        onPinClick = { p ->
                            centerLat = p.lat
                            centerLon = p.lng
                            zoomF = (zoomF + 1f).coerceAtMost(maxZoomFor(cfg!!))
                            highlightId = p.id
                        },
                    )

                }
            }
        }

        menuPoint?.let { target ->
            AppActionSheet(
                visible = true,
                title = target.name,
                items = listOf(
                    AppMenuItem(label = "移入回收站", icon = Icons.Outlined.Delete, danger = true) {
                        pendingPoint = target
                    }
                ),
                onDismiss = { menuPoint = null },
            )
        }

        pendingPoint?.let { target ->
            AppConfirmSheet(
                visible = true,
                title = "移入回收站",
                message = "「${target.name}」将移入回收站（动态本身不受影响）。",
                confirmText = "移入回收站",
                onConfirm = {
                    scope.launch {
                        runCatching { PointsRepository.delete(target.id) }
                            .onSuccess {
                                points = points?.filter { it.id != target.id }
                                toast = "已移入回收站"
                            }
                            .onFailure { toast = "删除失败：${errText(it, "")}" }
                        pendingPoint = null
                    }
                },
                onDismiss = { pendingPoint = null },
            )
        }
        AppToast(text = toast, onDismiss = { toast = null })
    }
}

@Composable
private fun MapHint(text: String) {
    Box(
        Modifier
            .padding(horizontal = 16.dp)
            .fillMaxSize()
            .clip(RoundedCornerShape(12.dp))
            .background(MaterialTheme.colorScheme.surfaceContainerHigh),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text,
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

/** 点位列表：绿色图标块 + 名称 + 日期 + 描述 + chevron。 */
@Composable
private fun PointList(
    points: List<PointDto>?,
    contentPadding: androidx.compose.foundation.layout.PaddingValues,
    onMenuRequest: (PointDto) -> Unit,
    onLocate: (PointDto) -> Unit,
    onAdd: () -> Unit,
) {
    val colors = LocalAppColors.current
    when {
        points == null -> Box(Modifier.fillMaxWidth().padding(24.dp), contentAlignment = Alignment.Center) {
            Text(
                "加载中…",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        points.isEmpty() -> AppEmptyState(
            title = "足迹还空着",
            hint = "标记第一个地点，慢慢攒成你们的足迹。",
            icon = Icons.Outlined.Place,
            cta = "标记地点",
            onCta = onAdd,
        )
        else -> LazyColumn(
            Modifier.fillMaxSize(),
            // 底部让开悬浮导航条：漏掉会让最后一条点位被压住
            contentPadding = PaddingValues(bottom = contentPadding.calculateBottomPadding()),
        ) {
            items(points, key = { it.id }) { p ->
                // 去卡片容器：行 + 细线分隔（与纪念日/设置统一），行内左右 16dp
                Column(
                    Modifier.fillMaxWidth().appClickAndLongPress(
                        onClick = { onLocate(p) },
                        onLongPress = { onMenuRequest(p) },
                    )
                ) {
                    Row(
                        Modifier
                            .fillMaxWidth()
                            .padding(horizontal = 16.dp, vertical = 14.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(12.dp),
                    ) {
                        AppIconBadge(
                            icon = Icons.Outlined.Place,
                            tint = colors.semGreen,
                            background = colors.semGreenSoft,
                            size = AppIconBadgeSize.Xl,
                            corner = 8.dp,
                        )
                        Column(Modifier.weight(1f)) {
                            Text(
                                p.name,
                                style = MaterialTheme.typography.titleMedium,
                                color = MaterialTheme.colorScheme.onSurface,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                            )
                            p.visit_date?.takeIf { it.isNotEmpty() }?.let {
                                Text(
                                    it,
                                    style = MaterialTheme.typography.labelMedium,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                    modifier = Modifier.padding(top = 2.dp),
                                )
                            }
                            p.description?.takeIf { it.isNotEmpty() }?.let {
                                Text(
                                    it,
                                    style = MaterialTheme.typography.bodySmall,
                                    color = MaterialTheme.colorScheme.onSurface.copy(alpha = 0.75f),
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                    modifier = Modifier.padding(top = 3.dp),
                                )
                            }
                        }
                        Icon(
                            Icons.AutoMirrored.Filled.KeyboardArrowRight,
                            contentDescription = null,
                            tint = MaterialTheme.colorScheme.outline,
                            modifier = Modifier.size(18.dp),
                        )
                    }
                    AppRowDivider(modifier = Modifier.padding(start = 16.dp))
                }
            }
            item { Spacer(Modifier.height(8.dp)) }
        }
    }
}
