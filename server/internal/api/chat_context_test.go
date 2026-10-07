package api

// 模型上下文组装（2026-09-22）：已裁决的确认卡必须以「工具调用往返」形式回放给模型，
// 否则模型看不到历史操作是否已执行（实测：重复出卡 / 声称未执行）。
// 断言口径 = 模型实际收到的 messages（fakeModel.lastMessages），而非脚本化回复。
// 测例矩阵：M1 已执行 / M2 pending / M3 取消 / M4 过期 / M5 失败 / M6 三态混合 / M7 结果缺失回落。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// toolRoundTrips 从 messages 中提取「assistant.tool_calls → 紧随 tool 结果」的往返对。
func toolRoundTrips(t *testing.T, msgs []map[string]any) []map[string]string {
	t.Helper()
	var out []map[string]string
	for i, m := range msgs {
		if m["role"] != "assistant" {
			continue
		}
		calls, _ := m["tool_calls"].([]any)
		for _, c := range calls {
			cm, _ := c.(map[string]any)
			fn, _ := cm["function"].(map[string]any)
			id, _ := cm["id"].(string)
			entry := map[string]string{
				"id":        id,
				"name":      fmt.Sprint(fn["name"]),
				"arguments": fmt.Sprint(fn["arguments"]),
			}
			// 紧随其后的 tool 消息必须同 id（协议要求一一配对）
			if i+1 < len(msgs) && msgs[i+1]["role"] == "tool" {
				entry["tool_call_id"] = fmt.Sprint(msgs[i+1]["tool_call_id"])
				entry["result"] = fmt.Sprint(msgs[i+1]["content"])
			}
			out = append(out, entry)
		}
	}
	return out
}

func messagesContain(msgs []map[string]any, sub string) bool {
	for _, m := range msgs {
		if s, _ := m["content"].(string); strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// chatTurn 在指定会话内发一轮消息并读完 SSE（返回会话 id 与流内容）。
func chatTurn(t *testing.T, ts *testEnv, token string, sessionID int64, content string) (int64, string) {
	t.Helper()
	res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", token,
		map[string]any{"content": content, "session_id": sessionID})
	body := sseBody(t, res)
	// session 事件只在懒创建时下发；已有会话沿用请求里的 id
	if sessionID == 0 {
		sessionID = sessionIDFromSSE(t, body)
	}
	return sessionID, body
}

func confirmCard(t *testing.T, ts *testEnv, token string, actionID int64, confirmed bool) string {
	t.Helper()
	decision := "cancel"
	if confirmed {
		decision = "approve"
	}
	res := doJSON(t, "POST", ts.URL+fmt.Sprintf("/api/v1/chat/proposals/%d/resolve", actionID), token,
		map[string]any{"decision": decision})
	defer res.Body.Close()
	var out struct {
		Status string `json:"status"`
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("confirm: %d %+v", res.StatusCode, out)
	}
	return out.Status
}

func cardSSE(scriptContent string) string {
	return `{"message":{"content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"propose_post","arguments":"{\"content\":\"` + scriptContent + `\"}"}}]}}`
}

// M1：已执行卡片 → 下一轮上下文含完整工具往返（且载体描述/marker 不得出现）。
func TestModelContextExecutedCardReplay(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	fake := newFakeModel(t, cardSSE("海边看日落"), `{"message":{"content":"好呀"}}`)
	seedFakeProvider(t, ts, fake.server.URL)

	sid, body := chatTurn(t, ts, adminToken, 0, "帮我发动态：海边看日落")
	actionID := cardFromSSE(t, body)
	if st := confirmCard(t, ts, adminToken, actionID, true); st != "executed" {
		t.Fatalf("confirm status = %q", st)
	}

	chatTurn(t, ts, adminToken, sid, "我刚才那条动态发成功了吗")
	msgs := fake.lastMessages(t)

	trips := toolRoundTrips(t, msgs)
	if len(trips) != 1 {
		t.Fatalf("want 1 tool round trip, got %+v", trips)
	}
	tr := trips[0]
	if tr["name"] != "propose_post" || !strings.Contains(tr["arguments"], "海边看日落") {
		t.Fatalf("round trip call wrong: %+v", tr)
	}
	if tr["tool_call_id"] != tr["id"] {
		t.Fatalf("tool_call_id must pair with call id: %+v", tr)
	}
	if !strings.Contains(tr["result"], "已发布") {
		t.Fatalf("executed result text missing: %+v", tr)
	}
	// 载体描述与旧 marker 不得进上下文（A5：会被模仿成「文字复述代替调工具」）
	if messagesContain(msgs, "需用户确认后生效") || messagesContain(msgs, "已生成确认卡片，等待确认") {
		t.Fatalf("card carrier text leaked into model context: %+v", msgs)
	}
	// 用户消息按原始顺序保留
	var users []string
	for _, m := range msgs {
		if m["role"] == "user" {
			users = append(users, fmt.Sprint(m["content"]))
		}
	}
	if len(users) != 2 || !strings.Contains(users[0], "海边看日落") || !strings.Contains(users[1], "发成功了吗") {
		t.Fatalf("user messages lost or reordered: %v", users)
	}
}

// M2：pending 卡片不进上下文（未发生的操作模型不知道）。
func TestModelContextPendingCardNotReplayed(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	fake := newFakeModel(t, cardSSE("还没确认的动态"), `{"message":{"content":"好呀"}}`)
	seedFakeProvider(t, ts, fake.server.URL)

	sid, body := chatTurn(t, ts, adminToken, 0, "帮我发动态：还没确认的动态")
	if cardFromSSE(t, body) == 0 {
		t.Fatal("card expected")
	}
	chatTurn(t, ts, adminToken, sid, "在吗")
	msgs := fake.lastMessages(t)

	if trips := toolRoundTrips(t, msgs); len(trips) != 0 {
		t.Fatalf("pending card must not be replayed: %+v", trips)
	}
	for _, m := range msgs {
		if m["role"] == "tool" {
			t.Fatalf("no tool message expected: %+v", msgs)
		}
	}
}

// M3：取消的卡片 → 往返结果「用户取消了该操作，未执行」。
func TestModelContextCancelledCardReplay(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	fake := newFakeModel(t, cardSSE("不想发了"), `{"message":{"content":"好的"}}`)
	seedFakeProvider(t, ts, fake.server.URL)

	sid, body := chatTurn(t, ts, adminToken, 0, "帮我发动态：不想发了")
	actionID := cardFromSSE(t, body)
	if st := confirmCard(t, ts, adminToken, actionID, false); st != "cancelled" {
		t.Fatalf("confirm status = %q", st)
	}
	chatTurn(t, ts, adminToken, sid, "算了")
	trips := toolRoundTrips(t, fake.lastMessages(t))
	if len(trips) != 1 || !strings.Contains(trips[0]["result"], "用户取消了该操作，未执行") {
		t.Fatalf("cancelled outcome wrong: %+v", trips)
	}
}

// M4：过期卡片（改库把 expires_at 置为过去）→ 「确认卡片已过期，未执行」。
func TestModelContextExpiredCardReplay(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	fake := newFakeModel(t, cardSSE("过期也没发"), `{"message":{"content":"好的"}}`)
	seedFakeProvider(t, ts, fake.server.URL)

	sid, body := chatTurn(t, ts, adminToken, 0, "帮我发动态：过期也没发")
	actionID := cardFromSSE(t, body)
	if _, err := ts.db.Exec(`UPDATE confirm_actions SET expires_at = ? WHERE id = ?`, "2020-01-01T00:00:00Z", actionID); err != nil {
		t.Fatal(err)
	}
	// 过期前置裁决：approve 触发 410 前置（置 expired 并回填上下文），再走下一轮验证回放
	res := doJSON(t, "POST", ts.URL+fmt.Sprintf("/api/v1/chat/proposals/%d/resolve", actionID), adminToken, map[string]any{"decision": "approve"})
	res.Body.Close()
	if res.StatusCode != http.StatusGone {
		t.Fatalf("expired resolve: want 410, got %d", res.StatusCode)
	}
	chatTurn(t, ts, adminToken, sid, "那条发了吗")
	trips := toolRoundTrips(t, fake.lastMessages(t))
	if len(trips) != 1 || !strings.Contains(trips[0]["result"], "确认卡片已过期，未执行") {
		t.Fatalf("expired outcome wrong: %+v", trips)
	}
}

// M5：执行失败的卡片 → 「执行失败：…」。用相册增员随 resolve 带 media_tokens 的既有失败路径
// （propose 阶段校验通过、execute 阶段被守卫拒绝，正是「执行失败」这一态）。
func TestModelContextFailedCardReplay(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	// 先建相册（album_id=1）：提案前置校验要求相册存在
	res0 := doJSON(t, "POST", ts.URL+"/api/v1/albums", adminToken, map[string]any{"name": "目的地相册"})
	res0.Body.Close()
	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"a1","type":"function","function":{"name":"propose_album_members","arguments":"{\"album_id\":1,\"add_media_ids\":[1]}"}}]}}`,
		`{"message":{"content":"好的"}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	mediaToken, _ := uploadMedia(t, ts.URL, adminToken, "p.jpg", genJPEG(t))
	sid, body := chatTurn(t, ts, adminToken, 0, "把照片加到相册")
	actionID := cardFromSSE(t, body)
	res := doJSON(t, "POST", ts.URL+fmt.Sprintf("/api/v1/chat/proposals/%d/resolve", actionID), adminToken, map[string]any{
		"decision": "approve", "media_tokens": []string{mediaToken},
	})
	var out struct {
		Status string `json:"status"`
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	res.Body.Close()
	if out.Status != "failed" {
		t.Fatalf("expect failed confirm, got %q", out.Status)
	}

	chatTurn(t, ts, adminToken, sid, "相册加好了吗")
	trips := toolRoundTrips(t, fake.lastMessages(t))
	if len(trips) != 1 {
		t.Fatalf("want 1 trip: %+v", trips)
	}
	// 回执文本 = 真实执行错误（cardOutcome 对 failed：result 非空 → 「执行失败：<原因>」，
	// 裁决回执 text 即执行器错误原文），二者必含「失败」语义
	if !strings.Contains(trips[0]["result"], "失败") {
		t.Fatalf("failed outcome wrong: %+v", trips)
	}
}

// M6：混合组合——同会话内 已执行 A / 已取消 B / pending C 三态共存且互不串位。
func TestModelContextMixedStates(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	fake := newFakeModel(t,
		cardSSE("动态A"), cardSSE("动态B"), cardSSE("动态C"),
		`{"message":{"content":"好的"}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	sid, body := chatTurn(t, ts, adminToken, 0, "帮我发动态：动态A")
	if st := confirmCard(t, ts, adminToken, cardFromSSE(t, body), true); st != "executed" {
		t.Fatalf("A status=%q", st)
	}
	_, body = chatTurn(t, ts, adminToken, sid, "帮我发动态：动态B")
	if st := confirmCard(t, ts, adminToken, cardFromSSE(t, body), false); st != "cancelled" {
		t.Fatalf("B status=%q", st)
	}
	_, body = chatTurn(t, ts, adminToken, sid, "帮我发动态：动态C")
	_ = cardFromSSE(t, body) // C 保持 pending，不确认

	chatTurn(t, ts, adminToken, sid, "总结一下现在的情况")
	trips := toolRoundTrips(t, fake.lastMessages(t))
	if len(trips) != 2 {
		t.Fatalf("want 2 trips (A executed + B cancelled, C pending excluded), got %+v", trips)
	}
	var sawA, sawB bool
	for _, tr := range trips {
		switch {
		case strings.Contains(tr["arguments"], "动态A") && strings.Contains(tr["result"], "已发布"):
			sawA = true
		case strings.Contains(tr["arguments"], "动态B") && strings.Contains(tr["result"], "用户取消了该操作"):
			sawB = true
		case strings.Contains(tr["arguments"], "动态C"):
			t.Fatalf("pending C must be excluded: %+v", tr)
		}
	}
	if !sawA || !sawB {
		t.Fatalf("mixed states wrong: %+v", trips)
	}
}

// M7：卡片载体在窗口内、执行结果行缺失 → 结果回落「操作已执行」（自洽不空洞）。
func TestModelContextMissingResultFallback(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	fake := newFakeModel(t, cardSSE("结果行会删掉"), `{"message":{"content":"好的"}}`)
	seedFakeProvider(t, ts, fake.server.URL)

	sid, body := chatTurn(t, ts, adminToken, 0, "帮我发动态：结果行会删掉")
	actionID := cardFromSSE(t, body)
	if st := confirmCard(t, ts, adminToken, actionID, true); st != "executed" {
		t.Fatalf("status=%q", st)
	}
	// 回执行（parts 载体）与上下文回放解耦：清空回执行不影响 pinned 上下文里的真实结果
	if _, err := ts.db.Exec(`UPDATE chat_messages SET parts = '[]' WHERE card_id = ? AND role = 'assistant' AND parts LIKE '%tool%'`, actionID); err != nil {
		t.Fatal(err)
	}
	chatTurn(t, ts, adminToken, sid, "那条成功了吗")
	trips := toolRoundTrips(t, fake.lastMessages(t))
	if len(trips) != 1 || !strings.Contains(trips[0]["result"], "已发布") {
		t.Fatalf("replay result wrong: %+v", trips)
	}
}
