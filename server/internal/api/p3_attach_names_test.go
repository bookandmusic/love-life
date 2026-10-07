package api

import (
	"strings"
	"testing"
)

// 随消息携带的媒体附件，模型上下文里要有**文件名**（不只数量）——
// 「只留一行元信息（文件名与数量），用于刷新后仍能知道这条消息发过 N 个文件」
// （AI模式设计「图片处理与存储」/PRODUCT「图片与「看图」边界」）。
func TestChatAttachmentModelTextCarriesFileNames(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	fake := newFakeModel(t, `{"message":{"content":"看到了"}}`)
	seedFakeProvider(t, ts, fake.server.URL)

	res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]any{
		"content": "帮我看看这两张",
		"media": []map[string]any{
			{"name": "客厅.jpg", "type": "image/jpeg", "size": 1024},
			{"name": "海边.png", "type": "image/png", "size": 2048},
		},
	})
	_ = sseBody(t, res) // 读完这一轮，模型请求才算发生

	// 取模型收到的 user 消息文本
	var userMsg string
	for _, m := range fake.lastMessages(t) {
		if m["role"] == "user" {
			if s, ok := m["content"].(string); ok && strings.Contains(s, "帮我看看这两张") {
				userMsg = s
			}
		}
	}
	if userMsg == "" {
		t.Fatal("未取到本轮 user 消息")
	}
	for _, want := range []string{"2 个媒体附件", "客厅.jpg", "海边.png"} {
		if !strings.Contains(userMsg, want) {
			t.Fatalf("附件元信息缺 %q：%s", want, userMsg)
		}
	}
}

// 文件名缺失时退化为只给数量（不让空串拼出「：」这种残句）。
func TestChatAttachmentModelTextWithoutNames(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	fake := newFakeModel(t, `{"message":{"content":"好的"}}`)
	seedFakeProvider(t, ts, fake.server.URL)

	res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]any{
		"content": "看这张",
		"media":   []map[string]any{{"name": "", "type": "image/jpeg", "size": 10}},
	})
	_ = sseBody(t, res)

	var userMsg string
	for _, m := range fake.lastMessages(t) {
		if m["role"] == "user" {
			if s, ok := m["content"].(string); ok && strings.Contains(s, "看这张") {
				userMsg = s
			}
		}
	}
	if !strings.Contains(userMsg, "1 个媒体附件") {
		t.Fatalf("应给出数量：%s", userMsg)
	}
	if strings.Contains(userMsg, "附件：") {
		t.Fatalf("无文件名时不得拼出空列表：%s", userMsg)
	}
}
