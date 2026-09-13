package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// S4 搜索结果卡片：模型调用 search_posts 后服务端应下发 event: result（契约 mock routes.mjs:897-905）。
// resultEventResult 与 APP ChatView 的 SearchItem 字段对齐。
type resultEventResult struct {
	Tool  string `json:"tool"`
	Query string `json:"query"`
	Items []struct {
		ID       int64  `json:"id"`
		Content  string `json:"content"`
		ThumbURL string `json:"thumb_url"`
	} `json:"items"`
}

// resultEventFromSSE 从 SSE 文本取 result 事件体；无该事件返回 nil。
func resultEventFromSSE(t *testing.T, body string) *resultEventResult {
	t.Helper()
	for _, block := range strings.Split(body, "\n\n") {
		if !strings.HasPrefix(block, "event: result") {
			continue
		}
		idx := strings.Index(block, "data: ")
		if idx < 0 {
			continue
		}
		var ev resultEventResult
		if err := json.Unmarshal([]byte(strings.TrimSpace(block[idx+6:])), &ev); err != nil {
			t.Fatalf("解析 result 事件失败: %v block=%q", err, block)
		}
		return &ev
	}
	return nil
}
func TestChatSearchResultEvent(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	for _, c := range []string{"海边的日落真美", "海边捡到的贝壳", "山里的清晨"} {
		res := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{"content": c})
		res.Body.Close()
	}

	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"s1","type":"function","function":{"name":"search_posts","arguments":"{\"query\":\"海边\"}"}}]}}`,
		`{"message":{"content":"找到了这些"}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]string{"content": "我们去过海边吗"})
	body := sseBody(t, res)
	if !strings.Contains(body, "event: result") {
		t.Fatalf("缺少 result 事件: %s", body)
	}
	ev := resultEventFromSSE(t, body)
	if ev == nil {
		t.Fatalf("result 事件无法解析: %s", body)
	}
	if ev.Tool != "search_posts" {
		t.Fatalf("tool=%q want search_posts", ev.Tool)
	}
	if ev.Query != "海边" {
		t.Fatalf("query=%q want 海边", ev.Query)
	}
	if len(ev.Items) != 2 {
		t.Fatalf("items=%d want 2（命中「海边」的两条）", len(ev.Items))
	}
	for _, it := range ev.Items {
		if it.ID == 0 {
			t.Fatal("item.id 缺失")
		}
		if n := len([]rune(it.Content)); n > 60 {
			t.Fatalf("content 截断失败: %d rune", n)
		}
		if it.ThumbURL != "" {
			t.Fatalf("无媒体动态 thumb_url 应为空串，got %q", it.ThumbURL)
		}
	}
	// result 应在 done 之前（客户端先落卡片气泡再收尾）
	if strings.Index(body, "event: result") > strings.Index(body, "event: done") {
		t.Fatal("result 事件应在 done 之前")
	}
	// 文案照 mock 口径（人话），且不得把工具内部 JSON 泄给用户
	if !strings.Contains(body, "找到 2 条") {
		t.Fatalf("缺少「找到 N 条」文案:\n%s", body)
	}
	if strings.Contains(body, "search_posts →") {
		t.Fatalf("不应把「工具名 → 原始 JSON」推给用户:\n%s", body)
	}
}
func TestChatSearchResultEmpty(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{"content": "山里的清晨"})
	res.Body.Close()

	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"s1","type":"function","function":{"name":"search_posts","arguments":"{\"query\":\"不存在的关键词\"}"}}]}}`,
		`{"message":{"content":"没找到"}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]string{"content": "查一下不存在的关键词"})
	body := sseBody(t, res)
	if !strings.Contains(body, "event: result") {
		t.Fatalf("空结果也应下发 result: %s", body)
	}
	if !strings.Contains(body, `"items":[]`) {
		t.Fatalf("空结果 items 必须是 []（非 null）: %s", body)
	}
}

func TestChatSearchResultThumbURL(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	mediaToken, code := uploadMedia(t, ts.URL, adminToken, "sea.jpg", genJPEG(t))
	if code != http.StatusOK {
		t.Fatalf("upload: %d", code)
	}
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{
		"content": "海边的日落真美", "media_tokens": []string{mediaToken},
	})
	res.Body.Close()

	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"s1","type":"function","function":{"name":"search_posts","arguments":"{\"query\":\"海边\"}"}}]}}`,
		`{"message":{"content":"找到了"}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]string{"content": "我们去过海边吗"})
	body := sseBody(t, res)
	ev := resultEventFromSSE(t, body)
	if ev == nil || len(ev.Items) != 1 {
		t.Fatalf("result 事件缺失或条数不对: %s", body)
	}
	thumb := ev.Items[0].ThumbURL
	if thumb == "" {
		t.Fatal("带媒体动态应有 thumb_url")
	}
	if !strings.Contains(thumb, "sig=") || !strings.Contains(thumb, "exp=") {
		t.Fatalf("thumb_url 应含短时签名参数: %q", thumb)
	}
}
