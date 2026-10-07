package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// 模型级「思考档位」的往返守卫：缺省 off；显式设置能落库并回显；非法取值被拒。
//
// 档位是三协议「是否请求思考」的唯一开关（off = 服务端不注入任何思考参数），
// 而开启会产生思考 token 计费，故取值面必须收紧、缺省必须是不花钱的 off。
func TestModelThinkingRoundTrip(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	// 建一个 anthropic 供应商（非 openai-chat，思考档位对它有意义）
	res := doJSON(t, "POST", ts.URL+"/api/v1/admin/model-providers", adminToken, map[string]any{
		"api_type": "anthropic", "name": "claude", "base_url": "http://127.0.0.1:1", "api_key": "sk-x",
	})
	var created struct {
		ID int64 `json:"id"`
	}
	_ = json.NewDecoder(res.Body).Decode(&created)
	res.Body.Close()
	if created.ID == 0 {
		t.Fatal("前置：供应商创建失败")
	}

	// ① 不传 thinking → 缺省 off（不花钱的默认）
	res = doJSON(t, "POST", ts.URL+"/api/v1/admin/models", adminToken, map[string]any{
		"kind": "chat", "provider_id": created.ID, "model_id": "claude-sonnet",
	})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create model: %d", res.StatusCode)
	}
	if got := thinkingOf(t, ts, adminToken, "claude-sonnet"); got != "off" {
		t.Fatalf("未传 thinking 时缺省应为 off，实得 %q", got)
	}

	// ② 显式设 high → 落库并回显
	res = doJSON(t, "POST", ts.URL+"/api/v1/admin/models", adminToken, map[string]any{
		"kind": "chat", "provider_id": created.ID, "model_id": "claude-thinking", "thinking": "high",
	})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create model with thinking: %d", res.StatusCode)
	}
	if got := thinkingOf(t, ts, adminToken, "claude-thinking"); got != "high" {
		t.Fatalf("thinking 应为 high，实得 %q", got)
	}

	// ③ 非法取值拒绝（不静默归 off——配置写错时应当立刻可见）
	res = doJSON(t, "POST", ts.URL+"/api/v1/admin/models", adminToken, map[string]any{
		"kind": "chat", "provider_id": created.ID, "model_id": "claude-bad", "thinking": "ultra",
	})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("非法 thinking 应 400，实得 %d", res.StatusCode)
	}

	// ④ 更新：显式置 off、且未传时保留原值
	id := modelIDOf(t, ts, adminToken, "claude-thinking")
	res = doJSON(t, "PUT", ts.URL+"/api/v1/admin/models/"+itoa(id), adminToken, map[string]any{
		"model_id": "claude-thinking", "thinking": "low",
	})
	res.Body.Close()
	if got := thinkingOf(t, ts, adminToken, "claude-thinking"); got != "low" {
		t.Fatalf("更新后应为 low，实得 %q", got)
	}
	// 只改模型名、不传 thinking → 保留 low
	res = doJSON(t, "PUT", ts.URL+"/api/v1/admin/models/"+itoa(id), adminToken, map[string]any{
		"model_id": "claude-thinking",
	})
	res.Body.Close()
	if got := thinkingOf(t, ts, adminToken, "claude-thinking"); got != "low" {
		t.Fatalf("未传 thinking 时应保留原值 low，实得 %q", got)
	}
}

// modelRowOf 从 admin 两条链里按模型名取一行（原样返回，调用方自取字段）。
func modelRowOf(t *testing.T, ts *testEnv, token, modelID string) map[string]any {
	t.Helper()
	for _, r := range chainRows(t, ts, token) {
		if r["model_id"] == modelID {
			return r
		}
	}
	t.Fatalf("模型链里找不到 %s", modelID)
	return nil
}

// thinkingOf 某模型当前的思考档位。
func thinkingOf(t *testing.T, ts *testEnv, token, modelID string) string {
	t.Helper()
	v, _ := modelRowOf(t, ts, token, modelID)["thinking"].(string)
	return v
}

// modelIDOf 某模型的条目 id（更新接口按它定位）。
func modelIDOf(t *testing.T, ts *testEnv, token, modelID string) int64 {
	t.Helper()
	id, _ := modelRowOf(t, ts, token, modelID)["id"].(float64)
	return int64(id)
}

func chainRows(t *testing.T, ts *testEnv, token string) []map[string]any {
	t.Helper()
	res := doJSON(t, "GET", ts.URL+"/api/v1/admin/models", token, nil)
	defer res.Body.Close()
	var chains map[string][]map[string]any
	_ = json.NewDecoder(res.Body).Decode(&chains)
	return append(append([]map[string]any{}, chains["chat"]...), chains["embedding"]...)
}

// OpenAI Chat 协议没有统一的思考参数（是否回思考由供应商自定），故不允许配置档位——
// 该规则必须由**服务端**执行：只做在 admin 下拉上时，直接调接口仍能写入，
// 值又会被 adapter 忽略，形成「配了不生效」的静默错觉。
func TestThinkingRejectedForOpenAIChat(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	// 建一个 openai-chat 供应商（不带模型）
	res := doJSON(t, "POST", ts.URL+"/api/v1/admin/model-providers", adminToken, map[string]any{
		"api_type": "openai-chat", "name": "compat", "base_url": "http://127.0.0.1:1", "api_key": "sk-x",
	})
	var created struct {
		ID int64 `json:"id"`
	}
	_ = json.NewDecoder(res.Body).Decode(&created)
	res.Body.Close()
	if created.ID == 0 {
		t.Fatal("前置：供应商创建失败")
	}

	// ① 单条新增带档位 → 拒绝
	res = doJSON(t, "POST", ts.URL+"/api/v1/admin/models", adminToken, map[string]any{
		"kind": "chat", "provider_id": created.ID, "model_id": "gpt-x", "thinking": "high",
	})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("openai-chat 配思考档位应 400，实得 %d", res.StatusCode)
	}

	// ② 关（off / 不传）放行
	res = doJSON(t, "POST", ts.URL+"/api/v1/admin/models", adminToken, map[string]any{
		"kind": "chat", "provider_id": created.ID, "model_id": "gpt-x",
	})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("缺省 off 应放行，实得 %d", res.StatusCode)
	}

	// ③ 单条更新改档位 → 拒绝
	id := modelIDOf(t, ts, adminToken, "gpt-x")
	res = doJSON(t, "PUT", ts.URL+"/api/v1/admin/models/"+itoa(id), adminToken, map[string]any{
		"model_id": "gpt-x", "thinking": "low",
	})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("openai-chat 更新档位应 400，实得 %d", res.StatusCode)
	}

	// ④ 供应商创建时一并带模型档位 → 拒绝
	res = doJSON(t, "POST", ts.URL+"/api/v1/admin/model-providers", adminToken, map[string]any{
		"api_type": "openai-chat", "name": "compat2", "base_url": "http://127.0.0.1:1", "api_key": "sk-x",
		"models": []map[string]any{{"id": "gpt-y", "kind": "chat", "thinking": "medium"}},
	})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("创建供应商时带档位应 400，实得 %d", res.StatusCode)
	}
}

// 模型列表（admin 两条链）必须回显 thinking —— 客户端靠它显示当前档位。
func TestModelThinkingAppearsInProviderPayload(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	res := doJSON(t, "POST", ts.URL+"/api/v1/admin/model-providers", adminToken, map[string]any{
		"api_type": "anthropic", "name": "claude", "base_url": "http://127.0.0.1:1", "api_key": "sk-x",
		"models": []map[string]any{{"id": "claude-think", "kind": "chat", "thinking": "medium"}},
	})
	var created struct {
		ID     int64 `json:"id"`
		Models []struct {
			Thinking string `json:"thinking"`
		} `json:"models"`
	}
	_ = json.NewDecoder(res.Body).Decode(&created)
	res.Body.Close()
	if len(created.Models) != 1 {
		t.Fatalf("供应商应回带 1 条模型，实得 %d", len(created.Models))
	}
	if created.Models[0].Thinking != "medium" {
		t.Fatalf("供应商载荷里的模型 thinking 应为 medium，实得 %q", created.Models[0].Thinking)
	}
}
