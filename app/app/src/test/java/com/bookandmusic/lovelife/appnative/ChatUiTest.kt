package com.bookandmusic.lovelife.appnative

import com.bookandmusic.lovelife.appnative.data.ChatRepository
import com.bookandmusic.lovelife.appnative.data.ChatUi
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * 确认卡预览的解析（交互卡与结果卡共用一套 PreviewView 形态词汇）：
 * 各意图的 body 摊平成 text / lines / change / media / items 的段序列。
 */
class ChatUiTest {

    private fun preview(kind: String, body: String): ChatUi.CardPreview =
        ChatUi.parsePreview(
            ChatRepository.Preview(kind = kind, body = Json.parseToJsonElement(body)),
        )

    @Test
    fun `动态预览 - 正文 + 元信息 + 媒体数量`() {
        val p = preview(
            "post",
            """{"content":"今天的日落很好看","mood":"开心","place":"西湖","place_city":"杭州","attachments":[{"id":1},{"id":2}]}""",
        )
        assertEquals("post", p.kind)
        assertEquals(3, p.views.size)
        assertEquals("今天的日落很好看", (p.views[0] as ChatUi.PreviewView.Text).text)
        val media = p.views[1] as ChatUi.PreviewView.Media
        assertEquals(2, media.count)
        assertTrue("只有数量、没有缩略图时不占位", media.media.isEmpty())
        val lines = (p.views[2] as ChatUi.PreviewView.Lines).lines
        assertEquals(listOf("心情", "地点"), lines.map { it.label })
        assertEquals("西湖 · 杭州", lines[1].value)
    }

    @Test
    fun `相册预览 - 正文 + 元信息 + 缩略图`() {
        val p = preview(
            "album",
            """{"name":"旅行","description":"去年夏天","count":3,"media":[{"id":9,"thumb_url":"u9"}]}""",
        )
        assertEquals("旅行", p.views.filterIsInstance<ChatUi.PreviewView.Text>().single().text)
        val lines = p.views.filterIsInstance<ChatUi.PreviewView.Lines>().single().lines
        assertEquals("3 张", lines.first { it.label == "收录" }.value)
        val media = p.views.filterIsInstance<ChatUi.PreviewView.Media>().single()
        assertEquals(1, media.media.size)
        assertEquals("u9", media.media[0].thumb_url)
        // 段序与阅读顺序一致：正文 → 媒体 → 元信息
        assertTrue(p.views[0] is ChatUi.PreviewView.Text)
        assertTrue(p.views[1] is ChatUi.PreviewView.Media)
        assertTrue(p.views[2] is ChatUi.PreviewView.Lines)
    }

    @Test
    fun `删除预览 - 批量条目带总数`() {
        val p = preview(
            "delete",
            """{"items":[{"id":1,"text":"a"},{"id":2,"text":"b"}],"count":2}""",
        )
        val items = p.views.single() as ChatUi.PreviewView.Items
        assertEquals(2, items.items.size)
        assertEquals(2, items.count)
    }

    @Test
    fun `无预览载荷时没有段`() {
        assertTrue(ChatUi.parsePreview(null).views.isEmpty())
        assertTrue(ChatUi.parsePreview(ChatRepository.Preview(kind = "post")).views.isEmpty())
    }
}
