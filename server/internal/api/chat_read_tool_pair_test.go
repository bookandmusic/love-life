package api

// 读工具路径的上下文配对守卫（AI模式设计「上下文组装」· 协议配对）：
// `tool_calls_json` 存 assistant 轮的 name+arguments，回放时原样还原配对。
// 严格要求下（OpenAI / Anthropic）`role=tool` 必须紧跟带同 id `tool_calls` 的 assistant 消息；
// 此前的缺陷是读工具那一轮只写 tool 行、不写带 tool_calls 的 assistant 行，
// 跨轮留下孤儿 tool 行（真实供应商可能 400），且助手正文从不进上下文（多轮失忆）。
//
// 现有 chat_context_test.go 的 toolRoundTrips 矩阵只覆盖提案（resolve）路径，
// 本文件补读工具路径。

import (
	"strconv"
	"strings"
	"testing"
)

// TestChatReadToolPairRoundTrip：轮 1 触发读工具、轮 2 追问后，
//  1. 读工具路径必须成对：tool 行数 == 带 tool_calls 的 assistant 行数；
//  2. 模型实收 messages 里每个 tool 结果都紧跟在带同 id 的 assistant.tool_calls 之后；
//  3. 轮 1 的助手正文须进上下文（kind=dialogue）。
func TestChatReadToolPairRoundTrip(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{"content": "海边的日落真美"})
	res.Body.Close()

	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"s1","type":"function","function":{"name":"find_posts","arguments":"{\"query\": \"海边\"}"}}]}}`,
		`{"message":{"content":"找到了：海边的日落真美"}}`,
		`{"message":{"content":"刚才我说的是那条海边的日落"}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	sid, _ := chatTurn(t, ts, adminToken, 0, "我们去过海边吗")

	// 上下文侧：读工具往返应成对落库
	var toolRows, assistantToolRows, assistantDialogueRows int
	if err := ts.db.QueryRow(`SELECT
			COALESCE(SUM(CASE WHEN role = 'tool' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN role = 'assistant' AND COALESCE(tool_calls_json,'') <> '' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN role = 'assistant' AND kind = 'dialogue' THEN 1 ELSE 0 END), 0)
		FROM chat_context WHERE session_id = ?`, sid).Scan(&toolRows, &assistantToolRows, &assistantDialogueRows); err != nil {
		t.Fatal(err)
	}
	if toolRows == 0 {
		t.Fatal("读工具路径应写 tool 行")
	}
	if toolRows != assistantToolRows {
		t.Fatalf("配对失衡：tool 行 %d ≠ 带 tool_calls 的 assistant 行 %d", toolRows, assistantToolRows)
	}
	if assistantDialogueRows == 0 {
		t.Fatal("轮 1 的助手正文应进上下文（kind=dialogue），否则多轮失忆")
	}

	// 轮 2：模型实收 messages 的配对必须完整
	chatTurn(t, ts, adminToken, sid, "再说一次")
	msgs := fake.lastMessages(t)

	trips := toolRoundTrips(t, msgs)
	if len(trips) == 0 {
		t.Fatalf("轮 2 应看到轮 1 的读工具往返，msgs=%+v", msgs)
	}
	if trips[0]["name"] != "find_posts" {
		t.Fatalf("往返工具名 = %q，want find_posts", trips[0]["name"])
	}
	// 孤儿 tool 行：出现 role=tool 但前一条不是带 tool_calls 的 assistant
	for i, m := range msgs {
		if m["role"] != "tool" {
			continue
		}
		if i == 0 {
			t.Fatalf("第 0 条就是 tool 行（孤儿）：%+v", m)
		}
		prev := msgs[i-1]
		if prev["role"] != "assistant" || prev["tool_calls"] == nil {
			t.Fatalf("孤儿 tool 行（前一条 role=%v 无 tool_calls）：%+v", prev["role"], m)
		}
	}
	// 助手正文回放：轮 2 的上下文应含轮 1 说的那句
	found := false
	for _, m := range msgs {
		if m["role"] == "assistant" {
			if s, _ := m["content"].(string); strings.Contains(s, "找到了") {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("轮 2 未收到轮 1 的助手正文，msgs=%+v", msgs)
	}
}

// TestChatReadToolFailPairStillPaired：读工具执行失败时，两个受众都要有回执——
// 失败分支原先完全不写 chat_context，会让该 call 在内存里回了执、在上下文里没有。
func TestChatReadToolFailPairStillPaired(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	// 故意给不可能的 id 让读工具失败（get_record 不存在的动态）
	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"f1","type":"function","function":{"name":"get_record","arguments":"{\"kind\":\"post\",\"id\":999999}"}}]}}`,
		`{"message":{"content":"没找到那条动态"}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	sid, _ := chatTurn(t, ts, adminToken, 0, "查一下 999999 号动态")

	var toolRows, assistantToolRows int
	if err := ts.db.QueryRow(`SELECT
			COALESCE(SUM(CASE WHEN role = 'tool' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN role = 'assistant' AND COALESCE(tool_calls_json,'') <> '' THEN 1 ELSE 0 END), 0)
		FROM chat_context WHERE session_id = ?`, sid).Scan(&toolRows, &assistantToolRows); err != nil {
		t.Fatal(err)
	}
	if toolRows != assistantToolRows {
		t.Fatalf("失败分支配对失衡：tool 行 %d ≠ assistant tool_calls 行 %d", toolRows, assistantToolRows)
	}
	if toolRows == 0 {
		t.Fatal("失败分支也应写配对行（内存与上下文两受众口径一致）")
	}
}

// TestChatContextTrimKeepsPairs：裁剪不得把 assistant 与其 tool 行拆开。
// 构造大量工具往返把上下文顶过预算，断言裁剪后不出现孤儿 tool 行。
func TestChatContextTrimKeepsPairs(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{"content": "海边的日落真美"})
	res.Body.Close()

	// 多轮：每轮一次读工具 + 一次正文，制造足量 read_result 顶破 24000 token 预算
	script := make([]string, 0, 40)
	for i := 0; i < 12; i++ {
		script = append(script,
			`{"message":{"content":"","tool_calls":[{"id":"s`+strconv.Itoa(i)+`","type":"function","function":{"name":"find_posts","arguments":"{\"query\": \"海边\"}"}}]}}`,
			`{"message":{"content":"第 `+strconv.Itoa(i)+` 轮结果：海边的日落真美"}}`)
	}
	fake := newFakeModel(t, script...)
	seedFakeProvider(t, ts, fake.server.URL)

	sid, _ := chatTurn(t, ts, adminToken, 0, "开始")
	for i := 0; i < 5; i++ {
		chatTurn(t, ts, adminToken, sid, "继续 "+strconv.Itoa(i))
	}

	msgs := fake.lastMessages(t)
	for i, m := range msgs {
		if m["role"] != "tool" {
			continue
		}
		if i == 0 {
			t.Fatalf("裁剪后出现孤儿 tool 行（首条）：%+v", m)
		}
		prev := msgs[i-1]
		if prev["role"] != "assistant" || prev["tool_calls"] == nil {
			t.Fatalf("裁剪把配对拆开了（前一条 role=%v）：%+v", prev["role"], m)
		}
	}
}
