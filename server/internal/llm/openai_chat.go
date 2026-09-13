package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// openaiChatAdapter 适配 OpenAI Chat Completions 协议（兼容生态：OpenAI / DeepSeek /
// 通义兼容模式 / GLM / vLLM / Ollama 等）。非流式——工具循环需要完整响应。
type openaiChatAdapter struct{}

// openaiHTTP 复用 HTTP 客户端（60s 超时，与原 Client.HTTP 一致）。
var openaiHTTP = &http.Client{Timeout: 60 * time.Second}

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
