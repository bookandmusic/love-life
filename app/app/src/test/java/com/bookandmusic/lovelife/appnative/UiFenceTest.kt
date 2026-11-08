package com.bookandmusic.lovelife.appnative

import com.bookandmusic.lovelife.appnative.data.ChatUi
import com.bookandmusic.lovelife.appnative.data.UiFence
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * 客户端展示工具：
 * 围栏拆块 / ref 校验 / 只认服务端 validated 的 view / 画不出来时落一行人话（不猜、不空串）。
 */
class UiFenceTest {

    private fun data(
        ref: String = "r_1",
        count: Int = 2,
        media: Boolean = false,
        stats: Boolean = false,
        entity: Boolean = false,
    ) = ChatUi.ResultData(
        ref = ref,
        title = "标题",
        records = (1..count).map { i ->
            ChatUi.UiRecord(
                id = i.toLong(),
                kind = if (media) "media" else "post",
                text = "条目$i",
                thumb_url = if (media) "u$i" else "",
            )
        },
        entity = if (entity) ChatUi.UiEntity(kind = "post", id = 1, text = "正文") else null,
        stats = if (stats) listOf(ChatUi.UiStat(label = "动态", value = "23 条")) else emptyList(),
    )

    private fun card(
        ref: String = "r_1",
        view: String = "list",
        validated: Boolean = true,
        data: ChatUi.ResultData? = data(ref),
    ) = ChatUi.CardEvent(validated = validated, view = view, ref = ref, title = "标题", data = data)

    private fun cards(vararg items: ChatUi.CardEvent) = items.associateBy { it.ref }

    private fun fence(ref: String, view: String = "list") = "```ui\n{\"ref\":\"$ref\",\"view\":\"$view\"}\n```"

    @Test
    fun `单卡围栏解析出卡片块`() {
        val body = "我们的空间数据有：\n${fence("r_1", "stats")}\n以上。"
        val out = UiFence.parse(body, cards(card(view = "stats", data = data(stats = true))), complete = true)
        assertEquals(3, out.size)
        assertTrue(out[0] is UiFence.Block.Text)
        val block = out[1] as UiFence.Block.Card
        assertEquals("stats", block.event.view)
        assertEquals("标题", block.data.title)
        assertTrue(out[2] is UiFence.Block.Text)
    }

    @Test
    fun `多卡与前后文本保持顺序`() {
        val a = card(ref = "r_a", view = "grid", data = data(ref = "r_a", media = true))
        val b = card(ref = "r_b", view = "list", data = data(ref = "r_b"))
        val body = "先说一句\n${fence("r_a", "grid")}\n中间\n${fence("r_b")}\n收尾"
        val out = UiFence.parse(body, cards(a, b), complete = true)
        assertEquals(5, out.size)
        assertEquals("r_a", (out[1] as UiFence.Block.Card).event.ref)
        assertEquals("r_b", (out[3] as UiFence.Block.Card).event.ref)
    }

    @Test
    fun `流式中未闭合围栏按文本缓冲`() {
        val body = "前言\n```ui\n{\"ref\":\"r_1\",\"view\":\"list\""
        val out = UiFence.parse(body, cards(card()), complete = false)
        // 不产出卡片块，正文完整（闭合后重组自然出卡）
        assertTrue(out.all { it is UiFence.Block.Text })
    }

    @Test
    fun `无效ref降级为一行人话且正文不丢`() {
        val body = "A\n${fence("nope")}\nB"
        val out = UiFence.parse(body, cards(card()), complete = true)
        assertEquals(3, out.size)
        assertTrue(out[0] is UiFence.Block.Text)
        val failed = out[1] as UiFence.Block.Failed
        assertTrue("不能给空串：用户要看得见一句人话", failed.reason.isNotBlank())
        assertTrue(out[2] is UiFence.Block.Text)
    }

    // 流式中服务端的 card 事件可能还没到：先不占位、别甩一句「结果已失效」再改成卡
    @Test
    fun `流式中引用不到先算在路上`() {
        val out = UiFence.parse(fence("r_1"), emptyMap(), complete = true, settled = false)
        assertEquals(UiFence.Block.Pending, out[0])
    }

    @Test
    fun `坏JSON降级`() {
        val body = "```ui\n{broken\n```"
        val out = UiFence.parse(body, cards(card()), complete = true)
        assertTrue(out[0] is UiFence.Block.Failed)
    }

    @Test
    fun `普通代码块不是卡片`() {
        val body = "看代码：\n```kotlin\nval a = 1\n```\n完"
        val out = UiFence.parse(body, cards(card()), complete = true)
        // 整段走文本（markdownish 自己处理代码块等宽），不产出卡片
        assertTrue(out.all { it is UiFence.Block.Text })
    }

    @Test
    fun `单行围栏也解析`() {
        val body = "总结一句。\n```ui {\"ref\":\"r_1\",\"view\":\"stats\",\"title\":\"我们的空间\"}```\n收尾问句。"
        val out = UiFence.parse(body, cards(card(view = "stats", data = data(stats = true))), complete = true)
        val found = out.filterIsInstance<UiFence.Block.Card>()
        assertEquals(1, found.size)
        assertEquals("r_1", found[0].event.ref)
    }

    // 客户端只认服务端下发的 view：围栏里模型写什么形态不作数（写错由服务端校验并让它重写）
    @Test
    fun `形态只认服务端下发的那份`() {
        val body = fence("r_1", "grid")
        val out = UiFence.parse(body, cards(card(view = "list")), complete = true)
        assertEquals("list", (out[0] as UiFence.Block.Card).event.view)
    }

    @Test
    fun `非法view不渲染`() {
        // 服务端只放行四型；出现别的值一律不出卡（不猜成 list，也不画一半）
        val body = fence("r_1", "pie")
        val out = UiFence.parse(body, cards(card(view = "pie")), complete = true)
        assertTrue("非法形态不能出卡", out.none { it is UiFence.Block.Card })
        assertTrue((out[0] as UiFence.Block.Failed).reason.isNotBlank())
    }

    @Test
    fun `未校验的卡不渲染`() {
        val body = fence("r_1")
        val out = UiFence.parse(body, cards(card(validated = false)), complete = true)
        assertTrue(out.none { it is UiFence.Block.Card })
        assertTrue((out[0] as UiFence.Block.Failed).reason.isNotBlank())
    }

    @Test
    fun `没有数据的卡不渲染`() {
        val body = fence("r_1")
        val out = UiFence.parse(body, cards(card(data = null)), complete = true)
        assertTrue(out.none { it is UiFence.Block.Card })
        assertTrue((out[0] as UiFence.Block.Failed).reason.isNotBlank())
    }

    @Test
    fun `围栏没写ref不渲染`() {
        val body = "```ui\n{\"view\":\"list\"}\n```"
        val out = UiFence.parse(body, cards(card()), complete = true)
        assertTrue(out.none { it is UiFence.Block.Card })
        assertTrue((out[0] as UiFence.Block.Failed).reason.isNotBlank())
    }

    /** kpi 是新形态：客户端认它，并把 only / limit 原样带到渲染层（裁剪在 ChatCards 做） */
    @Test
    fun `kpi 卡与收窄指令透传`() {
        val event = ChatUi.CardEvent(
            validated = true, view = "kpi", ref = "r_1", title = "动态总数",
            only = listOf("动态"), limit = 0,
            data = data(stats = true),
        )
        val body = "一共 23 条：\n${fence("r_1", "kpi")}"
        val out = UiFence.parse(body, mapOf("r_1" to event), complete = true)
        val block = out.filterIsInstance<UiFence.Block.Card>().single()
        assertEquals("kpi", block.event.view)
        assertEquals(listOf("动态"), block.event.only)

        val listEvent = ChatUi.CardEvent(validated = true, view = "list", ref = "r_1", limit = 2, data = data())
        val out2 = UiFence.parse("这两条：\n${fence("r_1", "list")}", mapOf("r_1" to listEvent), complete = true)
        assertEquals(2, out2.filterIsInstance<UiFence.Block.Card>().single().event.limit)
    }

}
