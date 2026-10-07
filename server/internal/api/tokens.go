package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/bookandmusic/love-life/server/internal/store"
)

// patAPI 个人访问令牌签发与管理。
type patAPI struct {
	db *store.DB
}

// item 契约形状（routes.mjs /admin/tokens）：
// {id, name, user, tier, createdAt, status}；user 为绑定用户显示名，createdAt 为 YYYY-MM-DD。
type patItem struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	User      string `json:"user"`
	Tier      string `json:"tier"`
	CreatedAt string `json:"createdAt"`
	Status    string `json:"status"`
}

func toPATItem(t *store.PersonalAccessToken) patItem {
	created := t.CreatedAt
	if len(created) >= 10 {
		created = created[:10] // 契约只到日期
	}
	return patItem{ID: t.ID, Name: t.Name, User: t.Username, Tier: t.Tier, CreatedAt: created, Status: t.Status}
}

// list GET /api/v1/admin/tokens
func (a *patAPI) list(w http.ResponseWriter, r *http.Request) {
	toks, err := a.db.ListPATs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	out := make([]patItem, 0, len(toks))
	for _, t := range toks {
		out = append(out, toPATItem(t))
	}
	writeJSON(w, http.StatusOK, out)
}

// create POST /api/v1/admin/tokens
// 契约校验顺序（routes.mjs，逐字对齐）：name 必填 → tier 仅 readonly/readwrite →
// 绑定用户存在 → admin 不签发。成功 201，响应含一次性明文 token。
func (a *patAPI) create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		User string `json:"user"`
		Tier string `json:"tier"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name 必填")
		return
	}
	if req.Tier != "readonly" && req.Tier != "readwrite" {
		writeError(w, http.StatusBadRequest, "tier 仅支持 readonly / readwrite")
		return
	}

	// 绑定用户：按昵称或用户名匹配（契约 user 字段可为任一）
	var userID int64
	var role string
	err := a.db.QueryRowContext(r.Context(),
		`SELECT id, role FROM users WHERE nickname = ? OR username = ? LIMIT 1`, req.User, req.User).Scan(&userID, &role)
	if err != nil {
		writeError(w, http.StatusBadRequest, "绑定用户不存在")
		return
	}
	if role == "admin" {
		writeError(w, http.StatusBadRequest, "PAT 仅可绑定情侣用户（girl/boy），admin 不签发令牌")
		return
	}

	plain, tok, err := a.db.CreatePAT(r.Context(), req.Name, userID, req.Tier)
	if err != nil {
		if errors.Is(err, store.ErrPATAdminForbidden) {
			writeError(w, http.StatusBadRequest, "PAT 仅可绑定情侣用户（girl/boy），admin 不签发令牌")
			return
		}
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusBadRequest, "绑定用户不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id": tok.ID, "name": tok.Name, "user": tok.Username, "tier": tok.Tier,
		"createdAt": toPATItem(tok).CreatedAt, "status": tok.Status, "token": plain,
	})
}

// revoke POST /api/v1/admin/tokens/{id}/revoke
func (a *patAPI) revoke(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "令牌不存在")
		return
	}
	tok, err := a.db.RevokePAT(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "令牌不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusOK, toPATItem(tok))
}
