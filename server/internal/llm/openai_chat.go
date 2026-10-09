package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// openaiChatAdapter 适配 OpenAI Chat Completions 协议（兼容生态：OpenAI / DeepSeek /
// 通义兼容模式 / GLM / vLLM / Ollama 等）。流式：stream:true + include_usage，
// SSE 增量归一为 Delta 回调（AI模式设计「模型网关」）。
type openaiChatAdapter struct{}

// openaiHTTP 复用 HTTP 客户端。流式请求**不设请求总超时**（长回答会被 60s 砍断）：
// 连接期用 Transport.ResponseHeaderTimeout 兜住，整体长度由整轮墙钟预算控制
// （AI模式设计「模型网关」；该预算也约束链内逐个模型的重试，见「预算与并行」）。
// 改这个 30s 时一并看那两处。
var openaiHTTP = &http.Client{
	Timeout: 0,
	Transport: &http.Transport{
		ResponseHeaderTimeout: 30 * time.Second,
	},
}

// buildOpenAIChatBody 组装请求体。Chat 与 StreamChat 共用一处装配，避免两条路径各拼一份
// 而漂移（新增参数只改一边 = 「配了不生效」）。
func buildOpenAIChatBody(model string, req *ChatRequest, stream bool) map[string]any {
	body := map[string]any{
		"model":    model,
		"messages": stripSignatures(req.Messages),
		"tools":    req.Tools,
	}
	if stream {
		// include_usage：流式下 usage 只在末帧给出，不开就收不到用量。
		body["stream"] = true
		body["stream_options"] = map[string]any{"include_usage": true}
	}
	applyMaxTokens(body, req)
	return body
}

func (openaiChatAdapter) Chat(ctx context.Context, baseURL, apiKey, model string, req *ChatRequest) (*ChatResponse, error) {
	body := buildOpenAIChatBody(model, req, false)
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(bodyBytes))
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
	// 原生 usage 字段名与归一化命名一致（prompt_tokens/completion_tokens/total_tokens），
	// 故直接解到 ChatResponse.Usage。上游不返回 usage 时为零值。
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	return parseOpenAIChatBody(data, res.StatusCode)
}

// parseOpenAIChatBody 解析整块（非流式）响应体。非流式调用与「忽略 stream:true 直接回 JSON」
// 的兜底路径共用同一份解析，避免两处各写一遍解码与空响应判断。
func parseOpenAIChatBody(data []byte, status int) (*ChatResponse, error) {
	var out ChatResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("decode model response: %w", err)
	}
	if out.Error != nil {
		return nil, upstreamMessage(status, out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("模型返回空响应（HTTP %d）", status)
	}
	return &out, nil
}

// streamDelta openai-chat SSE 帧 choices[0].delta 形状（字段缺省 = 无该类增量）。
type streamDelta struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	}
	Usage *Usage `json:"usage,omitempty"`
}

// StreamChat 流式实现：SSE 逐帧解析 data: 行；[DONE] 结束。
// tool_calls 分片按 index 累积——adapter 内部完成归组，Agent 层只见完整 tool_call。
func (openaiChatAdapter) StreamChat(ctx context.Context, baseURL, apiKey, model string, req *ChatRequest, onDelta func(Delta)) (string, error) {
	body := buildOpenAIChatBody(model, req, true)
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	res, err := openaiHTTP.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if err := streamStatusError(res); err != nil {
		return "", err
	}
	// 非事件流响应（兼容生态对 stream:true 仍回 JSON）：一次性解析并回放 Delta，
	// 不重发请求（同一响应、同一计费）。回放顺序与原生流式一致，走共享实现。
	if !isEventStream(res) {
		return streamFallbackReplay(res, parseOpenAIChatBody, onDelta)
	}

	finish := ""
	var usage *Usage
	// 工具参数分片在**本 adapter 内**按 index 累积（AI模式设计「预算与并行」· tool_calls 增量累积：
	// 「累积在 adapter 内部完成，Agent 层只看到完整的 tool_call」，协议差异不外泄）。
	// 故这里不发半截 ToolCall，扫完整个流后按 index 发起顺序各发一次完整调用。
	type pendingCall struct {
		id   string
		name string
		args strings.Builder
	}
	pending := map[int]*pendingCall{}
	var pendingOrder []int
	sc := newSSEScanner(res.Body)
	for {
		data, ok := sc.next()
		if !ok {
			break
		}
		if data == "[DONE]" {
			break
		}
		var frame streamDelta
		if !sseJSON(data, &frame) {
			continue
		}
		if frame.Usage != nil {
			u := *frame.Usage
			usage = &u
		}
		if len(frame.Choices) == 0 {
			continue
		}
		c := frame.Choices[0]
		d := Delta{Reasoning: c.Delta.ReasoningContent, Content: c.Delta.Content}
		for _, tc := range c.Delta.ToolCalls {
			p, ok := pending[tc.Index]
			if !ok {
				p = &pendingCall{}
				pending[tc.Index] = p
				pendingOrder = append(pendingOrder, tc.Index)
			}
			// 首个分片可能就带 ID / 名称与前导参数片段，逐个覆盖与拼接都在这里做完
			if tc.ID != "" {
				p.id = tc.ID
			}
			if tc.Function.Name != "" {
				p.name = tc.Function.Name
			}
			p.args.WriteString(tc.Function.Arguments)
		}
		if d.Reasoning != "" || d.Content != "" {
			onDelta(d)
		}
		if c.FinishReason != "" {
			finish = c.FinishReason
		}
	}
	if err := sc.err(); err != nil {
		return "", err
	}
	// 整流通了再各发一次完整 tool_call（按 index 发起顺序，上层据 Index/顺序归位）
	for _, i := range pendingOrder {
		p := pending[i]
		onDelta(Delta{ToolCall: &ToolCallDelta{ID: p.id, Name: p.name, Arguments: p.args.String()}})
	}
	if usage != nil {
		onDelta(Delta{Usage: usage})
	}
	return finish, nil
}
