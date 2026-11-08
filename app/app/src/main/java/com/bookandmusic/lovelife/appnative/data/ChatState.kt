package com.bookandmusic.lovelife.appnative.data

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.intOrNull
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.longOrNull

/**
 * 一轮对话的分段渲染状态机：
 * - Bubble 分段：think（无壳竖线行）/ text（正文，结果卡由正文围栏就地插入）/ tool（灰底过程容器）/ card（确认卡）
 * - 流式侧与历史回显同一形态（parts 顺序拆段）
 * - tool 按 call_id 归位（tool.end 乱序容错）
 * - 思考秒数 = 各次 thinking.end 的 seconds 求和（两侧同算法）
 * - 结果卡只认服务端 `validated` 的 view（SSE card 事件 / 历史 card_event），客户端不推断形态
 *
 * **字段全部是 Compose 快照状态**（`mutableStateOf` / `mutableStateListOf`）：流式期间
 * 每个 delta 都要让读取它的重组作用域失效；普通集合写入对 Compose 不可见，只能等收尾
 * 一次刷新，会表现为「等一段时间后整段一次性出现」。**必须保持快照状态类型。**
 */
object ChatState {

    /** 渲染段（一轮内的有序单元） */
    sealed interface Seg {
        /** 思考块：流式展开/折叠；seconds 为该块累计 */
        class Think(text: String = "", seconds: Int = 0, done: Boolean = false) : Seg {
            var text by mutableStateOf(text)
            var seconds by mutableIntStateOf(seconds)
            var done by mutableStateOf(done) // 回答开始 → 折叠
        }

        /** 正文段（AI 白泡） */
        class Text(text: String = "", done: Boolean = false) : Seg {
            var text by mutableStateOf(text)
            var done by mutableStateOf(done)
        }

        /** 工具块（灰底容器）：开始 label → 运行中 → 结束 status/text */
        class Tool(
            val callId: String,
            label: String = "",
            running: Boolean = true,
            status: String = "", // ok | error | pending
            resultText: String = "",
            /** 工具内嵌的确认卡（proposal 或历史 card part） */
            card: ChatRepository.CardSnapshot? = null,
            /**
             * 待确认卡的到期时刻（epoch ms，0 = 无卡/已裁决）。
             * 服务端只给「剩余秒数」，落成本地绝对时刻才能自己倒计时——否则刷新或回显后
             * 就只剩一个不会走的数字，卡片永远显示「待确认」。
             */
            cardExpireAt: Long = 0,
        ) : Seg {
            var label by mutableStateOf(label)
            var running by mutableStateOf(running)
            var status by mutableStateOf(status)
            var resultText by mutableStateOf(resultText)
            var card by mutableStateOf(card)
            var cardExpireAt by mutableLongStateOf(cardExpireAt)
        }
    }

    /** 一条消息（用户/助手）的渲染形态 */
    class RenderMessage(
        val id: Long,
        val role: String, // user | assistant
        val segs: MutableList<Seg> = mutableStateListOf(),
    ) {
        /**
         * 所属轮次（服务端 turn_id）：仅用于「重新生成」定位整轮，不参与渲染。
         * SSE 回调在裸线程写、主线程读，必须是快照状态才有安全发布。
         */
        var turnId: String by mutableStateOf("")

        /** 这轮用户输入带附件或引用：重新生成要重放它们，做不到就不给按钮（静默丢附件比不给更糟） */
        var hasRichInput: Boolean = false

        /** 用户消息气泡里回显的引用条（记录摘要文字）：发出后与刷新后都要看得见引用了什么 */
        var quotes: List<String> = emptyList()

        /** 用户消息带几个媒体附件：气泡里显示「附 N 个媒体」，否则刷新后附件就凭空消失了 */
        var mediaCount: Int = 0

        /**
         * 本轮已验证的渲染卡（ref → CardEvent）：SSE `card` 事件与历史 part 的 card_event 两路都灌进来。
         * 正文 ```ui 围栏按 ref 就地取卡——**客户端不按数据推形态**，只认这里下发的 view。
         */
        val cards = mutableStateMapOf<String, com.bookandmusic.lovelife.appnative.data.ChatUi.CardEvent>()
        var interrupted: Boolean by mutableStateOf(false)
    }

    /** SSE 事件 → 状态归约。返回值仅作信号（错误事件抛给上层）。 */
    fun applyEvent(turn: RenderMessage, ev: ChatRepository.SseEvent): Boolean {
        when (ev.event) {
            "thinking.delta" -> {
                // 轮界 = 工具往返：tool/text 块之后的思考必须新开一块（过程呈现：
                // 思考块独立成行；工具循环每轮各一条 thinking part——复用同一块会把两轮思考粘在一起）
                val seg = if (turn.segs.lastOrNull() is Seg.Think) {
                    turn.segs.last() as Seg.Think
                } else {
                    Seg.Think().also { turn.segs.add(it) }
                }
                seg.text += parseJsonString(ev.data, "text")
            }
            "thinking.end" -> {
                val seg = if (turn.segs.lastOrNull() is Seg.Think) {
                    turn.segs.last() as Seg.Think
                } else {
                    Seg.Think().also { turn.segs.add(it) }
                }
                seg.seconds += (parseJsonInt(ev.data, "seconds"))
                seg.done = true
            }
            "answer.delta" -> {
                // 正文首帧 → 当前思考块折叠已完成态
                val t = lastThink(turn)
                if (t != null && !t.done) t.done = true
                val seg = lastText(turn) ?: newSeg(turn, Seg.Text()) as Seg.Text
                seg.text += parseJsonString(ev.data, "text")
            }
            "answer.end" -> {
                val seg = lastText(turn) ?: newSeg(turn, Seg.Text()) as Seg.Text
                seg.done = true
            }
            "tool.start" -> {
                val seg = Seg.Tool(
                    callId = parseJsonString(ev.data, "call_id"),
                    label = parseJsonString(ev.data, "label"),
                )
                turn.segs.add(seg)
            }
            "tool.end" -> {
                val callId = parseJsonString(ev.data, "call_id")
                val seg = turn.segs.lastOrNull { it is Seg.Tool && it.callId == callId } as? Seg.Tool
                if (seg != null) {
                    seg.running = false
                    seg.status = parseJsonString(ev.data, "status")
                    seg.resultText = parseJsonString(ev.data, "text")
                }
            }
            "card" -> {
                // 服务端校验过的渲染卡：正文围栏按 ref 取（流式里围栏可能先到，卡片到齐后重组出卡）
                parseCardEvent(ev.data)?.takeIf { it.ref.isNotBlank() }?.let { turn.cards[it.ref] = it }
            }
            "proposal" -> {
                // 卡片嵌最后一个工具块内（tool.end{待确认} 先于 proposal）
                // 事件载荷是 {"proposal":{…}}：直接解外层会把整张卡解成默认值（全空、无按钮）
                // 服务端不下发 status 字段——刚出的提案必为 pending，这里补上缺省值
                // （空串会被 StatusChip 判为未知态显示「失败」）。
                val snap = parseCard(extractJsonObject(ev.data, "proposal") ?: ev.data)
                    ?.copy(status = "pending")
                val toolSeg = turn.segs.lastOrNull { it is Seg.Tool } as? Seg.Tool
                if (toolSeg != null) {
                    toolSeg.card = snap
                    toolSeg.cardExpireAt = snap?.let { expiryFromNow(it.expires_in) } ?: 0
                }
            }
            "turn.end" -> return true
            "error" -> throw ChatStreamException(parseJsonString(ev.data, "message").ifEmpty { "AI 出错了" })
        }
        return false
    }

    class ChatStreamException(message: String) : Exception(message)

    // ---------- 确认卡有效期（15 分钟未处理自动过期） ----------

    /** 剩余秒数 → 本地到期时刻；非正数表示服务端已判定过期或未下发（返回 0） */
    fun expiryFromNow(seconds: Int, now: Long = System.currentTimeMillis()): Long =
        if (seconds > 0) now + seconds * 1000L else 0L

    /** 卡片在当前时刻的有效状态：本地时间已过 → expired（不依赖用户回来点一次才变） */
    fun effectiveStatus(status: String, expireAt: Long, now: Long = System.currentTimeMillis()): String =
        if (status == "pending" && expireAt > 0 && now >= expireAt) "expired" else status

    /** 卡片剩余秒数（用于倒计时文案；已过期或未下发返回 0） */
    fun remainingSeconds(expireAt: Long, now: Long = System.currentTimeMillis()): Int =
        if (expireAt <= now) 0 else ((expireAt - now) / 1000).toInt()

    fun lastThink(m: RenderMessage): Seg.Think? =
        m.segs.lastOrNull { it is Seg.Think } as? Seg.Think

    fun lastText(m: RenderMessage): Seg.Text? =
        m.segs.lastOrNull { it is Seg.Text } as? Seg.Text

    private fun newSeg(m: RenderMessage, seg: Seg): Seg {
        m.segs.add(seg)
        return seg
    }

    /**
     * 「重新生成」的可重放参数：只对**尾条回复**、且该轮用户输入是纯文本时给。
     * 返回 (turnId, 该轮用户原文)，不可重放返回 null。
     *
     * 为什么抽成纯函数：这是唯一决定「能不能重放」的地方，判错就是把用户内容丢掉
     * （删整轮后才发现在重放不了）。放在 UI 里就只能靠手点，放在这里能单测。
     */
    data class RegenerateTarget(val turnId: String, val text: String)

    fun regenerableTarget(messages: List<RenderMessage>, target: RenderMessage): RegenerateTarget? {
        if (target.role != "assistant" || target.turnId.isEmpty()) return null
        // 只能删尾轮：删中间的轮会在历史里留坑
        if (messages.lastOrNull { it.role == "assistant" }?.id != target.id) return null
        val ask = messages.firstOrNull { it.role == "user" && it.turnId == target.turnId } ?: return null
        // 附件与引用重放不了（要用户重新选），宁可不给入口
        if (ask.hasRichInput) return null
        val text = ask.segs.filterIsInstance<Seg.Text>().joinToString("") { it.text }
        if (text.isBlank()) return null
        return RegenerateTarget(target.turnId, text)
    }

    /** 历史回显：parts → segs（与流式形态一致） */
    fun fromHistory(msg: ChatRepository.HistoryMessage): RenderMessage {
        val m = RenderMessage(id = msg.id, role = msg.role)
        m.turnId = msg.turn_id
        for (p in msg.parts) {
            // 结果卡 part：服务端校验后落库（与 SSE card 事件同形），先灌句柄映射，正文围栏随后按 ref 取卡
            if (p.type == "result_card") {
                val ref = p.ref.orEmpty()
                val data = p.data
                if (ref.isNotBlank() && data != null) {
                    m.cards[ref] = com.bookandmusic.lovelife.appnative.data.ChatUi.CardEvent(
                        validated = true, // 只有校验通过的卡才会落库
                        view = p.view.orEmpty(),
                        ref = ref,
                        title = p.title.orEmpty(),
                        note = p.note.orEmpty(),
                        only = p.only,
                        limit = p.limit,
                        data = data,
                    )
                }
            }
            when (p.type) {
                "thinking" -> m.segs.add(Seg.Think(p.text ?: "", p.seconds, done = true))
                "text" -> m.segs.add(Seg.Text(p.text ?: "", done = true))
                "tool" -> {
                    m.segs.add(
                        Seg.Tool(
                            callId = "h${m.segs.size}", // 历史 tool part 无 call_id，用位置稳定键
                            label = p.label ?: "",
                            running = false,
                            status = "ok",
                        ),
                    )
                }
                "card" -> {
                    // card part 独立挂卡（历史里带 card 的行；快照已在 part.card）
                    val toolSeg = Seg.Tool(callId = "h${m.segs.size}", label = p.label ?: "", running = false, status = "ok")
                    toolSeg.card = p.card
                    toolSeg.cardExpireAt = p.card?.let { expiryFromNow(it.expires_in) } ?: 0
                    m.segs.add(toolSeg)
                }
                "attachment" -> m.mediaCount += 1
                "quote" -> p.refs?.label?.takeIf { it.isNotBlank() }?.let { m.quotes = m.quotes + it }
                // result_card 只进句柄映射（卡由正文围栏渲染）；interrupted 走下面的标记
            }
        }
        if (msg.parts.any { it.type == "interrupted" }) m.interrupted = true
        if (msg.parts.any { it.type == "attachment" || it.type == "quote" }) m.hasRichInput = true
        return m
    }

    // ---------- SSE data 字段提取（统一走 kotlinx.serialization：与 DTO 解码同一套解析语义） ----------

    /** SSE data 解析成对象；不是合法对象时返回 null，调用方按缺省值处理 */
    private fun jsonObjectOrNull(json: String): JsonObject? =
        runCatching { ApiClient.json.parseToJsonElement(json) as? JsonObject }.getOrNull()

    /** 字符串字段；缺失、null、非字符串（数字/布尔）都返回空串 */
    internal fun parseJsonString(json: String, key: String): String {
        val el = jsonObjectOrNull(json)?.get(key) as? JsonPrimitive ?: return ""
        return if (el.isString) el.content else ""
    }

    /** 嵌套对象字段的原始 JSON 串（如 proposal 的 `{"proposal":{…}}`）；不是对象返回 null */
    internal fun extractJsonObject(json: String, key: String): String? =
        (jsonObjectOrNull(json)?.get(key) as? JsonObject)?.toString()

    internal fun parseJsonLong(json: String, key: String): Long =
        (jsonObjectOrNull(json)?.get(key) as? JsonPrimitive)?.longOrNull ?: 0L

    internal fun parseJsonInt(json: String, key: String): Int =
        (jsonObjectOrNull(json)?.get(key) as? JsonPrimitive)?.intOrNull ?: 0

    internal fun parseCard(json: String): ChatRepository.CardSnapshot? {
        // SSE 流里的提案字段名是 id（历史回显才是 action_id）——两者同一事实。
        // kotlinx 不支持多别名，decode 又因全字段默认值从不抛错，所以只能在解码后补：
        // action_id 仍为 0 时从原始 JSON 取 id 填充。
        val snap = runCatching {
            ApiClient.json.decodeFromString(ChatRepository.CardSnapshot.serializer(), json)
        }.getOrNull() ?: return null
        if (snap.action_id != 0L) return snap
        val id = (ApiClient.json.parseToJsonElement(json) as? JsonObject)
            ?.get("id")?.jsonPrimitive?.longOrNull ?: return snap
        return snap.copy(action_id = id)
    }

    /**
     * SSE `card` 事件 / 历史 part 的 card_event → 渲染卡。
     * 载荷就是卡对象本身（`{validated, view, ref, title, note, data}`）；包一层 `{"card":{…}}` 也认。
     */
    internal fun parseCardEvent(json: String): com.bookandmusic.lovelife.appnative.data.ChatUi.CardEvent? {
        fun decode(s: String) = runCatching {
            ApiClient.json.decodeFromString(
                com.bookandmusic.lovelife.appnative.data.ChatUi.CardEvent.serializer(), s,
            )
        }.getOrNull()
        val direct = decode(json)
        if (direct != null && direct.ref.isNotBlank()) return direct
        return extractJsonObject(json, "card")?.let(::decode) ?: direct
    }
}
