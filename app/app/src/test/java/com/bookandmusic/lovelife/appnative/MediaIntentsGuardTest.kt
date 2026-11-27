package com.bookandmusic.lovelife.appnative

import com.bookandmusic.lovelife.appnative.ui.MEDIA_INTENTS
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * 服务端所有「确认后会把本轮媒体转正」的提案意图，都必须在这里登记。
 *
 * 漏登记的后果是静默的：用户在 AI 对话里附了图让 AI 去执行，卡片照常出、
 * 确认也成功，但图从来没被上传，服务端收到的是空 token 列表——用户以为发成功了。
 */
class MediaIntentsGuardTest {

    @Test
    fun `带媒体的提案意图都已登记`() {
        // 与服务端 tools.Registry 里 Attachments/媒体转正相关意图保持一致
        val mustBeRegistered = listOf(
            "propose_post",           // 发动态
            "propose_album",          // 建相册并收首批图
            "propose_album_members",  // 相册增删成员
            "propose_comment",        // 评论带图
        )
        for (intent in mustBeRegistered) {
            assertTrue("意图 $intent 未登记进 MEDIA_INTENTS，确认时会静默丢图", intent in MEDIA_INTENTS)
        }
    }

    @Test
    fun `不涉及媒体的意图不要误登记`() {
        // 登记错的后果是把无关媒体挂到不支持的提案上，服务端会拒
        val mustNotBeRegistered = listOf(
            "propose_anniversary",
            "propose_place",
            "propose_reaction",
            "propose_delete",
        )
        for (intent in mustNotBeRegistered) {
            assertTrue("意图 $intent 不该登记进 MEDIA_INTENTS", intent !in MEDIA_INTENTS)
        }
    }
}
