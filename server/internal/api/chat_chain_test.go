package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/crypto"
)

// ---- 故障切换链（DESIGN §4.1 有序链）与模型级参数端到端 ----
// 基建沿用 chat_protocols_test.go 的假上游与 seedFakeProviderType。

// newFailingUpstream 每次请求都返回 500 + 非 JSON 体（adapter 解码必失败 → chat 视为该供应商失败）。
func newFailingUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream down", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newCapturingChatUpstream 记录收到的请求体并回放固定 openai-chat 文本响应。
func newCapturingChatUpstream(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &bodies
}

// seedNamedProvider 直插指定链序的供应商（区别于 seedFakeProviderType 固定链头）。
func seedNamedProvider(t *testing.T, env *testEnv, apiType, name, model, baseURL string, sortOrder int) int64 {
	t.Helper()
	enc, err := crypto.Encrypt(crypto.KeyFrom(env.tm.Secret()), "sk-"+name)
	if err != nil {
		t.Fatalf("encrypt key: %v", err)
	}
	res, err := env.db.Exec(`INSERT INTO model_providers (api_type, name, base_url, api_key_enc, default_model, sort_order) VALUES (?, ?, ?, ?, ?, ?)`,
		apiType, name, baseURL, enc, model, sortOrder)
	if err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	return id
}

func usageRows(t *testing.T, env *testEnv) []struct {
	ProviderID int64
	Status     string
} {
	t.Helper()
	rows, err := env.db.Query(`SELECT provider_id, status FROM ai_usage_logs ORDER BY id`)
	if err != nil {
		t.Fatalf("query ai_usage_logs: %v", err)
	}
	defer rows.Close()
	var out []struct {
		ProviderID int64
		Status     string
	}
	for rows.Next() {
		var r struct {
			ProviderID int64
			Status     string
		}
		if err := rows.Scan(&r.ProviderID, &r.Status); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// TestChatFailoverToNextProvider 链头调用失败 → 自动切到链尾并正常回复；
// 每次尝试各落一条统计（failed 归链头、success 归链尾）。
func TestChatFailoverToNextProvider(t *testing.T) {
	ts, token, _ := newPostTestEnv(t)

	headID := seedNamedProvider(t, ts, "openai-chat", "head", "m1", newFailingUpstream(t).URL, 0)
	fake := newFakeGemini(t, `{"text":"链尾顶上"}`)
	tailID := seedNamedProvider(t, ts, "gemini", "tail", "gemini-1.5-pro", fake.server.URL, 1)

	assertTextReply(t, ts, token, "在吗", "链尾顶上")

	rows := usageRows(t, ts)
	if len(rows) != 2 {
		t.Fatalf("ai_usage_logs rows = %d, want 2（failed + success）", len(rows))
	}
	if rows[0].ProviderID != headID || rows[0].Status != "failed" {
		t.Errorf("rows[0] = (%d,%s), want (%d,failed)", rows[0].ProviderID, rows[0].Status, headID)
	}
	if rows[1].ProviderID != tailID || rows[1].Status != "success" {
		t.Errorf("rows[1] = (%d,%s), want (%d,success)", rows[1].ProviderID, rows[1].Status, tailID)
	}
}

// TestChatChainExhausted 全链失败 → SSE error + done，无 text/card。
func TestChatChainExhausted(t *testing.T) {
	ts, token, _ := newPostTestEnv(t)

	failURL := newFailingUpstream(t).URL
	seedNamedProvider(t, ts, "openai-chat", "head", "m1", failURL, 0)
	seedNamedProvider(t, ts, "gemini", "tail", "gemini-1.5-pro", failURL, 1)

	res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", token, map[string]string{"content": "在吗"})
	body := sseBody(t, res)
	if !strings.Contains(body, "event: error") || !strings.Contains(body, "event: done") {
		t.Fatalf("want error + done events:\n%s", body)
	}
	if strings.Contains(body, "event: text") || strings.Contains(body, "event: card") {
		t.Fatalf("unexpected text/card events:\n%s", body)
	}
	for i, r := range usageRows(t, ts) {
		if r.Status != "failed" {
			t.Errorf("usage[%d].status = %s, want failed", i, r.Status)
		}
	}
}

// TestChatSkipsProviderWithoutModels 链头有 Key 但无模型条目 → 构链时跳过，链尾服务。
// （旧行为：选中链头后因无模型直接 423。）
func TestChatSkipsProviderWithoutModels(t *testing.T) {
	ts, token, _ := newPostTestEnv(t)

	seedNamedProvider(t, ts, "openai-chat", "nomodel", "", "https://invalid", 0)
	fake := newFakeGemini(t, `{"text":"无模型跳过"}`)
	seedNamedProvider(t, ts, "gemini", "tail", "gemini-1.5-pro", fake.server.URL, 1)

	assertTextReply(t, ts, token, "在吗", "无模型跳过")
}

// TestChatModelLevelParamsEndToEnd provider_models 配置的 temperature/max_tokens
// 进入发往上游的请求体（openai-chat 形状）。
func TestChatModelLevelParamsEndToEnd(t *testing.T) {
	ts, token, _ := newPostTestEnv(t)

	srv, bodies := newCapturingChatUpstream(t)
	id := seedNamedProvider(t, ts, "openai-chat", "params", "gpt-x", srv.URL, 0)
	if _, err := ts.db.Exec(`INSERT INTO provider_models (provider_id, model_id, temperature, max_tokens) VALUES (?, 'gpt-x', 0.7, 2048)`, id); err != nil {
		t.Fatalf("seed provider_models: %v", err)
	}

	assertTextReply(t, ts, token, "在吗", "ok")

	if len(*bodies) != 1 {
		t.Fatalf("upstream calls = %d, want 1", len(*bodies))
	}
	if !strings.Contains((*bodies)[0], `"temperature":0.7`) || !strings.Contains((*bodies)[0], `"max_tokens":2048`) {
		t.Fatalf("model params missing in request body:\n%s", (*bodies)[0])
	}
}

// TestChatDecryptFailureChain Key 解密失败的供应商构链时跳过；全部因此不可用 → 500。
func TestChatDecryptFailureChain(t *testing.T) {
	t.Run("坏 Key 链头被跳过", func(t *testing.T) {
		ts, token, _ := newPostTestEnv(t)
		if _, err := ts.db.Exec(`INSERT INTO model_providers (api_type, name, base_url, api_key_enc, default_model, sort_order) VALUES ('openai-chat', 'badkey', 'https://invalid', 'not-a-valid-ciphertext', 'm1', 0)`); err != nil {
			t.Fatal(err)
		}
		fake := newFakeGemini(t, `{"text":"坏 Key 不挡道"}`)
		seedNamedProvider(t, ts, "gemini", "tail", "gemini-1.5-pro", fake.server.URL, 1)
		assertTextReply(t, ts, token, "在吗", "坏 Key 不挡道")
	})

	t.Run("仅坏 Key 供应商返回 500", func(t *testing.T) {
		ts, token, _ := newPostTestEnv(t)
		if _, err := ts.db.Exec(`INSERT INTO model_providers (api_type, name, base_url, api_key_enc, default_model, sort_order) VALUES ('openai-chat', 'badkey', 'https://invalid', 'not-a-valid-ciphertext', 'm1', 0)`); err != nil {
			t.Fatal(err)
		}
		res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", token, map[string]string{"content": "在吗"})
		body := sseBody(t, res)
		if res.StatusCode != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", res.StatusCode)
		}
		if !strings.Contains(body, "模型 Key 解密失败") {
			t.Fatalf("body = %s, want 解密失败文案", body)
		}
	})

	// 混合跳过不触发 500：只有「全部因解 Key 失败」才算解密问题，
	// 其余供应商因无模型跳过时仍按链空 → 423。
	t.Run("坏 Key 与无模型混合跳过返回 423", func(t *testing.T) {
		ts, token, _ := newPostTestEnv(t)
		if _, err := ts.db.Exec(`INSERT INTO model_providers (api_type, name, base_url, api_key_enc, default_model, sort_order) VALUES ('openai-chat', 'badkey', 'https://invalid', 'not-a-valid-ciphertext', 'm1', 0)`); err != nil {
			t.Fatal(err)
		}
		seedNamedProvider(t, ts, "openai-chat", "nomodel", "", "https://invalid", 1)
		res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", token, map[string]string{"content": "在吗"})
		body := sseBody(t, res)
		if res.StatusCode != http.StatusLocked {
			t.Fatalf("status = %d, want 423", res.StatusCode)
		}
		if !strings.Contains(body, "未配置模型") {
			t.Fatalf("body = %s, want 未配置模型文案", body)
		}
	})
}

// TestChatModelParamsFallbackToUnset default_model 手填了无参数条目的模型名时，
// 参数按未设置降级（不传），不报错。
func TestChatModelParamsFallbackToUnset(t *testing.T) {
	ts, token, _ := newPostTestEnv(t)

	srv, bodies := newCapturingChatUpstream(t)
	id := seedNamedProvider(t, ts, "openai-chat", "manual", "gpt-x", srv.URL, 0)
	// 参数只挂在另一个模型条目 gpt-y 上，default_model 的 gpt-x 无条目
	if _, err := ts.db.Exec(`INSERT INTO provider_models (provider_id, model_id, temperature, max_tokens) VALUES (?, 'gpt-y', 0.7, 2048)`, id); err != nil {
		t.Fatalf("seed provider_models: %v", err)
	}

	assertTextReply(t, ts, token, "在吗", "ok")

	if len(*bodies) != 1 {
		t.Fatalf("upstream calls = %d, want 1", len(*bodies))
	}
	sent := (*bodies)[0]
	if strings.Contains(sent, `"temperature"`) || strings.Contains(sent, `"max_tokens"`) {
		t.Fatalf("params must be absent when no matching model entry:\n%s", sent)
	}
}
