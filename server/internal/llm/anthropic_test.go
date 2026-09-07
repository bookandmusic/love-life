package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestAnthropicAdapter_RequestShape(t *testing.T) {
	srv, gotP, bodyP := fakeChatUpstream(t,
		`{"content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn"}`)

	adp := anthropicAdapter{}
	req := &ChatRequest{
		Messages: []Message{
			{Role: "system", Content: "你是助手"},
			{Role: "user", Content: "ping"},
		},
		Tools: []Tool{{Type: "function", Function: ToolFunction{
			Name: "get_weather", Description: "d", Parameters: map[string]any{"type": "object"},
		}}},
	}
	resp, err := adp.Chat(context.Background(), srv.URL, "key-x", "claude-3", req)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	r := *gotP

	// 路径 + 双鉴权头
	if r.Method != http.MethodPost || r.URL.Path != "/v1/messages" {
		t.Errorf("req = %s %s, want POST /v1/messages", r.Method, r.URL.Path)
	}
	if r.Header.Get("x-api-key") != "key-x" {
		t.Errorf("x-api-key = %q", r.Header.Get("x-api-key"))
	}
	if r.Header.Get("anthropic-version") != "2023-06-01" {
		t.Errorf("anthropic-version = %q", r.Header.Get("anthropic-version"))
	}

	// 请求体
	var sent map[string]any
	_ = json.Unmarshal(*bodyP, &sent)
	if sent["model"] != "claude-3" {
		t.Errorf("model = %v", sent["model"])
	}
	if sent["max_tokens"] != float64(anthropicDefaultMaxTokens) {
		t.Errorf("max_tokens = %v, want %d（默认）", sent["max_tokens"], anthropicDefaultMaxTokens)
	}
	if sent["system"] != "你是助手" {
		t.Errorf("system = %v, want 你是助手（顶层）", sent["system"])
	}
	msgs, _ := sent["messages"].([]any)
	// system 抽走，messages 只剩 user
	if len(msgs) != 1 {
		t.Fatalf("messages len = %d, want 1: %+v", len(msgs), msgs)
	}
	first, _ := msgs[0].(map[string]any)
	if first["role"] != "user" || first["content"] != "ping" {
		t.Errorf("messages[0] = %v", first)
	}
	tools, _ := sent["tools"].([]any)
	tool0, _ := tools[0].(map[string]any)
	if tool0["name"] != "get_weather" {
		t.Errorf("tools[0].name = %v", tool0["name"])
	}
	if _, ok := tool0["input_schema"]; !ok {
		t.Error("tools[0] must use input_schema, not parameters")
	}
	if _, ok := tool0["parameters"]; ok {
		t.Error("tools[0] must not have 'parameters' field")
	}

	if resp.Choices[0].Message.Content != "hi" {
		t.Errorf("content = %q", resp.Choices[0].Message.Content)
	}
	if resp.Choices[0].FinishReason != "end_turn" {
		t.Errorf("finish_reason = %q", resp.Choices[0].FinishReason)
	}
}

func TestAnthropicAdapter_ToolUseResponse(t *testing.T) {
	srv, _, _ := fakeChatUpstream(t,
		`{"content":[`+
			`{"type":"text","text":"let me check"},`+
			`{"type":"tool_use","id":"toolu_1","name":"search_posts","input":{"query":"海边"}}`+
			`],"stop_reason":"tool_use"}`)

	resp, err := anthropicAdapter{}.Chat(context.Background(), srv.URL, "k", "m", &ChatRequest{
		Messages: []Message{{Role: "user", Content: "海边"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Choices[0].Message.Content != "let me check" {
		t.Errorf("content = %q", resp.Choices[0].Message.Content)
	}
	tc := resp.Choices[0].Message.ToolCalls
	if len(tc) != 1 || tc[0].ID != "toolu_1" || tc[0].Function.Name != "search_posts" {
		t.Fatalf("tool_calls = %+v", tc)
	}
	// input object → JSON 字符串
	if tc[0].Function.Arguments != `{"query":"海边"}` {
		t.Errorf("arguments = %q, want JSON string", tc[0].Function.Arguments)
	}
}

func TestAnthropicAdapter_ToolResultEcho(t *testing.T) {
	srv, _, bodyP := fakeChatUpstream(t,
		`{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`)

	_, err := anthropicAdapter{}.Chat(context.Background(), srv.URL, "k", "m", &ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "记一条"},
			{Role: "assistant", ToolCalls: []ToolCall{newToolCall("toolu_1", "create_post", `{"content":"hi"}`)}},
			{Role: "tool", Content: `{"id":1}`, ToolCallID: "toolu_1"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	var sent map[string]any
	_ = json.Unmarshal(*bodyP, &sent)
	msgs, _ := sent["messages"].([]any)
	// 期望顺序：user / assistant(tool_use) / user(tool_result)
	if len(msgs) != 3 {
		t.Fatalf("messages len = %d, want 3: %+v", len(msgs), msgs)
	}
	// assistant 带 tool_use
	asst, _ := msgs[1].(map[string]any)
	if asst["role"] != "assistant" {
		t.Errorf("msgs[1].role = %v", asst["role"])
	}
	content, _ := asst["content"].([]any)
	blk, _ := content[0].(map[string]any)
	if blk["type"] != "tool_use" || blk["id"] != "toolu_1" {
		t.Errorf("assistant tool_use block = %+v", blk)
	}
	// input 应该是 object（反序列化自 arguments 字符串）
	in, ok := blk["input"].(map[string]any)
	if !ok {
		t.Errorf("input should be object, got %T: %+v", blk["input"], blk["input"])
	} else if in["content"] != "hi" {
		t.Errorf("input = %+v", in)
	}
	// tool 结果作为 user 消息中的 tool_result
	tr, _ := msgs[2].(map[string]any)
	if tr["role"] != "user" {
		t.Errorf("msgs[2].role = %v, want user", tr["role"])
	}
	trContent, _ := tr["content"].([]any)
	trBlk, _ := trContent[0].(map[string]any)
	if trBlk["type"] != "tool_result" || trBlk["tool_use_id"] != "toolu_1" || trBlk["content"] != `{"id":1}` {
		t.Errorf("tool_result block = %+v", trBlk)
	}
}

func TestAnthropicAdapter_Errors(t *testing.T) {
	t.Run("error 字段", func(t *testing.T) {
		srv, _, _ := fakeChatUpstream(t, `{"content":[],"error":{"message":"invalid"}}`)
		_, err := anthropicAdapter{}.Chat(context.Background(), srv.URL, "k", "m", &ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}}})
		if err == nil || !strings.Contains(err.Error(), "invalid") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("空 content", func(t *testing.T) {
		srv, _, _ := fakeChatUpstream(t, `{"content":[]}`)
		_, err := anthropicAdapter{}.Chat(context.Background(), srv.URL, "k", "m", &ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}}})
		if err == nil || !strings.Contains(err.Error(), "模型返回空响应") {
			t.Fatalf("err = %v", err)
		}
	})
}
