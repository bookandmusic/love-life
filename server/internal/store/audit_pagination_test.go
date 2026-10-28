package store

import (
	"fmt"
	"testing"
)

// B2：审计日志分页边界。audit_logs 永久保留、会持续增长，分页读必须精确。
func TestListAuditLogsPagination(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	ctx := t.Context()

	// 造 25 条：action 带序号便于断言顺序
	for i := 0; i < 25; i++ {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO audit_logs (actor_type, actor_id, action, params_digest, params, result)
			 VALUES ('user', 1, ?, '{}', '', 'success')`,
			fmt.Sprintf("act-%02d", i),
		); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	// 第 1 页：最新在前（act-24 起）
	page1, total, err := db.ListAuditLogs(ctx, 10, 0)
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if total != 25 {
		t.Fatalf("total 应为 25，实际 %d", total)
	}
	if len(page1) != 10 {
		t.Fatalf("第 1 页应 10 条，实际 %d", len(page1))
	}
	if page1[0].Action != "act-24" {
		t.Fatalf("最新在前：首条应 act-24，实际 %s", page1[0].Action)
	}
	if page1[9].Action != "act-15" {
		t.Fatalf("第 1 页末条应 act-15，实际 %s", page1[9].Action)
	}

	// 第 3 页：只剩 5 条，且不与前两页重叠
	page3, total3, err := db.ListAuditLogs(ctx, 10, 20)
	if err != nil {
		t.Fatalf("page3: %v", err)
	}
	if total3 != 25 {
		t.Fatalf("total 应恒为 25，实际 %d", total3)
	}
	if len(page3) != 5 {
		t.Fatalf("第 3 页应 5 条，实际 %d", len(page3))
	}
	if page3[0].Action != "act-04" || page3[4].Action != "act-00" {
		t.Fatalf("第 3 页应为 act-04..act-00，实际 %s..%s", page3[0].Action, page3[4].Action)
	}

	// 越界页：0 条，不报错
	beyond, _, err := db.ListAuditLogs(ctx, 10, 100)
	if err != nil {
		t.Fatalf("越界页不应报错：%v", err)
	}
	if len(beyond) != 0 {
		t.Fatalf("越界页应 0 条，实际 %d", len(beyond))
	}
}
