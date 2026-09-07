// Package llm 协议适配（L2 阶段一）：包内未导出的 adapter 接口把「规范化 ↔ 协议 native」
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
}

// selectAdapter 按 api_type 选 adapter。所有协议 adapter 均已实现（阶段二）。
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
