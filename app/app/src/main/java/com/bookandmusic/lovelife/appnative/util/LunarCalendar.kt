package com.bookandmusic.lovelife.appnative.util

import java.time.LocalDate

/**
 * 内置离线农历换算（1900–2100，表驱动经典算法）。周期表与服务端同源（LunarTable.kt）。
 *
 * 闰月语义与服务端一致：闰月排在同号平月之后；isLeap 由调用方明确传入，
 * 不做「平月查不到退闰月」的兜底（会让同一纪念日一年命中两次）。
 */
object LunarCalendar {

    private const val BASE_YEAR = 1900

    /** 农历年总天数 */
    private fun yearDays(y: Int): Int {
        var sum = 348
        var i = 0x8000
        while (i > 0x8) {
            if (LUNAR_INFO[y - BASE_YEAR] and i != 0) sum++
            i = i shr 1
        }
        return sum + leapDays(y)
    }

    /** 该农历年的闰月（1-12；0 无闰） */
    fun leapMonth(y: Int): Int = LUNAR_INFO[y - BASE_YEAR] and 0xf

    /** 农历年闰月的天数（无闰返回 0） */
    private fun leapDays(y: Int): Int {
        val lm = leapMonth(y)
        if (lm == 0) return 0
        return if (LUNAR_INFO[y - BASE_YEAR] and 0x10000 != 0) 30 else 29
    }

    /** 农历平月天数（大月 30，小月 29） */
    fun monthDays(y: Int, m: Int): Int =
        if (LUNAR_INFO[y - BASE_YEAR] and (0x10000 shr m) != 0) 30 else 29

    /** 农历某年闰月的天数（对闰月行公开：日历格数要用它） */
    fun leapMonthDays(y: Int): Int = leapDays(y)

    /** 农历 → 公历；非法（越界 / 小月三十 / 平年闰月）返回 null */
    fun lunarToSolar(y: Int, m: Int, d: Int, isLeap: Boolean): LocalDate? {
        if (y < BASE_YEAR || y > BASE_YEAR + LUNAR_INFO.size - 1) return null
        if (m !in 1..12) return null
        if (d < 1) return null
        val lm = leapMonth(y)
        if (isLeap && lm != m) return null
        if (!isLeap && d > monthDays(y, m)) return null
        if (isLeap && d > leapDays(y)) return null
        var offset = 0
        for (i in BASE_YEAR until y) offset += yearDays(i)
        for (i in 1 until m) {
            offset += monthDays(y, i)
            if (i == lm) offset += leapDays(y)
        }
        // 闰 m 月排在平 m 月之后：先加平月 m 整月，d-1 自然落在闰月内
        if (isLeap) offset += monthDays(y, m)
        offset += d - 1
        return LocalDate.of(1900, 1, 31).plusDays(offset.toLong())
    }

    /**
     * 公历 → 农历（含闰月归属）。1900-01-31 之前 / 2100 表尾之后返回 null。
     * 返回 [LunarDate]；闰月由 isLeap 标记。
     */
    fun solarToLunar(date: LocalDate): LunarDate? {
        val base = LocalDate.of(1900, 1, 31)
        if (date.isBefore(base)) return null
        var offset = java.time.temporal.ChronoUnit.DAYS.between(base, date).toInt()
        var y = BASE_YEAR
        while (y <= BASE_YEAR + LUNAR_INFO.size - 1) {
            val yd = yearDays(y)
            if (offset < yd) break
            offset -= yd
            y++
        }
        if (y > BASE_YEAR + LUNAR_INFO.size - 1) return null
        val lm = leapMonth(y)
        var m = 1
        var isLeap = false
        while (m <= 12) {
            var md = monthDays(y, m)
            var leapHere = false
            if (lm == m) {
                // 平 m 月在前，闰 m 月在后
                if (offset < md) {
                    break
                }
                offset -= md
                md = leapDays(y)
                leapHere = true
                if (offset < md) {
                    isLeap = true
                    break
                }
                offset -= md
                m++
                continue
            }
            if (offset < md) break
            offset -= md
            m++
            if (leapHere) break
        }
        if (m > 12) return null
        val d = offset + 1
        return LunarDate(year = y, month = m, day = d, isLeap = isLeap)
    }

    /** 天干地支年（如「丙午」） */
    fun yearGanZhi(y: Int): String {
        val gan = listOf("甲", "乙", "丙", "丁", "戊", "己", "庚", "辛", "壬", "癸")
        val zhi = listOf("子", "丑", "寅", "卯", "辰", "巳", "午", "未", "申", "酉", "戌", "亥")
        return gan[(y - 4) % 10] + zhi[(y - 4) % 12]
    }
}

/** 农历日期（month 1-12；闰月用 isLeap 标记，公历日期由 LunarCalendar.lunarToSolar 换算） */
data class LunarDate(val year: Int, val month: Int, val day: Int, val isLeap: Boolean)

/** 农历日名（初一…三十） */
fun lunarDayName(d: Int): String = listOf(
    "初一", "初二", "初三", "初四", "初五", "初六", "初七", "初八", "初九", "初十",
    "十一", "十二", "十三", "十四", "十五", "十六", "十七", "十八", "十九", "二十",
    "廿一", "廿二", "廿三", "廿四", "廿五", "廿六", "廿七", "廿八", "廿九", "三十",
).getOrElse(d - 1) { "$d" }

/** 农历月名（正月…腊月；闰月前缀「闰」由调用方拼） */
fun lunarMonthName(m: Int): String = listOf(
    "正月", "二月", "三月", "四月", "五月", "六月",
    "七月", "八月", "九月", "十月", "冬月", "腊月",
).getOrElse(m - 1) { "$m 月" }
