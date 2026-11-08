package com.bookandmusic.lovelife.appnative

import com.bookandmusic.lovelife.appnative.update.UpdateDownloader
import java.io.EOFException
import java.io.IOException
import java.net.SocketTimeoutException
import java.net.UnknownHostException
import org.junit.Assert.assertFalse
import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * 失败文案必须是人话。
 *
 * 真机上报过一次弹窗显示 `Software caused connection abort`——那是 socket 层原文，
 * 用户看不懂，而且看着像崩溃。这里钉住「常见异常都有中文对应、任何情况下都不回显英文」。
 */
class UpdateErrorMessageTest {

    private fun err(e: Throwable): String = UpdateDownloader.friendlyError(e)

    private fun assertChinese(text: String) {
        assertFalse("失败文案里不能出现英文原文：$text", Regex("[A-Za-z]{4,}").containsMatchIn(text))
    }

    @Test
    fun connectionAbortReadsAsChinese() {
        val text = err(IOException("Software caused connection abort"))
        assertChinese(text)
        assertEquals("网络连接被中断", text)
    }

    @Test
    fun timeoutAndDnsAreDistinguished() {
        val timeout = err(SocketTimeoutException("timeout"))
        val dns = err(UnknownHostException("example.com"))
        assertChinese(timeout)
        assertChinese(dns)
        assertEquals("连接超时，请稍后重试", timeout)
        assertEquals("网络不可用，请检查连接", dns)
    }

    @Test
    fun truncatedStreamSaysDownloadInterrupted() {
        val text = err(EOFException("unexpected end of stream"))
        assertChinese(text)
        assertEquals("下载中断，请重试", text)
    }

    @Test
    fun plainIoAndUnknownExceptionsStillReadable() {
        assertChinese(err(IOException("broken pipe")))
        assertChinese(err(IOException()))
        assertChinese(err(IllegalStateException("something weird happened")))
        assertEquals("下载失败，请重试", err(IllegalStateException("something weird happened")))
    }

    @Test
    fun everyMappingIsFreeOfEnglish() {
        val samples = listOf(
            IOException("Software caused connection abort"),
            IOException("Connection reset by peer"),
            IOException("Connection refused"),
            SocketTimeoutException("Read timed out"),
            UnknownHostException("unable to resolve host"),
            EOFException("unexpected end of stream"),
            javax.net.ssl.SSLHandshakeException("certificate verify failed"),
            IOException(),
            RuntimeException("x"),
        )
        samples.forEach { assertChinese(err(it)) }
    }
}