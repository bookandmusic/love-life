package chat

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// 错误响应必须是合法 JSON：消息里的引号/换行由 encoder 转义，
// 且 Content-Type 不能退化成 text/plain（旧实现手写 JSON 字面量时曾如此）。
func TestWriteChatErrorIsJSON(t *testing.T) {
	const msg = `引号 " 与换行\n 与反斜杠 \ 都要原样回到客户端`

	rec := httptest.NewRecorder()
	writeChatError(rec, 400, msg)

	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q，应为 application/json", ct)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON：%v（body=%q）", err, rec.Body.String())
	}
	if body["error"] != msg {
		t.Fatalf("error = %q，应为 %q", body["error"], msg)
	}
}
