package chat

import "testing"

// 出提案（stop_reason=proposal）时不得再发收尾调用：提案 break 会跳过后面的 tool_calls
// （一轮一卡），它们在请求里没有回执，收尾请求会被上游拒——用户刚拿到卡就看到一句
// 「这次没能整理完」。
//
// 变异验证：把 shouldWrapUp 改回 `!answered || budgetHit` → 第 2 条用例变红。
func TestShouldWrapUp(t *testing.T) {
	tests := []struct {
		name       string
		answered   bool
		budgetHit  bool
		stopReason string
		want       bool
	}{
		{"未作答（!answered 短路，与预算无关）", false, false, "aborted", true},
		{"已作答 + 预算触顶 + 出提案 → 不收尾（卡片已是完整交付物）", true, true, "proposal", false},
		{"已作答 + 未触顶 + 正常回答 → 不收尾", true, false, "answer", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldWrapUp(tc.answered, tc.budgetHit, tc.stopReason); got != tc.want {
				t.Errorf("shouldWrapUp(%v, %v, %q) = %v, want %v",
					tc.answered, tc.budgetHit, tc.stopReason, got, tc.want)
			}
		})
	}
}
