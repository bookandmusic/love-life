package com.bookandmusic.lovelife.appnative

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.AnnotatedString
import com.bookandmusic.lovelife.appnative.data.ChatState
import com.bookandmusic.lovelife.appnative.ui.markdownOf
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * AI 模式收尾的纯逻辑：确认卡有效期 + 轻量 Markdown。
 * 两者都会「静默错」——卡片永远挂着待确认、正文把列表当成破折号，肉眼不报错但功能失效，
 * 所以钉在单测里。
 */
class ChatRenderTest {

    // ---------- 确认卡有效期（15 分钟未处理自动过期） ----------

    @Test
    fun expiryIsAbsoluteDeadlineNotRawSeconds() {
        val now = 1_000_000L
        assertEquals(now + 900_000L, ChatState.expiryFromNow(900, now))
        // 非正数（服务端已判过期 / 未下发）→ 没有倒计时，交给 status 说话
        assertEquals(0L, ChatState.expiryFromNow(0, now))
        assertEquals(0L, ChatState.expiryFromNow(-5, now))
    }

    @Test
    fun pendingTurnsExpiredLocallyAtDeadline() {
        val deadline = 2_000_000L
        // 未到点：仍是待确认
        assertEquals("pending", ChatState.effectiveStatus("pending", deadline, deadline - 1))
        // 到点：本地就转已过期，不必等用户点一次
        assertEquals("expired", ChatState.effectiveStatus("pending", deadline, deadline))
        assertEquals("expired", ChatState.effectiveStatus("pending", deadline, deadline + 60_000))
    }

    @Test
    fun resolvedCardsKeepServerStatus() {
        // 已裁决的卡不参与本地倒计时覆盖（否则「已执行」会被改成「已过期」）
        val deadline = 1L
        assertEquals("executed", ChatState.effectiveStatus("executed", deadline, 10_000))
        assertEquals("cancelled", ChatState.effectiveStatus("cancelled", deadline, 10_000))
        // 无到期时刻（0）时 pending 也保持原状
        assertEquals("pending", ChatState.effectiveStatus("pending", 0, 10_000))
    }

    @Test
    fun remainingSecondsCountsDown() {
        val now = 5_000_000L
        assertEquals(900, ChatState.remainingSeconds(now + 900_000, now))
        assertEquals(0, ChatState.remainingSeconds(now, now))
        assertEquals(0, ChatState.remainingSeconds(now - 1, now))
        assertEquals(0, ChatState.remainingSeconds(0, now))
    }

    // ---------- 轻量 Markdown ----------

    private fun md(src: String): AnnotatedString = markdownOf(src, Color.Gray)

    @Test
    fun bulletListDropsMarkerAndKeepsText() {
        val out = md("- 第一条\n- 第二条").text
        assertFalse("列表符号不该原样留在正文里", out.contains("- 第一条"))
        assertTrue(out.contains("第一条"))
        assertTrue(out.contains("第二条"))
        assertTrue("列表行要有可见的项目符号", out.contains("•"))
    }

    @Test
    fun orderedListKeepsItsNumber() {
        val out = md("1. 先做这个\n2. 再做那个").text
        assertTrue(out.contains("1."))
        assertTrue(out.contains("先做这个"))
        assertTrue(out.contains("2."))
    }

    @Test
    fun fencedCodeIsKeptVerbatim() {
        val src = "```\nval a = 1\nval b = 2\n```"
        val out = md(src).text
        assertFalse("围栏标记本身不该出现", out.contains("```"))
        assertTrue(out.contains("val a = 1"))
        assertTrue(out.contains("val b = 2"))
    }

    @Test
    fun inlineCodeLosesBackticks() {
        val out = md("请运行 `gradlew build` 试试").text
        assertFalse(out.contains("`"))
        assertTrue(out.contains("gradlew build"))
    }

    @Test
    fun linkKeepsTextDropsUrl() {
        val out = md("见 [项目主页](https://example.com/x) 了解").text
        assertTrue(out.contains("项目主页"))
        assertFalse("正文里不该出现裸 URL（也不做可点外链）", out.contains("https://example.com"))
    }

    @Test
    fun boldMarkersAreConsumed() {
        val out = md("这是 **重点** 内容").text
        assertFalse(out.contains("**"))
        assertTrue(out.contains("重点"))
    }

    @Test
    fun headingDropsHashes() {
        val out = md("## 小结\n正文").text
        assertFalse(out.contains("##"))
        assertTrue(out.contains("小结"))
    }

    @Test
    fun quoteLineGetsBarPrefix() {
        val out = md("> 她说的那句话").text
        assertFalse(out.contains("> 她说的那句话"))
        assertTrue(out.contains("她说的那句话"))
        assertTrue(out.contains("│"))
    }

    @Test
    fun plainTextSurvivesUntouched() {
        val out = md("今天我们去了西湖，风很大。").text
        assertEquals("今天我们去了西湖，风很大。", out)
    }
}