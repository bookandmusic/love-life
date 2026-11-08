package store

import (
	"fmt"
	"testing"
	"time"
)

// 回收站列表分页：total 为分页前总数，limit/offset 按 deleted_at 倒序切片。
func TestListRecyclePagination(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	seedPostAuthor(t, db)
	ctx := t.Context()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	delAt := func(i int) string { return base.Add(time.Duration(i) * time.Second).Format("2006-01-02 15:04:05") }
	for i := 0; i < 7; i++ {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO posts (author_id, content, deleted_at) VALUES (1, ?, ?)`,
			fmt.Sprintf("post-%d", i), delAt(i)); err != nil {
			t.Fatalf("seed post %d: %v", i, err)
		}
	}
	// 第二张表（评论）也进同一条 UNION：总数必须跨表统计
	var livePost int64
	if err := db.QueryRow(`INSERT INTO posts (author_id, content) VALUES (1, 'live') RETURNING id`).Scan(&livePost); err != nil {
		t.Fatalf("seed live post: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO comments (post_id, author_id, content, deleted_at) VALUES (?, 1, 'c', ?)`,
		livePost, delAt(100)); err != nil {
		t.Fatalf("seed comment: %v", err)
	}

	items, total, err := db.ListRecycle(ctx, 3, 0)
	if err != nil {
		t.Fatalf("ListRecycle: %v", err)
	}
	if total != 8 {
		t.Fatalf("total = %d, want 8（跨表计数）", total)
	}
	if len(items) != 3 {
		t.Fatalf("len(items) = %d, want 3", len(items))
	}
	if items[0].DeletedAt != delAt(100) || items[2].DeletedAt != delAt(5) {
		t.Fatalf("排序不符：%s ... %s", items[0].DeletedAt, items[2].DeletedAt)
	}

	// 末页：只剩 1 条，total 不变
	items, total, err = db.ListRecycle(ctx, 3, 7)
	if err != nil {
		t.Fatalf("ListRecycle last page: %v", err)
	}
	if total != 8 || len(items) != 1 || items[0].DeletedAt != delAt(0) {
		t.Fatalf("末页 = len %d total %d，want 1/8", len(items), total)
	}

	// 非法 limit/offset 兜底：limit<=0 用默认页大小、offset<0 归 0
	items, total, err = db.ListRecycle(ctx, 0, -5)
	if err != nil {
		t.Fatalf("ListRecycle fallback: %v", err)
	}
	if total != 8 || len(items) != 8 {
		t.Fatalf("非法参数兜底 = len %d total %d，want 8/8", len(items), total)
	}
}
