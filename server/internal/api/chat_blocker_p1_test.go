package api

// P1 阻断项的回归守卫（B2 纯媒体/仅引用消息、B5 统计软删过滤、B7 回收站对 AI 不可见、D1 AI 互动通知）。

import (
	"net/http"
	"strings"
	"testing"
)

// TestChatEmptyMessageStillRejected：三者全空仍是空消息（400）。
// 与 B2 配对：放开「仅媒体 / 仅引用」不等于放开「什么都没有」。
func TestChatEmptyMessageStillRejected(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	fake := newFakeModel(t, `{"message":{"content":"不该被调用"}}`)
	seedFakeProvider(t, ts, fake.server.URL)

	for _, tc := range []struct {
		name string
		body map[string]any
	}{
		{"全空", map[string]any{}},
		{"仅空白文本", map[string]any{"content": "   "}},
	} {
		res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, tc.body)
		res.Body.Close()
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s：want 400，got %d", tc.name, res.StatusCode)
		}
	}
	if fake.calls.Load() != 0 {
		t.Fatalf("空消息不得调用模型，实调 %d 次", fake.calls.Load())
	}
}

// TestChatMediaOnlyMessageAccepted：只带媒体、无文本 → 能发送且建会话
// （PRODUCT「发布动态」正文可选、可留空只传媒体）。
func TestChatMediaOnlyMessageAccepted(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	fake := newFakeModel(t, `{"message":{"content":"收到你的照片"}}`)
	seedFakeProvider(t, ts, fake.server.URL)

	res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]any{
		"content": "",
		"media": []map[string]any{
			{"name": "a.jpg", "type": "image/jpeg", "size": 1024},
		},
	})
	body := sseBody(t, res)
	if !strings.Contains(body, "event: turn.start") {
		t.Fatalf("仅媒体消息应被接受并开始本轮，SSE：\n%s", body)
	}
	if fake.calls.Load() != 1 {
		t.Fatalf("仅媒体消息应调用模型 1 次，实得 %d", fake.calls.Load())
	}

	// 用户 part 不应出现空 text part（回显空行）
	var parts string
	if err := ts.db.QueryRow(`SELECT parts FROM chat_messages WHERE role = 'user' ORDER BY id DESC LIMIT 1`).Scan(&parts); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(parts, `"type":"text"`) {
		t.Fatalf("空文本不应产生 text part，parts=%s", parts)
	}
	if !strings.Contains(parts, "attachment") {
		t.Fatalf("应含 attachment part，parts=%s", parts)
	}
}

// TestChatWhitespaceOnlyTextProducesNoTextPart：只有空白字符 + 有媒体时，
// 不得落一个只含空白的 text part（入参校验用 TrimSpace，part 判据必须同口径，
// 否则回显会多出一行空白）。
func TestChatWhitespaceOnlyTextProducesNoTextPart(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	fake := newFakeModel(t, `{"message":{"content":"收到"}}`)
	seedFakeProvider(t, ts, fake.server.URL)

	res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]any{
		"content": "   ",
		"media": []map[string]any{
			{"name": "a.jpg", "type": "image/jpeg", "size": 1024},
		},
	})
	body := sseBody(t, res)
	if !strings.Contains(body, "event: turn.start") {
		t.Fatalf("空白文本 + 媒体应被接受，SSE：\n%s", body)
	}

	var parts string
	if err := ts.db.QueryRow(`SELECT parts FROM chat_messages WHERE role = 'user' ORDER BY id DESC LIMIT 1`).Scan(&parts); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(parts, `"type":"text"`) {
		t.Fatalf("仅空白字符不应产生 text part，parts=%s", parts)
	}
}

// TestChatQuotedOnlyMessageAccepted：只勾引用、无文本 → 能发送
// （引用的资源必须来自本会话查询结果，故先跑一轮读工具拿到句柄）。
func TestChatQuotedOnlyMessageAccepted(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{"content": "海边的日落"})
	res.Body.Close()

	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"f1","type":"function","function":{"name":"find_posts","arguments":"{\"query\":\"海边\"}"}}]}}`,
		`{"message":{"content":"找到了一条"}}`,
		`{"message":{"content":"我看到你引用的那条了"}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	sid, body := chatTurn(t, ts, adminToken, 0, "找找海边的动态")
	uis := toolEndUI(t, body)
	if len(uis) == 0 || uis[0].ResultID == "" || len(uis[0].Records) == 0 {
		t.Fatalf("前置：需要一条可引用结果，tool.end ui = %+v", uis)
	}

	// 只勾引用、不带文本
	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]any{
		"content":    "",
		"session_id": sid,
		"quoted":     []map[string]any{{"result_id": uis[0].ResultID, "ids": []int64{uis[0].Records[0].ID}}},
	})
	body2 := sseBody(t, res)
	if !strings.Contains(body2, "event: turn.start") {
		t.Fatalf("仅引用消息应被接受并开始本轮，SSE：\n%s", body2)
	}

	// 引用上下文须转述给模型（不带空 text part 造成的空行）
	var quoted string
	for _, m := range fake.lastMessages(t) {
		if m["role"] == "user" {
			if c, ok := m["content"].(string); ok && strings.Contains(c, "引用") {
				quoted = c
			}
		}
	}
	if quoted == "" {
		t.Fatal("仅引用消息应把引用转述进模型上下文")
	}
}
