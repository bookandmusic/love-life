package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// anthropicDefaultMaxTokens Anthropic Messages API 强制要求 max_tokens；
// 模型级未配置 max_tokens 时的安全默认。
const anthropicDefaultMaxTokens = 4096

// anthropicAdapter 适配 Anthropic Messages API（POST /v1/messages）。
// 差异：system 顶层、max_tokens 必填、messages 只含 user/assistant、
// content 是 block 数组、tool 调用以 tool_use/tool_result + tool_use_id 关联、
// tools 用 input_schema（非 parameters）。
type anthropicAdapter struct{}

func (anthropicAdapter) Chat(ctx context.Context, baseURL, apiKey, model string, req *ChatRequest) (*ChatResponse, error) {
	system, messages := buildAnthropicMessages(req.Messages)
	tools := buildAnthropicTools(req.Tools)

	body := map[string]any{
		"model":      model,
		"max_tokens": anthropicDefaultMaxTokens,
		"messages":   messages,
	}
	applyModelParams(body, req)
	if system != "" {
		body["system"] = system
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/messages", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	res, err := openaiHTTP.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var raw struct {
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		// Anthropic usage：input_tokens/output_tokens（归一化）
		Usage *struct {
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
		} `json:"usage"`
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
	if len(raw.Content) == 0 {
		return nil, fmt.Errorf("模型返回空响应（HTTP %d）", res.StatusCode)
	}

	var choice struct {
		Message struct {
			Content   string     `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	}
	for _, block := range raw.Content {
		switch block.Type {
		case "text":
			if choice.Message.Content != "" {
				choice.Message.Content += "\n"
			}
			choice.Message.Content += block.Text
		case "tool_use":
			tc := ToolCall{ID: block.ID, Type: "function"}
			tc.Function.Name = block.Name
			// input 是 object；规范化要求 arguments 是 JSON 字符串
			if len(block.Input) > 0 {
				tc.Function.Arguments = string(block.Input)
			} else {
				tc.Function.Arguments = "{}"
			}
			choice.Message.ToolCalls = append(choice.Message.ToolCalls, tc)
		}
	}
	choice.FinishReason = raw.StopReason

	out := &ChatResponse{}
	out.Choices = append(out.Choices, choice)
	// Anthropic 不返回 total，按 input+output 求和
	if raw.Usage != nil {
		out.Usage = Usage{
			PromptTokens:     raw.Usage.InputTokens,
			CompletionTokens: raw.Usage.OutputTokens,
			TotalTokens:      raw.Usage.InputTokens + raw.Usage.OutputTokens,
		}
	}
	return out, nil
}

// buildAnthropicMessages 把规范化 messages 翻译为 Anthropic 形状：
// system 抽出到顶层；user/assistant 保留；连续 tool 消息合并进一个 user 消息（tool_result blocks）。
func buildAnthropicMessages(msgs []Message) (system string, out []any) {
	for _, m := range msgs {
		switch m.Role {
		case "system":
			if system == "" {
				system = m.Content
			} else {
				system += "\n" + m.Content
			}
		case "assistant":
			content := []any{}
			if m.Content != "" {
				content = append(content, map[string]any{"type": "text", "text": m.Content})
			}
			for _, tc := range m.ToolCalls {
				// arguments 是 JSON 字符串；Anthropic 要 input object
				var input any
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
					input = map[string]any{"raw": tc.Function.Arguments}
				}
				content = append(content, map[string]any{
					"type":  "tool_use",
					"id":    tc.ID,
					"name":  tc.Function.Name,
					"input": input,
				})
			}
			out = append(out, map[string]any{"role": "assistant", "content": content})
		case "tool":
			// tool 结果作为 user 消息中的 tool_result block
			out = append(out, map[string]any{
				"role": "user",
				"content": []any{map[string]any{
					"type":        "tool_result",
					"tool_use_id": m.ToolCallID,
					"content":     m.Content,
				}},
			})
		default: // user
			out = append(out, map[string]any{"role": "user", "content": m.Content})
		}
	}
	return system, out
}

// buildAnthropicTools 把规范化 Tool 翻译为 Anthropic 的 tools 形状（input_schema）。
func buildAnthropicTools(tools []Tool) []any {
	out := make([]any, 0, len(tools))
	for _, t := range tools {
		entry := map[string]any{
			"name":        t.Function.Name,
			"description": t.Function.Description,
		}
		if t.Function.Parameters != nil {
			entry["input_schema"] = t.Function.Parameters
		} else {
			entry["input_schema"] = map[string]any{"type": "object"}
		}
		out = append(out, entry)
	}
	return out
}
