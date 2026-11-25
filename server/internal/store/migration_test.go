package store

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// TestFullSchema 验证 schema 基线 + 增量文件建出的库结构：
// 覆盖结构约束（kind 收 audio、一文件一相册、anniversaries.type、chat_messages.media、
// webhook_channels 投递归属）、模型网关数据模型（model_providers/provider_models/output_styles/
// app_settings，内置风格种子与全局默认风格），以及会话表与对话回显角色。
// 仓库规则：001_init.sql 是全量基线；schema 变更追加同级 NNN_*.sql，由 migrate.go
// 按版本顺序应用——已部署的库据此升级，不重建、不丢数据。
func TestFullSchema(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// 「单一基线 + 递增增量」规则的守卫：001 是全量基线，其余按版本号连续递增（002、003…），
	// 每个文件只应用一次。禁止出现重号文件——两处同时演进会出现「002 引用库里不存在的列」。
	entries, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("migrations 至少要有 001_init.sql")
	}
	for i, p := range entries {
		want := fmt.Sprintf("migrations/%03d", i+1)
		if !strings.HasPrefix(p, want) {
			t.Fatalf("迁移文件须自 001 连续递增：第 %d 个是 %s，want 前缀 %s", i+1, p, want)
		}
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil || n != len(entries) {
		t.Fatalf("schema_migrations rows = %d err=%v, want %d", n, err, len(entries))
	}
	var baseline string
	if err := db.QueryRow(`SELECT version FROM schema_migrations ORDER BY version LIMIT 1`).Scan(&baseline); err != nil || baseline != "001_init" {
		t.Fatalf("schema_migrations 首版 = %q err=%v, want 001_init", baseline, err)
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

	// 不变式 5：webhook_channels.owner_id——可空投递归属（002 收敛）；
	// NULL = 空间级渠道（群 / 共享），非空 = 该用户的私人渠道
	rows, err := db.Query(`PRAGMA table_info(webhook_channels)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var hasOwner bool
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dflt any
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		if name == "owner_id" {
			hasOwner = true
			if notnull != 0 {
				t.Fatal("webhook_channels.owner_id must be nullable (NULL = space-level)")
			}
		}
	}
	if !hasOwner {
		t.Fatal("webhook_channels must have owner_id (002)")
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
	if _, err := db.Exec(`INSERT INTO provider_models (provider_id, kind, model_id, max_tokens, thinking, thinking_levels, context_window) VALUES (1, 'chat', 'gpt-5.5', 8192, 'medium', 'off,low,medium,high', 400000)`); err != nil {
		t.Fatalf("model entry must be accepted: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO provider_models (provider_id, kind, model_id) VALUES (1, 'chat', 'gpt-5.5-mini')`); err != nil {
		t.Fatalf("params must be optional (defaults apply): %v", err)
	}
	// 参数面收敛：temperature 已删（与现役模型的思考参数互斥），列不存在即是最硬的守卫
	if _, err := db.Exec(`INSERT INTO provider_models (provider_id, kind, model_id, temperature) VALUES (1, 'chat', 'x')`); err == nil {
		t.Fatal("temperature column must be gone")
	}
	if _, err := db.Exec(`INSERT INTO provider_models (provider_id, kind, model_id) VALUES (1, 'chat', 'gpt-5.5')`); err == nil {
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

	// 不变式 11：confirm_actions 状态机含 failed（确认后执行失败的终态回写）
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

	// 不变式 14：confirm_actions.preview_json 提案预览落库——工具自构造、非反推
	if _, err := db.Exec(`INSERT INTO confirm_actions (user_id, tool_name, params_json, preview_json, expires_at) VALUES (1, 'propose_post', '{}', '{"kind":"post"}', '2026-09-30T00:00:00Z')`); err != nil {
		t.Fatalf("preview_json must be accepted: %v", err)
	}

	// 不变式 15：chat_result_sets 结果集句柄——随会话存取
	if _, err := db.Exec(`INSERT INTO chat_result_sets (session_id, result_id, kind, ids_json) VALUES (1, 'r_1', 'post', '[1,2]')`); err != nil {
		t.Fatalf("chat_result_sets must be accepted: %v", err)
	}
}

// 已部署的库（只有基线 001）能就地升级到最新 schema：加列不重建、不丢既有数据。
// 这是「增量文件」的核心承诺——没有这条守卫，某个 002 写成需要重建库的改动也没人发现。
func TestMigrateUpgradesExistingDBInPlace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "love-life.db")

	baseline, err := migrationFS.ReadFile("migrations/001_init.sql")
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	old, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open old db: %v", err)
	}
	if _, err := old.Exec(string(baseline)); err != nil {
		_ = old.Close()
		t.Fatalf("apply baseline: %v", err)
	}
	if _, err := old.Exec(`CREATE TABLE schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		_ = old.Close()
		t.Fatalf("create schema_migrations: %v", err)
	}
	if _, err := old.Exec(`INSERT INTO schema_migrations (version) VALUES ('001_init')`); err != nil {
		_ = old.Close()
		t.Fatalf("record baseline: %v", err)
	}
	// 老库里已有渠道：升级后必须还在，且归属为 NULL（空间级）
	if _, err := old.Exec(`INSERT INTO webhook_channels (name, method, url, events_json)
		VALUES ('老渠道', 'POST', 'https://example.invalid/hook', '["like"]')`); err != nil {
		_ = old.Close()
		t.Fatalf("seed old channel: %v", err)
	}
	if err := old.Close(); err != nil {
		t.Fatalf("close old db: %v", err)
	}

	db, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen with migrations: %v", err)
	}
	defer db.Close()

	list, err := db.ListWebhookChannels(context.Background())
	if err != nil {
		t.Fatalf("list channels: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("升级后应保留 1 个渠道，实得 %d", len(list))
	}
	if list[0].Name != "老渠道" || !list[0].IsShared() {
		t.Fatalf("老渠道应保留且默认为空间级：%+v", list[0])
	}
	var applied int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&applied); err != nil || applied != 2 {
		t.Fatalf("schema_migrations = %d err=%v, want 2", applied, err)
	}
}
