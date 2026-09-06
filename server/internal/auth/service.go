package auth

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/bookandmusic/love-life/server/internal/store"
)

type ctxKey struct{}

type Service struct {
	db      *store.DB
	tm      *TokenManager
	limiter *LoginLimiter
}

func NewService(db *store.DB, tm *TokenManager) *Service {
	return &Service{db: db, tm: tm, limiter: NewLoginLimiter()}
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Client   string `json:"client"` // "admin"（后台，仅 admin 角色）或 "app"
}

type userResponse struct {
	ID                int64  `json:"id"`
	Username          string `json:"username"`
	Role              string `json:"role"`
	Nickname          string `json:"nickname"`
	Birthday          string `json:"birthday"`
	AIStyle           string `json:"ai_style"`
	ChatRetentionDays int    `json:"chat_retention_days"`
}

// newUserResponse 组装用户响应（login/refresh/me 同源，对齐 mock userById 契约）。
func newUserResponse(u *store.User) userResponse {
	return userResponse{
		ID:                u.ID,
		Username:          u.Username,
		Role:              u.Role,
		Nickname:          u.Nickname,
		Birthday:          u.Birthday.String,
		AIStyle:           u.AIStyle,
		ChatRetentionDays: u.ChatRetentionDays,
	}
}

type tokenResponse struct {
	Token        string       `json:"token"`
	RefreshToken string       `json:"refresh_token"`
	User         userResponse `json:"user"`
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Service) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "用户名与密码不能为空")
		return
	}

	key := req.Username + "|" + clientIP(r)
	if !s.limiter.Allowed(key) {
		writeError(w, http.StatusTooManyRequests, "失败次数过多，已锁定 15 分钟")
		return
	}

	user, err := s.db.GetUserByUsername(r.Context(), req.Username)
	if err != nil || !CheckPassword(user.PasswordHash, req.Password) {
		s.limiter.Fail(key)
		writeError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	if req.Client == "admin" && user.Role != "admin" {
		writeError(w, http.StatusForbidden, "仅 admin 角色可登录后台")
		return
	}
	s.limiter.Reset(key)

	refresh, hash, err := NewRefreshToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	if err := s.db.SaveRefreshToken(r.Context(), user.ID, hash, time.Now().Add(RefreshTokenTTL())); err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	token, err := s.tm.IssueAccessToken(user.ID, user.Username, user.Role)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}

	writeJSON(w, http.StatusOK, tokenResponse{
		Token:        token,
		RefreshToken: refresh,
		User:         newUserResponse(user),
	})
}

func (s *Service) Refresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	stored, err := s.db.GetRefreshToken(r.Context(), hashRefreshToken(req.RefreshToken))
	if err != nil || time.Now().After(stored.ExpiresAt) {
		writeError(w, http.StatusUnauthorized, "refresh_token 无效或已过期")
		return
	}
	// 轮换：旧 token 立即作废
	if err := s.db.DeleteRefreshToken(r.Context(), stored.TokenHash); err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	user, err := s.db.GetUserByID(r.Context(), stored.UserID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "用户不存在")
		return
	}
	refresh, hash, err := NewRefreshToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	if err := s.db.SaveRefreshToken(r.Context(), user.ID, hash, time.Now().Add(RefreshTokenTTL())); err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	token, err := s.tm.IssueAccessToken(user.ID, user.Username, user.Role)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	writeJSON(w, http.StatusOK, tokenResponse{
		Token:        token,
		RefreshToken: refresh,
		User:         newUserResponse(user),
	})
}

func (s *Service) Logout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	_ = s.db.DeleteRefreshToken(r.Context(), hashRefreshToken(req.RefreshToken))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Service) Me(w http.ResponseWriter, r *http.Request) {
	claims, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "未认证")
		return
	}
	user, err := s.db.GetUserByID(r.Context(), claims.UserID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "用户不存在")
		return
	}
	writeJSON(w, http.StatusOK, newUserResponse(user))
}

// RequireAuth 校验 Bearer JWT 并注入用户 Claims；保护 /api/v1 业务路由。
func (s *Service) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "未认证")
			return
		}
		claims, err := s.tm.ParseAccessToken(strings.TrimPrefix(h, "Bearer "))
		if err != nil {
			writeError(w, http.StatusUnauthorized, "认证已过期")
			return
		}
		ctx := context.WithValue(r.Context(), ctxKey{}, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// AllowTokenQuery 认证中间件：支持 Authorization header 或 ?token= query 参数
// （瓦片等 img 资源无法带 header，用签名 query 参数）。
func (s *Service) AllowTokenQuery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := ""
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			tok = strings.TrimPrefix(h, "Bearer ")
		} else if q := r.URL.Query().Get("token"); q != "" {
			tok = q
		}
		if tok == "" {
			writeError(w, http.StatusUnauthorized, "未认证")
			return
		}
		claims, err := s.tm.ParseAccessToken(tok)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "认证已过期")
			return
		}
		ctx := context.WithValue(r.Context(), ctxKey{}, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func UserFromContext(ctx context.Context) (*Claims, bool) {
	claims, ok := ctx.Value(ctxKey{}).(*Claims)
	return claims, ok
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
