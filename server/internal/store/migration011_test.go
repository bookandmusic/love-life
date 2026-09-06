package store

import (
	"testing"

	_ "modernc.org/sqlite"
)

// TestFullSchema 验证压缩版 001_init.sql 一次建出的最新库结构：
// 覆盖 011/012/013 的最终约束（kind 收 audio、一文件一相册、
// anniversaries.type、chat_messages.media、webhook_channels 空间级）。
// 压缩后无 002~013 升级路径，此测试确保收敛后的单一迁移仍满足全部最终不变式。
func TestFullSchema(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// 迁移记录只有 001（无 002~013 残留）
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("schema_migrations rows = %d err=%v, want 1", n, err)
	}

	// 种子：两个用户、两条动态
	seed := []string{
		`INSERT INTO users (username, password_hash, role) VALUES ('a', 'x', 'girl')`,
		`INSERT INTO users (username, password_hash, role) VALUES ('b', 'x', 'boy')`,
		`INSERT INTO posts (author_id, content) VALUES (1, 'p1')`,
		`INSERT INTO posts (author_id, content) VALUES (2, 'p2')`,
	}
	for _, s := range seed {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("seed: %v: %v", s, err)
		}
	}

	// 不变式 1：media.kind CHECK 收 audio（011 收敛）
	if _, err := db.Exec(`INSERT INTO media (post_id, kind, file_path, duration) VALUES (1, 'audio', 'a/3.mp3', 3.5)`); err != nil {
		t.Fatalf("audio kind must be accepted: %v", err)
	}
	var dur float64
	if err := db.QueryRow(`SELECT duration FROM media WHERE file_path = 'a/3.mp3'`).Scan(&dur); err != nil || dur != 3.5 {
		t.Fatalf("duration persisted: %v err=%v", dur, err)
	}
	if _, err := db.Exec(`INSERT INTO media (post_id, kind, file_path) VALUES (1, 'weird', 'a/x.png')`); err == nil {
		t.Fatal("invalid kind must be rejected")
	}

	// 不变式 2：一文件一相册——UNIQUE(media_id)（011 收敛）
	var mediaID int64
	if err := db.QueryRow(`SELECT id FROM media WHERE file_path = 'a/3.mp3'`).Scan(&mediaID); err != nil {
		t.Fatal(err)
	}
	albumSeed := []string{
		`INSERT INTO albums (type, name) VALUES ('manual', '甲')`,
		`INSERT INTO albums (type, name) VALUES ('manual', '乙')`,
		`INSERT INTO albums (type, name, month_key) VALUES ('monthly', '2026-08', '2026-08')`,
	}
	for _, s := range albumSeed {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("seed album: %v: %v", s, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO album_items (album_id, media_id, added_by) VALUES (1, ?, 1)`, mediaID); err != nil {
		t.Fatalf("first ownership: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO album_items (album_id, media_id, added_by) VALUES (2, ?, 1)`, mediaID); err == nil {
		t.Fatal("duplicate media ownership must violate UNIQUE(media_id)")
	}

	// 不变式 3：anniversaries.type（012 收敛）
	if _, err := db.Exec(`INSERT INTO anniversaries (name, date, type) VALUES ('生日', '1996-05-15', 'lunar')`); err != nil {
		t.Fatalf("lunar type must be accepted: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO anniversaries (name, date, type) VALUES ('周年', '2020-01-01', 'foo')`); err == nil {
		t.Fatal("invalid anniversary type must be rejected")
	}

	// 不变式 4：chat_messages.media（013 收敛）
	if _, err := db.Exec(`INSERT INTO chat_messages (user_id, role, content, media) VALUES (1, 'user', 'hi', '[{\"name\":\"p.jpg\"}]')`); err != nil {
		t.Fatalf("media column must exist: %v", err)
	}
	var m string
	if err := db.QueryRow(`SELECT media FROM chat_messages WHERE id = 1`).Scan(&m); err != nil || m == "" {
		t.Fatalf("media persisted: %q err=%v", m, err)
	}

	// 不变式 5：webhook_channels 空间级——无 user_id 列（007 收敛）
	rows, err := db.Query(`PRAGMA table_info(webhook_channels)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dflt any
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		if name == "user_id" {
			t.Fatal("webhook_channels must not have user_id (space-level)")
		}
	}
}
