package com.bookandmusic.lovelife.appnative.designsystem.atoms

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.KeyboardArrowLeft
import androidx.compose.material.icons.outlined.KeyboardArrowRight
import androidx.compose.material.icons.outlined.KeyboardDoubleArrowLeft
import androidx.compose.material.icons.outlined.KeyboardDoubleArrowRight
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import com.bookandmusic.lovelife.appnative.designsystem.theme.LocalAppColors
import java.time.LocalDate

/**
 * 日期选择结果。两种模式都同时携带两套语义：
 * - [date]：选中日的**公历**日期（农历模式 = 该农历日换算出的公历日，供界面显示）。
 * - [lunarYear]/[lunarMonth]/[lunarDay]/[lunarLeap]：选中日的**农历**语义（月号 1-12 + 闰月标记）。
 *
 * 提交口径由调用方决定：服务端纪念日契约 = lunar 时 date 字段填**农历年月日**
 * （YYYY-MM-DD 按农历语义，如中秋 = 2026-08-15），solar 时填公历。
 */
data class PickedDate(
    val date: LocalDate,
    val lunarYear: Int,
    val lunarMonth: Int,
    val lunarDay: Int,
    val lunarLeap: Boolean,
)

/** 农历月（月号 1-12 + 是否闰月位；同年同号平月/闰月是两个序位） */
data class LunarMonth(val month: Int, val isLeap: Boolean)

/**
 * 微信式日期选择 sheet：月历网格（7 列）+ 年月双层切换（«年 ‹月 标题 月› 年»）+
 * 周标题行 + 「今天」快捷。支持公历 / 农历两种模式：
 * - 公历模式：格内公历日。
 * - 农历模式：格内农历日名（初一显示月名）+ **对应阳历 M/d 副标**（用户不识农历日时可直接读阳历）。
 *
 * 农历月导航按**序位**（position）而非月号：闰年 13 个月里「六月」与「闰六月」是两个序位，
 * 按月号导航会永远停在平月（闰月翻不进去）。年份切换双模式都支持（1900–2100，表界）。
 *
 * 农历换算由调用方注入（[lunarEngine]），避免 designsystem 依赖 app 的 LunarCalendar。
 */
@Composable
fun AppDatePickerSheet(
    visible: Boolean,
    onDismiss: () -> Unit,
    onPick: (PickedDate) -> Unit,
    lunarMode: Boolean = false,
    initial: LocalDate? = null,
    lunarEngine: LunarEngine? = null,
) {
    val today = remember { LocalDate.now() }
    val init = initial ?: today
    val lunar = lunarMode && lunarEngine != null
    val engine = lunarEngine

    // 初始定位：农历模式由公历 initial 反推农历年 + 月序位；阳历模式 = 月-1
    val initLunar = if (lunar) engine!!.solarToLunar(init) else null
    var year by remember(visible) {
        mutableStateOf(if (initLunar != null) initLunar.lunarYear else init.year)
    }
    var monthPos by remember(visible) {
        mutableStateOf(
            if (initLunar != null) engine!!.positionOf(initLunar.lunarYear, initLunar.lunarMonth, initLunar.lunarLeap)
            else init.monthValue - 1,
        )
    }

    // 年份界：农历表 1900–2100；阳历同界（纪念日场景足够，统一防越界）
    val minYear = 1900
    val maxYear = 2100

    fun stepMonth(delta: Int) {
        if (lunar) {
            val count = engine!!.monthCount(year)
            var pos = monthPos + delta
            var y = year
            if (pos < 0) {
                y -= 1
                if (y < minYear) return
                pos = engine.monthCount(y) - 1
            } else if (pos >= count) {
                y += 1
                if (y > maxYear) return
                pos = 0
            }
            year = y
            monthPos = pos
        } else {
            var m = monthPos + delta
            var y = year
            if (m < 0) { y -= 1; if (y < minYear) return; m = 11 }
            else if (m > 11) { y += 1; if (y > maxYear) return; m = 0 }
            year = y
            monthPos = m
        }
    }

    fun stepYear(delta: Int) {
        val y = (year + delta).coerceIn(minYear, maxYear)
        if (y == year) return
        year = y
        if (lunar) {
            // 新年月数可能变少（闰年 13 → 平年 12）：序位越界时收到末月
            monthPos = monthPos.coerceIn(0, engine!!.monthCount(y) - 1)
        }
    }

    AppSheet(visible = visible, onDismiss = onDismiss) {
        // ===== 年月双层切换：«年 ‹月 [标题] 月› 年» =====
        Row(
            Modifier.fillMaxWidth().padding(bottom = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Box(
                Modifier
                    .size(34.dp)
                    .clip(CircleShape)
                    .clickable(enabled = year > minYear) { stepYear(-1) },
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    Icons.Outlined.KeyboardDoubleArrowLeft,
                    contentDescription = "上一年",
                    tint = MaterialTheme.colorScheme.onSurface.copy(alpha = if (year > minYear) 1f else 0.3f),
                    modifier = Modifier.size(18.dp),
                )
            }
            Box(
                Modifier
                    .size(34.dp)
                    .clip(CircleShape)
                    .clickable { stepMonth(-1) },
                contentAlignment = Alignment.Center,
            ) {
                Icon(Icons.Outlined.KeyboardArrowLeft, contentDescription = "上个月", tint = MaterialTheme.colorScheme.onSurface)
            }
            Spacer(Modifier.weight(1f))
            Text(
                if (lunar) {
                    "${engine!!.yearGanZhi(year)} $year · ${engine.monthLabel(year, monthPos)}"
                } else {
                    "$year 年 ${monthPos + 1} 月"
                },
                style = MaterialTheme.typography.titleMedium,
                color = MaterialTheme.colorScheme.onSurface,
            )
            Spacer(Modifier.weight(1f))
            Box(
                Modifier
                    .size(34.dp)
                    .clip(CircleShape)
                    .clickable { stepMonth(1) },
                contentAlignment = Alignment.Center,
            ) {
                Icon(Icons.Outlined.KeyboardArrowRight, contentDescription = "下个月", tint = MaterialTheme.colorScheme.onSurface)
            }
            Box(
                Modifier
                    .size(34.dp)
                    .clip(CircleShape)
                    .clickable(enabled = year < maxYear) { stepYear(1) },
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    Icons.Outlined.KeyboardDoubleArrowRight,
                    contentDescription = "下一年",
                    tint = MaterialTheme.colorScheme.onSurface.copy(alpha = if (year < maxYear) 1f else 0.3f),
                    modifier = Modifier.size(18.dp),
                )
            }
        }

        // 周标题
        Row(Modifier.fillMaxWidth()) {
            listOf("日", "一", "二", "三", "四", "五", "六").forEach { w ->
                Text(
                    w,
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    textAlign = TextAlign.Center,
                    modifier = Modifier.weight(1f),
                )
            }
        }

        // ===== 日历网格（6 行 × 7 列） =====
        val first: LocalDate
        val daysInMonth: Int
        if (lunar) {
            first = engine!!.monthStart(year, monthPos)
            daysInMonth = engine.monthLength(year, monthPos)
        } else {
            first = LocalDate.of(year, monthPos + 1, 1)
            daysInMonth = first.lengthOfMonth()
        }
        val leading = first.dayOfWeek.value % 7
        val cells = leading + daysInMonth
        val rows = (cells + 6) / 7
        val curMonth = if (lunar) engine!!.monthAt(year, monthPos) else null
        Column(Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
            repeat(rows) { r ->
                Row(Modifier.fillMaxWidth()) {
                    repeat(7) { c ->
                        val idx = r * 7 + c
                        Box(
                            Modifier
                                .weight(1f)
                                .aspectRatio(1f)
                                .padding(2.dp),
                            contentAlignment = Alignment.Center,
                        ) {
                            if (idx >= leading && idx < cells) {
                                val d = idx - leading + 1
                                val date = if (lunar) {
                                    engine!!.lunarToSolar(year, monthPos, d)
                                } else {
                                    LocalDate.of(year, monthPos + 1, d)
                                }
                                val isToday = date == today
                                val dayLabel = if (lunar) {
                                    engine!!.dayLabel(year, monthPos, d)
                                } else d.toString()
                                Box(
                                    Modifier
                                        .fillMaxWidth()
                                        .aspectRatio(1f)
                                        .clip(RoundedCornerShape(999.dp))
                                        .background(if (isToday) LocalAppColors.current.tintWash else Color.Transparent)
                                        .clickable(enabled = date != null) {
                                            if (date != null) {
                                                onPick(
                                                    PickedDate(
                                                        date = date,
                                                        lunarYear = year,
                                                        lunarMonth = curMonth?.month ?: (monthPos + 1),
                                                        lunarDay = d,
                                                        lunarLeap = curMonth?.isLeap ?: false,
                                                    )
                                                )
                                            }
                                        },
                                    contentAlignment = Alignment.Center,
                                ) {
                                    Column(horizontalAlignment = Alignment.CenterHorizontally) {
                                        Text(
                                            dayLabel,
                                            style = MaterialTheme.typography.bodyLarge,
                                            color = MaterialTheme.colorScheme.onSurface,
                                            textAlign = TextAlign.Center,
                                        )
                                        // 农历格副标：对应阳历 M/d（闰月由标题标识，不逐格标）
                                        if (lunar && date != null) {
                                            Text(
                                                "${date.monthValue}/${date.dayOfMonth}",
                                                style = MaterialTheme.typography.labelMedium,
                                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                                                textAlign = TextAlign.Center,
                                            )
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }

        // 今天快捷
        Text(
            "今天",
            style = MaterialTheme.typography.labelLarge,
            color = LocalAppColors.current.tint,
            textAlign = TextAlign.Center,
            modifier = Modifier
                .fillMaxWidth()
                .padding(top = 4.dp)
                .clickable {
                    val l = lunarEngine?.solarToLunar(today)
                    onPick(
                        PickedDate(
                            date = today,
                            lunarYear = l?.lunarYear ?: today.year,
                            lunarMonth = l?.lunarMonth ?: today.monthValue,
                            lunarDay = l?.lunarDay ?: today.dayOfMonth,
                            lunarLeap = l?.lunarLeap ?: false,
                        )
                    )
                }
                .padding(vertical = 8.dp),
        )
    }
}

/** 农历换算引擎（由 app 侧注入，designsystem 不依赖 LunarCalendar）。
 *  月寻址一律用**序位**（pos，0 起）：闰年 13 个月、同号平月/闰月各占一序位。 */
interface LunarEngine {
    /** 该农历年的月总数（12 或 13） */
    fun monthCount(y: Int): Int

    /** 序位 → 农历月（月号 + 闰位标记） */
    fun monthAt(y: Int, pos: Int): LunarMonth

    /** （月号, 闰位）→ 序位；不存在返回 -1 */
    fun positionOf(y: Int, month: Int, isLeap: Boolean): Int

    /** 该序位月的公历起始日 */
    fun monthStart(y: Int, pos: Int): LocalDate

    /** 该序位月的天数（闰月位 = 闰月天数） */
    fun monthLength(y: Int, pos: Int): Int

    /** 该序位月某日 → 公历；非法（如小月三十）返回 null */
    fun lunarToSolar(y: Int, pos: Int, d: Int): LocalDate?

    /** 公历 → 农历（含闰位） */
    fun solarToLunar(date: LocalDate): PickedDate?

    /** 格内日名（初一显示月名，闰月初一显示「闰X月」） */
    fun dayLabel(y: Int, pos: Int, d: Int): String

    /** 年干支（如「丙午」） */
    fun yearGanZhi(y: Int): String

    /** 月标签（「八月」/「闰六月」） */
    fun monthLabel(y: Int, pos: Int): String
}
