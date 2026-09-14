package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// embeddingConfigWire 嵌入配置的线上契约形状（测试侧自带，字段名与 tag 即契约）。
type embeddingConfigWire struct {
	ProviderID int64  `json:"provider_id"`
	Model      string `json:"model"`
}

// 嵌入模型配置端点：复用既有供应商做语义检索，未配置 = 关闭。
func TestEmbeddingConfigAPI(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	pid := seedNamedProvider(t, ts, "openai-chat", "嵌入供应商", "deepseek-chat", "https://api.example.com/v1", 0)
	anthropicID := seedNamedProvider(t, ts, "anthropic", "Anthropic", "claude-x", "https://api.anthropic.com", 1)

	get := func() embeddingConfigWire {
		t.Helper()
		res := doJSON(t, "GET", ts.URL+"/api/v1/admin/embedding-config", adminToken, nil)
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("get status = %d", res.StatusCode)
		}
		var got embeddingConfigWire
		if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
			t.Fatalf("decode get: %v", err)
		}
		return got
	}
	put := func(body map[string]any) (int, embeddingConfigWire) {
		t.Helper()
		res := doJSON(t, "PUT", ts.URL+"/api/v1/admin/embedding-config", adminToken, body)
		defer res.Body.Close()
		var got embeddingConfigWire
		if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
			t.Fatalf("decode put: %v", err)
		}
		return res.StatusCode, got
	}
	putErr := func(body map[string]any) (int, string) {
		t.Helper()
		res := doJSON(t, "PUT", ts.URL+"/api/v1/admin/embedding-config", adminToken, body)
		defer res.Body.Close()
		var out map[string]any
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			t.Fatalf("decode put error: %v", err)
		}
		msg, _ := out["error"].(string)
		return res.StatusCode, msg
	}

	// 初始未配置
	if got := get(); got.ProviderID != 0 || got.Model != "" {
		t.Fatalf("get default = %+v", got)
	}

	// 保存 → 读回
	code, saved := put(map[string]any{"provider_id": pid, "model": "text-embedding-3-small"})
	if code != http.StatusOK || saved.ProviderID != pid || saved.Model != "text-embedding-3-small" {
		t.Fatalf("put = %d %+v", code, saved)
	}
	if got := get(); got.ProviderID != pid || got.Model != "text-embedding-3-small" {
		t.Fatalf("get after put = %+v", got)
	}

	// provider_id 非 0 但 model 为空 → 400（显式要求模型名，不复用供应商默认聊天模型）
	if code, msg := putErr(map[string]any{"provider_id": pid, "model": "   "}); code != http.StatusBadRequest || msg != "model 必填" {
		t.Fatalf("put empty model = %d %q", code, msg)
	}

	// 非 openai-chat 协议 → 400（其端点不提供 OpenAI 兼容 embeddings）
	if code, msg := putErr(map[string]any{"provider_id": anthropicID, "model": "voyage-3"}); code != http.StatusBadRequest || msg != "嵌入模型仅支持 OpenAI Chat 协议的供应商" {
		t.Fatalf("put anthropic = %d %q", code, msg)
	}

	// 未知供应商 → 400，且不改动既有配置
	if code, msg := putErr(map[string]any{"provider_id": 99999, "model": "x"}); code != http.StatusBadRequest || msg != "供应商不存在" {
		t.Fatalf("put unknown provider = %d %q", code, msg)
	}
	if got := get(); got.ProviderID != pid {
		t.Fatalf("失败请求不应改动配置: %+v", got)
	}

	// provider_id=0 → 清空（关闭语义检索）
	if code, cleared := put(map[string]any{"provider_id": 0}); code != http.StatusOK || cleared.ProviderID != 0 || cleared.Model != "" {
		t.Fatalf("clear = %d %+v", code, cleared)
	}
	if got := get(); got.ProviderID != 0 || got.Model != "" {
		t.Fatalf("get after clear = %+v", got)
	}
}
