package store

import (
	"database/sql"
	"testing"
)

// newOrderTestDB 建一个只有一条用户行的空库（排序断言只关心 posts）。
func newOrderTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(t.Context(),
		`INSERT INTO users (username, password_hash, role) VALUES ('g', 'x', 'girl')`); err != nil {
		t.Fatal(err)
	}
	return db
}

// seedTimedPost 写入一条动态后回填内容时间——刻意让 id 顺序与内容时间顺序不一致：
// 迁移回填历史时间、补记往事都是这个形态（写入发生在后、内容时间在前）。
func seedTimedPost(t *testing.T, db *DB, content, createdAt string, pointID int64) int64 {
	t.Helper()
	p := &Post{AuthorID: 1, Content: content}
	if pointID != 0 {
		p.PointID = sql.NullInt64{Int64: pointID, Valid: true}
	}
	id, err := db.CreatePostWithMedia(t.Context(), p, nil)
	if err != nil {
		t.Fatalf("seed post: %v", err)
	}
	if _, err := db.ExecContext(t.Context(),
		`UPDATE posts SET created_at = ? WHERE id = ?`, createdAt, id); err != nil {
		t.Fatalf("set created_at: %v", err)
	}
	return id
}

func contentsOf(posts []*Post) []string {
	out := make([]string, 0, len(posts))
	for _, p := range posts {
		out = append(out, p.Content)
	}
	return out
}

// TestListPostsOrdersByContentTime 动态列表按**内容时间**倒序（PRODUCT「动态」：按时间倒序展示所有动态），
// 而不是按写入顺序（id）。id 在正常写入下与时间同序，一旦回填历史时间就会露馅——
// 曾经的表现是刚导入的旧内容被顶在时间轴最前。
func TestListPostsOrdersByContentTime(t *testing.T) {
	db := newOrderTestDB(t)
	// 写入顺序 id 1→2→3，内容时间却是 2024 → 2026 → 2025
	seedTimedPost(t, db, "旧事补记", "2024-05-04 05:00:00", 0)
	seedTimedPost(t, db, "最新一条", "2026-10-12 14:36:55", 0)
	seedTimedPost(t, db, "中间一条", "2025-06-23 04:00:00", 0)

	list, total, err := db.ListPosts(t.Context(), 1, 10, false)
	if err != nil {
		t.Fatalf("ListPosts: %v", err)
	}
	if total != 3 {
		t.Fatalf("total = %d, want 3", total)
	}
	want := []string{"最新一条", "中间一条", "旧事补记"}
	if got := contentsOf(list); len(got) != len(want) {
		t.Fatalf("len = %d, want %d（%v）", len(got), len(want), got)
	}
	for i, w := range want {
		if list[i].Content != w {
			t.Fatalf("按时间倒序应为 %v，实得 %v（第 %d 条是 %q）", want, contentsOf(list), i+1, list[i].Content)
		}
	}
}

// TestListPostsByPointOrdersByContentTime 同一点位下的动态（地图点位详情 / AI get_location_posts）
// 同样按内容时间倒序。
func TestListPostsByPointOrdersByContentTime(t *testing.T) {
	db := newOrderTestDB(t)
	pointID, err := db.CreatePoint(t.Context(), &MapPoint{Name: "老君山", Lat: 33.7, Lng: 111.6, Source: "manual", CreatedBy: 1})
	if err != nil {
		t.Fatalf("create point: %v", err)
	}
	// 写入顺序：先写内容时间较新的，后写较早的——使 id 倒序与时间倒序**相反**，
	// 否则这条用例在按 id 排的实现下也会通过（没有鉴别力）。
	seedTimedPost(t, db, "点位最新", "2026-01-02 04:00:00", pointID)
	seedTimedPost(t, db, "点位旧事", "2023-05-20 04:00:00", pointID)

	list, err := db.ListPostsByPoint(t.Context(), pointID, 10)
	if err != nil {
		t.Fatalf("ListPostsByPoint: %v", err)
	}
	want := []string{"点位最新", "点位旧事"}
	if got := contentsOf(list); len(got) != len(want) {
		t.Fatalf("len = %d, want %d（%v）", len(got), len(want), got)
	}
	for i, w := range want {
		if list[i].Content != w {
			t.Fatalf("按时间倒序应为 %v，实得 %v", want, contentsOf(list))
		}
	}
}
