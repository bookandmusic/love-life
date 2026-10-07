package store

import (
	"testing"
	"time"
)

// 在一起天数的口径：首日算第 1 天；未设开始日或格式非法一律不给数字
// （调用方留空/省略，不得编造「0 天」）。
func TestDaysTogether(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.Local)
	if _, ok := DaysTogether("", now); ok {
		t.Fatal("未设开始日不得给出数字")
	}
	if _, ok := DaysTogether("2026/10/01", now); ok {
		t.Fatal("格式非法不得给出数字")
	}
	for _, tc := range []struct {
		start string
		want  int
	}{
		{"2026-10-15", 1}, // 首日算第 1 天
		{"2026-10-14", 2},
		{"2026-01-01", 288},
		{"2025-10-15", 366},
	} {
		got, ok := DaysTogether(tc.start, now)
		if !ok || got != tc.want {
			t.Fatalf("DaysTogether(%s) = %d,%v，want %d,true", tc.start, got, ok, tc.want)
		}
	}
}

// 下一次纪念日的挑选口径：无法换算日期的条目不参与（不得把无效项当成最近的一项）。
func TestPickNextAnniversarySkipsUnconvertible(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.Local)
	// 无效日期（解析不出）的条目 DaysUntil 为负；有效项在它之后
	list := []*Anniversary{
		{Name: "坏的", Date: "not-a-date", Type: "solar"},
		{Name: "三天后", Date: "2026-10-18", Type: "solar"},
	}
	a, ok := PickNextAnniversary(list, now)
	if !ok || a.Name != "三天后" {
		t.Fatalf("应跳过无法换算的条目，实得 %+v ok=%v", a, ok)
	}
	if _, ok := PickNextAnniversary(nil, now); ok {
		t.Fatal("空列表不得给出「下一个」")
	}
	if _, ok := PickNextAnniversary(list[:1], now); ok {
		t.Fatal("全部无法换算时不得给出「下一个」")
	}
}
