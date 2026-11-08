package com.bookandmusic.lovelife.appnative.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectTransformGestures
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Place
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Remove
import androidx.compose.material.icons.outlined.RestartAlt
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.dp
import coil.compose.AsyncImage
import com.bookandmusic.lovelife.appnative.data.MapConfigDto
import com.bookandmusic.lovelife.appnative.data.MapRepository
import com.bookandmusic.lovelife.appnative.data.PointDto
import com.bookandmusic.lovelife.appnative.data.latLonToTile
import com.bookandmusic.lovelife.appnative.data.tileToLatLon
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppChromeSurface
import com.bookandmusic.lovelife.appnative.designsystem.atoms.appChrome
import com.bookandmusic.lovelife.appnative.designsystem.theme.LocalAppColors
import com.bookandmusic.lovelife.appnative.util.formatCoord
import kotlin.math.ceil
import kotlin.math.floor
import kotlin.math.ln
import kotlin.math.pow
import kotlin.math.roundToInt

/** 地图上展示的点位（足迹页 = PointDto；选点页 = 单个标记） */
data class MapPin(
    val id: Long,
    val lat: Double,
    val lng: Double,
    val name: String,
    val highlight: Boolean = false,
)

/**
 * 显示上限：服务端下发 max_zoom（= 后台「探测」的数据物理上限）减一——顶档留给取图补偿层，
 * 每一档显示都有高清补偿（配 18 → 放大到 17 顶，z17 取 z18 瓦片）。
 * 未下发（0/越界）时按 provider 分档兜底。
 */
fun maxZoomFor(cfg: MapConfigDto): Float {
    val dataMax = if (cfg.maxZoom in 3..20) cfg.maxZoom else if (cfg.provider == "tianditu") 14 else 17
    return (dataMax - 1).coerceAtLeast(3).toFloat()
}

/** 取图层上限（补偿用）：= 数据物理上限本身——客户端 clamp 保证不请求超出数据的层级（防错乱）。 */
fun dataZoomFor(cfg: MapConfigDto): Int =
    if (cfg.maxZoom in 3..20) cfg.maxZoom else if (cfg.provider == "tianditu") 14 else 17

/**
 * 可交互瓦片地图（足迹页与添加地点选点共用）：
 * WebMercator 瓦片网格 + 平移/缩放（detectTransformGestures）+ 单指点击选点 +
 * 水滴 pin（名称白标签）+ 右上 chrome 缩放控件。
 *
 * 状态由调用方持有（zoomF / centerLat / centerLon / selected），本组件只渲染与上报手势。
 * [onMapTap] 非空时开启「点选模式」：点击地图任意处回调经纬度（添加地点用）；
 * 足迹页不传，单击落到 pin 命中区（pin 自带 clickable）。
 */
@Composable
fun MapCanvas(
    cfg: MapConfigDto,
    pins: List<MapPin>,
    zoomF: Float,
    centerLat: Double,
    centerLon: Double,
    /** 手势结束的最终态回调（缩放围绕双指中点后的 zoom/中心）——宿主存为持久源 */
    onGesture: (zoom: Float, lat: Double, lon: Double) -> Unit,
    onZoomButton: (Float) -> Unit,
    modifier: Modifier = Modifier,
    onReset: (() -> Unit)? = null,
    onPinClick: ((MapPin) -> Unit)? = null,
    onMapTap: ((lat: Double, lng: Double) -> Unit)? = null,
    /** 缩放控件距画布顶的额外偏移：宿主有浮层顶栏时下移避让（选点页 = 顶栏高 + 间距；足迹页 0） */
    zoomControlOffsetY: androidx.compose.ui.unit.Dp = 0.dp,
) {
    val colors = LocalAppColors.current
    val density = LocalDensity.current
    val baseTilePx = with(density) { 256.dp.toPx() }

    // 手势内聚：双指手势期间用本地渲染态连续合成（不依赖宿主 state 每帧重组——
    // 宿主重组一帧滞后会让 pan 换算用上旧瓦片尺寸，造成缩放漂移）。
    // 宿主 state 仍是唯一持久源：手势每步合成完毕后同步回去。
    var renderZoom by androidx.compose.runtime.remember(zoomF, centerLat, centerLon) {
        androidx.compose.runtime.mutableFloatStateOf(zoomF)
    }
    var renderLat by androidx.compose.runtime.remember(zoomF, centerLat, centerLon) {
        androidx.compose.runtime.mutableDoubleStateOf(centerLat)
    }
    var renderLon by androidx.compose.runtime.remember(zoomF, centerLat, centerLon) {
        androidx.compose.runtime.mutableDoubleStateOf(centerLon)
    }
    // 宿主主动改（控件缩放/重置/列表定位）→ 以宿主为准刷新渲染态
    androidx.compose.runtime.LaunchedEffect(zoomF, centerLat, centerLon) {
        renderZoom = zoomF; renderLat = centerLat; renderLon = centerLon
    }
    // 首屏瓦片是否已有任意一片加载成功：瓦片是逐片异步拉的，内网也要数秒，
    // 这段空白期不给提示会像「地图挂了」
    var tileShown by androidx.compose.runtime.remember { androidx.compose.runtime.mutableStateOf(false) }
    val zoom = renderZoom.toInt()
    val scaleFactor = 2f.pow(renderZoom - zoom)
    // 高清渲染：瓦片按「显示层级 + 密度层级」取图（density=3 → +1.585 层）。
    // 瓦片固定 256 物理像素，256dp 在 density=3 屏上 = 768 物理像素——不补层就是 3 倍拉伸发虚。
    // zReq 不超源上限（超限退回上限层，接受放大——大层级无瓦片）。
    val densityZoom = (ln(density.density) / ln(2f))
    // 取图 clamp 到数据物理上限（显示层已被 maxZoomFor 限在上限-1 → 正常路径永有补偿层）
    val zReq = (renderZoom + densityZoom).toInt().coerceAtMost(dataZoomFor(cfg))
    val dz = zReq - zoom
    val tileSizePx = baseTilePx * scaleFactor / 2f.pow(dz)  // 瓦片显示尺寸：更密的瓦片拼同样屏幕面积
    val tilePxState = androidx.compose.runtime.rememberUpdatedState(tileSizePx)
    val (cx, cy) = latLonToTile(renderLat, renderLon, zReq)
    val renderState = androidx.compose.runtime.rememberUpdatedState(Triple(renderZoom, renderLat, renderLon))
    val gestureCallback = androidx.compose.runtime.rememberUpdatedState(onGesture)

    BoxWithConstraints(
        modifier
            .fillMaxSize()
            // 瓦片用 .offset{} 绝对定位、按 ±3 屏范围铺，必须在本组件内裁剪：
            // 依赖宿主 clip 会漏（表单页只给了固定高度 Box，瓦片会溢出到整个滚动区）
            .clipToBounds()
            .background(MaterialTheme.colorScheme.surfaceContainerHigh)
            .pointerInput(Unit) {
                detectTransformGestures { centroid, pan, gestureZoom, _ ->
                    val (curZoom, curLat, curLon) = renderState.value
                    val curTilePx = tilePxState.value
                    val w = size.width.toFloat()
                    val h = size.height.toFloat()
                    // 1) 缩放围绕双指中点（锚定视口中心会让捏合时内容绕远点跳变）
                    val nextZoom = (curZoom + ln(gestureZoom) / ln(2f)).coerceIn(3f, maxZoomFor(cfg))
                    val zoomRatio = 2f.pow(nextZoom - curZoom)
                    val (fcx, fcy) = latLonToTile(curLat, curLon, curZoom.toInt())
                    // 中点在屏幕上的位置（显示层瓦片坐标差）：curTilePx 是 zReq 层瓦片显示宽，
                    // 显示层一瓦片 = 2^dz 张 zReq 瓦片，须除回——否则补偿过 2^dz 倍（中心过移）
                    val dzCur = ((curZoom + densityZoom).toInt().coerceAtMost(dataZoomFor(cfg))) - curZoom.toInt()
                    val dispTilePx = curTilePx * 2f.pow(dzCur)
                    val offX = (centroid.x - w / 2) / dispTilePx
                    val offY = (centroid.y - h / 2) / dispTilePx
                    // 缩放后：保持中点下的地理点仍在指尖中点 → 中心需反向补偿
                    val newCx = fcx + offX - offX / zoomRatio - pan.x / dispTilePx / zoomRatio
                    val newCy = fcy + offY - offY / zoomRatio - pan.y / dispTilePx / zoomRatio
                    val (nLat, nLon) = tileToLatLon(newCx, newCy, curZoom.toInt())
                    renderZoom = nextZoom
                    renderLat = nLat.coerceIn(-85.0, 85.0)
                    renderLon = ((nLon + 180.0) % 360.0) - 180.0
                    // 同步宿主（持久源）：一次性回传最终态，宿主不自行重算（帧内一致性由本地保证）
                    gestureCallback.value(nextZoom, renderLat, renderLon)
                }
            }
            .pointerInput(onMapTap != null) {
                if (onMapTap == null) return@pointerInput
                detectTapGestures { offset ->
                    val zx = cx + (offset.x - size.width / 2) / tilePxState.value
                    val zy = cy + (offset.y - size.height / 2) / tilePxState.value
                    val (lat, lon) = tileToLatLon(zx, zy, zReq)
                    onMapTap.invoke(lat, lon)
                }
            },
    ) {
        val centerPxX = with(density) { (maxWidth / 2).toPx() }.roundToInt()
        val centerPxY = with(density) { (maxHeight / 2).toPx() }.roundToInt()
        val baseX = floor(cx).toInt()
        val baseY = floor(cy).toInt()
        // 覆盖范围按视口动态算（原硬编码 ±2/±3 在长竖屏上不够：z5 时 11 行只铺 7 行，出现整条灰带）
        val viewW = with(density) { maxWidth.toPx() }
        val viewH = with(density) { maxHeight.toPx() }
        val rangeX = (ceil(viewW / tileSizePx / 2).toInt() + 1).coerceAtLeast(2)
        val rangeY = (ceil(viewH / tileSizePx / 2).toInt() + 1).coerceAtLeast(3)
        // 连续舍入：同一行/列的瓦片共享同一基准，消除非整数 zoom 下相邻瓦片各舍各的造成的 1px 缝
        val tilePxI = tileSizePx.roundToInt()
        val baseOffsetX = ((baseX - cx) * tileSizePx).roundToInt()
        val baseOffsetY = ((baseY - cy) * tileSizePx).roundToInt()
        for (dx in -rangeX..rangeX) {
            for (dy in -rangeY..rangeY) {
                val tx = baseX + dx
                val ty = baseY + dy
                val max = 1 shl zReq
                if (ty < 0 || ty >= max) continue
                val wrappedX = ((tx % max) + max) % max
                val offsetX = baseOffsetX + dx * tilePxI
                val offsetY = baseOffsetY + dy * tilePxI
                // 加载失败/加载中占位与画布底同色：上游抖动时无缝观感（不留灰白条带）
                Box(
                    Modifier
                        .offset { IntOffset(centerPxX + offsetX, centerPxY + offsetY) }
                        .size(with(density) { tileSizePx.toDp() })
                        .background(MaterialTheme.colorScheme.surfaceContainerHigh),
                ) {
                    AsyncImage(
                        model = MapRepository.tileUrl(cfg, zReq, wrappedX, ty),
                        contentDescription = null,
                        contentScale = ContentScale.FillBounds,
                        onSuccess = { tileShown = true },
                        modifier = Modifier.fillMaxSize(),
                    )
                }
            }
        }
        // pin：水滴图标 + 名称白标签，命中放大
        pins.forEach { p ->
            val (px, py) = latLonToTile(p.lat, p.lng, zReq)
            val offsetX = ((px - cx) * tileSizePx).roundToInt()
            val offsetY = ((py - cy) * tileSizePx).roundToInt()
            val isHi = p.highlight
            Column(
                Modifier
                    .offset { IntOffset(centerPxX + offsetX, centerPxY + offsetY) }
                    .clickable(enabled = onPinClick != null) { onPinClick?.invoke(p) },
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                Icon(
                    Icons.Filled.Place,
                    contentDescription = p.name,
                    tint = if (isHi) MaterialTheme.colorScheme.error else colors.semGreen,
                    modifier = Modifier
                        .offset(y = (-28).dp)
                        .size(if (isHi) 36.dp else 28.dp),
                )
                Text(
                    p.name.substringBefore("·").trim(),
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.onSurface,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier
                        .offset(y = (-30).dp)
                        .clip(RoundedCornerShape(8.dp))
                        .background(MaterialTheme.colorScheme.surface)
                        .padding(horizontal = 7.dp, vertical = 2.dp),
                )
            }
        }
        if (!tileShown) {
            Text(
                "地图加载中…",
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier
                    .align(Alignment.TopCenter)
                    .padding(top = 14.dp + zoomControlOffsetY)
                    .clip(RoundedCornerShape(999.dp))
                    .background(MaterialTheme.colorScheme.surface.copy(alpha = 0.92f))
                    .padding(horizontal = 12.dp, vertical = 5.dp),
            )
        }
        // 缩放控件（chrome，唯一允许的 chrome 浮层之一）
        Column(
            Modifier
                .align(Alignment.TopEnd)
                .padding(top = 12.dp + zoomControlOffsetY, end = 12.dp)
                .clip(RoundedCornerShape(12.dp))
                .appChrome(AppChromeSurface.Floating, RoundedCornerShape(12.dp)),
        ) {
            MapControlButton(Icons.Outlined.Add, "放大") { onZoomButton(1f) }
            MapControlButton(Icons.Outlined.Remove, "缩小") { onZoomButton(-1f) }
            if (onReset != null) {
                MapControlButton(Icons.Outlined.RestartAlt, "重置") { onReset() }
            }
        }
        Text(
            "z${formatCoord(zoomF.toDouble(), 1)}${if (pins.isNotEmpty()) " · ${pins.size} 个点位" else ""}",
            style = MaterialTheme.typography.labelMedium,
            color = Color.White,
            modifier = Modifier
                .align(Alignment.BottomStart)
                .padding(12.dp)
                .clip(RoundedCornerShape(8.dp))
                .background(Color.Black.copy(alpha = 0.45f))
                .padding(horizontal = 8.dp, vertical = 4.dp),
        )
    }
}

@Composable
private fun MapControlButton(
    icon: androidx.compose.ui.graphics.vector.ImageVector,
    label: String,
    onClick: () -> Unit,
) {
    val colors = LocalAppColors.current
    Box(
        Modifier
            .size(width = 38.dp, height = 34.dp)
            .clickable { onClick() },
        contentAlignment = Alignment.Center,
    ) {
        Icon(icon, contentDescription = label, tint = colors.tint, modifier = Modifier.size(17.dp))
    }
}
