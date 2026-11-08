package com.bookandmusic.lovelife.appnative

import com.bookandmusic.lovelife.appnative.data.AlbumsRepository
import com.bookandmusic.lovelife.appnative.data.ApiClient
import com.bookandmusic.lovelife.appnative.data.ApiError
import com.bookandmusic.lovelife.appnative.data.ChatRepository
import com.bookandmusic.lovelife.appnative.data.MapRepository
import com.bookandmusic.lovelife.appnative.data.MediaRepository
import com.bookandmusic.lovelife.appnative.ui.groupedSessions
import com.bookandmusic.lovelife.appnative.ui.isMediaContentType
import com.bookandmusic.lovelife.appnative.util.formatCoord
import java.io.File
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter
import java.time.temporal.ChronoUnit
import java.util.Locale
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * 页面边界的守卫（这些坑都是「看起来正常、换台设备/换个环境就出错」的一类）：
 * 坐标格式化随语言变、保存到本机把登录页当媒体、会话时间按 UTC 日期分组、
 * 时间轴单页大小超过服务端硬上限导致 has_more 永远为 false。
 */
class AppFixGuardTest {

    private val utcFmt = DateTimeFormatter.ofPattern("yyyy-MM-dd HH:mm:ss")

    private fun utcDaysAgo(days: Long): String =
        Instant.now().minus(days, ChronoUnit.DAYS).atOffset(ZoneOffset.UTC).format(utcFmt)

    /** 指定本地日期的正午换成 UTC 串：正午跨时区也落回同一天，分组月份不随设备时区漂 */
    private fun utcNoonOf(date: LocalDate): String =
        date.atTime(12, 0)
            .atZone(ZoneId.systemDefault())
            .withZoneSameInstant(ZoneOffset.UTC)
            .format(utcFmt)

    @Test
    fun `坐标格式与系统语言无关`() {
        val old = Locale.getDefault()
        try {
            Locale.setDefault(Locale.GERMANY)
            assertEquals("31.230400", formatCoord(31.2304))
            assertEquals("121.473700", formatCoord(121.4737))
            assertEquals("31.23040", formatCoord(31.2304, 5))
        } finally {
            Locale.setDefault(old)
        }
    }

    @Test
    fun `保存到本机只接受媒体类型`() {
        assertTrue(isMediaContentType("image/jpeg"))
        assertTrue(isMediaContentType("video/mp4"))
        assertTrue(isMediaContentType("audio/mp4; charset=binary"))
        // 通道 302 到登录页时拿到的是 HTML：不能当成「已保存」
        assertFalse(isMediaContentType("text/html; charset=utf-8"))
        assertFalse(isMediaContentType("application/json"))
        assertFalse(isMediaContentType(""))
    }

    @Test
    fun `会话分组按置顶-今天-7天内-月份-更早且不漏不重`() {
        // 月份固定在两年前：始终落在「更早」那一侧，且 10 月与 9 月相邻——
        // 拿展示串「2026年9月」排序会把 9 月排到 10 月前面（正是要守的坑）。
        val year = LocalDate.now().year - 2
        fun monthAt(month: Int): String = utcNoonOf(LocalDate.of(year, month, 10))

        val sessions = listOf(
            ChatRepository.SessionInfo(id = 1, title = "置顶的", updated_at = monthAt(12), pinned = true),
            ChatRepository.SessionInfo(id = 2, title = "今天的", updated_at = utcDaysAgo(0)),
            ChatRepository.SessionInfo(id = 3, title = "三天前", updated_at = utcDaysAgo(3)),
            ChatRepository.SessionInfo(id = 4, title = "十月", updated_at = monthAt(10)),
            ChatRepository.SessionInfo(id = 5, title = "九月", updated_at = monthAt(9)),
            ChatRepository.SessionInfo(id = 6, title = "七月", updated_at = monthAt(7)),
            ChatRepository.SessionInfo(id = 7, title = "三月", updated_at = monthAt(3)),
        )
        val groups = groupedSessions(sessions)
        val labels = groups.map { it.first }

        // (a) 组标签全集与顺序：月份组只留最近两个，其余并入「更早」
        assertEquals(
            listOf("置顶", "今天", "7天内", "${year}年10月", "${year}年9月", "更早"),
            labels,
        )
        // (c) 10 月必须排在 9 月前面
        assertTrue(
            "10 月应排在 9 月前面，实际顺序：$labels",
            labels.indexOf("${year}年10月") < labels.indexOf("${year}年9月"),
        )
        assertEquals(listOf(1L), groups[0].second.map { it.id })
        assertEquals(listOf(2L), groups[1].second.map { it.id })
        assertEquals(listOf(3L), groups[2].second.map { it.id })
        assertEquals(listOf(4L), groups[3].second.map { it.id })
        assertEquals(listOf(5L), groups[4].second.map { it.id })
        // 未进前两个月份的会话必须落到「更早」，不能凭空消失
        assertEquals(listOf(6L, 7L), groups[5].second.map { it.id })

        // (b) 每个会话恰好归一组：并集 = 原列表，且条数相等（无遗漏、无重复）
        val ids = groups.flatMap { g -> g.second.map { it.id } }
        assertEquals(sessions.size, ids.size)
        assertEquals(sessions.map { it.id }.toSet(), ids.toSet())
    }

    @Test
    fun `时间轴单页大小不超过服务端硬上限`() {
        // 服务端 size>50 会被静默夹到 50；客户端若声明更大，has_more 判定会失真
        assertEquals(50, AlbumsRepository.TIMELINE_PAGE_SIZE)
    }

    @Test
    fun `ui 源码里没有随系统语言变形的数字格式`() {
        // "%.3f".format(v) 走默认 Locale：德语/阿语环境会写出 "31,230"，坐标必须走 formatCoord
        val rawPercentFormat = Regex("\"%\\.[0-9]+f\"\\.format\\(")
        val offenders = uiSourceDir().walkTopDown()
            .filter { it.isFile && it.extension == "kt" }
            .flatMap { f ->
                f.readLines().withIndex()
                    .filter { (_, line) -> rawPercentFormat.containsMatchIn(line) }
                    .map { (i, line) -> "${f.name}:${i + 1} ${line.trim()}" }
            }
            .toList()
        assertEquals("数字格式化应走 Locale.US（formatCoord）", emptyList<String>(), offenders)
    }

    @Test
    fun `保存到本机不给下载设总时长上限`() {
        // 单文件上限 500MB，慢网下必然超过一分钟；设总时长会把大文件判成失败，还会删掉已建文档
        val src = findSource(
            "src/main/java/com/bookandmusic/lovelife/appnative/ui/MediaSave.kt",
            "app/app/src/main/java/com/bookandmusic/lovelife/appnative/ui/MediaSave.kt",
        ).readText()
        assertFalse("下载不应设 callTimeout", src.contains(".callTimeout("))
        assertTrue("连接与单次读取仍要限时", src.contains(".connectTimeout(") && src.contains(".readTimeout("))
    }

    @Test
    fun `勾选行带复选框语义与设计档位圆角`() {
        val src = findSource(
            "core/designsystem/src/main/java/com/bookandmusic/lovelife/appnative/designsystem/atoms/AppFieldRow.kt",
            "app/core/designsystem/src/main/java/com/bookandmusic/lovelife/appnative/designsystem/atoms/AppFieldRow.kt",
        ).readText()
        // 只有 clickable + 装饰图标时，读屏读不出「勾选/未勾选」
        assertTrue("勾选行应有复选框角色", src.contains("role = Role.Checkbox"))
        assertTrue("勾选行应播报勾选状态", src.contains("stateDescription"))
        assertFalse("圆角不在设计档位（2/8/12/20/999）", src.contains("RoundedCornerShape(5.dp)"))
    }

    @Test
    fun `空响应只有 Unit 调用合法`() {
        // 204/空体：Unit 调用合法；集合/对象必须在解码前给可读文案，而不是 ClassCastException
        assertEquals(Unit, ApiClient.emptyBodyResult(Unit::class))
        val e = assertThrows(ApiError::class.java) { ApiClient.emptyBodyResult(List::class) }
        assertTrue("报错要说明缺了什么：${e.message}", e.message.orEmpty().contains("服务端没有返回内容"))
        assertEquals(0, e.status)
    }

    @Test
    fun `会话失效事件可由登录成功复位`() {
        ApiClient.markSessionRestored()
        assertFalse(ApiClient.sessionExpired.value)
        ApiClient.markSessionExpired()
        assertTrue("401 清会话后必须发全局事件，否则用户只能手动退出", ApiClient.sessionExpired.value)
        ApiClient.markSessionRestored()
        assertFalse("重新登录后事件要复位", ApiClient.sessionExpired.value)
    }

    @Test
    fun `媒体上传复用单例 client 且显式超时`() {
        val src = mainSource("data/MediaRepository.kt").readText()
        assertTrue("上传 client 应是进程级单例", src.contains("private val http: OkHttpClient by lazy"))
        assertTrue("连接要限时", src.contains(".connectTimeout("))
        assertTrue("读取要限时", src.contains(".readTimeout("))
        assertTrue("写入要限时", src.contains(".writeTimeout("))
        assertFalse("上传路径不应每次新建 client", src.contains("okhttp3.OkHttpClient.Builder().build()"))
    }

    @Test
    fun `临时媒体撤销按 tokens 数组上报`() {
        // 服务端 discardMedia 收 {tokens:[…]}；单次上限 100（超了 400）
        assertEquals("""{"tokens":["a","b"]}""", MediaRepository.discardBody(listOf("a", "b")))
        val body = MediaRepository.discardBody((1..150).map { "t$it" })
        assertEquals(MediaRepository.MAX_DISCARD_TOKENS, Regex(""""t[0-9]+"""").findAll(body).count())
    }

    @Test
    fun `地图配置首拉失败走短退避`() {
        // 首拉失败不能等一个换签周期（20 分钟），否则页面一直停在「地图加载中…」
        assertTrue(
            "首拉失败重试间隔应在 5–10s",
            MapRepository.nextConfigDelayMs(hasConfig = false, expSeconds = 0) in 5_000..10_000,
        )
        // 已拿到配置后仍按 exp 换签，不缩成秒级轮询
        assertTrue(MapRepository.nextConfigDelayMs(hasConfig = true, expSeconds = 0) >= 60_000)
    }

    @Test
    fun `会话轮次 id 收进页面状态`() {
        val src = mainSource("ui/ChatScreen.kt").readText()
        // 文件级可变单例会跨会话残留：切会话后「停止生成」会 abort 上一轮
        assertFalse("轮次 id 不能是文件级单例", Regex("""(?m)^private var currentTurnId""").containsMatchIn(src))
        assertTrue(src.contains("var currentTurnId by remember { mutableStateOf<String?>(null) }"))
        val switchBody = Regex("""fun switchSession\(id: Long\) \{[\s\S]*?\n    \}""").find(src)?.value.orEmpty()
        assertTrue("切换会话必须清空轮次 id", switchBody.contains("currentTurnId = null"))
        val newBody = Regex("""fun newSession\(\) \{[\s\S]*?\n    \}""").find(src)?.value.orEmpty()
        assertTrue("新建会话必须清空轮次 id", newBody.contains("currentTurnId = null"))
    }

    @Test
    fun `发布页媒体条目字段只读`() {
        val src = mainSource("ui/FormScreens.kt").readText()
        val picked = Regex("""private data class PickedMedia\([\s\S]*?\n\)""").find(src)?.value.orEmpty()
        assertTrue(picked.isNotEmpty())
        // 状态只经 copy() 整体替换；var 字段意味着有人会就地改列表项（重组不感知）
        assertFalse("PickedMedia 字段应为 val：$picked", picked.contains("var "))
    }

    @Test
    fun `关于页项目主页只存在一个常量`() {
        // 静态关于页外链不是数据通道：不随环境变化、不经服务端下发；显示与打开共用同一常量
        val src = mainSource("ui/AboutScreen.kt").readText()
        assertEquals(
            "项目主页地址只应出现在一个常量里",
            1,
            Regex("""github\.com/bookandmusic/love-life""").findAll(src).count(),
        )
    }

    /** 主源码文件（:app 模块内相对路径，如 ui/ChatScreen.kt） */
    private fun mainSource(relative: String): File {
        val path = "src/main/java/com/bookandmusic/lovelife/appnative/$relative"
        return findSource(path, "app/app/$path")
    }

    /** ui 源码目录：单测工作目录是模块目录（app/app），也兼容从仓库根或 app 目录跑 */
    private fun uiSourceDir(): File {
        val relative = "src/main/java/com/bookandmusic/lovelife/appnative/ui"
        return findRelative(relative, "app/app/$relative")
    }

    private fun findSource(vararg relatives: String): File {
        var dir: File? = File("").absoluteFile
        while (dir != null) {
            for (rel in relatives) {
                val f = File(dir, rel)
                if (f.isFile) return f
            }
            dir = dir.parentFile
        }
        throw IllegalStateException("找不到源码：${relatives.joinToString()}（工作目录 ${File("").absolutePath}）")
    }

    private fun findRelative(vararg relatives: String): File {
        var dir: File? = File("").absoluteFile
        while (dir != null) {
            for (rel in relatives) {
                val f = File(dir, rel)
                if (f.isDirectory) return f
            }
            dir = dir.parentFile
        }
        throw IllegalStateException("找不到源码目录：${relatives.joinToString()}（工作目录 ${File("").absolutePath}）")
    }
}
