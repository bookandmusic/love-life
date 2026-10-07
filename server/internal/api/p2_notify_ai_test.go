package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// P2-10 / P2-11：AI 代操作的通知口径。
//   - 动态带地点时服务端会自动建点，但**不另发**足迹通知（该次发布已有 post_created）；
//   - AI 代发的动态 / 评论：站内快照带 via:"ai"，外部推送正文带「（AI）」标记
//     （接收端据此判别来源，ARCHITECTURE「通知分发器」快照字段读法）。
func TestAINotifyMarksAIAndSkipsAutoCreatedPoint(t *testing.T) {
	ts, adminToken, girlToken := newPostTestEnv(t)

	// 先让「对方」的动态存在，供 AI 评论（这一步的外推发生在注册渠道之前，不干扰计数）
	pres := doJSON(t, "POST", ts.URL+"/api/v1/posts", girlToken, map[string]any{"content": "她的一条动态"})
	var pb struct {
		ID int64 `json:"id"`
	}
	_ = json.NewDecoder(pres.Body).Decode(&pb)
	pres.Body.Close()

	rc := newWebhookReceiver(t)
	res := doJSON(t, "POST", ts.URL+"/api/v1/admin/webhooks", adminToken, map[string]any{
		"name": "ai-hook", "method": "POST", "url": rc.server.URL,
		"events": []string{"new_post", "comment", "footprint"},
	})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create webhook: %d", res.StatusCode)
	}

	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"p1","type":"function","function":{"name":"propose_post","arguments":"{\"content\":\"去鼓浪屿看海\",\"location_name\":\"厦门·鼓浪屿\"}"}}]}}`,
		fmt.Sprintf(`{"message":{"content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"propose_comment","arguments":"{\"post_id\":%d,\"content\":\"拍得真好\"}"}}]}}`, pb.ID),
	)
	seedFakeProvider(t, ts, fake.server.URL)

	// 1) AI 代发带地点动态
	_, body := chatTurn(t, ts, adminToken, 0, "帮我记一条去鼓浪屿的动态")
	actionID := cardFromSSE(t, body)
	if actionID == 0 {
		t.Fatalf("未取到确认卡：\n%s", body)
	}
	if st := confirmCard(t, ts, adminToken, actionID, true); st != "executed" {
		t.Fatalf("确认执行状态 = %q", st)
	}
	var pts, mapNotifs, postNotifs int
	var locName string
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM map_points WHERE deleted_at IS NULL`).Scan(&pts)
	// P2-10 只删通知，不动建点本身：点位照建、动态仍带 location_name
	_ = ts.db.QueryRow(`SELECT location_name FROM posts ORDER BY id DESC LIMIT 1`).Scan(&locName)
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE event_type='map_point_created'`).Scan(&mapNotifs)
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE event_type='post_created'`).Scan(&postNotifs)
	if pts != 1 {
		t.Fatalf("带地点动态应自动建点，实得 %d 个", pts)
	}
	if locName != "厦门·鼓浪屿" {
		t.Fatalf("动态应保留 location_name，实得 %q", locName)
	}
	if postNotifs != 1 {
		t.Fatalf("post_created 站内通知 = %d，want 1", postNotifs)
	}
	if mapNotifs != 0 {
		t.Fatalf("动态自动建点不得另发足迹通知，实得 %d 条", mapNotifs)
	}
	if rc.hits != 1 || !strings.Contains(rc.body, "（AI）") {
		t.Fatalf("AI 发动态应只外推一次且正文带（AI）：hits=%d bodies=%v", rc.hits, rc.bodies)
	}
	var postSnap string
	_ = ts.db.QueryRow(`SELECT snapshot_json FROM notifications WHERE event_type='post_created'`).Scan(&postSnap)
	if !strings.Contains(postSnap, `"via":"ai"`) || !strings.Contains(postSnap, `"operator"`) {
		t.Fatalf("AI 发动态快照应带 via=ai 与 operator：%s", postSnap)
	}
	// 快照读法：operator 恒为真实发起人用户名（ARCHITECTURE「通知分发器」快照字段读法）
	var snapMap map[string]any
	if err := json.Unmarshal([]byte(postSnap), &snapMap); err != nil {
		t.Fatalf("快照不是合法 JSON：%s", postSnap)
	}
	if snapMap["operator"] != "admin" {
		t.Fatalf("快照 operator 应为真实用户名 admin：%v", snapMap["operator"])
	}

	// 2) AI 代评论
	_, cbody := chatTurn(t, ts, adminToken, 0, "帮我评论她的动态")
	cid := cardFromSSE(t, cbody)
	if cid == 0 {
		t.Fatalf("未取到评论确认卡：\n%s", cbody)
	}
	if st := confirmCard(t, ts, adminToken, cid, true); st != "executed" {
		t.Fatalf("评论确认执行状态 = %q", st)
	}
	var cmtSnap string
	_ = ts.db.QueryRow(`SELECT snapshot_json FROM notifications WHERE event_type='post_commented'`).Scan(&cmtSnap)
	if !strings.Contains(cmtSnap, `"via":"ai"`) {
		t.Fatalf("AI 评论快照应带 via=ai：%s", cmtSnap)
	}
	// 评论外推必须自己到达（不能只看「最后一次 body」——那可能是上一步动态的正文）
	if rc.hits != 2 {
		t.Fatalf("AI 评论应新增一次外推：hits=%d bodies=%v", rc.hits, rc.bodies)
	}
	last := rc.bodies[len(rc.bodies)-1]
	if !strings.Contains(last, "（AI）") || !strings.Contains(last, "admin") || !strings.Contains(last, "拍得真好") {
		t.Fatalf("AI 评论推送正文应含操作者与（AI）与内容：%s", last)
	}
}
