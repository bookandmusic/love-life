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

// TestDBTimeRoundTrip 库内时间统一写法：写出的形态与 SQLite datetime('now') 同形，
// 且解析同时接受规范形态与历史 RFC3339 行。
func TestDBTimeRoundTrip(t *testing.T) {
	now := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	got := FormatDBTime(now)
	if want := "2026-10-01 10:00:00"; got != want {
		t.Fatalf("FormatDBTime = %q, want %q（须与 datetime('now') 同形，否则字符串比较会失真）", got, want)
	}
	// 规范形态可解析
	if back, err := ParseDBTime(got); err != nil || !back.Equal(now) {
		t.Fatalf("ParseDBTime(%q) = %v, %v", got, back, err)
	}
	// 历史 RFC3339 行可解析（老库升级后仍读得出）
	legacy := "2026-10-01T10:00:00Z"
	if back, err := ParseDBTime(legacy); err != nil || !back.Equal(now) {
		t.Fatalf("ParseDBTime(RFC3339 %q) = %v, %v（存量行必须可读）", legacy, back, err)
	}
	// 非 UTC 输入按 UTC 归一
	local := time.Date(2026, 10, 1, 18, 0, 0, 0, time.FixedZone("CST", 8*3600))
	if got := FormatDBTime(local); got != "2026-10-01 10:00:00" {
		t.Fatalf("FormatDBTime(带时区) = %q, want UTC 归一结果", got)
	}
	// 非法串报错（不静默返回零值）
	if _, err := ParseDBTime("not-a-time"); err == nil {
		t.Fatal("ParseDBTime 对非法串应报错")
	}
}

// insertPendingCard 造一条「带卡片的助手消息 + pending 提案」，expiresAt 由调用方给定字符串。
func insertPendingCard(t *testing.T, db *DB, sessionID int64, expiresAt string) {
	t.Helper()
	ctx := t.Context()
	res, err := db.ExecContext(ctx,
		`INSERT INTO confirm_actions (user_id, tool_name, params_json, preview_json, status, expires_at)
		 VALUES (1, 'propose_post', '{}', '{}', 'pending', ?)`, expiresAt)
	if err != nil {
		t.Fatal(err)
	}
	cardID, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO chat_messages (user_id, session_id, role, parts, card_id) VALUES (1, ?, 'assistant', '[]', ?)`,
		sessionID, cardID); err != nil {
		t.Fatal(err)
	}
}

// TestCountPendingProposalsExcludesExpired 过期判定不得因时间串格式而失真。
//
// 回归的缺陷：expires_at 曾写 RFC3339（`2026-10-01T10:00:00Z`），而比较是
// `expires_at > datetime('now')` 的**字符串比较**——第 11 位 `'T'(0x54) > ' '(0x20)`
// 恒真，于是**同一天内已过期**的卡片仍被算作未过期 pending，导致删会话被误拒 409。
func TestCountPendingProposalsExcludesExpired(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	ctx := t.Context()
	if _, err := db.ExecContext(ctx, `INSERT INTO users (username, password_hash, role) VALUES ('g', 'x', 'girl')`); err != nil {
		t.Fatal(err)
	}
	sid, err := db.CreateChatSession(ctx, 1, "s")
	if err != nil {
		t.Fatal(err)
	}

	// 未过期（+15 分钟）→ 算作 pending
	insertPendingCard(t, db, sid, FormatDBTime(time.Now().Add(15*time.Minute)))
	// 已过期（-1 分钟，且与「现在」同一天——这正是原缺陷失效的场景）→ 不算
	insertPendingCard(t, db, sid, FormatDBTime(time.Now().Add(-1*time.Minute)))

	n, err := db.CountPendingProposals(ctx, 1, sid)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("未过期 pending 数 = %d，want 1（同一天内已过期的卡片必须被排除，否则删会话误拒 409）", n)
	}
}

// TestCountPendingProposalsLegacyRFC3339 存量行（RFC3339）也要被判对：
// 比较处用 datetime() 归一化后，过期与未过期都不再受格式影响。
func TestCountPendingProposalsLegacyRFC3339(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	ctx := t.Context()
	if _, err := db.ExecContext(ctx, `INSERT INTO users (username, password_hash, role) VALUES ('g', 'x', 'girl')`); err != nil {
		t.Fatal(err)
	}
	sid, err := db.CreateChatSession(ctx, 1, "s")
	if err != nil {
		t.Fatal(err)
	}
	// 存量过期行
	insertPendingCard(t, db, sid, time.Now().UTC().Add(-1*time.Minute).Format(time.RFC3339))
	n, err := db.CountPendingProposals(ctx, 1, sid)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("存量 RFC3339 过期行被算作 pending（n=%d），datetime() 归一化未生效", n)
	}
	// 存量未过期行
	insertPendingCard(t, db, sid, time.Now().UTC().Add(10*time.Minute).Format(time.RFC3339))
	n, err = db.CountPendingProposals(ctx, 1, sid)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("存量 RFC3339 未过期行应算 pending，n=%d", n)
	}
}

// TestWritesUseDBTimeLayout 真实写入路径必须落 datetime 形态（与 datetime('now') 同形）。
//
// 这条守卫独立于「比较处归一化」：即使比较都包了 datetime()，
// 若某处又写回 RFC3339，任何未包归一化的字符串比较都会重新失真——
// 库内时间列一律 datetime 形态是 schema 级约定，须逐写入点守住。
func TestWritesUseDBTimeLayout(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	ctx := t.Context()
	if _, err := db.ExecContext(ctx, `INSERT INTO users (username, password_hash, role) VALUES ('g', 'x', 'girl')`); err != nil {
		t.Fatal(err)
	}

	// 提案有效期：曾写 RFC3339，使「同一天已过期」被判未过期（删会话误拒 409）
	if _, err := db.CreateConfirmAction(ctx, 1, "propose_post", "{}", "{}"); err != nil {
		t.Fatal(err)
	}
	var expires string
	if err := db.QueryRowContext(ctx, `SELECT expires_at FROM confirm_actions ORDER BY id DESC LIMIT 1`).Scan(&expires); err != nil {
		t.Fatal(err)
	}
	assertDBTimeLayout(t, "confirm_actions.expires_at", expires)

	// 刷新令牌过期时间：清理任务做字符串比较
	if err := db.SaveRefreshToken(ctx, 1, "hash-x", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var tokExpires string
	if err := db.QueryRowContext(ctx, `SELECT expires_at FROM refresh_tokens ORDER BY id DESC LIMIT 1`).Scan(&tokExpires); err != nil {
		t.Fatal(err)
	}
	assertDBTimeLayout(t, "refresh_tokens.expires_at", tokExpires)

	// 读回也必须是同一个时刻（写读闭环）
	tok, err := db.GetRefreshToken(ctx, "hash-x")
	if err != nil {
		t.Fatalf("GetRefreshToken: %v（写入形态变更后读侧须同步兼容）", err)
	}
	if tok.ExpiresAt.IsZero() {
		t.Fatal("读回的过期时间为零值")
	}
}

func assertDBTimeLayout(t *testing.T, column, raw string) {
	t.Helper()
	if raw == "" {
		t.Fatalf("%s 为空", column)
	}
	if _, err := time.Parse(dbTimeLayout, raw); err != nil {
		t.Fatalf("%s = %q 不是库内规范形态 %q（写成了 RFC3339 会让字符串比较失真）",
			column, raw, dbTimeLayout)
	}
}
