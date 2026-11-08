package com.bookandmusic.lovelife.appnative.util

import java.time.LocalDate
import java.time.LocalDateTime
import java.time.ZoneId
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter

/**
 * 库内时间串的解析与展示：服务端统一写 UTC，
 * 形如 "2026-10-17 15:16:22" 且不带时区；直接按本地解析会整体慢 8 小时，
 * 故先按 UTC 解释，再换算到设备本地时区展示。
 */
object TimeFmt {

    private val dbFmt = DateTimeFormatter.ofPattern("yyyy-MM-dd HH:mm:ss")
    private val dbMinuteFmt = DateTimeFormatter.ofPattern("yyyy-MM-dd HH:mm")
    private val dateFmt = DateTimeFormatter.ofPattern("yyyy-MM-dd")

    /** 解析库内时间串为本地 LocalDateTime；失败返回 null */
    fun parse(raw: String?): LocalDateTime? {
        val s = raw?.trim().orEmpty()
        if (s.isEmpty()) return null
        return runCatching {
            val normalized = s.replace('T', ' ')
            val local = when {
                normalized.length >= 19 -> LocalDateTime.parse(normalized.take(19), dbFmt)
                normalized.length >= 16 -> LocalDateTime.parse(normalized, dbMinuteFmt)
                else -> LocalDateTime.parse("${normalized.take(10)} 00:00:00", dbFmt)
            }
            local.atOffset(ZoneOffset.UTC).atZoneSameInstant(ZoneId.systemDefault()).toLocalDateTime()
        }.getOrNull()
    }

    /** "YYYY-MM-DD · HH:mm"（本地时区） */
    fun dateTime(raw: String?): String {
        val d = parse(raw) ?: return raw?.take(16)?.replace('T', ' ')?.replace(' ', '·') ?: ""
        return "${d.format(dateFmt)} · ${two(d.hour)}:${two(d.minute)}"
    }

    /** "YYYY-MM-DD"（本地时区） */
    fun date(raw: String?): String {
        val d = parse(raw) ?: return raw?.take(10) ?: ""
        return d.format(dateFmt)
    }

    /** "2026.10.23"（头卡「在一起 · 2023.04.29 开始」的写法） */
    fun dotted(date: String?): String = (date ?: "").replace("-", ".")

    /** 月份键 "YYYY-MM" → "2026年10月" */
    fun monthLabel(key: String): String {
        if (key.length < 7) return "其他"
        val y = key.substring(0, 4).toIntOrNull() ?: return key
        val m = key.substring(5, 7).toIntOrNull() ?: return key
        return "${y}年${m}月"
    }

    /** 相对今天的天数差（正 = 未来） */
    fun daysFromToday(date: LocalDate): Long =
        java.time.temporal.ChronoUnit.DAYS.between(LocalDate.now(), date)

    private fun two(n: Int): String = n.toString().padStart(2, '0')
}
