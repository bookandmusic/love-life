package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestGeminiAdapter_RequestShape(t *testing.T) {
	srv, gotP, bodyP := fakeChatUpstream(t,
		`{"candidates":[{"content":{"parts":[{"text":"hi"}]},"finishReason":"STOP"}]}`)

	adp := geminiAdapter{}
	req := &ChatRequest{
		Messages: []Message{
			{Role: "system", Content: "你是助手"},
			{Role: "user", Content: "ping"},
		},
		Tools: []Tool{{Type: "function", Function: ToolFunction{
			Name: "get_weather", Description: "d", Parameters: map[string]any{"type": "object"},
		}}},
	}
	resp, err := adp.Chat(context.Background(), srv.URL, "g-key", "gemini-1.5-pro", req)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	r := *gotP

	// model 在 URL 路径里
	if r.Method != http.MethodPost {
		t.Errorf("method = %q", r.Method)
	}
	if r.URL.Path != "/v1beta/models/gemini-1.5-pro:generateContent" {
		t.Errorf("path = %q, want model in path", r.URL.Path)
	}
	if r.Header.Get("x-goog-api-key") != "g-key" {
		t.Errorf("x-goog-api-key = %q", r.Header.Get("x-goog-api-key"))
	}

	// 请求体
	var sent map[string]any
	_ = json.Unmarshal(*bodyP, &sent)
	if _, ok := sent["systemInstruction"]; !ok {
		t.Error("systemInstruction 必须在顶层")
	}
	si, _ := sent["systemInstruction"].(map[string]any)
	parts, _ := si["parts"].([]any)
	part0, _ := parts[0].(map[string]any)
	if part0["text"] != "你是助手" {
		t.Errorf("systemInstruction.parts[0].text = %v", part0["text"])
	}
	contents, _ := sent["contents"].([]any)
	if len(contents) != 1 {
		t.Fatalf("contents len = %d, want 1（system 抽走）: %+v", len(contents), contents)
	}
	c0, _ := contents[0].(map[string]any)
	if c0["role"] != "user" {
		t.Errorf("contents[0].role = %v, want user", c0["role"])
	}
	toolsArr, _ := sent["tools"].([]any)
	if len(toolsArr) != 1 {
		t.Fatalf("tools len = %d", len(toolsArr))
	}
	tools0, _ := toolsArr[0].(map[string]any)
	decls, _ := tools0["functionDeclarations"].([]any)
	decl0, _ := decls[0].(map[string]any)
	if decl0["name"] != "get_weather" {
		t.Errorf("functionDeclarations[0].name = %v", decl0["name"])
	}

	if resp.Choices[0].Message.Content != "hi" {
		t.Errorf("content = %q", resp.Choices[0].Message.Content)
	}
}

func TestGeminiAdapter_FunctionCallResponse(t *testing.T) {
	srv, _, _ := fakeChatUpstream(t,
		`{"candidates":[{"content":{"parts":[`+
			`{"text":"checking"},`+
			`{"functionCall":{"name":"search_posts","args":{"query":"海边"}}}`+
			`]},"finishReason":"STOP"}]}`)

	resp, err := geminiAdapter{}.Chat(context.Background(), srv.URL, "k", "m", &ChatRequest{
		Messages: []Message{{Role: "user", Content: "海边"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Choices[0].Message.Content != "checking" {
		t.Errorf("content = %q", resp.Choices[0].Message.Content)
	}
	tc := resp.Choices[0].Message.ToolCalls
	if len(tc) != 1 || tc[0].Function.Name != "search_posts" {
		t.Fatalf("tool_calls = %+v", tc)
	}
	// call-id 由 adapter 合成（前缀 gemini-）
	if !strings.HasPrefix(tc[0].ID, "gemini-search_posts-") {
		t.Errorf("ID = %q, want 合成的 gemini- 前缀", tc[0].ID)
	}
	// args object → JSON 字符串
	if tc[0].Function.Arguments != `{"query":"海边"}` {
		t.Errorf("arguments = %q", tc[0].Function.Arguments)
	}
}

func TestGeminiAdapter_FunctionResponseEcho(t *testing.T) {
	srv, _, bodyP := fakeChatUpstream(t,
		`{"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`)

	// tool 消息的 ToolCallID 用合成的 gemini- 格式，验证 toolNameFromCallID 还原
	_, err := geminiAdapter{}.Chat(context.Background(), srv.URL, "k", "m", &ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "记一条"},
			{Role: "assistant", ToolCalls: []ToolCall{newToolCall("gemini-create_post-0", "create_post", `{"content":"hi"}`)}},
			{Role: "tool", Content: `{"id":1}`, ToolCallID: "gemini-create_post-0"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	var sent map[string]any
	_ = json.Unmarshal(*bodyP, &sent)
	contents, _ := sent["contents"].([]any)
	// 期望顺序：user / model(functionCall) / user(functionResponse)
	if len(contents) != 3 {
		t.Fatalf("contents len = %d, want 3: %+v", len(contents), contents)
	}
	// model 带 functionCall（role = model，非 assistant）
	model, _ := contents[1].(map[string]any)
	if model["role"] != "model" {
		t.Errorf("contents[1].role = %v, want model", model["role"])
	}
	mParts, _ := model["parts"].([]any)
	fcPart, _ := mParts[0].(map[string]any)
	fc, _ := fcPart["functionCall"].(map[string]any)
	if fc["name"] != "create_post" {
		t.Errorf("functionCall.name = %v", fc["name"])
	}
	// args 必须是 object
	args, ok := fc["args"].(map[string]any)
	if !ok {
		t.Errorf("functionCall.args should be object, got %T", fc["args"])
	} else if args["content"] != "hi" {
		t.Errorf("functionCall.args = %+v", args)
	}
	// user 带 functionResponse（response 必须是 object）
	resp, _ := contents[2].(map[string]any)
	if resp["role"] != "user" {
		t.Errorf("contents[2].role = %v, want user", resp["role"])
	}
	rParts, _ := resp["parts"].([]any)
	frPart, _ := rParts[0].(map[string]any)
	fr, _ := frPart["functionResponse"].(map[string]any)
	// name 从合成 call-id 还原
	if fr["name"] != "create_post" {
		t.Errorf("functionResponse.name = %v, want create_post（从合成 call-id 还原）", fr["name"])
	}
	frResp, ok := fr["response"].(map[string]any)
	if !ok {
		t.Errorf("functionResponse.response must be object, got %T", fr["response"])
	} else if frResp["id"] != float64(1) {
		t.Errorf("functionResponse.response = %+v", frResp)
	}
}

func TestToolNameFromCallID(t *testing.T) {
	tests := []struct{ in, want string }{
		{"gemini-create_post-0", "create_post"},
		{"gemini-search_posts-12", "search_posts"},
		{"toolu_abc", "toolu_abc"},                        // 非 gemini 前缀原样返回
		{"gemini-solo", "solo"},                           // 无尾部序号
		{"gemini-my-tool-1791536698308596485", "my-tool"}, // 带连字符的工具名
		{"", ""},
	}
	for _, tc := range tests {
		if got := toolNameFromCallID(tc.in); got != tc.want {
			t.Errorf("toolNameFromCallID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestGeminiAdapter_Errors(t *testing.T) {
	t.Run("error 字段", func(t *testing.T) {
		srv, _, _ := fakeChatUpstream(t, `{"candidates":[],"error":{"message":"bad"}}`)
		_, err := geminiAdapter{}.Chat(context.Background(), srv.URL, "k", "m", &ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}}})
		if err == nil || !strings.Contains(err.Error(), "bad") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("空 candidates", func(t *testing.T) {
		srv, _, _ := fakeChatUpstream(t, `{"candidates":[]}`)
		_, err := geminiAdapter{}.Chat(context.Background(), srv.URL, "k", "m", &ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}}})
		if err == nil || !strings.Contains(err.Error(), "模型返回空响应") {
			t.Fatalf("err = %v", err)
		}
	})
}

// 本组守一个真实故障（用户报 502）：gemini 的工具往返只带 name 不带 id，
// 转换型网关（gemini → 必须有 call-id 的协议）只能拿工具名当 id，
// 同一工具在不同轮各调一次就撞成两个同 id，上游报 Duplicate function_call_output。
// 修法：回放带上本仓唯一的 id，且它必须跨轮唯一。

// geminiParts 取出 contents 里全部 functionCall / functionResponse 的内层 map（按出现顺序）。
func geminiParts(contents []any) (calls, resps []map[string]any) {
	for _, c := range contents {
		cm, _ := c.(map[string]any)
		parts, _ := cm["parts"].([]any)
		for _, p := range parts {
			pm, _ := p.(map[string]any)
			if fc, ok := pm["functionCall"].(map[string]any); ok {
				calls = append(calls, fc)
			}
			if fr, ok := pm["functionResponse"].(map[string]any); ok {
				resps = append(resps, fr)
			}
		}
	}
	return calls, resps
}

func idOr(o map[string]any) string {
	s, _ := o["id"].(string)
	return s
}

// TestGeminiReplayKeepsDistinctToolIDs 同一工具调用两次，回放的 id 必须互不相同且两侧一致。
// 去掉「回放带 id」即变红。
func TestGeminiReplayKeepsDistinctToolIDs(t *testing.T) {
	// 形状取自真实会话：同一工具 get_record 在不同轮各调一次
	_, contents := buildGeminiContents([]Message{
		{Role: "user", Content: "看看记录"},
		{Role: "assistant", ToolCalls: []ToolCall{newToolCall("call_9815d660", "get_record", `{"id":1}`)}},
		{Role: "tool", Content: `{"r":"动态"}`, ToolCallID: "call_9815d660", ToolName: "get_record"},
		{Role: "user", Content: "再看一张照片"},
		{Role: "assistant", ToolCalls: []ToolCall{newToolCall("call_a14a7810", "get_record", `{"id":2}`)}},
		{Role: "tool", Content: `{"r":"媒体"}`, ToolCallID: "call_a14a7810", ToolName: "get_record"},
	})
	calls, resps := geminiParts(contents)
	if len(calls) != 2 || len(resps) != 2 {
		t.Fatalf("functionCall=%d functionResponse=%d, want 各 2", len(calls), len(resps))
	}
	if idOr(calls[0]) == idOr(calls[1]) {
		t.Errorf("两次调用的 functionCall.id 相同（%q）——网关会当同一个 call，上游报 Duplicate", idOr(calls[0]))
	}
	if idOr(resps[0]) == idOr(resps[1]) {
		t.Errorf("两次回执的 functionResponse.id 相同（%q）——上游报 Duplicate function_call_output", idOr(resps[0]))
	}
	// 两侧必须一致，否则变成「输出找不到对应的调用」
	for i := range calls {
		if idOr(calls[i]) != idOr(resps[i]) {
			t.Errorf("第 %d 对 id 不一致：functionCall=%q functionResponse=%q", i+1, idOr(calls[i]), idOr(resps[i]))
		}
	}
}

// TestGeminiReplayFillsEmptyID 库里 id 为空（那几轮由别的协议产出且供应商没回 id）时，
// 回放要**分配一个 id 并两侧一致**：不带 id 的话转换型网关会退回拿工具名当 id，
// 同名工具调两次即被上游以 Duplicate 拒。
func TestGeminiReplayFillsEmptyID(t *testing.T) {
	_, contents := buildGeminiContents([]Message{
		{Role: "user", Content: "记一条"},
		{Role: "assistant", ToolCalls: []ToolCall{newToolCall("", "create_post", `{}`)}},
		{Role: "tool", Content: `{"ok":true}`, ToolName: "create_post"},
	})
	calls, resps := geminiParts(contents)
	if len(calls) != 1 || len(resps) != 1 {
		t.Fatalf("functionCall=%d functionResponse=%d, want 各 1", len(calls), len(resps))
	}
	if idOr(calls[0]) == "" {
		t.Error("空 id 应被补上一个合成 id，否则网关只能拿工具名充当 call-id")
	}
	if idOr(calls[0]) != idOr(resps[0]) {
		t.Errorf("两侧 id 必须一致：functionCall=%q functionResponse=%q", idOr(calls[0]), idOr(resps[0]))
	}
}

// TestGeminiReplayEnforcesUniqueIDs 回放的**整请求级不变式**：
// 每个 functionCall 恰好一个同 id 的 functionResponse，且请求内 id 互不重复。
//
// 两类会被上游拒的历史都在此：空 id（网关退回用工具名）与重复 id（上游报 Duplicate）。
// 变异验证：去掉 replayGeminiCallID 的判重/补空 → 本用例变红。
func TestGeminiReplayEnforcesUniqueIDs(t *testing.T) {
	_, contents := buildGeminiContents([]Message{
		{Role: "user", Content: "我们记过什么"},
		// ① 空 id：两个不同工具（供应商没回 id）
		{Role: "assistant", ToolCalls: []ToolCall{newToolCall("", "get_space_stats", `{}`)}},
		{Role: "tool", Content: `{"a":1}`, ToolName: "get_space_stats"},
		// ② 重复 id：同一工具两次（旧合成格式 gemini-x-0 跨轮即如此；网关按名合成亦然）
		{Role: "assistant", ToolCalls: []ToolCall{newToolCall("gemini-get_record-0", "get_record", `{"id":1}`)}},
		{Role: "tool", Content: `{"r":1}`, ToolCallID: "gemini-get_record-0", ToolName: "get_record"},
		{Role: "assistant", ToolCalls: []ToolCall{newToolCall("gemini-get_record-0", "get_record", `{"id":2}`)}},
		{Role: "tool", Content: `{"r":2}`, ToolCallID: "gemini-get_record-0", ToolName: "get_record"},
		// ③ 孤儿 tool 行：请求里没有对应调用
		{Role: "tool", Content: `{"orphan":true}`, ToolCallID: "call_ghost", ToolName: "find_posts"},
	})
	calls, resps := geminiParts(contents)
	if len(calls) != 3 {
		t.Fatalf("functionCall = %d, want 3", len(calls))
	}

	// 每个 functionCall 的 id 非空
	byCall := map[string]int{}
	for i, c := range calls {
		id := idOr(c)
		if id == "" {
			t.Fatalf("第 %d 个 functionCall 没带 id —— 网关只能拿工具名充当", i+1)
		}
		byCall[id]++
	}
	// 请求内不重复
	for id, n := range byCall {
		if n > 1 {
			t.Errorf("functionCall 的 id %q 出现 %d 次 —— 上游会以 Duplicate 拒", id, n)
		}
	}
	// 一一配对（孤儿回执不带 id，故只数带 id 的回执）
	respIDs := map[string]int{}
	for _, r := range resps {
		if id := idOr(r); id != "" {
			respIDs[id]++
		}
	}
	for id, n := range byCall {
		if respIDs[id] != n {
			t.Errorf("id %q：functionCall %d 个、functionResponse %d 个，必须一一对应", id, n, respIDs[id])
		}
	}
}

// TestPickGeminiCallID 解析侧：合成 id 靠**自增**而非时钟（时钟粒度粗时会给出相同值，
// 回拨还会与库里的旧 id 重逢），且仍能被反解出工具名；上游给了 id 则原样沿用。
func TestPickGeminiCallID(t *testing.T) {
	const prefix = "gemini-get_record-"
	var prev int64
	for i := 0; i < 3; i++ {
		id := pickGeminiCallID("", "get_record")
		n, err := strconv.ParseInt(strings.TrimPrefix(id, prefix), 10, 64)
		if err != nil {
			t.Fatalf("id 形态变了：%q", id)
		}
		if i > 0 && n != prev+1 {
			t.Fatalf("序号未递增 1：前 %d、本次 %d —— 唯一性必须靠自增，不能只靠时钟", prev, n)
		}
		prev = n
		if got := toolNameFromCallID(id); got != "get_record" {
			t.Errorf("toolNameFromCallID(%q) = %q, want get_record", id, got)
		}
	}
	if got := pickGeminiCallID("call_upstream_42", "get_record"); got != "call_upstream_42" {
		t.Errorf("上游给了 id 应原样沿用，得 %q", got)
	}
}

// TestGeminiAdapter_KeepsUpstreamCallID 上游回了 functionCall.id 时，适配器必须原样沿用，
// 不得用自造 id 覆盖（否则回执对不上上游那一次调用）。
func TestGeminiAdapter_KeepsUpstreamCallID(t *testing.T) {
	const upstreamID = "call_upstream_42"
	srv, _, _ := fakeChatUpstream(t,
		`{"candidates":[{"content":{"parts":[`+
			`{"functionCall":{"id":"`+upstreamID+`","name":"get_record","args":{"id":1}}}`+
			`]},"finishReason":"STOP"}]}`)

	resp, err := geminiAdapter{}.Chat(context.Background(), srv.URL, "k", "m", &ChatRequest{
		Messages: []Message{{Role: "user", Content: "看看记录"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	tc := resp.Choices[0].Message.ToolCalls
	if len(tc) != 1 {
		t.Fatalf("tool_calls = %+v", tc)
	}
	if tc[0].ID != upstreamID {
		t.Errorf("ID = %q, want 沿用的上游 id %q（自造 id 会让回执对不上）", tc[0].ID, upstreamID)
	}
}

// TestGeminiReplayPassesThroughWellFormedIDs 格式良好的历史（id 非空且不重复）必须**原样沿用**。
//
// 这条守的是「修复不改变本来正常的请求」：真实 Gemini 要求回模型给的那个 id，
// 若为了判重而一律重写，正常链路反而会被拒。会话级实测（真实网关成功）用的正是这种历史，
// 故本用例也是那次实测在无网环境下的等价性替身。
func TestGeminiReplayPassesThroughWellFormedIDs(t *testing.T) {
	_, contents := buildGeminiContents([]Message{
		{Role: "user", Content: "看看记录"},
		{Role: "assistant", ToolCalls: []ToolCall{newToolCall("call_9815d660", "get_record", `{"id":1}`)}},
		{Role: "tool", Content: `{"r":"动态"}`, ToolCallID: "call_9815d660", ToolName: "get_record"},
		{Role: "assistant", ToolCalls: []ToolCall{newToolCall("toolu_abc", "find_posts", `{}`)}},
		{Role: "tool", Content: `{"r":"列表"}`, ToolCallID: "toolu_abc", ToolName: "find_posts"},
	})
	calls, resps := geminiParts(contents)
	wantCalls := []string{"call_9815d660", "toolu_abc"}
	wantResps := []string{"call_9815d660", "toolu_abc"}
	if len(calls) != 2 || len(resps) != 2 {
		t.Fatalf("functionCall=%d functionResponse=%d, want 各 2", len(calls), len(resps))
	}
	for i := range wantCalls {
		if got := idOr(calls[i]); got != wantCalls[i] {
			t.Errorf("functionCall[%d].id = %q, want 原样沿用 %q", i, got, wantCalls[i])
		}
		if got := idOr(resps[i]); got != wantResps[i] {
			t.Errorf("functionResponse[%d].id = %q, want 原样沿用 %q", i, got, wantResps[i])
		}
	}
}
