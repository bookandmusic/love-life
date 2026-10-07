package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
// （AI模式设计「模型网关」）。
var openaiHTTP = &http.Client{
	Timeout: 0,
	Transport: &http.Transport{
		ResponseHeaderTimeout: 30 * time.Second,
	},
}

func (openaiChatAdapter) Chat(ctx context.Context, baseURL, apiKey, model string, req *ChatRequest) (*ChatResponse, error) {
	body := map[string]any{
		"model":    model,
		"messages": req.Messages,
		"tools":    req.Tools,
	}
	applyModelParams(body, req)
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
	var out ChatResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode model response: %w", err)
	}
	if out.Error != nil {
		return nil, fmt.Errorf("模型服务错误: %s", out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("模型返回空响应（HTTP %d）", res.StatusCode)
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
	body := map[string]any{
		"model":          model,
		"messages":       req.Messages,
		"tools":          req.Tools,
		"stream":         true,
		"stream_options": map[string]any{"include_usage": true},
	}
	applyModelParams(body, req)
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
	if res.StatusCode != http.StatusOK {
		var out ChatResponse
		_ = json.NewDecoder(res.Body).Decode(&out)
		if out.Error != nil {
			return "", fmt.Errorf("模型服务错误: %s", out.Error.Message)
		}
		return "", fmt.Errorf("模型服务返回 HTTP %d", res.StatusCode)
	}
	// 非事件流响应（兼容生态对 stream:true 仍回 JSON）：一次性解析并回放 Delta，
	// 不重发请求（同一响应、同一计费）。
	if ct := res.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		var out ChatResponse
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			return "", fmt.Errorf("decode model response: %w", err)
		}
		if out.Error != nil {
			return "", fmt.Errorf("模型服务错误: %s", out.Error.Message)
		}
		if len(out.Choices) == 0 {
			return "", fmt.Errorf("模型返回空响应（HTTP %d）", res.StatusCode)
		}
		m := out.Choices[0].Message
		if m.Reasoning != "" {
			onDelta(Delta{Reasoning: m.Reasoning})
		}
		if m.Content != "" {
			onDelta(Delta{Content: m.Content})
		}
		for i := range m.ToolCalls {
			onDelta(Delta{ToolCall: &ToolCallDelta{Index: i, ID: m.ToolCalls[i].ID,
				Name: m.ToolCalls[i].Function.Name, Arguments: m.ToolCalls[i].Function.Arguments}})
		}
		u := out.Usage
		onDelta(Delta{Usage: &u})
		return out.Choices[0].FinishReason, nil
	}

	finish := ""
	var usage *Usage
	scanner := bufio.NewScanner(res.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		var frame streamDelta
		if json.Unmarshal([]byte(data), &frame) != nil {
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
			d.ToolCall = &ToolCallDelta{Index: tc.Index, ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments}
			onDelta(d)
			d.ToolCall = nil
		}
		if d.Reasoning != "" || d.Content != "" {
			onDelta(d)
		}
		if c.FinishReason != "" {
			finish = c.FinishReason
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if usage != nil {
		onDelta(Delta{Usage: usage})
	}
	return finish, nil
}
