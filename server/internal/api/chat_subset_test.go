package api

// 点选优先的硬不变量（AI模式设计「结果集句柄与点选优先」）：
//   - 正例：用勾选子集句柄 r_M 出卡 → 范围就是勾的那几条；
//   - 反例（本文件）：模型拿**父全集句柄**引用（或直给未勾选 id）时，目标 id 集必须
//     ⊆ 勾选并集，超出即校验前置驳回、不出卡，并把「请引用 r_M」回填给模型。
//
// 父句柄按设计保留不动（用户可回头引用更早的结果集），所以「模型随时能拿父全集
// 覆盖勾选范围」是真实可达路径——这道护栏是点选优先的唯一硬约束。

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// seedPostsWithHandle 造 3 条动态并让模型查一次拿到父句柄。
// 返回 (会话 id, 父句柄 r_N, 三条动态 id, 假上游)。
// 假上游只 seed 一次（链路按 chain_order 走，重复 seed 会落到链尾而不生效），
// 后续场景通过改写 script 复用同一个 fake。
func seedPostsWithHandle(t *testing.T, ts *testEnv, token string) (int64, string, []int64, *fakeModel) {
	t.Helper()
	var ids []int64
	for i := 0; i < 3; i++ {
		res := doJSON(t, "POST", ts.URL+"/api/v1/posts", token, map[string]any{
			"content": fmt.Sprintf("海边的回忆 %d", i),
		})
		var pc struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(res.Body).Decode(&pc)
		res.Body.Close()
		ids = append(ids, pc.ID)
	}
	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"f1","type":"function","function":{"name":"find_posts","arguments":"{\"query\":\"海边\"}"}}]}}`,
		`{"message":{"content":"找到 3 条"}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)
	sid, body := chatTurn(t, ts, token, 0, "找找海边的回忆")
	uis := toolEndUI(t, body)
	if len(uis) != 1 || uis[0].ResultID == "" || len(uis[0].Records) != 3 {
		t.Fatalf("前置：tool.end ui = %+v", uis)
	}
	return sid, uis[0].ResultID, ids, fake
}

// setScript 改写假上游脚本（同一 fake 内切换后续响应）。
// 计数一并归零：fakeModel 用 calls 递增取 script[i]，只换脚本不清计数会一直取到最后一条。
func setScript(f *fakeModel, script ...string) {
	f.mu.Lock()
	f.script = script
	f.mu.Unlock()
	f.calls.Store(0)
}

// TestChatQuotedSubsetRejectsParentHandle：只勾 1 条，却用父全集句柄提删除 → 必须驳回不出卡。
func TestChatQuotedSubsetRejectsParentHandle(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	sid, parentHandle, ids, fake := seedPostsWithHandle(t, ts, adminToken)
	picked := ids[0]

	// 模型这次用**父句柄**（而非勾选子集）提批量删除——正是要防的越界路径
	setScript(fake,
		fmt.Sprintf(`{"message":{"content":"","tool_calls":[{"id":"d1","type":"function","function":{"name":"propose_delete","arguments":"{\"kind\":\"post\",\"source\":{\"result_id\":\"%s\"}}"}}]}}`, parentHandle),
		`{"message":{"content":"明白，我只按你勾选的那条来"}}`,
	)

	res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]any{
		"content":    "删掉我勾的这条",
		"session_id": sid,
		"quoted":     []map[string]any{{"result_id": parentHandle, "ids": []int64{picked}}},
	})
	body := sseBody(t, res)

	if strings.Contains(body, "event: proposal") {
		t.Fatalf("越界引用必须驳回、不出卡，实得：\n%s", body)
	}
	if !strings.Contains(body, `"status":"failed"`) {
		t.Fatalf("越界引用应以工具失败回填：\n%s", body)
	}
	if !strings.Contains(body, "超出用户勾选") || !strings.Contains(body, "请引用") {
		t.Fatalf("回填文案应说明超出勾选并指引 r_M：\n%s", body)
	}
	var n int
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM confirm_actions WHERE status = 'pending'`).Scan(&n)
	if n != 0 {
		t.Fatalf("越界引用产生了 pending 卡 %d 张", n)
	}
	var alive int
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM posts WHERE deleted_at IS NULL`).Scan(&alive)
	if alive != 3 {
		t.Fatalf("动态被误删：存活 %d，期望 3", alive)
	}
}

// TestChatQuotedSubsetAllowsPickedOnly：勾 1 条并用**子集句柄**提删除 → 正常出卡且只含那 1 条。
func TestChatQuotedSubsetAllowsPickedOnly(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	sid, parentHandle, ids, fake := seedPostsWithHandle(t, ts, adminToken)
	picked := ids[0]

	// 第一条消息：勾选并落子集句柄（从模型实际收到的上下文里取 r_M）
	setScript(fake, `{"message":{"content":"好的"}}`)
	res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]any{
		"content":    "就用我勾的这条",
		"session_id": sid,
		"quoted":     []map[string]any{{"result_id": parentHandle, "ids": []int64{picked}}},
	})
	sseBody(t, res)

	sub := subHandleFromContext(t, fake, parentHandle)
	if sub == "" {
		t.Fatal("未拿到勾选子集句柄 r_M")
	}

	// 第二条消息：用子集句柄提删除 → 出卡，且范围恰好 1 条
	setScript(fake,
		fmt.Sprintf(`{"message":{"content":"","tool_calls":[{"id":"d1","type":"function","function":{"name":"propose_delete","arguments":"{\"kind\":\"post\",\"source\":{\"result_id\":\"%s\"}}"}}]}}`, sub),
	)
	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]any{
		"content": "删掉它", "session_id": sid,
	})
	body := sseBody(t, res)
	actionID := cardFromSSE(t, body)

	var preview string
	if err := ts.db.QueryRow(`SELECT preview_json FROM confirm_actions WHERE id=?`, actionID).Scan(&preview); err != nil {
		t.Fatal(err)
	}
	// 预览是「删除单条」形态（count=1）——批量预览才带 items；且必须提到勾选的那条正文
	if !strings.Contains(preview, `"count":1`) {
		t.Fatalf("预览条数应为 1：%s", preview)
	}
	if !strings.Contains(preview, fmt.Sprintf("海边的回忆 %d", 0)) {
		t.Fatalf("预览应是勾选的那条（%d）：%s", picked, preview)
	}
	// 执行参数必须恰好只含勾选的 id（其余两条不进 params）
	var params string
	if err := ts.db.QueryRow(`SELECT params_json FROM confirm_actions WHERE id=?`, actionID).Scan(&params); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		has := strings.Contains(params, fmt.Sprintf(`%d`, id))
		if id == picked && !has {
			t.Fatalf("params 缺勾选 id %d：%s", id, params)
		}
		if id != picked && has {
			t.Fatalf("params 混入未勾选 id %d：%s", id, params)
		}
	}
}

// TestChatQuotedSubsetRejectsDirectIDs：绕开句柄、直给 id 列也照样受勾选并集约束。
func TestChatQuotedSubsetRejectsDirectIDs(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	sid, parentHandle, ids, fake := seedPostsWithHandle(t, ts, adminToken)
	picked := ids[0]

	setScript(fake,
		fmt.Sprintf(`{"message":{"content":"","tool_calls":[{"id":"d1","type":"function","function":{"name":"propose_delete","arguments":"{\"kind\":\"post\",\"ids\":[%d,%d]}"}}]}}`, picked, ids[1]),
		`{"message":{"content":"只删你勾的那条"}}`,
	)
	res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]any{
		"content":    "删掉勾选的",
		"session_id": sid,
		"quoted":     []map[string]any{{"result_id": parentHandle, "ids": []int64{picked}}},
	})
	body := sseBody(t, res)

	if strings.Contains(body, "event: proposal") {
		t.Fatalf("直给未勾选 id 必须驳回、不出卡：\n%s", body)
	}
	if !strings.Contains(body, "超出用户勾选") {
		t.Fatalf("应回填超出勾选的说明：\n%s", body)
	}
}

// TestChatNoQuoteNoSubsetRestriction：没有勾选时不受约束（模型按查询结果正常出卡）。
func TestChatNoQuoteNoSubsetRestriction(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	sid, parentHandle, _, fake := seedPostsWithHandle(t, ts, adminToken)

	setScript(fake,
		fmt.Sprintf(`{"message":{"content":"","tool_calls":[{"id":"d1","type":"function","function":{"name":"propose_delete","arguments":"{\"kind\":\"post\",\"source\":{\"result_id\":\"%s\"}}"}}]}}`, parentHandle),
		`{"message":{"content":"好"}}`,
	)
	res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]any{
		"content": "把海边的都删了", "session_id": sid,
	})
	body := sseBody(t, res)
	if !strings.Contains(body, "event: proposal") {
		t.Fatalf("无勾选时不应拦截提案：\n%s", body)
	}
	if !strings.Contains(body, `"count":3`) {
		t.Fatalf("无勾选时应按查询结果出 3 条：\n%s", body)
	}
}

// subHandleFromContext 从模型收到的 user 消息里取勾选子集句柄（≠父句柄的那个 r_N）。
// 上下文文案形如「…；引用句柄 r_2；父集合 r_1 仍可引用」——只认「引用句柄」紧随的那个。
func subHandleFromContext(t *testing.T, f *fakeModel, parent string) string {
	t.Helper()
	for _, m := range f.lastMessages(t) {
		if m["role"] != "user" {
			continue
		}
		c, _ := m["content"].(string)
		const marker = "引用句柄"
		for idx := strings.Index(c, marker); idx >= 0; idx = strings.Index(c[idx+len(marker):], marker) {
			rest := strings.TrimLeft(c[idx+len(marker):], " ：:")
			fields := strings.FieldsFunc(rest, func(r rune) bool {
				return r == '；' || r == ' ' || r == '，' || r == '。' || r == '）'
			})
			if len(fields) == 0 {
				continue
			}
			tok := strings.TrimRight(fields[0], "）；。，")
			if strings.HasPrefix(tok, "r_") && tok != parent {
				return tok
			}
		}
	}
	return ""
}

// TestChatQuotedSubsetCoversCommentAndReaction：勾选存在时，评论与点赞也只能作用于勾选那条
// （AI模式设计「结果集句柄与点选优先」不变量一节的覆盖清单）。
func TestChatQuotedSubsetCoversCommentAndReaction(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	sid, parentHandle, ids, fake := seedPostsWithHandle(t, ts, adminToken)
	picked := ids[0]
	other := ids[1]

	// 评论未勾选的那条 → 驳回
	setScript(fake,
		fmt.Sprintf(`{"message":{"content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"propose_comment","arguments":"{\"post_id\":%d,\"content\":\"写错了对象\"}"}}]}}`, other),
		`{"message":{"content":"只评论你勾的那条"}}`,
	)
	res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]any{
		"content":    "评论一下",
		"session_id": sid,
		"quoted":     []map[string]any{{"result_id": parentHandle, "ids": []int64{picked}}},
	})
	body := sseBody(t, res)
	if strings.Contains(body, "event: proposal") {
		t.Fatalf("评论未勾选动态必须驳回：\n%s", body)
	}
	if !strings.Contains(body, "超出用户勾选") {
		t.Fatalf("应回填超出勾选：\n%s", body)
	}

	// 点赞未勾选的那条 → 驳回
	setScript(fake,
		fmt.Sprintf(`{"message":{"content":"","tool_calls":[{"id":"r1","type":"function","function":{"name":"propose_reaction","arguments":"{\"post_id\":%d,\"like\":true}"}}]}}`, other),
		`{"message":{"content":"只点赞你勾的那条"}}`,
	)
	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]any{
		"content":    "点个赞",
		"session_id": sid,
		"quoted":     []map[string]any{{"result_id": parentHandle, "ids": []int64{picked}}},
	})
	body = sseBody(t, res)
	if strings.Contains(body, "event: proposal") {
		t.Fatalf("点赞未勾选动态必须驳回：\n%s", body)
	}

	// 评论勾选的那条 → 正常出卡
	setScript(fake,
		fmt.Sprintf(`{"message":{"content":"","tool_calls":[{"id":"c2","type":"function","function":{"name":"propose_comment","arguments":"{\"post_id\":%d,\"content\":\"这条真好\"}"}}]}}`, picked),
	)
	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]any{
		"content":    "评论勾的这条",
		"session_id": sid,
		"quoted":     []map[string]any{{"result_id": parentHandle, "ids": []int64{picked}}},
	})
	body = sseBody(t, res)
	if !strings.Contains(body, "event: proposal") {
		t.Fatalf("评论勾选的那条应正常出卡：\n%s", body)
	}
}

// TestChatQuotedSubsetRejectsWrongKind：勾照片却提删动态 → 按 kind 分组比对，必须驳回
// 并给出类型引导（不映射成「删动态」，AI模式设计「kind 不一致一律驳回，不做映射」）。
func TestChatQuotedSubsetRejectsWrongKind(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	var mediaIDs []int64
	for i := 0; i < 2; i++ {
		tok, _ := uploadMedia(t, ts.URL, adminToken, fmt.Sprintf("m%d.jpg", i), genJPEG(t))
		res := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{
			"content": fmt.Sprintf("带图%d", i), "media_tokens": []string{tok},
		})
		var pc struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(res.Body).Decode(&pc)
		res.Body.Close()
		var id int64
		if err := ts.db.QueryRow(`SELECT id FROM media WHERE post_id=?`, pc.ID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		mediaIDs = append(mediaIDs, id)
	}

	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"f1","type":"function","function":{"name":"find_media","arguments":"{\"unfiled\":true}"}}]}}`,
		`{"message":{"content":"找到照片"}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)
	sid, body := chatTurn(t, ts, adminToken, 0, "找找照片")
	uis := toolEndUI(t, body)
	if len(uis) == 0 || uis[0].ResultID == "" {
		t.Fatalf("前置：未拿到 media 句柄 %+v", uis)
	}

	// 勾的是照片，模型却提删动态 → 目标 id 不在勾选并集（且 kind 不匹配）
	setScript(fake,
		fmt.Sprintf(`{"message":{"content":"","tool_calls":[{"id":"d1","type":"function","function":{"name":"propose_delete","arguments":"{\"kind\":\"post\",\"ids\":[%d]}"}}]}}`, 1),
		`{"message":{"content":"删除照片要先删所属动态"}}`,
	)
	res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]any{
		"content":    "把这两张删了",
		"session_id": sid,
		"quoted":     []map[string]any{{"result_id": uis[0].ResultID, "ids": []int64{mediaIDs[0]}}},
	})
	body = sseBody(t, res)
	if strings.Contains(body, "event: proposal") {
		t.Fatalf("kind 不一致必须驳回、不做映射：\n%s", body)
	}
	// 勾选里没有 post：文案应说明类型不对，而非「超额」
	if !strings.Contains(body, "本轮勾选里没有动态") {
		t.Fatalf("应给出类型引导文案：\n%s", body)
	}
}
