package llm

import (
	"context"
	"encoding/json"
	"testing"
)

// 模型级参数：ChatRequest 的 MaxTokens 按协议 native 形状下发；未设置时不传
// （协议必填项由各自安全默认兜底）。字段名写错、恒传/恒不传都会被本组测试捕获。
//
// 不设 temperature：现役模型的思考参数恒开、与 temperature 互斥（同传即 400），
// 故该参数已从模型配置面删除——本组测试顺带守住「请求体里不会出现 temperature」。

const paramsMaxTok = int64(2048)

func sentBody(t *testing.T, bodyP *[]byte) map[string]any {
	t.Helper()
	var sent map[string]any
	if err := json.Unmarshal(*bodyP, &sent); err != nil {
		t.Fatalf("unmarshal sent body: %v: %s", err, string(*bodyP))
	}
	return sent
}

func TestAdaptersPassModelParams(t *testing.T) {
	maxTok := paramsMaxTok

	t.Run("openai-chat 顶层平铺", func(t *testing.T) {
		srv, _, bodyP := fakeChatUpstream(t, `{"choices":[{"message":{"content":"hi"}}]}`)
		if _, err := (openaiChatAdapter{}).Chat(context.Background(), srv.URL, "k", "m", &ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}}, MaxTokens: &maxTok,
		}); err != nil {
			t.Fatalf("Chat: %v", err)
		}
		sent := sentBody(t, bodyP)
		if sent["max_tokens"] != float64(paramsMaxTok) {
			t.Errorf("max_tokens = %v, want %d", sent["max_tokens"], paramsMaxTok)
		}
	})

	t.Run("openai-responses 顶层平铺 max_output_tokens", func(t *testing.T) {
		srv, _, bodyP := fakeChatUpstream(t, `{"output":[{"type":"message","content":[{"type":"output_text","text":"hi"}]}]}`)
		if _, err := (openaiResponsesAdapter{}).Chat(context.Background(), srv.URL, "k", "m", &ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}}, MaxTokens: &maxTok,
		}); err != nil {
			t.Fatalf("Chat: %v", err)
		}
		sent := sentBody(t, bodyP)
		if sent["max_output_tokens"] != float64(paramsMaxTok) {
			t.Errorf("max_output_tokens = %v, want %d", sent["max_output_tokens"], paramsMaxTok)
		}
		// Responses 不认 max_tokens：带上即被上游以 Unknown parameter 拒成 400
		if _, ok := sent["max_tokens"]; ok {
			t.Error("responses 请求体不得出现 max_tokens（上游会 400）")
		}
	})

	t.Run("anthropic 覆盖必填默认", func(t *testing.T) {
		srv, _, bodyP := fakeChatUpstream(t, `{"content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn"}`)
		if _, err := (anthropicAdapter{}).Chat(context.Background(), srv.URL, "k", "m", &ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}}, MaxTokens: &maxTok,
		}); err != nil {
			t.Fatalf("Chat: %v", err)
		}
		sent := sentBody(t, bodyP)
		if sent["max_tokens"] != float64(paramsMaxTok) {
			t.Errorf("max_tokens = %v, want %d", sent["max_tokens"], paramsMaxTok)
		}
	})

	t.Run("gemini generationConfig", func(t *testing.T) {
		srv, _, bodyP := fakeChatUpstream(t, `{"candidates":[{"content":{"parts":[{"text":"hi"}]},"finishReason":"STOP"}]}`)
		if _, err := (geminiAdapter{}).Chat(context.Background(), srv.URL, "k", "m", &ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}}, MaxTokens: &maxTok,
		}); err != nil {
			t.Fatalf("Chat: %v", err)
		}
		sent := sentBody(t, bodyP)
		gen, ok := sent["generationConfig"].(map[string]any)
		if !ok {
			t.Fatalf("generationConfig missing: %v", sent)
		}
		if gen["maxOutputTokens"] != float64(paramsMaxTok) {
			t.Errorf("generationConfig.maxOutputTokens = %v, want %d", gen["maxOutputTokens"], paramsMaxTok)
		}
	})
}

func TestAdaptersOmitUnsetModelParams(t *testing.T) {
	t.Run("openai-chat 不出现 max_tokens", func(t *testing.T) {
		srv, _, bodyP := fakeChatUpstream(t, `{"choices":[{"message":{"content":"hi"}}]}`)
		if _, err := (openaiChatAdapter{}).Chat(context.Background(), srv.URL, "k", "m", &ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}},
		}); err != nil {
			t.Fatalf("Chat: %v", err)
		}
		sent := sentBody(t, bodyP)
		if _, ok := sent["max_tokens"]; ok {
			t.Error("max_tokens must be absent when unset")
		}
	})

	t.Run("openai-responses 保留兜底 max_output_tokens", func(t *testing.T) {
		srv, _, bodyP := fakeChatUpstream(t, `{"output":[{"type":"message","content":[{"type":"output_text","text":"hi"}]}]}`)
		if _, err := (openaiResponsesAdapter{}).Chat(context.Background(), srv.URL, "k", "m", &ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}},
		}); err != nil {
			t.Fatalf("Chat: %v", err)
		}
		sent := sentBody(t, bodyP)
		if sent["max_output_tokens"] != float64(16384) {
			t.Errorf("max_output_tokens = %v, want 兜底 16384", sent["max_output_tokens"])
		}
	})

	t.Run("anthropic 保留必填默认", func(t *testing.T) {
		srv, _, bodyP := fakeChatUpstream(t, `{"content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn"}`)
		if _, err := (anthropicAdapter{}).Chat(context.Background(), srv.URL, "k", "m", &ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}},
		}); err != nil {
			t.Fatalf("Chat: %v", err)
		}
		sent := sentBody(t, bodyP)
		if sent["max_tokens"] != float64(anthropicDefaultMaxTokens) {
			t.Errorf("max_tokens = %v, want 默认 %d", sent["max_tokens"], anthropicDefaultMaxTokens)
		}
	})

	t.Run("gemini 不出现 generationConfig", func(t *testing.T) {
		srv, _, bodyP := fakeChatUpstream(t, `{"candidates":[{"content":{"parts":[{"text":"hi"}]},"finishReason":"STOP"}]}`)
		if _, err := (geminiAdapter{}).Chat(context.Background(), srv.URL, "k", "m", &ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}},
		}); err != nil {
			t.Fatalf("Chat: %v", err)
		}
		if _, ok := sentBody(t, bodyP)["generationConfig"]; ok {
			t.Error("generationConfig must be absent when max_tokens unset")
		}
	})
}

// TestAdaptersNeverSendTemperature 守住「temperature 已从参数面删除」：
// 四协议的请求体里都不得出现该字段（anthropic 尤其不能——它与思考参数互斥，同传即 400）。
func TestAdaptersNeverSendTemperature(t *testing.T) {
	cases := []struct {
		name string
		chat func(t *testing.T, req *ChatRequest) map[string]any
	}{
		{"openai-chat", func(t *testing.T, req *ChatRequest) map[string]any {
			srv, _, bodyP := fakeChatUpstream(t, `{"choices":[{"message":{"content":"hi"}}]}`)
			if _, err := (openaiChatAdapter{}).Chat(context.Background(), srv.URL, "k", "m", req); err != nil {
				t.Fatalf("Chat: %v", err)
			}
			return sentBody(t, bodyP)
		}},
		{"openai-responses", func(t *testing.T, req *ChatRequest) map[string]any {
			srv, _, bodyP := fakeChatUpstream(t, `{"output":[{"type":"message","content":[{"type":"output_text","text":"hi"}]}]}`)
			if _, err := (openaiResponsesAdapter{}).Chat(context.Background(), srv.URL, "k", "m", req); err != nil {
				t.Fatalf("Chat: %v", err)
			}
			return sentBody(t, bodyP)
		}},
		{"anthropic", func(t *testing.T, req *ChatRequest) map[string]any {
			srv, _, bodyP := fakeChatUpstream(t, `{"content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn"}`)
			if _, err := (anthropicAdapter{}).Chat(context.Background(), srv.URL, "k", "m", req); err != nil {
				t.Fatalf("Chat: %v", err)
			}
			return sentBody(t, bodyP)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sent := tc.chat(t, &ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}, Thinking: "medium"})
			if _, ok := sent["temperature"]; ok {
				t.Errorf("temperature must never be sent: %v", sent)
			}
		})
	}
	t.Run("gemini", func(t *testing.T) {
		srv, _, bodyP := fakeChatUpstream(t, `{"candidates":[{"content":{"parts":[{"text":"hi"}]},"finishReason":"STOP"}]}`)
		if _, err := (geminiAdapter{}).Chat(context.Background(), srv.URL, "k", "m", &ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}}, Thinking: "medium",
		}); err != nil {
			t.Fatalf("Chat: %v", err)
		}
		sent := sentBody(t, bodyP)
		if gen, ok := sent["generationConfig"].(map[string]any); ok {
			if _, has := gen["temperature"]; has {
				t.Errorf("generationConfig.temperature must never be sent: %v", gen)
			}
		}
	})
}
