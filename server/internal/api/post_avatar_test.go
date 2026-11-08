package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// 动态列表必须下发作者头像路径：动态卡的头像图靠它，缺了只能显示首字色块。
func TestPostResponseCarriesAuthorAvatar(t *testing.T) {
	ts, adminToken, girlToken := newPostTestEnv(t)

	// 给发帖人 girl 写一个头像路径（真实上传走独立通道，这里只验下发口径）。
	if _, err := ts.db.ExecContext(t.Context(),
		`UPDATE users SET avatar_path = ? WHERE username = 'girl'`, "avatars/2026/11/x.jpg"); err != nil {
		t.Fatalf("set avatar: %v", err)
	}

	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", girlToken, map[string]any{"content": "带头像的动态"})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create post: %d", res.StatusCode)
	}

	res = doJSON(t, "GET", ts.URL+"/api/v1/posts?page=1&page_size=10", adminToken, nil)
	defer res.Body.Close()
	var list struct {
		Items []struct {
			Author       string `json:"author"`
			AuthorAvatar string `json:"author_avatar"`
		} `json:"items"`
	}
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("期望 1 条动态，实际 %d", len(list.Items))
	}
	// 下发的是带签名的 URL（裸路径会 401），必须同时含 exp 与 sig 参数
	got := list.Items[0].AuthorAvatar
	if got == "" {
		t.Fatal("有头像的用户必须下发 author_avatar")
	}
	if !strings.Contains(got, "exp=") || !strings.Contains(got, "sig=") {
		t.Fatalf("author_avatar 必须是签名 URL（含 exp / sig），实际 %q", got)
	}

	// 无头像的用户下发空串（客户端据此回退首字）
	if _, err := ts.db.ExecContext(t.Context(), `UPDATE users SET avatar_path = NULL WHERE username = 'girl'`); err != nil {
		t.Fatalf("clear avatar: %v", err)
	}
	hits, _, err := ts.db.ListPosts(t.Context(), 1, 10, false)
	if err != nil {
		t.Fatalf("ListPosts: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("应能读回动态")
	}
	if hits[0].AuthorAvatar.Valid {
		t.Fatalf("清空后 author_avatar 应为 NULL/空，实际 %v", hits[0].AuthorAvatar)
	}
}
