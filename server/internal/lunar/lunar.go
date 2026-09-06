// Package lunar 内置离线农历换算（1900–2100 社区通用表驱动算法，与 app/src/utils/lunar.ts 同源同表）。
// 说明：首版表驱动实现，2026-09 起入库（A3）；区间精度校准见 docs/TODO.md A2。
// 周期编码（每农历年的整型 info）：低 4 位 = 闰月(1-12，0 无闰)；0x10000 = 闰月为大月(30 天)；
// 0x10000>>m = 第 m 个月为大月(30 天)（m=1..12）；其余月份为小月(29 天)。
package lunar

import "time"

var info = []int{
	0x04bd8, 0x04ae0, 0x0a570, 0x054d5, 0x0d260, 0x0d950, 0x16554, 0x056a0, 0x09ad0, 0x055d2, // 1900
	0x04ae0, 0x0a5b6, 0x0a4d0, 0x0d250, 0x1d255, 0x0b540, 0x0d6a0, 0x0ada2, 0x095b0, 0x14977, // 1910
	0x04970, 0x0a4b0, 0x0b4b5, 0x06a50, 0x06d40, 0x1ab54, 0x02b60, 0x09570, 0x052f2, 0x04970, // 1920
	0x06566, 0x0d4a0, 0x0ea50, 0x06e95, 0x05ad0, 0x02b60, 0x186e3, 0x092e0, 0x1c8d7, 0x0c950, // 1930
	0x0d4a0, 0x1d8a6, 0x0b550, 0x056a0, 0x1a5b4, 0x025d0, 0x092d0, 0x0d2b2, 0x0a950, 0x0b557, // 1940
	0x06ca0, 0x0b550, 0x15355, 0x04da0, 0x0a5b0, 0x14573, 0x052b0, 0x0a9a8, 0x0e950, 0x06aa0, // 1950
	0x0aea6, 0x0ab50, 0x04b60, 0x0aae4, 0x0a570, 0x05260, 0x0f263, 0x0d950, 0x05b57, 0x056a0, // 1960
	0x096d0, 0x04dd5, 0x04ad0, 0x0a4d0, 0x0d4d4, 0x0d250, 0x0d558, 0x0b540, 0x0b6a0, 0x195a6, // 1970
	0x095b0, 0x049b0, 0x0a974, 0x0a4b0, 0x0b27a, 0x06a50, 0x06d40, 0x0af46, 0x0ab60, 0x09570, // 1980
	0x04af5, 0x04970, 0x064b0, 0x074a3, 0x0ea50, 0x06b58, 0x055c0, 0x0ab60, 0x096d5, 0x092e0, // 1990
	0x0c960, 0x0d954, 0x0d4a0, 0x0da50, 0x07552, 0x056a0, 0x0abb7, 0x025d0, 0x092d0, 0x0c5b5, // 2000
	0x0a950, 0x0b4a0, 0x0baa4, 0x0ad50, 0x055d9, 0x04ba0, 0x0a5b0, 0x15176, 0x052b0, 0x0a930, // 2010
	0x07954, 0x06aa0, 0x0ad50, 0x05b52, 0x04b60, 0x0a6e6, 0x0a4e0, 0x0d260, 0x0ea65, 0x0d530, // 2020
	0x05aa0, 0x076a3, 0x096d0, 0x04afb, 0x04ad0, 0x0a4d0, 0x1d0b6, 0x0d250, 0x0d520, 0x0dd45, // 2030
	0x0b5a0, 0x056d0, 0x055b2, 0x049b0, 0x0a577, 0x0a4b0, 0x0aa50, 0x1b255, 0x06d20, 0x0ada0, // 2040
	0x14b63, 0x09370, 0x049f8, 0x04970, 0x064b0, 0x168a6, 0x0ea50, 0x06b20, 0x1a6c4, 0x0aae0, // 2050
	0x092e0, 0x0d2e3, 0x0c960, 0x0d557, 0x0d4a0, 0x0da50, 0x05d55, 0x056a0, 0x0a6d0, 0x055d4, // 2060
	0x052d0, 0x0a9b8, 0x0a950, 0x0b4a0, 0x0b6a6, 0x0ad50, 0x055a0, 0x0aba4, 0x0a5b0, 0x052b0, // 2070
	0x0b273, 0x06930, 0x07337, 0x06aa0, 0x0ad50, 0x14b55, 0x04b60, 0x0a570, 0x054e4, 0x0d160, // 2080
	0x0e968, 0x0d520, 0x0daa0, 0x16aa6, 0x056d0, 0x04ae0, 0x0a9d4, 0x0a2d0, 0x0d150, 0x0f252, // 2090
	0x0d520, // 2100
}

const baseYear = 1900

// 基准日：农历 1900 年正月初一 = 公历 1900-01-31。
var baseDate = time.Date(1900, 1, 31, 0, 0, 0, 0, time.UTC)

func inTable(y int) bool { return y >= baseYear && y <= baseYear+len(info)-1 }

// LeapMonth 农历年 y 的闰月（0=无闰）。
func LeapMonth(y int) int { return info[y-baseYear] & 0xf }

// leapDays 农历年 y 闰月天数（无闰为 0）。
func leapDays(y int) int {
	lm := LeapMonth(y)
	if lm == 0 {
		return 0
	}
	if info[y-baseYear]&0x10000 != 0 {
		return 30
	}
	return 29
}

// monthDays 农历年 y 月 m 的天数（大月 30，小月 29）。
func monthDays(y, m int) int {
	if info[y-baseYear]&(0x10000>>m) != 0 {
		return 30
	}
	return 29
}

// yearDays 农历年 y 的总天数。
func yearDays(y int) int {
	sum := 348
	for i := 0x8000; i > 0x8; i >>= 1 {
		if info[y-baseYear]&i != 0 {
			sum++
		}
	}
	return sum + leapDays(y)
}

// LunarToSolar 农历 y 年 m 月 d 日（isLeap=闰月）→ 公历；超出表范围或日期不存在返回 false。
// 小月无「三十」等该农历日不存在的输入同样返回 false（与 TS 版一致）。
func LunarToSolar(y, m, d int, isLeap bool) (time.Time, bool) {
	if !inTable(y) || m < 1 || m > 12 {
		return time.Time{}, false
	}
	lm := LeapMonth(y)
	if d < 1 || d > monthDays(y, m) {
		return time.Time{}, false
	}
	if isLeap && lm != m {
		return time.Time{}, false
	}
	offset := 0
	for i := baseYear; i < y; i++ {
		offset += yearDays(i)
	}
	for i := 1; i < m; i++ {
		offset += monthDays(y, i)
		if i == lm {
			offset += leapDays(y)
		}
	}
	if isLeap {
		offset += leapDays(y)
	}
	offset += d - 1
	return baseDate.AddDate(0, 0, offset), true
}

// NextLunarOccurrence 农历 m 月 d 日的下一次（含 today）公历日期 YYYY-MM-DD；
// 自农历当年起逐年、先平月后闰月；找不到（超出表尾）返回 ""。
// 注意用换算结果自身的公历年比较：冬月/腊月常落到次年 1 月，取农历年会差一年。
func NextLunarOccurrence(m, d int, today time.Time) string {
	tYear, tMonth, tDay := today.Date()
	todayDay := time.Date(tYear, tMonth, tDay, 0, 0, 0, 0, today.Location())
	endYear := baseYear + len(info) - 1
	for y := tYear; y <= endYear; y++ {
		if !inTable(y) {
			break
		}
		for _, leap := range []bool{false, true} {
			if leap && LeapMonth(y) != m {
				continue
			}
			dt, ok := LunarToSolar(y, m, d, leap)
			if !ok {
				continue
			}
			day := time.Date(dt.Year(), dt.Month(), dt.Day(), 0, 0, 0, 0, today.Location())
			if !day.Before(todayDay) {
				return day.Format("2006-01-02")
			}
		}
	}
	return ""
}
