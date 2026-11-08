package com.bookandmusic.lovelife.appnative

import com.bookandmusic.lovelife.appnative.util.LunarCalendar
import java.time.LocalDate
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * 日历组件的农历序位导航口径（AppLunarEngine 的核心逻辑镜像测试）：
 * 闰年 13 个月，同号平月/闰月是两个序位——按月号导航会永远停在平月（审核发现的 MAJOR bug）。
 * 这里直接用 LunarCalendar 验证序位展开的正确性。
 */
class LunarMonthPositionTest {

    /** 序位展开：2025 闰六月 → 13 个月，序位 6 = 平六月、序位 7 = 闰六月 */
    @Test
    fun `2025 年序位展开含闰六月`() {
        val lm = LunarCalendar.leapMonth(2025)
        assertEquals(6, lm)
        // 月序列：1,2,3,4,5,6,[闰6],7,...,12 → count = 13
        val count = 12 + (if (lm > 0) 1 else 0)
        assertEquals(13, count)

        // 序位 5 = 平六月初一 = 2025-06-25
        assertEquals(LocalDate.of(2025, 6, 25), LunarCalendar.lunarToSolar(2025, 6, 1, false))
        // 序位 6 = 闰六月初一 = 2025-07-25
        assertEquals(LocalDate.of(2025, 7, 25), LunarCalendar.lunarToSolar(2025, 6, 1, true))
        // 序位 7 = 七月初一 = 2025-08-23
        assertEquals(LocalDate.of(2025, 8, 23), LunarCalendar.lunarToSolar(2025, 7, 1, false))
    }

    /** 序位导航链：逐序位走的公历起始日必须严格递增（平六月 → 闰六月 → 七月不重不漏） */
    @Test
    fun `序位导航月首严格递增`() {
        val starts = mutableListOf<LocalDate>()
        for (m in 1..12) {
            starts.add(LunarCalendar.lunarToSolar(2025, m, 1, false)!!)
            if (m == 6) starts.add(LunarCalendar.lunarToSolar(2025, 6, 1, true)!!)
        }
        // 平月按月号升序；闰六月插在平六月后
        val sorted = starts.sorted()
        assertEquals("月首必须无重复", sorted.size, starts.toSet().size)
        assertEquals("展开序列应等价于排序序列", sorted, starts)
    }

    /** 提交语义镜像：农历模式的 date 字段 = 农历年月日拼接（服务端契约），不是公历投影 */
    @Test
    fun `农历提交语义为农历年月日`() {
        // 用户在 2026 年选「八月初三」：公历投影 = 2026-09-13（中秋 09-25 为八月十五，倒推初三）
        val solarProjection = LunarCalendar.lunarToSolar(2026, 8, 3, false)
        assertEquals(LocalDate.of(2026, 9, 13), solarProjection)
        // 提交 date 是 2026-08-03（农历语义），月/日与公历投影不同——本用例守的就是这条边界
        val submit = "%04d-%02d-%02d".format(2026, 8, 3)
        assertEquals("2026-08-03", submit)
        assertTrue(submit != solarProjection.toString())
    }

    /** positionOf 往返：公历 → 农历 → 定位序位 → 换算回公历，闰月内不丢闰位 */
    @Test
    fun `闰月内公历往返不丢闰位`() {
        // 2025-08-01 落在闰六月中
        val l = LunarCalendar.solarToLunar(LocalDate.of(2025, 8, 1))
        assertNotNull(l)
        assertTrue(l!!.isLeap)
        assertEquals(6, l.month)
        // 序位定位后反查：闰六月初一 = 2025-07-25 ≤ 2025-08-01 < 七月初一 2025-08-23
        val leapStart = LunarCalendar.lunarToSolar(2025, l.month, 1, l.isLeap)!!
        val nextStart = LunarCalendar.lunarToSolar(2025, 7, 1, false)!!
        assertTrue(leapStart <= LocalDate.of(2025, 8, 1))
        assertTrue(LocalDate.of(2025, 8, 1) < nextStart)
    }
}
