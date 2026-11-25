package api

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// 渠道入参的 JSON 解不开是**请求形状**问题，必须 400 并回一句可读的话；
// 早先这里直接把 encoding/json 的错误往上抛，落进 500 分支还会把 Go 术语漏给客户端。
func TestWebhookMalformedJSONIsBadRequest(t *testing.T) {
	ts, _, _ := newPostTestEnv(t)
	adminToken, code := loginToken(t, ts.URL, "admin", "admin-pw", "admin")
	if code != http.StatusOK {
		t.Fatalf("admin login: %d", code)
	}

	for _, body := range []string{
		`{`,               // 截断
		`{"name": "x", }`, // 尾逗号
		`[1,2,3]`,         // 形状不对：对象位置给了数组
	} {
		req, err := http.NewRequest("POST", ts.URL+"/api/v1/admin/webhooks", strings.NewReader(body))
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+adminToken)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("do request: %v", err)
		}
		raw, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("body=%s 应 400，实得 %d：%s", body, res.StatusCode, raw)
		}
		if !strings.Contains(string(raw), "请求格式错误") {
			t.Errorf("body=%s 应回可读错误，实得：%s", body, raw)
		}
	}
}

// 纯媒体 / 纯地点动态没有正文：推送句子不能以冒号结尾。
// 这条走完整的 REST → Dispatch 链路，只测 notify.Posted 会漏掉「调用方把空正文传进来了」。
func TestPostCreatedPushWithoutContentHasNoTrailingColon(t *testing.T) {
	ts, _, girlToken := newPostTestEnv(t)
	realAdmin, _ := loginToken(t, ts.URL, "admin", "admin-pw", "admin")
	rec := newNotifyRecorder(t)

	res := doJSON(t, "POST", ts.URL+"/api/v1/admin/webhooks", realAdmin, map[string]any{
		"name": "群", "url": rec.srv.URL + "/group", "events": []string{"new_post"},
	})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create channel: %d", res.StatusCode)
	}

	// 只带心情、没有文字：服务端允许（「至少填写文字、心情、地点或媒体」）
	res = doJSON(t, "POST", ts.URL+"/api/v1/posts", girlToken, map[string]any{"content": "", "mood": "开心"})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create post: %d", res.StatusCode)
	}

	if !rec.waitFor("/group", 1, 3*time.Second) {
		t.Fatalf("新动态未推送：%v", rec.texts("/group"))
	}
	got := rec.texts("/group")[0]
	if strings.HasSuffix(got, "：") {
		t.Fatalf("无正文动态的推送不得以冒号结尾：%q", got)
	}
	// 环境里 girl 昵称「她」
	if want := "她 发布了新动态"; got != want {
		t.Fatalf("推送正文 = %q，want %q", got, want)
	}
}

// 有正文时仍要带「：正文」，别把守卫写成永远不带正文。
func TestPostCreatedPushWithContentKeepsBody(t *testing.T) {
	ts, _, girlToken := newPostTestEnv(t)
	realAdmin, _ := loginToken(t, ts.URL, "admin", "admin-pw", "admin")
	rec := newNotifyRecorder(t)

	res := doJSON(t, "POST", ts.URL+"/api/v1/admin/webhooks", realAdmin, map[string]any{
		"name": "群", "url": rec.srv.URL + "/group", "events": []string{"new_post"},
	})
	res.Body.Close()

	res = doJSON(t, "POST", ts.URL+"/api/v1/posts", girlToken, map[string]any{"content": "晚霞"})
	res.Body.Close()

	if !rec.waitFor("/group", 1, 3*time.Second) {
		t.Fatalf("新动态未推送")
	}
	if want := "她 发布了新动态：晚霞"; rec.texts("/group")[0] != want {
		t.Fatalf("推送正文 = %q，want %q", rec.texts("/group")[0], want)
	}
}
