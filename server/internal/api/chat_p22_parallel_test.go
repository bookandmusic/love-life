package api

// 读工具并行的守卫（AI模式设计「预算与并行」：同一次响应里的多个读工具并行执行，
// 事件按 call_id 归位、parts 与上下文配对按**发起顺序**写）。
//
// 并行最容易错的两件事：① 顺序由完成顺序决定（parts/配对行乱序）；
// ② 把「出提案之后」的 call 也执行掉（一轮一卡被破坏）。本文件守前者，
// 后者由 TestChatProposalStopsRemainingCalls 守。

import (
	"encoding/json"
	"strings"
	"testing"
)

// threeReadCallsScript 一次响应里三个读工具（跨越不同工具与不同结果集 kind）。
const threeReadCallsScript = `{"message":{"content":"","tool_calls":[` +
	`{"id":"c1","type":"function","function":{"name":"find_posts","arguments":"{\"query\":\"海边\"}"}},` +
	`{"id":"c2","type":"function","function":{"name":"find_albums","arguments":"{}"}},` +
	`{"id":"c3","type":"function","function":{"name":"find_anniversaries","arguments":"{}"}}]}}`

// TestParallelReadToolsPreserveInitiationOrder：三个读工具并行执行后，
// parts 与上下文配对都按**发起顺序**（c1→c2→c3），不随完成顺序漂移。
func TestParallelReadToolsPreserveInitiationOrder(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	// 三类各造一条命中，确保三个工具都真的执行
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{"content": "海边的日落"})
	res.Body.Close()
	res = doJSON(t, "POST", ts.URL+"/api/v1/albums", adminToken, map[string]any{"name": "杭州之旅"})
	res.Body.Close()
	res = doJSON(t, "POST", ts.URL+"/api/v1/anniversaries", adminToken, map[string]any{"name": "在一起", "date": "2024-05-20"})
	res.Body.Close()

	fake := newFakeModel(t, threeReadCallsScript, `{"message":{"content":"都查到了"}}`)
	seedFakeProvider(t, ts, fake.server.URL)

	sid, body := chatTurn(t, ts, adminToken, 0, "把动态、相册、纪念日都给我看看")

	// 三个工具都要执行并各自发事件（并行不等于漏跑）
	for _, id := range []string{"c1", "c2", "c3"} {
		if !strings.Contains(body, `"call_id":"`+id+`"`) {
			t.Fatalf("缺少 %s 的 tool 事件：\n%s", id, body)
		}
	}
	if n := strings.Count(body, "event: tool.start"); n != 3 {
		t.Fatalf("tool.start 事件数 = %d，want 3（三个读工具都应执行）", n)
	}
	if n := strings.Count(body, "event: tool.end"); n != 3 {
		t.Fatalf("tool.end 事件数 = %d，want 3", n)
	}

	// 证明走的是并行路径：并行实现先把整批 tool.start 发完（顺序确定）再等结果，
	// 故三个 tool.start 必然都先于第一个 tool.end；串行实现是 start1→end1→start2→end2，
	// 不可能满足这一点。
	firstEnd := strings.Index(body, "event: tool.end")
	if firstEnd < 0 {
		t.Fatalf("无 tool.end 事件：\n%s", body)
	}
	for _, id := range []string{"c1", "c2", "c3"} {
		// 该 call_id 的 tool.start 必须出现在首个 tool.end 之前
		if i := strings.Index(body[:firstEnd], `"call_id":"`+id+`"`); i < 0 {
			t.Fatalf("%s 的 tool.start 未在首个 tool.end 之前发出（说明未走并行路径）：\n%s", id, body)
		}
	}

	// parts 顺序 = 发起顺序
	var parts string
	if err := ts.db.QueryRow(`SELECT parts FROM chat_messages
		WHERE session_id = ? AND role = 'assistant' ORDER BY id DESC LIMIT 1`, sid).Scan(&parts); err != nil {
		t.Fatal(err)
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(parts), &list); err != nil {
		t.Fatalf("parts 解析失败：%v (%s)", err, parts)
	}
	var got []string
	for _, p := range list {
		if p["type"] == "tool" {
			id, _ := p["call_id"].(string)
			got = append(got, id)
		}
	}
	want := []string{"c1", "c2", "c3"}
	if len(got) != len(want) {
		t.Fatalf("parts 里 tool 步骤 = %v，want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("parts 顺序 = %v，want %v（须按发起顺序，不由完成顺序决定）", got, want)
		}
	}

	// 上下文配对：tool 行数 == 带 tool_calls 的 assistant 行数，且 tool 行按发起顺序
	var toolRows, assistantRows int
	if err := ts.db.QueryRow(`SELECT
		COALESCE(SUM(CASE WHEN role='tool' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN role='assistant' AND COALESCE(tool_calls_json,'')<>'' THEN 1 ELSE 0 END),0)
		FROM chat_context WHERE session_id = ?`, sid).Scan(&toolRows, &assistantRows); err != nil {
		t.Fatal(err)
	}
	if toolRows != 3 || assistantRows != 3 {
		t.Fatalf("配对失衡：tool 行 %d、assistant tool_calls 行 %d，want 3/3", toolRows, assistantRows)
	}
	rows, err := ts.db.Query(`SELECT tool_call_id FROM chat_context
		WHERE session_id = ? AND role = 'tool' ORDER BY seq ASC`, sid)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ctxOrder []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ctxOrder = append(ctxOrder, id)
	}
	if len(ctxOrder) != len(want) {
		t.Fatalf("上下文 tool 行 = %v，want %v（每对往返一行，不能多也不能少）", ctxOrder, want)
	}
	for i := range want {
		if ctxOrder[i] != want[i] {
			t.Fatalf("上下文 tool 行顺序 = %v，want %v（seq 须按发起顺序）", ctxOrder, want)
		}
	}

	// 下一轮模型实收的 tool 消息：顺序与配对都要对（不能只看「非空」）
	var modelToolIDs []string
	for _, m := range fake.lastMessages(t) {
		if m["role"] == "tool" {
			id, _ := m["tool_call_id"].(string)
			modelToolIDs = append(modelToolIDs, id)
		}
	}
	if len(modelToolIDs) != len(want) {
		t.Fatalf("模型实收 tool 消息 = %v，want %v", modelToolIDs, want)
	}
	for i := range want {
		if modelToolIDs[i] != want[i] {
			t.Fatalf("模型实收 tool 消息顺序 = %v，want %v（按发起顺序）", modelToolIDs, want)
		}
	}
}

// TestChatProposalStopsRemainingCalls：一次响应里「写工具之后」的读工具**不得执行**
// ——出提案即停本轮（一轮一卡），若把后续 call 也并发跑掉就破坏了该语义。
func TestChatProposalStopsRemainingCalls(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[`+
			`{"id":"w1","type":"function","function":{"name":"propose_post","arguments":"{\"content\":\"海边的日落\"}"}},`+
			`{"id":"r1","type":"function","function":{"name":"find_posts","arguments":"{\"query\":\"海边\"}"}}]}}`,
		`{"message":{"content":"出卡了"}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	_, body := chatTurn(t, ts, adminToken, 0, "帮我发条动态，顺便找找海边的")
	if !strings.Contains(body, "event: proposal") {
		t.Fatalf("应出确认卡：\n%s", body)
	}
	// 提案之后的读工具不得执行：不该出现它的 tool.start
	if strings.Contains(body, `"call_id":"r1"`) {
		t.Fatalf("提案之后的 call 不得执行（一轮一卡），SSE：\n%s", body)
	}
}
