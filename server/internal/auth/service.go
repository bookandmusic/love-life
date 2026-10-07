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
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	Nickname string `json:"nickname"`
	AIStyle  string `json:"ai_style"`
}

// newUserResponse 组装用户响应（login/refresh/me 同源，对齐 mock userById 契约）。
func newUserResponse(u *store.User) userResponse {
	return userResponse{
		ID:       u.ID,
		Username: u.Username,
		Role:     u.Role,
		Nickname: u.Nickname,
		AIStyle:  u.AIStyle,
	}
}

type tokenResponse struct {
	Token        string       `json:"token"`
	RefreshToken string       `json:"refresh_token"`
	User         userResponse `json:"user"`
}

// loginLimitKey 登录失败计数 key：用户名 + IP。
func loginLimitKey(username, ip string) string { return username + "|" + ip }

// refreshLimitKey 令牌签发失败计数 key：只按来源 IP（该接口没有用户名可拼，
// 且「构成失败」时令牌查不到、也就归属不出用户）。
// 隔离**不是**靠「字符不可能出现」保证的——IP 由 RemoteAddr 透传、用户名只校验长度，
// 两者都可能含 "|" 或 ":"（用户名可含任意可打印字符）。这里靠的是**实测断言**：
// 两侧 key 的构造方式不同且带不同前缀，正常取值下不可能相等；
// 「任意输入下都不同」这一更强命题不成立，故不写进注释——若将来用户名/地址格式收紧，
// 应改为「先规范化再拼」而非假定字符集。见 limiter_test.go 的取值用例。
func refreshLimitKey(ip string) string { return "refresh:" + ip }

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

	key := loginLimitKey(req.Username, clientIP(r))
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
	if req.Client == "app" && user.Role == "admin" {
		writeError(w, http.StatusForbidden, "管理员账号请登录后台，不支持在 APP 使用")
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
	// 令牌签发接口同样内置限速与失败锁定（「统一登录与认证」安全基线）。
	// key 以**来源 IP** 为口径，前缀 refresh: 与登录的「用户名|IP」各自独立命名空间：
	// 本接口的请求体只有令牌、没有用户名，而「构成失败」的情形恰是「令牌查不到」——
	// 那一刻也无从归属用户，故失败计数只能落在 IP 上（能查到归属就不算失败，两者互斥，
	// 「用户名 + IP 的失败计数」这种形态在本接口不存在）。
	// 退化为 IP 单键仍达成基线目的：挡同一来源的在线爆破；令牌是 256 位随机值，不可枚举。
	key := refreshLimitKey(clientIP(r))
	// 先查锁再查库：已锁定的来源不必再付一次查询
	if !s.limiter.Allowed(key) {
		writeError(w, http.StatusTooManyRequests, "失败次数过多，已锁定 15 分钟")
		return
	}
	stored, err := s.db.GetRefreshToken(r.Context(), hashRefreshToken(req.RefreshToken))
	// 只把「查不到这条令牌」计为失败——那是爆破者的形态（token 猜出来的，库里必然无行）。
	// 合法用户持**已过期**令牌重试属正常生命周期，不计失败，否则等于惩罚自己人。
	if err != nil {
		s.limiter.Fail(key)
		writeError(w, http.StatusUnauthorized, "refresh_token 无效或已过期")
		return
	}
	if time.Now().After(stored.ExpiresAt) {
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
	// 刷新成功即清计数：避免长期在线用户的失败计数残留（与 Login 成功后 Reset 同款）
	s.limiter.Reset(key)
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
