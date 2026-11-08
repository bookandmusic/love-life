package com.bookandmusic.lovelife.appnative.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBars
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Place
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableDoubleStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.unit.dp
import com.bookandmusic.lovelife.appnative.data.MapConfigDto
import com.bookandmusic.lovelife.appnative.data.MapRepository
import com.bookandmusic.lovelife.appnative.data.latLonToTile
import com.bookandmusic.lovelife.appnative.data.tileToLatLon
import com.bookandmusic.lovelife.appnative.util.formatCoord
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppButton
import com.bookandmusic.lovelife.appnative.designsystem.atoms.AppChromeSurface
import com.bookandmusic.lovelife.appnative.designsystem.atoms.appChrome
import com.bookandmusic.lovelife.appnative.designsystem.theme.LocalAppColors

/**
 * 全屏地图选点页（微信「选择位置」同形态）：地图铺满 + 中心固定 pin + 底部坐标卡 + 确认。
 *
 * 为什么全屏而不是表单内嵌 200dp 小地图：内嵌块在滚动容器里既与滚动手势打架，
 * 又因瓦片绝对定位需要宿主裁剪才能不溢出——全屏页把这两类问题从结构上消掉。
 *
 * 选点口径 = **地图中心**（拖动/缩放地图，pin 不动），比「点击落点」更稳：
 * 手指点击会被 pin 图标遮挡、且双击缩放与点选冲突。
 */
@Composable
fun MapPickerScreen(
    onCancel: () -> Unit,
    onPicked: (lat: Double, lng: Double) -> Unit,
    initialLat: Double? = null,
    initialLng: Double? = null,
    title: String = "选择位置",
) {
    val colors = LocalAppColors.current
    var cfg by remember { mutableStateOf<MapConfigDto?>(null) }
    // 默认与足迹地图同：全国视图 3f（带坐标进来 = 定位语义，14f）
    val initZoom = if (initialLat != null) 14f else 3f
    val initLat = initialLat ?: 35.0
    val initLng = initialLng ?: 104.0
    var zoom by remember { mutableFloatStateOf(initZoom) }
    var lat by remember { mutableDoubleStateOf(initLat) }
    var lng by remember { mutableDoubleStateOf(initLng) }

    LaunchedEffect(Unit) {
        while (true) {
            val next = runCatching { MapRepository.config() }.getOrNull()
            if (next != null) cfg = next
            kotlinx.coroutines.delay(MapRepository.refreshDelayMs(next?.exp ?: 0))
        }
    }

    androidx.compose.ui.window.Dialog(
        onDismissRequest = onCancel,
        properties = androidx.compose.ui.window.DialogProperties(usePlatformDefaultWidth = false),
    ) {
    Box(Modifier.fillMaxSize().background(MaterialTheme.colorScheme.surfaceContainerHigh)) {
        val c = cfg
        if (c != null && c.provider.isNotEmpty()) {
            MapCanvas(
                cfg = c,
                pins = emptyList(),
                zoomF = zoom,
                centerLat = lat,
                centerLon = lng,
                onGesture = { z, nLat, nLon ->
                    zoom = z; lat = nLat; lng = nLon
                },
                onZoomButton = { delta -> zoom = (zoom + delta).coerceIn(3f, maxZoomFor(c)) },
                // 三控件对齐足迹地图：重置 = 回打开时的初始中心与缩放
                onReset = { zoom = initZoom; lat = initLat; lng = initLng },
                onPinClick = null,
                onMapTap = null,
                modifier = Modifier.fillMaxSize(),
                // 缩放控件下移避让本组件的浮层顶栏（statusBars + 44dp 栏高 + 12dp 间距）
                zoomControlOffsetY = 56.dp + WindowInsets.statusBars.asPaddingValues().calculateTopPadding(),
            )
        } else {
            Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                Text(
                    "未启用瓦片源——请手动填写经纬度",
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }

        // 中心固定 pin（地图动、pin 不动）
        Icon(
            Icons.Filled.Place,
            contentDescription = "选点位置",
            tint = colors.semGreen,
            modifier = Modifier
                .align(Alignment.Center)
                .offset(y = (-26).dp)
                .size(38.dp),
        )

        // 顶栏（chrome）
        Row(
            Modifier
                .align(Alignment.TopCenter)
                .fillMaxWidth()
                .appChrome(AppChromeSurface.Bar)
                .padding(top = WindowInsets.statusBars.asPaddingValues().calculateTopPadding())
                .height(44.dp)
                .padding(horizontal = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Box(
                Modifier.size(38.dp).clip(RoundedCornerShape(999.dp)).clickable { onCancel() },
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    Icons.AutoMirrored.Filled.ArrowBack,
                    contentDescription = "返回",
                    tint = MaterialTheme.colorScheme.primary,
                    modifier = Modifier.size(21.dp),
                )
            }
            Spacer(Modifier.weight(1f))
            Text(
                title,
                style = MaterialTheme.typography.titleMedium,
                color = MaterialTheme.colorScheme.onSurface,
            )
            Spacer(Modifier.weight(1f))
            Spacer(Modifier.size(38.dp))
        }

        // 底部坐标卡 + 确认
        Column(
            Modifier
                .align(Alignment.BottomCenter)
                .fillMaxWidth()
                .appChrome(AppChromeSurface.Bar)
                .padding(horizontal = 16.dp)
                .padding(top = 12.dp, bottom = 12.dp)
                .padding(bottom = WindowInsets.navigationBars.asPaddingValues().calculateBottomPadding()),
        ) {
            Text(
                "拖动地图选择位置，中心图钉即所选点",
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Text(
                "${formatCoord(lat)}, ${formatCoord(lng)}",
                style = MaterialTheme.typography.bodyLarge,
                color = MaterialTheme.colorScheme.onSurface,
                modifier = Modifier.padding(top = 2.dp),
            )
            Row(
                Modifier.fillMaxWidth().padding(top = 10.dp),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                AppButton(
                    "取消",
                    onClick = onCancel,
                    variant = com.bookandmusic.lovelife.appnative.designsystem.atoms.AppButtonVariant.Ghost,
                    modifier = Modifier.weight(1f),
                )
                AppButton(
                    "确定",
                    onClick = { onPicked(lat, lng) },
                    modifier = Modifier.weight(1f),
                )
            }
        }
    }
    }
}
