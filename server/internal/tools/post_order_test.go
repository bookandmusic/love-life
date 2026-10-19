package tools

import (
	"testing"

	"github.com/bookandmusic/love-life/server/internal/notify"
)

// TestFindPostsOrderedByContentTime find_posts 的浏览路径与关键词路径都按**内容时间**倒序。
// 用例刻意让写入顺序与内容时间不同序（迁移回填历史时间、补记往事都是这个形态）——
// 曾按 id 排序时，刚导入的旧内容会被顶到结果最前，模型据此复述的「最近」是错的。
func TestFindPostsOrderedByContentTime(t *testing.T) {
	db := newToolDB(t)
	d := &Deps{DB: db, DP: notify.New(db)}
	old := seedPostAt(t, db, "旧事补记：厦门", "2024-05-04 05:00:00")
	latest := seedPostAt(t, db, "最新一条", "2026-10-12 14:36:55")
	mid := seedPostAt(t, db, "中间：厦门散步", "2025-06-23 04:00:00")

	// 浏览路径（query 留空）
	res := exec(t, "find_posts", d, 1, map[string]any{})
	got := recordIDs(res.UI.Records)
	want := []int64{latest, mid, old}
	if len(got) != len(want) {
		t.Fatalf("浏览路径命中 %d 条，want %d（%v）", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("浏览路径应按内容时间倒序 %v，实得 %v", want, got)
		}
	}

	// 关键词路径（未配置嵌入模型时回落 LIKE）
	res = exec(t, "find_posts", d, 1, map[string]any{"query": "厦门"})
	got = recordIDs(res.UI.Records)
	want = []int64{mid, old}
	if len(got) != len(want) {
		t.Fatalf("关键词路径命中 %d 条，want %d（%v）", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("关键词路径应按内容时间倒序 %v，实得 %v", want, got)
		}
	}
}
