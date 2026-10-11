package api

// 模型调用失败轮的用户消息必须标 interrupted（AI模式设计「停止与断连」）：
// error 收尾与断连同口径——否则刷新后它成孤儿消息，没有「编辑重发」入口可回收这条无人回答的残局。

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestChatModelErrorMarksUserInterrupted：上游 500 → error 事件收尾 →
// 用户消息落库带 interrupted part（与断连路径同口径）。
func TestChatModelErrorMarksUserInterrupted(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	fail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(fail.Close)
	seedFakeProvider(t, ts, fail.URL)

	res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]string{"content": "随便说一句"})
	sc := bufio.NewScanner(res.Body)
	var got string
	deadline := time.Now().Add(10 * time.Second)
	for sc.Scan() && time.Now().Before(deadline) {
		line := sc.Text()
		got += line + "\n"
		if strings.Contains(got, "event: error") {
			break
		}
	}
	res.Body.Close()
	if !strings.Contains(got, "event: error") {
		t.Fatalf("应收到 error 事件，SSE 片段：%s", got)
	}

	// 打标与 SSE 返回无顺序保证，轮询等待落库
	var userParts string
	marked := false
	for i := 0; i < 60; i++ {
		if err := ts.db.QueryRow(`SELECT parts FROM chat_messages
			WHERE role = 'user' ORDER BY id DESC LIMIT 1`).Scan(&userParts); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(userParts, "interrupted") {
			marked = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !marked {
		t.Fatalf("失败轮的用户消息应带 interrupted 标记，parts=%s", userParts)
	}
}
