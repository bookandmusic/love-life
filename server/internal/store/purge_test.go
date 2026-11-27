package store

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

// purgeSetDeleted 受控回拨 deleted_at（格式与库内 datetime('now') 同形，UTC）。
func purgeSetDeleted(t *testing.T, db *DB, table string, id int64, ts time.Time) {
	t.Helper()
	if _, err := db.Exec("UPDATE "+table+" SET deleted_at = ? WHERE id = ?", ts.UTC().Format("2006-01-02 15:04:05"), id); err != nil {
		t.Fatalf("set deleted_at %s#%d: %v", table, id, err)
	}
}

func purgeCount(t *testing.T, db *DB, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}

func purgeInsert(t *testing.T, db *DB, query string, args ...any) int64 {
	t.Helper()
	res, err := db.Exec(query, args...)
	if err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	return id
}

// 永久删除：仅超期删、恰好到期保留；子行无条件清理（含 live 评论）；媒体路径含缩略图。
func TestPurgeExpiredRecycle(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	seedPostAuthor(t, db) // user 1
	ctx := context.Background()

	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	cutoff := now.AddDate(0, 0, -30)
	expired := now.AddDate(0, 0, -44)
	recent := now.AddDate(0, 0, -3)

	// 过期动态：媒体（主文件+缩略图）+ 未软删的 live 评论 + 点赞 + 收藏
	p1 := purgeInsert(t, db, "INSERT INTO posts (author_id, content) VALUES (1, '过期动态')")
	purgeSetDeleted(t, db, "posts", p1, expired)
	m1 := purgeInsert(t, db, "INSERT INTO media (owner_type, owner_id, kind, file_path, thumb_path) VALUES ('post', ?, 'photo', 'media/a.jpg', 'media/a_thumb.jpg')", p1)
	purgeSetDeleted(t, db, "media", m1, expired)
	purgeInsert(t, db, "INSERT INTO comments (post_id, author_id, content) VALUES (?, 1, '过期动态下的 live 评论')", p1)
	purgeInsert(t, db, "INSERT INTO likes (post_id, user_id) VALUES (?, 1)", p1)
	purgeInsert(t, db, "INSERT INTO favorites (post_id, user_id) VALUES (?, 1)", p1)

	// 边界：恰好 30 天前 → 保留
	p2 := purgeInsert(t, db, "INSERT INTO posts (author_id, content) VALUES (1, '恰好到期')")
	purgeSetDeleted(t, db, "posts", p2, cutoff)

	// 未超期 → 保留
	p3 := purgeInsert(t, db, "INSERT INTO posts (author_id, content) VALUES (1, '未超期')")
	purgeSetDeleted(t, db, "posts", p3, recent)

	// 活跃动态 P4：挂着「自身超期的独立媒体」与「自身超期的评论」
	p4 := purgeInsert(t, db, "INSERT INTO posts (author_id, content) VALUES (1, '活跃动态')")
	m2 := purgeInsert(t, db, "INSERT INTO media (owner_type, owner_id, kind, file_path) VALUES ('post', ?, 'photo', 'media/b.jpg')", p4)
	purgeSetDeleted(t, db, "media", m2, expired)
	c2 := purgeInsert(t, db, "INSERT INTO comments (post_id, author_id, content) VALUES (?, 1, '自身超期的评论')", p4)
	purgeSetDeleted(t, db, "comments", c2, expired)

	// 相册：AL1 活跃（含 m1/m2）；AL2 过期（含活跃媒体 m3）
	al1 := purgeInsert(t, db, "INSERT INTO albums (name, created_by) VALUES ('活跃相册', 1)")
	al2 := purgeInsert(t, db, "INSERT INTO albums (name, created_by) VALUES ('过期相册', 1)")
	purgeSetDeleted(t, db, "albums", al2, expired)
	m3 := purgeInsert(t, db, "INSERT INTO media (owner_type, owner_id, kind, file_path) VALUES ('post', ?, 'photo', 'media/c.jpg')", p4)
	purgeInsert(t, db, "INSERT INTO album_items (album_id, media_id, added_by) VALUES (?, ?, 1)", al1, m1)
	purgeInsert(t, db, "INSERT INTO album_items (album_id, media_id, added_by) VALUES (?, ?, 1)", al1, m2)
	purgeInsert(t, db, "INSERT INTO album_items (album_id, media_id, added_by) VALUES (?, ?, 1)", al2, m3)

	// 纪念日 / 点位：各一过期一保留
	an1 := purgeInsert(t, db, "INSERT INTO anniversaries (name, date, created_by) VALUES ('过期纪念日', '2026-01-01', 1)")
	purgeSetDeleted(t, db, "anniversaries", an1, expired)
	an2 := purgeInsert(t, db, "INSERT INTO anniversaries (name, date, created_by) VALUES ('保留纪念日', '2026-02-02', 1)")
	purgeSetDeleted(t, db, "anniversaries", an2, recent)
	mp1 := purgeInsert(t, db, "INSERT INTO map_points (name, created_by) VALUES ('过期点位', 1)")
	purgeSetDeleted(t, db, "map_points", mp1, expired)
	mp2 := purgeInsert(t, db, "INSERT INTO map_points (name, created_by) VALUES ('保留点位', 1)")
	purgeSetDeleted(t, db, "map_points", mp2, recent)

	res, err := db.PurgeExpiredRecycle(ctx, cutoff)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}

	want := map[string]int64{"动态": 1, "评论": 2, "照片": 2, "相册": 1, "纪念日": 1, "地图点位": 1}
	if len(res.ByType) != len(want) {
		t.Errorf("ByType = %v, want keys %v", res.ByType, want)
	}
	for label, n := range want {
		if res.ByType[label] != n {
			t.Errorf("ByType[%s] = %d, want %d", label, res.ByType[label], n)
		}
	}

	joined := strings.Join(res.MediaPaths, ",")
	for _, p := range []string{"media/a.jpg", "media/a_thumb.jpg", "media/b.jpg"} {
		if !strings.Contains(joined, p) {
			t.Errorf("MediaPaths 缺 %s: %v", p, res.MediaPaths)
		}
	}
	if strings.Contains(joined, "media/c.jpg") {
		t.Errorf("活跃媒体不应进待删列表: %v", res.MediaPaths)
	}

	sum := res.Summary()
	for _, frag := range []string{"动态 1", "评论 2", "照片 2", "相册 1", "纪念日 1", "地图点位 1"} {
		if !strings.Contains(sum, frag) {
			t.Errorf("Summary = %q, 缺 %q", sum, frag)
		}
	}

	// 保留：恰好到期 P2、未超期 P3、活跃 P4；AL1、AN2、MP2、活跃媒体 M3
	if n := purgeCount(t, db, "SELECT COUNT(*) FROM posts"); n != 3 {
		t.Errorf("posts = %d, want 3", n)
	}
	if n := purgeCount(t, db, "SELECT COUNT(*) FROM comments"); n != 0 {
		t.Errorf("comments = %d, want 0（live 级联 + 自身超期）", n)
	}
	if n := purgeCount(t, db, "SELECT COUNT(*) FROM likes"); n != 0 {
		t.Errorf("likes = %d, want 0", n)
	}
	if n := purgeCount(t, db, "SELECT COUNT(*) FROM favorites"); n != 0 {
		t.Errorf("favorites = %d, want 0", n)
	}
	if n := purgeCount(t, db, "SELECT COUNT(*) FROM media"); n != 1 {
		t.Errorf("media = %d, want 1（仅活跃 M3）", n)
	}
	if n := purgeCount(t, db, "SELECT COUNT(*) FROM album_items"); n != 0 {
		t.Errorf("album_items = %d, want 0", n)
	}
	if n := purgeCount(t, db, "SELECT COUNT(*) FROM albums"); n != 1 {
		t.Errorf("albums = %d, want 1（活跃 AL1）", n)
	}
	if n := purgeCount(t, db, "SELECT COUNT(*) FROM anniversaries"); n != 1 {
		t.Errorf("anniversaries = %d, want 1", n)
	}
	if n := purgeCount(t, db, "SELECT COUNT(*) FROM map_points"); n != 1 {
		t.Errorf("map_points = %d, want 1", n)
	}
}

// 无超期内容：不报错，空结果，空摘要（调用方据此不写日志）。
func TestPurgeExpiredRecycleNothing(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	seedPostAuthor(t, db)

	res, err := db.PurgeExpiredRecycle(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if len(res.ByType) != 0 || len(res.MediaPaths) != 0 {
		t.Fatalf("res = %+v, want empty", res)
	}
	if res.Summary() != "" {
		t.Fatalf("Summary = %q, want empty", res.Summary())
	}
}

// 历史库可能混有 RFC3339 格式的 deleted_at（见 scripts/sql/fix-time-format.sql）；
// 裸字符串比较会因 T > 空格 把同日更早的时间误判为更晚而漏删，必须归一化比较。
func TestPurgeExpiredRecycleNormalizesLegacyTimeFormat(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	seedPostAuthor(t, db)

	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	cutoff := now.AddDate(0, 0, -30) // 2026-08-15 12:00:00

	// 同日 00:00（早于 cutoff）但写成 RFC3339：应被删
	old := purgeInsert(t, db, "INSERT INTO posts (author_id, content) VALUES (1, 'RFC3339 过期')")
	if _, err := db.Exec("UPDATE posts SET deleted_at = ? WHERE id = ?", "2026-08-15T00:00:00Z", old); err != nil {
		t.Fatalf("set legacy ts: %v", err)
	}
	// 同日 13:00（晚于 cutoff）：应保留
	keep := purgeInsert(t, db, "INSERT INTO posts (author_id, content) VALUES (1, 'RFC3339 未到期')")
	if _, err := db.Exec("UPDATE posts SET deleted_at = ? WHERE id = ?", "2026-08-15T13:00:00Z", keep); err != nil {
		t.Fatalf("set legacy ts: %v", err)
	}

	res, err := db.PurgeExpiredRecycle(context.Background(), cutoff)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if res.ByType["动态"] != 1 {
		t.Fatalf("ByType = %v, want 动态 1（RFC3339 同日更早者应删、更晚者应留）", res.ByType)
	}
	if n := purgeCount(t, db, "SELECT COUNT(*) FROM posts WHERE id = ?", keep); n != 1 {
		t.Fatalf("未到期的 RFC3339 行被误删")
	}
}

// 相册封面指向待删媒体时须置空，否则活跃相册封面变破图。
func TestPurgeExpiredRecycleNullsAlbumCover(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	seedPostAuthor(t, db)

	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	cutoff := now.AddDate(0, 0, -30)
	expired := now.AddDate(0, 0, -44)

	post := purgeInsert(t, db, "INSERT INTO posts (author_id, content) VALUES (1, '活跃动态')")
	m := purgeInsert(t, db, "INSERT INTO media (owner_type, owner_id, kind, file_path) VALUES ('post', ?, 'photo', 'media/cover.jpg')", post)
	purgeSetDeleted(t, db, "media", m, expired)
	al := purgeInsert(t, db, "INSERT INTO albums (name, created_by, cover_media_id) VALUES ('活跃相册', 1, ?)", m)

	if _, err := db.PurgeExpiredRecycle(context.Background(), cutoff); err != nil {
		t.Fatalf("purge: %v", err)
	}
	var cover sql.NullInt64
	if err := db.QueryRow("SELECT cover_media_id FROM albums WHERE id = ?", al).Scan(&cover); err != nil {
		t.Fatalf("read cover: %v", err)
	}
	if cover.Valid {
		t.Fatalf("封面应置空（媒体已永久删除），got %d", cover.Int64)
	}
}

// 每日清理的评论图路径：评论软删超期时，它名下的图必须一并物理删除。
//
// 这条守的是顺序依赖——清理顺序是 comments 先于 media，之后再上溯
// 「属于过期评论的媒体」就查不到评论了。实现为此先把待删媒体物化成临时表；
// 去掉那层物化，这里的图会变成永久孤儿（磁盘有文件、库里无引用）。
func TestPurgeExpiredCommentMedia(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	seedPostAuthor(t, db)
	ctx := context.Background()

	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	cutoff := now.AddDate(0, 0, -30)
	expired := now.AddDate(0, 0, -44)
	recent := now.AddDate(0, 0, -3)

	// 动态本身活着；其下有一条超期软删的评论，评论带两张图
	p := purgeInsert(t, db, "INSERT INTO posts (author_id, content) VALUES (1, '活着的动态')")
	c1 := purgeInsert(t, db, "INSERT INTO comments (post_id, author_id, content) VALUES (?, 1, '过期评论')", p)
	purgeSetDeleted(t, db, "comments", c1, expired)
	cm1 := purgeInsert(t, db, "INSERT INTO media (owner_type, owner_id, kind, file_path, thumb_path) VALUES ('comment', ?, 'photo', 'media/c1.jpg', 'media/c1_thumb.jpg')", c1)
	cm2 := purgeInsert(t, db, "INSERT INTO media (owner_type, owner_id, kind, file_path) VALUES ('comment', ?, 'photo', 'media/c2.jpg')", c1)
	purgeSetDeleted(t, db, "media", cm1, expired)
	purgeSetDeleted(t, db, "media", cm2, expired)

	// 对照：未超期的评论图必须保留
	c2 := purgeInsert(t, db, "INSERT INTO comments (post_id, author_id, content) VALUES (?, 1, '未到期评论')", p)
	purgeSetDeleted(t, db, "comments", c2, recent)
	cm3 := purgeInsert(t, db, "INSERT INTO media (owner_type, owner_id, kind, file_path) VALUES ('comment', ?, 'photo', 'media/keep.jpg')", c2)
	purgeSetDeleted(t, db, "media", cm3, recent)

	res, err := db.PurgeExpiredRecycle(ctx, cutoff)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}

	// 超期评论图：行没了，路径进了待删列表（含缩略图）
	if got := purgeCount(t, db, `SELECT COUNT(*) FROM media WHERE id = ?`, cm1); got != 0 {
		t.Errorf("过期评论图 cm1 应被物理删除，仍剩 %d 行", got)
	}
	if got := purgeCount(t, db, `SELECT COUNT(*) FROM media WHERE id = ?`, cm2); got != 0 {
		t.Errorf("过期评论图 cm2 应被物理删除，仍剩 %d 行", got)
	}
	want := map[string]bool{"media/c1.jpg": true, "media/c1_thumb.jpg": true, "media/c2.jpg": true}
	for _, p := range res.MediaPaths {
		delete(want, p)
	}
	if len(want) != 0 {
		t.Errorf("待删文件列表缺少：%v（实际 %v）", want, res.MediaPaths)
	}

	// 未超期的评论图必须还在
	if got := purgeCount(t, db, `SELECT COUNT(*) FROM media WHERE id = ?`, cm3); got != 1 {
		t.Errorf("未到期评论图不应被删除，实得 %d 行", got)
	}
	if got := purgeCount(t, db, `SELECT COUNT(*) FROM comments WHERE id = ?`, c2); got != 1 {
		t.Errorf("未到期评论不应被删除，实得 %d 行", got)
	}
}

// 动态超期时，连带删掉它下面的评论图（经 posts 上溯的那条分支）。
func TestPurgeExpiredPostCascadesCommentMedia(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	seedPostAuthor(t, db)
	ctx := context.Background()

	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	cutoff := now.AddDate(0, 0, -30)
	expired := now.AddDate(0, 0, -44)

	// 动态超期软删；其评论本身没被单独软删（级联删除只标动态，评论的 deleted_at 为空）
	p := purgeInsert(t, db, "INSERT INTO posts (author_id, content) VALUES (1, '过期动态')")
	purgeSetDeleted(t, db, "posts", p, expired)
	c := purgeInsert(t, db, "INSERT INTO comments (post_id, author_id, content) VALUES (?, 1, '随动态一起走')", p)
	m := purgeInsert(t, db, "INSERT INTO media (owner_type, owner_id, kind, file_path) VALUES ('comment', ?, 'photo', 'media/cm.jpg')", c)

	res, err := db.PurgeExpiredRecycle(ctx, cutoff)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	// 评论随动态删；评论图也要删（若不物化，评论已删就再也查不到这张图）
	if got := purgeCount(t, db, `SELECT COUNT(*) FROM comments WHERE id = ?`, c); got != 0 {
		t.Errorf("动态超期应级联删评论，仍剩 %d 行", got)
	}
	if got := purgeCount(t, db, `SELECT COUNT(*) FROM media WHERE id = ?`, m); got != 0 {
		t.Errorf("动态超期应级联删其评论图，仍剩 %d 行", got)
	}
	found := false
	for _, path := range res.MediaPaths {
		if path == "media/cm.jpg" {
			found = true
		}
	}
	if !found {
		t.Errorf("待删文件列表未包含评论图：%v", res.MediaPaths)
	}
}

// 清理任务会重复跑（启动补跑 + 每日定时），必须可重入：
// 待删媒体的 TEMP 表属于连接而非事务，第二轮不能因「表已存在」失败。
func TestPurgeExpiredRecycleIsRerunnable(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	seedPostAuthor(t, db)
	ctx := context.Background()

	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	cutoff := now.AddDate(0, 0, -30)
	expired := now.AddDate(0, 0, -44)

	p := purgeInsert(t, db, "INSERT INTO posts (author_id, content) VALUES (1, '过期动态')")
	purgeSetDeleted(t, db, "posts", p, expired)
	c := purgeInsert(t, db, "INSERT INTO comments (post_id, author_id, content) VALUES (?, 1, '评论')", p)
	purgeInsert(t, db, "INSERT INTO media (owner_type, owner_id, kind, file_path) VALUES ('comment', ?, 'photo', 'media/cm.jpg')", c)

	// 第一轮
	if _, err := db.PurgeExpiredRecycle(ctx, cutoff); err != nil {
		t.Fatalf("第一轮清理失败: %v", err)
	}
	// 第二轮：同一条连接上必须还能跑（TEMP 表已存在）
	if _, err := db.PurgeExpiredRecycle(ctx, cutoff); err != nil {
		t.Fatalf("第二轮清理失败（TEMP 表未清理）：%v", err)
	}
}
