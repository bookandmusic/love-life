package api

// 卡片提案链路（服务端三段分离）：工具只给数据 → 模型正文围栏提案 → ValidateSpec 校验 →
// 校验通过才下发 card 事件；不合法带错误码回给模型重写，连续 2 次失败本轮不出卡。

import (
	"strings"
	"testing"
)

// findPostsToolCall 一轮 find_posts 的模型脚本（读工具 → 有结果集 r_1）。
const findPostsToolCall = `{"message":{"content":"","tool_calls":[{"id":"f1","type":"function","function":{"name":"find_posts","arguments":"{\"query\":\"海边\"}"}}]}}`

// seedSeaPosts 造两条「海边」动态。
func seedSeaPosts(t *testing.T, ts *testEnv, adminToken string) {
	t.Helper()
	for _, c := range []string{"海边的日落", "海边捡到的贝壳", "山里的清晨"} {
		res := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{"content": c})
		res.Body.Close()
	}
}

// TestChatCardValidated：合法提案 → card 事件带 validated/view/ref/title/note/data，且在 answer.end 之前。
func TestChatCardValidated(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	seedSeaPosts(t, ts, adminToken)

	fake := newFakeModel(t,
		findPostsToolCall,
		fakeAnswer("海边这两条：\n"+uiFence(`{"ref":"r_1","view":"list","title":"海边的回忆","note":"共 2 条"}`)),
	)
	seedFakeProvider(t, ts, fake.server.URL)

	_, body := chatTurn(t, ts, adminToken, 0, "我们去过海边吗")
	cards := cardsFromSSE(t, body)
	if len(cards) != 1 {
		t.Fatalf("应下发 1 张卡，实得 %d：\n%s", len(cards), body)
	}
	c := cards[0]
	if !c.Validated {
		t.Fatal("下发的卡必须是 validated")
	}
	if c.View != "list" || c.Ref != "r_1" || c.Title != "海边的回忆" || c.Note != "共 2 条" {
		t.Fatalf("卡载荷不对：%+v", c)
	}
	if c.Data == nil || len(c.Data.Records) != 2 {
		t.Fatalf("卡必须带数据（客户端不再自己按 ref 取数）：%+v", c.Data)
	}
	// 卡在 answer.end 之前（客户端先落卡再收尾）
	if strings.Index(body, "event: card") > strings.Index(body, "event: answer.end") {
		t.Fatal("card 应在 answer.end 之前")
	}
}

// TestChatCardRejectedThenRewritten：数据画不了网格 → 服务端拒绝并回灌错误码 → 模型重写为合法卡。
func TestChatCardRejectedThenRewritten(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	seedSeaPosts(t, ts, adminToken)

	fake := newFakeModel(t,
		findPostsToolCall,
		// 第一次乱写：records 数据画不了网格
		fakeAnswer("给你看看：\n"+uiFence(`{"ref":"r_1","view":"grid"}`)),
		// 重写：只给围栏
		fakeAnswer(uiFence(`{"ref":"r_1","view":"list"}`)),
	)
	seedFakeProvider(t, ts, fake.server.URL)

	_, body := chatTurn(t, ts, adminToken, 0, "我们去过海边吗")
	cards := cardsFromSSE(t, body)
	if len(cards) != 1 || cards[0].View != "list" {
		t.Fatalf("重写后应只有一张 list 卡：%+v\n%s", cards, body)
	}
	// 错误码必须真的回给了模型（否则它无从重写）
	var sawCode bool
	for _, m := range fake.lastMessages(t) {
		if s, _ := m["content"].(string); strings.Contains(s, "grid_needs_media") {
			sawCode = true
		}
	}
	if !sawCode {
		t.Fatalf("重写请求里应带错误码 grid_needs_media：%+v", fake.lastMessages(t))
	}
}

// TestChatCardTwoFailuresNoCard：连续两次不合法 → 本轮不出卡（正文照常），并记一条审计。
func TestChatCardTwoFailuresNoCard(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	seedSeaPosts(t, ts, adminToken)

	fake := newFakeModel(t,
		findPostsToolCall,
		fakeAnswer("海边这两条：\n"+uiFence(`{"ref":"r_1","view":"grid"}`)),
		fakeAnswer(uiFence(`{"ref":"r_1","view":"grid"}`)),
	)
	seedFakeProvider(t, ts, fake.server.URL)

	_, body := chatTurn(t, ts, adminToken, 0, "我们去过海边吗")
	if cards := cardsFromSSE(t, body); len(cards) != 0 {
		t.Fatalf("两次校验失败不该出卡：%+v", cards)
	}
	// 正文照常（卡没了，话还在）
	if !strings.Contains(body, "海边这两条") {
		t.Fatalf("正文应照常下发：\n%s", body)
	}
	var n int
	if err := ts.db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action = 'render_card'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("校验失败应记 1 条审计，实得 %d", n)
	}
}

// TestChatCardUnknownRef：ref 编错 → 错误码 unknown_ref + 可用句柄清单回给模型。
func TestChatCardUnknownRef(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	seedSeaPosts(t, ts, adminToken)

	fake := newFakeModel(t,
		findPostsToolCall,
		fakeAnswer("看图：\n"+uiFence(`{"ref":"r_9","view":"list"}`)),
		fakeAnswer(uiFence(`{"ref":"r_1","view":"list"}`)),
	)
	seedFakeProvider(t, ts, fake.server.URL)

	_, body := chatTurn(t, ts, adminToken, 0, "我们去过海边吗")
	if cards := cardsFromSSE(t, body); len(cards) != 1 || cards[0].Ref != "r_1" {
		t.Fatalf("应按可用句柄重写出一张卡：%+v", cards)
	}
	var rewrite string
	for _, m := range fake.lastMessages(t) {
		if s, _ := m["content"].(string); strings.Contains(s, "unknown_ref") {
			rewrite = s
		}
	}
	if !strings.Contains(rewrite, "r_1") {
		t.Fatalf("unknown_ref 文案应列出可用句柄：%q", rewrite)
	}
}

// TestChatCardCrossTurnRef：下一轮直接引用**上一轮**的结果集句柄。
// 模型上下文里一直带着旧 ref（read_result 留在 chat_context），所以「再看一次那两条」这类
// 追问会引用上一轮的 r_1——而本轮没有任何工具结果。服务端要从会话结果集把数据重建出来，
// 否则 unknown_ref 会让本轮不出卡、客户端显示「引用的结果已失效」。
// 这条链路上出过两次同一个病（服务端 hydrateHistoryCards / 客户端会话级卡片索引），故留守卫。
func TestChatCardCrossTurnRef(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	seedSeaPosts(t, ts, adminToken)

	fake := newFakeModel(t,
		findPostsToolCall,
		fakeAnswer("海边这两条：\n"+uiFence(`{"ref":"r_1","view":"list","title":"海边的回忆"}`)),
		// 第二轮不调工具，直接引用第一轮的 r_1
		fakeAnswer("还是这两条：\n"+uiFence(`{"ref":"r_1","view":"list","title":"海边的回忆","note":"同一批"}`)),
	)
	seedFakeProvider(t, ts, fake.server.URL)

	sid, body1 := chatTurn(t, ts, adminToken, 0, "我们去过海边吗")
	if cards := cardsFromSSE(t, body1); len(cards) != 1 {
		t.Fatalf("第一轮应出卡：\n%s", body1)
	}

	_, body2 := chatTurn(t, ts, adminToken, sid, "再看一次那两条")
	if strings.Contains(body2, "unknown_ref") {
		t.Fatalf("跨轮引用不该被判 unknown_ref：\n%s", body2)
	}
	cards := cardsFromSSE(t, body2)
	if len(cards) != 1 {
		t.Fatalf("跨轮引用应出 1 张卡（数据从会话结果集重建），实得 %d 张：\n%s", len(cards), body2)
	}
	if c := cards[0]; c.Ref != "r_1" || c.Data == nil || len(c.Data.Records) != 2 {
		t.Fatalf("跨轮卡的 ref/数据不对：%+v", c)
	}
}

// TestChatCardKpiWithSections：只问一项时，工具按 sections 只回那一项，模型用 kpi 画一个数字；
// 卡事件必须把 only / limit 原样透传（客户端据此裁剪渲染）。
func TestChatCardKpiWithSections(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	seedSeaPosts(t, ts, adminToken)

	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"f1","type":"function","function":{"name":"get_space_stats","arguments":"{\"sections\":[\"posts\"]}"}}]}}`,
		fakeAnswer("一共 3 条动态：\n"+uiFence(`{"ref":"r_1","view":"kpi","title":"动态总数"}`)),
	)
	seedFakeProvider(t, ts, fake.server.URL)

	_, body := chatTurn(t, ts, adminToken, 0, "我们一共记了多少条动态")
	data := toolEndData(t, body)
	if len(data) != 1 {
		t.Fatalf("应有一次工具结果：%+v", data)
	}
	if len(data[0].Stats) != 1 || data[0].Stats[0].Label != "动态" {
		t.Fatalf("sections=[posts] 只该回「动态」一项：%+v", data[0].Stats)
	}
	if len(data[0].Series) != 0 {
		t.Fatalf("没要 distribution 就不该带分布：series=%d", len(data[0].Series))
	}
	cards := cardsFromSSE(t, body)
	if len(cards) != 1 || cards[0].View != "kpi" {
		t.Fatalf("应出 1 张 kpi 卡：%+v", cards)
	}
}

// TestChatCardOnlyAndLimitPassthrough：only / limit 校验通过后要原样下发（客户端按它裁剪）。
func TestChatCardOnlyAndLimitPassthrough(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	seedSeaPosts(t, ts, adminToken)

	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"f1","type":"function","function":{"name":"get_space_stats","arguments":"{}"}}]}}`,
		fakeAnswer("概览：\n"+uiFence(`{"ref":"r_1","view":"stats","only":["媒体"]}`)),
		// 第二轮：列表卡带 limit
		findPostsToolCall,
		// 第二轮 find_posts 的结果集是 r_2（句柄按会话递增），r_1 是第一轮的 stats（无条目）
		fakeAnswer("这几条：\n"+uiFence(`{"ref":"r_2","view":"list","limit":2}`)),
	)
	seedFakeProvider(t, ts, fake.server.URL)

	sid, body1 := chatTurn(t, ts, adminToken, 0, "空间概览")
	cards := cardsFromSSE(t, body1)
	if len(cards) != 1 || cards[0].View != "stats" || len(cards[0].Only) != 1 || cards[0].Only[0] != "媒体" {
		t.Fatalf("only 未透传：%+v", cards)
	}

	_, body2 := chatTurn(t, ts, adminToken, sid, "只看两条")
	cards2 := cardsFromSSE(t, body2)
	if len(cards2) != 1 || cards2[0].View != "list" || cards2[0].Limit != 2 {
		t.Fatalf("limit 未透传：%+v", cards2)
	}
}

// TestChatCardPhotosKpi：photos 口径只数照片（不含视频与录音）。
// 覆盖 want("photos") 分支（回执只有「照片 N 张」）：模型用 kpi 画一个大数字。
func TestChatCardPhotosKpi(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	for _, name := range []string{"sea1.jpg", "sea2.jpg"} {
		tok, code := uploadMedia(t, ts.URL, adminToken, name, genJPEG(t))
		if code != 200 {
			t.Fatalf("upload %s: %d", name, code)
		}
		res := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{
			"content": "海边 " + name, "media_tokens": []string{tok},
		})
		res.Body.Close()
	}

	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"f1","type":"function","function":{"name":"get_space_stats","arguments":"{\"sections\":[\"photos\"]}"}}]}}`,
		fakeAnswer("你们一共拍了 2 张照片：\n"+uiFence(`{"ref":"r_1","view":"kpi","title":"照片"}`)),
	)
	seedFakeProvider(t, ts, fake.server.URL)

	_, body := chatTurn(t, ts, adminToken, 0, "我们一共拍了多少张照片")
	data := toolEndData(t, body)
	if len(data) != 1 {
		t.Fatalf("应有一次工具结果：%+v", data)
	}
	if len(data[0].Stats) != 1 || data[0].Stats[0].Label != "照片" || data[0].Stats[0].Value != "2 张" {
		t.Fatalf("sections=[photos] 只该回「照片 2 张」：%+v", data[0].Stats)
	}
	if len(data[0].Series) != 0 {
		t.Fatalf("没要 distribution 就不该带分布：series=%d", len(data[0].Series))
	}
	if hint := data[0].Stats[0].Hint; !strings.Contains(hint, "不含视频") {
		t.Fatalf("照片口径的提示应说明不含视频与录音，实得 %q", hint)
	}
	cards := cardsFromSSE(t, body)
	if len(cards) != 1 || cards[0].View != "kpi" {
		t.Fatalf("应出 1 张 kpi 卡：%+v", cards)
	}
	if c := cards[0]; c.Data == nil || len(c.Data.Stats) != 1 || c.Data.Stats[0].Label != "照片" {
		t.Fatalf("卡上的数据只该含照片一项：%+v", c.Data)
	}
}
