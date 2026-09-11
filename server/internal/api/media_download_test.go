package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// mediaURL 取媒体当前签名地址（相对路径拼上测试服务地址）。
func mediaURL(t *testing.T, ts *testEnv, token string, mediaID int64) string {
	t.Helper()
	res := doJSON(t, "POST", ts.URL+"/api/v1/media/reurl", token, map[string]any{"ids": []int64{mediaID}})
	defer res.Body.Close()
	var out struct {
		Items []struct {
			URL string `json:"url"`
		} `json:"items"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil || len(out.Items) == 0 || out.Items[0].URL == "" {
		t.Fatalf("reurl: status=%d items=%d err=%v", res.StatusCode, len(out.Items), err)
	}
	return ts.URL + out.Items[0].URL
}

func getWithHeaders(t *testing.T, url string, hdr map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res, body
}

// TestMediaFileDownloadHeader download=1 出 Content-Disposition：
// 文件名 = {日期}_{照片|视频|录音}_{序号}.{ext}，中文走 RFC 5987 filename*。
// 跨域下 <a download> 的自定义名会被浏览器忽略，浏览器/WebView 只认这个头。
func TestMediaFileDownloadHeader(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	mediaID := newMediaPost(t, ts, adminToken)
	raw := mediaURL(t, ts, adminToken, mediaID)

	// 默认（不带 download）：内联预览，不应出 Content-Disposition
	res, body := getWithHeaders(t, raw, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("inline status=%d", res.StatusCode)
	}
	if cd := res.Header.Get("Content-Disposition"); cd != "" {
		t.Fatalf("inline 不应带 Content-Disposition: %q", cd)
	}
	if res.Header.Get("Content-Length") == "" || len(body) == 0 {
		t.Fatalf("应带 Content-Length 且体非空: len=%d", len(body))
	}
	// 跨域下 JS 只有被 Expose 的头才读得到，APP 靠它拿服务端文件名
	if !strings.Contains(res.Header.Get("Access-Control-Expose-Headers"), "Content-Disposition") {
		t.Fatalf("CORS 需暴露 Content-Disposition: %q", res.Header.Get("Access-Control-Expose-Headers"))
	}

	// download=1：attachment + UTF-8 文件名（照片 = %E7%85%A7%E7%89%87）
	date := time.Now().Format("2006-01-02")
	res, body2 := getWithHeaders(t, raw+"&download=1", nil)
	cd := res.Header.Get("Content-Disposition")
	// ASCII 兜底名去掉中文并压掉重复下划线；filename* 才是浏览器实际用的名字
	want := "attachment; filename=\"" + date + "_1.jpg\"; filename*=UTF-8''" + date + "_%E7%85%A7%E7%89%87_1.jpg"
	if cd != want {
		t.Fatalf("Content-Disposition=\n%q\nwant\n%q", cd, want)
	}
	if len(body2) != len(body) {
		t.Fatalf("下载体应与内联一致: %d vs %d", len(body2), len(body))
	}
}

// TestMediaFileRange serveFile 改走 http.ServeContent 后支持 Range：
// 大视频/大音频另存可续传、播放器可拖进度（原来 io.Copy 一次性灌完）。
func TestMediaFileRange(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	mediaID := newMediaPost(t, ts, adminToken)
	raw := mediaURL(t, ts, adminToken, mediaID)

	full, all := getWithHeaders(t, raw, nil)
	if full.StatusCode != http.StatusOK {
		t.Fatalf("full status=%d", full.StatusCode)
	}
	if ar := full.Header.Get("Accept-Ranges"); ar != "bytes" {
		t.Fatalf("Accept-Ranges=%q，期望 bytes", ar)
	}

	res, part := getWithHeaders(t, raw, map[string]string{"Range": "bytes=0-3"})
	if res.StatusCode != http.StatusPartialContent {
		t.Fatalf("Range 状态码=%d，期望 206", res.StatusCode)
	}
	if cr := res.Header.Get("Content-Range"); !strings.HasPrefix(cr, "bytes 0-3/") {
		t.Fatalf("Content-Range=%q", cr)
	}
	if len(part) != 4 || string(part) != string(all[:4]) {
		t.Fatalf("分片内容不对: len=%d", len(part))
	}

	// 越界 Range → 416（ServeContent 语义，客户端据此可知文件已变）
	res416, _ := getWithHeaders(t, raw, map[string]string{"Range": "bytes=99999999-"})
	if res416.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("越界 Range 状态码=%d，期望 416", res416.StatusCode)
	}
}