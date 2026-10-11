package llm

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

// 思考档位的**按协议**注入守卫（现役代参数形态）。
//
// 只钉三件事：各协议的字段形状正确、会引起 400 的硬约束不被违反
// （gemini 的 thinkingLevel 与 thinkingBudget 互斥）、以及 temperature 一律不下发。

// chatBody 跑一次 Chat 并取回请求体（fakeChatUpstream 已解析并记下）。
func chatBody(t *testing.T, adp adapter, model string, req *ChatRequest) map[string]any {
	t.Helper()
	srv, _, bodyP := fakeChatUpstream(t, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],"output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}],"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","candidates":[{"content":{"parts":[{"text":"ok"}]}}],"usage":{}}`)
	if _, err := adp.Chat(context.Background(), httpClientFor(0), srv.URL, "key-x", model, req); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(*bodyP, &body); err != nil {
		t.Fatalf("解析请求体: %v", err)
	}
	return body
}

func TestAnthropicThinkingInjection(t *testing.T) {
	adp := anthropicAdapter{}
	msgs := []Message{{Role: "user", Content: "ping"}}

	// 各档位都发 adaptive + effort；现役代关不掉思考，「关」落到最低档
	for _, tc := range []struct{ level, effort string }{
		{"off", "low"}, // 关不掉 → 最低档
		{"low", "low"},
		{"medium", "medium"},
		{"high", "high"},
	} {
		body := chatBody(t, adp, "claude-opus-5-5", &ChatRequest{Messages: msgs, Thinking: tc.level})
		th, _ := body["thinking"].(map[string]any)
		if th == nil || th["type"] != "adaptive" {
			t.Fatalf("%s：应为 adaptive：%v", tc.level, body["thinking"])
		}
		if _, has := th["budget_tokens"]; has {
			t.Fatalf("%s：不得发 budget_tokens（已弃用）：%v", tc.level, th)
		}
		oc, _ := body["output_config"].(map[string]any)
		if oc == nil || oc["effort"] != tc.effort {
			t.Fatalf("%s：应带 output_config.effort=%s：%v", tc.level, tc.effort, body["output_config"])
		}
	}
}

// 调用方未指定档位（空串）时什么都不注入：档位由配置面决定，
// adapter 不猜模型代际，也不替调用方做选择。
func TestThinkingNotInjectedWhenUnspecified(t *testing.T) {
	msgs := []Message{{Role: "user", Content: "ping"}}

	t.Run("anthropic", func(t *testing.T) {
		body := chatBody(t, anthropicAdapter{}, "claude-opus-5-5", &ChatRequest{Messages: msgs})
		for _, k := range []string{"thinking", "output_config", "temperature"} {
			if _, has := body[k]; has {
				t.Fatalf("未指定档位不应注入 %s：%v", k, body)
			}
		}
	})

	t.Run("responses", func(t *testing.T) {
		body := chatBody(t, openaiResponsesAdapter{}, "gpt-5.5", &ChatRequest{Messages: msgs})
		if _, has := body["reasoning"]; has {
			t.Fatalf("未指定档位不应注入 reasoning：%v", body)
		}
	})

	t.Run("gemini", func(t *testing.T) {
		body := chatBody(t, geminiAdapter{}, "gemini-3.5-flash", &ChatRequest{Messages: msgs})
		if gen, ok := body["generationConfig"].(map[string]any); ok {
			if _, has := gen["thinkingConfig"]; has {
				t.Fatalf("未指定档位不应注入 thinkingConfig：%v", gen)
			}
		}
	})
}

func TestGeminiThinkingInjection(t *testing.T) {
	adp := geminiAdapter{}
	msgs := []Message{{Role: "user", Content: "ping"}}

	for _, tc := range []struct {
		level, want  string
		wantThoughts bool
	}{
		{"off", "low", false}, // 关不掉 → 最低档，且不请求摘要
		{"low", "low", true},
		{"medium", "medium", true},
		{"high", "high", true},
	} {
		body := chatBody(t, adp, "gemini-3.5-flash", &ChatRequest{Messages: msgs, Thinking: tc.level})
		gen, _ := body["generationConfig"].(map[string]any)
		cfg, _ := gen["thinkingConfig"].(map[string]any)
		if cfg == nil || cfg["thinkingLevel"] != tc.want {
			t.Fatalf("%s：应发 thinkingLevel=%s：%v", tc.level, tc.want, body["generationConfig"])
		}
		if cfg["includeThoughts"] != tc.wantThoughts {
			t.Fatalf("%s：includeThoughts 应为 %v：%v", tc.level, tc.wantThoughts, cfg)
		}
		// 与 thinkingLevel 互斥：同传即 400
		if _, has := cfg["thinkingBudget"]; has {
			t.Fatalf("%s：不得同传 thinkingBudget（同传报错）：%v", tc.level, cfg)
		}
	}
}

func TestResponsesThinkingInjection(t *testing.T) {
	adp := openaiResponsesAdapter{}
	msgs := []Message{{Role: "user", Content: "ping"}}

	// 「关」→ 最低档 minimal，且不请求摘要
	body := chatBody(t, adp, "gpt-5.5", &ChatRequest{Messages: msgs, Thinking: "off"})
	r, _ := body["reasoning"].(map[string]any)
	if r == nil || r["effort"] != "minimal" {
		t.Fatalf("off 应为 effort=minimal：%v", body["reasoning"])
	}
	if _, has := r["summary"]; has {
		t.Fatalf("off 不应请求思考摘要：%v", r)
	}

	// 其余档位直映 + summary:"auto"（否则不回摘要）
	for _, level := range []string{"low", "medium", "high"} {
		body = chatBody(t, adp, "gpt-5.5", &ChatRequest{Messages: msgs, Thinking: level})
		r, _ = body["reasoning"].(map[string]any)
		if r == nil || r["effort"] != level || r["summary"] != "auto" {
			t.Fatalf("%s：应发 reasoning{effort:%s,summary:auto}：%v", level, level, body["reasoning"])
		}
	}
}

// openai-chat 无统一思考参数：任何档位都不应向请求里加思考字段。
func TestOpenAIChatSendsNoThinkingParams(t *testing.T) {
	adp := openaiChatAdapter{}
	body := chatBody(t, adp, "gpt-5.5", &ChatRequest{
		Messages: []Message{{Role: "user", Content: "ping"}}, Thinking: "high",
	})
	for _, k := range []string{"thinking", "reasoning", "thinkingConfig", "generationConfig", "output_config"} {
		if _, has := body[k]; has {
			t.Fatalf("openai-chat 不应含 %s：%v", k, body)
		}
	}
}

// TestThinkingInjectionIgnoresModelName 注入只看**配置面给的档位**，绝不解析模型名：
// 名字带别名/日期后缀时按名分支会判错，判错即 400 或静默不生效。
// 同一个档位在**任意模型名下**必须得到完全相同的参数。
func TestThinkingInjectionIgnoresModelName(t *testing.T) {
	msgs := []Message{{Role: "user", Content: "ping"}}
	// 刻意混入别名、日期后缀、旧代名字与跨厂商名字：都不该改变结果
	names := []string{
		"claude-opus-5-5",
		"claude-opus-4-1-20250805",
		"claude-3-5-sonnet-20241022",
		"some-relay-alias/claude",
	}

	for _, level := range []string{"off", "low", "medium", "high"} {
		want := chatBody(t, anthropicAdapter{}, names[0], &ChatRequest{Messages: msgs, Thinking: level})
		for _, n := range names[1:] {
			got := chatBody(t, anthropicAdapter{}, n, &ChatRequest{Messages: msgs, Thinking: level})
			if !reflect.DeepEqual(got["thinking"], want["thinking"]) ||
				!reflect.DeepEqual(got["output_config"], want["output_config"]) {
				t.Fatalf("档位 %s 在模型名 %q 下注入不同：%v vs %v", level, n, got["output_config"], want["output_config"])
			}
		}
	}

	gemNames := []string{"gemini-3.1-pro-preview", "gemini-3.5-flash", "gemini-2.0-flash-exp", "relay/gemini-pro"}
	for _, level := range []string{"off", "low", "medium", "high"} {
		wantGen, _ := chatBody(t, geminiAdapter{}, gemNames[0], &ChatRequest{Messages: msgs, Thinking: level})["generationConfig"].(map[string]any)
		for _, n := range gemNames[1:] {
			gotGen, _ := chatBody(t, geminiAdapter{}, n, &ChatRequest{Messages: msgs, Thinking: level})["generationConfig"].(map[string]any)
			if !reflect.DeepEqual(gotGen["thinkingConfig"], wantGen["thinkingConfig"]) {
				t.Fatalf("gemini 档位 %s 在模型名 %q 下注入不同：%v vs %v", level, n, gotGen["thinkingConfig"], wantGen["thinkingConfig"])
			}
		}
	}

	respNames := []string{"gpt-5.5", "gpt-5", "o4-mini", "relay/gpt-5"}
	for _, level := range []string{"off", "low", "medium", "high"} {
		want := chatBody(t, openaiResponsesAdapter{}, respNames[0], &ChatRequest{Messages: msgs, Thinking: level})
		for _, n := range respNames[1:] {
			got := chatBody(t, openaiResponsesAdapter{}, n, &ChatRequest{Messages: msgs, Thinking: level})
			if !reflect.DeepEqual(got["reasoning"], want["reasoning"]) {
				t.Fatalf("responses 档位 %s 在模型名 %q 下注入不同：%v vs %v", level, n, got["reasoning"], want["reasoning"])
			}
		}
	}
}
