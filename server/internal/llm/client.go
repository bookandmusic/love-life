// Package llm 模型客户端（L2 阶段一）：对外暴露规范化类型与 Client 薄壳，
// 协议 native 适配由未导出的 adapter 接口承担（按 api_type 选用）。
// 阶段一仅实现 openai-chat；其余协议后续阶段接入。
package llm

import (
	"context"
)

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// Client 模型客户端薄壳：按 api_type 持有 adapter，对外只暴露 Chat。
type Client struct {
	BaseURL string
	APIKey  string
	Model   string
	apiType string
	adp     adapter
}

// New 默认 openai-chat 协议（保持向后兼容）。
func New(baseURL, apiKey, model string) *Client {
	c, _ := NewForProtocol(baseURL, apiKey, model, APITypeOpenAIChat)
	return c
}

// NewForProtocol 按协议构造客户端；不支持的协议返回错误（调用方应处理，不静默）。
func NewForProtocol(baseURL, apiKey, model, apiType string) (*Client, error) {
	adp, err := selectAdapter(apiType)
	if err != nil {
		return nil, err
	}
	return &Client{BaseURL: baseURL, APIKey: apiKey, Model: model, apiType: apiType, adp: adp}, nil
}

type ChatRequest struct {
	Messages []Message `json:"messages"`
	Tools    []Tool    `json:"tools,omitempty"`
}

type ChatResponse struct {
	Choices []struct {
		Message struct {
			Content   string     `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	// Usage 上游返回的 token 用量（L5）。各协议字段名不同，由各 adapter 归一化后填入；
	// 上游未返回 usage 时为零值（不视为错误，统计侧如实记 0）。
	Usage Usage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Usage 归一化 token 用量（L5 统计与仪表盘 ai-usage 消费）。
type Usage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

// Chat 委托给当前协议的 adapter（规范化 ↔ native 由 adapter 负责）。
func (c *Client) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	return c.adp.Chat(ctx, c.BaseURL, c.APIKey, c.Model, req)
}
