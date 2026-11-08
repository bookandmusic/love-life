package com.bookandmusic.lovelife.appnative.data

import com.bookandmusic.lovelife.appnative.data.ChatUi.CardEvent

/**
 * 正文 ```ui 围栏 → 块序列。
 *
 * 围栏是模型写的**卡片提案**（[ChatUi.CardSpec]：ref / view / title / note），客户端不按它画：
 * 只拿 ref 去查服务端下发的 [CardEvent]（`validated: true` + 最终 view）。**只解析、不兜底**：
 * 引用不存在 / 未校验 / 形态不认识 / 没有数据 → 一行人话（[Block.Failed]），不猜形态、不空串。
 *
 * - 只有语言标记恰为 `ui` 的围栏才是卡片；普通 ``` 代码块走文本原样渲染
 * - 流式中围栏未闭合 → 尾部按文本缓冲，闭合后重组自然出卡
 */
object UiFence {

    /** 正文里的一个渲染块 */
    sealed interface Block {
        /** 普通文本（走 markdownish） */
        data class Text(val text: String) : Block

        /** 已验证的卡：[event] 带最终形态与文案，[data] 已确认非空 */
        data class Card(val event: CardEvent, val data: ChatUi.ResultData) : Block

        /**
         * 卡片还在路上：本轮尚未结束（服务端校验后补发 card 事件）。
         * 只在流式期间出现——先不占位、也不报错，事件到达后自动出卡。
         */
        data object Pending : Block

        /** 这张卡画不出来：reason 是给人看的一句话 */
        data class Failed(val reason: String) : Block
    }

    // 开栏两种写法都认：多行（```ui 独占一行，JSON 在后续行）与单行（```ui {json} ```——
    // 模型实测两种都会输出；单行不认会让卡片漏解析、只能走兜底，与工具行内的兜底路径叠成双卡）
    private val FENCE_OPEN = Regex("^```ui\\s*(\\{.*\\})?\\s*(```)?\\s*$")
    private val FENCE_CLOSE = Regex("^```\\s*$")

    /**
     * 把正文拆成块序列。
     * @param cards 本轮已验证的渲染卡（ref → CardEvent）
     * @param complete false = 流式中（尾部未闭合围栏按文本处理，闭合后下次重组自然出卡）
     * @param settled false = 本轮还没结束（card 事件可能还在路上：引用不到先算 [Block.Pending]）
     */
    fun parse(
        body: String,
        cards: Map<String, CardEvent>,
        complete: Boolean,
        settled: Boolean = true,
    ): List<Block> {
        val out = mutableListOf<Block>()
        val lines = body.split('\n')
        var i = 0
        var textBuf = StringBuilder()
        fun flushText() {
            if (textBuf.isNotEmpty()) {
                out.add(Block.Text(textBuf.toString()))
                textBuf = StringBuilder()
            }
        }
        while (i < lines.size) {
            val line = lines[i]
            val inline = FENCE_OPEN.find(line)   // 单行：```ui {json} ```
            if (inline != null) {
                val inlineJson = inline.groupValues[1]
                if (inlineJson.isNotEmpty()) {
                    flushText()
                    out.add(cardBlock(inlineJson, cards, settled))
                    i++
                    continue
                }
                // 多行：找闭合围栏
                var j = i + 1
                var closed = false
                val json = StringBuilder()
                while (j < lines.size) {
                    if (FENCE_CLOSE.containsMatchIn(lines[j])) { closed = true; break }
                    json.append(lines[j]).append('\n')
                    j++
                }
                if (!closed && !complete) {
                    // 流式中尾部未闭合：先按文本走（闭合后完整重拆）
                    textBuf.append(line).append('\n')
                    i++
                    continue
                }
                flushText()
                if (!closed) {
                    // complete 但仍未闭合（异常残局）：按文本原样
                    textBuf.append(line)
                    i++
                    continue
                }
                out.add(cardBlock(json.toString(), cards, settled))
                i = j + 1
                continue
            }
            textBuf.append(line)
            if (i != lines.lastIndex) textBuf.append('\n')
            i++
        }
        flushText()
        return out
    }

    /** 单个围栏 JSON → 卡片块：只认服务端已验证的那份，任何一环不成立都落一行人话 */
    internal fun cardBlock(json: String, cards: Map<String, CardEvent>, settled: Boolean = true): Block {
        val spec = runCatching {
            ApiClient.json.decodeFromString(ChatUi.CardSpec.serializer(), json.trim())
        }.getOrNull() ?: return Block.Failed("这张卡没能显示（围栏格式不对）")
        val ref = spec.ref.trim()
        if (ref.isEmpty()) return Block.Failed("这张卡没能显示（围栏里没写引用）")
        val event = cards[ref]
            ?: return if (settled) Block.Failed("这张卡没能显示（引用的结果已失效）") else Block.Pending
        if (!event.validated) return Block.Failed("这张卡没能显示（服务端还没校验通过）")
        if (event.view !in ChatUi.VIEWS) return Block.Failed("这张卡没能显示（不支持的形态）")
        val data = event.data ?: return Block.Failed("这张卡没能显示（没有数据）")
        return Block.Card(event, data)
    }
}
