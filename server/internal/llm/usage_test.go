package llm

import (
	"context"
	"testing"
)

// 四个协议适配器都必须把上游 usage 归一化到 ChatResponse.Usage。
// 各协议字段名不同（Responses: input/output；Anthropic: input/output 无 total；
// Gemini: usageMetadata + prompt/candidatesTokenCount），此处逐一锁定。

func TestUsageOpenAIChat(t *testing.T) {
	srv, _, _ := fakeChatUpstream(t,
		`{"choices":[{"message":{"content":"hi"},"finish_reason":"stop"}],
		  "usage":{"prompt_tokens":120,"completion_tokens":30,"total_tokens":150}}`)
	out, err := openaiChatAdapter{}.Chat(context.Background(), httpClientFor(0), srv.URL, "k", "m", &ChatRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Usage.PromptTokens != 120 || out.Usage.CompletionTokens != 30 || out.Usage.TotalTokens != 150 {
		t.Fatalf("usage = %+v", out.Usage)
	}
}

// 上游不返回 usage 时为零值，且不报错（统计侧如实记 0）。
func TestUsageMissingIsZeroNotError(t *testing.T) {
	srv, _, _ := fakeChatUpstream(t, `{"choices":[{"message":{"content":"hi"},"finish_reason":"stop"}]}`)
	out, err := openaiChatAdapter{}.Chat(context.Background(), httpClientFor(0), srv.URL, "k", "m", &ChatRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Usage.TotalTokens != 0 || out.Usage.PromptTokens != 0 {
		t.Fatalf("missing usage must be zero: %+v", out.Usage)
	}
}

func TestUsageOpenAIResponses(t *testing.T) {
	srv, _, _ := fakeChatUpstream(t,
		`{"output":[{"type":"message","content":[{"type":"output_text","text":"hi"}]}],
		  "usage":{"input_tokens":200,"output_tokens":50,"total_tokens":250}}`)
	out, err := openaiResponsesAdapter{}.Chat(context.Background(), httpClientFor(0), srv.URL, "k", "m", &ChatRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Usage.PromptTokens != 200 || out.Usage.CompletionTokens != 50 || out.Usage.TotalTokens != 250 {
		t.Fatalf("usage = %+v", out.Usage)
	}
}

// Responses 缺 total 时按 input+output 补齐。
func TestUsageOpenAIResponsesDerivesTotal(t *testing.T) {
	srv, _, _ := fakeChatUpstream(t,
		`{"output":[{"type":"message","content":[{"type":"output_text","text":"hi"}]}],
		  "usage":{"input_tokens":7,"output_tokens":3}}`)
	out, err := openaiResponsesAdapter{}.Chat(context.Background(), httpClientFor(0), srv.URL, "k", "m", &ChatRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Usage.TotalTokens != 10 {
		t.Fatalf("total should be derived = %d", out.Usage.TotalTokens)
	}
}

func TestUsageAnthropic(t *testing.T) {
	srv, _, _ := fakeChatUpstream(t,
		`{"content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn",
		  "usage":{"input_tokens":88,"output_tokens":12}}`)
	out, err := anthropicAdapter{}.Chat(context.Background(), httpClientFor(0), srv.URL, "k", "m", &ChatRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Usage.PromptTokens != 88 || out.Usage.CompletionTokens != 12 {
		t.Fatalf("usage = %+v", out.Usage)
	}
	// Anthropic 不返回 total → 按和补齐
	if out.Usage.TotalTokens != 100 {
		t.Fatalf("anthropic total = %d, want 100", out.Usage.TotalTokens)
	}
}

func TestUsageGemini(t *testing.T) {
	srv, _, _ := fakeChatUpstream(t,
		`{"candidates":[{"content":{"parts":[{"text":"hi"}]},"finishReason":"STOP"}],
		  "usageMetadata":{"promptTokenCount":64,"candidatesTokenCount":16,"totalTokenCount":80}}`)
	out, err := geminiAdapter{}.Chat(context.Background(), httpClientFor(0), srv.URL, "k", "m", &ChatRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Usage.PromptTokens != 64 || out.Usage.CompletionTokens != 16 || out.Usage.TotalTokens != 80 {
		t.Fatalf("usage = %+v", out.Usage)
	}
}
