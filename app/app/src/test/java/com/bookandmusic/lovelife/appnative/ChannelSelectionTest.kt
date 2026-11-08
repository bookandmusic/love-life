package com.bookandmusic.lovelife.appnative

import com.bookandmusic.lovelife.appnative.data.Channel
import com.bookandmusic.lovelife.appnative.data.Channel.Candidate
import com.bookandmusic.lovelife.appnative.data.Channel.Kind
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * 选路的排序契约（`Channel.merged`）——APP 拿到候选就按这个顺序探活，
 * 顺序错了「权重高优先」这条需求就失效，而且症状是静默的（连上了但不该是这条）。
 */
class ChannelSelectionTest {

    private fun builtin(url: String = "http://192.0.2.10:8083") =
        Candidate(Kind.BUILTIN, url, Channel.BUILTIN_WEIGHT)

    private fun backup(url: String, weight: Int) = Candidate(Kind.BACKUP, url, weight)

    private fun uglink(url: String, weight: Int, cookie: String = "c=1") =
        Candidate(Kind.UGLINK, url, weight, cookie = cookie)

    @Test
    fun higherWeightComesFirst() {
        val out = Channel.merged(
            builtinWeight = 100,
            builtin = "http://192.0.2.10:8083",
            backend = listOf(backup("https://low.example.com", 10), backup("https://high.example.com", 80)),
        )
        assertEquals(
            listOf("http://192.0.2.10:8083", "https://high.example.com", "https://low.example.com"),
            out.map { it.url },
        )
    }

    // 内置权重恒 100：后端配得比它高时才排前面——这正是「部署者判断」的意义。
    @Test
    fun backendCanOutrankBuiltin() {
        val out = Channel.merged(
            builtinWeight = 100,
            builtin = "http://192.0.2.10:8083",
            backend = listOf(uglink("https://ug.example.com", 120)),
        )
        assertEquals("https://ug.example.com", out.first().url)
    }

    // 同权重时内置在前面（稳定排序 + 先加入）：直连少一跳，不该被同权重的中转挤后。
    @Test
    fun sameWeightKeepsBuiltinFirst() {
        val out = Channel.merged(
            builtinWeight = 50,
            builtin = "http://192.0.2.10:8083",
            backend = listOf(uglink("https://ug.example.com", 50)),
        )
        assertEquals(Kind.BUILTIN, out.first().kind)
    }

    @Test
    fun builtinSkippedWhenBlank() {
        val out = Channel.merged(
            builtinWeight = 100,
            builtin = "",
            backend = listOf(backup("https://only.example.com", 10)),
        )
        assertEquals(listOf("https://only.example.com"), out.map { it.url })
    }

    @Test
    fun builtinTrailingSlashNormalized() {
        val out = Channel.merged(
            builtinWeight = 100,
            builtin = "http://192.0.2.10:8083/",
            backend = emptyList(),
        )
        assertEquals("http://192.0.2.10:8083", out.single().url)
    }

    // cookie 只随绿联通道：其余通道带了 cookie 就会被当成中转会话发出去。
    @Test
    fun onlyUglinkCarriesCookie() {
        val out = Channel.merged(
            builtinWeight = 100,
            builtin = "http://192.0.2.10:8083",
            backend = listOf(
                uglink("https://ug.example.com", 80, cookie = "session=abc"),
                backup("https://bu.example.com", 60),
            ),
        )
        val ug = out.first { it.kind == Kind.UGLINK }
        val bu = out.first { it.kind == Kind.BACKUP }
        assertEquals("session=abc", ug.cookie)
        assertEquals("", bu.cookie)
        assertTrue(out.first { it.kind == Kind.BUILTIN }.cookie.isEmpty())
    }

    @Test
    fun emptyEverythingStillYieldsBuiltin() {
        val out = Channel.merged(builtinWeight = 100, builtin = "http://192.0.2.10:8083", backend = emptyList())
        assertEquals(1, out.size)
        assertEquals(Kind.BUILTIN, out.single().kind)
    }
}