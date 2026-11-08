package com.bookandmusic.lovelife.appnative

import com.bookandmusic.lovelife.appnative.update.UpdateDownloader
import com.bookandmusic.lovelife.appnative.update.UpdateDownloader.Action
import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * 续传状态机的分支判定（Range / 206 / 200 / 416）。
 *
 * 这三条分支错了都是**静默坏**：206 追加写错位置 = 包体错位、200 仍追加 = 文件变长、
 * 416 不删半成品 = 用户永远重试失败。它们不适合在真机上试，钉在单测里。
 */
class UpdateDownloaderTest {

    @Test
    fun partialDownloadWith206Appends() {
        assertEquals(Action.APPEND, UpdateDownloader.decide(206, 1024))
    }

    @Test
    fun noPartialWith206FallsBackToRestart() {
        // 服务端回了 206 但本地没有半成品：当全量写，不能 APPEND（会把偏移当已有长度）
        assertEquals(Action.RESTART, UpdateDownloader.decide(206, 0))
    }

    @Test
    fun ok200MeansServerIgnoredRangeSoRestart() {
        assertEquals(Action.RESTART, UpdateDownloader.decide(200, 1024))
        assertEquals(Action.RESTART, UpdateDownloader.decide(200, 0))
    }

    @Test
    fun rangeNotSatisfiableResetsPartial() {
        assertEquals(Action.RESET_AND_FAIL, UpdateDownloader.decide(416, 2048))
        // 416 但本地没有半成品（对端文件比我们发的 Range 还短）：同样按重置处理
        assertEquals(Action.RESET_AND_FAIL, UpdateDownloader.decide(416, 0))
    }

    @Test
    fun nonSuccessCodesAreNotHandledHere() {
        assertEquals(Action.OTHER, UpdateDownloader.decide(401, 1024))
        assertEquals(Action.OTHER, UpdateDownloader.decide(404, 0))
        assertEquals(Action.OTHER, UpdateDownloader.decide(500, 512))
    }
}