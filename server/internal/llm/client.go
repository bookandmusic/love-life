// Package llm 模型客户端：对外暴露规范化类型与 Client 薄壳，
// 协议 native 适配由未导出的 adapter 接口承担（按 api_type 选用）。
package llm

import (
	"context"
	"net/http"
	"sync"
	"time"
)

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	// ToolName 供 gemini 回放取工具名：该协议的回执虽然也带 id，但历史往返的 ID 可能形如
	// card-<n>、反解不出名字，故优先用它。不上网（json:"-"），其余协议靠 ID 关联、忽略本字段。
	ToolName string `json:"-"`
}

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
	// Signature 协议私有的思考凭据（gemini 3 系的 thoughtSignature）。
	// 它是**part 级**字段（与 functionCall 平级），不是 functionCall 的子字段；
	// gemini 3 在工具往返时强制要求原样带回，缺失即 400。带上 json tag 是因为
	// 它必须随 tool_calls_json 落库、跨轮回放；其余协议不回传该字段
	// （openai-chat 由 stripSignatures 剥掉，anthropic / responses 逐字段自建结构）。
	Signature string `json:"signature,omitempty"`
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

// headerTimeoutDefault 等待上游响应头（首包）的默认超时。上游推理层挂起时据此快速判死切链
// （AI模式设计「预算与并行」：链重试受整轮墙钟约束）；供应商可在后台按条目覆盖。
const headerTimeoutDefault = 30 * time.Second

// httpClients 按响应头超时值缓存 HTTP 客户端：同一超时值的供应商共享连接池，
// 不为每个供应商各建一份 Transport。键只可能是后台配置出现的少数几个值。
var httpClients sync.Map // time.Duration → *http.Client

// httpClientFor 取响应头超时为 headerTimeout 的共享客户端。流式请求**不设请求总超时**
// （长回答会被砍断），整体长度由 chat 层的整轮墙钟预算控制。
func httpClientFor(headerTimeout time.Duration) *http.Client {
	if headerTimeout <= 0 {
		headerTimeout = headerTimeoutDefault
	}
	if c, ok := httpClients.Load(headerTimeout); ok {
		return c.(*http.Client)
	}
	c := &http.Client{
		Timeout: 0,
		Transport: &http.Transport{
			ResponseHeaderTimeout: headerTimeout,
		},
	}
	actual, _ := httpClients.LoadOrStore(headerTimeout, c)
	return actual.(*http.Client)
}

// Client 模型客户端薄壳：按 api_type 持有 adapter，对外只暴露 Chat。
type Client struct {
	BaseURL string
	APIKey  string
	Model   string
	apiType string
	http    *http.Client
	adp     adapter
}

// New 默认 openai-chat 协议（保持向后兼容）。
func New(baseURL, apiKey, model string) *Client {
	c, _ := NewForProtocol(baseURL, apiKey, model, APITypeOpenAIChat, 0)
	return c
}

// NewForProtocol 按协议构造客户端；不支持的协议返回错误（调用方应处理，不静默）。
// headerTimeout 为等待上游响应头（首包）的超时，0 = 默认 30s；供应商可在后台按条目配置。
func NewForProtocol(baseURL, apiKey, model, apiType string, headerTimeout time.Duration) (*Client, error) {
	adp, err := selectAdapter(apiType)
	if err != nil {
		return nil, err
	}
	return &Client{BaseURL: baseURL, APIKey: apiKey, Model: model, apiType: apiType, http: httpClientFor(headerTimeout), adp: adp}, nil
}

type ChatRequest struct {
	Messages []Message `json:"messages"`
	Tools    []Tool    `json:"tools,omitempty"`
	// MaxTokens 模型级输出上限：nil = 未设置，native 请求组装时不传
	// （各协议必填项由 adapter 用自己的安全默认兜底）。ChatRequest 不直接序列化。
	MaxTokens *int64
	// Thinking 思考档位（off/low/medium/high，见 store 同名常量）。
	// 空串 = 调用方未指定档位 → 不注入任何思考参数；生产一律经 store.NormalizeThinking，
	// 故「关」是显式 off → 各 adapter 按**协议**映射到最低档（现役模型关不掉思考，
	// 省略参数会让模型留在自己的默认档，形成「配了不生效」）。adapter 不解析模型名。
	Thinking string
}

// ChoiceMessage 归一化的模型回复：正文 + 思考链 + 工具调用。
type ChoiceMessage struct {
	Content string `json:"content"`
	// 思考链：OpenAI 兼容协议的 reasoning_content（DeepSeek-R1 /
	// SiliconFlow 思考模型等）。仅展示转发用，不进模型历史（回传会污染后续轮次）；
	// 其他协议不下发该字段时为零值，上层按「无思考」处理
	Reasoning string     `json:"reasoning_content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls"`
}

// Choice 归一化的单候选响应。
type Choice struct {
	Message      ChoiceMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type ChatResponse struct {
	Choices []Choice
	// Usage 上游返回的 token 用量。各协议字段名不同，由各 adapter 归一化后填入；
	// 上游未返回 usage 时为零值（不视为错误，统计侧如实记 0）。
	Usage Usage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Usage 归一化 token 用量（调用统计与仪表盘 ai-usage 消费）。
type Usage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

// stripSignatures 剥掉 ToolCall.Signature 后返回消息副本。
//
// Signature 是 gemini 3 系的私有思考凭据：它随 tool_calls_json 落库并跨轮回放，
// 但**只有 gemini 协议认它**。openai-chat 直接把 Message 序列化进请求体，
// 若不清掉就会把 `signature` 塞进 tool_calls 里发给 OpenAI / DeepSeek 等上游——
// 未知字段轻则被拒（400），重则被部分网关当作参数处理。其余协议逐字段自建请求
// 结构（anthropic 只取 id/name/arguments，responses 同理），不存在这个泄漏面。
//
// 无签名时原样返回入参（不复制），避免每轮请求都多一次全量拷贝。
func stripSignatures(msgs []Message) []Message {
	has := false
	for _, m := range msgs {
		for _, tc := range m.ToolCalls {
			if tc.Signature != "" {
				has = true
				break
			}
		}
		if has {
			break
		}
	}
	if !has {
		return msgs
	}
	out := make([]Message, len(msgs))
	copy(out, msgs)
	for i := range out {
		if len(out[i].ToolCalls) == 0 {
			continue
		}
		calls := make([]ToolCall, len(out[i].ToolCalls))
		copy(calls, out[i].ToolCalls)
		for j := range calls {
			calls[j].Signature = ""
		}
		out[i].ToolCalls = calls
	}
	return out
}

// Chat 委托给当前协议的 adapter（规范化 ↔ native 由 adapter 负责）。
func (c *Client) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	return c.adp.Chat(ctx, c.http, c.BaseURL, c.APIKey, c.Model, req)
}

// StreamChat 流式：协议差异封进 adapter（分片归组/usage 末帧/finish_reason），
// 调用方只消费归一化 Delta（AI模式设计「模型网关」）。
func (c *Client) StreamChat(ctx context.Context, req *ChatRequest, onDelta func(Delta)) (string, error) {
	return c.adp.StreamChat(ctx, c.http, c.BaseURL, c.APIKey, c.Model, req, onDelta)
}
