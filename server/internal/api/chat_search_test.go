package api

// 读工具结果卡片（新协议）：结果不再走 text/result 事件，而是 tool.start/tool.end
// 的语义事件——tool.end.ui 携带结构化 UI 块（可勾选引用），tool.end.text 是一句人话。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/chat"
)

// TestChatSearchResultEvent：find_posts 的 UI 块经 tool.end 下发（含条件、条目、句柄）。
func TestChatSearchResultEvent(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	for _, c := range []string{"海边的日落真美", "海边捡到的贝壳", "山里的清晨"} {
		res := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{"content": c})
		res.Body.Close()
	}

	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"s1","type":"function","function":{"name":"find_posts","arguments":"{\"query\":\"海边\"}"}}]}}`,
		`{"message":{"content":"找到了这些"}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]string{"content": "我们去过海边吗"})
	body := sseBody(t, res)
	if !strings.Contains(body, "event: tool.start") {
		t.Fatalf("缺少 tool.start 事件: %s", body)
	}
	uis := toolEndUI(t, body)
	if len(uis) != 1 {
		t.Fatalf("tool.end ui 块 = %d, want 1\n%s", len(uis), body)
	}
	ui := uis[0]
	if ui.Kind != "record_list" {
		t.Fatalf("UI kind = %q, want record_list", ui.Kind)
	}
	if len(ui.Records) != 2 {
		t.Fatalf("records = %d, want 2（命中「海边」的两条）", len(ui.Records))
	}
	for _, r := range ui.Records {
		if r.ID == 0 || r.Kind != "post" {
			t.Fatalf("record 形状不对: %+v", r)
		}
	}
	if !ui.Selectable {
		t.Fatal("记录列表应可勾选引用（D9）")
	}
	if ui.ResultID == "" {
		t.Fatal("UI 块必须带结果集句柄（D10）")
	}
	// 结果卡片在 turn.end 之前（客户端先落卡片再收尾）
	if strings.Index(body, "event: tool.end") > strings.Index(body, "event: turn.end") {
		t.Fatal("tool.end 应在 turn.end 之前")
	}
	// 人话文案经 tool.end.text 下发；工具名与原始 JSON 不得进对话流
	if !strings.Contains(body, "找到 2 条") {
		t.Fatalf("缺少「找到 N 条」文案:\n%s", body)
	}
	if strings.Contains(body, "find_posts →") || strings.Contains(body, "🔎") {
		t.Fatalf("不得把工具名/带内信令推给用户:\n%s", body)
	}
	// 模型上下文拿到的是 ModelText 摘要（含句柄），而非原始 JSON
	msgs := fake.lastMessages(t)
	var sawModelText bool
	for _, m := range msgs {
		if m["role"] == "tool" {
			if s, _ := m["content"].(string); strings.Contains(s, "共 2 条") && strings.Contains(s, "结果集") {
				sawModelText = true
			}
		}
	}
	if !sawModelText {
		t.Fatalf("读工具产出应作为 ModelText 回填模型（含条数与句柄）: %+v", msgs)
	}
}

// 空结果：UI 块照常下发（records=[]），无句柄；模型拿到「没有找到」。
func TestChatSearchResultEmpty(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{"content": "山里的清晨"})
	res.Body.Close()

	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"s1","type":"function","function":{"name":"find_posts","arguments":"{\"query\":\"不存在的关键词\"}"}}]}}`,
		`{"message":{"content":"没找到"}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]string{"content": "查一下不存在的关键词"})
	body := sseBody(t, res)
	uis := toolEndUI(t, body)
	if len(uis) != 1 {
		t.Fatalf("空结果也应下发 UI 块: %s", body)
	}
	if len(uis[0].Records) != 0 {
		t.Fatalf("空结果 records 应为空: %+v", uis[0].Records)
	}
	if !strings.Contains(body, "没找到") {
		t.Fatalf("空结果文案缺失:\n%s", body)
	}
}

// 带图动态：UI 条目带签名缩略图。
func TestChatSearchResultThumbURL(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	mediaToken, code := uploadMedia(t, ts.URL, adminToken, "sea.jpg", genJPEG(t))
	if code != http.StatusOK {
		t.Fatalf("upload: %d", code)
	}
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{
		"content": "海边的日落真美", "media_tokens": []string{mediaToken},
	})
	res.Body.Close()

	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"s1","type":"function","function":{"name":"find_posts","arguments":"{\"query\":\"海边\"}"}}]}}`,
		`{"message":{"content":"找到了"}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]string{"content": "我们去过海边吗"})
	body := sseBody(t, res)
	uis := toolEndUI(t, body)
	if len(uis) != 1 || len(uis[0].Records) != 1 {
		t.Fatalf("UI 块缺失或条数不对: %s", body)
	}
	thumb := uis[0].Records[0].ThumbURL
	if thumb == "" {
		t.Fatal("带媒体动态应有 thumb_url")
	}
	if !strings.Contains(thumb, "sig=") || !strings.Contains(thumb, "exp=") {
		t.Fatalf("thumb_url 应含短时签名参数: %q", thumb)
	}
}

// 空 query = 浏览模式：按时间倒序返回全部动态（不限关键词）。
func TestChatSearchBrowseEmptyQuery(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	for _, c := range []string{"第一条", "第二条", "第三条"} {
		res := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{"content": c})
		res.Body.Close()
	}
	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"s1","type":"function","function":{"name":"find_posts","arguments":"{}"}}]}}`,
		`{"message":{"content":"以下是最近的动态"}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]string{"content": "展示最近的动态"})
	body := sseBody(t, res)
	uis := toolEndUI(t, body)
	if len(uis) != 1 || len(uis[0].Records) != 3 {
		t.Fatalf("浏览模式应返回全部 3 条: %s", body)
	}
}

// 提案纯媒体（对齐 REST/）：content 空 + 用户带附件 → 出卡且附件挂在提案上。
func TestChatCreatePostPureMedia(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"propose_post","arguments":"{\"content\":\"\"}"}}]}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]any{
		"content": "帮我发一条纯照片动态",
		"media":   []map[string]any{{"name": "p.jpg", "type": "image/jpeg", "size": 1}},
	})
	body := sseBody(t, res)
	props := proposalsFromSSE(t, body)
	if len(props) != 1 {
		t.Fatalf("应出 1 张提案卡:\n%s", body)
	}
	if props[0].Preview.Kind != "post" {
		t.Fatalf("preview kind = %q", props[0].Preview.Kind)
	}
	if len(props[0].Attachments) != 1 || props[0].Attachments[0].Name != "p.jpg" {
		t.Fatalf("附件清单应在提案上（展示用元数据）: %+v", props[0].Attachments)
	}
	actionID := props[0].ID

	mediaToken, code := uploadMedia(t, ts.URL, adminToken, "pure.jpg", genJPEG(t))
	if code != http.StatusOK {
		t.Fatalf("upload: %d", code)
	}
	res = doJSON(t, "POST", ts.URL+fmt.Sprintf("/api/v1/chat/proposals/%d/resolve", actionID), adminToken, map[string]any{
		"decision": "approve", "media_tokens": []string{mediaToken},
	})
	var out struct {
		Status string `json:"status"`
		Record struct {
			Kind string `json:"kind"`
			ID   int64  `json:"id"`
		} `json:"record"`
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || out.Status != "executed" || out.Record.Kind != "post" {
		t.Fatalf("pure-media resolve: %d %+v", res.StatusCode, out)
	}
	var n int
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM posts WHERE deleted_at IS NULL AND content = ''`).Scan(&n)
	if n != 1 {
		t.Fatalf("pure media post should exist with empty content, count=%d", n)
	}
}

// 提案全空（无正文/媒体/心情/地点）必须**不出卡**：校验前置后模型当场拿到失败原因并可重试。
func TestChatCreatePostAllEmptyRejected(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"propose_post","arguments":"{}"}}]}}`,
		`{"message":{"content":"请告诉我这条动态要写什么内容"}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]string{"content": "发动态"})
	body := sseBody(t, res)
	if strings.Contains(body, "event: proposal") {
		t.Fatalf("全空动态不得生成提案:\n%s", body)
	}
	// 模型必须在当轮就看到校验失败的原因，才有机会修正
	msgs := fake.lastMessages(t)
	var sawErr bool
	for _, m := range msgs {
		if m["role"] == "tool" && strings.Contains(fmt.Sprint(m["content"]), "至少填一项") {
			sawErr = true
		}
	}
	if !sawErr {
		t.Fatalf("校验失败原因未回填给模型: %+v", msgs)
	}
	var n int
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM chat_messages WHERE card_id IS NOT NULL`).Scan(&n)
	if n != 0 {
		t.Fatalf("card carrier rows = %d, want 0", n)
	}
}

// 一轮附件只归首个提案：同轮两张卡若都绑同一批 token，确认两次会重复转正。
func TestChatTurnAttachmentsClaimedOnce(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[`+
			`{"id":"p1","type":"function","function":{"name":"propose_post","arguments":"{\"content\":\"第一条\"}"}},`+
			`{"id":"p2","type":"function","function":{"name":"propose_post","arguments":"{\"content\":\"第二条\"}"}}]}}`)
	seedFakeProvider(t, ts, fake.server.URL)

	res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]any{
		"content": "发两条动态",
		"media":   []map[string]any{{"name": "a.jpg", "type": "image/jpeg", "size": 1}},
	})
	body := sseBody(t, res)
	props := proposalsFromSSE(t, body)
	// 一轮只出一张卡（AI模式设计「预算与并行」：写工具串行，一旦产出提案即停止本轮）——
	// 模型即便一次回了两个提案调用，也只第一张出卡，故「附件只归首张」由截断结构性保证。
	if len(props) != 1 {
		t.Fatalf("一轮只应出一张提案，实得 %d（body:\n%s）", len(props), body)
	}
	if len(props[0].Attachments) != 1 {
		t.Fatalf("首个提案应带走本轮附件，实得 %d", len(props[0].Attachments))
	}
	// 未出卡的那个调用不应留下 pending 卡
	var pending int
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM confirm_actions WHERE status = 'pending'`).Scan(&pending)
	if pending != 1 {
		t.Fatalf("pending 卡应为 1，实得 %d", pending)
	}
}

// 预算耗尽必收尾：轮次用尽时必须补一次收尾回答（不静默结束）。
// 轮次上限见 chat 包的 maxToolRounds（文档口径 8）——这里让假上游每轮都调工具把它耗光。
func TestChatBudgetExhaustionStillAnswers(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	for _, c := range []string{"海边的日落", "山里的清晨"} {
		res := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{"content": c})
		res.Body.Close()
	}
	// maxToolRounds 轮「只调工具不出正文」+ 1 条收尾回答
	responses := make([]string, 0, chat.MaxToolRounds+1)
	for i := 0; i < chat.MaxToolRounds; i++ {
		responses = append(responses, fmt.Sprintf(
			`{"message":{"content":"","tool_calls":[{"id":"s%d","type":"function","function":{"name":"find_posts","arguments":"{\"query\":\"海边\"}"}}]}}`, i+1))
	}
	responses = append(responses, `{"message":{"content":"查到 2 条海边的回忆，但工具次数用完了，先给你这些"}}`)
	fake := newFakeModel(t, responses...)
	seedFakeProvider(t, ts, fake.server.URL)

	res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]string{"content": "帮我理一下海边的回忆"})
	body := sseBody(t, res)
	if !strings.Contains(body, "工具次数用完") {
		t.Fatalf("预算耗尽未给收尾回答（静默结束）:\n%s", body)
	}
	if !strings.Contains(body, `"stop_reason":"budget"`) {
		t.Fatalf("turn.end 应标 budget:\n%s", body)
	}
	var n int
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM chat_messages WHERE role = 'assistant' AND parts LIKE '%工具次数用完%'`).Scan(&n)
	if n != 1 {
		t.Fatalf("收尾回答未落库，rows = %d", n)
	}
}
