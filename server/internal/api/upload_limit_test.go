package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/media"
)

// 上传上限的守卫用例（ARCHITECTURE「媒体上传管线」· 格式与大小）：
// 上限必须在**读取阶段**生效——超限文件不再落盘，且 HTTP 层截断与 SaveTemp
// 判限两条来源归同一个状态码与文案。

const testMediaLimit = 1 << 20 // 1 MiB：小上限让用例快速且不占盘

// TestSaveTempRejectsOversizeWithoutFullWrite 根因守卫：超限文件不得整份落盘。
// 旧实现先 io.Copy 全量写完再判限——一个 100 KiB 的上传会实打实写满 100 KiB。
func TestSaveTempRejectsOversizeWithoutFullWrite(t *testing.T) {
	dir := t.TempDir()
	const limit = 1024
	p := media.New(dir, []byte("0123456789abcdef0123456789abcdef"), limit)

	var written int
	r := &countingReader{r: bytes.NewReader(bytes.Repeat([]byte("A"), limit*100)), total: &written}
	_, err := p.SaveTemp(r, "big.jpg", 1)
	if !errors.Is(err, media.ErrTooLarge) {
		t.Fatalf("err=%v want ErrTooLarge", err)
	}
	// 关键断言：写入量不超过上限+1（LimitReader 截断），而不是 100 倍上限
	if written > limit+1 {
		t.Fatalf("超限文件写入了 %d 字节（上限 %d）：必须在读取阶段截断", written, limit)
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, "media", "tmp")); len(entries) != 0 {
		t.Fatalf("失败后临时区残留 %d 项", len(entries))
	}
}

// TestSaveTempBoundaries 边界：恰好等于上限必须接受，上限+1 必须拒绝。
// 用音频扩展名：photo 会同步生成缩略图（喂假字节会解码失败），音频解析失败不阻塞上传。
func TestSaveTempBoundaries(t *testing.T) {
	dir := t.TempDir()
	const limit = 2048
	p := media.New(dir, []byte("0123456789abcdef0123456789abcdef"), limit)

	up, err := p.SaveTemp(bytes.NewReader(bytes.Repeat([]byte("A"), limit)), "at-limit.mp3", 1)
	if err != nil {
		t.Fatalf("恰好等于上限应接受: %v", err)
	}
	if up.Size != limit {
		t.Fatalf("size=%d want %d", up.Size, limit)
	}
	if _, err := p.SaveTemp(bytes.NewReader(bytes.Repeat([]byte("A"), limit+1)), "over.mp3", 1); !errors.Is(err, media.ErrTooLarge) {
		t.Fatalf("上限+1 应判 ErrTooLarge，实际 %v", err)
	}
}

// TestUploadOversizeReturns413 超限一律 413，且两条来源（HTTP 层截断 / SaveTemp 判限）
// 说法一致——不留两种状态码与两种文案。
func TestUploadOversizeReturns413(t *testing.T) {
	ts, adminToken := newPostTestEnvWithLimit(t, testMediaLimit)

	cases := []struct {
		name string
		size int
		// 超出信封余量（8 KiB）→ HTTP 层截断；仅超上限 2 KiB → HTTP 放行、SaveTemp 判限
	}{
		{"HTTP 层截断", testMediaLimit * 2},
		{"夹缝（SaveTemp 判限）", testMediaLimit + 2048},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := uploadRaw(t, ts.URL, adminToken, "big.mp4", bytes.Repeat([]byte("A"), c.size))
			defer res.Body.Close()
			if res.StatusCode != http.StatusRequestEntityTooLarge {
				t.Fatalf("code=%d want 413", res.StatusCode)
			}
			var out struct {
				Error string `json:"error"`
			}
			_ = json.NewDecoder(res.Body).Decode(&out)
			if !strings.Contains(out.Error, "大小上限") {
				t.Fatalf("413 文案应含「大小上限」: %q", out.Error)
			}
		})
	}
}

// TestUploadOversizeWritesNothing 核心收益守卫：超限请求不产生任何落盘。
func TestUploadOversizeWritesNothing(t *testing.T) {
	ts, adminToken := newPostTestEnvWithLimit(t, testMediaLimit)

	res := uploadRaw(t, ts.URL, adminToken, "big.mp4", bytes.Repeat([]byte("A"), testMediaLimit*2))
	res.Body.Close()
	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("code=%d want 413", res.StatusCode)
	}
	entries, err := os.ReadDir(filepath.Join(ts.dataDir, "media", "tmp"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read tmp: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("超限上传后临时区有 %d 项（应在读取阶段截断、不落盘）", len(entries))
	}
}

// TestUploadWithinLimitStillWorks 回归：上限内的上传照常（守 LimitReader 语义——
// 若误用 io.CopyN 且照旧判 err != nil，正常文件会被判失败，此用例会红）。
func TestUploadWithinLimitStillWorks(t *testing.T) {
	ts, adminToken := newPostTestEnvWithLimit(t, testMediaLimit)

	token, code := uploadMedia(t, ts.URL, adminToken, "ok.jpg", genJPEG(t))
	if code != http.StatusOK {
		t.Fatalf("code=%d want 200", code)
	}
	if token == "" {
		t.Fatal("正常上传应返回 media_token")
	}
}

// ---- 用例辅助 ----

// countingReader 记录实际被读取的字节数（断言「没有全量落盘」）。
type countingReader struct {
	r     io.Reader
	total *int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	*c.total += n
	return n, err
}

// newPostTestEnvWithLimit 指定媒体上限的测试环境 + admin 令牌。
func newPostTestEnvWithLimit(t *testing.T, mediaMaxBytes int64) (*testEnv, string) {
	t.Helper()
	ts := newTestServerWithLimit(t, mediaMaxBytes)
	adminToken, code := loginToken(t, ts.URL, "admin", "admin-pw", "admin")
	if code != http.StatusOK {
		t.Fatalf("admin login: %d", code)
	}
	return ts, adminToken
}
