package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/crypto"
)

// ---- 三协议假上游（native 形状脚本回放）+ 端到端测试 ----
// 断言口径与 openai-chat 基线（chat_test.go）一致：同一对话在四协议下
// 产生相同的 SSE 事件序列（协议差异全部被 adapter 吸收）。

// seedFakeProviderType 参数化协议的供应商直插库；model 为该供应商 default_model。
func seedFakeProviderType(t *testing.T, env *testEnv, apiType, model, baseURL string) {
	t.Helper()
	enc, err := crypto.Encrypt(crypto.KeyFrom(env.tm.Secret()), "sk-test")
	if err != nil {
		t.Fatalf("encrypt key: %v", err)
	}
	res, err := env.db.Exec(`INSERT INTO model_providers (api_type, name, base_url, api_key_enc) VALUES (?, 'fake', ?, ?)`, apiType, baseURL, enc)
	if err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	if model != "" {
		pid, _ := res.LastInsertId()
		if _, err := env.db.Exec(`INSERT INTO provider_models (provider_id, kind, model_id, chain_order) VALUES (?, 'chat', ?, 0)`, pid, model); err != nil {
			t.Fatalf("seed model: %v", err)
		}
	}
}

// fakeNativeModel 按给定包装函数回放脚本：wrapper 把脚本元素包成完整响应体。
type fakeNativeModel struct {
	server  *httptest.Server
	calls   atomic.Int32
	wrapper func(script string) string
}

func newFakeNativeModel(t *testing.T, wrapper func(string) string, script ...string) *fakeNativeModel {
	t.Helper()
	f := &fakeNativeModel{wrapper: wrapper}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := int(f.calls.Add(1)) - 1
		if i >= len(script) {
			i = len(script) - 1
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(f.wrapper(script[i])))
	}))
	t.Cleanup(f.server.Close)
	return f
}

// 协议假上游构造器：脚本元素分别是各协议的最小内容单元

// newFakeAnthropic 脚本元素为 content blocks 数组 JSON。
func newFakeAnthropic(t *testing.T, script ...string) *fakeNativeModel {
	return newFakeNativeModel(t, func(s string) string {
		return `{"content":[` + s + `],"stop_reason":"end_turn"}`
	}, script...)
}

// newFakeGemini 脚本元素为 parts 数组 JSON。
func newFakeGemini(t *testing.T, script ...string) *fakeNativeModel {
	return newFakeNativeModel(t, func(s string) string {
		return `{"candidates":[{"content":{"parts":[` + s + `]},"finishReason":"STOP"}]}`
	}, script...)
}

// newFakeResponses 脚本元素为 output 数组 JSON。
func newFakeResponses(t *testing.T, script ...string) *fakeNativeModel {
	return newFakeNativeModel(t, func(s string) string {
		return `{"output":[` + s + `]}`
	}, script...)
}

// assertTextReply 端到端：纯文本对话 → SSE text + done 事件且内容一致。
func assertTextReply(t *testing.T, ts *testEnv, token, ask, wantText string) {
	t.Helper()
	res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", token, map[string]string{"content": ask})
	body := sseBody(t, res)
	if !strings.Contains(body, "event: answer.delta") || !strings.Contains(body, wantText) {
		t.Fatalf("sse text event missing:\n%s", body)
	}
	if !strings.Contains(body, "event: turn.end") {
		t.Fatalf("sse done event missing:\n%s", body)
	}
	if strings.Contains(body, "event: proposal") {
		t.Fatalf("unexpected card event:\n%s", body)
	}
}

// assertCardReply 端到端：写工具调用 → SSE card 事件（含 tool 名与 action_id）。
func assertCardReply(t *testing.T, ts *testEnv, token, ask, tool string) int64 {
	t.Helper()
	res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", token, map[string]string{"content": ask})
	body := sseBody(t, res)
	if !strings.Contains(body, "event: proposal") || !strings.Contains(body, `"intent":"`+tool+`"`) {
		t.Fatalf("sse card event missing:\n%s", body)
	}
	return cardFromSSE(t, body)
}

const (
	toolCallAnthropic = `{"type":"tool_use","id":"toolu_1","name":"propose_post","input":{"content":"今天去看了电影","mood":"开心"}}`
	toolCallGemini    = `{"functionCall":{"name":"propose_post","args":{"content":"今天去看了电影","mood":"开心"}}}`
	toolCallResponses = `{"type":"function_call","call_id":"call_1","name":"propose_post","arguments":"{\"content\":\"今天去看了电影\",\"mood\":\"开心\"}"}`
)

func TestChatAnthropicEndToEnd(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	// 纯文本回复
	fake := newFakeAnthropic(t, `{"type":"text","text":"找到了：海边的日落真美"}`)
	seedFakeProviderType(t, ts, "anthropic", "claude-3", fake.server.URL)
	assertTextReply(t, ts, adminToken, "我们去过海边吗", "海边的日落真美")

	// 写工具 → 确认卡片（换新 server 避免脚本污染；新库换 testEnv）
	ts2, token2, _ := newPostTestEnv(t)
	fake2 := newFakeAnthropic(t, toolCallAnthropic)
	seedFakeProviderType(t, ts2, "anthropic", "claude-3", fake2.server.URL)
	assertCardReply(t, ts2, token2, "帮我记一条动态", "propose_post")
}

func TestChatGeminiEndToEnd(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	fake := newFakeGemini(t, `{"text":"找到了：海边的日落真美"}`)
	seedFakeProviderType(t, ts, "gemini", "gemini-1.5-pro", fake.server.URL)
	assertTextReply(t, ts, adminToken, "我们去过海边吗", "海边的日落真美")

	ts2, token2, _ := newPostTestEnv(t)
	fake2 := newFakeGemini(t, toolCallGemini)
	seedFakeProviderType(t, ts2, "gemini", "gemini-1.5-pro", fake2.server.URL)
	assertCardReply(t, ts2, token2, "帮我记一条动态", "propose_post")
}

func TestChatResponsesEndToEnd(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	fake := newFakeResponses(t, `{"type":"message","content":[{"type":"output_text","text":"找到了：海边的日落真美"}]}`)
	seedFakeProviderType(t, ts, "openai-responses", "gpt-5", fake.server.URL)
	assertTextReply(t, ts, adminToken, "我们去过海边吗", "海边的日落真美")

	ts2, token2, _ := newPostTestEnv(t)
	fake2 := newFakeResponses(t, toolCallResponses)
	seedFakeProviderType(t, ts2, "openai-responses", "gpt-5", fake2.server.URL)
	assertCardReply(t, ts2, token2, "帮我记一条动态", "propose_post")
}

// TestChatProviderChainOrder 端到端验证链序选择：链头无 Key 的供应商被跳过，
// 选中链上首个已配 Key 者（协议不限）。
func TestChatProviderChainOrder(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	// 链头（无 Key 的 openai-chat）不入 chat 链 → 跳过
	if _, err := ts.db.Exec(`INSERT INTO model_providers (api_type, name, base_url, api_key_enc) VALUES ('openai-chat', 'nokey', 'https://invalid', '')`); err != nil {
		t.Fatal(err)
	}
	// 另一位 gemini 有 Key → 被选中
	fake := newFakeGemini(t, `{"text":"链序生效"}`)
	seedFakeProviderType(t, ts, "gemini", "gemini-1.5-pro", fake.server.URL)

	assertTextReply(t, ts, adminToken, "在吗", "链序生效")
}
