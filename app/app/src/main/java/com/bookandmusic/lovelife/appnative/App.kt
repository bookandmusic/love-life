package com.bookandmusic.lovelife.appnative

import android.app.Application
import coil.ImageLoader
import coil.ImageLoaderFactory
import com.bookandmusic.lovelife.appnative.data.Channel
import okhttp3.OkHttpClient

/**
 * 应用入口：为 Coil 提供全局 ImageLoader。
 *
 * 为什么必须自定义：媒体（缩略图/照片/头像/地图瓦片）经绿联中转加载时，
 * 绿联要求每个请求带 `ugreen-proxy-token` cookie——Coil 默认的 OkHttpClient
 * 不带，请求被 302 到登录页，表现为「图裂」。JSON 接口走 ApiClient（自带
 * cookie），所以文字一直正常、只有媒体裂。
 *
 * cookie 动态读取（@Volatile 读，无锁）：绿联凭据一小时续期、失败换通道，
 * 启动时固定一份会在续期后失效。非绿联通道下 cookieValue 为空串，
 * 拦截器不加头，行为与默认一致。
 */
class App : Application(), ImageLoaderFactory {
    override fun newImageLoader(): ImageLoader {
        val http = OkHttpClient.Builder()
            .addInterceptor { chain ->
                val cookie = Channel.cookieValue()
                val req = if (cookie.isEmpty()) {
                    chain.request()
                } else {
                    chain.request().newBuilder()
                        .header("Cookie", cookie)
                        .build()
                }
                chain.proceed(req)
            }
            .build()
        return ImageLoader.Builder(this)
            .okHttpClient(http)
            .crossfade(true)
            .build()
    }
}
