package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/crypto"
)

// fakeModel 起 OpenAI 兼容假模型服务，按脚本依次返回响应。
type fakeModel struct {
	server *httptest.Server
	calls  atomic.Int32
	script []string // 每次调用的 choices.message JSON
}

func newFakeModel(t *testing.T, script ...string) *fakeModel {
	t.Helper()
	f := &fakeModel{script: script}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := int(f.calls.Add(1)) - 1
		if i >= len(f.script) {
			i = len(f.script) - 1
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[` + f.script[i] + `]}`))
	}))
	t.Cleanup(f.server.Close)
	return f
}

// seedFakeProvider 直接插库注册假模型供应商（链头 openai-chat）；Key 用服务侧
// 同一密钥派生加密，与客户端零 AI 配置铁律一致（测试不经管理 API——数据模型重构后
// 该契约已移除，AD1 会提供新管理接口）。
func seedFakeProvider(t *testing.T, env *testEnv, baseURL string) {
	t.Helper()
	enc, err := crypto.Encrypt(crypto.KeyFrom(env.tm.Secret()), "sk-test")
	if err != nil {
		t.Fatalf("encrypt key: %v", err)
	}
	if _, err := env.db.Exec(`INSERT INTO model_providers (api_type, name, base_url, api_key_enc, default_model, sort_order) VALUES ('openai-chat', 'fake', ?, ?, 'fake-model', 0)`, baseURL, enc); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
}

func sseBody(t *testing.T, res *http.Response) string {
	t.Helper()
	defer res.Body.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(res.Body)
	return buf.String()
}

func TestChatToolConfirmFlow(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	// 未配置模型 → 423
	res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]string{"content": "你好"})
	res.Body.Close()
	if res.StatusCode != http.StatusLocked {
		t.Fatalf("chat without model: want 423, got %d", res.StatusCode)
	}

	// 假模型：第一次返回 create_post 工具调用（写→确认卡片）
	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"create_post","arguments":"{\"content\":\"今天去看了电影\",\"mood\":\"开心\"}"}}]}}`,
	)

	// 假模型供应商直接落库（旧管理契约已删除，AD1 提供新契约后另行覆盖管理面）
	seedFakeProvider(t, ts, fake.server.URL)

	// 对话 → SSE 收到 card 事件（写工具待确认）
	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]string{"content": "帮我记一条动态"})
	body := sseBody(t, res)
	if !strings.Contains(body, "event: card") || !strings.Contains(body, `"tool":"create_post"`) {
		t.Fatalf("sse card event missing:\n%s", body)
	}
	var actionID int64
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "data: ") && strings.Contains(line, "action_id") {
			var card struct {
				ActionID int64 `json:"action_id"`
			}
			_ = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &card)
			actionID = card.ActionID
		}
	}
	if actionID == 0 {
		t.Fatal("action_id not parsed")
	}

	// 取消路径：另一张卡片 → cancelled → 再确认冲突 409
	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]string{"content": "再记一条"})
	_ = sseBody(t, res)
	// 直接取消 actionID（复用第一张卡）
	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/confirm", adminToken, map[string]any{"action_id": actionID, "confirmed": false})
	var out struct{ Status string `json:"status"` }
	_ = json.NewDecoder(res.Body).Decode(&out)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || out.Status != "cancelled" {
		t.Fatalf("cancel: %d %+v", res.StatusCode, out)
	}
	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/confirm", adminToken, map[string]any{"action_id": actionID, "confirmed": true})
	res.Body.Close()
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("confirm after cancel: want 409, got %d", res.StatusCode)
	}

	// 再来一张卡 → 确认 → 动态创建 + 审计 + girl 收到通知（AI 代发=新动态口径）
	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]string{"content": "帮我记一条动态"})
	body = sseBody(t, res)
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "data: ") && strings.Contains(line, "action_id") {
			var card struct {
				ActionID int64 `json:"action_id"`
			}
			_ = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &card)
			actionID = card.ActionID
		}
	}
	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/confirm", adminToken, map[string]any{"action_id": actionID, "confirmed": true})
	_ = json.NewDecoder(res.Body).Decode(&out)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || out.Status != "executed" {
		t.Fatalf("confirm execute: %d %+v", res.StatusCode, out)
	}

	var posts, audits, notifs int
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM posts WHERE deleted_at IS NULL`).Scan(&posts)
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action='create_post'`).Scan(&audits)
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE event_type='post_created'`).Scan(&notifs)
	if posts != 1 || audits != 1 || notifs != 1 {
		t.Fatalf("post=%d audit=%d notif=%d (want 1/1/1)", posts, audits, notifs)
	}

	// 历史
	res = doJSON(t, "GET", ts.URL+"/api/v1/chat/history", adminToken, nil)
	var hist struct {
		Items []struct {
			Role string `json:"role"`
		} `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&hist)
	res.Body.Close()
	if len(hist.Items) < 3 {
		t.Fatalf("history: %+v", hist.Items)
	}
}

func TestChatReadToolInline(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	// 先用 REST 发一条动态
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{"content": "海边的日落真美"})
	res.Body.Close()

	// 假模型：先工具调用 search_posts，再纯文本回复
	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"s1","type":"function","function":{"name":"search_posts","arguments":"{\"query\":\"海边\"}"}}]}}`,
		`{"message":{"content":"找到了：海边的日落真美"}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]string{"content": "我们去过海边吗"})
	body := sseBody(t, res)
	if !strings.Contains(body, "海边的日落真美") || !strings.Contains(body, "event: done") {
		t.Fatalf("inline read tool flow:\n%s", body)
	}
	if strings.Contains(body, "event: card") {
		t.Fatal("read tool must not produce confirm card")
	}
	if fake.calls.Load() != 2 {
		t.Fatalf("model calls: %d (want 2)", fake.calls.Load())
	}
}
func TestChatMediaFlow(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	// 假模型：先创建带媒体的动态卡片
	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"create_post","arguments":"{\"content\":\"去看了海\"}"}}]}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)
	var res *http.Response

	// 1) message 携带 media 引用（仅回显用）→ 历史 media 字段回显
	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]any{
		"content": "帮我发动态",
		"media": []map[string]any{
			{"name": "p1.jpg", "type": "image/jpeg", "size": 1234, "data_url": "data:image/jpeg;base64,AAAA"},
		},
	})
	body := sseBody(t, res)
	if !strings.Contains(body, "event: card") {
		t.Fatalf("media message card:\n%s", body)
	}
	actionID := cardFromSSE(t, body)
	if actionID == 0 { t.Fatal("action_id missing") }

	res = doJSON(t, "GET", ts.URL+"/api/v1/chat/history", adminToken, nil)
	var hist struct {
		Items []struct {
			Role  string          `json:"role"`
			Media json.RawMessage `json:"media"`
		} `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&hist)
	res.Body.Close()
	var gotMedia bool
	for _, it := range hist.Items {
		if it.Role == "user" && len(it.Media) > 2 {
			gotMedia = true
		}
	}
	if !gotMedia { t.Fatalf("media not echoed in history: %+v", hist.Items) }

	// 2) 取消路径：media token 即使上传也不会落库（零垃圾）
	mediaToken, code := uploadMedia(t, ts.URL, adminToken, "photo1.jpg", genJPEG(t))
	if code != http.StatusOK { t.Fatalf("upload: %d", code) }
	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/confirm", adminToken, map[string]any{
		"action_id": actionID, "confirmed": false, "media_tokens": []string{mediaToken},
	})
	res.Body.Close()
	var postCount int
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM posts WHERE deleted_at IS NULL`).Scan(&postCount)
	if postCount != 0 { t.Fatalf("cancel must not create post, got %d", postCount) }

	// 3) 确认路径：media token → 落库为媒体（随动态）
	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]string{"content": "再发一条"})
	body = sseBody(t, res)
	actionID = cardFromSSE(t, body)
	mediaToken, _ = uploadMedia(t, ts.URL, adminToken, "photo2.jpg", genJPEG(t))
	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/confirm", adminToken, map[string]any{
		"action_id": actionID, "confirmed": true, "media_tokens": []string{mediaToken},
	})
	var out struct{ Status string `json:"status"` }
	_ = json.NewDecoder(res.Body).Decode(&out)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || out.Status != "executed" {
		t.Fatalf("confirm: %d %+v", res.StatusCode, out)
	}
	var mediaN int
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM media WHERE deleted_at IS NULL`).Scan(&mediaN)
	if mediaN != 1 { t.Fatalf("expect 1 media, got %d", mediaN) }
}

func TestChatAlbumTools(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	// 先准备一个动态媒体（REST 发布）
	mediaToken, _ := uploadMedia(t, ts.URL, adminToken, "p.jpg", genJPEG(t))
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{
		"content": "带图动态", "media_tokens": []string{mediaToken},
	})
	var pc struct{ ID int64 `json:"id"` }
	_ = json.NewDecoder(res.Body).Decode(&pc)
	res.Body.Close()
	var mediaID int64
	_ = ts.db.QueryRow(`SELECT id FROM media WHERE post_id=?`, pc.ID).Scan(&mediaID)

	// 相册工具：create_album → confirm → 相册存在；add_photos → confirm → media 归入相册
	fakeScript := fmt.Sprintf(`{"message":{"content":"","tool_calls":[{"id":"a2","type":"function","function":{"name":"add_photos_to_album","arguments":"{\"album_id\":1,\"media_ids\":[%d]}"}}]}}`, mediaID)
	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"a1","type":"function","function":{"name":"create_album","arguments":"{\"name\":\"旅行\"}"}}]}}`,
		fakeScript,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]string{"content": "建相册"})
	body := sseBody(t, res)
	actionID := cardFromSSE(t, body)
	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/confirm", adminToken, map[string]any{"action_id": actionID, "confirmed": true})
	res.Body.Close()

	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]string{"content": "加照片"})
	body = sseBody(t, res)
	actionID = cardFromSSE(t, body)
	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/confirm", adminToken, map[string]any{"action_id": actionID, "confirmed": true})
	res.Body.Close()

	var albumID, itemN int
	_ = ts.db.QueryRow(`SELECT id FROM albums WHERE name=?`, "旅行").Scan(&albumID)
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM album_items WHERE album_id=?`, albumID).Scan(&itemN)
	if albumID == 0 || itemN != 1 {
		t.Fatalf("album=%d items=%d (want 1/1)", albumID, itemN)
	}
}

func cardFromSSE(t *testing.T, body string) int64 {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "data: ") && strings.Contains(line, "action_id") {
			var card struct{ ActionID int64 `json:"action_id"` }
			_ = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &card)
			return card.ActionID
		}
	}
	t.Fatalf("no card in SSE:\n%s", body)
	return 0
}


// TestChatAlbumToolsReject add_photos：①随 confirm 上传 token 直接拒绝（对齐 mock：媒体只能随动态）；
// ②所属动态已软删（回收站）的媒体拒绝入相册——两种情况下 album_items 均零变化。
func TestChatAlbumToolsReject(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	mediaToken, _ := uploadMedia(t, ts.URL, adminToken, "p.jpg", genJPEG(t))
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{
		"content": "带图动态", "media_tokens": []string{mediaToken},
	})
	var pc struct{ ID int64 `json:"id"` }
	_ = json.NewDecoder(res.Body).Decode(&pc)
	res.Body.Close()
	var mediaID int64
	_ = ts.db.QueryRow(`SELECT id FROM media WHERE post_id=?`, pc.ID).Scan(&mediaID)

	// fake 脚本：[create_album, add_photos, add_photos]（第三次起复用最后一次脚本）
	script := fmt.Sprintf(`{"message":{"content":"","tool_calls":[{"id":"a2","type":"function","function":{"name":"add_photos_to_album","arguments":"{\"album_id\":1,\"media_ids\":[%d]}"}}]}}`, mediaID)
	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"a1","type":"function","function":{"name":"create_album","arguments":"{\"name\":\"旅行\"}"}}]}}`,
		script,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]string{"content": "建相册"})
	body := sseBody(t, res)
	actionID := cardFromSSE(t, body)
	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/confirm", adminToken, map[string]any{"action_id": actionID, "confirmed": true})
	res.Body.Close()

	countItems := func() int {
		var n int
		_ = ts.db.QueryRow(`SELECT COUNT(*) FROM album_items WHERE album_id=1`).Scan(&n)
		return n
	}

	// ① 携带 media_tokens 确认 add_photos → 工具拒绝（HTTP 500），无落库
	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]string{"content": "加照片"})
	body = sseBody(t, res)
	actionID = cardFromSSE(t, body)
	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/confirm", adminToken, map[string]any{
		"action_id": actionID, "confirmed": true, "media_tokens": []string{mediaToken},
	})
	var out struct{ Status string `json:"status"` }
	_ = json.NewDecoder(res.Body).Decode(&out)
	res.Body.Close()
	if out.Status != "failed" {
		t.Fatalf("confirm with media_tokens on add_photos: status=%q want failed", out.Status)
	}
	if n := countItems(); n != 0 {
		t.Fatalf("token confirm must not add items, got %d", n)
	}

	// ② 软删动态后 add_photos → 工具拒绝（媒体所属动态已删），无落库
	_ = ts.db.SoftDeletePost(t.Context(), pc.ID)
	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]string{"content": "加照片"})
	body = sseBody(t, res)
	actionID = cardFromSSE(t, body)
	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/confirm", adminToken, map[string]any{"action_id": actionID, "confirmed": true})
	var out2 struct{ Status string `json:"status"` }
	_ = json.NewDecoder(res.Body).Decode(&out2)
	res.Body.Close()
	if out2.Status != "failed" {
		t.Fatalf("confirm add_photos on soft-deleted post: status=%q want failed", out2.Status)
	}
	if n := countItems(); n != 0 {
		t.Fatalf("deleted-media confirm must not add items, got %d", n)
	}
}