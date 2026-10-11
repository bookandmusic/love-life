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

// openaiResponsesAdapter 适配 OpenAI Responses API（POST /responses）。
// 与 Chat Completions 的差异：input 数组（非 messages）、tools 扁平（无 function 包裹）、
// 响应是 output 数组（非 choices）、tool 调用以 function_call/function_call_output + call_id 关联。
type openaiResponsesAdapter struct{}

// responsesDefaultMaxTokens 模型级未配置输出上限时的兜底值。
// `max_output_tokens` 本可省略，但省略后由供应商取默认值、不受我们控制，故显式兜底。
const responsesDefaultMaxTokens = 16384

// buildResponsesBody 组装请求体。Chat 与 StreamChat 共用一处装配，避免两条路径各拼一份
// 而漂移（新增参数只改一边 = 「配了不生效」）。
//
// 输出上限的字段是 **`max_output_tokens`**（不是 max_tokens，写成后者上游直接
// `Unknown parameter: 'max_tokens'` 400）。模型级未配置时用协议兜底：
// Responses 可省略该字段，但省略后由供应商取默认值、不受我们控制，故显式兜底。
func buildResponsesBody(model string, req *ChatRequest, stream bool) map[string]any {
	systemPrompt, inputItems := buildResponsesInput(req.Messages)
	tools := buildResponsesTools(req.Tools)

	body := map[string]any{
		"model": model,
		"input": inputItems,
	}
	// 字段名就地写死（本协议唯一用户）：写成 max_tokens 会被上游 400，
	// 放在调用点上让这处差异一眼可见，别再被抽进「三协议同名」的共用 helper。
	if req.MaxTokens != nil {
		body["max_output_tokens"] = *req.MaxTokens
	} else {
		body["max_output_tokens"] = responsesDefaultMaxTokens
	}
	if stream {
		body["stream"] = true
	}
	applyResponsesThinking(body, req.Thinking)
	if systemPrompt != "" {
		body["instructions"] = systemPrompt
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}
	return body
}

func (openaiResponsesAdapter) Chat(ctx context.Context, hc *http.Client, baseURL, apiKey, model string, req *ChatRequest) (*ChatResponse, error) {
	body := buildResponsesBody(model, req, false)

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

	res, err := hc.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	respBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	return parseResponsesBody(respBody, res.StatusCode)
}

// responsesRaw Responses API 整块响应体（Chat 与「非事件流兜底」共用同一解析）。
type responsesRaw struct {
	Output []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		// reasoning 项的思考摘要（summary[].text）；与 message 项的 content 同层
		Summary []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"summary"`
		CallID    string `json:"call_id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"output"`
	// Responses API 的 usage 用 input_tokens/output_tokens 命名（归一化）
	Usage *struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
		TotalTokens  int64 `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func parseResponsesBody(data []byte, status int) (*ChatResponse, error) {
	var raw responsesRaw
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode model response: %w", err)
	}
	if raw.Error != nil {
		return nil, upstreamMessage(status, raw.Error.Message)
	}
	if len(raw.Output) == 0 {
		return nil, fmt.Errorf("模型返回空响应（HTTP %d）", status)
	}

	out := &ChatResponse{}
	var choice Choice
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
		case "reasoning":
			// 思考摘要：summary[].text 拼成思考链（不回传上游，只做展示与落库）
			for _, s := range item.Summary {
				if s.Text != "" {
					choice.Message.Reasoning += s.Text
				}
			}
		case "function_call":
			tc := ToolCall{ID: item.CallID, Type: "function"}
			tc.Function.Name = item.Name
			tc.Function.Arguments = item.Arguments
			choice.Message.ToolCalls = append(choice.Message.ToolCalls, tc)
		}
	}
	out.Choices = append(out.Choices, choice)
	// 归一化 usage（上游字段名为 input/output；total 缺失时按两者之和补齐）
	if raw.Usage != nil {
		out.Usage = Usage{
			PromptTokens:     raw.Usage.InputTokens,
			CompletionTokens: raw.Usage.OutputTokens,
			TotalTokens:      raw.Usage.TotalTokens,
		}
		if out.Usage.TotalTokens == 0 {
			out.Usage.TotalTokens = out.Usage.PromptTokens + out.Usage.CompletionTokens
		}
	}
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

// StreamChat 原生流式（POST /responses + stream:true）。
//
// 事件时序：response.output_item.added（function_call 项在此拿到 name/call_id）→
// response.output_text.delta（正文）/ response.reasoning_summary_text.delta（思考摘要，
// 部分网关发 response.reasoning_text.delta，两个都认）/
// response.function_call_arguments.delta（参数分片）→ response.output_item.done
// （该事件的 arguments 是**拼好的完整串**）→ response.completed（usage 只在这里）。
//
// 参数分片按 item_id 分桶累积；output_item.done 到达时以它的完整串为准
// （同一 item 只下发一次），因为增量只是「过程中的碎片」，done 才是权威结果。
func (openaiResponsesAdapter) StreamChat(ctx context.Context, hc *http.Client, baseURL, apiKey, model string, req *ChatRequest, onDelta func(Delta)) (string, error) {
	body := buildResponsesBody(model, req, true)

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/responses", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	res, err := hc.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if err := streamStatusError(res); err != nil {
		return "", err
	}
	if !isEventStream(res) {
		return streamFallbackReplay(res, parseResponsesBody, onDelta)
	}

	// 工具项累积器：key = item_id（output_item.added 给 name/call_id，delta 给参数碎片）
	type callItem struct {
		callID    string
		name      string
		args      strings.Builder
		completed string // output_item.done 的权威完整串（非空则优先）
	}
	items := map[string]*callItem{}
	var order []string

	var usage *Usage
	sc := newSSEScanner(res.Body)
	for {
		data, ok := sc.next()
		if !ok {
			break
		}
		var f struct {
			Type      string `json:"type"`
			Delta     string `json:"delta"`
			ItemID    string `json:"item_id"`
			Arguments string `json:"arguments"`
			Item      *struct {
				Type      string `json:"type"`
				ID        string `json:"id"`
				CallID    string `json:"call_id"`
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"item"`
			Response *struct {
				Usage *struct {
					InputTokens  int64 `json:"input_tokens"`
					OutputTokens int64 `json:"output_tokens"`
					TotalTokens  int64 `json:"total_tokens"`
				} `json:"usage"`
				Error *struct {
					Message string `json:"message"`
				} `json:"error"`
			} `json:"response"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if !sseJSON(data, &f) {
			continue
		}
		if f.Error != nil {
			return "", upstreamMessage(0, f.Error.Message)
		}
		if f.Response != nil && f.Response.Error != nil {
			return "", upstreamMessage(0, f.Response.Error.Message)
		}
		switch f.Type {
		case "response.output_text.delta":
			if f.Delta != "" {
				onDelta(Delta{Content: f.Delta})
			}
		case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
			if f.Delta != "" {
				onDelta(Delta{Reasoning: f.Delta})
			}
		case "response.output_item.added":
			if f.Item != nil && f.Item.Type == "function_call" {
				it := &callItem{callID: f.Item.CallID, name: f.Item.Name}
				items[f.Item.ID] = it
				order = append(order, f.Item.ID)
			}
		case "response.function_call_arguments.delta":
			if it := items[f.ItemID]; it != nil {
				it.args.WriteString(f.Delta)
			}
		case "response.function_call_arguments.done":
			// 该事件的权威字段是 arguments（不是 delta，这里没有 delta）。
			if it := items[f.ItemID]; it != nil {
				it.completed = f.Arguments
			}
		case "response.output_item.done":
			if f.Item != nil && f.Item.Type == "function_call" {
				it := items[f.Item.ID]
				if it == nil {
					it = &callItem{callID: f.Item.CallID, name: f.Item.Name}
					items[f.Item.ID] = it
					order = append(order, f.Item.ID)
				}
				if it.callID == "" {
					it.callID = f.Item.CallID
				}
				if it.name == "" {
					it.name = f.Item.Name
				}
				if f.Item.Arguments != "" {
					it.completed = f.Item.Arguments
				}
			}
		case "response.completed":
			if f.Response != nil && f.Response.Usage != nil {
				u := Usage{
					PromptTokens:     f.Response.Usage.InputTokens,
					CompletionTokens: f.Response.Usage.OutputTokens,
					TotalTokens:      f.Response.Usage.TotalTokens,
				}
				if u.TotalTokens == 0 {
					u.TotalTokens = u.PromptTokens + u.CompletionTokens
				}
				usage = &u
			}
		}
	}
	if err := sc.err(); err != nil {
		return "", err
	}
	// 工具调用在流结束后按出现顺序下发（与 OpenAI Chat 同一契约：
	// Agent 层只看到完整 tool_call，且不会在参数还半截时就去执行）
	for _, id := range order {
		it := items[id]
		args := it.completed
		if args == "" {
			args = it.args.String()
		}
		if args == "" {
			args = "{}"
		}
		onDelta(Delta{ToolCall: &ToolCallDelta{ID: it.callID, Name: it.name, Arguments: args}})
	}
	if usage != nil {
		onDelta(Delta{Usage: usage})
	}
	return "", nil
}
