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
		"api_key": "sk-live", "default_model": "gpt-4o",
		"models": []map[string]any{
			{"id": "gpt-4o", "temperature": 0.7, "max_tokens": 4096},
			{"id": "gpt-4o-mini"},
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
	if created["default_model"] != "gpt-4o" {
		t.Fatalf("default_model = %+v", created["default_model"])
	}
	models := created["models"].([]any)
	if len(models) != 2 {
		t.Fatalf("models = %+v", models)
	}
	m0 := models[0].(map[string]any)
	if m0["id"] != "gpt-4o" || m0["temperature"] != 0.7 || m0["max_tokens"] != float64(4096) {
		t.Fatalf("models[0] = %+v", m0)
	}
	m1 := models[1].(map[string]any)
	if m1["temperature"] != nil || m1["max_tokens"] != nil {
		t.Fatalf("null params must stay null: %+v", m1)
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

	// 编辑：部分更新——name trim 空保留、models 整表替换、default_model 原样存
	res = doJSON(t, "PUT", ts.URL+"/api/v1/admin/model-providers/"+itoa(id), adminToken, map[string]any{
		"name": "   ", "models": []map[string]any{{"id": "gpt-4.1", "temperature": 0.2}},
		"default_model": "gpt-4.1",
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
	if updated["default_model"] != "gpt-4.1" {
		t.Fatalf("default_model = %+v", updated["default_model"])
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

	// 排序：部分 ids / 未知 id 拒绝
	res = doJSON(t, "PUT", ts.URL+"/api/v1/admin/model-providers/order", adminToken, map[string]any{"ids": []int64{id}})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("partial order: %d", res.StatusCode)
	}
	res = doJSON(t, "PUT", ts.URL+"/api/v1/admin/model-providers/order", adminToken, map[string]any{"ids": []int64{id, 999}})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown id order: %d", res.StatusCode)
	}
	// 正确排序：anthropic（id2）在前 → 列表顺序反转
	res = doJSON(t, "PUT", ts.URL+"/api/v1/admin/model-providers/order", adminToken, map[string]any{"ids": []int64{id2, id}})
	var ordered []map[string]any
	_ = json.NewDecoder(res.Body).Decode(&ordered)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || ordered[0]["id"] != float64(id2) {
		t.Fatalf("order: %d %+v", res.StatusCode, ordered)
	}
	// GET 列表顺序与 order 一致
	res = doJSON(t, "GET", ts.URL+"/api/v1/admin/model-providers", adminToken, nil)
	_ = json.NewDecoder(res.Body).Decode(&list)
	res.Body.Close()
	if list[0]["id"] != float64(id2) {
		t.Fatalf("list after order = %+v", list)
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
		{"temperature 越界", map[string]any{"api_type": "openai-chat", "base_url": "u", "api_key": "k", "models": []map[string]any{{"id": "m", "temperature": 2.5}}}, "temperature 需在 0~2 之间"},
		{"max_tokens 下界", map[string]any{"api_type": "openai-chat", "base_url": "u", "api_key": "k", "models": []map[string]any{{"id": "m", "max_tokens": 100}}}, "max_tokens 需为 256~32768 整数"},
		{"max_tokens 非整数", map[string]any{"api_type": "openai-chat", "base_url": "u", "api_key": "k", "models": []map[string]any{{"id": "m", "max_tokens": 4096.5}}}, "max_tokens 需为 256~32768 整数"},
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
