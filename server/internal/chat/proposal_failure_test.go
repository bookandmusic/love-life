package chat

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/auth"
	"github.com/bookandmusic/love-life/server/internal/crypto"
	"github.com/bookandmusic/love-life/server/internal/llm"
	"github.com/bookandmusic/love-life/server/internal/store"
	"github.com/bookandmusic/love-life/server/internal/tools"
)

// captureChatSlog 把默认 logger 换到内存缓冲，返回快照读函数。
func captureChatSlog(t *testing.T) func() string {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return buf.String
}

// 启停状态读失败必须 fail-closed：读不到「哪些工具被停用」时按「都没有停用」放行，
// 等于放行所有已被后台停用的工具。
func TestInvokeToolFailsClosedWhenDisabledReadFails(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := NewService(db, []byte("0123456789abcdef0123456789abcdef"), &tools.Deps{DB: db})
	logged := captureChatSlog(t)

	if _, err := db.Exec(`DROP TABLE app_settings`); err != nil {
		t.Fatal(err)
	}

	var call llm.ToolCall
	call.ID = "call_1"
	call.Function.Name = "find_posts"
	call.Function.Arguments = `{}`
	out, err := svc.invokeTool(t.Context(), 1, 1, "turn_1", call, nil)

	if err == nil {
		t.Fatalf("启停状态读失败时必须拒绝执行，实得结果 %+v", out)
	}
	if !strings.Contains(err.Error(), "工具暂不可用") {
		t.Fatalf("错误文案 = %q，应为「工具暂不可用」（fail-closed）", err)
	}
	if got := logged(); !strings.Contains(got, "读取工具启停状态失败") {
		t.Fatalf("读取失败未进日志：%q", got)
	}
}

// fakeToolCallUpstream 回一条「调用 propose_reaction」的整块响应。
// 用 application/json 而非事件流：adapter 的整块兜底路径会把它按增量回放，
// 本轮流程因此照常走到提案分支（与真实模型声明 tool_calls 等价）。
func fakeToolCallUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"content": "",
					"tool_calls": []map[string]any{{
						"id": "call_1", "type": "function",
						"function": map[string]any{
							"name":      "propose_reaction",
							"arguments": `{"post_id":1,"like":true}`,
						},
					}},
				},
				"finish_reason": "tool_calls",
			}},
			"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// 提案落库失败必须显式收口：客户端要收到 error 与 turn.end（否则停在「正在…」），
// 失败原因进服务端日志，用户消息标中断可编辑重发。
func TestProposalSaveFailureEmitsErrorEvents(t *testing.T) {
	upstream := fakeToolCallUpstream(t)
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	key := []byte("0123456789abcdef0123456789abcdef")
	svc := NewService(db, key, &tools.Deps{DB: db})

	// 供应商 Key 用与 Service 同一把派生密钥加密（NewService 内部即 crypto.KeyFrom(seed)）
	enc, err := crypto.Encrypt(crypto.KeyFrom(key), "sk-test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateProvider(t.Context(), &store.ModelProvider{
		APIType: llm.APITypeOpenAIChat, Name: "fake", BaseURL: upstream.URL, APIKeyEnc: enc,
	}, []store.ProviderModel{{Kind: "chat", ModelID: "m"}}); err != nil {
		t.Fatal(err)
	}

	hash, err := auth.HashPassword("pw")
	if err != nil {
		t.Fatal(err)
	}
	uid, err := db.CreateUser(t.Context(), &store.User{Username: "girl", PasswordHash: hash, Role: "girl"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreatePostWithMedia(t.Context(), &store.Post{AuthorID: uid, Content: "要互动的动态"}, nil); err != nil {
		t.Fatal(err)
	}
	// 让提案落库失败：确认卡表没了
	if _, err := db.Exec(`DROP TABLE confirm_actions`); err != nil {
		t.Fatal(err)
	}

	logged := captureChatSlog(t)
	tm, err := auth.LoadOrCreateSecret(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	token, err := tm.IssueAccessToken(uid, "girl", "girl")
	if err != nil {
		t.Fatal(err)
	}
	h := auth.NewService(db, tm).RequireAuth(http.HandlerFunc(svc.Message))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/", strings.NewReader(`{"content":"点赞那条动态"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{
		"event: tool.start",
		`"code":"proposal_failed"`, // 用户看得到失败
		`"stop_reason":"error"`,    // 轮次正常收口，不会停在「正在…」
		`"status":"failed"`,        // 打开的工具步骤条被收口
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("SSE 缺少 %q：\n%s", want, body)
		}
	}
	if got := logged(); !strings.Contains(got, "提案落库失败") {
		t.Fatalf("落库失败未进日志：%q", got)
	}
	// 用户消息标中断（刷新后可编辑重发，不留孤儿）
	var parts string
	if err := db.QueryRow(`SELECT parts FROM chat_messages WHERE user_id = ? AND role = 'user' ORDER BY id LIMIT 1`, uid).Scan(&parts); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(parts, "interrupted") {
		t.Fatalf("用户消息未标中断：%s", parts)
	}
}
