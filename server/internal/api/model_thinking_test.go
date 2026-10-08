package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// newThinkingProvider 建一个供应商并返回其 id。openai-chat 无思考档位，
// 故思考相关用例统一用 anthropic；name 用于同一用例里建多个供应商时区分。
func newThinkingProvider(t *testing.T, ts *testEnv, adminToken, apiType, name string, models []map[string]any) int64 {
	t.Helper()
	payload := map[string]any{
		"api_type": apiType, "name": name, "base_url": "http://127.0.0.1:1", "api_key": "sk-x",
	}
	if models != nil {
		payload["models"] = models
	}
	res := doJSON(t, "POST", ts.URL+"/api/v1/admin/model-providers", adminToken, payload)
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("前置：建 %s 供应商应 201，实得 %d", apiType, res.StatusCode)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	_ = json.NewDecoder(res.Body).Decode(&created)
	if created.ID == 0 {
		t.Fatal("前置：供应商创建失败")
	}
	return created.ID
}

// 模型级「思考档位」的往返守卫：缺省 off；显式设置能落库并回显；非法取值被拒。
//
// 档位是三协议「请求多少思考」的唯一开关（off = 该协议的最低档，现役模型关不掉思考），
// 而开启会产生思考 token 计费，故取值面必须收紧、缺省必须是不花钱的 off。
func TestModelThinkingRoundTrip(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	// 建一个 anthropic 供应商（非 openai-chat，思考档位对它有意义）
	pid := newThinkingProvider(t, ts, adminToken, "anthropic", "claude", nil)

	// ① 不传 thinking / thinking_levels → 缺省 off + 只有关（不花钱的默认）
	res := doJSON(t, "POST", ts.URL+"/api/v1/admin/models", adminToken, map[string]any{
		"kind": "chat", "provider_id": pid, "model_id": "claude-sonnet",
	})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create model: %d", res.StatusCode)
	}
	if got := thinkingOf(t, ts, adminToken, "claude-sonnet"); got != "off" {
		t.Fatalf("未传 thinking 时缺省应为 off，实得 %q", got)
	}
	if got := levelsOf(t, ts, adminToken, "claude-sonnet"); got != "off" {
		t.Fatalf("未传 thinking_levels 时缺省应为 off，实得 %q", got)
	}

	// ② 显式声明支持档位并选 high → 落库并回显
	res = doJSON(t, "POST", ts.URL+"/api/v1/admin/models", adminToken, map[string]any{
		"kind": "chat", "provider_id": pid, "model_id": "claude-thinking",
		"thinking": "high", "thinking_levels": "off,low,medium,high",
	})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create model with thinking: %d", res.StatusCode)
	}
	if got := thinkingOf(t, ts, adminToken, "claude-thinking"); got != "high" {
		t.Fatalf("thinking 应为 high，实得 %q", got)
	}
	if got := levelsOf(t, ts, adminToken, "claude-thinking"); got != "off,low,medium,high" {
		t.Fatalf("thinking_levels 应为 off,low,medium,high，实得 %q", got)
	}

	// ③ 非法取值拒绝（不静默归 off——配置写错时应当立刻可见）
	for name, payload := range map[string]map[string]any{
		"档位非法":     {"thinking": "ultra", "thinking_levels": "off,low"},
		"集合非法":     {"thinking": "low", "thinking_levels": "off,ultra"},
		"档位不在声明集内": {"thinking": "medium", "thinking_levels": "off,low,high"},
	} {
		body := map[string]any{"kind": "chat", "provider_id": pid, "model_id": "claude-bad"}
		for k, v := range payload {
			body[k] = v
		}
		res = doJSON(t, "POST", ts.URL+"/api/v1/admin/models", adminToken, body)
		res.Body.Close()
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s：应 400，实得 %d", name, res.StatusCode)
		}
	}

	// ④ 更新：显式改档位、且未传时保留原值
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

// 档位集合是「该模型支持哪些档位」的声明：乱序输入要归一成固定顺序，且恒含 off。
func TestThinkingLevelsNormalized(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	pid := newThinkingProvider(t, ts, adminToken, "gemini", "gemini", nil)

	// Gemini 3 Pro 只接受低/高：声明集如实写，选「中」必须被拒
	res := doJSON(t, "POST", ts.URL+"/api/v1/admin/models", adminToken, map[string]any{
		"kind": "chat", "provider_id": pid, "model_id": "gemini-3.1-pro",
		"thinking": "high", "thinking_levels": "high,low",
	})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create model: %d", res.StatusCode)
	}
	if got := levelsOf(t, ts, adminToken, "gemini-3.1-pro"); got != "off,low,high" {
		t.Fatalf("乱序输入应归一为 off,low,high（并自动补 off），实得 %q", got)
	}

	id := modelIDOf(t, ts, adminToken, "gemini-3.1-pro")
	res = doJSON(t, "PUT", ts.URL+"/api/v1/admin/models/"+itoa(id), adminToken, map[string]any{
		"model_id": "gemini-3.1-pro", "thinking": "medium",
	})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("档位不在声明集内应 400，实得 %d", res.StatusCode)
	}
}

// 上下文窗口是裁剪历史预算的依据；最大输出不得超过它，否则「窗口 − 输出预留」为负。
func TestContextWindowValidation(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	pid := newThinkingProvider(t, ts, adminToken, "anthropic", "claude", nil)

	// 合法：窗口 > 输出
	res := doJSON(t, "POST", ts.URL+"/api/v1/admin/models", adminToken, map[string]any{
		"kind": "chat", "provider_id": pid, "model_id": "claude-ok",
		"max_tokens": 8192, "context_window": 200000,
	})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("合法窗口应放行，实得 %d", res.StatusCode)
	}
	if got := contextWindowOf(t, ts, adminToken, "claude-ok"); got != 200000 {
		t.Fatalf("context_window 应为 200000，实得 %v", got)
	}

	for name, payload := range map[string]map[string]any{
		"窗口越界":  {"context_window": 100},
		"输出超窗口": {"max_tokens": 8192, "context_window": 4096},
	} {
		body := map[string]any{"kind": "chat", "provider_id": pid, "model_id": "claude-bad-" + name}
		for k, v := range payload {
			body[k] = v
		}
		res = doJSON(t, "POST", ts.URL+"/api/v1/admin/models", adminToken, body)
		res.Body.Close()
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s：应 400，实得 %d", name, res.StatusCode)
		}
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

// levelsOf 某模型声明的思考档位集合。
func levelsOf(t *testing.T, ts *testEnv, token, modelID string) string {
	t.Helper()
	v, _ := modelRowOf(t, ts, token, modelID)["thinking_levels"].(string)
	return v
}

// contextWindowOf 某模型声明的上下文窗口（未声明时为 0）。
func contextWindowOf(t *testing.T, ts *testEnv, token, modelID string) int64 {
	t.Helper()
	v, _ := modelRowOf(t, ts, token, modelID)["context_window"].(float64)
	return int64(v)
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
	pid := newThinkingProvider(t, ts, adminToken, "openai-chat", "compat", nil)

	// ① 单条新增带档位 → 拒绝
	res := doJSON(t, "POST", ts.URL+"/api/v1/admin/models", adminToken, map[string]any{
		"kind": "chat", "provider_id": pid, "model_id": "gpt-x", "thinking": "high",
	})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("openai-chat 配思考档位应 400，实得 %d", res.StatusCode)
	}

	// ② 声明集带 off 以外的档位也拒绝：声明集是发给供应商的取值面，写了就会被忽略
	res = doJSON(t, "POST", ts.URL+"/api/v1/admin/models", adminToken, map[string]any{
		"kind": "chat", "provider_id": pid, "model_id": "gpt-x", "thinking_levels": "off,low",
	})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("openai-chat 声明档位集合应 400，实得 %d", res.StatusCode)
	}

	// ③ 关（off / 不传）放行
	res = doJSON(t, "POST", ts.URL+"/api/v1/admin/models", adminToken, map[string]any{
		"kind": "chat", "provider_id": pid, "model_id": "gpt-x",
	})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("缺省 off 应放行，实得 %d", res.StatusCode)
	}

	// ④ 单条更新改档位 → 拒绝
	id := modelIDOf(t, ts, adminToken, "gpt-x")
	res = doJSON(t, "PUT", ts.URL+"/api/v1/admin/models/"+itoa(id), adminToken, map[string]any{
		"model_id": "gpt-x", "thinking": "low",
	})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("openai-chat 更新档位应 400，实得 %d", res.StatusCode)
	}

	// ⑤ 供应商创建时一并带模型档位 → 拒绝
	res = doJSON(t, "POST", ts.URL+"/api/v1/admin/model-providers", adminToken, map[string]any{
		"api_type": "openai-chat", "name": "compat2", "base_url": "http://127.0.0.1:1", "api_key": "sk-x",
		"models": []map[string]any{{"id": "gpt-y", "kind": "chat", "thinking": "medium"}},
	})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("创建供应商时带档位应 400，实得 %d", res.StatusCode)
	}
}

// TestProviderProtocolSwitchRevalidatesThinking 改协议但不带 models 时，
// 名下旧条目的档位也必须重新校验：admin 的供应商表单正是「只提交 api_type/name/base_url」，
// 少了这道校验，改成 openai-chat 的供应商会原样留下 thinking=medium——该协议根本不发思考参数，
// 界面上却显示已配思考，形成「配了不生效」的静默错觉。
func TestProviderProtocolSwitchRevalidatesThinking(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	// 每个用例一个供应商，避免用例间互相影响（都要改协议）
	cases := []struct {
		name       string
		models     []map[string]any
		toAPIType  string
		wantStatus int
	}{
		{"改到 openai-chat：旧条目档位非 off → 拒绝",
			[]map[string]any{{"id": "m", "kind": "chat", "thinking": "medium", "thinking_levels": "off,low,medium,high"}},
			"openai-chat", http.StatusBadRequest},
		{"改到 openai-chat：旧条目档位全 off → 放行",
			[]map[string]any{{"id": "m", "kind": "chat"}},
			"openai-chat", http.StatusOK},
		{"改离 openai-chat（responses）：档位非 off 也放行",
			[]map[string]any{{"id": "m", "kind": "chat", "thinking": "medium", "thinking_levels": "off,low,medium,high"}},
			"openai-responses", http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pid := newThinkingProvider(t, ts, adminToken, "anthropic", "claude", c.models)
			// 只提交协议、不带 models：admin 的供应商表单正是这个形状
			res := doJSON(t, "PUT", ts.URL+"/api/v1/admin/model-providers/"+itoa(pid), adminToken, map[string]any{
				"api_type": c.toAPIType, "base_url": "http://127.0.0.1:1",
			})
			res.Body.Close()
			if res.StatusCode != c.wantStatus {
				t.Fatalf("改协议为 %s：应 %d，实得 %d", c.toAPIType, c.wantStatus, res.StatusCode)
			}
		})
	}
}

// 模型列表（admin 两条链）必须回显 thinking / thinking_levels / context_window
// —— 客户端靠它们显示当前档位、生成档位下拉、算历史预算。
func TestModelThinkingAppearsInProviderPayload(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	res := doJSON(t, "POST", ts.URL+"/api/v1/admin/model-providers", adminToken, map[string]any{
		"api_type": "anthropic", "name": "claude", "base_url": "http://127.0.0.1:1", "api_key": "sk-x",
		"models": []map[string]any{{
			"id": "claude-think", "kind": "chat", "thinking": "medium",
			"thinking_levels": "off,low,medium,high", "context_window": 200000,
		}},
	})
	var created struct {
		ID     int64 `json:"id"`
		Models []struct {
			Thinking       string `json:"thinking"`
			ThinkingLevels string `json:"thinking_levels"`
			ContextWindow  *int64 `json:"context_window"`
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
	if created.Models[0].ThinkingLevels != "off,low,medium,high" {
		t.Fatalf("供应商载荷里的模型 thinking_levels 应回显，实得 %q", created.Models[0].ThinkingLevels)
	}
	if created.Models[0].ContextWindow == nil || *created.Models[0].ContextWindow != 200000 {
		t.Fatalf("供应商载荷里的模型 context_window 应回显，实得 %v", created.Models[0].ContextWindow)
	}
}
