package api

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

// buildTestWAV 最小可解析 WAV（字节率 8000，1 秒数据）。
func buildTestWAV(t *testing.T) []byte {
	t.Helper()
	bs := []byte("RIFF\x00\x00\x00\x00WAVE")
	fmt := []byte("fmt ")
	fmt = binary.LittleEndian.AppendUint32(fmt, 16)
	fmt = binary.LittleEndian.AppendUint16(fmt, 1)
	fmt = binary.LittleEndian.AppendUint16(fmt, 1)
	fmt = binary.LittleEndian.AppendUint32(fmt, 8000)
	fmt = binary.LittleEndian.AppendUint32(fmt, 8000)
	fmt = binary.LittleEndian.AppendUint16(fmt, 1)
	fmt = binary.LittleEndian.AppendUint16(fmt, 8)
	data := []byte("data")
	data = binary.LittleEndian.AppendUint32(data, 8000)
	data = append(data, make([]byte, 8000)...)
	return append(append(bs, fmt...), data...)
}

func uploadAudio(t *testing.T, ts *testEnv, token string) (string, int) {
	t.Helper()
	var body strings.Builder
	w := multipart.NewWriter(&body)
	fw, _ := w.CreateFormFile("file", "voice.wav")
	_, _ = fw.Write(buildTestWAV(t))
	_ = w.Close()
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/media/upload", strings.NewReader(body.String()))
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out struct {
		MediaToken string `json:"media_token"`
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	return out.MediaToken, res.StatusCode
}

// TestAudioEndToEnd 音频主链路：上传带时长 → 发布落库 → 时间轴带 duration →
// thumb=蓝底 SVG 占位、raw=按扩展名 Content-Type。
func TestAudioEndToEnd(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	token, code := uploadAudio(t, ts, adminToken)
	if code != http.StatusOK {
		t.Fatalf("upload audio: %d", code)
	}

	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{
		"content": "语音日记", "media_tokens": []string{token},
	})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create audio post: %d", res.StatusCode)
	}

	res = doJSON(t, "GET", ts.URL+"/api/v1/media/timeline", adminToken, nil)
	var tl struct {
		Items []struct {
			ID       int64    `json:"id"`
			Kind     string   `json:"kind"`
			Duration *float64 `json:"duration"`
			URL      string   `json:"url"`
			ThumbURL string   `json:"thumb_url"`
		} `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&tl)
	res.Body.Close()
	if len(tl.Items) != 1 || tl.Items[0].Kind != "audio" {
		t.Fatalf("audio in timeline: %+v", tl.Items)
	}
	it := tl.Items[0]
	if it.Duration == nil || *it.Duration < 0.9 || *it.Duration > 1.1 {
		t.Fatalf("audio duration = %v, want ~1s", it.Duration)
	}

	// thumb → SVG 占位；raw → audio/wav
	res2, err := http.Get(ts.URL + it.ThumbURL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res2.Body)
	res2.Body.Close()
	if res2.StatusCode != http.StatusOK || !strings.HasPrefix(res2.Header.Get("Content-Type"), "image/svg") || !strings.Contains(string(body), "音频") {
		t.Fatalf("audio thumb: %d %s %q", res2.StatusCode, res2.Header.Get("Content-Type"), string(body[:40]))
	}
	res3, err := http.Get(ts.URL + it.URL)
	if err != nil {
		t.Fatal(err)
	}
	res3.Body.Close()
	if res3.StatusCode != http.StatusOK || res3.Header.Get("Content-Type") != "audio/wav" {
		t.Fatalf("audio raw: %d %s", res3.StatusCode, res3.Header.Get("Content-Type"))
	}

	// 落库校验：media.duration REAL
	var dur float64
	if err := ts.db.QueryRow(`SELECT duration FROM media WHERE kind = 'audio'`).Scan(&dur); err != nil {
		t.Fatal(err)
	}
}
