package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/bookandmusic/love-life/server/internal/auth"
	"github.com/bookandmusic/love-life/server/internal/store"
)

// parseID 解析路径参数 id；失败写 400 并返回 false。
func parseID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidID)
		return 0, false
	}
	return id, true
}

// securityAPI 数据安全设置。
type securityAPI struct {
	db *store.DB
}

// get GET /api/v1/admin/security
func (a *securityAPI) get(w http.ResponseWriter, r *http.Request) {
	s, err := a.db.GetSecuritySettings(r.Context())
	if err != nil {
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"recycleRetentionDays": s.RecycleRetentionDays,
	})
}

// put PUT /api/v1/admin/security —— 部分更新：只提交变更字段，服务端按字段合并，
// 不得整体覆盖。
// 当前只有回收站保留期限一个字段，故它为可选（缺省即不改）。
// 注：对话历史**不设保留期限**——没有 chat_retention_days，也没有按保留时长清理对话的任务。
func (a *securityAPI) put(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RecycleRetentionDays *int64 `json:"recycleRetentionDays"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}

	if req.RecycleRetentionDays != nil {
		d := *req.RecycleRetentionDays
		if d < store.RecycleRetentionMin || d > store.RecycleRetentionMax {
			writeError(w, http.StatusBadRequest, "回收站保留期限需在 7 ~ 365 天之间")
			return
		}
	}
	if err := a.db.SaveSecuritySettings(r.Context(), req.RecycleRetentionDays); err != nil {
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
		return
	}

	// 与 GET 同形状返回合并后的最新值（契约：PUT 回整项）
	s, err := a.db.GetSecuritySettings(r.Context())
	if err != nil {
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"recycleRetentionDays": s.RecycleRetentionDays,
	})
}

// recycleAPI 回收站管理。
type recycleAPI struct {
	db *store.DB
}

// 回收站列表分页：默认 50、上限 200；非法/负数回落默认（与 restore-log、评论列表同口径）。
const (
	recyclePageSizeDefault = 50
	recyclePageSizeMax     = 200
)

// recyclePageParams 解析 ?limit=&offset=：limit 缺省 50、夹到 1..200；offset 缺省/负数归 0。
func recyclePageParams(r *http.Request) (limit, offset int) {
	limit = recyclePageSizeDefault
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > recyclePageSizeMax {
		limit = recyclePageSizeMax
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			offset = n
		}
	}
	return limit, offset
}

// list GET /api/v1/admin/recycle —— 五类已删除内容按删除时间倒序，分页返回 {items,total,has_more}。
func (a *recycleAPI) list(w http.ResponseWriter, r *http.Request) {
	limit, offset := recyclePageParams(r)
	items, total, err := a.db.ListRecycle(r.Context(), limit, offset)
	if err != nil {
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
		return
	}

	type item struct {
		ID        int64  `json:"id"`
		Type      string `json:"type"`
		Summary   string `json:"summary"`
		DeletedAt string `json:"deletedAt"`
		DeletedBy string `json:"deletedBy"`
	}
	out := make([]item, 0, len(items))
	for _, it := range items {
		out = append(out, item{ID: it.ID, Type: it.Type, Summary: it.Summary, DeletedAt: it.DeletedAt, DeletedBy: it.DeletedBy})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": out, "total": total, "has_more": offset+len(items) < int(total),
	})
}

// restoreOne POST /api/v1/admin/recycle/{id}/restore（单条，二次确认后由前端调用）。
func (a *recycleAPI) restoreOne(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())

	id, ok := parseID(w, r)
	if !ok {
		return
	}

	res, err := a.db.RestoreRecycle(r.Context(), []int64{id})
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "内容不存在或未删除")
		return
	}
	if err != nil {
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
		return
	}

	a.writeLog(r, claims, res)
	writeJSON(w, http.StatusOK, map[string]any{})
}

// restoreMany POST /api/v1/admin/recycle/restore（批量）。
// 契约：ids 必填（非空数组）→ 400「ids 必填（非空数组）」。
func (a *recycleAPI) restoreMany(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())

	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "ids 必填（非空数组）")
		return
	}
	if len(req.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "ids 必填（非空数组）")
		return
	}

	res, err := a.db.RestoreRecycle(r.Context(), req.IDs)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "内容不存在或未删除")
		return
	}
	if err != nil {
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
		return
	}

	a.writeLog(r, claims, res)
	writeJSON(w, http.StatusOK, map[string]any{"restored": res.Restored})
}

// writeLog 回收站操作日志：记录谁在什么时间恢复了什么（系统超期永久删除由清理任务另行写入），永久保留。
func (a *recycleAPI) writeLog(r *http.Request, claims *auth.Claims, res *store.RestoreResult) {
	operator := "admin"
	if claims != nil && claims.Username != "" {
		operator = claims.Username
	}
	for _, sum := range res.Summaries {
		if err := a.db.AppendRestoreLog(r.Context(), operator, sum); err != nil {
			slog.Warn("append restore log failed", "operator", operator, "err", err)
		}
	}
}

// restoreLog GET /api/v1/admin/recycle/restore-log
// 返回 {items, total, has_more}：日志永久保留，默认只回最近 200 条（items 字段兼容既有 admin 页）。
const restoreLogDefaultLimit = 200

func (a *recycleAPI) restoreLog(w http.ResponseWriter, r *http.Request) {
	entries, total, err := a.db.ListRestoreLogs(r.Context(), restoreLogDefaultLimit)
	if err != nil {
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
		return
	}

	type item struct {
		Time     string `json:"time"`
		Operator string `json:"operator"`
		Summary  string `json:"summary"`
	}
	out := make([]item, 0, len(entries))
	for _, e := range entries {
		out = append(out, item{Time: e.Time, Operator: e.Operator, Summary: e.Summary})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": out, "total": total, "has_more": total > int64(len(out)),
	})
}
