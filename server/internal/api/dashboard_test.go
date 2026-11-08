package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/bookandmusic/love-life/server/internal/store"
)

// 端到端： 落库 → 的 ai-usage 聚合 → 的审计 latency 显示。
// 这条链路是台账 / 依赖 的实质含义，必须整体验证而非各测一半。
func TestAIUsageFeedsDashboardAndAudit(t *testing.T) {
	ts, _, girlToken := newPostTestEnv(t)
	adminToken, _ := loginToken(t, ts.URL, "admin", "admin-pw", "admin")
	ctx := t.Context()

	// admin 专属校验
	for _, p := range []string{"/dashboard/stats", "/dashboard/activity", "/dashboard/ai-usage", "/dashboard/alerts", "/audit"} {
		res := doJSON(t, "GET", ts.URL+"/api/v1/admin"+p, girlToken, nil)
		res.Body.Close()
		if res.StatusCode != http.StatusForbidden {
			t.Fatalf("%s girl: want 403, got %d", p, res.StatusCode)
		}
	}

	// 空库：ai-usage 为 items:[]（不补零），audit 为 []
	res := doJSON(t, "GET", ts.URL+"/api/v1/admin/dashboard/ai-usage", adminToken, nil)
	var usage struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&usage)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || len(usage.Items) != 0 {
		t.Fatalf("empty ai-usage: %d %+v", res.StatusCode, usage)
	}

	// 写入 统计（模拟两次成功 + 一次失败）
	for _, l := range []*store.AIUsageLog{
		{ProviderID: 1, Model: "gpt-4o", PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120, LatencyMs: 800, Status: "success"},
		{ProviderID: 1, Model: "gpt-4o", PromptTokens: 200, CompletionTokens: 40, TotalTokens: 240, LatencyMs: 1200, Status: "success"},
		{ProviderID: 1, Model: "gpt-4o", PromptTokens: 10, CompletionTokens: 0, TotalTokens: 10, LatencyMs: 9000, Status: "failed"},
	} {
		if err := ts.db.InsertAIUsage(ctx, l); err != nil {
			t.Fatal(err)
		}
	}

	// ai-usage 聚合为一天
	res = doJSON(t, "GET", ts.URL+"/api/v1/admin/dashboard/ai-usage", adminToken, nil)
	_ = json.NewDecoder(res.Body).Decode(&usage)
	res.Body.Close()
	if len(usage.Items) != 1 {
		t.Fatalf("ai-usage days = %d, want 1", len(usage.Items))
	}
	day := usage.Items[0]
	if day["calls"].(float64) != 3 {
		t.Fatalf("calls = %v, want 3", day["calls"])
	}
	if day["tokens"].(float64) != 370 {
		t.Fatalf("tokens = %v, want 370", day["tokens"])
	}
	// avgLatency = (800+1200+9000)/3 = 3666.67 → 3666
	if got := day["avgLatencyMs"].(float64); got < 3666 || got > 3667 {
		t.Fatalf("avgLatencyMs = %v, want ~3666", got)
	}

	// alerts 含失败调用（error 级）
	res = doJSON(t, "GET", ts.URL+"/api/v1/admin/dashboard/alerts", adminToken, nil)
	var alerts struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&alerts)
	res.Body.Close()
	foundErr := false
	for _, a := range alerts.Items {
		if a["level"] == "error" {
			foundErr = true
		}
	}
	if !foundErr {
		t.Fatalf("failed AI call should raise an error alert: %+v", alerts.Items)
	}

	// stats 形状
	res = doJSON(t, "GET", ts.URL+"/api/v1/admin/dashboard/stats", adminToken, nil)
	var stats []map[string]any
	_ = json.NewDecoder(res.Body).Decode(&stats)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || len(stats) != 4 {
		t.Fatalf("stats: %d len=%d", res.StatusCode, len(stats))
	}
	for _, s := range stats {
		for _, k := range []string{"label", "value", "change", "trend", "icon"} {
			if _, ok := s[k]; !ok {
				t.Fatalf("stat missing %s: %+v", k, s)
			}
		}
		if s["trend"] != "up" && s["trend"] != "neutral" {
			t.Fatalf("stat trend 非法: %+v", s)
		}
	}

	// 审计日志形状 + latencyMs 语义（ai 有值、user 为 null）
	if err := ts.db.InsertAuditLogFull(ctx, store.ActorTypeAI, 1, "search_posts", "digest", `query: "海边"`, "success", ptrInt64(1320)); err != nil {
		t.Fatal(err)
	}
	if err := ts.db.InsertAuditLogFull(ctx, store.ActorTypeUser, 1, "create_post", "content", "", "success", nil); err != nil {
		t.Fatal(err)
	}
	// pat 行：actor 解析为令牌名
	plain, patTok, err := ts.db.CreatePAT(ctx, "Claude Desktop", 2, "readonly")
	if err != nil {
		t.Fatal(err)
	}
	_ = plain
	if err := ts.db.InsertAuditLogFull(ctx, store.ActorTypePAT, patTok.ID, "list_favorites", "d", "page: 1", "success", ptrInt64(861)); err != nil {
		t.Fatal(err)
	}

	// 审计接口自 B2 起分页：响应是 {items, total, page, page_size}
	res = doJSON(t, "GET", ts.URL+"/api/v1/admin/audit", adminToken, nil)
	var auditPage struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	_ = json.NewDecoder(res.Body).Decode(&auditPage)
	res.Body.Close()
	audit := auditPage.Items
	if res.StatusCode != http.StatusOK || len(audit) != 3 || auditPage.Total != 3 {
		t.Fatalf("audit: %d len=%d total=%d", res.StatusCode, len(audit), auditPage.Total)
	}
	byAction := map[string]map[string]any{}
	for _, e := range audit {
		byAction[e["action"].(string)] = e
		for _, k := range []string{"time", "actorType", "actor", "action", "params", "result", "latencyMs"} {
			if _, ok := e[k]; !ok {
				t.Fatalf("audit entry missing %s: %+v", k, e)
			}
		}
	}
	ai := byAction["search_posts"]
	if ai["latencyMs"] == nil || ai["latencyMs"].(float64) != 1320 {
		t.Fatalf("ai latencyMs = %v, want 1320", ai["latencyMs"])
	}
	if ai["params"] != `query: "海边"` {
		t.Fatalf("params must be human readable: %v", ai["params"])
	}
	if ai["actor"] == "" || ai["actor"] == nil {
		t.Fatalf("ai actor must resolve: %+v", ai)
	}
	user := byAction["create_post"]
	if user["latencyMs"] != nil {
		t.Fatalf("user latencyMs must be null, got %v", user["latencyMs"])
	}
	patEntry := byAction["list_favorites"]
	if patEntry["actor"] != "Claude Desktop" {
		t.Fatalf("pat actor should resolve to token name, got %v", patEntry["actor"])
	}
	if patEntry["latencyMs"] == nil || patEntry["latencyMs"].(float64) != 861 {
		t.Fatalf("pat latencyMs = %v, want 861", patEntry["latencyMs"])
	}
}

func ptrInt64(v int64) *int64 { return &v }

// 回归：activity 曾因 likes 无 id 列而整条 UNION 失败（空库单测掩盖了该分支）。
// 此处刻意插入四类事件各一条——尤其 likes——确保每个 UNION 分支都被真实执行。
func TestDashboardActivityAllBranches(t *testing.T) {
	ts, _, _ := newPostTestEnv(t)
	adminToken, _ := loginToken(t, ts.URL, "admin", "admin-pw", "admin")
	ctx := t.Context()

	if _, err := ts.db.Exec(`INSERT INTO posts (author_id, content) VALUES (1,'p')`); err != nil {
		t.Fatal(err)
	}
	if _, err := ts.db.Exec(`INSERT INTO media (post_id, kind, file_path) VALUES (1,'photo','a.jpg')`); err != nil {
		t.Fatal(err)
	}
	if _, err := ts.db.Exec(`INSERT INTO comments (post_id, author_id, content) VALUES (1,1,'c')`); err != nil {
		t.Fatal(err)
	}
	// likes 是复合主键（post_id,user_id），无 id 列——正是导致 500 的分支
	if _, err := ts.db.Exec(`INSERT INTO likes (post_id, user_id) VALUES (1,1)`); err != nil {
		t.Fatal(err)
	}
	// 后台操作流水：仪表盘「最近活动」要显示 admin 自己干过的事（唯一来源是回收站操作日志；
	// audit_logs 只记 ai/pat 调用，那是「审计日志」页的内容）
	if _, err := ts.db.Exec(`INSERT INTO restore_logs (operator, summary) VALUES ('admin','恢复了动态「x」')`); err != nil {
		t.Fatal(err)
	}

	res := doJSON(t, "GET", ts.URL+"/api/v1/admin/dashboard/activity", adminToken, nil)
	var acts []map[string]any
	_ = json.NewDecoder(res.Body).Decode(&acts)
	res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("activity must be 200 when all event kinds exist, got %d", res.StatusCode)
	}
	if len(acts) != 5 {
		t.Fatalf("expected 5 activity entries, got %d: %+v", len(acts), acts)
	}
	icons := map[string]bool{}
	for _, a := range acts {
		icons[a["icon"].(string)] = true
		if a["text"] == nil || a["time"] == nil {
			t.Fatalf("activity entry incomplete: %+v", a)
		}
	}
	// 后台流水那条：文案是「操作者 + 空格 + 摘要」——逐字比对，顺序与分隔符错了也算失败
	var adminOp string
	for _, a := range acts {
		if a["icon"] == "refresh-cw" {
			adminOp, _ = a["text"].(string)
		}
	}
	if want := "admin 恢复了动态「x」"; adminOp != want {
		t.Fatalf("后台流水条目文案 = %q，want %q（全部：%+v）", adminOp, want, acts)
	}
	for _, want := range []string{"message-square", "image", "message-circle", "heart", "refresh-cw"} {
		if !icons[want] {
			t.Fatalf("missing activity branch %s: %+v", want, acts)
		}
	}
	_ = ctx
}

// 仪表盘时间文案按**本地日历日**比较：23 小时前若已跨日应显示「昨天」，
// 不能用工时差除以 24（那会把昨天的早间事件显示成今天）。
func TestHumanizeTimeUsesLocalCalendarDay(t *testing.T) {
	now := time.Date(2026, 3, 2, 10, 0, 0, 0, time.Local)
	cases := []struct {
		name string
		at   time.Time
		want string
	}{
		{"同一天 2 小时前", now.Add(-2 * time.Hour), "今天"},
		{"跨日 23 小时前", now.Add(-23 * time.Hour), "昨天"},
		{"跨两日 47 小时前", now.Add(-47 * time.Hour), "2 天前"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := tc.at.UTC().Format("2006-01-02 15:04:05")
			if got := humanizeTimeAt(raw, now); got != tc.want {
				t.Fatalf("humanizeTimeAt(%q) = %q，want %q", raw, got, tc.want)
			}
		})
	}
	if got := humanizeTimeAt("", now); got != "" {
		t.Fatalf("空串应原样返回，实得 %q", got)
	}
	if got := humanizeTimeAt("not-a-time", now); got != "not-a-time" {
		t.Fatalf("无法解析应原样返回，实得 %q", got)
	}
}

// 已软删内容的互动不得出现在活动流：整条动态进回收站后，它的赞/评不再可见；
// 评论本身软删也一样（与统计卡「已删内容不计入」同口径）。
func TestDashboardActivityExcludesSoftDeletedContent(t *testing.T) {
	ts, boyToken, girlToken := newPostTestEnv(t)
	adminToken, code := loginToken(t, ts.URL, "admin", "admin-pw", "admin")
	if code != http.StatusOK {
		t.Fatalf("admin login: %d", code)
	}
	ctx := t.Context()

	createPost := func(content string) int64 {
		res := doJSON(t, "POST", ts.URL+"/api/v1/posts", boyToken, map[string]any{"content": content})
		defer res.Body.Close()
		var body struct {
			ID int64 `json:"id"`
		}
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil || body.ID == 0 {
			t.Fatalf("create post %q: %v", content, err)
		}
		return body.ID
	}
	droppedID := createPost("将被删的那条")
	keptID := createPost("保留的那条")

	likeRes := doJSON(t, "POST", ts.URL+"/api/v1/posts/"+decStr(droppedID)+"/like", girlToken, map[string]any{})
	likeRes.Body.Close()
	cmtDrop := doJSON(t, "POST", ts.URL+"/api/v1/posts/"+decStr(droppedID)+"/comments", boyToken, map[string]any{"content": "将被连带删的评论"})
	cmtDrop.Body.Close()
	cmtKeep := doJSON(t, "POST", ts.URL+"/api/v1/posts/"+decStr(keptID)+"/comments", boyToken, map[string]any{"content": "将被单独删的评论"})
	cmtKeep.Body.Close()

	var commentID int64
	if err := ts.db.QueryRow(`SELECT id FROM comments WHERE post_id = ?`, keptID).Scan(&commentID); err != nil {
		t.Fatalf("query comment id: %v", err)
	}

	activityTexts := func() map[string]int {
		res := doJSON(t, "GET", ts.URL+"/api/v1/admin/dashboard/activity", adminToken, nil)
		defer res.Body.Close()
		var acts []map[string]any
		if err := json.NewDecoder(res.Body).Decode(&acts); err != nil {
			t.Fatalf("decode activity: %v", err)
		}
		got := map[string]int{}
		for _, a := range acts {
			if s, ok := a["text"].(string); ok {
				got[s]++
			}
		}
		return got
	}

	// 前置：删之前两类互动都在
	before := activityTexts()
	if before["点赞了动态"] != 1 || before["发表了评论"] != 2 {
		t.Fatalf("删前活动流 = %+v，want 赞 1 评 2", before)
	}

	if err := ts.db.SoftDeletePost(ctx, droppedID, store.ActorTypeUser, 1); err != nil {
		t.Fatalf("soft delete post: %v", err)
	}
	if err := ts.db.SoftDeleteComment(ctx, commentID, store.ActorTypeUser, 1); err != nil {
		t.Fatalf("soft delete comment: %v", err)
	}

	after := activityTexts()
	if after["点赞了动态"] != 0 {
		t.Fatalf("已删动态的点赞不该出现在活动流：%+v", after)
	}
	if after["发表了评论"] != 0 {
		t.Fatalf("已删动态的评论 / 已单删的评论都不该出现：%+v", after)
	}
	if after["发布了一条新动态"] != 1 {
		t.Fatalf("保留的动态仍应可见：%+v", after)
	}
}
