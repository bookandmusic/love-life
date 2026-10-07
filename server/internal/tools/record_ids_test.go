package tools

import "testing"

// TestRecordIDsSkipsPlaceCandidates 句柄只能装真实记录 id。
//
// 回归的缺陷：find_albums / find_anniversaries / find_places 此前不落句柄，
// 三类结果永远走不了点选引用；而 find_places 的离线地名候选以 ID: 0 表示
// 「建议你去建这个点」，把它收进句柄会让展开/批量删除带出不存在的 id
// （批量删除会整批失败乃至回滚）。
func TestRecordIDsSkipsPlaceCandidates(t *testing.T) {
	got := recordIDs([]UIRecord{
		{ID: 7, Kind: "place", Text: "杭州·西湖"},
		{ID: 0, Kind: "place", Text: "浙江·杭州·西湖", Badge: "候选"},
		{ID: 9, Kind: "place", Text: "上海·外滩"},
		{ID: 0, Kind: "place", Text: "江苏·苏州·平江路", Badge: "候选"},
	})
	want := []int64{7, 9}
	if len(got) != len(want) {
		t.Fatalf("recordIDs = %v, want %v（候选 ID=0 必须被过滤）", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("recordIDs = %v, want %v（保持展示顺序）", got, want)
		}
	}

	// 全是候选 → 无真实 id → 句柄应为空（不可被勾选引用）
	if ids := recordIDs([]UIRecord{{ID: 0}, {ID: 0}}); len(ids) != 0 {
		t.Fatalf("全候选应得空 id 列表，实得 %v", ids)
	}
	// 空输入不 panic
	if ids := recordIDs(nil); len(ids) != 0 {
		t.Fatalf("nil 应得空列表，实得 %v", ids)
	}
}
