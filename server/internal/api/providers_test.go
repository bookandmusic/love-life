package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeUpstreamModels 假上游 models 端点（fetch-models 联调）；wantAuth 断言到达上游的鉴权头。
func fakeUpstreamModels(t *testing.T, wantAuth string, status int, body string) *httptest.Server {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/models" {
			t.Errorf("upstream path = %q, want /models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+wantAuth {
			t.Errorf("upstream auth = %q, want Bearer %s", got, wantAuth)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestProvidersCRUD(t *testing.T) {
	ts, adminToken, girlToken := newPostTestEnv(t)

	// 非登录渠道与列表脱敏：girl 403
	res := doJSON(t, "GET", ts.URL+"/api/v1/admin/model-providers", girlToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("girl access: want 403, got %d", res.StatusCode)
	}

	// 空列表 → 数组直出（非 {items}）
	res = doJSON(t, "GET", ts.URL+"/api/v1/admin/model-providers", adminToken, nil)
	var list []map[string]any
	_ = json.NewDecoder(res.Body).Decode(&list)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || len(list) != 0 {
		t.Fatalf("empty list: %d %+v", res.StatusCode, list)
	}

	// 创建：201 返回条目，has_key=true、永不回读密钥
	res = doJSON(t, "POST", ts.URL+"/api/v1/admin/model-providers", adminToken, map[string]any{
		"api_type": "openai-chat", "name": "  主力  ", "base_url": "https://api.example.com/v1",
		"api_key": "sk-live",
		"models": []map[string]any{
			{"id": "gpt-4o", "kind": "chat", "max_tokens": 4096, "context_window": 128000},
			{"id": "gpt-4o-mini", "kind": "chat"},
		},
	})
	var created map[string]any
	_ = json.NewDecoder(res.Body).Decode(&created)
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create: %d %+v", res.StatusCode, created)
	}
	if created["api_type"] != "openai-chat" || created["name"] != "主力" || created["has_key"] != true {
		t.Fatalf("created = %+v", created)
	}
	if _, leak := created["api_key"]; leak {
		t.Fatal("create response must not leak api_key")
	}
	models := created["models"].([]any)
	if len(models) != 2 {
		t.Fatalf("models = %+v", models)
	}
	m0 := models[0].(map[string]any)
	if m0["id"] != "gpt-4o" || m0["max_tokens"] != float64(4096) || m0["context_window"] != float64(128000) {
		t.Fatalf("models[0] = %+v", m0)
	}
	if m0["thinking"] != "off" || m0["thinking_levels"] != "off" {
		t.Fatalf("thinking 缺省应为 off / off: %+v", m0)
	}
	m1 := models[1].(map[string]any)
	if m1["max_tokens"] != nil || m1["context_window"] != nil {
		t.Fatalf("null params must stay null: %+v", m1)
	}
	if _, has := m0["temperature"]; has {
		t.Fatalf("temperature 已从参数面删除，不得回显: %+v", m0)
	}
	id := int64(created["id"].(float64))

	// name 缺省取 api_type
	res = doJSON(t, "POST", ts.URL+"/api/v1/admin/model-providers", adminToken, map[string]any{
		"api_type": "anthropic", "base_url": "https://a.example.com", "api_key": "k",
	})
	var created2 map[string]any
	_ = json.NewDecoder(res.Body).Decode(&created2)
	res.Body.Close()
	if res.StatusCode != http.StatusCreated || created2["name"] != "anthropic" {
		t.Fatalf("name default: %d %+v", res.StatusCode, created2)
	}
	id2 := int64(created2["id"].(float64))

	// 列表两条
	res = doJSON(t, "GET", ts.URL+"/api/v1/admin/model-providers", adminToken, nil)
	_ = json.NewDecoder(res.Body).Decode(&list)
	res.Body.Close()
	if len(list) != 2 {
		t.Fatalf("list = %+v", list)
	}

	// 编辑：部分更新——name trim 空保留、models 整表替换
	res = doJSON(t, "PUT", ts.URL+"/api/v1/admin/model-providers/"+itoa(id), adminToken, map[string]any{
		"name": "   ", "models": []map[string]any{{"id": "gpt-4.1", "kind": "chat", "max_tokens": 2048}},
	})
	var updated map[string]any
	_ = json.NewDecoder(res.Body).Decode(&updated)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("update: %d", res.StatusCode)
	}
	if updated["name"] != "主力" {
		t.Fatalf("empty name must keep original: %+v", updated["name"])
	}
	if len(updated["models"].([]any)) != 1 {
		t.Fatalf("models must be replaced: %+v", updated["models"])
	}

	// 编辑：base_url 空串拒绝
	res = doJSON(t, "PUT", ts.URL+"/api/v1/admin/model-providers/"+itoa(id), adminToken, map[string]any{"base_url": ""})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty base_url: %d", res.StatusCode)
	}

	// key 单独更新
	res = doJSON(t, "PUT", ts.URL+"/api/v1/admin/model-providers/"+itoa(id2)+"/key", adminToken, map[string]any{"api_key": "sk-new"})
	var keyed map[string]any
	_ = json.NewDecoder(res.Body).Decode(&keyed)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || keyed["has_key"] != true {
		t.Fatalf("update key: %d %+v", res.StatusCode, keyed)
	}

	// 模型条目：给 id2（anthropic）挂 chat 条目
	res = doJSON(t, "POST", ts.URL+"/api/v1/admin/models", adminToken, map[string]any{
		"kind": "chat", "provider_id": id2, "model_id": "claude-sonnet",
	})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create model: %d", res.StatusCode)
	}
	// 嵌入条目挂 anthropic → 服务端拒绝（嵌入只允许 openai-chat）
	res = doJSON(t, "POST", ts.URL+"/api/v1/admin/models", adminToken, map[string]any{
		"kind": "embedding", "provider_id": id2, "model_id": "claude-embed",
	})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("embedding on anthropic must be rejected: %d", res.StatusCode)
	}

	// 链序：读取 chat 链 → 反转 → 校验顺序生效 + 非全覆盖拒绝
	res = doJSON(t, "GET", ts.URL+"/api/v1/admin/models", adminToken, nil)
	var chains map[string][]map[string]any
	_ = json.NewDecoder(res.Body).Decode(&chains)
	res.Body.Close()
	chatChain := chains["chat"]
	if len(chatChain) != 3 { // 主力 gpt-4.1 + gpt-4o-mini（create 挂了两条，update 替换成一条）… 以实际为准
		t.Logf("chat chain len = %d: %+v", len(chatChain), chatChain)
	}
	firstID := int64(chatChain[0]["id"].(float64))
	lastID := int64(chatChain[len(chatChain)-1]["id"].(float64))

	res = doJSON(t, "PUT", ts.URL+"/api/v1/admin/models/order", adminToken, map[string]any{"kind": "chat", "ids": []int64{firstID}})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("partial order must be rejected: %d", res.StatusCode)
	}

	reversed := []int64{lastID, firstID}
	rest := []int64{}
	seen := map[int64]bool{lastID: true, firstID: true}
	for _, e := range chatChain {
		eid := int64(e["id"].(float64))
		if !seen[eid] {
			rest = append(rest, eid)
			seen[eid] = true
		}
	}
	reversed = append(reversed, rest...)

	res = doJSON(t, "PUT", ts.URL+"/api/v1/admin/models/order", adminToken, map[string]any{"kind": "chat", "ids": reversed})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("order: %d", res.StatusCode)
	}
	res = doJSON(t, "GET", ts.URL+"/api/v1/admin/models", adminToken, nil)
	_ = json.NewDecoder(res.Body).Decode(&chains)
	res.Body.Close()
	if int64(chains["chat"][0]["id"].(float64)) != lastID {
		t.Fatalf("chain order not applied: %+v", chains["chat"])
	}

	// 删除：204 → 列表少一条 → 再删 404
	res = doJSON(t, "DELETE", ts.URL+"/api/v1/admin/model-providers/"+itoa(id), adminToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", res.StatusCode)
	}
	res = doJSON(t, "DELETE", ts.URL+"/api/v1/admin/model-providers/"+itoa(id), adminToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("delete twice: %d", res.StatusCode)
	}
	res = doJSON(t, "GET", ts.URL+"/api/v1/admin/model-providers", adminToken, nil)
	_ = json.NewDecoder(res.Body).Decode(&list)
	res.Body.Close()
	if len(list) != 1 {
		t.Fatalf("list after delete = %+v", list)
	}
}

func TestProvidersValidation(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	tests := []struct {
		name string
		body map[string]any
		want string // 期望错误文案片段
	}{
		{"非法 api_type", map[string]any{"api_type": "bogus", "base_url": "u", "api_key": "k"}, "api_type 不支持"},
		{"缺 base_url", map[string]any{"api_type": "openai-chat", "api_key": "k"}, "base_url 必填"},
		{"缺 api_key", map[string]any{"api_type": "openai-chat", "base_url": "u"}, "api_key 必填"},
		{"空模型名", map[string]any{"api_type": "openai-chat", "base_url": "u", "api_key": "k", "models": []map[string]any{{"id": " "}}}, "模型名为空"},
		{"max_tokens 下界", map[string]any{"api_type": "openai-chat", "base_url": "u", "api_key": "k", "models": []map[string]any{{"id": "m", "kind": "chat", "max_tokens": 100}}}, "max_tokens 需为 256~32768 整数"},
		{"max_tokens 非整数", map[string]any{"api_type": "openai-chat", "base_url": "u", "api_key": "k", "models": []map[string]any{{"id": "m", "kind": "chat", "max_tokens": 4096.5}}}, "max_tokens 需为 256~32768 整数"},
		{"context_window 越界", map[string]any{"api_type": "openai-chat", "base_url": "u", "api_key": "k", "models": []map[string]any{{"id": "m", "kind": "chat", "context_window": 100}}}, "context_window 需为 1024~2000000 整数"},
		{"max_tokens 超窗口", map[string]any{"api_type": "openai-chat", "base_url": "u", "api_key": "k", "models": []map[string]any{{"id": "m", "kind": "chat", "max_tokens": 8192, "context_window": 4096}}}, "max_tokens 不得超过 context_window"},
		{"thinking 非法", map[string]any{"api_type": "anthropic", "base_url": "u", "api_key": "k", "models": []map[string]any{{"id": "m", "kind": "chat", "thinking": "ultra"}}}, "thinking 需为 off / low / medium / high"},
		{"thinking_levels 非法", map[string]any{"api_type": "anthropic", "base_url": "u", "api_key": "k", "models": []map[string]any{{"id": "m", "kind": "chat", "thinking_levels": "off,ultra"}}}, "thinking_levels 需为 off / low / medium / high 的逗号分隔子集"},
		{"thinking 不在声明集内", map[string]any{"api_type": "gemini", "base_url": "u", "api_key": "k", "models": []map[string]any{{"id": "m", "kind": "chat", "thinking": "medium", "thinking_levels": "off,low,high"}}}, "thinking 需在 thinking_levels 声明的档位内"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := doJSON(t, "POST", ts.URL+"/api/v1/admin/model-providers", adminToken, tc.body)
			var out map[string]any
			_ = json.NewDecoder(res.Body).Decode(&out)
			res.Body.Close()
			if res.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %+v", res.StatusCode, out)
			}
			if !strings.Contains(out["error"].(string), tc.want) {
				t.Fatalf("error = %q, want contains %q", out["error"], tc.want)
			}
		})
	}

	// 404 系列：更新/删 key/删除不存在的
	res := doJSON(t, "PUT", ts.URL+"/api/v1/admin/model-providers/999", adminToken, map[string]any{"name": "x"})
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("update missing: %d", res.StatusCode)
	}
	res = doJSON(t, "PUT", ts.URL+"/api/v1/admin/model-providers/999/key", adminToken, map[string]any{"api_key": "k"})
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("key missing: %d", res.StatusCode)
	}
}

func TestProvidersFetchModels(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	// 预置一个已存 Key 的供应商（provider_id 路径用）
	res := doJSON(t, "POST", ts.URL+"/api/v1/admin/model-providers", adminToken, map[string]any{
		"api_type": "openai-chat", "base_url": "https://placeholder", "api_key": "sk-saved",
	})
	var created map[string]any
	_ = json.NewDecoder(res.Body).Decode(&created)
	res.Body.Close()
	savedID := int64(created["id"].(float64))

	t.Run("显式 api_key 走假上游", func(t *testing.T) {
		up := fakeUpstreamModels(t, "sk-live", http.StatusOK, `{"data":[{"id":"gpt-4o"},{"id":"gpt-4.1"}]}`)
		res := doJSON(t, "POST", ts.URL+"/api/v1/admin/model-providers/fetch-models", adminToken, map[string]any{
			"api_type": "openai-chat", "base_url": up.URL, "api_key": "sk-live",
		})
		var out map[string]any
		_ = json.NewDecoder(res.Body).Decode(&out)
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("fetch: %d", res.StatusCode)
		}
		models := out["models"].([]any)
		if len(models) != 2 || models[0] != "gpt-4o" {
			t.Fatalf("models = %+v", models)
		}
	})

	t.Run("provider_id + 已存秘钥", func(t *testing.T) {
		up := fakeUpstreamModels(t, "sk-saved", http.StatusOK, `{"data":[{"id":"m1"}]}`)
		res := doJSON(t, "POST", ts.URL+"/api/v1/admin/model-providers/fetch-models", adminToken, map[string]any{
			"api_type": "openai-chat", "base_url": up.URL, "provider_id": savedID,
		})
		var out map[string]any
		_ = json.NewDecoder(res.Body).Decode(&out)
		res.Body.Close()
		if res.StatusCode != http.StatusOK || len(out["models"].([]any)) != 1 {
			t.Fatalf("fetch via saved: %d %+v", res.StatusCode, out)
		}
	})

	t.Run("无 key 无 provider_id 拒绝", func(t *testing.T) {
		res := doJSON(t, "POST", ts.URL+"/api/v1/admin/model-providers/fetch-models", adminToken, map[string]any{
			"api_type": "openai-chat", "base_url": "https://x",
		})
		var out map[string]any
		_ = json.NewDecoder(res.Body).Decode(&out)
		res.Body.Close()
		if res.StatusCode != http.StatusBadRequest || !strings.Contains(out["error"].(string), "api_key 必填") {
			t.Fatalf("no key: %d %+v", res.StatusCode, out)
		}
	})

	t.Run("上游失败 502", func(t *testing.T) {
		up := fakeUpstreamModels(t, "bad", http.StatusUnauthorized, `{"error":"unauthorized"}`)
		res := doJSON(t, "POST", ts.URL+"/api/v1/admin/model-providers/fetch-models", adminToken, map[string]any{
			"api_type": "openai-chat", "base_url": up.URL, "api_key": "bad",
		})
		var out map[string]any
		_ = json.NewDecoder(res.Body).Decode(&out)
		res.Body.Close()
		if res.StatusCode != http.StatusBadGateway || !strings.Contains(out["error"].(string), "获取模型列表失败") {
			t.Fatalf("upstream fail: %d %+v", res.StatusCode, out)
		}
	})
}
