package com.bookandmusic.lovelife.appnative

import com.bookandmusic.lovelife.appnative.data.absoluteUrl
import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * 媒体地址绝对化：服务端只下发相对路径（`/api/v1/media/file/13?v=thumb&…`），
 * 客户端必须补上当前通道的 base，否则 Coil 加载不出来（漏掉时结果卡整片空白）。
 */
class ChannelUrlTest {

    @Test fun `relative path gets base`() {
        assertEquals(
            "http://192.0.2.10:8083/api/v1/media/file/13?v=thumb&exp=1&sig=x",
            absoluteUrl("http://192.0.2.10:8083", "/api/v1/media/file/13?v=thumb&exp=1&sig=x"),
        )
    }

    @Test fun `base trailing slash and path without slash both fine`() {
        assertEquals("http://h:1/a/b", absoluteUrl("http://h:1/", "a/b"))
        assertEquals("http://h:1/a/b", absoluteUrl("http://h:1", "/a/b"))
    }

    @Test fun `absolute urls untouched`() {
        assertEquals("https://cdn/x.jpg", absoluteUrl("http://h:1", "https://cdn/x.jpg"))
        assertEquals("http://other/x", absoluteUrl("http://h:1", "http://other/x"))
    }

    @Test fun `protocol relative becomes https`() {
        assertEquals("https://cdn/x.jpg", absoluteUrl("http://h:1", "//cdn/x.jpg"))
    }

    @Test fun `blank stays blank`() {
        assertEquals("", absoluteUrl("http://h:1", ""))
    }
}
