package api

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// 分片上传（断点续传）的守卫用例（ARCHITECTURE「媒体上传管线」）：
// 无头兼容、首片即末片、offset 重同步、末片幂等、归属校验、累计判限、半成品清理。
// 片大小在测试里用 testMediaLimit/4，让一个几 KB 的文件就能覆盖多片路径。

// chunkUpload 发一片：id 为空表示首片；final 表示末片。返回响应与状态码。
func chunkUpload(t *testing.T, baseURL, token, filename string, content []byte,
	uploadID string, offset int64, final bool) (*http.Response, map[string]any) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", filename)
	_, _ = fw.Write(content)
	_ = mw.Close()

	req, _ := http.NewRequest("POST", baseURL+"/api/v1/media/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Upload-Offset", strconv.FormatInt(offset, 10))
	if uploadID != "" {
		req.Header.Set("X-Upload-Id", uploadID)
	}
	if final {
		req.Header.Set("X-Upload-Final", "1")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("chunk upload: %v", err)
	}
	var out map[string]any
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	_ = json.Unmarshal(b, &out)
	return res, out
}

// TestChunkedUploadMultiChunk 多片上传：3 片（含末片）→ 得 token，字节与源一致。
func TestChunkedUploadMultiChunk(t *testing.T) {
	ts, adminToken := newPostTestEnvWithLimit(t, testMediaLimit)
	chunk := testMediaLimit / 4 // 4 片
	// 音频扩展名：不生成缩略图，字节可直接比对（photo 会被重编码）
	src := bytes.Repeat([]byte("ABCD"), chunk) // 4*chunk 字节
	name := "song.mp3"

	var uploadID string
	for off := int64(0); off < int64(len(src)); off += int64(chunk) {
		end := off + int64(chunk)
		if end > int64(len(src)) {
			end = int64(len(src))
		}
		final := end == int64(len(src))
		res, out := chunkUpload(t, ts.URL, adminToken, name, src[off:end], uploadID, off, final)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("片 off=%d code=%d out=%v", off, res.StatusCode, out)
		}
		if final {
			tok, _ := out["media_token"].(string)
			if tok == "" {
				t.Fatalf("末片应回 media_token: %v", out)
			}
			// 落盘字节与源一致
			raw := filepath.Join(ts.dataDir, "media", "tmp", tok+".mp3")
			got, err := os.ReadFile(raw)
			if err != nil {
				t.Fatalf("读临时文件: %v", err)
			}
			if !bytes.Equal(got, src) {
				t.Fatalf("分片拼接后字节不一致: got %d want %d", len(got), len(src))
			}
			return
		}
		id, _ := out["upload_id"].(string)
		if id == "" {
			t.Fatalf("中间片应回 upload_id: %v", out)
		}
		uploadID = id
		if recv, _ := out["received"].(float64); int64(recv) != end {
			t.Fatalf("received=%v want %d", out["received"], end)
		}
	}
	t.Fatal("循环未走完")
}

// TestChunkedUploadSingleChunk 首片即末片（最常见：照片/录音/短视频）→ 直接回 token。
func TestChunkedUploadSingleChunk(t *testing.T) {
	ts, adminToken := newPostTestEnvWithLimit(t, testMediaLimit)
	res, out := chunkUpload(t, ts.URL, adminToken, "one.mp3", bytes.Repeat([]byte("X"), 100), "", 0, true)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("code=%d out=%v", res.StatusCode, out)
	}
	if tok, _ := out["media_token"].(string); tok == "" {
		t.Fatalf("首片即末片必须直接回 media_token（不是 upload_id）: %v", out)
	}
}

// TestChunkedOffsetMismatch409 offset 不符 → 409 且带服务端实际已收长度。
func TestChunkedOffsetMismatch409(t *testing.T) {
	ts, adminToken := newPostTestEnvWithLimit(t, testMediaLimit)
	chunk := testMediaLimit / 4

	// 首片
	res, out := chunkUpload(t, ts.URL, adminToken, "m.mp3", bytes.Repeat([]byte("A"), chunk), "", 0, false)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("首片 code=%d", res.StatusCode)
	}
	id, _ := out["upload_id"].(string)

	// 故意用落后 offset（0）再发一片 → 409 + received=chunk
	res2, out2 := chunkUpload(t, ts.URL, adminToken, "m.mp3", bytes.Repeat([]byte("B"), chunk), id, 0, false)
	if res2.StatusCode != http.StatusConflict {
		t.Fatalf("code=%d want 409", res2.StatusCode)
	}
	if recv, _ := out2["received"].(float64); int64(recv) != int64(chunk) {
		t.Fatalf("received=%v want %d（客户端据它重同步）", out2["received"], chunk)
	}
}

// TestChunkedFinalIdempotent 末片重发两次 → 两次都回可用结果（响应丢失不卡死）。
func TestChunkedFinalIdempotent(t *testing.T) {
	ts, adminToken := newPostTestEnvWithLimit(t, testMediaLimit)
	chunk := testMediaLimit / 4

	res, out := chunkUpload(t, ts.URL, adminToken, "i.mp3", bytes.Repeat([]byte("A"), chunk), "", 0, false)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("首片 code=%d", res.StatusCode)
	}
	id, _ := out["upload_id"].(string)

	// 末片（第 2 片即末片）
	res1, out1 := chunkUpload(t, ts.URL, adminToken, "i.mp3", bytes.Repeat([]byte("B"), chunk), id, int64(chunk), true)
	if res1.StatusCode != http.StatusOK {
		t.Fatalf("末片 code=%d out=%v", res1.StatusCode, out1)
	}
	tok1, _ := out1["media_token"].(string)
	if tok1 == "" {
		t.Fatalf("末片应回 token: %v", out1)
	}

	// 重发同一末片（offset 已落后）→ 幂等：仍回 token，不因 offset 不符回 409
	res2, out2 := chunkUpload(t, ts.URL, adminToken, "i.mp3", bytes.Repeat([]byte("B"), chunk), id, int64(chunk), true)
	if res2.StatusCode != http.StatusOK {
		t.Fatalf("末片重发 code=%d（应幂等回 200）: %v", res2.StatusCode, out2)
	}
	if tok2, _ := out2["media_token"].(string); tok2 != tok1 {
		t.Fatalf("末片重发应回同一个 token: %q vs %q", tok2, tok1)
	}
}

// TestChunkedOwnershipRejected 他人 upload_id 不可追加（归属校验先于一切）。
func TestChunkedOwnershipRejected(t *testing.T) {
	ts, adminToken, girlToken := newPostTestEnvWithLimit2(t, testMediaLimit)
	chunk := testMediaLimit / 4

	res, out := chunkUpload(t, ts.URL, adminToken, "o.mp3", bytes.Repeat([]byte("A"), chunk), "", 0, false)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("首片 code=%d", res.StatusCode)
	}
	id, _ := out["upload_id"].(string)

	// girl 拿 admin 的 upload_id 追加 → 拒绝
	res2, _ := chunkUpload(t, ts.URL, girlToken, "o.mp3", bytes.Repeat([]byte("B"), chunk), id, int64(chunk), false)
	if res2.StatusCode != http.StatusNotFound {
		t.Fatalf("他人 upload_id code=%d want 404", res2.StatusCode)
	}
	// admin 的半成品字节数未被改变
	raw := filepath.Join(ts.dataDir, "media", "tmp", id+".mp3")
	info, err := os.Stat(raw)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() != int64(chunk) {
		t.Fatalf("半成品被他人改动: size=%d want %d", info.Size(), chunk)
	}
}

// TestChunkedCumulativeTooLarge 累计超限 → 413，且半成品被删除。
func TestChunkedCumulativeTooLarge(t *testing.T) {
	ts, adminToken := newPostTestEnvWithLimit(t, testMediaLimit)
	chunk := testMediaLimit / 4

	res, out := chunkUpload(t, ts.URL, adminToken, "big.mp3", bytes.Repeat([]byte("A"), chunk), "", 0, false)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("首片 code=%d", res.StatusCode)
	}
	id, _ := out["upload_id"].(string)

	// 继续追加到超过上限：每次 offset 正确、但累计超限
	off := int64(chunk)
	var code int
	for i := 0; i < 6; i++ {
		res2, _ := chunkUpload(t, ts.URL, adminToken, "big.mp3", bytes.Repeat([]byte("B"), chunk), id, off, false)
		code = res2.StatusCode
		if code == http.StatusRequestEntityTooLarge {
			break
		}
		if code != http.StatusOK {
			t.Fatalf("中间片 code=%d", code)
		}
		off += int64(chunk)
	}
	if code != http.StatusRequestEntityTooLarge {
		t.Fatalf("累计超限应回 413，实际 %d", code)
	}
	// 半成品已清理
	if entries, _ := os.ReadDir(filepath.Join(ts.dataDir, "media", "tmp")); len(entries) != 0 {
		t.Fatalf("超限后临时区残留 %d 项", len(entries))
	}
}

// TestChunkedUploadNotFound 不存在的 upload_id → 404（客户端据此从头重传）。
func TestChunkedUploadNotFound(t *testing.T) {
	ts, adminToken := newPostTestEnvWithLimit(t, testMediaLimit)
	res, _ := chunkUpload(t, ts.URL, adminToken, "x.mp3", []byte("A"), "0123456789abcdef0123456789abcdef", 0, false)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("code=%d want 404", res.StatusCode)
	}
}

// TestChunkedDiscard 半成品可被 discard 撤销（归属校验对分片中途仍生效）。
func TestChunkedDiscard(t *testing.T) {
	ts, adminToken := newPostTestEnvWithLimit(t, testMediaLimit)
	chunk := testMediaLimit / 4

	res, out := chunkUpload(t, ts.URL, adminToken, "d.mp3", bytes.Repeat([]byte("A"), chunk), "", 0, false)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("首片 code=%d", res.StatusCode)
	}
	id, _ := out["upload_id"].(string)

	discard := doJSON(t, "POST", ts.URL+"/api/v1/media/discard", adminToken, map[string]any{"tokens": []string{id}})
	discard.Body.Close()
	if entries, _ := os.ReadDir(filepath.Join(ts.dataDir, "media", "tmp")); len(entries) != 0 {
		t.Fatalf("discard 后临时区残留 %d 项", len(entries))
	}
}
