package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"strconv"
	"testing"
)

// TestGAP16AvatarBirthday 覆盖头像上传、生日更新、头像公开读取、签名校验（独立通道）。
func TestGAP16AvatarBirthday(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	// newPostTestEnv 已创建 admin(1) + girl(2)；直接对 girl 测试头像与生日
	const girlID = 2
	var res *http.Response

	// 1) 上传头像（multipart）
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("avatar", "avatar.jpg")
	_, _ = fw.Write(genJPEG(t))
	_ = mw.Close()
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/admin/users/"+strconv.FormatInt(girlID, 10)+"/avatar", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+adminToken)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("avatar upload: %v", err)
	}
	var up struct {
		Status string `json:"status"`
		Avatar string `json:"avatar"`
	}
	_ = json.NewDecoder(r.Body).Decode(&up)
	r.Body.Close()
	if r.StatusCode != http.StatusOK || up.Avatar == "" {
		t.Fatalf("avatar upload: status=%d avatar=%q", r.StatusCode, up.Avatar)
	}

	// 2) listUsers 应返回 avatar + birthday
	res = doJSON(t, "GET", ts.URL+"/api/v1/admin/users", adminToken, nil)
	var users []map[string]any
	_ = json.NewDecoder(res.Body).Decode(&users)
	res.Body.Close()
	var girlEntry map[string]any
	for _, u := range users {
		if u["role"] == "girl" {
			girlEntry = u
		}
	}
	if girlEntry == nil {
		t.Fatal("girl missing from listUsers")
	}
	if girlEntry["avatar"] == "" || girlEntry["avatar"] == nil {
		t.Fatalf("girl avatar missing after upload: %+v", girlEntry)
	}

	// 3) 设置生日
	res = doJSON(t, "PUT", ts.URL+"/api/v1/admin/users/"+strconv.FormatInt(girlID, 10)+"/birthday", adminToken, map[string]string{"birthday": "1998-06-15"})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("set birthday: %d", res.StatusCode)
	}
	// 生日格式错误 → 400
	res = doJSON(t, "PUT", ts.URL+"/api/v1/admin/users/"+strconv.FormatInt(girlID, 10)+"/birthday", adminToken, map[string]string{"birthday": "98/6/15"})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad birthday: want 400, got %d", res.StatusCode)
	}

	// 4) /space 不应暴露 birthday（仅后台）
	res = doJSON(t, "GET", ts.URL+"/api/v1/space", adminToken, nil)
	var space struct {
		Members []map[string]any `json:"members"`
	}
	_ = json.NewDecoder(res.Body).Decode(&space)
	res.Body.Close()
	for _, m := range space.Members {
		if _, leaked := m["birthday"]; leaked {
			t.Fatalf("birthday must NOT leak to APP /space: %+v", m)
		}
	}

	// 5) 头像公开读取：签名即凭证（不带签名 → 401）
	req, _ = http.NewRequest("GET", ts.URL+"/api/v1/avatar/"+strconv.FormatInt(girlID, 10), nil)
	r, _ = http.DefaultClient.Do(req)
	r.Body.Close()
	if r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("avatar unsigned: want 401, got %d", r.StatusCode)
	}
	// 带正确签名 → 200 image
	req, _ = http.NewRequest("GET", ts.URL+up.Avatar, nil)
	r, _ = http.DefaultClient.Do(req)
	r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("avatar signed: want 200, got %d", r.StatusCode)
	}
	if ct := r.Header.Get("Content-Type"); ct != "image/jpeg" {
		t.Fatalf("avatar content-type: %q", ct)
	}

	// 6) 清空生日
	res = doJSON(t, "PUT", ts.URL+"/api/v1/admin/users/"+strconv.FormatInt(girlID, 10)+"/birthday", adminToken, map[string]string{"birthday": ""})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("clear birthday: %d", res.StatusCode)
	}
}
