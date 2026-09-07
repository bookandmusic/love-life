package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// openaiResponsesAdapter 适配 OpenAI Responses API（POST /responses）。
// 与 Chat Completions 的差异：input 数组（非 messages）、tools 扁平（无 function 包裹）、
// 响应是 output 数组（非 choices）、tool 调用以 function_call/function_call_output + call_id 关联。
type openaiResponsesAdapter struct{}

func (openaiResponsesAdapter) Chat(ctx context.Context, baseURL, apiKey, model string, req *ChatRequest) (*ChatResponse, error) {
	systemPrompt, inputItems := buildResponsesInput(req.Messages)
	tools := buildResponsesTools(req.Tools)

	body := map[string]any{
		"model":       model,
		"input":       inputItems,
		"max_tokens":  16384, // Responses API 也接受 max_tokens；给一个足够大的上限
	}
	if systemPrompt != "" {
		body["instructions"] = systemPrompt
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/responses", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	res, err := openaiHTTP.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var raw struct {
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"output"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(res.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode model response: %w", err)
	}
	if raw.Error != nil {
		return nil, fmt.Errorf("模型服务错误: %s", raw.Error.Message)
	}

	out := &ChatResponse{}
	var choice struct {
		Message struct {
			Content   string     `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	}
	for _, item := range raw.Output {
		switch item.Type {
		case "message":
			for _, c := range item.Content {
				if c.Type == "output_text" && c.Text != "" {
					if choice.Message.Content != "" {
						choice.Message.Content += "\n"
					}
					choice.Message.Content += c.Text
				}
			}
		case "function_call":
			tc := ToolCall{ID: item.CallID, Type: "function"}
			tc.Function.Name = item.Name
			tc.Function.Arguments = item.Arguments
			choice.Message.ToolCalls = append(choice.Message.ToolCalls, tc)
		}
	}
	if len(raw.Output) == 0 {
		return nil, fmt.Errorf("模型返回空响应（HTTP %d）", res.StatusCode)
	}
	out.Choices = append(out.Choices, choice)
	return out, nil
}

// buildResponsesInput 把规范化 messages 翻译为 Responses API 的 input 数组，
// 同时抽出 system prompt（顶层 instructions 字段承载）。
func buildResponsesInput(msgs []Message) (system string, input []any) {
	for _, m := range msgs {
		switch m.Role {
		case "system":
			if system == "" {
				system = m.Content
			} else {
				system += "\n" + m.Content
			}
			continue
		case "assistant":
			if len(m.ToolCalls) == 0 {
				input = append(input, map[string]any{"role": "assistant", "content": m.Content})
				continue
			}
			// assistant 带 tool_calls：content 数组承载 function_call 项
			content := []any{}
			if m.Content != "" {
				content = append(content, map[string]any{"type": "output_text", "text": m.Content})
			}
			for _, tc := range m.ToolCalls {
				content = append(content, map[string]any{
					"type":      "function_call",
					"call_id":   tc.ID,
					"name":      tc.Function.Name,
					"arguments": tc.Function.Arguments,
				})
			}
			input = append(input, map[string]any{"role": "assistant", "content": content})
		case "tool":
			// 工具结果以 function_call_output 顶层项承载
			input = append(input, map[string]any{
				"type":    "function_call_output",
				"call_id": m.ToolCallID,
				"output":  m.Content,
			})
		default: // user
			input = append(input, map[string]any{"role": "user", "content": m.Content})
		}
	}
	return system, input
}

// buildResponsesTools 把规范化 Tool 翻译为 Responses API 的扁平 tools 形状。
func buildResponsesTools(tools []Tool) []any {
	out := make([]any, 0, len(tools))
	for _, t := range tools {
		out = append(out, map[string]any{
			"type":        "function",
			"name":        t.Function.Name,
			"description": t.Function.Description,
			"parameters":  t.Function.Parameters,
		})
	}
	return out
}
