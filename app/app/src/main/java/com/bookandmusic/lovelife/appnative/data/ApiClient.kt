package com.bookandmusic.lovelife.appnative.data

import com.bookandmusic.lovelife.appnative.BuildConfig

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.withContext
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.json.Json
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import java.time.Duration

/** 统一 API 错误：message 为服务端文案（登录/刷新自身的失败透传服务端原文） */
class ApiError(val status: Int, message: String) : Exception(message)

/**
 * HTTP 客户端契约：
 * Bearer 令牌 → 401 单飞刷新 + 单次重放 → 失败清会话；
 * 网络失败 / 非 401 失败 → 通道层重取一次后重试（仅非 auth 路径）；
 * 登录 / 刷新自身的失败不重试，透传服务端文案；204 返回 null。
 */
object ApiClient {

    /**
     * `coerceInputValues`：服务端对「没有值的可选字段」会下发 `null`（如 `card.data.records:null`），
     * 而 DTO 里这些字段是非空类型 + 默认值。不开这个开关时解码直接抛异常，解析处的
     * runCatching 会把它吞成 null——整块统计卡/记录卡就此消失，界面上什么都看不出来。
     */
    @PublishedApi
    internal val json = Json { ignoreUnknownKeys = true; coerceInputValues = true }

    private val http = OkHttpClient.Builder().callTimeout(Duration.ofSeconds(30)).build()

    fun builtinBase(): String = BuildConfig.DEFAULT_BASE

    @Volatile private var authEpoch = 0
    private val refreshMutex = Mutex()

    /** 会话失效（401 且刷新失败）：壳层订阅后切回登录页；登录成功调用 [markSessionRestored] 复位 */
    private val _sessionExpired = MutableStateFlow(false)
    val sessionExpired: StateFlow<Boolean> = _sessionExpired.asStateFlow()

    @PublishedApi
    internal fun markSessionExpired() {
        _sessionExpired.value = true
    }

    fun markSessionRestored() {
        _sessionExpired.value = false
    }

    fun clearAuth() {
        authEpoch++
        SecureStore.clearTokens()
    }

    suspend inline fun <reified T> api(
        path: String,
        method: String = "GET",
        body: String? = null,
        retry: Boolean = true,
    ): T {
        val text = apiRaw(path, method, body, retry)
        @Suppress("UNCHECKED_CAST")
        return if (text == null) emptyBodyResult(T::class) as T else json.decodeFromString(text)
    }

    /**
     * 空响应（204 或空体）只有 `Unit` 调用合法：其余类型必须在解码前给可读文案，
     * 否则 `Unit as T` 会在调用方抛 ClassCastException，看不出是哪一步坏了。
     */
    @PublishedApi
    internal fun emptyBodyResult(type: kotlin.reflect.KClass<*>): Any =
        if (type == Unit::class) {
            Unit
        } else {
            throw ApiError(0, "服务端没有返回内容（期望 ${type.simpleName ?: "对象"}），请重试")
        }

    @PublishedApi
    internal suspend fun apiRaw(
        path: String,
        method: String,
        body: String?,
        retry: Boolean,
        authEntry: Boolean = path.startsWith("/api/v1/auth/login") || path.startsWith("/api/v1/auth/refresh"),
    ): String? = withContext(Dispatchers.IO) {
        var attempt = retry
        while (true) {
            Channel.ensureFresh()
            val request = buildRequest(path, method, body)
            val res = try {
                http.newCall(request).execute()
            } catch (e: Exception) {
                // 通道层失败：标记当前通道（置空）→ 重选一次。短请求可以立刻换，
                // 对话中的长连接固定不切通道（见 ChatRepository）。
                if (attempt && !authEntry) {
                    Channel.markFailed()
                    Channel.ensureResolved()
                    attempt = false
                    continue
                }
                throw e
            }
            try {
                if (res.code == 401 && attempt && !authEntry) {
                    val seen = SecureStore.token() ?: ""
                    if (refreshOnce(seen)) {
                        attempt = false
                        continue
                    }
                    clearAuth()
                    markSessionExpired()
                    throw ApiError(401, "登录已过期，请重新登录")
                }
                // 非 401 的失败（5xx / 网关错误页）也换一次通道：可能是这条通道的网关问题
                if (!res.isSuccessful && attempt && !authEntry) {
                    Channel.markFailed()
                    Channel.ensureResolved()
                    attempt = false
                    continue
                }
                if (!res.isSuccessful) {
                    val text = res.body?.string() ?: ""
                    val message = runCatching {
                        json.decodeFromString(ErrorDto.serializer(), text).error
                    }.getOrNull() ?: "HTTP ${res.code}"
                    throw ApiError(res.code, message)
                }
                if (res.code == 204) return@withContext null
                return@withContext res.body?.string()?.takeIf { it.isNotEmpty() }
            } finally {
                res.close()
            }
        }
        error("unreachable")
    }

    /** 单飞刷新：并发 401 共享同一次（rotation 下多发必有一发失败）；登出后结果作废 */
    @PublishedApi
    internal suspend fun refreshOnce(seenToken: String): Boolean = refreshMutex.withLock {
        if (SecureStore.token() != seenToken) return@withLock true // 已被并发刷新
        val rt = SecureStore.refreshToken() ?: return@withLock false
        val epoch = authEpoch
        val text = try {
            http.newCall(
                Request.Builder()
                    .url(Channel.base().trimEnd('/') + "/api/v1/auth/refresh")
                    .post(json.encodeToString(RefreshRequest.serializer(), RefreshRequest(rt)).toRequestBody("application/json; charset=utf-8".toMediaType()))
                    .apply { applyAuth(this) } // 与业务请求同头：绿联通道下缺 Cookie 会被中转 302 → 刷新失败 → 用户被登出
                    .build()
            ).execute().use { res ->
                if (!res.isSuccessful) return@withLock false
                res.body?.string() ?: return@withLock false
            }
        } catch (_: Exception) {
            return@withLock false
        }
        if (epoch != authEpoch) return@withLock false // 期间已登出
        val parsed = runCatching { json.decodeFromString(RefreshResponse.serializer(), text) }.getOrNull()
            ?: return@withLock false
        SecureStore.setTokens(parsed.token, parsed.refresh_token)
        true
    }

    private fun buildRequest(path: String, method: String, body: String?): Request {
        val base = Channel.base().trimEnd('/')
        val builder = Request.Builder().url(base + path)
        val media = "application/json; charset=utf-8".toMediaType()
        val part = body?.toRequestBody(media)
        // 需要请求体的方法（POST/PUT/PATCH）无 body 时补空体：/me 这类「只改一个字段」的调用
        // 若传 null 会直接抛 IllegalArgumentException
        val payload = part ?: "".toRequestBody(media)
        when (method) {
            "GET" -> {}
            "POST" -> builder.post(payload)
            "PUT" -> builder.put(payload)
            "DELETE" -> builder.delete(part)
            else -> builder.method(method, payload)
        }
        applyAuth(builder)
        return builder.build()
    }

    /**
     * 复用同一套鉴权与通道语义给非 JSON 的长任务（应用更新下载器）：
     * 令牌 + 绿联 cookie 必须与业务请求一致，否则下载会在绿联通道下 401。
     */
    internal fun applyAuth(builder: Request.Builder) {
        SecureStore.token()?.let { builder.header("Authorization", "Bearer $it") }
        Channel.cookieValue().takeIf { it.isNotEmpty() }?.let { builder.header("Cookie", it) }
    }
}

/** 统一错误文案：ApiError 透出服务端文案，其余用调用方兜底 */
fun errText(e: Throwable, fallback: String): String =
    (e as? ApiError)?.message?.takeIf { it.isNotEmpty() } ?: fallback
