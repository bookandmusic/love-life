package api

// 读工具结果集句柄守卫：三类列表（相册 / 纪念日 / 地点）都要落句柄且可勾选，
// 否则「点选优先」在这三类上永远走不通，批量提案也没有 source 可用。

import (
	"encoding/json"
	"strings"
	"testing"
)

// assertHandle 断言 UI 块带句柄且可勾选，返回句柄。
func assertHandle(t *testing.T, body, wantKind, label string) string {
	t.Helper()
	uis := toolEndUI(t, body)
	if len(uis) == 0 {
		t.Fatalf("%s：应下发 UI 块\n%s", label, body)
	}
	ui := uis[0]
	if ui.ResultID == "" {
		t.Fatalf("%s：UI 块必须带结果集句柄（否则走不了点选引用）", label)
	}
	if !ui.Selectable {
		t.Fatalf("%s：记录列表应可勾选引用", label)
	}
	return ui.ResultID
}

// TestFindAlbumsHasHandle：find_albums 落句柄。
func TestFindAlbumsHasHandle(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	res := doJSON(t, "POST", ts.URL+"/api/v1/albums", adminToken, map[string]any{"name": "杭州之旅"})
	res.Body.Close()

	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"a1","type":"function","function":{"name":"find_albums","arguments":"{}"}}]}}`,
		`{"message":{"content":"你的相册"}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	_, body := chatTurn(t, ts, adminToken, 0, "我有哪些相册")
	assertHandle(t, body, "record_list", "find_albums")
}

// TestFindAnniversariesHasHandle：find_anniversaries 落句柄。
func TestFindAnniversariesHasHandle(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	res := doJSON(t, "POST", ts.URL+"/api/v1/anniversaries", adminToken,
		map[string]any{"name": "在一起", "date": "2024-05-20"})
	res.Body.Close()

	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"n1","type":"function","function":{"name":"find_anniversaries","arguments":"{}"}}]}}`,
		`{"message":{"content":"你的纪念日"}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	_, body := chatTurn(t, ts, adminToken, 0, "我有哪些纪念日")
	assertHandle(t, body, "record_list", "find_anniversaries")
}

// TestFindPlacesHasHandleExcludingCandidates：find_places 落句柄，且**候选不进口句柄**
// ——离线地名候选 ID 为 0，进句柄会让批量删除带出不存在的 id。
func TestFindPlacesHasHandleExcludingCandidates(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	// 点位名与离线候选**不同名**：否则候选会因同名去重而不出现，
	// 测不到「候选照常展示但被排除出句柄」这条口径。
	res := doJSON(t, "POST", ts.URL+"/api/v1/map/points", adminToken, map[string]any{
		"name": "西湖", "lat": 30.2467, "lng": 120.4599,
	})
	res.Body.Close()

	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"p1","type":"function","function":{"name":"find_places","arguments":"{\"query\":\"西湖\"}"}}]}}`,
		`{"message":{"content":"你的足迹"}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	sid, body := chatTurn(t, ts, adminToken, 0, "我去过西湖吗")
	handle := assertHandle(t, body, "place_candidates", "find_places")

	// 句柄里存的 id 必须全是真实点位（不含 0）
	var idsJSON string
	if err := ts.db.QueryRow(`SELECT ids_json FROM chat_result_sets
		WHERE session_id = ? AND result_id = ?`, sid, handle).Scan(&idsJSON); err != nil {
		t.Fatalf("读取结果集失败：%v（handle=%s）", err, handle)
	}
	var ids []int64
	if err := json.Unmarshal([]byte(idsJSON), &ids); err != nil {
		t.Fatalf("ids_json 解析失败：%v (%s)", err, idsJSON)
	}
	if len(ids) == 0 {
		t.Fatalf("已有真实点位时句柄不应为空，ids=%v", ids)
	}
	for _, id := range ids {
		if id <= 0 {
			t.Fatalf("句柄混入了非真实记录 id=%d（候选 ID=0 必须被过滤），ids=%v", id, ids)
		}
	}
	// 候选仍应展示在卡上（只是不可勾选引用）
	if !strings.Contains(body, "候选") {
		t.Fatalf("离线候选应照常展示在卡上，body=\n%s", body)
	}
}

// TestAlbumHandleConsumedByProposeDelete 验收口径：`find_albums` 落下的句柄必须能被
// `propose_delete(source=句柄)` 展开，并通过 kind 同名校验——这才是句柄存在的意义
// （只断言 result_id 非空，等于没守住「能不能用」）。
func TestAlbumHandleConsumedByProposeDelete(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	for _, name := range []string{"杭州之旅", "春天"} {
		res := doJSON(t, "POST", ts.URL+"/api/v1/albums", adminToken, map[string]any{"name": name})
		res.Body.Close()
	}

	// 轮 1：拿句柄。全新库的首个结果集即 r_1；断言它，使句柄方案变化时报错而非静默测错对象。
	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"a1","type":"function","function":{"name":"find_albums","arguments":"{}"}}]}}`,
		`{"message":{"content":"你的相册"}}`,
		// 轮 2：用该句柄发起批量删除
		`{"message":{"content":"","tool_calls":[{"id":"d1","type":"function","function":{"name":"propose_delete","arguments":"{\"kind\":\"album\",\"source\":{\"result_id\":\"r_1\"}}"}}]}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	sid, body := chatTurn(t, ts, adminToken, 0, "我有哪些相册")
	handle := assertHandle(t, body, "record_list", "find_albums")
	if handle != "r_1" {
		t.Fatalf("句柄 = %q，本用例按 r_1 脚本化（全新库首个结果集）；方案变化须同步更新", handle)
	}
	// 句柄落库的 kind 必须与删除 kind 同名（deleteResultSetKind 是恒等映射）
	var kind string
	if err := ts.db.QueryRow(`SELECT kind FROM chat_result_sets WHERE session_id = ? AND result_id = ?`, sid, handle).Scan(&kind); err != nil {
		t.Fatalf("读取结果集 kind 失败：%v", err)
	}
	if kind != "album" {
		t.Fatalf("结果集 kind = %q，want album（须与 propose_delete 的 kind 同名才能展开）", kind)
	}

	// 轮 2：句柄被展开成具体相册 → 出确认卡（若能展开且 kind 校验通过，就一定有卡）
	_, body2 := chatTurn(t, ts, adminToken, sid, "把刚才那些相册都删掉")
	if !strings.Contains(body2, "event: proposal") {
		t.Fatalf("source=句柄 应能展开并出删除确认卡，SSE：\n%s", body2)
	}
	actionID := cardFromSSE(t, body2)
	if actionID == 0 {
		t.Fatal("未取到确认卡 id")
	}
	var preview string
	if err := ts.db.QueryRow(`SELECT preview_json FROM confirm_actions WHERE id = ?`, actionID).Scan(&preview); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"杭州之旅", "春天"} {
		if !strings.Contains(preview, name) {
			t.Fatalf("删除预览应展开出句柄里的相册「%s」，preview=%s", name, preview)
		}
	}
}
