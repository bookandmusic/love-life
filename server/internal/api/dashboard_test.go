package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/store"
)

// 端到端： 落库 → 的 ai-usage 聚合 → 的审计 latency 显示。
// 这条链路是台账 / 依赖 的实质含义，必须整体验证而非各测一半。
func TestAIUsageFeedsDashboardAndAudit(t *testing.T) {
	ts, adminToken, girlToken := newPostTestEnv(t)
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

	res = doJSON(t, "GET", ts.URL+"/api/v1/admin/audit", adminToken, nil)
	var audit []map[string]any
	_ = json.NewDecoder(res.Body).Decode(&audit)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || len(audit) != 3 {
		t.Fatalf("audit: %d len=%d", res.StatusCode, len(audit))
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
	ts, adminToken, _ := newPostTestEnv(t)
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
