package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/bookandmusic/love-life/server/internal/auth"
	"github.com/bookandmusic/love-life/server/internal/store"
)

// adminAPI 后台成员管理（需求 §8.3：本空间信息与成员，含 girl/boy 密码重置）。
type adminAPI struct {
	db *store.DB
}

func (a *adminAPI) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := a.db.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	type item struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
		Role     string `json:"role"`
		Nickname string `json:"nickname"`
	}
	out := make([]item, 0, len(users))
	for _, u := range users {
		out = append(out, item{ID: u.ID, Username: u.Username, Role: u.Role, Nickname: u.Nickname})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *adminAPI) createUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
		Nickname string `json:"nickname"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if req.Role != "girl" && req.Role != "boy" {
		writeError(w, http.StatusBadRequest, "角色仅允许 girl / boy（admin 由部署内置）")
		return
	}
	if n := len([]rune(req.Username)); n < 2 || n > 32 {
		writeError(w, http.StatusBadRequest, "用户名长度需 2-32 字符")
		return
	}
	if len(req.Password) < 6 {
		writeError(w, http.StatusBadRequest, "密码至少 6 位")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	id, err := a.db.CreateUser(r.Context(), &store.User{
		Username: req.Username, PasswordHash: hash, Role: req.Role, Nickname: req.Nickname,
	})
	if err != nil {
		if errors.Is(err, store.ErrDuplicateUsername) {
			writeError(w, http.StatusConflict, "用户名已存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "username": req.Username, "role": req.Role})
}

func (a *adminAPI) resetPassword(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的用户 ID")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if len(req.Password) < 6 {
		writeError(w, http.StatusBadRequest, "密码至少 6 位")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	if err := a.db.UpdateUserPassword(r.Context(), id, hash); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "用户不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// requireAdmin 在认证之上叠加角色校验：后台路由仅 admin（需求 §8.3）。
func requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := auth.UserFromContext(r.Context())
		if !ok || claims.Role != "admin" {
			writeError(w, http.StatusForbidden, "仅 admin 可访问")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
