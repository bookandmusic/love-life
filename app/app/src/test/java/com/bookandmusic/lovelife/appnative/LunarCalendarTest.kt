package com.bookandmusic.lovelife.appnative

import java.time.LocalDate
import com.bookandmusic.lovelife.appnative.util.LunarCalendar
import com.bookandmusic.lovelife.appnative.util.LunarDate
import com.bookandmusic.lovelife.appnative.util.lunarDayName
import com.bookandmusic.lovelife.appnative.util.lunarMonthName
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * 农历换算锚点测试：与服务端 Go 侧同表同算法，锚点值可与中国农历公开日历互核。
 */
class LunarCalendarTest {

    @Test
    fun `2026 春节 = 正月初一`() {
        assertEquals(LocalDate.of(2026, 2, 17), LunarCalendar.lunarToSolar(2026, 1, 1, false))
    }

    @Test
    fun `2026 中秋 = 八月十五`() {
        assertEquals(LocalDate.of(2026, 9, 25), LunarCalendar.lunarToSolar(2026, 8, 15, false))
    }

    @Test
    fun `2025 闰六月存在且换算正确`() {
        assertEquals(6, LunarCalendar.leapMonth(2025))
        // 闰六月初一 = 2025-07-25（公历）；平六月初一 = 2025-06-25
        assertEquals(LocalDate.of(2025, 6, 25), LunarCalendar.lunarToSolar(2025, 6, 1, false))
        assertEquals(LocalDate.of(2025, 7, 25), LunarCalendar.lunarToSolar(2025, 6, 1, true))
        // 2025 闰六月是小月（29 天）：7-25 起 29 天 → 七月初一 = 8-23
        assertEquals(29, LunarCalendar.leapMonthDays(2025))
        assertEquals(LocalDate.of(2025, 8, 23), LunarCalendar.lunarToSolar(2025, 7, 1, false))
    }

    @Test
    fun `2026 无闰月`() {
        assertEquals(0, LunarCalendar.leapMonth(2026))
    }

    @Test
    fun `干支年 2026 = 丙午`() {
        assertEquals("丙午", LunarCalendar.yearGanZhi(2026))
        assertEquals("乙巳", LunarCalendar.yearGanZhi(2025))
    }

    @Test
    fun `公历转农历往返一致`() {
        // 春节当天转农历 = 正月初一
        val lunar = LunarCalendar.solarToLunar(LocalDate.of(2026, 2, 17))
        assertNotNull(lunar)
        assertEquals(LunarDate(2026, 1, 1, false), lunar)

        // 中秋当天
        val midAutumn = LunarCalendar.solarToLunar(LocalDate.of(2026, 9, 25))
        assertEquals(LunarDate(2026, 8, 15, false), midAutumn)

        // 闰六月里的公历日 → isLeap = true
        val leapDay = LunarCalendar.solarToLunar(LocalDate.of(2025, 8, 1))
        assertNotNull(leapDay)
        assertTrue(leapDay!!.isLeap)
        assertEquals(6, leapDay.month)

        // 闰六月结束后一天（2025-08-23 = 七月初一，闰六月 7-25 起 29 天）
        val afterLeap = LunarCalendar.solarToLunar(LocalDate.of(2025, 8, 23))
        assertEquals(LunarDate(2025, 7, 1, false), afterLeap)
    }

    @Test
    fun `非法输入返回 null`() {
        // 平年传闰月
        assertNull(LunarCalendar.lunarToSolar(2026, 6, 1, true))
        // 小月三十（2026 正月是 30 天大月不行——找一个小月：2025 五月 29 天）
        assertNull(LunarCalendar.lunarToSolar(2025, 5, 30, false))
        // 越界年份
        assertNull(LunarCalendar.lunarToSolar(1899, 1, 1, false))
        assertNull(LunarCalendar.lunarToSolar(2101, 1, 1, false))
        // 表头之前
        assertNull(LunarCalendar.solarToLunar(LocalDate.of(1900, 1, 30)))
    }

    @Test
    fun `往返全量抽样一致（闰月边界）`() {
        // 从 2024-01-01 到 2026-12-31 逐日：solar→lunar→solar 必须回到同一天
        var d = LocalDate.of(2024, 1, 1)
        val end = LocalDate.of(2026, 12, 31)
        while (!d.isAfter(end)) {
            val lunar = LunarCalendar.solarToLunar(d)
            assertNotNull("失败于 $d", lunar)
            val back = LunarCalendar.lunarToSolar(lunar!!.year, lunar.month, lunar.day, lunar.isLeap)
            assertEquals("失败于 $d -> $lunar", d, back)
            d = d.plusDays(1)
        }
    }

    @Test
    fun `日名与月名`() {
        assertEquals("初一", lunarDayName(1))
        assertEquals("三十", lunarDayName(30))
        assertEquals("腊月", lunarMonthName(12))
    }
}
