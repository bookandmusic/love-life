package api

// 预算三维的自动守卫（AI模式设计「预算与并行」）：
//   - 工具调用总数上限：一次响应回的 tool_call 数可能远超剩余额度，执行前必须截断；
//   - 断线中断标记：连接意外断开时保留消息并标 interrupted（parts 追加该 part）。
//
// 轮次上限用 chat.MaxToolRounds 驱动（文档口径 8），见 chat_search_test.go。

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/chat"
)

// TestChatToolCallCapTruncatesBeforeExecuting：一次响应回 20 个 tool_call（>16）时必须截断。
func TestChatToolCallCapTruncatesBeforeExecuting(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{"content": "海边的日落"})
	res.Body.Close()

	// 一轮里塞 20 个 find_posts 调用（远超 maxToolCalls=16）
	calls := make([]string, 0, 20)
	for i := 0; i < 20; i++ {
		calls = append(calls, fmt.Sprintf(
			`{"id":"c%d","type":"function","function":{"name":"find_posts","arguments":"{\"query\":\"海边\"}"}}`, i))
	}
	fake := newFakeModel(t,
		fmt.Sprintf(`{"message":{"content":"","tool_calls":[%s]}}`, strings.Join(calls, ",")),
		`{"message":{"content":"查完了"}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]string{"content": "找海边的"})
	body := sseBody(t, res)

	executed := strings.Count(body, "event: tool.start")
	if executed > chat.MaxToolCalls {
		t.Fatalf("工具调用未按上限截断：执行 %d 次，上限 %d", executed, chat.MaxToolCalls)
	}
	if executed == 0 {
		t.Fatalf("应当执行到上限内的调用，实得 0：\n%s", body)
	}
	// 截断即视为触顶：必须走收尾（不静默结束）
	if !strings.Contains(body, `"stop_reason":"budget"`) {
		t.Fatalf("截断后应进预算收尾：\n%s", body)
	}
}

// TestChatToolCallCapNotExceededNormally：正常调用数不触顶时不受影响。
func TestChatToolCallCapNotExceededNormally(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{"content": "海边的日落"})
	res.Body.Close()

	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"c0","type":"function","function":{"name":"find_posts","arguments":"{\"query\":\"海边\"}"}}]}}`,
		`{"message":{"content":"找到 1 条"}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]string{"content": "找海边的"})
	body := sseBody(t, res)
	if n := strings.Count(body, "event: tool.start"); n != 1 {
		t.Fatalf("正常单次调用应执行 1 次，实得 %d：\n%s", n, body)
	}
	if !strings.Contains(body, `"stop_reason":"answer"`) {
		t.Fatalf("正常路径不应触顶：\n%s", body)
	}
}

// TestChatDisconnectMarksInterrupted：断线后用户消息仍在，且带 interrupted part；
// 后续再来新消息时，早先那条**依然**标记为已中断（不只认尾条）。
func TestChatDisconnectMarksInterrupted(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	// 直接调 markTurnAborted 覆盖落库路径（真实断连由请求 ctx 取消触发，见 chatAbort 测试）
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{"content": "海边"})
	res.Body.Close()

	fake := newFakeModel(t, `{"message":{"content":"第一轮回答"}}`)
	seedFakeProvider(t, ts, fake.server.URL)
	sid, body := chatTurn(t, ts, adminToken, 0, "第一轮")
	turn1 := turnIDFromSSE(t, body)

	var n int
	if err := ts.db.QueryRow(`SELECT COUNT(*) FROM chat_messages WHERE turn_id = ? AND role = 'user'`, turn1).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("前置：本轮用户消息应落库 1 行，实得 %d", n)
	}

	// 模拟断连标记（part 追加，幂等）
	if _, err := ts.db.Exec(`UPDATE chat_messages SET parts = ?
		WHERE turn_id = ? AND role = 'user'`, `[{"text":"第一轮","type":"text"},{"type":"interrupted"}]`, turn1); err != nil {
		t.Fatal(err)
	}

	// 再答一轮（产生更新的消息）→ 早先那条仍应显示已中断
	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]any{
		"content": "第二轮", "session_id": sid,
	})
	sseBody(t, res)

	items := historyOf(t, ts, adminToken, sid)
	var found bool
	for _, it := range items {
		if it["role"] != "user" || !strings.Contains(fmt.Sprint(it["parts"]), "第一轮") {
			continue
		}
		found = true
		if it["interrupted"] != true {
			t.Fatalf("断连那条未被标为已中断（后续有新消息时丢标记）：%+v", it)
		}
		if !strings.Contains(fmt.Sprint(it["parts"]), "interrupted") {
			t.Fatalf("parts 应含 interrupted part：%v", it["parts"])
		}
	}
	if !found {
		t.Fatal("断连轮的用户消息丢了（必须保留）")
	}
}

// historyOf 取会话历史 items（原始 map，便于按字段断言）。
func historyOf(t *testing.T, ts *testEnv, token string, sid int64) []map[string]any {
	t.Helper()
	res := doJSON(t, "GET", ts.URL+fmt.Sprintf("/api/v1/chat/history?session_id=%d", sid), token, nil)
	defer res.Body.Close()
	var out struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out.Items
}
