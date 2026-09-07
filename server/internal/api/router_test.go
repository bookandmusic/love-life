package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/bookandmusic/love-life/server/internal/auth"
	"github.com/bookandmusic/love-life/server/internal/config"
	"github.com/bookandmusic/love-life/server/internal/media"
	"github.com/bookandmusic/love-life/server/internal/notify"
	"github.com/bookandmusic/love-life/server/internal/store"
)

type testEnv struct {
	*httptest.Server
	db *store.DB
	tm *auth.TokenManager
}

func newTestServer(t *testing.T) *testEnv {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	hash, _ := auth.HashPassword("admin-pw")
	if _, err := db.CreateUser(t.Context(), &store.User{Username: "admin", PasswordHash: hash, Role: "admin", Nickname: "管理员"}); err != nil {
		t.Fatalf("create admin: %v", err)
	}

	tm, err := auth.LoadOrCreateSecret(t.TempDir())
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	mp := media.New(t.TempDir(), tm.Secret(), 500<<20)
	dp := notify.New(db)
	webuiFS := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>placeholder</html>")}}
	ts := httptest.NewServer(NewRouter(config.Config{}, db, tm, mp, dp, webuiFS))
	t.Cleanup(ts.Close)
	return &testEnv{Server: ts, db: db, tm: tm}
}

func doJSON(t *testing.T, method, url, token string, body any) *http.Response {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	return res
}

func loginToken(t *testing.T, baseURL, username, password, client string) (string, int) {
	t.Helper()
	res := doJSON(t, "POST", baseURL+"/api/v1/auth/login", "", map[string]string{
		"username": username, "password": password, "client": client,
	})
	defer res.Body.Close()
	var out struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	return out.Token, res.StatusCode
}

func TestAdminUserManagement(t *testing.T) {
	ts := newTestServer(t)

	adminToken, code := loginToken(t, ts.URL, "admin", "admin-pw", "admin")
	if code != http.StatusOK {
		t.Fatalf("admin login: %d", code)
	}

	// couple 原子创建 girl/boy（AD9，契约对齐 mock-check C1-C4）
	res := doJSON(t, "POST", ts.URL+"/api/v1/admin/users/couple", adminToken, map[string]any{
		"girl": map[string]string{"username": "girl", "password": "girl-pw", "nickname": "她"},
		"boy":  map[string]string{"username": "boy", "password": "boy-pw", "nickname": "他"},
	})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("couple create: want 201, got %d", res.StatusCode)
	}

	// C1 用户名长度 → 400（格式校验先于存在性判断）
	res = doJSON(t, "POST", ts.URL+"/api/v1/admin/users/couple", adminToken, map[string]any{
		"girl": map[string]string{"username": "a", "password": "pw-123456"},
		"boy":  map[string]string{"username": "bb", "password": "pw-123456"},
	})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("couple username length: want 400, got %d", res.StatusCode)
	}

	// C4 情侣已存在优先于用户名冲突 → 400（不得 409）
	res = doJSON(t, "POST", ts.URL+"/api/v1/admin/users/couple", adminToken, map[string]any{
		"girl": map[string]string{"username": "admin", "password": "pw-123456"},
		"boy":  map[string]string{"username": "b2", "password": "pw-123456"},
	})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("couple exists precedence: want 400, got %d", res.StatusCode)
	}

	// 单创接口已随 Q6 删除 → 405（Go 1.22 method pattern 下路径存在但方法不匹配）
	res = doJSON(t, "POST", ts.URL+"/api/v1/admin/users", adminToken, map[string]string{
		"username": "solo", "password": "pw-123456", "role": "girl",
	})
	res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("single create removed: want 405, got %d", res.StatusCode)
	}

	// 列表：admin + girl + boy = 3
	res = doJSON(t, "GET", ts.URL+"/api/v1/admin/users", adminToken, nil)
	var users []map[string]any
	_ = json.NewDecoder(res.Body).Decode(&users)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || len(users) != 3 {
		t.Fatalf("list users: status=%d count=%d", res.StatusCode, len(users))
	}
	for _, u := range users {
		if _, leaked := u["password_hash"]; leaked {
			t.Fatal("list must not leak password_hash")
		}
	}

	// PUT 昵称：仅昵称可改，返回更新后条目
	res = doJSON(t, "PUT", ts.URL+"/api/v1/admin/users/2", adminToken, map[string]string{"nickname": "她改"})
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		t.Fatalf("update nickname: want 200, got %d", res.StatusCode)
	}
	var updated map[string]any
	_ = json.NewDecoder(res.Body).Decode(&updated)
	res.Body.Close()
	if updated["nickname"] != "她改" || updated["username"] != "girl" || updated["role"] != "girl" {
		t.Fatalf("update nickname response: %+v", updated)
	}

	// PUT 昵称：空昵称 → 400；用户不存在 → 404
	res = doJSON(t, "PUT", ts.URL+"/api/v1/admin/users/2", adminToken, map[string]string{"nickname": "  "})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("blank nickname: want 400, got %d", res.StatusCode)
	}
	res = doJSON(t, "PUT", ts.URL+"/api/v1/admin/users/999", adminToken, map[string]string{"nickname": "x"})
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("nickname unknown user: want 404, got %d", res.StatusCode)
	}

	// 重置密码后新密码可登录
	res = doJSON(t, "POST", ts.URL+"/api/v1/admin/users/2/reset-password", adminToken, map[string]string{"password": "new-pw-9"})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("reset password: %d", res.StatusCode)
	}
	if _, code := loginToken(t, ts.URL, "girl", "new-pw-9", "app"); code != http.StatusOK {
		t.Fatalf("girl login with new password: %d", code)
	}

	// girl 访问后台成员接口 → 403
	girlToken, code := loginToken(t, ts.URL, "girl", "new-pw-9", "app")
	if code != http.StatusOK {
		t.Fatalf("girl login: %d", code)
	}
	res = doJSON(t, "GET", ts.URL+"/api/v1/admin/users", girlToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("girl access admin api: want 403, got %d", res.StatusCode)
	}

	// 未认证访问 → 401
	res = doJSON(t, "GET", ts.URL+"/api/v1/admin/users", "", nil)
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous access: want 401, got %d", res.StatusCode)
	}
}
