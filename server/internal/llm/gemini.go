package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// geminiAdapter 适配 Gemini generateContent（POST /v1beta/models/{model}:generateContent）。
// 差异：model 在 URL 路径、systemInstruction 顶层、role 是 user/model（非 assistant）、
// content 是 parts 数组、tool 调用以 functionCall/functionResponse + name 关联（无 call-id）。
type geminiAdapter struct{}

// buildGeminiBody 组装请求体。Chat 与 StreamChat 共用一处装配，避免两条路径各拼一份
// 而漂移（新增参数只改一边 = 「配了不生效」）。
func buildGeminiBody(req *ChatRequest) map[string]any {
	system, contents := buildGeminiContents(req.Messages)
	tools := buildGeminiTools(req.Tools)

	body := map[string]any{"contents": contents}
	gen := map[string]any{}
	if req.MaxTokens != nil {
		gen["maxOutputTokens"] = *req.MaxTokens
	}
	// 思考档位同样落 generationConfig（gemini 无顶层平铺参数）
	applyGeminiThinking(gen, req.Thinking)
	if len(gen) > 0 {
		body["generationConfig"] = gen
	}
	if system != "" {
		body["systemInstruction"] = map[string]any{"parts": []any{map[string]any{"text": system}}}
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}
	return body
}

func (geminiAdapter) Chat(ctx context.Context, baseURL, apiKey, model string, req *ChatRequest) (*ChatResponse, error) {
	body := buildGeminiBody(req)

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	url := baseURL + "/v1beta/models/" + model + ":generateContent"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", apiKey)

	res, err := openaiHTTP.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	respBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	return parseGeminiBody(respBody, res.StatusCode)
}

// geminiRaw Gemini 单帧/整块响应体（Chat 与「非事件流兜底」共用同一解析）。
type geminiRaw struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
				// thought:true 的 part 是思考链（不是正文），必须与正文分开，
				// 否则思考会混进回答里展示给用户
				Thought bool `json:"thought"`
				// thoughtSignature 是 **part 级**字段（与 functionCall 平级），
				// 不是 functionCall 的子字段——Google 文档原文：「Responses can
				// include a thought_signature field within any content part」。
				// gemini 3 在工具往返时强制要求原样带回，缺失即 400。
				ThoughtSignature string `json:"thoughtSignature"`
				FunctionCall     struct {
					Name string          `json:"name"`
					Args json.RawMessage `json:"args"`
				} `json:"functionCall"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	// Gemini usageMetadata：promptTokenCount/candidatesTokenCount/totalTokenCount（归一化）
	UsageMetadata *struct {
		PromptTokenCount     int64 `json:"promptTokenCount"`
		CandidatesTokenCount int64 `json:"candidatesTokenCount"`
		TotalTokenCount      int64 `json:"totalTokenCount"`
	} `json:"usageMetadata"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func parseGeminiBody(data []byte, status int) (*ChatResponse, error) {
	var raw geminiRaw
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode model response: %w", err)
	}
	if raw.Error != nil {
		return nil, fmt.Errorf("模型服务错误: %s", raw.Error.Message)
	}
	if len(raw.Candidates) == 0 {
		return nil, fmt.Errorf("模型返回空响应（HTTP %d）", status)
	}

	var choice Choice
	c := raw.Candidates[0]
	choice.FinishReason = c.FinishReason
	for idx, part := range c.Content.Parts {
		if part.Text != "" && part.Thought {
			// 思考链：拼接但不与正文混流（流式路径同口径）
			choice.Message.Reasoning += part.Text
			continue
		}
		if part.Text != "" {
			if choice.Message.Content != "" {
				choice.Message.Content += "\n"
			}
			choice.Message.Content += part.Text
		}
		if part.FunctionCall.Name != "" {
			tc := ToolCall{
				// Gemini 无 call-id，合成一个用于规范化往返关联
				ID:   fmt.Sprintf("gemini-%s-%d", part.FunctionCall.Name, idx),
				Type: "function",
				// 签名与 functionCall 平级，回传时必须原样带回（gemini 3 强制）
				Signature: part.ThoughtSignature,
			}
			tc.Function.Name = part.FunctionCall.Name
			if len(part.FunctionCall.Args) > 0 {
				tc.Function.Arguments = string(part.FunctionCall.Args)
			} else {
				tc.Function.Arguments = "{}"
			}
			choice.Message.ToolCalls = append(choice.Message.ToolCalls, tc)
		}
	}

	out := &ChatResponse{}
	out.Choices = append(out.Choices, choice)
	// 归一化 usage（candidatesTokenCount = 输出 token）
	if raw.UsageMetadata != nil {
		out.Usage = Usage{
			PromptTokens:     raw.UsageMetadata.PromptTokenCount,
			CompletionTokens: raw.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      raw.UsageMetadata.TotalTokenCount,
		}
		if out.Usage.TotalTokens == 0 {
			out.Usage.TotalTokens = out.Usage.PromptTokens + out.Usage.CompletionTokens
		}
	}
	return out, nil
}

// buildGeminiContents 把规范化 messages 翻译为 Gemini 的 contents 数组。
// role 映射：assistant → model；user/tool → user。
// tool 结果以 functionResponse part 承载（response 必须是 object）。
// 历史中 assistant 的 tool_calls 还原为 functionCall part。
func buildGeminiContents(msgs []Message) (system string, contents []any) {
	for _, m := range msgs {
		switch m.Role {
		case "system":
			if system == "" {
				system = m.Content
			} else {
				system += "\n" + m.Content
			}
		case "assistant":
			parts := []any{}
			if m.Content != "" {
				parts = append(parts, map[string]any{"text": m.Content})
			}
			for _, tc := range m.ToolCalls {
				var args any
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
					args = map[string]any{"raw": tc.Function.Arguments}
				}
				part := map[string]any{
					"functionCall": map[string]any{
						"name": tc.Function.Name,
						"args": args,
					},
				}
				// thoughtSignature 是 part 级字段，必须与 functionCall 平级回传；
				// gemini 3 缺失即 400。历史来自别的模型时本就没有签名，此时**不伪造**
				// （编造的签名会被上游拒绝或导致推理状态错乱），留空让上游报错可见。
				if tc.Signature != "" {
					part["thoughtSignature"] = tc.Signature
				}
				parts = append(parts, part)
			}
			contents = append(contents, map[string]any{"role": "model", "parts": parts})
		case "tool":
			// 工具结果：response 必须是 object；尝试把 Content 当 JSON 解析，失败则包一层
			var responseObj any
			if err := json.Unmarshal([]byte(m.Content), &responseObj); err != nil {
				responseObj = map[string]any{"result": m.Content}
			}
			// 工具名优先取 ToolName（历史回放的往返 ID 形如 card-<n>，反解不出名字）；
			// 缺省回落 call-id 反解（本协议在途往返的合成 ID）
			name := m.ToolName
			if name == "" {
				name = toolNameFromCallID(m.ToolCallID)
			}
			contents = append(contents, map[string]any{
				"role": "user",
				"parts": []any{map[string]any{
					"functionResponse": map[string]any{
						"name":     name,
						"response": responseObj,
					},
				}},
			})
		default: // user
			contents = append(contents, map[string]any{
				"role":  "user",
				"parts": []any{map[string]any{"text": m.Content}},
			})
		}
	}
	return system, contents
}

// toolNameFromCallID 从合成的 call-id（形如 gemini-{name}-{idx}）还原工具名；
// 非 gemini 前缀则原样返回（兼容历史）。
func toolNameFromCallID(id string) string {
	const prefix = "gemini-"
	if len(id) > len(prefix) && id[:len(prefix)] == prefix {
		rest := id[len(prefix):]
		// 剥尾部 -{idx}
		for i := len(rest) - 1; i > 0; i-- {
			if rest[i] == '-' {
				return rest[:i]
			}
		}
		return rest
	}
	return id
}

// buildGeminiTools 把规范化 Tool 翻译为 Gemini 的 functionDeclarations。
func buildGeminiTools(tools []Tool) []any {
	decls := make([]any, 0, len(tools))
	for _, t := range tools {
		entry := map[string]any{
			"name":        t.Function.Name,
			"description": t.Function.Description,
		}
		if t.Function.Parameters != nil {
			entry["parameters"] = t.Function.Parameters
		}
		decls = append(decls, entry)
	}
	if len(decls) == 0 {
		return nil
	}
	return []any{map[string]any{"functionDeclarations": decls}}
}

// StreamChat 原生流式（POST /v1beta/models/{model}:streamGenerateContent?alt=sse）。
//
// 每帧形如 {"candidates":[{"content":{"parts":[…]},"finishReason":…}],"usageMetadata":…}。
// parts 里：thought:true 的 text 是思考链，普通 text 是正文，functionCall 是工具调用。
//
// **Gemini API 的函数参数不分片**：官方 SDK 明确标注 partialArgs / willContinue 与
// streamFunctionCallArguments 均「not supported in Gemini API」（那是 Vertex AI /
// Agent Platform 的能力）；且 args 在线上是 JSON **object**，无法表达半截 JSON。
// 故这里不做分片累积：args 出现在哪一帧就取哪一帧的完整对象，流结束后统一下发。
//
// 思考链与正文可能同帧出现（Gemini 交替下发），故逐 part 判断而不是「一帧一类」。
func (geminiAdapter) StreamChat(ctx context.Context, baseURL, apiKey, model string, req *ChatRequest, onDelta func(Delta)) (string, error) {
	body := buildGeminiBody(req)

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	url := baseURL + "/v1beta/models/" + model + ":streamGenerateContent?alt=sse"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", apiKey)

	res, err := openaiHTTP.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if err := streamStatusError(res); err != nil {
		return "", err
	}
	// 非事件流兜底（中转网关忽略 stream:true 回整块 JSON）：按同一响应解析回放。
	// Gemini 的整块响应体就是「只有一帧」的同一结构，故复用同一解析函数。
	if !isEventStream(res) {
		return streamFallbackReplay(res, parseGeminiBody, onDelta)
	}

	// 逐帧按 geminiRaw 解析（与 Chat 同一结构），工具调用按出现顺序收集
	type gemCall struct {
		name      string
		signature string
		args      json.RawMessage
	}
	var calls []*gemCall
	var usage *Usage
	finish := ""
	sawFrame := false

	sc := newSSEScanner(res.Body)
	for {
		data, ok := sc.next()
		if !ok {
			break
		}
		var f geminiRaw
		if !sseJSON(data, &f) {
			continue
		}
		sawFrame = true
		if f.Error != nil {
			return "", fmt.Errorf("模型服务错误: %s", f.Error.Message)
		}
		if len(f.Candidates) > 0 {
			c := f.Candidates[0]
			if c.FinishReason != "" {
				finish = c.FinishReason
			}
			for _, part := range c.Content.Parts {
				switch {
				case part.FunctionCall.Name != "":
					calls = append(calls, &gemCall{name: part.FunctionCall.Name,
						signature: part.ThoughtSignature, args: part.FunctionCall.Args})
				case part.Text != "" && part.Thought:
					onDelta(Delta{Reasoning: part.Text})
				case part.Text != "":
					onDelta(Delta{Content: part.Text})
				}
			}
		}
		if f.UsageMetadata != nil {
			u := Usage{
				PromptTokens:     f.UsageMetadata.PromptTokenCount,
				CompletionTokens: f.UsageMetadata.CandidatesTokenCount,
				TotalTokens:      f.UsageMetadata.TotalTokenCount,
			}
			if u.TotalTokens == 0 {
				u.TotalTokens = u.PromptTokens + u.CompletionTokens
			}
			usage = &u
		}
	}
	if err := sc.err(); err != nil {
		return "", err
	}
	if !sawFrame {
		return "", fmt.Errorf("模型返回空响应（HTTP %d）", res.StatusCode)
	}
	for i, c := range calls {
		args := "{}"
		if len(c.args) > 0 {
			args = string(c.args)
		}
		// Gemini 无 call-id，合成一个用于规范化往返关联（与 Chat 同一口径）
		onDelta(Delta{ToolCall: &ToolCallDelta{
			ID:        fmt.Sprintf("gemini-%s-%d", c.name, i),
			Name:      c.name,
			Arguments: args,
			Signature: c.signature,
		}})
	}
	if usage != nil {
		onDelta(Delta{Usage: usage})
	}
	return finish, nil
}
