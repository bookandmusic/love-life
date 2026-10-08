package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// anthropicDefaultMaxTokens Anthropic Messages API 强制要求 max_tokens；
// 模型级未配置 max_tokens 时的安全默认。
const anthropicDefaultMaxTokens = 4096

// anthropicAdapter 适配 Anthropic Messages API（POST /v1/messages）。
// 差异：system 顶层、max_tokens 必填、messages 只含 user/assistant、
// content 是 block 数组、tool 调用以 tool_use/tool_result + tool_use_id 关联、
// tools 用 input_schema（非 parameters）。
type anthropicAdapter struct{}

// buildAnthropicBody 组装请求体。Chat 与 StreamChat 共用一处装配：两条路径各拼一份
// 迟早会漂移（新增参数只改一边），表现为「配了不生效」——那正是本设计要杜绝的失败模式。
func buildAnthropicBody(model string, req *ChatRequest, stream bool) map[string]any {
	system, messages := buildAnthropicMessages(req.Messages)
	tools := buildAnthropicTools(req.Tools)

	body := map[string]any{
		"model":      model,
		"max_tokens": anthropicDefaultMaxTokens,
		"messages":   messages,
	}
	if stream {
		body["stream"] = true
	}
	applyModelParams(body, req)
	// 思考参数：现役代 adaptive 恒开，档位映射见 llm/thinking.go（不解析模型名）。
	applyAnthropicThinking(body, req.Thinking)
	if system != "" {
		body["system"] = system
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}
	return body
}

func (anthropicAdapter) Chat(ctx context.Context, baseURL, apiKey, model string, req *ChatRequest) (*ChatResponse, error) {
	body := buildAnthropicBody(model, req, false)

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

	respBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	return parseAnthropicBody(respBody, res.StatusCode)
}

// anthropicRaw Anthropic Messages 响应体（Chat 与「非事件流兜底」共用同一解析）。
type anthropicRaw struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
		// thinking block 的思考链字段名是 thinking（不是 text）
		Thinking string          `json:"thinking"`
		ID       string          `json:"id"`
		Name     string          `json:"name"`
		Input    json.RawMessage `json:"input"`
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

func parseAnthropicBody(data []byte, status int) (*ChatResponse, error) {
	var raw anthropicRaw
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode model response: %w", err)
	}
	if raw.Error != nil {
		return nil, fmt.Errorf("模型服务错误: %s", raw.Error.Message)
	}
	if len(raw.Content) == 0 {
		return nil, fmt.Errorf("模型返回空响应（HTTP %d）", status)
	}

	var choice Choice
	for _, block := range raw.Content {
		switch block.Type {
		case "text":
			if choice.Message.Content != "" {
				choice.Message.Content += "\n"
			}
			choice.Message.Content += block.Text
		case "thinking":
			// 思考链：拼接但不与正文混流（流式路径同口径）
			choice.Message.Reasoning += block.Thinking
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

// anthropicStreamFrame Anthropic 流式帧。各事件类型字段互斥，故用一个大结构体承接，
// 只读与 type 匹配的那几个字段（比按 type 反序列化两次省一次拷贝）。
type anthropicStreamFrame struct {
	Type string `json:"type"`
	// message_start
	Message *struct {
		Usage *struct {
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message"`
	// content_block_start / content_block_stop
	Index        int `json:"index"`
	ContentBlock *struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"content_block"`
	// content_block_delta / message_delta
	Delta *struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	// message_delta
	Usage *struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// StreamChat 原生流式（POST /v1/messages + stream:true）。
//
// 事件时序：message_start（input usage）→ 每个内容块 content_block_start →
// 若干 content_block_delta → content_block_stop → message_delta（output usage + stop_reason）
// → message_stop。工具参数以 input_json_delta 分片到达，按 content_block 序号累积，
// **到 content_block_stop 才拼完下发**（AI模式设计「预算与并行」· 分片跨帧：
// 「累积器只拼字符串、不中途解析」）。
//
// signature_delta 是思考块完整性凭据，本仓不消费（思考链不进上下文），按规范忽略。
func (anthropicAdapter) StreamChat(ctx context.Context, baseURL, apiKey, model string, req *ChatRequest, onDelta func(Delta)) (string, error) {
	body := buildAnthropicBody(model, req, true)

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/messages", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	res, err := openaiHTTP.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if err := streamStatusError(res); err != nil {
		return "", err
	}
	// 非事件流兜底（中转网关忽略 stream:true 回整块 JSON）：按同一响应解析回放
	if !isEventStream(res) {
		return streamFallbackReplay(res, parseAnthropicBody, onDelta)
	}

	// 工具块累积器：key = content_block 序号。Anthropic 保证同一块的 start/delta/stop 相邻，
	// 但并行多个 tool_use 块会交错，故必须按序号分桶而不是「当前块」。
	type toolBlock struct {
		id   string
		name string
		args strings.Builder
	}
	blocks := map[int]*toolBlock{}

	finish := ""
	var promptTokens, completionTokens int64
	sc := newSSEScanner(res.Body)
	for {
		data, ok := sc.next()
		if !ok {
			break
		}
		var f anthropicStreamFrame
		if !sseJSON(data, &f) {
			continue
		}
		if f.Error != nil {
			return "", fmt.Errorf("模型服务错误: %s", f.Error.Message)
		}
		switch f.Type {
		case "message_start":
			if f.Message != nil && f.Message.Usage != nil {
				promptTokens = f.Message.Usage.InputTokens
			}
		case "content_block_start":
			if f.ContentBlock != nil && f.ContentBlock.Type == "tool_use" {
				blocks[f.Index] = &toolBlock{id: f.ContentBlock.ID, name: f.ContentBlock.Name}
			}
		case "content_block_delta":
			if f.Delta == nil {
				continue
			}
			switch f.Delta.Type {
			case "text_delta":
				if f.Delta.Text != "" {
					onDelta(Delta{Content: f.Delta.Text})
				}
			case "thinking_delta":
				if f.Delta.Thinking != "" {
					onDelta(Delta{Reasoning: f.Delta.Thinking})
				}
			case "input_json_delta":
				// 只拼字符串，不中途解析（分片可能把 JSON 截在任意字节处）
				if b := blocks[f.Index]; b != nil {
					b.args.WriteString(f.Delta.PartialJSON)
				}
			}
		case "content_block_stop":
			// 块拼完才下发完整调用：Agent 层只看到完整 tool_call，协议差异不外泄
			if b := blocks[f.Index]; b != nil {
				args := b.args.String()
				if args == "" {
					args = "{}"
				}
				onDelta(Delta{ToolCall: &ToolCallDelta{ID: b.id, Name: b.name, Arguments: args}})
				delete(blocks, f.Index)
			}
		case "message_delta":
			if f.Delta != nil && f.Delta.StopReason != "" {
				finish = f.Delta.StopReason
			}
			if f.Usage != nil {
				completionTokens = f.Usage.OutputTokens
			}
		}
	}
	if err := sc.err(); err != nil {
		return "", err
	}
	// Anthropic 不返回 total，按 input+output 求和（与 Chat 同一口径）
	u := Usage{PromptTokens: promptTokens, CompletionTokens: completionTokens,
		TotalTokens: promptTokens + completionTokens}
	onDelta(Delta{Usage: &u})
	return finish, nil
}
