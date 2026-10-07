package llm

// 历史回放的协议映射（2026-09-22）：确认卡重组的工具往返必须能被各协议正确表达——
// openai-chat / responses / anthropic 靠 call-id 关联；gemini 靠工具名关联，
// 重组 ID 形如 card-<n>、反解不出名字，必须优先用 Message.ToolName。

import (
	"encoding/json"
	"testing"
)

// replayMessages 一次「已裁决卡片」重组的规范化往返。
func replayMessages() []Message {
	call := ToolCall{ID: "card-7", Type: "function"}
	call.Function.Name = "create_post"
	call.Function.Arguments = `{"content":"海边看日落"}`
	return []Message{
		{Role: "user", Content: "帮我发动态：海边看日落"},
		{Role: "assistant", ToolCalls: []ToolCall{call}},
		{Role: "tool", ToolCallID: "card-7", ToolName: "create_post", Content: "动态 #3 已发布"},
		{Role: "user", Content: "刚才成功了吗"},
	}
}

func TestReplayGeminiUsesToolName(t *testing.T) {
	_, contents := buildGeminiContents(replayMessages())
	var gotCall, gotResp string
	for _, c := range contents {
		cm, _ := c.(map[string]any)
		parts, _ := cm["parts"].([]any)
		for _, p := range parts {
			pm, _ := p.(map[string]any)
			if fc, ok := pm["functionCall"].(map[string]any); ok {
				gotCall, _ = fc["name"].(string)
			}
			if fr, ok := pm["functionResponse"].(map[string]any); ok {
				gotResp, _ = fr["name"].(string)
			}
		}
	}
	if gotCall != "create_post" || gotResp != "create_post" {
		t.Fatalf("gemini names: call=%q response=%q (want 工具名 create_post，不得回落 card-7)", gotCall, gotResp)
	}
}

func TestReplayAnthropicPairsToolUseID(t *testing.T) {
	_, msgs := buildAnthropicMessages(replayMessages())
	var sawUse, sawResult bool
	for _, m := range msgs {
		mm, _ := m.(map[string]any)
		blocks, _ := mm["content"].([]any)
		for _, b := range blocks {
			bm, _ := b.(map[string]any)
			if bm["type"] == "tool_use" && bm["id"] == "card-7" && bm["name"] == "create_post" {
				sawUse = true
			}
			if bm["type"] == "tool_result" && bm["tool_use_id"] == "card-7" {
				sawResult = true
			}
		}
	}
	if !sawUse || !sawResult {
		t.Fatalf("anthropic pairing broken: tool_use=%v tool_result=%v", sawUse, sawResult)
	}
}

func TestReplayResponsesPairsCallID(t *testing.T) {
	_, input := buildResponsesInput(replayMessages())
	// 按本适配器既有映射口径断言：function_call 承载在 assistant 消息的 content 数组内
	//（与在途往返同形状；Responses 规范顶层 item 形状问题另行观察，不在本次改动范围）
	var sawCall, sawOut bool
	for _, item := range input {
		im, _ := item.(map[string]any)
		if im["type"] == "function_call_output" && im["call_id"] == "card-7" {
			sawOut = true
		}
		content, _ := im["content"].([]any)
		for _, part := range content {
			pm, _ := part.(map[string]any)
			if pm["type"] == "function_call" && pm["call_id"] == "card-7" && pm["name"] == "create_post" {
				sawCall = true
			}
		}
	}
	if !sawCall || !sawOut {
		t.Fatalf("responses pairing broken: call=%v output=%v", sawCall, sawOut)
	}
}

// ToolName 不上网：openai-chat 直接序列化 Message，线上形状不得多出字段。
func TestReplayToolNameNotSerialized(t *testing.T) {
	b, err := json.Marshal(Message{Role: "tool", ToolCallID: "card-7", ToolName: "create_post", Content: "ok"})
	if err != nil {
		t.Fatal(err)
	}
	if json.Valid(b) && (string(b) != `{"role":"tool","content":"ok","tool_call_id":"card-7"}`) {
		t.Fatalf("wire shape changed: %s", b)
	}
}
