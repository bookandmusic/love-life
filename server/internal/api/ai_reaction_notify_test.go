package api

// D1 AI 侧点赞/收藏必须通知另一方（点赞/收藏允许自赞/自藏，
// 任何一方动作都通知另一方）。
// 缺陷点：AI 工具的 propose_reaction 执行分支只改状态、拼文案，不写通知。
//
// 收件人口径：ListPartnerIDs 排除 admin 与操作者本人，故「另一方」必须是另一个
// 非 admin 账号——测试里补一个 boy，girl 发动态、boy 让 AI 去赞。

import (
	"encoding/json"
	"testing"
)

// seedBoy 取 boy 账号 id（newPostTestEnv 已建 girl+boy）。
func seedBoy(t *testing.T, ts *testEnv) int64 {
	t.Helper()
	u, err := ts.db.GetUserByUsername(t.Context(), "boy")
	if err != nil {
		t.Fatalf("get boy: %v", err)
	}
	return u.ID
}

// TestAIReactionNotifiesPartner：boy 让 AI 点赞 girl 的动态 → girl 收到 post_liked。
func TestAIReactionNotifiesPartner(t *testing.T) {
	ts, _, girlToken := newPostTestEnv(t)
	seedBoy(t, ts)
	boyToken, code := loginToken(t, ts.URL, "boy", "boy-pw", "app")
	if code != 200 {
		t.Fatalf("boy login: %d", code)
	}

	// girl 发动态（收件人 = 动态作者 girl）
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", girlToken, map[string]any{"content": "海边的日落"})
	var created struct {
		ID int64 `json:"id"`
	}
	decodeInto(t, res, &created)
	if created.ID == 0 {
		t.Fatal("前置：未取到动态 id")
	}
	var authorID int64
	if err := ts.db.QueryRow(`SELECT author_id FROM posts WHERE id = ?`, created.ID).Scan(&authorID); err != nil {
		t.Fatal(err)
	}

	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"r1","type":"function","function":{"name":"propose_reaction","arguments":"{\"post_id\":`+i64s(created.ID)+`,\"like\":true}"}}]}}`,
		`{"message":{"content":"已经帮你说好了"}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	_, body := chatTurn(t, ts, boyToken, 0, "给 TA 那条动态点个赞")
	if st := confirmCard(t, ts, boyToken, cardFromSSE(t, body), true); st != "executed" {
		t.Fatalf("确认执行状态 = %q", st)
	}

	var n int
	var snapshot string
	if err := ts.db.QueryRow(`SELECT COUNT(*), COALESCE(MAX(snapshot_json),'')
		FROM notifications WHERE recipient_id = ? AND event_type = 'post_liked' AND target_id = ?`,
		authorID, created.ID).Scan(&n, &snapshot); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("AI 点赞后另一方应收到 post_liked 通知")
	}
	// 快照带 via=ai 与 operator（接收端据此判别来源）
	var snap map[string]any
	if err := json.Unmarshal([]byte(snapshot), &snap); err != nil {
		t.Fatalf("snapshot 解析失败：%v (%s)", err, snapshot)
	}
	if snap["via"] != "ai" {
		t.Fatalf("快照应带 via=ai，实得 %+v", snap)
	}
	if snap["operator"] == nil || snap["operator"] == "" {
		t.Fatalf("快照应带 operator，实得 %+v", snap)
	}
	// 快照带 author_id = 动态作者（互动归属：boy 赞 girl 的动态 → 对方视角「你的动态」）
	if aid, ok := snap["author_id"].(float64); !ok || int64(aid) != authorID {
		t.Fatalf("快照 author_id 应为动态作者 %d，实得 %+v", authorID, snap)
	}
}

// TestAIReactionNoNotifyWhenUnchanged：取消点赞不得新增通知
// （对齐 REST：只在 liked/favorited 为 true 时发）。
func TestAIReactionNoNotifyWhenUnchanged(t *testing.T) {
	ts, _, girlToken := newPostTestEnv(t)
	seedBoy(t, ts)
	boyToken, code := loginToken(t, ts.URL, "boy", "boy-pw", "app")
	if code != 200 {
		t.Fatalf("boy login: %d", code)
	}

	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", girlToken, map[string]any{"content": "一条动态"})
	var created struct {
		ID int64 `json:"id"`
	}
	decodeInto(t, res, &created)
	var authorID int64
	if err := ts.db.QueryRow(`SELECT author_id FROM posts WHERE id = ?`, created.ID).Scan(&authorID); err != nil {
		t.Fatal(err)
	}

	// 注意：出提案即 break，本轮**不会**再有第二次模型调用 → 脚本里每次调用一个出卡响应
	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"r1","type":"function","function":{"name":"propose_reaction","arguments":"{\"post_id\":`+i64s(created.ID)+`,\"like\":true}"}}]}}`,
		`{"message":{"content":"","tool_calls":[{"id":"r2","type":"function","function":{"name":"propose_reaction","arguments":"{\"post_id\":`+i64s(created.ID)+`,\"like\":false}"}}]}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	_, body := chatTurn(t, ts, boyToken, 0, "点个赞")
	if st := confirmCard(t, ts, boyToken, cardFromSSE(t, body), true); st != "executed" {
		t.Fatalf("点赞确认 = %q", st)
	}
	var afterLike int
	if err := ts.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE recipient_id = ? AND event_type = 'post_liked'`, authorID).Scan(&afterLike); err != nil {
		t.Fatal(err)
	}
	if afterLike == 0 {
		t.Fatal("前置：点赞应产生通知")
	}

	_, body2 := chatTurn(t, ts, boyToken, 0, "算了取消吧")
	if st := confirmCard(t, ts, boyToken, cardFromSSE(t, body2), true); st != "executed" {
		t.Fatalf("取消点赞确认 = %q", st)
	}
	var afterUnlike int
	if err := ts.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE recipient_id = ? AND event_type = 'post_liked'`, authorID).Scan(&afterUnlike); err != nil {
		t.Fatal(err)
	}
	if afterUnlike != afterLike {
		t.Fatalf("取消点赞不得新增通知：前 %d、后 %d", afterLike, afterUnlike)
	}
}
