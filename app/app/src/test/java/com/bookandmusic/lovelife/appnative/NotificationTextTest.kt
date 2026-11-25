package com.bookandmusic.lovelife.appnative

import com.bookandmusic.lovelife.appnative.data.NotificationDto
import com.bookandmusic.lovelife.appnative.ui.notificationText
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * 通知列表文案：服务端下发的 `snapshot.text` 是唯一事实源，旧通知按事件类型回退拼接。
 * 两条路都必须有守卫——回退分支写错不会崩，只会让用户看到「你的动态」之类的错句子。
 */
class NotificationTextTest {

    private fun dto(event: String, snapshot: String) =
        NotificationDto(id = 1, event_type = event, snapshot = snapshot)

    @Test
    fun serverRenderedTextWins() {
        // 有 text 时不再看 event_type 与快照里的其它字段
        val got = notificationText(dto("post_liked", """{"text":"小六 赞了你的动态","operator":"ignored"}"""))
        assertEquals("小六 赞了你的动态", got)
    }

    @Test
    fun legacySnapshotFallsBackPerEvent() {
        assertEquals(
            "小六 评论了你的动态：真好看",
            notificationText(dto("post_commented", """{"operator":"小六","content":"真好看"}""")),
        )
        assertEquals(
            "小六 删除了你的相册「海边」",
            notificationText(dto("album_deleted", """{"deleted_by":"小六","name":"海边"}""")),
        )
    }

    @Test
    fun emptyTextAndEmptyContentLeaveNoTrailingColon() {
        // 空 text 走回退；纯媒体动态没有 content，句子不能以冒号结尾
        val got = notificationText(dto("post_created", """{"operator":"小六","content":""}"""))
        assertEquals("小六 发布了新动态", got)
        assertTrue(notificationText(dto("post_created", """{"operator":"小六","content":"晚霞"}"""))
            .endsWith("：晚霞"))
    }

    @Test
    fun unparsableSnapshotDoesNotCrash() {
        assertEquals("系统通知", notificationText(dto("unknown_event", "not-json")))
        assertEquals("对方 赞了你的动态", notificationText(dto("post_liked", "{}")))
    }
}
