package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/auth"
	"github.com/bookandmusic/love-life/server/internal/store"
)

// loginRefresh 登录并取回 refresh_token。
func loginRefresh(t *testing.T, baseURL, username, password, client string) string {
	t.Helper()
	res := doJSON(t, "POST", baseURL+"/api/v1/auth/login", "", map[string]string{
		"username": username, "password": password, "client": client,
	})
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login %s: %d", username, res.StatusCode)
	}
	var out struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode login %s: %v", username, err)
	}
	if out.RefreshToken == "" {
		t.Fatalf("login %s 未返回 refresh_token", username)
	}
	return out.RefreshToken
}

// A1：admin 重置密码后，该用户此前签发的 refresh token 必须立刻失效，且不误伤他人。
// 这是「重置密码」的实质——否则旧设备还能续 30 天。
func TestResetPasswordRevokesRefreshTokens(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	ctx := t.Context()

	// 造一个 boy 用户，用来验证「不误伤他人」
	hash, _ := auth.HashPassword("boy-pw")
	boyID, err := ts.db.CreateUser(ctx, &store.User{
		Username: "boy", PasswordHash: hash, Role: "boy", Nickname: "他",
	})
	if err != nil {
		t.Fatalf("create boy: %v", err)
	}

	// girl 登录两次（模拟两台设备）+ boy 登录一次
	girlTokens := []string{
		loginRefresh(t, ts.URL, "girl", "girl-pw", "app"),
		loginRefresh(t, ts.URL, "girl", "girl-pw", "app"),
	}
	boyToken := loginRefresh(t, ts.URL, "boy", "boy-pw", "app")

	var girlID int64
	if err := ts.db.QueryRowContext(ctx, `SELECT id FROM users WHERE username = 'girl'`).Scan(&girlID); err != nil {
		t.Fatal(err)
	}
	if girlID == boyID {
		t.Fatal("测试数据异常：girl 与 boy 是同一行")
	}

	res := doJSON(t, "POST", ts.URL+"/api/v1/admin/users/"+strconv.FormatInt(girlID, 10)+"/reset-password",
		adminToken, map[string]string{"password": "new-girl-pw"})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("reset password: %d", res.StatusCode)
	}

	for i, rt := range girlTokens {
		res := doJSON(t, "POST", ts.URL+"/api/v1/auth/refresh", "", map[string]string{"refresh_token": rt})
		res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("girl 旧 token #%d 应 401，实际 %d", i, res.StatusCode)
		}
	}

	res = doJSON(t, "POST", ts.URL+"/api/v1/auth/refresh", "", map[string]string{"refresh_token": boyToken})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("boy 的 token 不应被误删，实际 %d", res.StatusCode)
	}
}

// A1：登出接口吊销传入的 refresh token（此前该接口无任何调用方，属未接线）。
func TestLogoutRevokesRefreshToken(t *testing.T) {
	ts, _, _ := newPostTestEnv(t)

	rt := loginRefresh(t, ts.URL, "girl", "girl-pw", "app")

	res := doJSON(t, "POST", ts.URL+"/api/v1/auth/logout", "", map[string]string{"refresh_token": rt})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("logout 应 200，实际 %d", res.StatusCode)
	}

	res = doJSON(t, "POST", ts.URL+"/api/v1/auth/refresh", "", map[string]string{"refresh_token": rt})
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("登出后 refresh 应 401，实际 %d", res.StatusCode)
	}
}
