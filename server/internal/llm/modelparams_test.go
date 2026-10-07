package llm

import (
	"context"
	"encoding/json"
	"testing"
)

// 模型级参数：ChatRequest 的 Temperature/MaxTokens 按协议 native 形状下发；
// 未设置时不传（协议必填项由各自安全默认兜底）。字段名写错、恒传/恒不传都会被本组测试捕获。

const (
	paramsTemp   = 0.7
	paramsMaxTok = int64(2048)
)

func sentBody(t *testing.T, bodyP *[]byte) map[string]any {
	t.Helper()
	var sent map[string]any
	if err := json.Unmarshal(*bodyP, &sent); err != nil {
		t.Fatalf("unmarshal sent body: %v: %s", err, string(*bodyP))
	}
	return sent
}

func TestAdaptersPassModelParams(t *testing.T) {
	temp, maxTok := paramsTemp, paramsMaxTok

	t.Run("openai-chat 顶层平铺", func(t *testing.T) {
		srv, _, bodyP := fakeChatUpstream(t, `{"choices":[{"message":{"content":"hi"}}]}`)
		if _, err := (openaiChatAdapter{}).Chat(context.Background(), srv.URL, "k", "m", &ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}}, Temperature: &temp, MaxTokens: &maxTok,
		}); err != nil {
			t.Fatalf("Chat: %v", err)
		}
		sent := sentBody(t, bodyP)
		if sent["temperature"] != paramsTemp {
			t.Errorf("temperature = %v, want %v", sent["temperature"], paramsTemp)
		}
		if sent["max_tokens"] != float64(paramsMaxTok) {
			t.Errorf("max_tokens = %v, want %d", sent["max_tokens"], paramsMaxTok)
		}
	})

	t.Run("openai-responses 覆盖兜底", func(t *testing.T) {
		srv, _, bodyP := fakeChatUpstream(t, `{"output":[{"type":"message","content":[{"type":"output_text","text":"hi"}]}]}`)
		if _, err := (openaiResponsesAdapter{}).Chat(context.Background(), srv.URL, "k", "m", &ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}}, Temperature: &temp, MaxTokens: &maxTok,
		}); err != nil {
			t.Fatalf("Chat: %v", err)
		}
		sent := sentBody(t, bodyP)
		if sent["temperature"] != paramsTemp {
			t.Errorf("temperature = %v, want %v", sent["temperature"], paramsTemp)
		}
		if sent["max_tokens"] != float64(paramsMaxTok) {
			t.Errorf("max_tokens = %v, want %d", sent["max_tokens"], paramsMaxTok)
		}
	})

	t.Run("anthropic 覆盖必填默认", func(t *testing.T) {
		srv, _, bodyP := fakeChatUpstream(t, `{"content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn"}`)
		if _, err := (anthropicAdapter{}).Chat(context.Background(), srv.URL, "k", "m", &ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}}, Temperature: &temp, MaxTokens: &maxTok,
		}); err != nil {
			t.Fatalf("Chat: %v", err)
		}
		sent := sentBody(t, bodyP)
		if sent["temperature"] != paramsTemp {
			t.Errorf("temperature = %v, want %v", sent["temperature"], paramsTemp)
		}
		if sent["max_tokens"] != float64(paramsMaxTok) {
			t.Errorf("max_tokens = %v, want %d", sent["max_tokens"], paramsMaxTok)
		}
	})

	t.Run("gemini generationConfig", func(t *testing.T) {
		srv, _, bodyP := fakeChatUpstream(t, `{"candidates":[{"content":{"parts":[{"text":"hi"}]},"finishReason":"STOP"}]}`)
		if _, err := (geminiAdapter{}).Chat(context.Background(), srv.URL, "k", "m", &ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}}, Temperature: &temp, MaxTokens: &maxTok,
		}); err != nil {
			t.Fatalf("Chat: %v", err)
		}
		sent := sentBody(t, bodyP)
		gen, ok := sent["generationConfig"].(map[string]any)
		if !ok {
			t.Fatalf("generationConfig missing: %v", sent)
		}
		if gen["temperature"] != paramsTemp {
			t.Errorf("generationConfig.temperature = %v, want %v", gen["temperature"], paramsTemp)
		}
		if gen["maxOutputTokens"] != float64(paramsMaxTok) {
			t.Errorf("generationConfig.maxOutputTokens = %v, want %d", gen["maxOutputTokens"], paramsMaxTok)
		}
	})
}

func TestAdaptersOmitUnsetModelParams(t *testing.T) {
	t.Run("openai-chat 两字段均不出现", func(t *testing.T) {
		srv, _, bodyP := fakeChatUpstream(t, `{"choices":[{"message":{"content":"hi"}}]}`)
		if _, err := (openaiChatAdapter{}).Chat(context.Background(), srv.URL, "k", "m", &ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}},
		}); err != nil {
			t.Fatalf("Chat: %v", err)
		}
		sent := sentBody(t, bodyP)
		if _, ok := sent["temperature"]; ok {
			t.Error("temperature must be absent when unset")
		}
		if _, ok := sent["max_tokens"]; ok {
			t.Error("max_tokens must be absent when unset")
		}
	})

	t.Run("openai-responses 保留兜底不传 temperature", func(t *testing.T) {
		srv, _, bodyP := fakeChatUpstream(t, `{"output":[{"type":"message","content":[{"type":"output_text","text":"hi"}]}]}`)
		if _, err := (openaiResponsesAdapter{}).Chat(context.Background(), srv.URL, "k", "m", &ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}},
		}); err != nil {
			t.Fatalf("Chat: %v", err)
		}
		sent := sentBody(t, bodyP)
		if _, ok := sent["temperature"]; ok {
			t.Error("temperature must be absent when unset")
		}
		if sent["max_tokens"] != float64(16384) {
			t.Errorf("max_tokens = %v, want 兜底 16384", sent["max_tokens"])
		}
	})

	t.Run("anthropic 保留必填默认不传 temperature", func(t *testing.T) {
		srv, _, bodyP := fakeChatUpstream(t, `{"content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn"}`)
		if _, err := (anthropicAdapter{}).Chat(context.Background(), srv.URL, "k", "m", &ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}},
		}); err != nil {
			t.Fatalf("Chat: %v", err)
		}
		sent := sentBody(t, bodyP)
		if _, ok := sent["temperature"]; ok {
			t.Error("temperature must be absent when unset")
		}
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
			t.Error("generationConfig must be absent when both params unset")
		}
	})
}
