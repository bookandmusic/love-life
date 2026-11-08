package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// CORS 守卫：浏览器预检必须放行分片上传自定义头，否则上传在浏览器里直接失败，
// 而 curl 测试不会触发预检，表现为「测试全绿、页面不通」。
func TestCORSPreflightAllowsUploadHeaders(t *testing.T) {
	h := cors(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/media/upload", nil)
	req.Header.Set("Origin", "http://tauri.localhost")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", rec.Code)
	}
	hdr := rec.Header()
	if got := hdr.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("allow-origin = %q", got)
	}
	allow := hdr.Get("Access-Control-Allow-Headers")
	for _, want := range []string{"Authorization", "Content-Type", "Range", hdrUploadID, hdrUploadOffset, hdrUploadFinal} {
		if !strings.Contains(allow, want) {
			t.Fatalf("allow-headers 缺 %q：%q", want, allow)
		}
	}
	if got := hdr.Get("Access-Control-Expose-Headers"); !strings.Contains(got, "Content-Disposition") {
		t.Fatalf("expose-headers 缺 Content-Disposition：%q", got)
	}

	// 非 /api 路径不参与 CORS（后台页面与 API 同源）。
	req = httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("非 /api 路径不应带 CORS 头：%q", got)
	}
}
