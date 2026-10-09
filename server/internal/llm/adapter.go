// Package llm 协议适配：包内未导出的 adapter 接口把「规范化 ↔ 协议 native」
// 的双向映射封进各协议实现，chat 层与外部调用方永不感知具体协议。新增协议只需新增一个
// adapter 实现并在 selectAdapter 注册。
package llm

import (
	"context"
	"errors"
	"fmt"
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

// UpstreamError 上游（模型供应商）返回的错误。
//
// 单独成类型是为了让调用方按 **HTTP 状态** 分类（鉴权失败 / 参数被拒 / 限流 / 上游 5xx），
// 而不是去匹配错误字符串——措辞一改，字符串匹配就静默失效。
// StatusCode 为 0 表示错误来自流式帧里的 error 事件（没有 HTTP 状态可用）。
type UpstreamError struct {
	StatusCode int
	// Message 上游自述的原因（可能含英文与协议细节）。会被写入服务端日志；
	// 面向用户的文案由调用方按状态码决定，不直接透出。
	Message string
}

func (e *UpstreamError) Error() string {
	if e.Message != "" {
		return "模型服务错误: " + e.Message
	}
	return fmt.Sprintf("模型服务返回 HTTP %d", e.StatusCode)
}

// upstreamMessage 构造「上游自述原因」类错误（响应体里带 error.message 的情形）。
func upstreamMessage(status int, msg string) error {
	return &UpstreamError{StatusCode: status, Message: msg}
}

// upstreamStatus 构造「只有状态码」类错误（非 2xx 但响应体没给出可读原因）。
func upstreamStatus(status int) error {
	return &UpstreamError{StatusCode: status}
}

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

// ToolCallDelta 一次平台工具调用：**各 adapter 已在内部把分片按协议归组拼好**
// （AI模式设计「预算与并行」· tool_calls 增量累积：「累积在 adapter 内部完成，
// Agent 层只看到完整的 tool_call」），故这里给到上层的已是完整调用，不含协议特有的
// 分片序号——调用顺序由下发顺序表达。
type ToolCallDelta struct {
	ID        string
	Name      string
	Arguments string // 已拼全的参数 JSON（解析在上层执行）
	// Signature 协议私有思考凭据（gemini 3 系的 thoughtSignature），仅该协议填充；
	// 上层原样存进 ToolCall.Signature 以便历史回传（其余协议恒为空）。
	Signature string
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

// applyMaxTokens 把模型级输出上限注入请求体，字段名 `max_tokens`
// （openai-chat 与 anthropic 同形；nil = 未设置，不传，由协议必填默认兜底）。
//
// **openai-responses 不要用它**：那一侧的字段是 `max_output_tokens`，写成 max_tokens 会被
// 上游以 `Unknown parameter: 'max_tokens'` 拒成 400（该 bug 真实发生过）。
// 正因为字段名按协议不同，这里刻意不抽成「传字段名」的通用形状——那会让
// 「Responses 也走这个 helper」看起来是合法的。gemini 走 generationConfig，亦自行组装。
func applyMaxTokens(body map[string]any, req *ChatRequest) {
	if req.MaxTokens != nil {
		body["max_tokens"] = *req.MaxTokens
	}
}
