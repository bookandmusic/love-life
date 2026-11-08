package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sseServer 起一个按帧序列回 SSE 的假上游；frames 为完整帧体（不含 "data: " 前缀）。
func sseServer(t *testing.T, frames []string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		for _, f := range frames {
			if strings.HasPrefix(f, "event:") || f == "" {
				fmt.Fprintf(w, "%s\n", f)
			} else {
				fmt.Fprintf(w, "data: %s\n\n", f)
			}
			if fl != nil {
				fl.Flush()
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// collectDeltas 跑一次 StreamChat 并归集回调结果。
func collectDeltas(t *testing.T, adp adapter, url string, req *ChatRequest) (string, []Delta) {
	t.Helper()
	var got []Delta
	finish, err := adp.StreamChat(context.Background(), httpClientFor(0), url, "k", "m", req, func(d Delta) {
		got = append(got, d)
	})
	if err != nil {
		t.Fatalf("StreamChat 失败: %v", err)
	}
	return finish, got
}

func reasoningOf(deltas []Delta) string {
	var b strings.Builder
	for _, d := range deltas {
		b.WriteString(d.Reasoning)
	}
	return b.String()
}

func contentOf(deltas []Delta) string {
	var b strings.Builder
	for _, d := range deltas {
		b.WriteString(d.Content)
	}
	return b.String()
}

func toolCallsOf(deltas []Delta) []*ToolCallDelta {
	var out []*ToolCallDelta
	for _, d := range deltas {
		if d.ToolCall != nil {
			out = append(out, d.ToolCall)
		}
	}
	return out
}

func usageOf(deltas []Delta) *Usage {
	for _, d := range deltas {
		if d.Usage != nil {
			return d.Usage
		}
	}
	return nil
}

// orderOf 抽出增量序列里思考/正文的到达顺序（如 ["reasoning","content","reasoning"]）。
// 只按 Reasoning/Content 归类，忽略 Usage（它在流末单独下发，不参与交错）。
func orderOf(deltas []Delta) []string {
	var out []string
	for _, d := range deltas {
		switch {
		case d.Reasoning != "" && d.Content == "":
			out = append(out, "reasoning")
		case d.Content != "" && d.Reasoning == "":
			out = append(out, "content")
		}
	}
	return out
}

// TestStreamReasoningAndContentKeepArrivalOrder 钉住「正文与思考按到达顺序回调」。
//
// 为什么单列一条：各协议的思考/正文是**交错**到达的（现代模型边想边说，思考可能分多段，
// 正文插在中间）。若某个 adapter 把思考先攒起来、等流末再一次性下发，正文与思考的相对
// 位置就会错乱——用户看到的是「先答完再想」。reasoningOf/contentOf 各按字段拼接会把这个
// 顺序抹平，故必须按到达次序断言。
func TestStreamReasoningAndContentKeepArrivalOrder(t *testing.T) {
	cases := []struct {
		name   string
		adp    adapter
		frames []string
	}{
		{
			name: "anthropic",
			adp:  anthropicAdapter{},
			frames: []string{
				`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"想一"}}`,
				`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"答一"}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"想二"}}`,
				`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"答二"}}`,
			},
		},
		{
			name: "openai-responses",
			adp:  openaiResponsesAdapter{},
			frames: []string{
				`{"type":"response.reasoning_summary_text.delta","delta":"想一"}`,
				`{"type":"response.output_text.delta","item_id":"msg_1","delta":"答一"}`,
				`{"type":"response.reasoning_summary_text.delta","delta":"想二"}`,
				`{"type":"response.output_text.delta","item_id":"msg_1","delta":"答二"}`,
			},
		},
		{
			name: "gemini",
			adp:  geminiAdapter{},
			frames: []string{
				`{"candidates":[{"content":{"parts":[{"text":"想一","thought":true}]}}]}`,
				`{"candidates":[{"content":{"parts":[{"text":"答一"}]}}]}`,
				`{"candidates":[{"content":{"parts":[{"text":"想二","thought":true}]}}]}`,
				`{"candidates":[{"content":{"parts":[{"text":"答二"}]}}]}`,
			},
		},
	}
	want := []string{"reasoning", "content", "reasoning", "content"}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := sseServer(t, c.frames)
			_, deltas := collectDeltas(t, c.adp, srv.URL, &ChatRequest{})

			got := orderOf(deltas)
			if len(got) != len(want) {
				t.Fatalf("增量序列 = %v, want %v", got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("增量序列 = %v, want %v（思考与正文必须按到达顺序回调）", got, want)
				}
			}
		})
	}
}

// TestAnthropicStreamNative 覆盖 anthropic 原生流式的完整事件时序：
// message_start（input usage）→ thinking 块（含 signature_delta）→ text 块 →
// tool_use 块（参数跨帧 input_json_delta）→ message_delta → message_stop。
//
// 两条不变式：
//  1. 工具参数**到 content_block_stop 才下发**，且跨帧拼全（分片不能外泄到 Agent 层）
//  2. 思考与正文各归其位，signature_delta 按规范忽略（不报错、不当作正文）
func TestAnthropicStreamNative(t *testing.T) {
	frames := []string{
		`{"type":"message_start","message":{"usage":{"input_tokens":11,"output_tokens":0}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"先想"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig-x"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"text"}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"好"}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"的"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_1","name":"find_posts"}}`,
		`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"query\":"}}`,
		`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"\"海边\"}"}}`,
		`{"type":"content_block_stop","index":2}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":22}}`,
		`{"type":"message_stop"}`,
	}
	srv := sseServer(t, frames)
	finish, deltas := collectDeltas(t, anthropicAdapter{}, srv.URL, &ChatRequest{})

	if finish != "tool_use" {
		t.Fatalf("finish = %q, want tool_use", finish)
	}
	if got := reasoningOf(deltas); got != "先想" {
		t.Fatalf("思考 = %q, want 先想", got)
	}
	if got := contentOf(deltas); got != "好的" {
		t.Fatalf("正文 = %q, want 好的", got)
	}
	calls := toolCallsOf(deltas)
	if len(calls) != 1 {
		t.Fatalf("工具调用数 = %d, want 1（跨帧分片必须拼完再发）: %+v", len(calls), calls)
	}
	if calls[0].ID != "toolu_1" || calls[0].Name != "find_posts" || calls[0].Arguments != `{"query":"海边"}` {
		t.Fatalf("调用形状不对: %+v", calls[0])
	}
	u := usageOf(deltas)
	if u == nil || u.PromptTokens != 11 || u.CompletionTokens != 22 || u.TotalTokens != 33 {
		t.Fatalf("usage 不对: %+v", u)
	}
}

// TestAnthropicStreamParallelToolBlocks 并行多个 tool_use 块会**交错**到达，
// 必须按 content_block index 分桶累积，不能用「当前块」这种单一状态。
func TestAnthropicStreamParallelToolBlocks(t *testing.T) {
	frames := []string{
		`{"type":"message_start","message":{"usage":{"input_tokens":1,"output_tokens":0}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"t0","name":"find_posts"}}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"t1","name":"find_albums"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"q\":"}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"n\":"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"1}"}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"2}"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":3}}`,
	}
	srv := sseServer(t, frames)
	_, deltas := collectDeltas(t, anthropicAdapter{}, srv.URL, &ChatRequest{})

	calls := toolCallsOf(deltas)
	if len(calls) != 2 {
		t.Fatalf("调用数 = %d, want 2: %+v", len(calls), calls)
	}
	if calls[0].ID != "t0" || calls[0].Arguments != `{"q":1}` {
		t.Fatalf("第 1 个调用串了桶: %+v", calls[0])
	}
	if calls[1].ID != "t1" || calls[1].Arguments != `{"n":2}` {
		t.Fatalf("第 2 个调用串了桶: %+v", calls[1])
	}
}

// TestResponsesStreamNative 覆盖 Responses 原生流式：正文增量、思考摘要增量、
// 工具参数按 item_id 分片（done 事件的完整串为权威）、usage 只在 response.completed。
func TestResponsesStreamNative(t *testing.T) {
	frames := []string{
		`{"type":"response.reasoning_summary_text.delta","sequence_number":1,"delta":"想想"}`,
		`{"type":"response.output_text.delta","sequence_number":2,"item_id":"msg_1","delta":"你"}`,
		`{"type":"response.output_text.delta","sequence_number":3,"item_id":"msg_1","delta":"好"}`,
		`{"type":"response.output_item.added","sequence_number":4,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"find_posts","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","sequence_number":5,"item_id":"fc_1","delta":"{\"query\":"}`,
		`{"type":"response.function_call_arguments.delta","sequence_number":6,"item_id":"fc_1","delta":"\"海边\"}"}`,
		`{"type":"response.function_call_arguments.done","sequence_number":7,"item_id":"fc_1","arguments":"{\"query\":\"海边\"}"}`,
		`{"type":"response.output_item.done","sequence_number":8,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"find_posts","arguments":"{\"query\":\"海边\"}"}}`,
		`{"type":"response.completed","sequence_number":9,"response":{"usage":{"input_tokens":7,"output_tokens":9,"total_tokens":16}}}`,
	}
	srv := sseServer(t, frames)
	_, deltas := collectDeltas(t, openaiResponsesAdapter{}, srv.URL, &ChatRequest{})

	if got := reasoningOf(deltas); got != "想想" {
		t.Fatalf("思考 = %q", got)
	}
	if got := contentOf(deltas); got != "你好" {
		t.Fatalf("正文 = %q", got)
	}
	calls := toolCallsOf(deltas)
	if len(calls) != 1 {
		t.Fatalf("调用数 = %d, want 1: %+v", len(calls), calls)
	}
	if calls[0].ID != "call_1" || calls[0].Name != "find_posts" || calls[0].Arguments != `{"query":"海边"}` {
		t.Fatalf("调用形状不对: %+v", calls[0])
	}
	u := usageOf(deltas)
	if u == nil || u.TotalTokens != 16 {
		t.Fatalf("usage 不对: %+v", u)
	}
}

// TestResponsesStreamArgumentsDoneUsesArgumentsField 钉住 function_call_arguments.done 的字段名：
// 该事件的权威串在 `arguments`（**不是** `delta`——它没有 delta 字段）。若误读 `delta`，
// 拿到的是空串，`completed` 永不生效，只能退回逐帧累积；一旦某网关只发该事件不发
// output_item.done，参数就会是空的。
func TestResponsesStreamArgumentsDoneUsesArgumentsField(t *testing.T) {
	frames := []string{
		`{"type":"response.output_item.added","item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"find_posts","arguments":""}}`,
		`{"type":"response.function_call_arguments.done","item_id":"fc_1","arguments":"{\"query\":\"海边\"}"}`,
	}
	srv := sseServer(t, frames)
	_, deltas := collectDeltas(t, openaiResponsesAdapter{}, srv.URL, &ChatRequest{})

	calls := toolCallsOf(deltas)
	if len(calls) != 1 {
		t.Fatalf("调用数 = %d, want 1: %+v", len(calls), calls)
	}
	if calls[0].Arguments != `{"query":"海边"}` {
		t.Fatalf("参数 = %q, want %q（arguments.done 的字段名是 arguments，不是 delta）",
			calls[0].Arguments, `{"query":"海边"}`)
	}
}

// TestResponsesStreamReasoningTextAlias 中转网关的思考增量事件名不统一：官方文档是
// response.reasoning_summary_text.delta（思考摘要），部分网关发 response.reasoning_text.delta
// （原始思考）。两个都认，否则走这类网关时思考会被静默丢弃——即「配了不生效」。
func TestResponsesStreamReasoningTextAlias(t *testing.T) {
	frames := []string{
		`{"type":"response.reasoning_text.delta","sequence_number":1,"delta":"原始"}`,
		`{"type":"response.reasoning_text.delta","sequence_number":2,"delta":"思考"}`,
		`{"type":"response.output_text.delta","sequence_number":3,"item_id":"msg_1","delta":"答"}`,
	}
	srv := sseServer(t, frames)
	_, deltas := collectDeltas(t, openaiResponsesAdapter{}, srv.URL, &ChatRequest{})

	if got := reasoningOf(deltas); got != "原始思考" {
		t.Fatalf("思考 = %q, want %q", got, "原始思考")
	}
	if got := contentOf(deltas); got != "答" {
		t.Fatalf("正文 = %q", got)
	}
}

// TestResponsesStreamArgumentsAccumulatedWithoutDone 有些网关只发 arguments.delta、
// 不发 arguments.done / output_item.done（或 done 里 arguments 为空）。此时**必须**靠
// 逐帧累积拼出参数——若只认 done 的权威串，这条路径会得到空参数。
func TestResponsesStreamArgumentsAccumulatedWithoutDone(t *testing.T) {
	frames := []string{
		`{"type":"response.output_item.added","item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"find_posts","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"{\"query\":"}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"\"海边\"}"}`,
	}
	srv := sseServer(t, frames)
	_, deltas := collectDeltas(t, openaiResponsesAdapter{}, srv.URL, &ChatRequest{})

	calls := toolCallsOf(deltas)
	if len(calls) != 1 {
		t.Fatalf("调用数 = %d, want 1: %+v", len(calls), calls)
	}
	if calls[0].Arguments != `{"query":"海边"}` {
		t.Fatalf("未靠逐帧累积拼全参数: %q", calls[0].Arguments)
	}
	if calls[0].ID != "call_1" || calls[0].Name != "find_posts" {
		t.Fatalf("调用形状不对: %+v", calls[0])
	}
}

// TestResponsesStreamFailureEventResponses 错误既可能出现在顶层 error 事件，
// 也可能包在 response.failed 的 response.error 里——两种都要变成 error 返回。
func TestResponsesStreamFailureEventResponses(t *testing.T) {
	for _, frame := range []string{
		`{"type":"error","error":{"message":"boom"}}`,
		`{"type":"response.failed","response":{"error":{"message":"boom"}}}`,
	} {
		srv := sseServer(t, []string{frame})
		_, err := (openaiResponsesAdapter{}).StreamChat(context.Background(), httpClientFor(0), srv.URL, "k", "m", &ChatRequest{}, func(Delta) {})
		if err == nil || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("frame=%s err=%v", frame, err)
		}
	}
}

// TestGeminiStreamNative 覆盖 gemini 原生流式：thought:true 的 part 是思考、
// 普通 text 是正文、functionCall 带 **part 级** thoughtSignature、末帧 usageMetadata。
func TestGeminiStreamNative(t *testing.T) {
	frames := []string{
		`{"candidates":[{"content":{"parts":[{"text":"想","thought":true}]},"finishReason":"STOP"}]}`,
		`{"candidates":[{"content":{"parts":[{"text":"你"}]},"finishReason":"STOP"}]}`,
		`{"candidates":[{"content":{"parts":[{"text":"好"}]},"finishReason":"STOP"}]}`,
		`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"find_posts","args":{"query":"海边"}},"thoughtSignature":"sig-g"}]},"finishReason":"STOP"}]}`,
		`{"candidates":[{"content":{"parts":[]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":4,"totalTokenCount":7}}`,
	}
	srv := sseServer(t, frames)
	_, deltas := collectDeltas(t, geminiAdapter{}, srv.URL, &ChatRequest{})

	if got := reasoningOf(deltas); got != "想" {
		t.Fatalf("思考 = %q", got)
	}
	if got := contentOf(deltas); got != "你好" {
		t.Fatalf("正文 = %q", got)
	}
	calls := toolCallsOf(deltas)
	if len(calls) != 1 {
		t.Fatalf("调用数 = %d, want 1: %+v", len(calls), calls)
	}
	if calls[0].Name != "find_posts" || calls[0].Arguments != `{"query":"海边"}` {
		t.Fatalf("调用形状不对: %+v", calls[0])
	}
	// 签名是 part 级字段，必须被收下（否则第二轮 400）
	if calls[0].Signature != "sig-g" {
		t.Fatalf("thoughtSignature 丢失: %+v", calls[0])
	}
	u := usageOf(deltas)
	if u == nil || u.TotalTokens != 7 {
		t.Fatalf("usage 不对: %+v", u)
	}
}

// TestGeminiStreamSignatureRoundTrip 签名必须**原样回传**到下一轮请求体里
// （part 级、与 functionCall 平级）。缺失即 gemini 3 的 400。
func TestGeminiStreamSignatureRoundTrip(t *testing.T) {
	srv := sseServer(t, []string{
		`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"find_posts","args":{"query":"海边"}},"thoughtSignature":"sig-round"}]},"finishReason":"STOP"}]}`,
	})
	_, deltas := collectDeltas(t, geminiAdapter{}, srv.URL, &ChatRequest{})
	calls := toolCallsOf(deltas)
	if len(calls) != 1 || calls[0].Signature != "sig-round" {
		t.Fatalf("未取到签名: %+v", calls)
	}

	// 把签名放进历史（模拟落库后回放），检查请求体里的 part 形状
	tc := ToolCall{ID: calls[0].ID, Type: "function", Signature: "sig-round"}
	tc.Function.Name = calls[0].Name
	tc.Function.Arguments = calls[0].Arguments
	_, contents := buildGeminiContents([]Message{{Role: "assistant", ToolCalls: []ToolCall{tc}}})
	if len(contents) != 1 {
		t.Fatalf("contents = %+v", contents)
	}
	parts := contents[0].(map[string]any)["parts"].([]any)
	if len(parts) != 1 {
		t.Fatalf("parts = %+v", parts)
	}
	part := parts[0].(map[string]any)
	if part["thoughtSignature"] != "sig-round" {
		t.Fatalf("签名未回传（必须是 part 级平级字段）: %+v", part)
	}
	fc, ok := part["functionCall"].(map[string]any)
	if !ok {
		t.Fatalf("functionCall 形状不对: %+v", part)
	}
	if _, nested := fc["thoughtSignature"]; nested {
		t.Fatalf("签名不该嵌在 functionCall 内: %+v", fc)
	}
}

// TestGeminiStreamMissingSignatureNotFabricated 历史里没有签名时**不伪造**：
// 编造的签名会被上游拒绝或污染推理状态，留空让错误可见。
func TestGeminiStreamMissingSignatureNotFabricated(t *testing.T) {
	tc := newToolCall("gemini-find_posts-0", "find_posts", `{"query":"海边"}`)
	_, contents := buildGeminiContents([]Message{{Role: "assistant", ToolCalls: []ToolCall{tc}}})
	parts := contents[0].(map[string]any)["parts"].([]any)
	part := parts[0].(map[string]any)
	if _, present := part["thoughtSignature"]; present {
		t.Fatalf("缺签名时不得凭空补一个: %+v", part)
	}
}

// TestNonStreamingThinkingParsed 三协议**非流式** Chat 也要解析思考，
// 否则非流式路径把思考混进正文（或整段丢掉）。
func TestNonStreamingThinkingParsed(t *testing.T) {
	cases := []struct {
		name    string
		adp     adapter
		body    string
		wantThk string
		wantTxt string
	}{
		{
			name: "anthropic",
			adp:  anthropicAdapter{},
			body: `{"content":[{"type":"thinking","thinking":"想过了"},{"type":"text","text":"答案"}],"stop_reason":"end_turn"}`,
			// anthropic 的 text 块用 \n 连接；只有一块故无分隔
			wantThk: "想过了", wantTxt: "答案",
		},
		{
			name: "responses",
			adp:  openaiResponsesAdapter{},
			body: `{"output":[{"type":"reasoning","summary":[{"type":"summary_text","text":"想过了"}]},{"type":"message","content":[{"type":"output_text","text":"答案"}]}]}`,
			// responses 的 output_text 也按 \n 连接；只有一块故无分隔
			wantThk: "想过了", wantTxt: "答案",
		},
		{
			name: "gemini",
			adp:  geminiAdapter{},
			body: `{"candidates":[{"content":{"parts":[{"text":"想过了","thought":true},{"text":"答案"}]},"finishReason":"STOP"}]}`,
			// gemini 的正文块间插 \n；思考不参与，故无多余换行
			wantThk: "想过了", wantTxt: "答案",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()
			out, err := c.adp.Chat(context.Background(), httpClientFor(0), srv.URL, "k", "m", &ChatRequest{})
			if err != nil {
				t.Fatal(err)
			}
			if len(out.Choices) == 0 {
				t.Fatal("无候选")
			}
			m := out.Choices[0].Message
			if m.Reasoning != c.wantThk {
				t.Fatalf("思考 = %q, want %q", m.Reasoning, c.wantThk)
			}
			if m.Content != c.wantTxt {
				t.Fatalf("正文 = %q, want %q", m.Content, c.wantTxt)
			}
		})
	}
}

// TestOpenAIChatStripsGeminiSignature openai-chat 直接把 Message 序列化进请求体，
// 若不剥掉 gemini 的签名就会把未知字段发给 OpenAI / DeepSeek 等上游。
func TestOpenAIChatStripsGeminiSignature(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(b)
		got = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	tc := ToolCall{ID: "c1", Type: "function", Signature: "sig-gemini"}
	tc.Function.Name = "find_posts"
	tc.Function.Arguments = `{}`
	_, err := (openaiChatAdapter{}).Chat(context.Background(), httpClientFor(0), srv.URL, "k", "m", &ChatRequest{
		Messages: []Message{{Role: "assistant", ToolCalls: []ToolCall{tc}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "sig-gemini") || strings.Contains(got, "signature") {
		t.Fatalf("签名泄漏到 openai-chat 请求体: %s", got)
	}
}

// TestReplayKeepsSignature 非事件流兜底（网关回整块 JSON）也要保住 gemini 签名：
// 兜底路径丢掉凭据 = 下一轮 400，与原生流式路径同等致命。
func TestReplayKeepsSignature(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"find_posts","args":{"query":"海边"}},"thoughtSignature":"sig-fallback"}]},"finishReason":"STOP"}]}`))
	}))
	defer srv.Close()
	_, deltas := collectDeltas(t, geminiAdapter{}, srv.URL, &ChatRequest{})
	calls := toolCallsOf(deltas)
	if len(calls) != 1 || calls[0].Signature != "sig-fallback" {
		t.Fatalf("兜底路径丢了签名: %+v", calls)
	}
}

// TestSSEScannerMultilineAndComments 共享读取器要按 SSE 规范处理多行 data、
// 注释行与心跳——四协议共用，错一处四个都错。
func TestSSEScannerMultilineAndComments(t *testing.T) {
	raw := ": heartbeat\n" +
		"event: ping\ndata: {\"a\":1}\n\n" +
		"data: {\"b\":\n" +
		"data: 2}\n\n" +
		"data: [DONE]\n\n"
	sc := newSSEScanner(strings.NewReader(raw))
	var got []string
	for {
		d, ok := sc.next()
		if !ok {
			break
		}
		got = append(got, d)
	}
	if len(got) != 3 {
		t.Fatalf("事件数 = %d, want 3: %+v", len(got), got)
	}
	if got[0] != `{"a":1}` {
		t.Fatalf("第 1 帧 = %q", got[0])
	}
	if got[1] != "{\"b\":\n2}" {
		t.Fatalf("多行 data 未按规范拼接: %q", got[1])
	}
	if got[2] != "[DONE]" {
		t.Fatalf("第 3 帧 = %q", got[2])
	}
}

// TestStreamStatusErrorAllProtocols 四个协议共用一份非 200 响应解析（streamStatusError）：
// 上游报错体形状一致（{"error":{"message":…}}），必须把 message 透出来——只报 HTTP 码
// 会让「密钥错/额度尽/模型名错」这类可自愈问题在客户端无法区分。
func TestStreamStatusErrorAllProtocols(t *testing.T) {
	cases := []struct {
		name string
		adp  adapter
	}{
		{"anthropic", anthropicAdapter{}},
		{"gemini", geminiAdapter{}},
		{"openai-chat", openaiChatAdapter{}},
		{"openai-responses", openaiResponsesAdapter{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
			}))
			defer srv.Close()

			_, err := c.adp.StreamChat(context.Background(), httpClientFor(0), srv.URL, "k", "m", &ChatRequest{},
				func(Delta) {})
			if err == nil || !strings.Contains(err.Error(), "模型服务错误") ||
				!strings.Contains(err.Error(), "invalid api key") {
				t.Fatalf("err = %v, want 模型服务错误 + invalid api key", err)
			}
		})
	}
}

// TestStreamChatSetsStreamFlag 四个协议的 StreamChat 都必须在请求体里带 stream:true。
// 少了它，中转网关/上游会按一次性响应回整块 JSON（还能靠非事件流兜底「看起来正常」），
// 但原生流式就静默退化成整块下发——首字延迟与逐字展开全失效，且不留任何报错。
func TestStreamChatSetsStreamFlag(t *testing.T) {
	cases := []struct {
		name string
		adp  adapter
		body string
		// urlMarker 非空表示该协议用 URL 表达流式（gemini 是 :streamGenerateContent?alt=sse），
		// 空则要求在请求体里带 stream:true。
		urlMarker string
	}{
		{"anthropic", anthropicAdapter{}, `{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`, ""},
		{"openai-chat", openaiChatAdapter{}, `{"choices":[{"message":{"content":"ok"}}]}`, ""},
		{"responses", openaiResponsesAdapter{}, `{"output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`, ""},
		{"gemini", geminiAdapter{}, `{"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`, ":streamGenerateContent"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv, reqP, bodyP := fakeChatUpstream(t, c.body)
			if _, err := c.adp.StreamChat(context.Background(), httpClientFor(0), srv.URL, "k", "m", &ChatRequest{
				Messages: []Message{{Role: "user", Content: "hi"}},
			}, func(Delta) {}); err != nil {
				t.Fatalf("StreamChat: %v", err)
			}
			if c.urlMarker != "" {
				if u := (*reqP).URL.String(); !strings.Contains(u, c.urlMarker) {
					t.Errorf("请求 URL = %q, want 含 %q（原生流式开关）", u, c.urlMarker)
				}
				return
			}
			if sent := sentBody(t, bodyP); sent["stream"] != true {
				t.Errorf("请求体 stream = %v, want true（原生流式开关）: %v", sent["stream"], sent)
			}
		})
	}
}

// TestStreamNonEventStreamFallback 中转网关忽略 stream:true 回整块 JSON 时，
// 三个新协议也必须回放成 Delta（否则整轮静默变空回答）。
func TestStreamNonEventStreamFallback(t *testing.T) {
	cases := []struct {
		name string
		adp  adapter
		body string
	}{
		{"anthropic", anthropicAdapter{}, `{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`},
		{"responses", openaiResponsesAdapter{}, `{"output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`},
		{"gemini", geminiAdapter{}, `{"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()
			_, deltas := collectDeltas(t, c.adp, srv.URL, &ChatRequest{})
			if got := contentOf(deltas); got != "ok" {
				t.Fatalf("正文 = %q, want ok（整块响应必须回放）", got)
			}
		})
	}
}

// TestOpenAIChatStreamReasoningAlias 思考增量的字段名有两种：`reasoning_content`（部分
// 供应商）与 `reasoning`（实测网关用它，逐帧真增量）。只认前者会把思考**整段丢弃**——
// 表现为思考区一直空白、首字延迟等于整段生成时间。
//
// 与 TestResponsesStreamReasoningTextAlias 同类：网关字段名不同，两个都要认。
func TestOpenAIChatStreamReasoningAlias(t *testing.T) {
	frames := []string{
		`{"choices":[{"delta":{"reasoning":"用户"}}]}`,
		`{"choices":[{"delta":{"reasoning":"要求"}}]}`,
		`{"choices":[{"delta":{"content":"答"}}]}`,
		`data: [DONE]`,
	}
	srv := sseServer(t, frames)
	_, deltas := collectDeltas(t, openaiChatAdapter{}, srv.URL, &ChatRequest{})

	if got := reasoningOf(deltas); got != "用户要求" {
		t.Fatalf("思考 = %q, want %q（reasoning 字段被丢弃）", got, "用户要求")
	}
	if got := contentOf(deltas); got != "答" {
		t.Fatalf("正文 = %q", got)
	}
}

// TestOpenAIChatStreamReasoningContentStillWorks 另一个字段名不能因为加别名而失效。
func TestOpenAIChatStreamReasoningContentStillWorks(t *testing.T) {
	frames := []string{
		`{"choices":[{"delta":{"reasoning_content":"原"}}]}`,
		`{"choices":[{"delta":{"reasoning_content":"因"}}]}`,
		`{"choices":[{"delta":{"content":"答"}}]}`,
		`data: [DONE]`,
	}
	srv := sseServer(t, frames)
	_, deltas := collectDeltas(t, openaiChatAdapter{}, srv.URL, &ChatRequest{})

	if got := reasoningOf(deltas); got != "原因" {
		t.Fatalf("思考 = %q, want %q（reasoning_content 回归了）", got, "原因")
	}
}
