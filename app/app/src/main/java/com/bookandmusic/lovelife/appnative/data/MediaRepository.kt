package com.bookandmusic.lovelife.appnative.data

import android.content.Context
import android.net.Uri
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.longOrNull
import kotlinx.serialization.json.long
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.Serializable
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.MultipartBody
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.RequestBody.Companion.asRequestBody
import java.io.File
import java.io.FileOutputStream
import java.io.IOException
import java.util.concurrent.TimeUnit

/** 上传结果（服务端 media.Uploaded 契约） */
@Serializable
data class UploadedMedia(
    val media_token: String = "",
    val kind: String = "",
    val width: Int = 0,
    val height: Int = 0,
    val size: Long = 0,
    val duration: Double = 0.0,
    val taken_at: String = "",
    /** 本机文件 mime：服务端回执里没有，upload() 就地补上——聊天消息的 media.type 要用它 */
    val mime: String = "",
)

/**
 * 选择时读到的本机元数据（**不联网**）：聊天消息只需要 name / type / size，
 * 文件本体一直躺在设备上，直到用户点「确认执行」那一刻才真的上传。
 */
data class LocalMedia(
    val name: String,
    val mime: String,
    val size: Long,
    /** photo / video / audio（服务端按扩展名判定的同一套口径，客户端先按 mime 猜） */
    val kind: String,
)

/**
 * 媒体上传：选中的本机文件先拷进缓存目录，再换 media_token。
 *
 * 小文件（≤ 一片）走整文件上传；超过一片走分片（服务端 `X-Upload-Offset/Id/Final` 三段协议），
 * 好处是大文件不必把整个文件塞进一个请求体、单片刻失败只重发那一片。**没有做跨进程续传**
 * （App 被杀后重新选文件从头传）——临时区有 24h janitor 兜底，不值得为此落本地状态。
 */
object MediaRepository {

    /** 单文件上限：与服务端 MEDIA_MAX_MB 默认值（500MB）同口径 */
    private const val MAX_BYTES = 500L * 1024 * 1024

    /** 分片大小：与 server/internal/limits/media.json 的 uploadChunkBytes 同口径
     *  （服务端只校验 offset 连续，不校验片长，改这里不会把服务端弄坏） */
    internal const val CHUNK_BYTES = 8L * 1024 * 1024

    /** 分片循环兜底上限（正常路径远达不到） */
    private const val MAX_CHUNK_ATTEMPTS = 10_000

    /** 单片瞬断重试次数：网络错误最多再试 3 次（退避 1s / 2s / 4s）；HTTP 错误不重试。 */
    private const val CHUNK_RETRY_TIMES = 3

    /** 分片发送结果：中间片回执 / 收尾（拿到 token）/ 位置冲突（带服务端已收字节） */
    internal sealed interface ChunkResult {
        data class Ack(val uploadId: String, val received: Long) : ChunkResult
        data class Done(val media: UploadedMedia) : ChunkResult
        data class Conflict(val received: Long) : ChunkResult
    }

    /**
     * 上传专用 client：整个进程复用一个实例（每次请求新建会把连接池与线程池一起丢掉，重复握手）。
     * 超时口径沿用既有网络路径：连接 15s（同「保存到本机」的 MediaSave）、读 30s（同 ApiClient 单请求上限）、
     * 写 60s（单片上限 8MB，慢网留余量）——分片失败只重发那一片，不设总时长上限。
     */
    private val http: OkHttpClient by lazy {
        OkHttpClient.Builder()
            .connectTimeout(15, TimeUnit.SECONDS)
            .readTimeout(30, TimeUnit.SECONDS)
            .writeTimeout(60, TimeUnit.SECONDS)
            .build()
    }

    /**
     * 分片推进（纯编排，网络由 [send] 注入 → 可单测）：
     * 按 offset 逐片发，末片带 final；遇到 409 用服务端回的 `received` 重同步再续传。
     * **服务端回的 received 是唯一推进依据**（不是本地累加），所以「响应丢了但字节已落盘」能自动纠正。
     */
    internal suspend fun uploadChunks(
        size: Long,
        chunkBytes: Long,
        read: (offset: Long, length: Int) -> ByteArray,
        send: suspend (offset: Long, bytes: ByteArray, isFinal: Boolean, uploadId: String?) -> ChunkResult,
    ): UploadedMedia {
        require(size > 0) { "文件是空的，请重新选择" }
        require(chunkBytes > 0) { "分片大小非法" }
        var offset = 0L
        var uploadId: String? = null
        // 每次循环要么推进 offset 要么抛错，理论上最多 ceil(size/chunk) 次 + 少量 409 重同步；
        // 上限只是兜底（服务端行为异常时不至于死循环）
        var attempts = 0
        while (true) {
            check(attempts++ < MAX_CHUNK_ATTEMPTS) { "上传重试次数过多" }
            val len = (size - offset).coerceIn(0, chunkBytes).toInt()
            val bytes = if (len > 0) read(offset, len) else ByteArray(0)
            val isFinal = offset + len >= size
            when (val r = sendChunkWithRetry { send(offset, bytes, isFinal, uploadId) }) {
                is ChunkResult.Done -> return r.media
                is ChunkResult.Conflict -> {
                    // 服务端已收字节可能比本地以为的多（重发/超时），以它为准；不动就报错防死循环
                    val next = r.received.coerceIn(0, size)
                    if (next == offset && !isFinal) throw IllegalStateException("上传位置没有推进")
                    offset = next
                }
                is ChunkResult.Ack -> {
                    val next = r.received.coerceIn(0, size)
                    if (next == offset && !isFinal) throw IllegalStateException("上传位置没有推进")
                    uploadId = r.uploadId
                    offset = next
                }
            }
        }
    }

    /**
     * 单片发送的瞬断重试：只有网络层 IOException 重试，同一片按原 offset 重发，
     * 推进位置仍以服务端回的 received 为准（409 会纠正「字节已落盘但响应丢了」）。
     * ApiError（4xx/5xx 的 JSON 错误）不是 IOException，直接抛给上层，不重试。
     */
    private suspend fun sendChunkWithRetry(send: suspend () -> ChunkResult): ChunkResult {
        var attempt = 0
        while (true) {
            try {
                return send()
            } catch (e: IOException) {
                if (attempt >= CHUNK_RETRY_TIMES) throw e
                delay(1_000L shl attempt)
                attempt++
            }
        }
    }

    suspend fun upload(context: Context, uri: Uri): UploadedMedia = withContext(Dispatchers.IO) {
        val resolver = context.contentResolver
        val name = queryName(context, uri) ?: "upload-${System.currentTimeMillis()}"
        val mime = resolver.getType(uri) ?: guessMime(name)
        val temp = File(context.cacheDir, "upload-${System.currentTimeMillis()}-$name")
        resolver.openInputStream(uri)?.use { input ->
            FileOutputStream(temp).use { output -> input.copyTo(output) }
        } ?: throw IllegalStateException("无法读取所选文件")
        try {
            val size = temp.length()
            if (size > MAX_BYTES) throw IllegalStateException("文件超过 500MB")
            if (size == 0L) throw IllegalStateException("文件是空的，请重新选择")
            val url = Channel.base().trimEnd('/') + "/api/v1/media/upload"
            if (size <= CHUNK_BYTES) {
                // 小文件：整文件一次传（不带分片头 = 服务端的整文件上传分支）
                return@withContext postWholeFile(url, name, mime, temp).copy(mime = mime)
            }
            val raf = java.io.RandomAccessFile(temp, "r")
            try {
                return@withContext uploadChunks(
                    size = size,
                    chunkBytes = CHUNK_BYTES,
                    read = { offset, len ->
                        val buf = ByteArray(len)
                        raf.seek(offset)
                        raf.readFully(buf)
                        buf
                    },
                    send = { offset, bytes, isFinal, uploadId ->
                        sendChunk(url, name, mime, offset, bytes, isFinal, uploadId)
                    },
                ).copy(mime = mime)
            } finally {
                raf.close()
            }
        } finally {
            temp.delete()
        }
    }

    /** 整文件上传（≤ 一片的小文件路径） */
    private fun postWholeFile(
        url: String,
        name: String,
        mime: String,
        file: File,
    ): UploadedMedia {
        val form = MultipartBody.Builder()
            .setType(MultipartBody.FORM)
            .addFormDataPart("file", name, file.asRequestBody(mime.toMediaType()))
            .build()
        val request = Request.Builder()
            .url(url)
            .post(form)
            .apply { authHeaders() }
            .build()
        http.newCall(request).execute().use { res ->
            val text = res.body?.string().orEmpty()
            if (!res.isSuccessful) throw uploadError(res.code, text)
            return ApiClient.json.decodeFromString(UploadedMedia.serializer(), text)
        }
    }

    /** 发一片：按服务端三段协议带 offset / id / final；409 回 [ChunkResult.Conflict] 交给上层重同步 */
    private suspend fun sendChunk(
        url: String,
        name: String,
        mime: String,
        offset: Long,
        bytes: ByteArray,
        isFinal: Boolean,
        uploadId: String?,
    ): ChunkResult = withContext(Dispatchers.IO) {
        val form = MultipartBody.Builder()
            .setType(MultipartBody.FORM)
            .addFormDataPart("file", name, bytes.toRequestBody(mime.toMediaType()))
            .build()
        val request = Request.Builder()
            .url(url)
            .post(form)
            .header("X-Upload-Offset", offset.toString())
            .apply {
                if (isFinal) header("X-Upload-Final", "true")
                uploadId?.let { header("X-Upload-Id", it) }
                authHeaders()
            }
            .build()
        http.newCall(request).execute().use { res ->
            val text = res.body?.string().orEmpty()
            if (res.code == 409) {
                val received = runCatching {
                    ApiClient.json.parseToJsonElement(text).jsonObject["received"]!!.jsonPrimitive.long
                }.getOrNull()
                // 拿不到已收字节就没法重同步，只能如实报错
                received?.let { return@use ChunkResult.Conflict(it) }
            }
            if (!res.isSuccessful) throw uploadError(res.code, text)
            val obj = ApiClient.json.parseToJsonElement(text).jsonObject
            val token = obj["media_token"]?.jsonPrimitive?.contentOrNull
            if (!token.isNullOrEmpty()) {
                ChunkResult.Done(ApiClient.json.decodeFromString(UploadedMedia.serializer(), text))
            } else {
                // 回执形状不对（服务端升级/代理改写）时给可读错误，别让 !! 变成 NPE
                val id = obj["upload_id"]?.jsonPrimitive?.contentOrNull
                val received = obj["received"]?.jsonPrimitive?.longOrNull
                if (id.isNullOrEmpty() || received == null) throw ApiError(res.code, "上传回执格式异常")
                ChunkResult.Ack(uploadId = id, received = received)
            }
        }
    }

    private fun okhttp3.Request.Builder.authHeaders(): okhttp3.Request.Builder = apply {
        SecureStore.token()?.let { header("Authorization", "Bearer $it") }
        Channel.cookieValue().takeIf { it.isNotEmpty() }?.let { header("Cookie", it) }
    }

    private fun uploadError(code: Int, text: String): ApiError {
        val msg = runCatching {
            ApiClient.json.decodeFromString(ErrorDto.serializer(), text).error
        }.getOrNull() ?: "上传失败（HTTP $code）"
        return ApiError(code, msg)
    }

    /** 单次撤销上限：服务端超过 100 个直接 400 */
    internal const val MAX_DISCARD_TOKENS = 100

    /**
     * 放弃已上传的临时媒体（上传半途失败 / 裁决没落库后的清理）：
     * 服务端收 `{tokens:[…]}`，幂等、归属不符跳过；失败不阻断调用方。
     */
    suspend fun discard(tokens: List<String>) {
        val batch = tokens.take(MAX_DISCARD_TOKENS)
        if (batch.isEmpty()) return
        runCatching { ApiClient.api<DiscardResult>("/api/v1/media/discard", "POST", discardBody(batch)) }
    }

    /** 撤销请求体（服务端契约 `{tokens:[…]}`）；单测按字面形状断言 */
    internal fun discardBody(tokens: List<String>): String =
        ApiClient.json.encodeToString(
            DiscardRequest.serializer(),
            DiscardRequest(tokens.take(MAX_DISCARD_TOKENS)),
        )

    @Serializable
    internal data class DiscardRequest(val tokens: List<String>)

    /** 撤销回执：服务端回 `{discarded:n}` */
    @Serializable
    internal data class DiscardResult(val discarded: Int = 0)

    /**
     * 只读本机元数据（选择媒体时用，**不联网**）：
     * 「选中即上传」会为没发出去的文件占服务端磁盘（取消提案后成孤儿），
     * 所以上传推迟到确认执行那一刻。
     */
    fun localMeta(context: Context, uri: Uri): LocalMedia {
        val resolver = context.contentResolver
        val name = queryName(context, uri) ?: "upload-${System.currentTimeMillis()}"
        val mime = resolver.getType(uri) ?: guessMime(name)
        val size = runCatching {
            resolver.openAssetFileDescriptor(uri, "r")?.use { it.length }
        }.getOrNull() ?: 0L
        val kind = when {
            mime.startsWith("image/") -> "photo"
            mime.startsWith("video/") -> "video"
            mime.startsWith("audio/") -> "audio"
            else -> "photo" // 选择器限定在图片/视频/音频，兜底按图片走
        }
        return LocalMedia(name = name, mime = mime, size = size.coerceAtLeast(0), kind = kind)
    }

    private fun queryName(context: Context, uri: Uri): String? =
        context.contentResolver.query(uri, null, null, null, null)?.use { cursor ->
            val idx = cursor.getColumnIndex(android.provider.OpenableColumns.DISPLAY_NAME)
            if (idx >= 0 && cursor.moveToFirst()) cursor.getString(idx) else null
        }

    private fun guessMime(name: String): String = when (name.substringAfterLast('.', "").lowercase()) {
        "jpg", "jpeg" -> "image/jpeg"
        "png" -> "image/png"
        "webp" -> "image/webp"
        "gif" -> "image/gif"
        "heic" -> "image/heic"
        "mp4" -> "video/mp4"
        "mov" -> "video/quicktime"
        "m4a" -> "audio/mp4"
        "mp3" -> "audio/mpeg"
        "wav" -> "audio/wav"
        "aac" -> "audio/aac"
        else -> "application/octet-stream"
    }
}
