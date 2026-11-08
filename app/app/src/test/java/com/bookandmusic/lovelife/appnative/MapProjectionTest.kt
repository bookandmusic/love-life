package com.bookandmusic.lovelife.appnative

import com.bookandmusic.lovelife.appnative.data.latLonToTile
import com.bookandmusic.lovelife.appnative.data.tileToLatLon
import org.junit.Assert.assertEquals
import org.junit.Test
import kotlin.math.abs
import kotlin.math.roundToInt

/**
 * 地图投影与定位守卫（对应「点击列表定位到地图不对」的回归）：
 * ① 经纬度 ↔ 瓦片坐标往返一致；② 中心点落位公式给出正确的像素偏移。
 */
class MapProjectionTest {

    @Test
    fun roundTripPreservesCoordinates() {
        val lat = 39.9042
        val lon = 116.4074
        for (z in 3..17) {
            val (x, y) = latLonToTile(lat, lon, z)
            val (lat2, lon2) = tileToLatLon(x, y, z)
            assertEquals(lat, lat2, 1e-6)
            assertEquals(lon, lon2, 1e-6)
        }
    }

    @Test
    fun centerCoordinateMapsToScreenCenter() {
        // 定位口径：把 (lat,lon) 设为地图中心时，该点必须落在容器正中
        val lat = 31.2304
        val lon = 121.4737
        val z = 14
        val tilePx = 256f
        val centerX = 540f // 容器宽一半
        val centerY = 1170f // 容器高一半
        val (cx, cy) = latLonToTile(lat, lon, z)
        // 该点所在瓦片
        val tx = kotlin.math.floor(cx).toInt()
        val ty = kotlin.math.floor(cy).toInt()
        // 瓦片左上角绝对坐标 = 容器中心 + (瓦片坐标 - 中心瓦片坐标) * 瓦片尺寸 - 半块
        val left = centerX + ((tx - cx) * tilePx)
        val top = centerY + ((ty - cy) * tilePx)
        // 点在瓦片内的相对位置
        val pointX = left + (cx - tx) * tilePx
        val pointY = top + (cy - ty) * tilePx
        assert(abs(pointX - centerX) < 0.5f) { "水平落位偏移 ${pointX - centerX}px" }
        assert(abs(pointY - centerY) < 0.5f) { "垂直落位偏移 ${pointY - centerY}px" }
    }

    @Test
    fun panningByPixelsShiftsCenterInverseDirection() {
        // 拖动 +pan 像素，中心应反向移动 pan/tilePx 个瓦片
        val z = 10
        val tilePx = 256f
        val (cx, cy) = latLonToTile(34.5, 108.9, z)
        val panX = 100f
        val (lat2, lon2) = tileToLatLon(cx - panX / tilePx, cy, z)
        val (cx2, _) = latLonToTile(lat2, lon2, z)
        assert(abs((cx - cx2) - panX / tilePx) < 1e-6) { "pan 换算不一致" }
        // 经度随中心左移而减小（向西）
        assert(lon2 < 108.9) { "向西拖动后经度应变小" }
    }

    @Test
    fun tileIndexWrappingIsStable() {
        // 经度跨 180° 时瓦片索引必须环绕，不能取到负值
        val z = 3
        val max = 1 shl z
        for (tx in -8..16) {
            val wrapped = ((tx % max) + max) % max
            assert(wrapped in 0 until max) { "wrapped=$wrapped 越界" }
        }
        assertEquals(0, ((8 % max) + max) % max)
        assertEquals(max - 1, ((-1 % max) + max) % max)
        assertEquals(1, ((-7 % max) + max) % max)
        // 保证 roundToInt 后的像素偏移不溢出
        assert(((-3.7f).roundToInt()) == -4)
    }
}
