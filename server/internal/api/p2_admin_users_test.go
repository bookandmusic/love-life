package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"testing"
)

// P2-19：admin 账号只读——改昵称 / 重置密码 / 换头像都必须被拒，且昵称确实没被改写。
// 前端不给 admin 行渲染按钮，但契约守卫必须在后端（不能只靠不渲染）。
func TestAdminAccountIsReadOnly(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	ctx := t.Context()

	var before string
	if err := ts.db.QueryRowContext(ctx, `SELECT nickname FROM users WHERE role = 'admin'`).Scan(&before); err != nil {
		t.Fatal(err)
	}

	// 改昵称 → 403
	res := doJSON(t, "PUT", ts.URL+"/api/v1/admin/users/1", adminToken, map[string]string{"nickname": "管理员X"})
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("改 admin 昵称：want 403, got %d", res.StatusCode)
	}
	var after string
	if err := ts.db.QueryRowContext(ctx, `SELECT nickname FROM users WHERE role = 'admin'`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("admin 昵称被改写：%q → %q", before, after)
	}

	// 换头像 → 403（同款 multipart 通道，字段名 avatar）
	var abuf bytes.Buffer
	amw := multipart.NewWriter(&abuf)
	afw, _ := amw.CreateFormFile("avatar", "a.jpg")
	_, _ = afw.Write(genJPEG(t))
	_ = amw.Close()
	areq, _ := http.NewRequest("POST", ts.URL+"/api/v1/admin/users/1/avatar", &abuf)
	areq.Header.Set("Content-Type", amw.FormDataContentType())
	areq.Header.Set("Authorization", "Bearer "+adminToken)
	ares, err := http.DefaultClient.Do(areq)
	if err != nil {
		t.Fatal(err)
	}
	ares.Body.Close()
	if ares.StatusCode != http.StatusForbidden {
		t.Fatalf("改 admin 头像：want 403, got %d", ares.StatusCode)
	}

	// 重置密码 → 403（并确认原密码仍可用）
	res = doJSON(t, "POST", ts.URL+"/api/v1/admin/users/1/reset-password", adminToken, map[string]string{"password": "hacked-pw"})
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("重置 admin 密码：want 403, got %d", res.StatusCode)
	}
	if _, code := loginToken(t, ts.URL, "admin", "admin-pw", "admin"); code != http.StatusOK {
		t.Fatalf("admin 原密码应仍可用，登录码 %d", code)
	}

	// 情侣角色仍可改（守卫不是把整条路径关死）
	res = doJSON(t, "PUT", ts.URL+"/api/v1/admin/users/2", adminToken, map[string]string{"nickname": "她改"})
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		t.Fatalf("改 girl 昵称：want 200, got %d", res.StatusCode)
	}
	res.Body.Close()
}

// P2-18：PUT /admin/users/{id} 承载生日（省略则不改，空串清除，格式非法拒绝）。
// 独立端点仍在（兼容），但主端点必须能改——原先只解 nickname，生日被静默忽略。
func TestAdminUserUpdateCarriesBirthday(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	// 带生日 + 昵称：两者都生效
	res := doJSON(t, "PUT", ts.URL+"/api/v1/admin/users/2", adminToken, map[string]any{
		"nickname": "她", "birthday": "2000-01-01",
	})
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || out["birthday"] != "2000-01-01" {
		t.Fatalf("同端点改生日：status=%d out=%+v", res.StatusCode, out)
	}
	var bd string
	if err := ts.db.QueryRow(`SELECT COALESCE(birthday,'') FROM users WHERE id = 2`).Scan(&bd); err != nil {
		t.Fatal(err)
	}
	if bd != "2000-01-01" {
		t.Fatalf("库里生日 = %q", bd)
	}

	// 省略 birthday：只改昵称，生日保持
	res = doJSON(t, "PUT", ts.URL+"/api/v1/admin/users/2", adminToken, map[string]string{"nickname": "她2"})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("只改昵称：%d", res.StatusCode)
	}
	_ = ts.db.QueryRow(`SELECT COALESCE(birthday,'') FROM users WHERE id = 2`).Scan(&bd)
	if bd != "2000-01-01" {
		t.Fatalf("省略 birthday 不应改动，实得 %q", bd)
	}

	// 空串：清除
	res = doJSON(t, "PUT", ts.URL+"/api/v1/admin/users/2", adminToken, map[string]any{"nickname": "她2", "birthday": ""})
	res.Body.Close()
	_ = ts.db.QueryRow(`SELECT COALESCE(birthday,'') FROM users WHERE id = 2`).Scan(&bd)
	if bd != "" {
		t.Fatalf("空串应清除生日，实得 %q", bd)
	}

	// 格式非法：400，且**不得**把同请求里的昵称也改掉（先校验后落库）
	res = doJSON(t, "PUT", ts.URL+"/api/v1/admin/users/2", adminToken, map[string]any{"nickname": "不该生效", "birthday": "2000/01/01"})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("非法生日格式：want 400, got %d", res.StatusCode)
	}
	var nick string
	if err := ts.db.QueryRow(`SELECT nickname FROM users WHERE id = 2`).Scan(&nick); err != nil {
		t.Fatal(err)
	}
	if nick != "她2" {
		t.Fatalf("生日校验失败时昵称不得被改：%q（want 她2）", nick)
	}
}
