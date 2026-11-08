package com.bookandmusic.lovelife.appnative.data

import kotlinx.serialization.Serializable
import kotlin.math.PI
import kotlinx.serialization.SerialName
import kotlin.math.asinh
import kotlin.math.atan
import kotlin.math.sinh
import kotlin.math.tan

@Serializable
data class MapConfigDto(
    val provider: String = "",
    val tile_api: String = "/api/v1/map/tile",
    val exp: Long = 0,
    val sig: String = "",
    /** 服务端下发的缩放上限（0 = 未下发，回落按 provider 分档） */
    @SerialName("max_zoom") val maxZoom: Int = 0,
)

/**
 * 地图瓦片（WebMercator 自绘）：
 * 签名由 /map/config 下发（TTL 30 分钟），瓦片地址走 /map/tile 代理并带 exp/sig。
 * 页面按 [refreshDelayMs] 定时换签：长驻地图页不会因为签名过期而整片瓦片 401。
 */
object MapRepository {

    /** 首拉失败后的重试间隔（5–10s 短退避）：换签周期是 20 分钟，页面等不起 */
    const val CONFIG_RETRY_MS = 8_000L

    suspend fun config(): MapConfigDto =
        ApiClient.api("/api/v1/map/config")

    /**
     * 下次拉配置的等待：还没拿到配置（首拉失败）走短退避；
     * 已拿到配置才按 exp 换签。
     */
    fun nextConfigDelayMs(hasConfig: Boolean, expSeconds: Long, nowMs: Long = System.currentTimeMillis()): Long =
        if (hasConfig) refreshDelayMs(expSeconds, nowMs) else CONFIG_RETRY_MS

    /**
     * 下次换签的等待时长：按服务端下发的 exp **提前 2 分钟**，夹在 1–25 分钟之间。
     * 夹下限防「exp 已过期」时空转打接口，夹上限防 exp 缺失/异常时长驻页面永不换签。
     */
    fun refreshDelayMs(expSeconds: Long, nowMs: Long = System.currentTimeMillis()): Long {
        if (expSeconds <= 0) return 20 * 60_000L
        val wait = expSeconds * 1000 - nowMs - 120_000
        return wait.coerceIn(60_000L, 25 * 60_000L)
    }

    /** 瓦片图片地址（img 无法带 header，故签名走 query） */
    fun tileUrl(cfg: MapConfigDto, z: Int, x: Int, y: Int): String {
        val base = Channel.base().trimEnd('/')
        val sig = if (cfg.sig.isNotEmpty()) "&exp=${cfg.exp}&sig=${java.net.URLEncoder.encode(cfg.sig, "UTF-8")}" else ""
        return "$base${cfg.tile_api}?z=$z&x=$x&y=$y$sig"
    }
}

/** WebMercator：经纬度 → 瓦片坐标（含小数部分，用于像素级定位） */
fun latLonToTile(lat: Double, lon: Double, z: Int): Pair<Double, Double> {
    val n = 1 shl z
    val x = (lon + 180.0) / 360.0 * n
    val latRad = lat * PI / 180.0
    val y = (1.0 - asinh(tan(latRad)) / PI) / 2.0 * n
    return x to y
}

/** WebMercator 逆变换：瓦片坐标 → 经纬度 */
fun tileToLatLon(x: Double, y: Double, z: Int): Pair<Double, Double> {
    val n = 1 shl z
    val lon = x / n * 360.0 - 180.0
    val lat = atan(sinh(PI * (1.0 - 2.0 * y / n))) * 180.0 / PI
    return lat to lon
}
