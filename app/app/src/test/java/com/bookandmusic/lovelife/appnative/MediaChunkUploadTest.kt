package com.bookandmusic.lovelife.appnative

import com.bookandmusic.lovelife.appnative.data.MediaRepository
import com.bookandmusic.lovelife.appnative.data.UploadedMedia
import kotlinx.coroutines.runBlocking
import java.io.IOException
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

/**
 * 分片上传推进：服务端回的 received 是**唯一**推进依据（不是本地累加），
 * 所以「响应丢了但字节已落盘」要靠 409 重同步纠正。这里把网络换成假实现，逐条钉住行为。
 */
class MediaChunkUploadTest {

    private val media = UploadedMedia(media_token = "tok", kind = "photo", size = 10)

    @Test fun `threeChunksInOrder`() = runBlocking {
        val sent = mutableListOf<Triple<Long, Int, Boolean>>()
        var received = 0L
        val out = MediaRepository.uploadChunks(
            size = 10, chunkBytes = 4,
            read = { offset, len -> ByteArray(len) { (offset + it).toByte() } },
            send = { offset, bytes, isFinal, _ ->
                sent += Triple(offset, bytes.size, isFinal)
                received += bytes.size
                if (isFinal) MediaRepository.ChunkResult.Done(media)
                else MediaRepository.ChunkResult.Ack("up-1", received)
            },
        )
        assertEquals("tok", out.media_token)
        assertEquals(listOf(Triple(0L, 4, false), Triple(4L, 4, false), Triple(8L, 2, true)), sent)
    }

    @Test fun `conflictResyncsFromServerOffset`() = runBlocking {
        val offsets = mutableListOf<Long>()
        var received = 0L
        var first = true
        val out = MediaRepository.uploadChunks(
            size = 10, chunkBytes = 4,
            read = { _, len -> ByteArray(len) },
            send = { offset, bytes, isFinal, _ ->
                offsets += offset
                if (first) {
                    // 客户端以为从 0 开始，服务端说已经收了 8 字节（上一次响应丢了）
                    first = false
                    received = 8
                    MediaRepository.ChunkResult.Conflict(8)
                } else {
                    received += bytes.size
                    if (isFinal) MediaRepository.ChunkResult.Done(media)
                    else MediaRepository.ChunkResult.Ack("up-1", received)
                }
            },
        )
        assertEquals("tok", out.media_token)
        // 0（被 409 打回）→ 8（剩下的 2 字节就是末片，不必再切一片）
        assertEquals(listOf(0L, 8L), offsets)
    }

    @Test fun `noProgressFails`() = runBlocking {
        try {
            MediaRepository.uploadChunks(
                size = 10, chunkBytes = 4,
                read = { _, len -> ByteArray(len) },
                // 服务端回了 ack 但 received 没动 → 不能死循环
                send = { _, _, _, _ -> MediaRepository.ChunkResult.Ack("up-1", 0) },
            )
            fail("应抛异常")
        } catch (e: IllegalStateException) {
            assertTrue(e.message!!.contains("没有推进"))
        }
    }

    @Test fun `emptyFileRejected`() = runBlocking {
        try {
            MediaRepository.uploadChunks(
                size = 0, chunkBytes = 4,
                read = { _, len -> ByteArray(len) },
                send = { _, _, _, _ -> MediaRepository.ChunkResult.Done(media) },
            )
            fail("应抛异常")
        } catch (e: IllegalArgumentException) {
            assertTrue(e.message!!.contains("空"))
        }
    }

    @Test fun `transientIoErrorRetriesSameChunk`() = runBlocking {
        val attempts = mutableListOf<Long>()
        var received = 0L
        var failedOnce = false
        val out = MediaRepository.uploadChunks(
            size = 10, chunkBytes = 4,
            read = { offset, len -> ByteArray(len) { (offset + it).toByte() } },
            send = { offset, bytes, isFinal, _ ->
                attempts += offset
                if (offset == 4L && !failedOnce) {
                    failedOnce = true
                    throw IOException("connection reset")
                }
                received += bytes.size
                if (isFinal) MediaRepository.ChunkResult.Done(media)
                else MediaRepository.ChunkResult.Ack("up-1", received)
            },
        )
        assertEquals("tok", out.media_token)
        // 第 2 片（offset=4）失败一次后用同一 offset 重发；第 1、3 片各发一次
        assertEquals(listOf(0L, 4L, 4L, 8L), attempts)
    }

    @Test fun `persistentIoErrorGivesUpAfterRetries`() = runBlocking {
        var calls = 0
        try {
            MediaRepository.uploadChunks(
                size = 10, chunkBytes = 4,
                read = { offset, len -> ByteArray(len) { (offset + it).toByte() } },
                send = { _, _, _, _ ->
                    calls++
                    throw IOException("network down")
                },
            )
            fail("持续网络错误应抛异常")
        } catch (e: IOException) {
            assertEquals(4, calls) // 首次 + 3 次重试
        }
    }
}
