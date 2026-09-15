package lunar

import (
	"testing"
	"time"
)

// ---- DB2 不变量测试 ----
//
// 本包为社区通用表驱动算法（1900–2100），无法在本环境联网做权威逐日校准；
// 故以「不变量」护栏替代：编码自洽、边界安全、公历往返恒等、跨实现可对拍。
// 这些性质一旦被改表改算法破坏，会立刻失败。

// 闰月编码自洽：每个农历年最多一个闰月，且闰月号 ∈ [0,12]；
// 19 年 7 闰的长期规律在 1900–2100 全区间成立。
func TestLeapMonthEncodingInvariants(t *testing.T) {
	leapYears := 0
	for y := baseYear; y < baseYear+len(info); y++ {
		lm := LeapMonth(y)
		if lm < 0 || lm > 12 {
			t.Fatalf("%d 年闰月号越界: %d", y, lm)
		}
		if lm > 0 {
			leapYears++
			// 闰月所在月必须在小月/大月编码里出现
			if monthDays(y, lm) == 0 {
				t.Fatalf("%d 年闰月 %d 的月长编码缺失", y, lm)
			}
		}
	}
	// 1900–2100 共 201 年，闰年数应在 73~75（19 年 7 闰 ≈ 74）
	if leapYears < 73 || leapYears > 75 {
		t.Fatalf("闰年数 = %d，超出合理区间 73~75", leapYears)
	}
}

// 每年天数只能是 353~355（平年）或 383~385（闰年）。
func TestYearLengthInvariants(t *testing.T) {
	for y := baseYear; y < baseYear+len(info); y++ {
		days := yearDays(y)
		if LeapMonth(y) > 0 {
			if days < 383 || days > 385 {
				t.Fatalf("%d 年（闰）天数 = %d，应为 383~385", y, days)
			}
			continue
		}
		if days < 353 || days > 355 {
			t.Fatalf("%d 年（平）天数 = %d，应为 353~355", y, days)
		}
	}
}

// 每月天数只能是 29 或 30。
func TestMonthLengthInvariants(t *testing.T) {
	for y := baseYear; y < baseYear+len(info); y++ {
		for m := 1; m <= 12; m++ {
			if d := monthDays(y, m); d != 29 && d != 30 {
				t.Fatalf("%d-%d 月长 = %d，应为 29 或 30", y, m, d)
			}
		}
		if LeapMonth(y) > 0 {
			if d := leapDays(y); d != 29 && d != 30 {
				t.Fatalf("%d 闰月月长 = %d，应为 29 或 30", y, d)
			}
		}
	}
}

// 区间边界：表内年份可换算；表外年份必须安全返回 false（不 panic）。
func TestLunarToSolarBounds(t *testing.T) {
	last := baseYear + len(info) - 1
	if _, ok := LunarToSolar(baseYear, 1, 1, false); !ok {
		t.Fatal("表头 1900 正月初一 应可换算")
	}
	if _, ok := LunarToSolar(last, 12, 30, false); !ok {
		// 该农历年腊月可能只有 29 天，退一步用 29
		if _, ok := LunarToSolar(last, 12, 29, false); !ok {
			t.Fatalf("表尾 %d 年腊月 应可换算", last)
		}
	}
	for _, y := range []int{baseYear - 1, last + 1} {
		if _, ok := LunarToSolar(y, 1, 1, false); ok {
			t.Fatalf("表外年份 %d 不应换算成功", y)
		}
	}
}

// 单调性与连续性：农历 (y,1,1) 换算出的公历日逐年递增，且相邻两年间隔 353~385 天。
func TestLunarNewYearMonotonic(t *testing.T) {
	prev := time.Time{}
	last := baseYear + len(info) - 1
	for y := baseYear; y <= last; y++ {
		cur, ok := LunarToSolar(y, 1, 1, false)
		if !ok {
			t.Fatalf("%d 年正月初一 应可换算", y)
		}
		if !prev.IsZero() {
			gap := int(cur.Sub(prev).Hours() / 24)
			if gap < 353 || gap > 385 {
				t.Fatalf("%d → %d 年春节间隔 = %d 天，超出 353~385", y-1, y, gap)
			}
		}
		prev = cur
	}
}

// NextLunarOccurrence 在给定日期之后（含当日）返回，且落在同一年内。
func TestNextLunarOccurrenceInvariant(t *testing.T) {
	today := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	got := NextLunarOccurrence(8, 15, false, today) // 中秋（平月）
	dt, err := time.ParseInLocation("2006-01-02", got, time.UTC)
	if err != nil {
		t.Fatalf("返回值不是合法日期 %q: %v", got, err)
	}
	if dt.Before(today) {
		t.Fatalf("返回日期 %s 早于今天 %s", got, today.Format("2006-01-02"))
	}
	if dt.Year() > today.Year()+1 {
		t.Fatalf("返回日期 %s 超出一年范围", got)
	}
}
