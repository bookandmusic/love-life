package api

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 未认证端点也必须先截断再解码：登录与刷新不带 Bearer，匿名请求塞大包同样能打爆内存。
func TestAnonymousLoginRejectsOversizeBody(t *testing.T) {
	ts := newTestServer(t)
	body := bytes.Repeat([]byte("a"), maxJSONBody+1)
	req, err := http.NewRequest("POST", ts.URL+"/api/v1/auth/login", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("超限登录体 status=%d，want 400", res.StatusCode)
	}
}

// 上传端点传的是文件，不能套 JSON 上限；其余端点一律受限于 1MB。
func TestLimitBodyExemptsUploadPathsOnly(t *testing.T) {
	var readErr error
	var read int64
	h := limitBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		read, readErr = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	for _, path := range []string{"/api/v1/media/upload", "/api/v1/admin/users/7/avatar"} {
		readErr, read = nil, 0
		req := httptest.NewRequest("POST", path, bytes.NewReader(make([]byte, maxJSONBody*2)))
		h.ServeHTTP(httptest.NewRecorder(), req)
		if readErr != nil {
			t.Fatalf("%s 请求体被截断: %v", path, readErr)
		}
		if read != maxJSONBody*2 {
			t.Fatalf("%s 只读到 %d 字节，want %d", path, read, maxJSONBody*2)
		}
	}
}

// 上限按路径判断，伪造 Content-Type 绕不过去：multipart 头 + 超大 JSON 体照样被截断。
func TestLimitBodyIgnoresContentType(t *testing.T) {
	var readErr error
	h := limitBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.Copy(io.Discard, r.Body)
	}))
	req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(make([]byte, maxJSONBody+1)))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	h.ServeHTTP(httptest.NewRecorder(), req)
	var maxErr *http.MaxBytesError
	if !errors.As(readErr, &maxErr) {
		t.Fatalf("want MaxBytesError，实际 %v", readErr)
	}
}
