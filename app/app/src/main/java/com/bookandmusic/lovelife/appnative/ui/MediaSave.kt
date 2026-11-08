package com.bookandmusic.lovelife.appnative.ui

import android.content.Context
import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import com.bookandmusic.lovelife.appnative.data.Channel
import com.bookandmusic.lovelife.appnative.data.PostMediaUi
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import okhttp3.OkHttpClient
import okhttp3.Request
import java.io.IOException
import java.util.concurrent.TimeUnit

/**
 * 媒体「保存到本机」的共享实现：动态卡 / 查看器 / 相册共用同一个系统「创建文档」启动器。
 *
 * 下载走 OkHttp 而不是 `URL.openStream()`：媒体地址在绿联通道下需要中转会话（Cookie），
 * 且必须限时（无超时会一直转圈）；还要校验响应确实是 image/video/audio——
 * 通道把请求 302 到登录页时，`openStream()` 会把那张 HTML 当成功保存下来。
 * 只限连接与单次读取：整体下载不设总时长上限——单文件可达 500MB，慢网下必然超过一分钟，
 * 加了总时长会让大文件「必保存失败」，而失败路径还会删掉刚创建的文档。
 * 扩展名取媒体 URL 的真实后缀（取不到按类型回退），结果经 onToast 反馈（失败给具体原因）；
 * 用户选完（或取消）后回调 onFinished——调用方在此收起动作单。
 */
@Composable
internal fun rememberMediaSaver(
    onToast: (String) -> Unit,
    onFinished: () -> Unit = {},
): (PostMediaUi) -> Unit {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var pending by remember { mutableStateOf<PostMediaUi?>(null) }
    val client = remember {
        OkHttpClient.Builder()
            .connectTimeout(15, TimeUnit.SECONDS)
            // 读取超时按「单次读」计：连接卡住不吐数据会中断，正常的大文件下载不受限制
            .readTimeout(15, TimeUnit.SECONDS)
            .build()
    }
    val launcher = rememberLauncherForActivityResult(
        ActivityResultContracts.CreateDocument("*/*"),
    ) { uri ->
        val m = pending
        pending = null
        if (uri != null && m != null) {
            scope.launch {
                val error = withContext(Dispatchers.IO) { downloadTo(context, uri, m, client) }
                if (error == null) {
                    onToast("已保存到本机")
                } else {
                    // 失败不留半截文件：删掉刚创建的文档
                    withContext(Dispatchers.IO) { runCatching { context.contentResolver.delete(uri, null, null) } }
                    onToast("保存失败：$error")
                }
            }
        }
        onFinished()
    }
    return { m ->
        pending = m
        val ext = m.url?.substringBefore('?')?.substringAfterLast('.')?.takeIf { it.length in 1..5 }
            ?: when (m.kind) {
                "video" -> "mp4"
                "audio" -> "m4a"
                else -> "jpg"
            }
        launcher.launch("lovelife_${System.currentTimeMillis()}.$ext")
    }
}

/** 下载到所选文档；成功返回 null，失败返回一句人话原因 */
private fun downloadTo(
    context: Context,
    target: Uri,
    m: PostMediaUi,
    client: OkHttpClient,
): String? {
    val url = (m.url ?: m.thumbUrl).orEmpty()
    if (url.isEmpty()) return "这条媒体没有地址"
    val request = Request.Builder().url(url).apply {
        // 绿联通道的中转会话：不带就会 302 到登录页
        Channel.cookieValue().takeIf { it.isNotEmpty() }?.let { header("Cookie", it) }
    }.build()
    return try {
        client.newCall(request).execute().use { res ->
            if (!res.isSuccessful) return "服务端返回 HTTP ${res.code}"
            val type = res.header("Content-Type").orEmpty()
            if (!isMediaContentType(type)) {
                return "响应不是媒体（${type.ifEmpty { "无类型" }}），可能被通道重定向到登录页"
            }
            val out = context.contentResolver.openOutputStream(target)
                ?: return "无法写入所选位置"
            out.use { sink -> res.body.byteStream().use { it.copyTo(sink) } }
            null
        }
    } catch (e: IOException) {
        e.message ?: "网络异常"
    } catch (e: SecurityException) {
        e.message ?: "没有写入权限"
    }
}

/** 只接受图片 / 视频 / 音频：通道登录页返回的是 text/html */
internal fun isMediaContentType(contentType: String): Boolean {
    val t = contentType.substringBefore(';').trim().lowercase()
    return t.startsWith("image/") || t.startsWith("video/") || t.startsWith("audio/")
}
