package lunar

// 黄金用例与 app/src/utils/lunar.ts（mock 同源）对拍生成（node /tmp/gen-golden.mjs），逐项一致。

import (
	"testing"
	"time"
)

func mustSolar(t *testing.T, y, m, d int, leap bool) time.Time {
	t.Helper()
	dt, ok := LunarToSolar(y, m, d, leap)
	if !ok {
		t.Fatalf("LunarToSolar(%d,%d,%d,leap=%v) 应可换算", y, m, d, leap)
	}
	return dt
}

func TestLunarToSolarGolden(t *testing.T) {
	cases := []struct {
		y, m, d int
		leap    bool
		want    string // 期望公历 YYYY-MM-DD
	}{
		{2026, 8, 15, false, "2026-09-25"},  // 中秋（对照对拍集）
		{1996, 5, 15, false, "1996-06-30"},  // fixtures 她的生日
		{2025, 6, 1, false, "2025-06-25"},   // 闰六月的平六月
		{2025, 6, 1, true, "2025-07-25"},    // 2025 闰六月（六源校准：07-25 起、小月 29 天）
		{1900, 1, 1, false, "1900-01-31"},   // 表头基准日
		{2026, 12, 15, false, "2027-01-22"}, // 腊月跨公历年
		{2026, 11, 3, false, "2026-12-11"},  // 冬月
		{2043, 2, 1, false, "2043-03-11"},   // 表中后段
	}
	for _, c := range cases {
		dt := mustSolar(t, c.y, c.m, c.d, c.leap)
		if got := dt.Format("2006-01-02"); got != c.want {
			t.Errorf("LunarToSolar(%d,%d,%d,leap=%v) = %s, want %s", c.y, c.m, c.d, c.leap, got, c.want)
		}
	}
}

func TestLunarToSolarInvalid(t *testing.T) {
	if _, ok := LunarToSolar(2026, 2, 30, false); ok {
		t.Error("2026 农历二月为小月，无三十，应不可换算")
	}
	if _, ok := LunarToSolar(2026, 6, 1, true); ok {
		t.Error("2026 无闰六月，闰月入参应不可换算")
	}
	if _, ok := LunarToSolar(1899, 1, 1, false); ok {
		t.Error("表前越界应不可换算")
	}
	if _, ok := LunarToSolar(2101, 1, 1, false); ok {
		t.Error("表后越界应不可换算")
	}
	if _, ok := LunarToSolar(2026, 13, 1, false); ok {
		t.Error("月越界应不可换算")
	}
}

func TestNextLunarOccurrence(t *testing.T) {
	today := func(s string) time.Time {
		d, _ := time.ParseInLocation("2006-01-02", s, time.UTC)
		return d
	}
	cases := []struct {
		m, d  int
		leap  bool
		today string
		want  string
	}{
		{8, 15, false, "2026-06-01", "2026-09-25"},  // 平月：当年内
		{8, 15, false, "2026-10-01", "2027-09-15"},  // 平月：滚到次年
		{5, 15, false, "2026-06-01", "2026-06-29"},  // 平月：当年内
		{1, 1, false, "1900-01-31", "1900-01-31"},   // 平月：当天= occurrence（含今天）
		{12, 15, false, "2026-06-01", "2027-01-22"}, // 平月：腊月跨年（用换算结果的公历年）
		{12, 15, false, "2027-06-01", "2028-01-11"}, // 平月：腊月跨年次年
		{11, 3, false, "2026-06-01", "2026-12-11"},  // 平月：冬月年内
		{6, 1, false, "2025-05-01", "2025-06-25"},   // 平六月（2025 是闰六月年但 leap=false 只查平六月）
		{6, 1, false, "2025-07-01", "2026-07-14"},   // 平六月已过 → 次年平六月（不再回退到闰六月）
		{6, 1, true, "2025-05-01", "2025-07-25"},    // 闰六月：2025 当年有此闰月
		{6, 1, true, "2025-08-01", "2036-07-23"},    // 闰六月：2025 闰六月已过，下一次闰六年在 2036
		{5, 1, true, "2028-04-01", "2028-06-23"},    // 闰五月：2028 当年有此闰月
		{5, 1, true, "2028-07-01", "2039-06-22"},    // 闰五月：2028 闰五月已过，下一次闰五月在 2039
		{8, 15, false, "2101-01-01", ""},            // 平月：超表尾返回空
	}
	for _, c := range cases {
		got := NextLunarOccurrence(c.m, c.d, c.leap, today(c.today))
		if got != c.want {
			t.Errorf("NextLunarOccurrence(%d,%d,leap=%v,%s) = %q, want %q", c.m, c.d, c.leap, c.today, got, c.want)
		}
	}
}
