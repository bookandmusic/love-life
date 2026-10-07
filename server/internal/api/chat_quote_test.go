package api

// 结果集句柄端到端（/）：find_media 落句柄 → 下一条消息 quoted 引用其中一条
// → 模型上下文带上引用转述 → propose_album 用 source 展开，一次确认建册。

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

func TestChatQuotedResultSetBuildsAlbum(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	var mediaIDs []int64
	for i := 0; i < 2; i++ {
		tok, _ := uploadMedia(t, ts.URL, adminToken, fmt.Sprintf("p%d.jpg", i), genJPEG(t))
		res := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{
			"content": fmt.Sprintf("图%d", i), "media_tokens": []string{tok},
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
		`{"message":{"content":"找到了这些照片"}}`,
		`{"message":{"content":"","tool_calls":[{"id":"a1","type":"function","function":{"name":"propose_album","arguments":"{\"name\":\"勾选的一张\",\"source\":{\"result_id\":\"r_1\"}}"}}]}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	sid, body := chatTurn(t, ts, adminToken, 0, "找找未归档的照片")
	uis := toolEndUI(t, body)
	if len(uis) != 1 || uis[0].ResultID == "" || len(uis[0].Records) != 2 {
		t.Fatalf("tool.end ui = %+v", uis)
	}
	resultID := uis[0].ResultID
	picked := uis[0].Records[0].ID

	// 引用其中一条：模型上下文应带引用转述，且不能带上没勾的那条
	res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]any{
		"content":    "就用我勾的这张",
		"session_id": sid,
		"quoted":     []map[string]any{{"result_id": resultID, "ids": []int64{picked}}},
	})
	sseBody(t, res)
	var quoted string
	for _, m := range fake.lastMessages(t) {
		if m["role"] == "user" {
			if c, ok := m["content"].(string); ok && strings.Contains(c, "引用") {
				quoted = c
			}
		}
	}
	if !strings.Contains(quoted, resultID) || !strings.Contains(quoted, "勾选的 1 条") {
		t.Fatalf("模型没看到引用转述: %q", quoted)
	}
	// 点选优先：勾选交集落成子集结果集，上下文里出现新句柄 r_M（≠父句柄），
	// 模型拿到的就是「恰好那几条」的引用——不再只吃一句转述
	sub := ""
	for _, h := range regexp.MustCompile(`r_\d+`).FindAllString(quoted, -1) {
		if h != resultID {
			sub = h
		}
	}
	if sub == "" {
		t.Fatalf("上下文缺少勾选子集句柄 r_M: %q", quoted)
	}

	// 别的会话的句柄不能引用
	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]any{
		"content": "引用别人的", "session_id": 0,
		"quoted": []map[string]any{{"result_id": resultID}},
	})
	if res.StatusCode != 400 {
		t.Fatalf("跨会话引用 want 400, got %d", res.StatusCode)
	}
	res.Body.Close()

	// 假模型用勾选子集句柄出卡（点选优先：source 展开的就是勾的那几条）
	args, _ := json.Marshal(map[string]any{"name": "勾选的一张", "source": map[string]any{"result_id": sub}})
	call, _ := json.Marshal(map[string]any{"message": map[string]any{"content": "", "tool_calls": []any{
		map[string]any{"id": "a1", "type": "function", "function": map[string]any{"name": "propose_album", "arguments": string(args)}},
	}}})
	fake.mu.Lock()
	fake.script[2] = string(call)
	fake.mu.Unlock()
	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]any{
		"content": "把这批建成相册", "session_id": sid,
	})
	body = sseBody(t, res)
	actionID := cardFromSSE(t, body)

	// 展开后的执行参数 = 勾选的那一条（其余不进 params）
	var params string
	if err := ts.db.QueryRow(`SELECT params_json FROM confirm_actions WHERE id=?`, actionID).Scan(&params); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(params, fmt.Sprintf("%d", picked)) {
		t.Fatalf("params 缺勾选媒体 %d: %s", picked, params)
	}
	for _, id := range mediaIDs {
		if id != picked && strings.Contains(params, fmt.Sprintf("%d", id)) {
			t.Fatalf("未勾选的媒体 %d 不应出现在 params: %s", id, params)
		}
	}

	res = doJSON(t, "POST", ts.URL+fmt.Sprintf("/api/v1/chat/proposals/%d/resolve", actionID), adminToken,
		map[string]any{"decision": "approve"})
	res.Body.Close()
	var n int
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM album_items ai JOIN albums a ON a.id=ai.album_id WHERE a.name=?`, "勾选的一张").Scan(&n)
	if n != 1 {
		t.Fatalf("建册媒体数 = %d, want 1（勾几张就是几张）", n)
	}
}
