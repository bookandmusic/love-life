// Package llm 协议适配：包内未导出的 adapter 接口把「规范化 ↔ 协议 native」
// 的双向映射封进各协议实现，chat 层与外部调用方永不感知具体协议。新增协议只需新增一个
// adapter 实现并在 selectAdapter 注册。
package llm

import (
	"context"
	"errors"
)

// 支持的协议类型常量（与 DB CHECK / listmodels.go 同步）。
const (
	APITypeOpenAIChat      = "openai-chat"
	APITypeOpenAIResponses = "openai-responses"
	APITypeAnthropic       = "anthropic"
	APITypeGemini          = "gemini"
)

// ErrUnsupportedAPIType 不支持的协议类型（与 listmodels.go 同文案，避免重复字面量）。
var ErrUnsupportedAPIType = errors.New("api_type 不支持（openai-chat / openai-responses / anthropic / gemini）")

// adapter 把规范化的 ChatRequest 翻译为协议 native 请求、发出、把 native 响应
// 翻译回规范化 ChatResponse。具体协议各自实现；工厂按 api_type 选用。
type adapter interface {
	Chat(ctx context.Context, baseURL, apiKey, model string, req *ChatRequest) (*ChatResponse, error)
	// StreamChat 流式：回调收到归一化增量（AI模式设计「模型网关」：真增量逐字下发）；
	// usage 在末帧到达；返回 finish_reason。
	StreamChat(ctx context.Context, baseURL, apiKey, model string, req *ChatRequest, onDelta func(Delta)) (string, error)
}

// Delta 归一化流式增量（四协议 adapter 归一后统一形状，chat 层零感知协议差异）。
type Delta struct {
	// Reasoning 思考链增量（逐字）；空串 = 本帧无思考内容
	Reasoning string
	// Content 正文增量（逐字）；空串 = 本帧无正文
	Content string
	// ToolCall 工具调用分片（按上游 index 归组后回传完整片段；arguments 为拼串）
	ToolCall *ToolCallDelta
	// Usage 末帧用量（仅最后一帧非 nil）
	Usage *Usage
}

// ToolCallDelta 流式工具调用分片：Index 用于跨帧归组（同一调用的 arguments 分片拼接）。
type ToolCallDelta struct {
	Index     int
	ID        string
	Name      string
	Arguments string // 增量片段（调用方按 index 累积，finish 后才解析）
}

// selectAdapter 按 api_type 选 adapter。所有协议 adapter 均已实现。
func selectAdapter(apiType string) (adapter, error) {
	switch apiType {
	case APITypeOpenAIChat:
		return openaiChatAdapter{}, nil
	case APITypeOpenAIResponses:
		return openaiResponsesAdapter{}, nil
	case APITypeAnthropic:
		return anthropicAdapter{}, nil
	case APITypeGemini:
		return geminiAdapter{}, nil
	default:
		return nil, ErrUnsupportedAPIType
	}
}

// applyModelParams 把模型级参数按「顶层平铺」形状注入 native 请求体
// （openai-chat / openai-responses / anthropic 三协议字段名一致，共用此注入；
// gemini 走 generationConfig，由其 adapter 自行组装）。nil = 未设置，不传。
func applyModelParams(body map[string]any, req *ChatRequest) {
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.MaxTokens != nil {
		body["max_tokens"] = *req.MaxTokens
	}
}
