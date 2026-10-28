package api

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/logbuf"
)

// A3：writeErrorf 的契约——5xx 把原始 err 记进服务日志，4xx 不记。
//
// 只测这一个函数，不跑整条 HTTP 链路：137 处调用点已由编译器保证签名正确，
// 真正要守的是「5xx 有日志、4xx 不吵」这条语义。
func TestWriteErrorfLogsServerErrorsOnly(t *testing.T) {
	buf := logbuf.New(64)
	prev := slog.Default()
	// 下游必须是**独立**的 handler，不能传 slog.Default().Handler()：
	// 那样包的正是 Default 自己，SetDefault 之后包级 slog 会无限自递归（见 main.go 同款注释）。
	slog.SetDefault(slog.New(logbuf.NewHandler(slog.NewTextHandler(io.Discard, nil), buf)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts?page=1", nil)

	// 1) 500：必须记 ERROR，且带 err / method / path
	before := buf.LatestSeq()
	rec := httptest.NewRecorder()
	writeErrorf(rec, req, http.StatusInternalServerError, errInternal, errors.New("no such table: posts"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("状态码应 500，实际 %d", rec.Code)
	}
	if got := rec.Body.String(); got == "" || got == `"no such table: posts"` {
		t.Fatalf("响应体只应含通用文案，实际 %q", got)
	}
	entries := buf.Since(before)
	if len(entries) != 1 {
		t.Fatalf("500 应记 1 条日志，实际 %d", len(entries))
	}
	e := entries[0]
	if e.Level != "ERROR" {
		t.Fatalf("级别应 ERROR，实际 %s", e.Level)
	}
	if e.Attrs["err"] == "" {
		t.Fatalf("日志必须带原始 err，实际 attrs=%+v", e.Attrs)
	}
	if e.Attrs["path"] != "/api/v1/posts" {
		t.Fatalf("日志应带 path，实际 %+v", e.Attrs)
	}
	if e.Attrs["method"] != http.MethodGet {
		t.Fatalf("日志应带 method，实际 %+v", e.Attrs)
	}

	// 2) 4xx：不记日志（业务拒绝是常态，记下来会淹掉真正的故障）
	before = buf.LatestSeq()
	rec = httptest.NewRecorder()
	writeErrorf(rec, req, http.StatusBadRequest, "评论最多 500 字", errors.New("too long"))
	if n := len(buf.Since(before)); n != 0 {
		t.Fatalf("4xx 不应写日志，实际写了 %d 条", n)
	}

	// 3) 5xx 但 err 为 nil：不 panic，照常回文案
	before = buf.LatestSeq()
	rec = httptest.NewRecorder()
	writeErrorf(rec, req, http.StatusInternalServerError, errInternal, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("err 为 nil 时仍应回 500，实际 %d", rec.Code)
	}
	if n := len(buf.Since(before)); n != 0 {
		t.Fatalf("err 为 nil 时不该凭空造日志，实际 %d 条", n)
	}

	// 4) r 为 nil：不 panic（少数辅助函数拿不到请求）
	rec = httptest.NewRecorder()
	writeErrorf(rec, nil, http.StatusInternalServerError, errInternal, errors.New("boom"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("r 为 nil 时应仍回 500，实际 %d", rec.Code)
	}
}
