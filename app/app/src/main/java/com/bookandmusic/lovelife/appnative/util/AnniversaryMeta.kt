package com.bookandmusic.lovelife.appnative.util

import com.bookandmusic.lovelife.appnative.data.AnniversaryDto
import java.time.LocalDate
import java.time.format.DateTimeFormatter

/**
 * 纪念日行的展示元数据：
 * 副行1 = 「年 · 日期规则」（阳历「2025 年 · 10 月 1 日」/ 农历「1993 年 · 农历八月十五」），
 * 副行2 = 「下个公历 YYYY-MM-DD」。
 *
 * 下个公历不自己算农历换算：服务端已给出 `days_until`（含农历换算与闰月口径），
 * 直接 today + days_until 即得，与右侧天数严格自洽。
 */
data class AnniversaryMeta(
    val hot: Boolean,
    val anniv: String,
    val next: String,
    val days: Int,
    val invalid: Boolean,
)

private val CN_DAY = listOf(
    "初一", "初二", "初三", "初四", "初五", "初六", "初七", "初八", "初九", "初十",
    "十一", "十二", "十三", "十四", "十五", "十六", "十七", "十八", "十九", "二十",
    "廿一", "廿二", "廿三", "廿四", "廿五", "廿六", "廿七", "廿八", "廿九", "三十",
)
private val CN_MONTH = listOf(
    "正月", "二月", "三月", "四月", "五月", "六月",
    "七月", "八月", "九月", "十月", "冬月", "腊月",
)

fun anniversaryMeta(a: AnniversaryDto): AnniversaryMeta {
    val parts = a.date.split("-")
    val y = parts.getOrNull(0)?.toIntOrNull()
    val m = parts.getOrNull(1)?.toIntOrNull()
    val d = parts.getOrNull(2)?.toIntOrNull()
    val ok = y != null && m != null && d != null && m in 1..12 && d in 1..31
    val isLunar = a.type == "lunar"
    val days = a.days_until
    val invalid = !ok || days < 0

    val rule = when {
        !ok -> ""
        isLunar -> "农历" + (CN_MONTH.getOrNull(m!! - 1) ?: "$m 月") +
            (CN_DAY.getOrNull(d!! - 1) ?: "$d 日") + if (a.is_leap) "（闰）" else ""
        else -> "$m 月 $d 日"
    }
    val anniv = if (rule.isEmpty()) "" else "${y} 年 · $rule"
    val nextDate = if (invalid) "—" else LocalDate.now().plusDays(days.toLong()).format(DateTimeFormatter.ISO_LOCAL_DATE)
    return AnniversaryMeta(
        hot = days in 0..30,
        anniv = anniv,
        next = "下个公历 $nextDate",
        days = days,
        invalid = invalid,
    )
}
