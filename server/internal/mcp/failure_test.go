package mcp

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// captureSlog 把默认 logger 换到内存缓冲，返回快照读函数。
func captureSlog(t *testing.T) func() string {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return buf.String
}

// 启停状态读失败必须 fail-closed：读不到「哪些工具被停用」时按「都没有停用」放行，
// 就等于放行所有已被后台停用的工具。
func TestDisabledToolsReadFailureFailsClosed(t *testing.T) {
	db, svc, _, _ := newEnv(t)
	ctx := t.Context()
	ro, _, err := db.CreatePAT(ctx, "ro_fail", 2, "readonly")
	if err != nil {
		t.Fatal(err)
	}
	// 让启停状态读不出来（app_settings 没了），其余能力照常——用于确认拒绝的是
	// fail-closed 分支，而不是「库整体不可用」导致的连带失败。
	if _, err := db.Exec(`DROP TABLE app_settings`); err != nil {
		t.Fatal(err)
	}

	// tools/list：不下发清单（否则客户端会以为全部工具都可用）
	rec := call(t, svc, ro, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	var listResp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
		t.Fatal(err)
	}
	if listResp.Error == nil {
		t.Fatalf("启停状态读失败时 tools/list 必须回错误，实得：%s", rec.Body.String())
	}
	if len(listResp.Result) != 0 {
		t.Fatalf("启停状态读失败时不得下发工具清单：%s", rec.Body.String())
	}

	// tools/call：拒绝执行
	rec = call(t, svc, ro, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"find_posts","arguments":{}}}`)
	var callResp struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &callResp); err != nil {
		t.Fatal(err)
	}
	if callResp.Error == nil || !strings.Contains(callResp.Error.Message, "工具暂不可用") {
		t.Fatalf("启停状态读失败时必须按「工具不可用」拒绝，实得：%s", rec.Body.String())
	}
	// 没有审计行 = 工具没有真的执行
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_logs`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("拒绝执行时不应留下审计行，实得 %d 条", n)
	}
}

// 工具执行错误不得把原文透给外部客户端：原文里有表名、SQL 与实现细节。
// 客户端只拿人话分类，原文进服务端日志。
func TestToolErrorTextDoesNotLeakRawError(t *testing.T) {
	db, svc, _, _ := newEnv(t)
	ctx := t.Context()
	ro, _, err := db.CreatePAT(ctx, "ro_err", 2, "readonly")
	if err != nil {
		t.Fatal(err)
	}
	logged := captureSlog(t)
	// 制造一个真实执行失败：posts 表没了，find_posts 会在查询层报错
	if _, err := db.Exec(`DROP TABLE posts`); err != nil {
		t.Fatal(err)
	}

	rec := call(t, svc, ro, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"find_posts","arguments":{"query":"海边"}}}`)
	body := rec.Body.String()
	var resp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v（%s）", err, body)
	}
	if !resp.Result.IsError {
		t.Fatalf("执行失败必须标 isError=true：%s", body)
	}
	text := resp.Result.Content[0].Text
	if text != "工具执行失败，请稍后重试" {
		t.Fatalf("对外文案 = %q，want 人话分类「工具执行失败，请稍后重试」", text)
	}
	for _, leak := range []string{"no such table", "posts", "SQL", "sqlite", "SELECT"} {
		if strings.Contains(text, leak) {
			t.Fatalf("对外文案泄露了内部细节 %q：%s", leak, text)
		}
	}
	// 原文必须落到服务端日志，否则排障无线索
	if got := logged(); !strings.Contains(got, "no such table") || !strings.Contains(got, "MCP 工具执行失败") {
		t.Fatalf("原始错误未进日志：%q", got)
	}
}

// 审计落库失败不能静默，也不能影响调用结果本身。
func TestAuditWriteFailureIsLogged(t *testing.T) {
	db, svc, _, _ := newEnv(t)
	ctx := t.Context()
	ro, _, err := db.CreatePAT(ctx, "ro_audit", 2, "readonly")
	if err != nil {
		t.Fatal(err)
	}
	logged := captureSlog(t)
	if _, err := db.Exec(`DROP TABLE audit_logs`); err != nil {
		t.Fatal(err)
	}

	rec := call(t, svc, ro, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"find_posts","arguments":{}}}`)
	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v（%s）", err, rec.Body.String())
	}
	if resp.Error != nil || len(resp.Result) == 0 {
		t.Fatalf("审计写失败不应影响调用结果：%s", rec.Body.String())
	}
	if got := logged(); !strings.Contains(got, "MCP 审计写入失败") {
		t.Fatalf("审计写失败必须告警：%q", got)
	}
}
