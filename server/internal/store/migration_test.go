package store

import (
	"testing"

	_ "modernc.org/sqlite"
)

// TestFullSchema 验证压缩版 001_init.sql 一次建出的最新库结构：
// 覆盖 011/012/013 的最终约束（kind 收 audio、一文件一相册、
// anniversaries.type、chat_messages.media、webhook_channels 空间级），
// 以及模型网关数据模型（model_providers/provider_models/output_styles/app_settings，
// 内置风格种子与全局默认风格）。
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
	// 手动相册记创建者；月度相册由系统生成，created_by 留 NULL（可空约束）
	albumSeed := []string{
		`INSERT INTO albums (type, name, created_by) VALUES ('manual', '甲', 1)`,
		`INSERT INTO albums (type, name, created_by) VALUES ('manual', '乙', 1)`,
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

	// 不变式 3：anniversaries.type（012 收敛）+ created_by 非空
	if _, err := db.Exec(`INSERT INTO anniversaries (name, date, type, created_by) VALUES ('生日', '1996-05-15', 'lunar', 1)`); err != nil {
		t.Fatalf("lunar type must be accepted: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO anniversaries (name, date, type, created_by) VALUES ('周年', '2020-01-01', 'foo', 1)`); err == nil {
		t.Fatal("invalid anniversary type must be rejected")
	}
	// 纪念日 created_by 非空：缺列必须被拒（月相册不同，见上）
	if _, err := db.Exec(`INSERT INTO anniversaries (name, date, type) VALUES ('无主', '2020-02-02', 'solar')`); err == nil {
		t.Fatal("anniversaries.created_by must be NOT NULL")
	}
	// albums.created_by 可空：月度相册已在上方以 NULL 写入成功，此处再显式确认一次
	var mNull int
	if err := db.QueryRow(`SELECT COUNT(*) FROM albums WHERE type = 'monthly' AND created_by IS NULL`).Scan(&mNull); err != nil || mNull != 1 {
		t.Fatalf("monthly album created_by must be nullable: n=%d err=%v", mNull, err)
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

	// 不变式 6：模型网关表集 + 内置风格种子 + 全局默认风格（LLM 数据模型重构收敛）
	for _, table := range []string{"model_providers", "provider_models", "output_styles", "app_settings"} {
		if _, err := db.Exec("SELECT 1 FROM " + table + " LIMIT 0"); err != nil {
			t.Fatalf("%s must exist: %v", table, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO model_providers (api_type, name, base_url, default_model, sort_order) VALUES ('openai-chat', 'OpenAI', 'https://api.openai.com/v1', 'gpt-4o', 0)`); err != nil {
		t.Fatalf("valid provider must be accepted: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO model_providers (api_type, name, base_url) VALUES ('bogus', 'X', 'u')`); err == nil {
		t.Fatal("invalid api_type must be rejected")
	}
	if _, err := db.Exec(`INSERT INTO provider_models (provider_id, model_id, temperature) VALUES (1, 'gpt-4o', 0.7)`); err != nil {
		t.Fatalf("model entry must be accepted: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO provider_models (provider_id, model_id) VALUES (1, 'gpt-4o')`); err == nil {
		t.Fatal("UNIQUE(provider_id, model_id) must reject duplicate")
	}
	// DELETE 供应商级联删除模型条目（FK ON DELETE CASCADE）
	if _, err := db.Exec("DELETE FROM model_providers WHERE id = 1"); err != nil {
		t.Fatal(err)
	}
	var orphan int
	if err := db.QueryRow("SELECT COUNT(*) FROM provider_models WHERE provider_id = 1").Scan(&orphan); err != nil || orphan != 0 {
		t.Fatalf("orphan models = %d err=%v, want 0", orphan, err)
	}
	var builtinCount int
	// 内置 3 条（文艺/幽默/简洁，REQUIREMENTS §4.1 口径）
	if err := db.QueryRow("SELECT COUNT(*) FROM output_styles WHERE builtin = 1").Scan(&builtinCount); err != nil || builtinCount != 3 {
		t.Fatalf("builtin styles = %d err=%v, want 3", builtinCount, err)
	}
	var defStyle string
	if err := db.QueryRow("SELECT value FROM app_settings WHERE key = 'default_style_id'").Scan(&defStyle); err != nil || defStyle != "1" {
		t.Fatalf("default_style_id = %q err=%v, want '1'", defStyle, err)
	}

	// 不变式 7：后台真实化新增表集（AD8 PAT / L5 AI 调用统计 / AD5 恢复日志）
	for _, table := range []string{"personal_access_tokens", "ai_usage_logs", "restore_logs"} {
		if _, err := db.Exec("SELECT 1 FROM " + table + " LIMIT 0"); err != nil {
			t.Fatalf("%s must exist: %v", table, err)
		}
	}
	// PAT：tier/status 受 CHECK 约束，token_hash 唯一（明文不落库）
	if _, err := db.Exec(`INSERT INTO personal_access_tokens (name, user_id, tier, token_hash) VALUES ('Claude Desktop', 1, 'readonly', 'h1')`); err != nil {
		t.Fatalf("valid PAT must be accepted: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO personal_access_tokens (name, user_id, tier, token_hash) VALUES ('X', 1, 'bogus', 'h2')`); err == nil {
		t.Fatal("invalid PAT tier must be rejected")
	}
	if _, err := db.Exec(`INSERT INTO personal_access_tokens (name, user_id, tier, token_hash) VALUES ('Y', 1, 'readonly', 'h1')`); err == nil {
		t.Fatal("duplicate token_hash must violate UNIQUE")
	}
	// ai_usage_logs：latency/status 契约
	if _, err := db.Exec(`INSERT INTO ai_usage_logs (provider_id, model, prompt_tokens, completion_tokens, total_tokens, latency_ms, status) VALUES (1, 'gpt-4o', 10, 20, 30, 1320, 'success')`); err != nil {
		t.Fatalf("valid usage row must be accepted: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO ai_usage_logs (model, status) VALUES ('m', 'bogus')`); err == nil {
		t.Fatal("invalid usage status must be rejected")
	}
	// restore_logs：恢复日志永久保留
	if _, err := db.Exec(`INSERT INTO restore_logs (operator, summary) VALUES ('admin', '动态「周末出游」')`); err != nil {
		t.Fatalf("restore log must be accepted: %v", err)
	}

	// 不变式 8：审计日志补齐 AD7 字段（latency_ms 可空 + params 全文）
	if _, err := db.Exec(`INSERT INTO audit_logs (actor_type, actor_id, action, params_digest, params, result, latency_ms) VALUES ('ai', 1, 'search_posts', 'digest', 'query: "海边"', 'success', 1320)`); err != nil {
		t.Fatalf("audit row with latency/params must be accepted: %v", err)
	}
	var latency *int64
	if err := db.QueryRow(`SELECT latency_ms FROM audit_logs WHERE action = 'search_posts'`).Scan(&latency); err != nil || latency == nil || *latency != 1320 {
		t.Fatalf("audit latency_ms = %v err=%v, want 1320", latency, err)
	}
	// user 直接写库无耗时 → NULL（契约 latencyMs 仅 ai/pat 有值）
	if _, err := db.Exec(`INSERT INTO audit_logs (actor_type, actor_id, action) VALUES ('user', 1, 'create_post')`); err != nil {
		t.Fatalf("audit row without latency must be accepted: %v", err)
	}
	latency = nil
	if err := db.QueryRow(`SELECT latency_ms FROM audit_logs WHERE action = 'create_post'`).Scan(&latency); err != nil || latency != nil {
		t.Fatalf("user audit latency must be NULL, got %v err=%v", latency, err)
	}

	// 不变式 9：六类可回收表均具备删除者归属列（AD5 回收站「删除者」）
	for _, table := range []string{"posts", "comments", "media", "albums", "anniversaries", "map_points"} {
		if _, err := db.Exec("SELECT deleted_by, deleted_by_type FROM " + table + " LIMIT 0"); err != nil {
			t.Fatalf("%s must have deleted_by/deleted_by_type: %v", table, err)
		}
	}
	if _, err := db.Exec(`UPDATE posts SET deleted_at = '2026-09-12T00:00:00Z', deleted_by = 1, deleted_by_type = 'ai' WHERE id = 1`); err != nil {
		t.Fatalf("delete attribution must be writable: %v", err)
	}
	if _, err := db.Exec(`UPDATE posts SET deleted_by_type = 'bogus' WHERE id = 1`); err == nil {
		t.Fatal("invalid deleted_by_type must be rejected")
	}
}
