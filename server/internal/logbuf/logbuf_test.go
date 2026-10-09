package logbuf

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// 服务日志缓冲：环形覆盖、增量拉取、以及**密钥不打码就出不去**。
//
// 脱敏不是防攻击，而是别把凭据送进浏览器：stdout 里已有全量日志，网页没有理由多一份明文 Key。

func TestBufferRingOverwritesOldest(t *testing.T) {
	b := New(3)
	for _, m := range []string{"a", "b", "c", "d"} {
		b.Append(Entry{Msg: m})
	}
	got := b.Since(0)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3（容量上限）", len(got))
	}
	// 最旧的 a 被覆盖，剩下 b/c/d，且 Seq 单调
	want := []string{"b", "c", "d"}
	for i, e := range got {
		if e.Msg != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, e.Msg, want[i])
		}
	}
	if got[0].Seq >= got[1].Seq || got[1].Seq >= got[2].Seq {
		t.Errorf("Seq 必须严格递增：%d %d %d", got[0].Seq, got[1].Seq, got[2].Seq)
	}
}

func TestBufferSinceIsIncremental(t *testing.T) {
	b := New(10)
	b.Append(Entry{Msg: "one"})
	b.Append(Entry{Msg: "two"})
	last := b.LatestSeq()
	b.Append(Entry{Msg: "three"})

	got := b.Since(last)
	if len(got) != 1 || got[0].Msg != "three" {
		t.Fatalf("增量拉取应只给新条目，实得 %+v", got)
	}
	if len(b.Since(b.LatestSeq())) != 0 {
		t.Error("拉到最新后不应再有条目")
	}
}

func TestRedactCredentialKeys(t *testing.T) {
	// 必须打码：值本身是凭据
	mustRedact := []string{
		"api_key", "apikey", "key", "token", "secret", "password", "passwd",
		"authorization", "cookie", "credential", "sig", "signature",
		"refresh_token", "jwt_secret", "tianditu_key", "media_token",
		"password_hash", "signature_delta", "provider.api_key",
	}
	for _, k := range mustRedact {
		if !redact(k) {
			t.Errorf("%q 应打码", k)
		}
	}
}

// token 计数类键名**不得**打码：它们是排障与产品指标（仪表盘的 Token 消耗）都依赖的信息。
// 用子串匹配 会把 max_tokens / prompt_tokens 一并抹掉——这正是本测试守住的回归。
func TestRedactKeepsTokenCounts(t *testing.T) {
	mustKeep := []string{
		"max_tokens", "prompt_tokens", "input_tokens", "output_tokens",
		"total_tokens", "completion_tokens", "budget_tokens",
		"model", "provider_id", "session_id", "err", "count", "addr", "channel",
	}
	for _, k := range mustKeep {
		if redact(k) {
			t.Errorf("%q 不该打码（是计数/上下文，不是凭据）", k)
		}
	}
}

// TestHandlerRedactsAndForwards 端到端：写一条带 Key 的日志，
// 缓冲里应是打码值，而下游（stdout）照旧拿到原文。
func TestHandlerRedactsAndForwards(t *testing.T) {
	buf := New(10)
	var out bytes.Buffer
	next := slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(NewHandler(next, buf))

	logger.Warn("provider key decrypt failed", "api_key", "sk-super-secret-value", "provider_id", 7)

	got := buf.Since(0)
	if len(got) != 1 {
		t.Fatalf("应捕获 1 条，实得 %d", len(got))
	}
	e := got[0]
	if e.Level != "WARN" {
		t.Errorf("Level = %q, want WARN", e.Level)
	}
	if e.Attrs["api_key"] != Redacted {
		t.Errorf("api_key 未打码：%q", e.Attrs["api_key"])
	}
	if strings.Contains(e.Attrs["api_key"], "super-secret") {
		t.Error("密钥明文进入了缓冲")
	}
	// 非敏感上下文照旧保留（排障需要）
	if e.Attrs["provider_id"] != "7" {
		t.Errorf("provider_id = %q, want 7", e.Attrs["provider_id"])
	}
	// 下游（stdout）不受脱敏影响
	if !strings.Contains(out.String(), "sk-super-secret-value") {
		t.Error("下游 stdout 应照旧拿到原文（脱敏只作用于缓冲）")
	}
}

// TestEntryIsJSONSerializable 守住前端契约：Entry 必须能直接 JSON 化（含 attrs 省略）。
func TestEntryIsJSONSerializable(t *testing.T) {
	b := New(4)
	b.Append(Entry{Time: "2026-10-09 00:18:26", Level: "ERROR", Msg: "模型调用失败", Attrs: map[string]string{"provider_id": "16"}})
	b.Append(Entry{Time: "2026-10-09 00:18:27", Level: "INFO", Msg: "server listening"})

	raw, err := json.Marshal(b.Since(0))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back []Entry
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(back) != 2 || back[0].Attrs["provider_id"] != "16" {
		t.Fatalf("往返不一致：%s", string(raw))
	}
	if back[1].Attrs != nil {
		t.Error("无 attrs 的条目应省略该字段")
	}
}

// TestHandlerEnabledDelegates 守住「不改变下游过滤级别」：缓冲不应让 DEBUG 漏出来。
func TestHandlerEnabledDelegates(t *testing.T) {
	buf := New(4)
	next := slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelWarn})
	h := NewHandler(next, buf)
	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("下游只收 Warn 以上，Handler 不该自行放行 Info")
	}
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Error("下游收 Error，Handler 应放行")
	}
}

func TestWithGroupFlattensWithPrefix(t *testing.T) {
	buf := New(4)
	logger := slog.New(NewHandler(slog.NewTextHandler(&bytes.Buffer{}, nil), buf))
	logger.WithGroup("upstream").WithGroup("auth").Warn("failed", "api_key", "sk-x", "status", 401)

	got := buf.Since(0)
	if len(got) != 1 {
		t.Fatalf("应捕获 1 条，实得 %d", len(got))
	}
	if got[0].Attrs["upstream.auth.api_key"] != Redacted {
		t.Errorf("嵌套组的敏感键未打码：%+v", got[0].Attrs)
	}
	if got[0].Attrs["upstream.auth.status"] != "401" {
		t.Errorf("嵌套组的普通键应保留：%+v", got[0].Attrs)
	}
}

// TestNewHandlerRejectsNil 守住「别把 Default 的 handler 包进来」这个坑：
// nil 回落成 slog.Default().Handler() 会造出自锁（main.go 有事故注释），故这里直接 panic。
func TestNewHandlerRejectsNil(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("next 为 nil 应 panic，而不是回落到 Default（那会自锁）")
		}
	}()
	NewHandler(nil, New(4))
}
