package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestOpenAIResponsesAdapter_RequestShape(t *testing.T) {
	// 单条文本响应：重点验证请求形状（input 数组、instructions 顶层、tools 扁平）
	srv, gotP, bodyP := fakeChatUpstream(t,
		`{"output":[{"type":"message","content":[{"type":"output_text","text":"hi"}]}]}`)

	adp := openaiResponsesAdapter{}
	req := &ChatRequest{
		Messages: []Message{
			{Role: "system", Content: "你是助手"},
			{Role: "user", Content: "ping"},
		},
		Tools: []Tool{{Type: "function", Function: ToolFunction{
			Name: "get_weather", Description: "d", Parameters: map[string]any{"type": "object"},
		}}},
	}
	resp, err := adp.Chat(context.Background(), srv.URL, "sk-test", "gpt-5", req)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	r := *gotP

	// 方法、路径、鉴权
	if r.Method != http.MethodPost || r.URL.Path != "/responses" {
		t.Errorf("req = %s %s, want POST /responses", r.Method, r.URL.Path)
	}
	if r.Header.Get("Authorization") != "Bearer sk-test" {
		t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
	}

	// 请求体：model / instructions / input / tools（扁平）
	var sent map[string]any
	if err := json.Unmarshal(*bodyP, &sent); err != nil {
		t.Fatalf("unmarshal sent: %v", err)
	}
	if sent["model"] != "gpt-5" {
		t.Errorf("model = %v", sent["model"])
	}
	if sent["instructions"] != "你是助手" {
		t.Errorf("instructions = %v, want 你是助手", sent["instructions"])
	}
	input, _ := sent["input"].([]any)
	// system 抽走，input 只剩 user
	if len(input) != 1 {
		t.Fatalf("input len = %d, want 1（system 抽出）: %+v", len(input), input)
	}
	first, _ := input[0].(map[string]any)
	if first["role"] != "user" || first["content"] != "ping" {
		t.Errorf("input[0] = %v", first)
	}
	tools, _ := sent["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools len = %d", len(tools))
	}
	tool0, _ := tools[0].(map[string]any)
	// 扁平：name/parameters 在 tool 层级，无 function 包裹
	if tool0["name"] != "get_weather" {
		t.Errorf("tools[0].name = %v, want get_weather", tool0["name"])
	}
	if _, hasFunc := tool0["function"]; hasFunc {
		t.Error("Responses tools must be flat, no 'function' wrapper")
	}

	if resp.Choices[0].Message.Content != "hi" {
		t.Errorf("content = %q", resp.Choices[0].Message.Content)
	}
}

func TestOpenAIResponsesAdapter_ToolRoundtrip(t *testing.T) {
	// 响应含 function_call → 验证 tool_call 映射
	srv, _, _ := fakeChatUpstream(t,
		`{"output":[{"type":"function_call","call_id":"call_1","name":"create_post","arguments":"{\"content\":\"hi\"}"}]}`)

	resp, err := openaiResponsesAdapter{}.Chat(context.Background(), srv.URL, "k", "m", &ChatRequest{
		Messages: []Message{{Role: "user", Content: "记一条"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	tc := resp.Choices[0].Message.ToolCalls
	if len(tc) != 1 || tc[0].ID != "call_1" || tc[0].Function.Name != "create_post" {
		t.Fatalf("tool_calls = %+v", tc)
	}
	if tc[0].Function.Arguments != `{"content":"hi"}` {
		t.Errorf("arguments = %q", tc[0].Function.Arguments)
	}
}

func TestOpenAIResponsesAdapter_ToolResultEcho(t *testing.T) {
	// 验证规范化 tool message 翻译为 function_call_output
	srv, _, bodyP := fakeChatUpstream(t,
		`{"output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`)

	_, err := openaiResponsesAdapter{}.Chat(context.Background(), srv.URL, "k", "m", &ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "记一条"},
			{Role: "assistant", ToolCalls: []ToolCall{newToolCall("call_1", "create_post", `{"content":"hi"}`)}},
			{Role: "tool", Content: `{"id":1}`, ToolCallID: "call_1"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	var sent map[string]any
	_ = json.Unmarshal(*bodyP, &sent)
	input, _ := sent["input"].([]any)
	// 期望最后一项是 function_call_output
	last, _ := input[len(input)-1].(map[string]any)
	if last["type"] != "function_call_output" {
		t.Fatalf("last input type = %v, want function_call_output: %+v", last["type"], last)
	}
	if last["call_id"] != "call_1" || last["output"] != `{"id":1}` {
		t.Errorf("function_call_output = %+v", last)
	}
}

func TestOpenAIResponsesAdapter_Errors(t *testing.T) {
	t.Run("error 字段", func(t *testing.T) {
		srv, _, _ := fakeChatUpstream(t, `{"output":[],"error":{"message":"bad key"}}`)
		_, err := openaiResponsesAdapter{}.Chat(context.Background(), srv.URL, "k", "m", &ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}}})
		if err == nil || !strings.Contains(err.Error(), "模型服务错误") || !strings.Contains(err.Error(), "bad key") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("空 output", func(t *testing.T) {
		srv, _, _ := fakeChatUpstream(t, `{"output":[]}`)
		_, err := openaiResponsesAdapter{}.Chat(context.Background(), srv.URL, "k", "m", &ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}}})
		if err == nil || !strings.Contains(err.Error(), "模型返回空响应") {
			t.Fatalf("err = %v", err)
		}
	})
}
