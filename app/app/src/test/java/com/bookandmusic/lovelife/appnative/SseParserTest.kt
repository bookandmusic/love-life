package com.bookandmusic.lovelife.appnative

import com.bookandmusic.lovelife.appnative.data.ChatRepository
import com.bookandmusic.lovelife.appnative.data.ChatRepository.SseEvent
import okio.Buffer
import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * SSE 分帧解析单测（text/event-stream 契约）：
 * 空行分界 / event+data 行 / 注释行忽略 / 多行 data / 流末无空行收尾 / 半包 UTF-8 不进本层（Buffer 行读）。
 */
class SseParserTest {

    private fun parse(vararg lines: String): List<SseEvent> {
        val buf = Buffer().apply { writeUtf8(lines.joinToString("") { "$it\n" }) }
        val out = mutableListOf<SseEvent>()
        ChatRepository.parseSse(buf) { out.add(it) }
        return out
    }

    @Test fun `基本事件 - event与data空行分界`() {
        val ev = parse(
            "event: thinking.delta",
            """data: {"text":"你"}""",
            "",
            "event: thinking.delta",
            """data: {"text":"好"}""",
            "",
        )
        assertEquals(2, ev.size)
        assertEquals("thinking.delta", ev[0].event)
        assertEquals("""{"text":"你"}""", ev[0].data)
        assertEquals("""{"text":"好"}""", ev[1].data)
    }

    @Test fun `注释行与心跳忽略`() {
        val ev = parse(
            ": keep-alive",
            "event: answer.delta",
            """data: {"text":"hi"}""",
            "",
        )
        assertEquals(1, ev.size)
        assertEquals("answer.delta", ev[0].event)
    }

    @Test fun `多行data拼接换行`() {
        val ev = parse(
            "event: proposal",
            "data: line1",
            "data: line2",
            "",
        )
        assertEquals(1, ev.size)
        assertEquals("line1\nline2", ev[0].data)
    }

    @Test fun `无event行默认message`() {
        val ev = parse(
            """data: {"x":1}""",
            "",
        )
        assertEquals("message", ev[0].event)
    }

    @Test fun `流末无空行仍收尾`() {
        val ev = parse(
            "event: turn.end",
            """data: {"stop_reason":"done"}""",
        )
        assertEquals(1, ev.size)
        assertEquals("turn.end", ev[0].event)
    }

    @Test fun `空流无事件`() {
        assertEquals(0, parse("").size)
    }

    @Test fun `连续空行不产生空事件`() {
        val ev = parse(
            "event: a",
            """data: {}""",
            "",
            "",
            "",
            "event: b",
            """data: {}""",
            "",
        )
        assertEquals(2, ev.size)
    }
}
