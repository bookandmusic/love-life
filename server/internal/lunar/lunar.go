// Package lunar 内置离线农历换算（1900–2100 社区通用表驱动算法）。
// 说明：首版表驱动实现，2026-09 起入库；不引入外部天文数据做「权威校准」。
// 周期表是**单一数据源** lunar_table.json（本包内，go:embed 加载；APP 侧 lunar.ts 直接 import 同一份文件），
// 故两侧不可能漂移，不再需要「两份表逐项比对」的护栏；随附不变量测试见 invariants_test.go。
// 周期编码（每农历年的整型 info）：低 4 位 = 闰月(1-12，0 无闰)；0x10000 = 闰月为大月(30 天)；
// 0x10000>>m = 第 m 个月为大月(30 天)（m=1..12）；其余月份为小月(29 天)。
package lunar

import (
	_ "embed"
	"encoding/json"
	"time"
)

//go:embed lunar_table.json
var lunarTableJSON []byte

// info 农历年周期编码表（1900 起，每项一年）。由单一数据源 JSON 解析而来。
var info = mustParseTable(lunarTableJSON)

// mustParseTable 解析内嵌周期表。表随二进制一起编译进来，只有文件被改坏才可能失败，
// 故直接 panic：宁可启动即报错，也不要静默按空表算出错误日期。
func mustParseTable(b []byte) []int {
	var v []int
	if err := json.Unmarshal(b, &v); err != nil {
		panic("lunar: 解析 lunar_table.json 失败: " + err.Error())
	}
	if len(v) == 0 {
		panic("lunar: lunar_table.json 为空")
	}
	return v
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
	// 闰月自身的天数与平月不同：日期上限用 leapDays
	if isLeap && d > leapDays(y) {
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
	// 闰 m 月排在平 m 月之后：先加平月 m 整月；d-1 自然落在闰月内
	if isLeap {
		offset += monthDays(y, m)
	}
	offset += d - 1
	return baseDate.AddDate(0, 0, offset), true
}

// NextLunarOccurrence 农历 m 月 d 日（isLeap=是否闰月）的下一次（含 today）公历日期 YYYY-MM-DD；
// 逐年按 isLeap 标记查对应月份（闰月仅在该年有此闰月时存在）；找不到（超出表尾 / 该年无此闰月）返回 ""。
// 注意用换算结果自身的公历年比较：冬月/腊月常落到次年 1 月，取农历年会差一年。
func NextLunarOccurrence(m, d int, isLeap bool, today time.Time) string {
	tYear, tMonth, tDay := today.Date()
	todayDay := time.Date(tYear, tMonth, tDay, 0, 0, 0, 0, today.Location())
	endYear := baseYear + len(info) - 1
	for y := tYear; y <= endYear; y++ {
		if !inTable(y) {
			break
		}
		if isLeap && LeapMonth(y) != m {
			continue
		}
		dt, ok := LunarToSolar(y, m, d, isLeap)
		if !ok {
			continue
		}
		day := time.Date(dt.Year(), dt.Month(), dt.Day(), 0, 0, 0, 0, today.Location())
		if !day.Before(todayDay) {
			return day.Format("2006-01-02")
		}
	}
	return ""
}

// ValidDateShape 仅校验 YYYY-MM-DD 形状（solar/lunar 纪念日统一契约）；
// solar 的真实公历日由调用方 time.Parse 补充，lunar 保持宽容（可落在公历不存在日）。
func ValidDateShape(s string) bool {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return false
	}
	for i, c := range s {
		if i == 4 || i == 7 {
			continue
		}
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
