package com.bookandmusic.lovelife.appnative.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import java.util.concurrent.TimeUnit
import kotlin.coroutines.resume

/**
 * AI 对话仓库：
 * - 常规接口走 ApiClient（带鉴权/换通道重试）
 * - SSE 走专用长时连接（POST /chat/message 的流式 body，不能复用 ApiClient 的 30s callTimeout）
 *
 * SSE 事件集（12）：session / turn.start / thinking.delta / thinking.end / answer.delta /
 * answer.end / tool.start / tool.end / proposal / card / turn.end / error。
 * 真增量：thinking/answer.delta 逐帧累加渲染；tool.end 乱序 → 客户端按 call_id 归位。
 */
object ChatRepository {

    // ---------- DTO ----------

    @Serializable
    data class Bootstrap(
        val available: Boolean = false,
        val reason: String = "",
        val welcome: String = "",
        val chips: List<String> = emptyList(),
    )

    @Serializable
    data class ChatStatus(val available: Boolean = false, val reason: String = "")

    @Serializable
    data class SessionInfo(
        val id: Long,
        val title: String = "",
        val updated_at: String = "",
        val pinned: Boolean = false,
    )

    // history parts（服务端按类型下发；客户端按 kind 渲染）
    @Serializable
    data class Part(
        val type: String, // thinking | tool | card | result_card | text | attachment | quote | interrupted
        val text: String? = null,
        val seconds: Int = 0,
        val label: String? = null,
        // 确认卡快照（仅带 card part 的行下发）
        val card: CardSnapshot? = null,
        // 结果卡（type=result_card）：服务端校验后落库，与 SSE card 事件同形；
        // 正文 ```ui 围栏按 ref 取卡（刷新后与实时形态一致）
        val view: String? = null,
        val ref: String? = null,
        val title: String? = null,
        val note: String? = null,
        /** 收窄指令（模型提案里的 only / limit，服务端校验后原样落库） */
        val only: List<String> = emptyList(),
        val limit: Int = 0,
        val data: ChatUi.ResultData? = null,
        /**
         * quote part 的被引用条目：服务端**一条 ref 一个 part**，故这里是单个对象而不是数组。
         * 形状对不上会让整条历史反序列化失败（`refs` 是对象、模型声明成 List → 抛异常 →
         * history 整页退化成空会话），改动此字段必须同步服务端的 part 写入形状。
         */
        val refs: QuoteRef? = null,
    )

    @Serializable
    data class CardSnapshot(
        val action_id: Long = 0,
        val intent: String = "",
        val summary: String = "",
        val preview: Preview? = null,
        val warnings: List<String> = emptyList(),
        val status: String = "", // pending | executed | cancelled | expired | failed
        val result: String? = null,
        /** 距过期剩余秒数（服务端下发；pending 才有意义，历史回显里可能已是 0） */
        val expires_in: Int = 0,
    )

    @Serializable
    data class Preview(
        val kind: String = "",
        // 目标形态数据（结构按 kind 不同，ChatUi.parsePreview 按需解）
        val body: kotlinx.serialization.json.JsonElement? = null,
    )

    /** 提案裁决回执 */
    @Serializable
    data class ResolveResult(
        val status: String = "", // executed | cancelled | failed
        val result: String? = null,
        val error: String? = null,
    )

    @Serializable
    data class HistoryMessage(
        val id: Long,
        val role: String, // user | assistant
        val parts: List<Part> = emptyList(),
        val created_at: String = "",
        /** 所属轮次：与 assistant 同轮，可据它「重新生成」整轮 */
        val turn_id: String = "",
    )

    // SSE 事件（统一形状；具体字段按 event 填）
    data class SseEvent(
        val event: String,
        val data: String,
    )

    // ---------- 常规接口 ----------

    suspend fun bootstrap(): Bootstrap = ApiClient.api("/api/v1/chat/bootstrap")

    suspend fun status(): ChatStatus = ApiClient.api("/api/v1/chat/status")

    suspend fun sessions(): List<SessionInfo> =
        (ApiClient.api<SessionsPage>("/api/v1/chat/sessions")).items

    @Serializable
    private data class SessionsPage(val items: List<SessionInfo> = emptyList())

    suspend fun history(sessionId: Long): List<HistoryMessage> =
        ApiClient.api<HistoryPage>("/api/v1/chat/history?session_id=$sessionId").items

    @Serializable
    private data class HistoryPage(val items: List<HistoryMessage> = emptyList())

    suspend fun resolveProposal(
        actionId: Long,
        decision: String,
        mediaTokens: List<String> = emptyList(),
    ): ResolveResult {
        val body = ApiClient.json.encodeToString(
            ResolveRequest.serializer(), ResolveRequest(decision, mediaTokens))
        return ApiClient.api<ResolveResult>("/api/v1/chat/proposals/$actionId/resolve", "POST", body)
    }

    @Serializable
    private data class ResolveRequest(val decision: String, val media_tokens: List<String> = emptyList())

    suspend fun abortTurn(turnId: String) {
        ApiClient.api<Unit>("/api/v1/chat/turns/$turnId/abort", "POST", "{}")
    }
    /**
     * 删掉整轮（用户消息 + 回复 + 该轮模型上下文）：服务端与 abort 同一套回滚，
     * 区别是不登记中止意图——用于「重新生成」（轮次已结束，不需要拦断连落库）。
     */
    suspend fun deleteTurn(turnId: String) {
        ApiClient.api<Unit>("/api/v1/chat/turns/$turnId", "DELETE", "{}")
    }

    /** 置顶切换（服务端 POST /chat/sessions/{id}/pin） */
    suspend fun pinSession(id: Long) {
        ApiClient.api<Unit>("/api/v1/chat/sessions/$id/pin", "POST", "{}")
    }

    suspend fun deleteSession(sessionId: Long) {
        ApiClient.api<Unit>("/api/v1/chat/sessions/$sessionId", "DELETE")
    }

    // ---------- SSE ----------

    /**
     * 发消息并消费 SSE 流。onEvent 在 IO 线程回调（事件到达即回调，不攒批）。
     * 返回值 = 流正常读尽（true）/ 调用方取消（false，取消时不抛异常）。
     * 断连（IOException）由调用方捕获处理（断连不删消息、标「已中断」）。
     */
    suspend fun sendMessageStream(
        sessionId: Long,
        text: String,
        quoted: List<QuoteRef> = emptyList(),
        media: List<AttachmentMeta> = emptyList(),
        onEvent: (SseEvent) -> Unit,
    ): Boolean {
        val body = ApiClient.json.encodeToString(
            MessageRequest.serializer(),
            MessageRequest(
                session_id = if (sessionId > 0) sessionId else null,
                content = text,
                quoted = quoted,
                media = media,
                client_tools = CLIENT_TOOLS,
            ),
        )
        val client = OkHttpClient.Builder()
            .callTimeout(0, TimeUnit.MILLISECONDS) // 流式不设总超时：整轮墙钟由服务端预算控制
            .readTimeout(120, TimeUnit.SECONDS)   // 帧间隔兜底（服务端 90s 墙钟 + 余量）
            // 不跟随跳转：绿联中转在凭据失效时 302 到它自己的登录页，
            // 跟随后会拿到一张 HTML（200）当成 SSE 解析，结果「什么事件都没有」。
            // 让它显式暴露成 302，才能判断是「凭据过期」而不是「网络正常但没内容」。
            .followRedirects(false)
            .followSslRedirects(false)
            .build()

        // 通道选择只发生在这里——**发消息之前**。
        // 一旦开始收帧就固定通道（对话中不切，避免同一轮发两遍）。
        Channel.ensureResolved()
        Channel.ensureFresh()

        val request = Request.Builder()
            .url(Channel.base().trimEnd('/') + "/api/v1/chat/message")
            .post(body.toRequestBody("application/json; charset=utf-8".toMediaType()))
            .apply {
                SecureStore.token()?.let { header("Authorization", "Bearer $it") }
                Channel.cookieValue().takeIf { it.isNotEmpty() }?.let { header("Cookie", it) }
            }
            .build()

        // 阻塞网络必须切 IO：调用方是 Compose 的主线程 scope，
        // 主线程上 execute() 会抛 NetworkOnMainThreadException——
        // 表现为「发出去瞬间失败：网络异常」，而其它接口（走 ApiClient，内部已切 IO）全部正常。
        // 主线程执行是这条路径最容易踩的坑：其它接口走 ApiClient（内部已切 IO），只有流式是自建请求。
        // 异常不在此层加工：CancellationException 让取消语义传播，其余透传给 UI 判「已中断/失败」。
        return kotlinx.coroutines.withContext(kotlinx.coroutines.Dispatchers.IO) {
            client.newCall(request).execute().use { res ->
                streamOf(res, onEvent)
            }
        }
    }

    /** 处理一次流式响应：状态码检查 + 逐帧回调。失败抛 ApiError / IOException。 */
    private suspend fun streamOf(
        res: okhttp3.Response,
        onEvent: (SseEvent) -> Unit,
    ): Boolean {
        if (!res.isSuccessful) {
            // 流式客户端关了跟随跳转：绿联凭据失效时 302 会原样落到这里
            // （跟随则变成拿登录页 HTML 当 SSE 解析，一个事件都没有）。
            // 把"被网关重定向"说成人话，而不是「HTTP 302」。
            val code = res.code
            val msg = if (code == 302 || code == 307) {
                "服务地址凭据失效，请重新登录后重试"
            } else if (code == 401) {
                // 401 是 app 令牌过期，不是通道凭据问题；聊天路径不走 ApiClient 的单飞刷新，
                // 直接提示重登，避免用户误以为是后台配置问题
                "登录已过期，请重新登录"
            } else {
                runCatching {
                    Json.decodeFromString(ErrorDto.serializer(), res.body?.string() ?: "")
                }.getOrNull()?.error ?: "服务端返回错误（$code）"
            }
            throw ApiError(code, msg)
        }
        val source = res.body?.source() ?: return false
        return suspendCancellableCoroutine { cont ->
            val thread = Thread {
                try {
                    parseSse(source) { ev -> onEvent(ev) }
                    if (cont.isActive) cont.resume(true)
                } catch (e: Exception) {
                    if (cont.isActive) cont.resumeWith(Result.failure(e))
                }
            }
            thread.isDaemon = true
            thread.start()
            cont.invokeOnCancellation { thread.interrupt() }
        }
    }

    /**
     * 引用一条记录（服务端 quoteRef）：勾选的是「某条具体记录」，因此只填 kind + id。
     * 服务端只收对象：发裸 id 数组会 400，勾选追问整条失败。
     */
    @Serializable
    data class QuoteRef(
        val result_id: String = "",
        val group: String = "",
        val ids: List<Long> = emptyList(),
        val kind: String = "",
        val id: Long = 0,
        /** 展示用摘要（记录文字截断）：服务端原样存进 quote part，历史回显靠它显示引用了什么 */
        val label: String = "",
    )

    @Serializable
    private data class MessageRequest(
        val session_id: Long? = null,
        val content: String,
        val quoted: List<QuoteRef> = emptyList(),
        // 字段名必须是 media：服务端只认 media，
        // 写成 attachments 会被 JSON 解码静默忽略——附件元数据不进 parts、模型也看不到
        val media: List<AttachmentMeta> = emptyList(),
        // 客户端展示工具声明：服务端拼进提示词；缺省不发
        val client_tools: List<ClientToolDef> = emptyList(),
    )

    /** 卡片定义（name 限 [a-z_]{1,24}；desc/params ≤200 字；每次 ≤8 个——服务端校验兜底） */
    @Serializable
    data class ClientToolDef(val name: String, val desc: String, val params: String)

    /** 本客户端支持的卡片形态（新增一种卡 = 改这里 + 渲染分支，服务端零改动） */
    val CLIENT_TOOLS = listOf(
        ClientToolDef("list", "记录清单：一行一条（缩略图 + 文字 + 日期/角标）。用户问「有哪些」时用", "{ref,title?,note?,limit?}"),
        ClientToolDef("grid", "图片网格：3 列方图，点开看大图。用户要「看照片」时用", "{ref,title?,note?,limit?}"),
        ClientToolDef("detail", "单条详情：一条内容的全文 + 媒体 + 元信息。用户要「看看这条」时用", "{ref,title?,note?}"),
        ClientToolDef("stats", "数据卡：数字 + 分布条形。用户问「多少 / 分布」时用；只要几项就写 only", "{ref,title?,note?,only?}"),
        ClientToolDef("kpi", "单值卡：一个大数字 + 一句说明。只问一个数字时用，别端整份概览", "{ref,title?,note?,only?}"),
        ClientToolDef("timeline", "时间线：按日期倒序、按月分组的竖向时间轴。问「什么时候 / 这一年都做了什么」时用，比列表更看得出先后", "{ref,title?,note?,limit?}"),
        ClientToolDef("map", "地图卡：把带经纬度的地点画成地图标记，点标记看地点与日期。只有地点数据能用", "{ref,title?,note?}"),
    )

    /** 附件元数据：type 必须是 image/video/audio 前缀，缺失会被 400 拒 */
    @Serializable
    data class AttachmentMeta(val name: String, val type: String, val size: Long = 0)

    /**
     * SSE 分帧解析（text/event-stream）：空行分界；`event:` 与 `data:` 行；
     * data 可多行拼接（JSON 不允许换行，但按规范拼接 \n）。注释行（:开头）忽略。
     */
    internal fun parseSse(
        source: okio.BufferedSource,
        onEvent: (SseEvent) -> Unit,
    ) {
        val sb = StringBuilder()
        var event = ""
        while (!source.exhausted()) {
            val line = source.readUtf8Line() ?: break
            when {
                line.isEmpty() -> {
                    if (sb.isNotEmpty()) {
                        onEvent(SseEvent(event.ifEmpty { "message" }, sb.toString()))
                    }
                    sb.clear(); event = ""
                }
                line.startsWith(":") -> { /* 注释/心跳 */ }
                line.startsWith("event:") -> event = line.removePrefix("event:").trim()
                line.startsWith("data:") -> {
                    if (sb.isNotEmpty()) sb.append('\n')
                    sb.append(line.removePrefix("data:").trim())
                }
            }
        }
        // 流末尾未以空行收尾的事件（服务端正常应有）
        if (sb.isNotEmpty()) onEvent(SseEvent(event.ifEmpty { "message" }, sb.toString()))
    }
}
