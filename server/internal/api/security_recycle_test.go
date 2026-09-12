package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestSecuritySettingsPartialUpdate 覆盖 AD6：GET 默认值、PUT 部分更新按字段合并、
// 范围与档位校验。契约对齐 mock routes.mjs /admin/security（DESIGN §9 记录部分更新语义）。
func TestSecuritySettingsPartialUpdate(t *testing.T) {
	ts, adminToken, girlToken := newPostTestEnv(t)

	// girl 不可访问（admin 专属）
	res := doJSON(t, "GET", ts.URL+"/api/v1/admin/security", girlToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("girl security: want 403, got %d", res.StatusCode)
	}

	// 未设置 → 默认 30 / "30"
	res = doJSON(t, "GET", ts.URL+"/api/v1/admin/security", adminToken, nil)
	var got map[string]any
	_ = json.NewDecoder(res.Body).Decode(&got)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || got["recycleRetentionDays"].(float64) != 30 || got["chatRetention"] != "30" {
		t.Fatalf("defaults: %d %+v", res.StatusCode, got)
	}

	// 只提交 recycleRetentionDays → chatRetention 必须保持原值（按字段合并，不整体覆盖）
	res = doJSON(t, "PUT", ts.URL+"/api/v1/admin/security", adminToken, map[string]any{"recycleRetentionDays": 90})
	_ = json.NewDecoder(res.Body).Decode(&got)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || got["recycleRetentionDays"].(float64) != 90 || got["chatRetention"] != "30" {
		t.Fatalf("partial update 1: %d %+v", res.StatusCode, got)
	}

	// 再只提交 chatRetention → recycleRetentionDays 必须保持 90
	res = doJSON(t, "PUT", ts.URL+"/api/v1/admin/security", adminToken, map[string]any{"chatRetention": "permanent"})
	_ = json.NewDecoder(res.Body).Decode(&got)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || got["chatRetention"] != "permanent" || got["recycleRetentionDays"].(float64) != 90 {
		t.Fatalf("partial update 2: %d %+v", res.StatusCode, got)
	}

	// 持久化确认
	res = doJSON(t, "GET", ts.URL+"/api/v1/admin/security", adminToken, nil)
	_ = json.NewDecoder(res.Body).Decode(&got)
	res.Body.Close()
	if got["recycleRetentionDays"].(float64) != 90 || got["chatRetention"] != "permanent" {
		t.Fatalf("persisted: %+v", got)
	}

	// 校验：范围 7~365
	for _, bad := range []int64{6, 366, 0} {
		res := doJSON(t, "PUT", ts.URL+"/api/v1/admin/security", adminToken, map[string]any{"recycleRetentionDays": bad})
		var e map[string]any
		_ = json.NewDecoder(res.Body).Decode(&e)
		res.Body.Close()
		if res.StatusCode != http.StatusBadRequest || e["error"] != "回收站保留期限需在 7 ~ 365 天之间" {
			t.Fatalf("range %d: %d %+v", bad, res.StatusCode, e)
		}
	}
	// 校验：档位
	res = doJSON(t, "PUT", ts.URL+"/api/v1/admin/security", adminToken, map[string]any{"chatRetention": "15"})
	var e map[string]any
	_ = json.NewDecoder(res.Body).Decode(&e)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad retention tier: %d %+v", res.StatusCode, e)
	}
	// 边界值可用
	res = doJSON(t, "PUT", ts.URL+"/api/v1/admin/security", adminToken, map[string]any{"recycleRetentionDays": 7})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("min bound must be accepted: %d", res.StatusCode)
	}
	res = doJSON(t, "PUT", ts.URL+"/api/v1/admin/security", adminToken, map[string]any{"recycleRetentionDays": 365})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("max bound must be accepted: %d", res.StatusCode)
	}
}

// TestRecycleListAndRestore 覆盖 AD5：列表聚合六类、单条/批量恢复、非法 ids、
// 未找到 404、恢复日志与「删除者」归属。契约对齐 mock routes.mjs /admin/recycle。
func TestRecycleListAndRestore(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	// 初始回收站为空
	res := doJSON(t, "GET", ts.URL+"/api/v1/admin/recycle", adminToken, nil)
	var list []map[string]any
	_ = json.NewDecoder(res.Body).Decode(&list)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || len(list) != 0 {
		t.Fatalf("empty recycle: %d %+v", res.StatusCode, list)
	}

	// 造数据：一条动态（含媒体）+ 一条评论，然后软删除
	var postID int64
	if err := ts.db.QueryRow(`INSERT INTO posts (author_id, content) VALUES (1, '周末一起去海边散步看日落') RETURNING id`).Scan(&postID); err != nil {
		t.Fatalf("seed post: %v", err)
	}
	if _, err := ts.db.Exec(`INSERT INTO media (post_id, kind, file_path) VALUES (?, 'photo', 'media/2026/IMG_001.jpg')`, postID); err != nil {
		t.Fatalf("seed media: %v", err)
	}
	// 用户软删除（user 归属）→ 动态 + 其媒体均进回收站
	if err := ts.db.SoftDeletePost(t.Context(), postID, "user", 1); err != nil {
		t.Fatalf("soft delete post: %v", err)
	}
	// 另一类内容（评论）用于批量恢复分支：与动态无级联关系
	var commentID int64
	if err := ts.db.QueryRow(`INSERT INTO comments (post_id, author_id, content) VALUES (?, 1, '下次还要一起去！') RETURNING id`, postID).Scan(&commentID); err != nil {
		t.Fatalf("seed comment: %v", err)
	}
	if err := ts.db.SoftDeleteComment(t.Context(), commentID, "ai", 1); err != nil {
		t.Fatalf("soft delete comment: %v", err)
	}

	res = doJSON(t, "GET", ts.URL+"/api/v1/admin/recycle", adminToken, nil)
	_ = json.NewDecoder(res.Body).Decode(&list)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || len(list) != 3 {
		t.Fatalf("recycle after delete: %d len=%d %+v", res.StatusCode, len(list), list)
	}

	// 形状与「删除者」：动态与照片各一条，deletedBy 解析为用户显示名
	byType := map[string]map[string]any{}
	for _, it := range list {
		byType[it["type"].(string)] = it
	}
	post, ok := byType["动态"]
	if !ok {
		t.Fatalf("post entry missing: %+v", list)
	}
	for _, k := range []string{"id", "type", "summary", "deletedAt", "deletedBy"} {
		if _, ok := post[k]; !ok {
			t.Fatalf("post entry missing key %s: %+v", k, post)
		}
	}
	if post["summary"] != "周末一起去海边散步看日落" {
		t.Fatalf("summary = %v", post["summary"])
	}
	if post["deletedBy"] == "" || post["deletedBy"] == nil {
		t.Fatalf("deletedBy must resolve to a display name: %+v", post)
	}
	// 照片摘要取文件名（不是整条路径）
	photo, ok := byType["照片"]
	if !ok {
		t.Fatalf("photo entry missing (media cascade): %+v", list)
	}
	if photo["summary"] != "IMG_001.jpg" {
		t.Fatalf("photo summary = %v, want file name", photo["summary"])
	}
	// AI 通道删除 → deletedBy 显示「AI 助手」（DESIGN §9 回收站「删除者」列）
	comment, ok := byType["评论"]
	if !ok {
		t.Fatalf("comment entry missing: %+v", list)
	}
	if comment["deletedBy"] != "AI 助手" {
		t.Fatalf("ai-deleted entry deletedBy = %v, want AI 助手", comment["deletedBy"])
	}

	// 非法 ids → 400「ids 必填（非空数组）」
	for _, body := range []map[string]any{{}, {"ids": []int64{}}} {
		res := doJSON(t, "POST", ts.URL+"/api/v1/admin/recycle/restore", adminToken, body)
		var e map[string]any
		_ = json.NewDecoder(res.Body).Decode(&e)
		res.Body.Close()
		if res.StatusCode != http.StatusBadRequest || e["error"] != "ids 必填（非空数组）" {
			t.Fatalf("bad ids %+v: %d %+v", body, res.StatusCode, e)
		}
	}

	// 单条恢复动态（二次确认后由前端调用）→ 200 {}，并回到可见
	postIDenc := int64(post["id"].(float64))
	res = doJSON(t, "POST", ts.URL+"/api/v1/admin/recycle/"+itoa(postIDenc)+"/restore", adminToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("restore one: %d", res.StatusCode)
	}
	var visible int
	if err := ts.db.QueryRow(`SELECT COUNT(*) FROM posts WHERE id = ? AND deleted_at IS NULL`, postID).Scan(&visible); err != nil || visible != 1 {
		t.Fatalf("post not restored: %d err=%v", visible, err)
	}
	// 恢复动态必须连带恢复其级联删除的媒体：否则动态「复活」但照片不可见。
	// （SoftDeletePost 会把媒体一并软删；此处锁住该不变式，防止回归。）
	var mediaStillDeleted int
	if err := ts.db.QueryRow(`SELECT COUNT(*) FROM media WHERE post_id = ? AND deleted_at IS NOT NULL`, postID).Scan(&mediaStillDeleted); err != nil {
		t.Fatal(err)
	}
	if mediaStillDeleted != 0 {
		t.Fatalf("restoring a post must restore its cascaded media, %d still soft-deleted", mediaStillDeleted)
	}

	// 未找到 → 404
	res = doJSON(t, "POST", ts.URL+"/api/v1/admin/recycle/999999/restore", adminToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("restore missing: want 404, got %d", res.StatusCode)
	}

	// 批量恢复剩余（评论）：与动态无级联关系，逐条计数
	res = doJSON(t, "GET", ts.URL+"/api/v1/admin/recycle", adminToken, nil)
	_ = json.NewDecoder(res.Body).Decode(&list)
	res.Body.Close()
	ids := make([]int64, 0, len(list))
	for _, it := range list {
		ids = append(ids, int64(it["id"].(float64)))
	}
	if len(ids) == 0 {
		t.Fatal("expected remaining recycle items")
	}
	res = doJSON(t, "POST", ts.URL+"/api/v1/admin/recycle/restore", adminToken, map[string]any{"ids": ids})
	var rres map[string]any
	_ = json.NewDecoder(res.Body).Decode(&rres)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || rres["restored"].(float64) != float64(len(ids)) {
		t.Fatalf("batch restore: %d %+v", res.StatusCode, rres)
	}
	// 批量后回收站应为空
	res = doJSON(t, "GET", ts.URL+"/api/v1/admin/recycle", adminToken, nil)
	_ = json.NewDecoder(res.Body).Decode(&list)
	res.Body.Close()
	if len(list) != 0 {
		t.Fatalf("recycle must be empty after batch restore: %+v", list)
	}

	// 恢复日志：永久保留、有记录、形状 {time,operator,summary}
	res = doJSON(t, "GET", ts.URL+"/api/v1/admin/recycle/restore-log", adminToken, nil)
	var log struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&log)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || len(log.Items) == 0 {
		t.Fatalf("restore log empty: %d %+v", res.StatusCode, log)
	}
	first := log.Items[0]
	for _, k := range []string{"time", "operator", "summary"} {
		if _, ok := first[k]; !ok {
			t.Fatalf("restore log entry missing %s: %+v", k, first)
		}
	}
	if first["operator"] == "" {
		t.Fatalf("operator must be recorded: %+v", first)
	}
}
