package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// geminiAdapter 适配 Gemini generateContent（POST /v1beta/models/{model}:generateContent）。
// 差异：model 在 URL 路径、systemInstruction 顶层、role 是 user/model（非 assistant）、
// content 是 parts 数组、tool 调用以 functionCall/functionResponse + name 关联（无 call-id）。
type geminiAdapter struct{}

func (geminiAdapter) Chat(ctx context.Context, baseURL, apiKey, model string, req *ChatRequest) (*ChatResponse, error) {
	system, contents := buildGeminiContents(req.Messages)
	tools := buildGeminiTools(req.Tools)

	body := map[string]any{"contents": contents}
	if system != "" {
		body["systemInstruction"] = map[string]any{"parts": []any{map[string]any{"text": system}}}
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	url := baseURL + "/v1beta/models/" + model + ":generateContent"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", apiKey)

	res, err := openaiHTTP.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var raw struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text         string `json:"text"`
					FunctionCall struct {
						Name string          `json:"name"`
						Args json.RawMessage `json:"args"`
					} `json:"functionCall"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		// Gemini usageMetadata：promptTokenCount/candidatesTokenCount/totalTokenCount（L5 归一化）
		UsageMetadata *struct {
			PromptTokenCount     int64 `json:"promptTokenCount"`
			CandidatesTokenCount int64 `json:"candidatesTokenCount"`
			TotalTokenCount      int64 `json:"totalTokenCount"`
		} `json:"usageMetadata"`
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
	if len(raw.Candidates) == 0 {
		return nil, fmt.Errorf("模型返回空响应（HTTP %d）", res.StatusCode)
	}

	var choice struct {
		Message struct {
			Content   string     `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	}
	c := raw.Candidates[0]
	choice.FinishReason = c.FinishReason
	for idx, part := range c.Content.Parts {
		if part.Text != "" {
			if choice.Message.Content != "" {
				choice.Message.Content += "\n"
			}
			choice.Message.Content += part.Text
		}
		if part.FunctionCall.Name != "" {
			tc := ToolCall{
				// Gemini 无 call-id，合成一个用于规范化往返关联
				ID:   fmt.Sprintf("gemini-%s-%d", part.FunctionCall.Name, idx),
				Type: "function",
			}
			tc.Function.Name = part.FunctionCall.Name
			if len(part.FunctionCall.Args) > 0 {
				tc.Function.Arguments = string(part.FunctionCall.Args)
			} else {
				tc.Function.Arguments = "{}"
			}
			choice.Message.ToolCalls = append(choice.Message.ToolCalls, tc)
		}
	}

	out := &ChatResponse{}
	out.Choices = append(out.Choices, choice)
	// L5：归一化 usage（candidatesTokenCount = 输出 token）
	if raw.UsageMetadata != nil {
		out.Usage = Usage{
			PromptTokens:     raw.UsageMetadata.PromptTokenCount,
			CompletionTokens: raw.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      raw.UsageMetadata.TotalTokenCount,
		}
		if out.Usage.TotalTokens == 0 {
			out.Usage.TotalTokens = out.Usage.PromptTokens + out.Usage.CompletionTokens
		}
	}
	return out, nil
}

// buildGeminiContents 把规范化 messages 翻译为 Gemini 的 contents 数组。
// role 映射：assistant → model；user/tool → user。
// tool 结果以 functionResponse part 承载（response 必须是 object）。
// 历史中 assistant 的 tool_calls 还原为 functionCall part。
func buildGeminiContents(msgs []Message) (system string, contents []any) {
	for _, m := range msgs {
		switch m.Role {
		case "system":
			if system == "" {
				system = m.Content
			} else {
				system += "\n" + m.Content
			}
		case "assistant":
			parts := []any{}
			if m.Content != "" {
				parts = append(parts, map[string]any{"text": m.Content})
			}
			for _, tc := range m.ToolCalls {
				var args any
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
					args = map[string]any{"raw": tc.Function.Arguments}
				}
				parts = append(parts, map[string]any{
					"functionCall": map[string]any{
						"name": tc.Function.Name,
						"args": args,
					},
				})
			}
			contents = append(contents, map[string]any{"role": "model", "parts": parts})
		case "tool":
			// 工具结果：response 必须是 object；尝试把 Content 当 JSON 解析，失败则包一层
			var responseObj any
			if err := json.Unmarshal([]byte(m.Content), &responseObj); err != nil {
				responseObj = map[string]any{"result": m.Content}
			}
			name := toolNameFromCallID(m.ToolCallID)
			contents = append(contents, map[string]any{
				"role": "user",
				"parts": []any{map[string]any{
					"functionResponse": map[string]any{
						"name":     name,
						"response": responseObj,
					},
				}},
			})
		default: // user
			contents = append(contents, map[string]any{
				"role":  "user",
				"parts": []any{map[string]any{"text": m.Content}},
			})
		}
	}
	return system, contents
}

// toolNameFromCallID 从合成的 call-id（形如 gemini-{name}-{idx}）还原工具名；
// 非 gemini 前缀则原样返回（兼容历史）。
func toolNameFromCallID(id string) string {
	const prefix = "gemini-"
	if len(id) > len(prefix) && id[:len(prefix)] == prefix {
		rest := id[len(prefix):]
		// 剥尾部 -{idx}
		for i := len(rest) - 1; i > 0; i-- {
			if rest[i] == '-' {
				return rest[:i]
			}
		}
		return rest
	}
	return id
}

// buildGeminiTools 把规范化 Tool 翻译为 Gemini 的 functionDeclarations。
func buildGeminiTools(tools []Tool) []any {
	decls := make([]any, 0, len(tools))
	for _, t := range tools {
		entry := map[string]any{
			"name":        t.Function.Name,
			"description": t.Function.Description,
		}
		if t.Function.Parameters != nil {
			entry["parameters"] = t.Function.Parameters
		}
		decls = append(decls, entry)
	}
	if len(decls) == 0 {
		return nil
	}
	return []any{map[string]any{"functionDeclarations": decls}}
}
