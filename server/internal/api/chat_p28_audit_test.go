package api

// 批量删除的审计摘要口径（AI模式设计「批量删除契约」· 通道与审计）：
// params 摘要记「kind + 条数 + 前 3 个 id」——通用格式化会把 ids 数组按字母序截断，
// 后台看不出删了几条。
//
// 关键路径：句柄在出卡时展开成具体 ids 写进 params_json，resolve 时的审计用的正是它。

import (
	"strings"
	"testing"
)

func TestDeleteAuditSummaryShape(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	for _, name := range []string{"杭州之旅", "春天", "厦门"} {
		res := doJSON(t, "POST", ts.URL+"/api/v1/albums", adminToken, map[string]any{"name": name})
		res.Body.Close()
	}

	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"a1","type":"function","function":{"name":"find_albums","arguments":"{}"}}]}}`,
		`{"message":{"content":"你的相册"}}`,
		`{"message":{"content":"","tool_calls":[{"id":"d1","type":"function","function":{"name":"propose_delete","arguments":"{\"kind\":\"album\",\"source\":{\"result_id\":\"r_1\"}}"}}]}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	sid, body := chatTurn(t, ts, adminToken, 0, "我有哪些相册")
	if h := assertHandle(t, body, "record_list", "find_albums"); h != "r_1" {
		t.Fatalf("句柄 = %q，本用例按 r_1 脚本化", h)
	}
	_, body2 := chatTurn(t, ts, adminToken, sid, "把这些相册都删掉")
	actionID := cardFromSSE(t, body2)
	if actionID == 0 {
		t.Fatal("未取到确认卡")
	}
	if st := confirmCard(t, ts, adminToken, actionID, true); st != "executed" {
		t.Fatalf("确认执行状态 = %q", st)
	}

	// 审计有两条（出卡 pending + 裁决 resolve），**两条都要合口径**：
	// 出卡那条原先只记到句柄名（source: r_1），看不出删几条。
	rows, err := ts.db.Query(`SELECT result, params FROM audit_logs
		WHERE action = 'propose_delete' ORDER BY id ASC`)
	if err != nil {
		t.Fatalf("读取审计失败：%v", err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var result, params string
		if err := rows.Scan(&result, &params); err != nil {
			t.Fatal(err)
		}
		got[result] = params
	}
	if len(got) != 2 {
		t.Fatalf("审计应两条（pending + resolve），实得 %d 条：%v", len(got), got)
	}
	for _, result := range []string{"pending", "success"} {
		params, ok := got[result]
		if !ok {
			t.Fatalf("缺少 result=%s 的审计，实得 %v", result, got)
		}
		if !strings.Contains(params, "kind: album") {
			t.Fatalf("%s 摘要应含 kind，实得 %q", result, params)
		}
		if !strings.Contains(params, "条数: 3") {
			t.Fatalf("%s 摘要应含条数（句柄展开后的真实条数），实得 %q", result, params)
		}
		if !strings.Contains(params, "ids: [") {
			t.Fatalf("%s 摘要应含前 3 个 id，实得 %q", result, params)
		}
		if strings.Contains(params, "source:") {
			t.Fatalf("%s 摘要不应只剩句柄名（句柄应已展开），实得 %q", result, params)
		}
	}
}
