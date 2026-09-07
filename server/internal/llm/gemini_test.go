package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestGeminiAdapter_RequestShape(t *testing.T) {
	srv, gotP, bodyP := fakeChatUpstream(t,
		`{"candidates":[{"content":{"parts":[{"text":"hi"}]},"finishReason":"STOP"}]}`)

	adp := geminiAdapter{}
	req := &ChatRequest{
		Messages: []Message{
			{Role: "system", Content: "你是助手"},
			{Role: "user", Content: "ping"},
		},
		Tools: []Tool{{Type: "function", Function: ToolFunction{
			Name: "get_weather", Description: "d", Parameters: map[string]any{"type": "object"},
		}}},
	}
	resp, err := adp.Chat(context.Background(), srv.URL, "g-key", "gemini-1.5-pro", req)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	r := *gotP

	// model 在 URL 路径里
	if r.Method != http.MethodPost {
		t.Errorf("method = %q", r.Method)
	}
	if r.URL.Path != "/v1beta/models/gemini-1.5-pro:generateContent" {
		t.Errorf("path = %q, want model in path", r.URL.Path)
	}
	if r.Header.Get("x-goog-api-key") != "g-key" {
		t.Errorf("x-goog-api-key = %q", r.Header.Get("x-goog-api-key"))
	}

	// 请求体
	var sent map[string]any
	_ = json.Unmarshal(*bodyP, &sent)
	if _, ok := sent["systemInstruction"]; !ok {
		t.Error("systemInstruction 必须在顶层")
	}
	si, _ := sent["systemInstruction"].(map[string]any)
	parts, _ := si["parts"].([]any)
	part0, _ := parts[0].(map[string]any)
	if part0["text"] != "你是助手" {
		t.Errorf("systemInstruction.parts[0].text = %v", part0["text"])
	}
	contents, _ := sent["contents"].([]any)
	if len(contents) != 1 {
		t.Fatalf("contents len = %d, want 1（system 抽走）: %+v", len(contents), contents)
	}
	c0, _ := contents[0].(map[string]any)
	if c0["role"] != "user" {
		t.Errorf("contents[0].role = %v, want user", c0["role"])
	}
	toolsArr, _ := sent["tools"].([]any)
	if len(toolsArr) != 1 {
		t.Fatalf("tools len = %d", len(toolsArr))
	}
	tools0, _ := toolsArr[0].(map[string]any)
	decls, _ := tools0["functionDeclarations"].([]any)
	decl0, _ := decls[0].(map[string]any)
	if decl0["name"] != "get_weather" {
		t.Errorf("functionDeclarations[0].name = %v", decl0["name"])
	}

	if resp.Choices[0].Message.Content != "hi" {
		t.Errorf("content = %q", resp.Choices[0].Message.Content)
	}
}

func TestGeminiAdapter_FunctionCallResponse(t *testing.T) {
	srv, _, _ := fakeChatUpstream(t,
		`{"candidates":[{"content":{"parts":[`+
			`{"text":"checking"},`+
			`{"functionCall":{"name":"search_posts","args":{"query":"海边"}}}`+
			`]},"finishReason":"STOP"}]}`)

	resp, err := geminiAdapter{}.Chat(context.Background(), srv.URL, "k", "m", &ChatRequest{
		Messages: []Message{{Role: "user", Content: "海边"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Choices[0].Message.Content != "checking" {
		t.Errorf("content = %q", resp.Choices[0].Message.Content)
	}
	tc := resp.Choices[0].Message.ToolCalls
	if len(tc) != 1 || tc[0].Function.Name != "search_posts" {
		t.Fatalf("tool_calls = %+v", tc)
	}
	// call-id 由 adapter 合成（前缀 gemini-）
	if !strings.HasPrefix(tc[0].ID, "gemini-search_posts-") {
		t.Errorf("ID = %q, want 合成的 gemini- 前缀", tc[0].ID)
	}
	// args object → JSON 字符串
	if tc[0].Function.Arguments != `{"query":"海边"}` {
		t.Errorf("arguments = %q", tc[0].Function.Arguments)
	}
}

func TestGeminiAdapter_FunctionResponseEcho(t *testing.T) {
	srv, _, bodyP := fakeChatUpstream(t,
		`{"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`)

	// tool 消息的 ToolCallID 用合成的 gemini- 格式，验证 toolNameFromCallID 还原
	_, err := geminiAdapter{}.Chat(context.Background(), srv.URL, "k", "m", &ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "记一条"},
			{Role: "assistant", ToolCalls: []ToolCall{newToolCall("gemini-create_post-0", "create_post", `{"content":"hi"}`)}},
			{Role: "tool", Content: `{"id":1}`, ToolCallID: "gemini-create_post-0"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	var sent map[string]any
	_ = json.Unmarshal(*bodyP, &sent)
	contents, _ := sent["contents"].([]any)
	// 期望顺序：user / model(functionCall) / user(functionResponse)
	if len(contents) != 3 {
		t.Fatalf("contents len = %d, want 3: %+v", len(contents), contents)
	}
	// model 带 functionCall（role = model，非 assistant）
	model, _ := contents[1].(map[string]any)
	if model["role"] != "model" {
		t.Errorf("contents[1].role = %v, want model", model["role"])
	}
	mParts, _ := model["parts"].([]any)
	fcPart, _ := mParts[0].(map[string]any)
	fc, _ := fcPart["functionCall"].(map[string]any)
	if fc["name"] != "create_post" {
		t.Errorf("functionCall.name = %v", fc["name"])
	}
	// args 必须是 object
	args, ok := fc["args"].(map[string]any)
	if !ok {
		t.Errorf("functionCall.args should be object, got %T", fc["args"])
	} else if args["content"] != "hi" {
		t.Errorf("functionCall.args = %+v", args)
	}
	// user 带 functionResponse（response 必须是 object）
	resp, _ := contents[2].(map[string]any)
	if resp["role"] != "user" {
		t.Errorf("contents[2].role = %v, want user", resp["role"])
	}
	rParts, _ := resp["parts"].([]any)
	frPart, _ := rParts[0].(map[string]any)
	fr, _ := frPart["functionResponse"].(map[string]any)
	// name 从合成 call-id 还原
	if fr["name"] != "create_post" {
		t.Errorf("functionResponse.name = %v, want create_post（从合成 call-id 还原）", fr["name"])
	}
	frResp, ok := fr["response"].(map[string]any)
	if !ok {
		t.Errorf("functionResponse.response must be object, got %T", fr["response"])
	} else if frResp["id"] != float64(1) {
		t.Errorf("functionResponse.response = %+v", frResp)
	}
}

func TestToolNameFromCallID(t *testing.T) {
	tests := []struct{ in, want string }{
		{"gemini-create_post-0", "create_post"},
		{"gemini-search_posts-12", "search_posts"},
		{"toolu_abc", "toolu_abc"}, // 非 gemini 前缀原样返回
		{"gemini-solo", "solo"},    // 无尾部 -idx
		{"", ""},
	}
	for _, tc := range tests {
		if got := toolNameFromCallID(tc.in); got != tc.want {
			t.Errorf("toolNameFromCallID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestGeminiAdapter_Errors(t *testing.T) {
	t.Run("error 字段", func(t *testing.T) {
		srv, _, _ := fakeChatUpstream(t, `{"candidates":[],"error":{"message":"bad"}}`)
		_, err := geminiAdapter{}.Chat(context.Background(), srv.URL, "k", "m", &ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}}})
		if err == nil || !strings.Contains(err.Error(), "bad") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("空 candidates", func(t *testing.T) {
		srv, _, _ := fakeChatUpstream(t, `{"candidates":[]}`)
		_, err := geminiAdapter{}.Chat(context.Background(), srv.URL, "k", "m", &ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}}})
		if err == nil || !strings.Contains(err.Error(), "模型返回空响应") {
			t.Fatalf("err = %v", err)
		}
	})
}
