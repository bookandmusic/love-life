package com.bookandmusic.lovelife.appnative

import com.bookandmusic.lovelife.appnative.data.ChatRepository.SseEvent
import com.bookandmusic.lovelife.appnative.data.ChatState
import com.bookandmusic.lovelife.appnative.data.ChatUi
import com.bookandmusic.lovelife.appnative.data.ChatState.Seg
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import androidx.compose.runtime.snapshots.Snapshot
import org.junit.Test

/** 分段状态机单测：分段/求和/归位/历史同形态。 */
class ChatStateTest {

    private fun ev(event: String, json: String) = SseEvent(event, json)

    @Test fun `thinkDeltaAccumulateAndFold`() {
        val m = ChatState.RenderMessage(1, "assistant")
        ChatState.applyEvent(m, ev("thinking.delta", """{"text":"AAABBB"}"""))
        ChatState.applyEvent(m, ev("thinking.delta", """{"text":"中"}"""))
        ChatState.applyEvent(m, ev("thinking.end", """{"seconds":3}"""))
        ChatState.applyEvent(m, ev("answer.delta", """{"text":"答"}"""))
        val think = m.segs[0] as Seg.Think
        assertEquals("AAABBB中", think.text.toString())
        assertEquals(3, think.seconds)
        assertTrue(think.done) // 正文首帧折叠
        assertTrue(m.segs[1] is Seg.Text)
    }

    @Test fun `multipleThinkSecondsSum`() {
        val m = ChatState.RenderMessage(1, "assistant")
        ChatState.applyEvent(m, ev("thinking.end", """{"seconds":2}"""))
        ChatState.applyEvent(m, ev("tool.start", """{"call_id":"c1","label":"查询动态"}"""))
        ChatState.applyEvent(m, ev("tool.end", """{"call_id":"c1","status":"ok","text":"查到 3 条"}"""))
        ChatState.applyEvent(m, ev("thinking.delta", """{"text":"再想"}"""))
        ChatState.applyEvent(m, ev("thinking.end", """{"seconds":5}"""))
        val thinks = m.segs.filterIsInstance<Seg.Think>()
        // 契约：秒数取各次思考之和——渲染层求和，这里验证两块各自持秒
        assertEquals(2, thinks[0].seconds)
        assertEquals(5, thinks[1].seconds)
        assertEquals(7, thinks.sumOf { it.seconds })
    }

    @Test fun `toolEndOutOfOrderByCallId`() {
        val m = ChatState.RenderMessage(1, "assistant")
        ChatState.applyEvent(m, ev("tool.start", """{"call_id":"a","label":"查动态"}"""))
        ChatState.applyEvent(m, ev("tool.start", """{"call_id":"b","label":"查相册"}"""))
        // b 先结束（并行批次乱序）
        ChatState.applyEvent(m, ev("tool.end", """{"call_id":"b","status":"ok","text":"b完"}"""))
        val segA = m.segs[0] as Seg.Tool
        val segB = m.segs[1] as Seg.Tool
        assertTrue(segA.running) // a 未收口
        assertTrue(!segB.running)
        assertEquals("b完", segB.resultText)
        ChatState.applyEvent(m, ev("tool.end", """{"call_id":"a","status":"ok","text":"a完"}"""))
        assertTrue(!segA.running)
    }

    @Test fun `proposalEmbedsInLastTool`() {
        val m = ChatState.RenderMessage(1, "assistant")
        ChatState.applyEvent(m, ev("tool.start", """{"call_id":"c1","label":"发动态"}"""))
        ChatState.applyEvent(m, ev("tool.end", """{"call_id":"c1","status":"ok","text":"待确认"}"""))
        ChatState.applyEvent(m, ev("proposal", """{"action_id":9,"intent":"发布动态","summary":"发一条动态","status":"pending"}"""))
        val seg = m.segs[0] as Seg.Tool
        assertEquals(9L, seg.card?.action_id)
        assertEquals("pending", seg.card?.status)
    }

    @Test fun `jsonEscapeParse`() {
        assertEquals("a\"b\nc", ChatState.parseJsonString("""{"text":"a\"b\nc"}""", "text"))
        assertEquals(7, ChatState.parseJsonInt("""{"seconds":7}""", "seconds"))
    }

    @Test fun `historyPartsSameShapeAsStream`() {
        val hm = com.bookandmusic.lovelife.appnative.data.ChatRepository.HistoryMessage(
            id = 5, role = "assistant",
            parts = listOf(
                com.bookandmusic.lovelife.appnative.data.ChatRepository.Part(type = "thinking", text = "想了", seconds = 4),
                com.bookandmusic.lovelife.appnative.data.ChatRepository.Part(type = "tool", label = "查询"),
                com.bookandmusic.lovelife.appnative.data.ChatRepository.Part(type = "text", text = "结果"),
            ),
        )
        val m = ChatState.fromHistory(hm)
        assertEquals(3, m.segs.size)
        assertTrue(m.segs[0] is Seg.Think && (m.segs[0] as Seg.Think).seconds == 4 && (m.segs[0] as Seg.Think).done)
        assertTrue(m.segs[1] is Seg.Tool && !(m.segs[1] as Seg.Tool).running)
        assertTrue(m.segs[2] is Seg.Text)
    }

    /**
     * 「重新生成」的可重放判定：只有**尾条回复 + 纯文本输入**才给。
     * 判错就是删完整轮才发现重放不了（用户内容丢失），所以这里逐条钉死。
     */
    @Test fun `regenerableOnlyForLastPlainTextAnswer`() {
        fun user(turn: String, text: String, rich: Boolean = false) =
            ChatState.RenderMessage(System.nanoTime(), "user").apply {
                turnId = turn
                hasRichInput = rich
                segs.add(Seg.Text(text, done = true))
            }

        fun answer(turn: String) = ChatState.RenderMessage(System.nanoTime(), "assistant").apply { turnId = turn }

        val ask1 = user("t1", "第一条问题")
        val a1 = answer("t1")
        val ask2 = user("t2", "第二条问题")
        val a2 = answer("t2")

        // 尾条 + 纯文本 → 可重放，且带原文
        val tgt = ChatState.regenerableTarget(listOf(ask1, a1, ask2, a2), a2)
        assertEquals("t2", tgt?.turnId)
        assertEquals("第二条问题", tgt?.text)

        // 非尾条（删中间轮会留坑）
        assertEquals(null, ChatState.regenerableTarget(listOf(ask1, a1, ask2, a2), a1))

        // 该轮带附件/引用 → 重放不了
        val richAsk = user("t3", "带图的问题", rich = true)
        val a3 = answer("t3")
        assertEquals(null, ChatState.regenerableTarget(listOf(ask1, a1, richAsk, a3), a3))

        // turn_id 缺失（老数据）→ 定位不到整轮
        val noTurn = answer("")
        assertEquals(null, ChatState.regenerableTarget(listOf(ask1, a1, noTurn), noTurn))

        // 只有媒体没有文字 → 重放内容为空，也不给
        val mediaOnly = ChatState.RenderMessage(System.nanoTime(), "user").apply { turnId = "t4" }
        val a4 = answer("t4")
        assertEquals(null, ChatState.regenerableTarget(listOf(mediaOnly, a4), a4))
    }

    /**
     * 历史回显的引用与附件：服务端一条 ref 一个 quote part（`refs` 是**对象**）、附件一条一个 part。
     * 形状对不上时 kotlinx 会整条消息解析失败 → history 退化成空会话（不是少一行，是整页没了），
     * 所以这里按服务端真实 JSON 直接过一遍反序列化，而不是手工构造 Part。
     */
    @Test fun `historyQuoteAndAttachmentEcho`() {
        val raw = """
            {"id":7,"role":"user","created_at":"2026-11-06 10:00:00","turn_id":"t1","parts":[
              {"type":"text","text":"这条动态是哪天发的"},
              {"type":"quote","refs":{"result_id":"r_59","ids":[48],"label":"海边日落那张"}},
              {"type":"attachment","name":"photo-abc","mime":"image/png","size":0}
            ]}
        """.trimIndent()
        val msg = com.bookandmusic.lovelife.appnative.data.ApiClient.json
            .decodeFromString(com.bookandmusic.lovelife.appnative.data.ChatRepository.HistoryMessage.serializer(), raw)
        val m = ChatState.fromHistory(msg)
        assertEquals(listOf("海边日落那张"), m.quotes)
        assertEquals(1, m.mediaCount)
        assertEquals("t1", m.turnId)
        assertTrue(m.hasRichInput) // 带引用与附件 → 不给「重新生成」
    }

    @Test fun `nestedJsonObjectExtraction`() {
        val payload = """{"call_id":"c1","status":"ok","text":"查到 3 条","card":{"validated":true,"view":"list","ref":"r_1"}}"""
        val cardJson = ChatState.extractJsonObject(payload, "card")
        assertEquals("""{"validated":true,"view":"list","ref":"r_1"}""", cardJson)
        assertEquals(null, ChatState.extractJsonObject(payload, "nope"))
        // card 事件：载荷就是卡对象本身，包一层 {"card":{…}} 也认
        val direct = ChatState.parseCardEvent(
            """{"validated":true,"view":"list","ref":"r_1","title":"动态","data":{"ref":"r_1","records":[]}}"""
        )
        assertEquals("r_1", direct?.ref)
        assertEquals(true, direct?.validated)
        val wrapped = ChatState.parseCardEvent("""{"card":{"validated":true,"view":"stats","ref":"r_2","data":{"ref":"r_2"}}}""")
        assertEquals("r_2", wrapped?.ref)
        assertEquals("stats", wrapped?.view)
    }

    // 真实 payload 形状：服务端对「没有值的字段」下发 null，不是省字段。
    // 回归点：null 会让整个块解码失败 → 解析返回 null → 统计卡与工具行整段消失。
    @Test fun `cardEventWithNullRecordsParses`() {
        val payload = """{"validated":true,"view":"stats","ref":"r_1","title":"我们的空间","data":{"ref":"r_1","kind":"stats","records":null,"stats":[{"label":"在一起","value":"1286 天","hint":"自 2023-04-29"},{"label":"动态","value":"23 条"}],"series":[{"title":"按年","points":[{"label":"2026","count":23}]}]}}"""
        val card = ChatState.parseCardEvent(payload)
        assertTrue("records:null 不该让整块解析失败", card != null)
        assertEquals("stats", card!!.view)
        assertEquals(2, card.data?.stats?.size)
        assertEquals("1286 天", card.data?.stats?.get(0)?.value)
        assertEquals(1, card.data?.series?.size)
    }

    // 流式侧：card 事件按 ref 进句柄映射，正文围栏随后取卡
    @Test fun `cardEventRegistersByRef`() {
        val m = ChatState.RenderMessage(1, "assistant")
        ChatState.applyEvent(
            m,
            ev("card", """{"validated":true,"view":"list","ref":"r_7","title":"足迹","data":{"ref":"r_7","records":[]}}"""),
        )
        assertEquals("list", m.cards["r_7"]?.view)
    }

    // 守卫：流式期间能刷新，靠的是「读正文时读到的是快照状态」。
    // 若 Seg.Text.text 换成普通 String/StringBuilder，读取不注册观察者，
    // 界面就退回「整轮结束才一次性出现」——这个用例会红。
    @Test fun `streamingTextReadsSnapshotState`() {
        val m = ChatState.RenderMessage(1, "assistant")
        ChatState.applyEvent(m, ev("answer.delta", """{"text":"你"}"""))
        val seg = m.segs[0] as Seg.Text // 先取出引用，避免 segs 列表本身贡献读
        val reads = mutableListOf<Any>()
        Snapshot.observe({ reads.add(it) }, {}) { seg.text }
        assertTrue("正文必须由快照状态支撑，否则流式不会逐字刷新", reads.isNotEmpty())
    }

    // 折叠抗抖：中间轮正文不触发折叠判据——末段才是正文。
    // 判据在渲染层（ChatScreen），这里钉状态机前提：中间 answer.delta 不新增 Text 段、
    // 后续 tool.start 后 segs 末段是 Tool（即「还没出结论」）。
    @Test fun `intermediateTextKeepsToolLast`() {
        val m = ChatState.RenderMessage(1, "assistant")
        ChatState.applyEvent(m, ev("thinking.end", """{"seconds":1}"""))
        ChatState.applyEvent(m, ev("answer.delta", """{"text":"我先查一下"}""")) // 中间轮正文
        ChatState.applyEvent(m, ev("tool.start", """{"call_id":"c1","label":"查询动态"}"""))
        ChatState.applyEvent(m, ev("tool.end", """{"call_id":"c1","status":"ok","text":"3 条"}"""))
        // 末段是 Tool → 渲染层 concluded=false，折叠判据不触发
        assertTrue(m.segs.last() is Seg.Tool)
        // 中间轮正文没有另开 Text 段（复用 lastText）
        assertEquals(1, m.segs.count { it is Seg.Text })
    }

    // 历史侧句柄映射：fromHistory 把 result_card part 灌进 cards，
    // 刷新后正文 ```ui 围栏按 ref 仍能取到卡（形态是服务端校验过的那份）。
    @Test fun `historyResultCardPartFeedsCards`() {
        val hm = com.bookandmusic.lovelife.appnative.data.ChatRepository.HistoryMessage(
            id = 7, role = "assistant",
            parts = listOf(
                com.bookandmusic.lovelife.appnative.data.ChatRepository.Part(type = "tool", label = "搜索动态"),
                com.bookandmusic.lovelife.appnative.data.ChatRepository.Part(
                    type = "result_card", view = "list", ref = "r_9", title = "动态",
                    data = ChatUi.ResultData(ref = "r_9", kind = "records"),
                ),
                com.bookandmusic.lovelife.appnative.data.ChatRepository.Part(
                    type = "text", text = "看这些\n```ui\n{\"ref\":\"r_9\",\"view\":\"list\"}\n```",
                ),
            ),
        )
        val m = ChatState.fromHistory(hm)
        assertTrue(m.cards.containsKey("r_9"))
        // result_card 不占渲染段：tool + text 两段
        assertEquals(2, m.segs.size)
        // 正文里的围栏可解析出卡片块
        val blocks = com.bookandmusic.lovelife.appnative.data.UiFence.parse(
            (m.segs[1] as Seg.Text).text, m.cards, complete = true)
        val parsed = blocks.filterIsInstance<com.bookandmusic.lovelife.appnative.data.UiFence.Block.Card>().first()
        assertEquals("r_9", parsed.event.ref)
    }

    // turn_id 由 SSE 回调（裸线程）写、主线程读：落在快照状态上才有安全发布。
    // 换成普通 var 后这个用例会红——读到的值可能一直是空，整轮重放不了。
    @Test fun `turnIdReadsSnapshotState`() {
        val m = ChatState.RenderMessage(1, "assistant")
        val reads = mutableListOf<Any>()
        Snapshot.observe({ reads.add(it) }, {}) { m.turnId }
        assertTrue("turnId 必须由快照状态支撑（SSE 线程写 / 主线程读）", reads.isNotEmpty())
    }

    // SSE data 字段提取统一走 kotlinx.serialization：null / 缺失 / 非 JSON 都按缺省值处理，不抛异常。
    @Test fun `jsonExtractionHandlesNullMissingAndBadInput`() {
        assertEquals("", ChatState.parseJsonString("""{"text":null}""", "text"))
        assertEquals("", ChatState.parseJsonString("""{"text":3}""", "text")) // 非字符串不冒充文本
        assertEquals(0L, ChatState.parseJsonLong("""{"session_id":null}""", "session_id"))
        assertEquals(0, ChatState.parseJsonInt("""{"seconds":"oops"}""", "seconds"))
        assertEquals("", ChatState.parseJsonString("not json", "text"))
        assertEquals(null, ChatState.extractJsonObject("""{"card":[]}""", "card"))
        // 字符串化数字仍按数字读（服务端偶有把 id 写成字符串）
        assertEquals(42L, ChatState.parseJsonLong("""{"session_id":"42"}""", "session_id"))
        // unicode 转义交给 kotlinx 解码
        assertEquals("中", ChatState.parseJsonString("""{"text":"\u4e2d"}""", "text"))
    }
}
