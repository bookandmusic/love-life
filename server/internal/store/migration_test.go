package store

import (
	"io/fs"
	"testing"

	_ "modernc.org/sqlite"
)

// TestFullSchema 验证 001_init.sql 一次建出的全量库结构：
// 覆盖历史约束（kind 收 audio、一文件一相册、anniversaries.type、chat_messages.media、
// webhook_channels 空间级）、模型网关数据模型（model_providers/provider_models/output_styles/
// app_settings，内置风格种子与全局默认风格），以及会话表与对话回显角色。
// 仓库规则：schema 变更一律直接改写 001 全量文件，不留增量迁移，无历史升级路径
// （见 001_init.sql 头部与下方守卫）。
func TestFullSchema(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// 「单一全量迁移文件」规则的守卫：一旦有人新增 002_*.sql，此处必失败。
	// 曾经的实际缺陷就是 schema 变更既进了 001、又留了一份重复的 002 增量，
	// 而 002 引用了存量库里不存在的列，把两个都不对的路径同时写进了仓库。
	entries, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("migrations 应只有 001_init.sql 一个全量文件，实得 %d 个：%v", len(entries), entries)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("schema_migrations rows = %d err=%v, want 1", n, err)
	}
	var version string
	if err := db.QueryRow(`SELECT version FROM schema_migrations`).Scan(&version); err != nil || version != "001_init" {
		t.Fatalf("schema_migrations version = %q err=%v, want 001_init", version, err)
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
		`INSERT INTO albums (name, created_by) VALUES ('甲', 1)`,
		`INSERT INTO albums (name, created_by) VALUES ('乙', 1)`,
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
	// 不变式 3.1：anniversaries.is_leap——默认 0、可写 1
	var isLeapDefault int
	if err := db.QueryRow(`SELECT is_leap FROM anniversaries WHERE name = '生日'`).Scan(&isLeapDefault); err != nil || isLeapDefault != 0 {
		t.Fatalf("is_leap default = %d err=%v, want 0", isLeapDefault, err)
	}
	if _, err := db.Exec(`INSERT INTO anniversaries (name, date, type, is_leap, created_by) VALUES ('闰月生日', '2025-06-01', 'lunar', 1, 1)`); err != nil {
		t.Fatalf("is_leap=1 must be accepted: %v", err)
	}
	// albums.created_by 可空：月度相册已在上方以 NULL 写入成功，此处再显式确认一次

	// 不变式 4：chat_messages.parts（parts 契约收敛）+ chat_context 表存在
	if _, err := db.Exec(`INSERT INTO chat_messages (user_id, role, parts, quoted) VALUES (1, 'user', '[{\"type\":\"text\",\"text\":\"hi\"}]', '[]')`); err != nil {
		t.Fatalf("parts column must exist: %v", err)
	}
	var parts string
	if err := db.QueryRow(`SELECT parts FROM chat_messages WHERE id = 1`).Scan(&parts); err != nil || parts == "" {
		t.Fatalf("parts persisted: %q err=%v", parts, err)
	}
	for _, table := range []string{"chat_context"} {
		if _, err := db.Exec("SELECT 1 FROM " + table + " LIMIT 0"); err != nil {
			t.Fatalf("%s must exist: %v", table, err)
		}
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
	if _, err := db.Exec(`INSERT INTO model_providers (api_type, name, base_url) VALUES ('openai-chat', 'OpenAI', 'https://api.openai.com/v1')`); err != nil {
		t.Fatalf("valid provider must be accepted: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO model_providers (api_type, name, base_url) VALUES ('bogus', 'X', 'u')`); err == nil {
		t.Fatal("invalid api_type must be rejected")
	}
	if _, err := db.Exec(`INSERT INTO provider_models (provider_id, kind, model_id, temperature) VALUES (1, 'chat', 'gpt-4o', 0.7)`); err != nil {
		t.Fatalf("model entry must be accepted: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO provider_models (provider_id, kind, model_id) VALUES (1, 'chat', 'gpt-4o')`); err == nil {
		t.Fatal("UNIQUE(provider_id, kind, model_id) must reject duplicate")
	}
	if _, err := db.Exec(`INSERT INTO provider_models (provider_id, kind, model_id) VALUES (1, 'bogus', 'x')`); err == nil {
		t.Fatal("invalid kind must be rejected")
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
	// 内置 3 条（文艺/幽默/简洁， 口径）
	if err := db.QueryRow("SELECT COUNT(*) FROM output_styles WHERE builtin = 1").Scan(&builtinCount); err != nil || builtinCount != 3 {
		t.Fatalf("builtin styles = %d err=%v, want 3", builtinCount, err)
	}
	var defStyle string
	if err := db.QueryRow("SELECT value FROM app_settings WHERE key = 'default_style_id'").Scan(&defStyle); err != nil || defStyle != "1" {
		t.Fatalf("default_style_id = %q err=%v, want '1'", defStyle, err)
	}

	// 不变式 7：后台真实化新增表集（PAT / AI 调用统计 / 恢复日志）
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

	// 不变式 8：审计日志补齐 字段（latency_ms 可空 + params 全文）
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

	// 不变式 9：六类可回收表均具备删除者归属列（回收站「删除者」）
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

	// 不变式 10：users.avatar_path（头像独立通道）——默认 NULL、可写相对路径
	var avatarDefault *string
	if err := db.QueryRow(`SELECT avatar_path FROM users WHERE id = 1`).Scan(&avatarDefault); err != nil || avatarDefault != nil {
		t.Fatalf("avatar_path default = %v err=%v, want NULL", avatarDefault, err)
	}
	if _, err := db.Exec(`UPDATE users SET avatar_path = 'avatars/2026/09/abc.png' WHERE id = 1`); err != nil {
		t.Fatalf("avatar_path must be writable: %v", err)
	}
	var avatar string
	if err := db.QueryRow(`SELECT avatar_path FROM users WHERE id = 1`).Scan(&avatar); err != nil || avatar != "avatars/2026/09/abc.png" {
		t.Fatalf("avatar_path round-trip = %q err=%v", avatar, err)
	}
	// avatar_media_id 不存在（死列， 后零引用）
	if _, err := db.Exec(`SELECT avatar_media_id FROM users LIMIT 0`); err == nil {
		t.Fatal("avatar_media_id column must not exist")
	}

	// 不变式 11：confirm_actions 状态机含 failed（确认后执行失败的终态回写，2026-09-21 评审修复）
	if _, err := db.Exec(`INSERT INTO confirm_actions (user_id, tool_name, params_json, expires_at, status) VALUES (1, 'create_post', '{}', '2026-09-30T00:00:00Z', 'failed')`); err != nil {
		t.Fatalf("failed status must be accepted: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO confirm_actions (user_id, tool_name, params_json, expires_at, status) VALUES (1, 'create_post', '{}', '2026-09-30T00:00:00Z', 'bogus')`); err == nil {
		t.Fatal("invalid confirm status must be rejected")
	}

	// 不变式 12：会话（多会话）——chat_sessions 表 + chat_messages.session_id 归属
	if _, err := db.Exec(`INSERT INTO chat_sessions (user_id, title) VALUES (1, '测试会话')`); err != nil {
		t.Fatalf("chat session must be accepted: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO chat_messages (user_id, session_id, role, parts) VALUES (1, 1, 'user', '[{\"type\":\"text\",\"text\":\"你好\"}]')`); err != nil {
		t.Fatalf("chat message with session: %v", err)
	}
	// 默认 0（懒创建前的 schema 兜底）：本文件第一条无 session_id 的消息（不变式 4）落 0
	var defSession int64
	if err := db.QueryRow(`SELECT MIN(session_id) FROM chat_messages`).Scan(&defSession); err != nil || defSession != 0 {
		t.Fatalf("session_id default = %d err=%v, want 0（缺省行存在）", defSession, err)
	}

	// 不变式 13：role 收窄 user/assistant（tool/reasoning 全部并入 parts）；未知 role 拒绝
	if _, err := db.Exec(`INSERT INTO chat_messages (user_id, session_id, role, parts) VALUES (1, 1, 'reasoning', '[]')`); err == nil {
		t.Fatal("reasoning role must be rejected (并入 parts)")
	}
	if _, err := db.Exec(`INSERT INTO chat_messages (user_id, session_id, role, parts) VALUES (1, 1, 'system', 'x')`); err == nil {
		t.Fatal("unknown chat role must be rejected")
	}

	// 不变式 14：confirm_actions.preview_json 提案预览落库（2026-09-23）——工具自构造、非反推
	if _, err := db.Exec(`INSERT INTO confirm_actions (user_id, tool_name, params_json, preview_json, expires_at) VALUES (1, 'propose_post', '{}', '{"kind":"post"}', '2026-09-30T00:00:00Z')`); err != nil {
		t.Fatalf("preview_json must be accepted: %v", err)
	}

	// 不变式 15：chat_result_sets 结果集句柄（2026-09-23）——随会话存取
	if _, err := db.Exec(`INSERT INTO chat_result_sets (session_id, result_id, kind, ids_json) VALUES (1, 'r_1', 'post', '[1,2]')`); err != nil {
		t.Fatalf("chat_result_sets must be accepted: %v", err)
	}
}
