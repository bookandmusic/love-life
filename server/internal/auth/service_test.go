package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/store"
)

func newTestService(t *testing.T) (*Service, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	hash, err := HashPassword("right-password")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if _, err := db.CreateUser(context.Background(), &store.User{
		Username: "admin", PasswordHash: hash, Role: "admin", Nickname: "管理员",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}

	tm, err := LoadOrCreateSecret(dir)
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	svc := NewService(db, tm)
	return svc, httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			svc.Login(w, r)
		case "/refresh":
			svc.Refresh(w, r)
		case "/logout":
			svc.Logout(w, r)
		case "/me":
			svc.RequireAuth(http.HandlerFunc(svc.Me)).ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
}

func post(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	res, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	return res
}

func decode(t *testing.T, res *http.Response, v any) {
	t.Helper()
	defer res.Body.Close()
	if err := json.NewDecoder(res.Body).Decode(v); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestLoginLockRefreshFlow(t *testing.T) {
	_, ts := newTestService(t)
	defer ts.Close()

	// 正确登录（admin 后台口径）
	res := post(t, ts.URL+"/login", map[string]string{"username": "admin", "password": "right-password", "client": "admin"})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login: want 200, got %d", res.StatusCode)
	}
	var tok struct {
		Token        string `json:"token"`
		RefreshToken string `json:"refresh_token"`
		User struct {
			Role              string `json:"role"`
			Birthday          string `json:"birthday"`
			AIStyle           string `json:"ai_style"`
			ChatRetentionDays int    `json:"chat_retention_days"`
		} `json:"user"`
	}
	decode(t, res, &tok)
	if tok.Token == "" || tok.RefreshToken == "" || tok.User.Role != "admin" {
		t.Fatalf("login response incomplete: %+v", tok)
	}
	// AI 偏好与生日随 user 返回（新建用户走 DB 默认值，D9）
	if tok.User.AIStyle != "文艺" || tok.User.ChatRetentionDays != 30 || tok.User.Birthday != "" {
		t.Fatalf("login user preferences: %+v", tok.User)
	}

	// /me 受保护：带 token 可访问，且返回 AI 偏好字段（D9）
	req, _ := http.NewRequest("GET", ts.URL+"/me", nil)
	req.Header.Set("Authorization", "Bearer "+tok.Token)
	meRes, err := http.DefaultClient.Do(req)
	if err != nil || meRes.StatusCode != http.StatusOK {
		t.Fatalf("me with token failed: %v", meRes.Status)
	}
	var me struct {
		Birthday          string `json:"birthday"`
		AIStyle           string `json:"ai_style"`
		ChatRetentionDays int    `json:"chat_retention_days"`
	}
	_ = json.NewDecoder(meRes.Body).Decode(&me)
	meRes.Body.Close()
	if me.AIStyle != "文艺" || me.ChatRetentionDays != 30 || me.Birthday != "" {
		t.Fatalf("me preferences: %+v", me)
	}

	// 错误密码连续 5 次 → 锁定（第 6 次尝试正确密码也被拒）
	for i := 0; i < 5; i++ {
		res = post(t, ts.URL+"/login", map[string]string{"username": "admin", "password": "wrong", "client": "admin"})
		res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("wrong password #%d: want 401, got %d", i+1, res.StatusCode)
		}
	}
	res = post(t, ts.URL+"/login", map[string]string{"username": "admin", "password": "right-password", "client": "admin"})
	res.Body.Close()
	if res.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("after 5 fails: want 429, got %d", res.StatusCode)
	}

	// refresh 轮换：新 token 可用，旧 token 立即作废
	res = post(t, ts.URL+"/refresh", map[string]string{"refresh_token": tok.RefreshToken})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("refresh: want 200, got %d", res.StatusCode)
	}
	var tok2 struct {
		Token        string `json:"token"`
		RefreshToken string `json:"refresh_token"`
		User         struct {
			Birthday          string `json:"birthday"`
			AIStyle           string `json:"ai_style"`
			ChatRetentionDays int    `json:"chat_retention_days"`
		} `json:"user"`
	}
	decode(t, res, &tok2)
	// refresh 换发同样携带完整 user（与 login 同源组装，D9）
	if tok2.User.AIStyle != "文艺" || tok2.User.ChatRetentionDays != 30 || tok2.User.Birthday != "" {
		t.Fatalf("refresh user preferences: %+v", tok2.User)
	}
	res = post(t, ts.URL+"/refresh", map[string]string{"refresh_token": tok.RefreshToken})
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old refresh token reuse: want 401, got %d", res.StatusCode)
	}

	// 登出后 refresh 不可用
	res = post(t, ts.URL+"/logout", map[string]string{"refresh_token": tok2.RefreshToken})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("logout: want 200, got %d", res.StatusCode)
	}
	res = post(t, ts.URL+"/refresh", map[string]string{"refresh_token": tok2.RefreshToken})
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("refresh after logout: want 401, got %d", res.StatusCode)
	}
}

func TestNonAdminCannotLoginAdminClient(t *testing.T) {
	svc, ts := newTestService(t)
	defer ts.Close()

	hash, _ := HashPassword("pw")
	_, err := svc.db.CreateUser(context.Background(), &store.User{Username: "girl", PasswordHash: hash, Role: "girl"})
	if err != nil {
		t.Fatalf("create girl: %v", err)
	}

	res := post(t, ts.URL+"/login", map[string]string{"username": "girl", "password": "pw", "client": "admin"})
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("girl login admin client: want 403, got %d", res.StatusCode)
	}

	// APP 口径可正常登录
	res = post(t, ts.URL+"/login", map[string]string{"username": "girl", "password": "pw", "client": "app"})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("girl login app client: want 200, got %d", res.StatusCode)
	}
}

func TestSecretPersistsAcrossRestarts(t *testing.T) {
	dir := t.TempDir()
	tm1, err := LoadOrCreateSecret(dir)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	tm2, err := LoadOrCreateSecret(filepath.Join(dir))
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	token, err := tm1.IssueAccessToken(1, "admin", "admin")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := tm2.ParseAccessToken(token); err != nil {
		t.Fatalf("token from first secret should validate with second: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "jwt_secret")); err != nil {
		t.Fatalf("secret file missing: %v", err)
	}
}
