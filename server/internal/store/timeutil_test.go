package store

import (
	"testing"
	"time"
)

// TestLocalDate 时间口径（timeutil.go）：库内 UTC 串按本地时区取日期。
func TestLocalDate(t *testing.T) {
	// 2026-09-12 17:06 UTC = 2026-09-13 01:06 CST(+8)：本地口径应为次日。
	utcEvening := "2026-09-12 17:06:00"
	got := LocalDate(utcEvening)
	want := time.Date(2026, 9, 12, 17, 6, 0, 0, time.UTC).Local().Format("2006-01-02")
	if got != want {
		t.Fatalf("LocalDate(%q) = %q, want %q", utcEvening, got, want)
	}
	// RFC3339 同样支持
	if got := LocalDate("2026-09-12T17:06:00Z"); got != want {
		t.Fatalf("LocalDate(RFC3339) = %q, want %q", got, want)
	}
	// 解析失败回退前 10 字符；空串返回空
	if got := LocalDate("not-a-time-string"); got != "not-a-time" {
		t.Fatalf("fallback = %q, want %q", got, "not-a-time")
	}
	if got := LocalDate(""); got != "" {
		t.Fatalf("empty = %q, want empty", got)
	}
}

// TestListPostsOnMonthDayLocalTZ 那年今日按**本地**月/日匹配（库内 UTC）。
func TestListPostsOnMonthDayLocalTZ(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	ctx := t.Context()
	if _, err := db.ExecContext(ctx, `INSERT INTO users (username, password_hash, role) VALUES ('g', 'x', 'girl')`); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	// 那年今日排除本年（strftime('%Y', created_at) < 当前年），故构造「去年同月同日」的动态；
	// 时间戳以 UTC 落库（与生产一致），匹配须按本地月日成立。
	lastYear := now.AddDate(-1, 0, 0)
	if _, err := db.ExecContext(ctx,
		`INSERT INTO posts (author_id, content, created_at) VALUES (1, 'local-today', ?)`, lastYear.UTC().Format("2006-01-02 15:04:05")); err != nil {
		t.Fatal(err)
	}
	// 再构造一条「去年 本地前一日」的动态，必须不被匹配
	other := lastYear.AddDate(0, 0, -1)
	if _, err := db.ExecContext(ctx,
		`INSERT INTO posts (author_id, content, created_at) VALUES (1, 'other-day', ?)`, other.UTC().Format("2006-01-02 15:04:05")); err != nil {
		t.Fatal(err)
	}

	hits, total, err := db.ListPostsOnMonthDay(ctx, 50, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != len(hits) {
		t.Fatalf("total = %d, want %d（计数与列表同 WHERE 下推）", total, len(hits))
	}
	if len(hits) != 1 || hits[0].Content != "local-today" {
		t.Fatalf("hits = %+v, want only local-today (local TZ semantics)", hits)
	}
}

// TestMonthlyAlbumLocalMonth 月度相册归属按本地月（库内 created_at 为 UTC）。
func TestMonthlyAlbumLocalMonth(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	ctx := t.Context()
	if _, err := db.ExecContext(ctx, `INSERT INTO users (username, password_hash, role) VALUES ('g', 'x', 'girl')`); err != nil {
		t.Fatal(err)
	}
	// 取「本地当月 1 日 00:30」对应的 UTC 时刻：在 +8 时区即上月最后一天 16:30 UTC。
	now := time.Now()
	localFirst := time.Date(now.Year(), now.Month(), 1, 0, 30, 0, 0, time.Local)
	monthKey := localFirst.Format("2006-01")
	postID, err := db.CreatePostWithMedia(ctx, &Post{AuthorID: 1, Content: "月初"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE posts SET created_at = ? WHERE id = ?`, localFirst.UTC().Format("2006-01-02 15:04:05"), postID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO media (post_id, kind, file_path, created_at) VALUES (?, 'photo', 'a.jpg', ?)`, postID, localFirst.UTC().Format("2006-01-02 15:04:05")); err != nil {
		t.Fatal(err)
	}

	media, err := db.ListTimelineMedia(ctx, localFirst.Format("2006"), monthKey, 10)
	if err != nil {
		t.Fatalf("ListTimelineMedia: %v", err)
	}
	if len(media) != 1 {
		t.Fatalf("local-month membership = %d, want 1（本地月初的 UTC 时刻应归本地月而非上月）", len(media))
	}
}
