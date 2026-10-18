package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 空文件拒收的守卫用例（ARCHITECTURE「媒体上传管线」· 格式与大小）：
// 0 字节的 video / audio 曾因「容器头解析失败不阻塞上传」被静默收下，落库成 0:00 的假条目；
// 照片则是靠缩略图解码失败偶然挡住。三类必须在收尾处同一口径拒绝。

// TestUploadEmptyFileRejected 三种扩展名各传一次空文件：一律 400 + 面向用户文案，且临时区无残留。
func TestUploadEmptyFileRejected(t *testing.T) {
	ts, adminToken := newPostTestEnvWithLimit(t, testMediaLimit)

	for _, name := range []string{"empty.webm", "empty.wav", "empty.jpg", "empty.mp4", "empty.mp3"} {
		t.Run(name, func(t *testing.T) {
			res := uploadRaw(t, ts.URL, adminToken, name, nil)
			defer res.Body.Close()
			if res.StatusCode != http.StatusBadRequest {
				t.Fatalf("code=%d want 400", res.StatusCode)
			}
			var out struct {
				Error string `json:"error"`
			}
			_ = json.NewDecoder(res.Body).Decode(&out)
			if !strings.Contains(out.Error, "空的") {
				t.Fatalf("文案应说明文件是空的（面向用户），实际 %q", out.Error)
			}
		})
	}

	// 拒收即清理：不能让空文件留在临时区等 janitor 兜底
	entries, err := os.ReadDir(filepath.Join(ts.dataDir, "media", "tmp"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read tmp: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("空文件拒收后临时区残留 %d 项", len(entries))
	}
}

// TestUploadNonEmptyBrokenContainerAccepted 边界守卫：**只拒空文件**。
// 非空但容器头损坏的 webm/wav 仍必须通过——ARCHITECTURE 明确「解析失败不阻塞上传、
// duration 留空」，把它升级成拒收会破坏既有口径（前端时长角标留空即可）。
func TestUploadNonEmptyBrokenContainerAccepted(t *testing.T) {
	ts, adminToken := newPostTestEnvWithLimit(t, testMediaLimit)

	for _, name := range []string{"broken.webm", "broken.wav"} {
		t.Run(name, func(t *testing.T) {
			token, code := uploadMedia(t, ts.URL, adminToken, name, []byte("not-a-real-container"))
			if code != http.StatusOK {
				t.Fatalf("code=%d want 200（非空文件不得因容器头解析失败被拒）", code)
			}
			if token == "" {
				t.Fatal("应返回 media_token")
			}
		})
	}
}

// TestChunkedUploadEmptyFinalRejected 分片路径同口径：首片即末片传空文件也必须拒。
func TestChunkedUploadEmptyFinalRejected(t *testing.T) {
	ts, adminToken := newPostTestEnvWithLimit(t, testMediaLimit)

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "empty.webm")
	_, _ = fw.Write(nil)
	_ = mw.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/media/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("X-Upload-Offset", "0")
	req.Header.Set("X-Upload-Final", "1")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("code=%d want 400", res.StatusCode)
	}
}
