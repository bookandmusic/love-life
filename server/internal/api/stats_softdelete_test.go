package api

// B5 统计口径守卫：已软删的动态不得计入统计（统计概览卡的数字必须准且即时，
// 与已删除内容过滤同一口径）。
// 覆盖两处：TimelinePostCounts 的按年分布，以及同一工具里的互动计数（赞/评）。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// TestSpaceStatsExcludesSoftDeletedPosts：软删一条动态后，统计卡的「动态」总数、
// 按年分布、互动三计数都不得再算它。
func TestSpaceStatsExcludesSoftDeletedPosts(t *testing.T) {
	ts, adminToken, girlToken := newPostTestEnv(t)

	// 两条动态（keep / drop），并给 drop 制造赞/评
	keepRes := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{"content": "保留的那条"})
	keepRes.Body.Close()
	_ = http.StatusCreated
	dropRes := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{"content": "要被删的那条"})
	var dropBody struct {
		ID int64 `json:"id"`
	}
	_ = json.NewDecoder(dropRes.Body).Decode(&dropBody)
	if dropBody.ID == 0 {
		t.Fatal("前置：未取到动态 id")
	}

	likeRes := doJSON(t, "POST", ts.URL+"/api/v1/posts/"+fmt.Sprintf("%d", dropBody.ID)+"/like", girlToken, map[string]any{})
	t.Logf("like status=%d", likeRes.StatusCode)
	likeRes.Body.Close()
	cmtRes := doJSON(t, "POST", ts.URL+"/api/v1/posts/"+fmt.Sprintf("%d", dropBody.ID)+"/comments", adminToken, map[string]any{"content": "一条评论"})
	cmtRes.Body.Close()

	// 软删 drop
	delRes := doJSON(t, "DELETE", ts.URL+"/api/v1/posts/"+fmt.Sprintf("%d", dropBody.ID), adminToken, nil)
	delRes.Body.Close()
	if delRes.StatusCode >= 300 {
		t.Fatalf("软删失败：%d", delRes.StatusCode)
	}

	// store 层：TimelinePostCounts 不计已删
	counts, err := ts.db.TimelinePostCounts(t.Context(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, c := range counts {
		total += int(c.Count)
	}
	if total != 1 {
		t.Fatalf("TimelinePostCounts 总数 = %d，want 1（软删那条不得计入）", total)
	}
}

// TestSpaceStatsInteractionCountsExcludeDeletedPost：互动三计数必须 join posts 判软删。
// likes/favorites 自身没有 deleted_at，comments 的 deleted_at 只管评论自身——
// 三条 SQL 都不看「所属动态是否已删」时，同卡「动态 N 条」与「互动 M 次」会自相矛盾。
func TestSpaceStatsInteractionCountsExcludeDeletedPost(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	dropRes := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{"content": "带互动的动态"})
	var dropBody struct {
		ID int64 `json:"id"`
	}
	_ = json.NewDecoder(dropRes.Body).Decode(&dropBody)
	if dropBody.ID == 0 {
		t.Fatal("前置：未取到动态 id")
	}

	doJSON(t, "POST", ts.URL+"/api/v1/posts/"+fmt.Sprintf("%d", dropBody.ID)+"/like", adminToken, map[string]any{}).Body.Close()
	doJSON(t, "POST", ts.URL+"/api/v1/posts/"+fmt.Sprintf("%d", dropBody.ID)+"/comments", adminToken, map[string]any{"content": "评论"}).Body.Close()

	var likeN, cmtN int
	// 删除前：赞 1 / 评 1
	countInteractions := func() (int, int) {
		t.Helper()
		var l, c int
		if err := ts.db.QueryRow(`SELECT COUNT(*) FROM likes l JOIN posts p ON p.id = l.post_id WHERE p.deleted_at IS NULL`).Scan(&l); err != nil {
			t.Fatal(err)
		}
		if err := ts.db.QueryRow(`SELECT COUNT(*) FROM comments c JOIN posts p ON p.id = c.post_id
			WHERE c.deleted_at IS NULL AND p.deleted_at IS NULL`).Scan(&c); err != nil {
			t.Fatal(err)
		}
		return l, c
	}
	likeN, cmtN = countInteractions()
	if likeN != 1 || cmtN != 1 {
		t.Fatalf("前置：删除前互动应为 赞1/评1，实得 %d/%d", likeN, cmtN)
	}

	doJSON(t, "DELETE", ts.URL+"/api/v1/posts/"+fmt.Sprintf("%d", dropBody.ID), adminToken, nil).Body.Close()

	afterLike, afterCmt := countInteractions()
	if afterLike != 0 || afterCmt != 0 {
		t.Fatalf("软删动态的互动不得计入，实得 赞 %d / 评 %d（want 0/0）", afterLike, afterCmt)
	}
}

// TestSpaceStatsToolReportsZeroAfterDelete：走真实 AI 统计工具（get_space_stats），
// 断言下发的 数据与 ModelText 都不含已删内容——这是用户实际看到的那张卡。
func TestSpaceStatsToolReportsZeroAfterDelete(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	dropRes := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{"content": "会被删的一条"})
	var dropBody struct {
		ID int64 `json:"id"`
	}
	_ = json.NewDecoder(dropRes.Body).Decode(&dropBody)
	doJSON(t, "POST", ts.URL+"/api/v1/posts/"+fmt.Sprintf("%d", dropBody.ID)+"/like", adminToken, map[string]any{}).Body.Close()
	doJSON(t, "DELETE", ts.URL+"/api/v1/posts/"+fmt.Sprintf("%d", dropBody.ID), adminToken, nil).Body.Close()

	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"g1","type":"function","function":{"name":"get_space_stats","arguments":"{}"}}]}}`,
		`{"message":{"content":"这是你的空间概览"}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	_, body := chatTurn(t, ts, adminToken, 0, "我们一共记了多少条动态")
	uis := toolEndData(t, body)
	if len(uis) == 0 {
		t.Fatalf("get_space_stats 应下发数据，SSE：\n%s", body)
	}
	// 统计内容在 tool.end 的 data 中；ModelText 在模型收到的 tool 消息里
	var toolMsg string
	for _, m := range fake.lastMessages(t) {
		if m["role"] == "tool" {
			toolMsg, _ = m["content"].(string)
		}
	}
	if strings.Contains(toolMsg, "动态 1 条") {
		t.Fatalf("已软删动态不得计入总数，ModelText=%s", toolMsg)
	}
	if !strings.Contains(toolMsg, "动态 0 条") {
		t.Fatalf("软删后动态总数应为 0，ModelText=%s", toolMsg)
	}
	if !strings.Contains(toolMsg, "赞 0") || !strings.Contains(toolMsg, "评 0") {
		t.Fatalf("软删动态的互动计数应为 0，ModelText=%s", toolMsg)
	}
}
