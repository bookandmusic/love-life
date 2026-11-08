package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// P2-24：历史回显的 card.intent 必须**从 preview_json 读出**，不得反推自 tool_name 列。
// 构造一条两者不同的行：正常写入下两者本就一致，只有构造差异才能分辨读的是哪一个
// （intent / summary / preview / warnings / attachments 全部从 preview_json 读出）。
func TestHistoryCardIntentComesFromPreviewJSON(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"p1","type":"function","function":{"name":"propose_post","arguments":"{\"content\":\"历史回显 intent 验证\"}"}}]}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	sid, body := chatTurn(t, ts, adminToken, 0, "帮我发一条动态")
	actionID := cardFromSSE(t, body)
	if actionID == 0 {
		t.Fatalf("未取到确认卡：\n%s", body)
	}

	// 把 preview_json 里的 intent 改成与 tool_name 不同的值（模拟「展示契约在预览里」）
	var preview string
	if err := ts.db.QueryRow(`SELECT preview_json FROM confirm_actions WHERE id = ?`, actionID).Scan(&preview); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(preview), &doc); err != nil {
		t.Fatalf("preview_json 解析失败：%s", preview)
	}
	if doc["intent"] == nil {
		t.Fatalf("preview_json 应含 intent：%s", preview)
	}
	doc["intent"] = "preview_only_intent"
	mutated, _ := json.Marshal(doc)
	if _, err := ts.db.Exec(`UPDATE confirm_actions SET preview_json = ? WHERE id = ?`, string(mutated), actionID); err != nil {
		t.Fatal(err)
	}
	// 同时把列值改成第三个数，确保断言只能命中 preview_json 那一个
	if _, err := ts.db.Exec(`UPDATE confirm_actions SET tool_name = 'column_tool_name' WHERE id = ?`, actionID); err != nil {
		t.Fatal(err)
	}

	res := doJSON(t, "GET", ts.URL+fmt.Sprintf("/api/v1/chat/history?session_id=%d", sid), adminToken, nil)
	// 快照挂在 card part 上（历史回显与流式同一形状，消息顶层不带 card 字段）
	var hist struct {
		Items []struct {
			Parts []struct {
				Type string `json:"type"`
				Card *struct {
					Intent string `json:"intent"`
				} `json:"card"`
			} `json:"parts"`
		} `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&hist)
	res.Body.Close()

	var got string
	for _, it := range hist.Items {
		for _, pt := range it.Parts {
			if pt.Type == "card" && pt.Card != nil && pt.Card.Intent != "" {
				got = pt.Card.Intent
			}
		}
	}
	if got != "preview_only_intent" {
		t.Fatalf("card.intent = %q，want preview_only_intent（须从 preview_json 读，不得用 tool_name 列）", got)
	}
	if strings.Contains(got, "column_tool_name") {
		t.Fatalf("card.intent 反推自 tool_name 列：%q", got)
	}
}

// P2-27：resolve 路径的 media_tokens 也受「单条最多 9 个媒体」约束。
// 正常链路受 MaxChatMedia 间接约束（发消息时校验元数据数量），但 resolve 可被直接调用，
// 10 个 token 必须在入口被拒，而不是一路走到工具的转正逻辑。
func TestResolveProposalRejectsTooManyMediaTokens(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"p1","type":"function","function":{"name":"propose_post","arguments":"{\"content\":\"附件上限验证\"}"}}]}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)
	_, body := chatTurn(t, ts, adminToken, 0, "帮我发一条带图的动态")
	actionID := cardFromSSE(t, body)
	if actionID == 0 {
		t.Fatalf("未取到确认卡：\n%s", body)
	}

	tokens := make([]string, 10)
	for i := range tokens {
		tokens[i] = "tok-" + itoa(int64(i))
	}
	res := doJSON(t, "POST", ts.URL+fmt.Sprintf("/api/v1/chat/proposals/%d/resolve", actionID), adminToken, map[string]any{
		"decision": "approve", "media_tokens": tokens,
	})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("提交 10 个 media_token 应 400，实得 %d", res.StatusCode)
	}
	// 更关键的一半：入参被拒时**不得消耗卡片**。认领是消费性的（pending→confirmed），
	// 若先认领再校验，卡片会被吃掉却从未执行，而历史回显按 confirmed→executed 归一，
	// 用户会看到「已执行」——这种形态必须排除。
	var status string
	if err := ts.db.QueryRow(`SELECT status FROM confirm_actions WHERE id = ?`, actionID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Fatalf("入参被拒后卡片状态 = %q，want pending（不得被消耗）", status)
	}
	// 且仍可正常确认（证明卡片确实没被吃掉）
	if st := confirmCard(t, ts, adminToken, actionID, true); st != "executed" {
		t.Fatalf("被拒后卡片仍应可确认，实得 %q", st)
	}
}

// 提案轮的两条历史契约（一次跑齐，它们同源：同一个 parts 数组）：
//  1. 模型「先说话再调工具」时那段话要落库——否则刷新后整段消失；
//  2. 待确认卡快照要挂在该行的 card part 上——否则切走再回来确认卡变空块。
func TestHistoryKeepsPreToolTextAndCardSnapshot(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	fake := newFakeModel(t,
		// 同一条响应里既有正文又有工具调用（提案轮最常见：说完结论就出卡）
		`{"message":{"content":"我先把这条动态拟好，等你点头。","tool_calls":[{"id":"p1","type":"function","function":{"name":"propose_post","arguments":"{\"content\":\"历史回显验证\"}"}}]}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	sid, body := chatTurn(t, ts, adminToken, 0, "帮我发一条动态")
	if cardFromSSE(t, body) == 0 {
		t.Fatalf("未取到确认卡：\n%s", body)
	}

	res := doJSON(t, "GET", ts.URL+fmt.Sprintf("/api/v1/chat/history?session_id=%d", sid), adminToken, nil)
	defer res.Body.Close()
	var hist struct {
		Items []struct {
			Role  string `json:"role"`
			Parts []struct {
				Type string `json:"type"`
				Text string `json:"text"`
				Card *struct {
					ActionID int64  `json:"action_id"`
					Status   string `json:"status"`
					Intent   string `json:"intent"`
				} `json:"card"`
			} `json:"parts"`
		} `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&hist)

	var sawText, sawCard bool
	for _, it := range hist.Items {
		for _, p := range it.Parts {
			if p.Type == "text" && strings.Contains(p.Text, "等你点头") {
				sawText = true
			}
			if p.Type == "card" && p.Card != nil && p.Card.Status == "pending" && p.Card.Intent == "propose_post" {
				sawCard = true
			}
		}
	}
	if !sawText {
		t.Error("模型先说的话没落库（刷新后会消失）")
	}
	if !sawCard {
		t.Error("确认卡快照没写进 card part（切走再回来卡会消失）")
	}
}
