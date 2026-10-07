package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bookandmusic/love-life/server/internal/crypto"
	"github.com/bookandmusic/love-life/server/internal/store"
)

// fakeModel 起 OpenAI 兼容假模型服务，按脚本依次返回响应；并捕获每次请求体
// （上下文组装类断言看「模型实际收到了什么」，故记录 messages）。
type fakeModel struct {
	server *httptest.Server
	calls  atomic.Int32
	script []string // 每次调用的 choices.message JSON
	mu     sync.Mutex
	reqs   []map[string]any // 每次调用的请求体
}

func newFakeModel(t *testing.T, script ...string) *fakeModel {
	t.Helper()
	f := &fakeModel{script: script}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		if json.Unmarshal(body, &req) == nil {
			f.mu.Lock()
			f.reqs = append(f.reqs, req)
			f.mu.Unlock()
		}
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

// lastMessages 最近一次模型调用实际收到的 messages（上下文组装断言入口）。
func (f *fakeModel) lastMessages(t *testing.T) []map[string]any {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.reqs) == 0 {
		t.Fatal("no model request captured")
	}
	raw, _ := f.reqs[len(f.reqs)-1]["messages"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, m := range raw {
		if mm, ok := m.(map[string]any); ok {
			out = append(out, mm)
		}
	}
	return out
}

// seedFakeProvider 直接插库注册假模型供应商（链头 openai-chat）；Key 用服务侧
// 同一密钥派生加密，与客户端零 AI 配置铁律一致（测试不经管理 API——数据模型重构后
// 该契约已移除， 会提供新管理接口）。
func seedFakeProvider(t *testing.T, env *testEnv, baseURL string) {
	t.Helper()
	enc, err := crypto.Encrypt(crypto.KeyFrom(env.tm.Secret()), "sk-test")
	if err != nil {
		t.Fatalf("encrypt key: %v", err)
	}
	res, err := env.db.Exec(`INSERT INTO model_providers (api_type, name, base_url, api_key_enc) VALUES ('openai-chat', 'fake', ?, ?)`, baseURL, enc)
	if err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	if pid, err := res.LastInsertId(); err != nil {
		t.Fatal(err)
	} else if _, err := env.db.Exec(`INSERT INTO provider_models (provider_id, kind, model_id, chain_order) VALUES (?, 'chat', 'fake-model', 0)`, pid); err != nil {
		t.Fatalf("seed model: %v", err)
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

	// 假模型：第一次返回 propose_post 工具调用（写→确认卡片）
	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"propose_post","arguments":"{\"content\":\"今天去看了电影\",\"mood\":\"开心\"}"}}]}}`,
	)

	// 假模型供应商直接落库（旧管理契约已删除， 提供新契约后另行覆盖管理面）
	seedFakeProvider(t, ts, fake.server.URL)

	// 对话 → SSE 收到 card 事件（写工具待确认）；会话 id 从 session 事件取，后续消息同会话
	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]string{"content": "帮我记一条动态"})
	body := sseBody(t, res)
	sessID := sessionIDFromSSE(t, body)
	if !strings.Contains(body, "event: proposal") || !strings.Contains(body, `"intent":"propose_post"`) {
		t.Fatalf("sse card event missing:\n%s", body)
	}
	actionID := cardFromSSE(t, body)

	// 取消路径：另一张卡片 → cancelled → 再确认冲突 409
	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]any{"content": "再记一条", "session_id": sessID})
	_ = sseBody(t, res)
	// 直接取消 actionID（复用第一张卡）
	res = doJSON(t, "POST", ts.URL+fmt.Sprintf("/api/v1/chat/proposals/%d/resolve", actionID), adminToken, map[string]any{"decision": "cancel"})
	var out struct {
		Status string `json:"status"`
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || out.Status != "cancelled" {
		t.Fatalf("cancel: %d %+v", res.StatusCode, out)
	}
	res = doJSON(t, "POST", ts.URL+fmt.Sprintf("/api/v1/chat/proposals/%d/resolve", actionID), adminToken, map[string]any{"decision": "approve"})
	res.Body.Close()
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("confirm after cancel: want 409, got %d", res.StatusCode)
	}

	// 再来一张卡 → 确认 → 动态创建 + 审计 + girl 收到通知（AI 代发=新动态口径）
	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]any{"content": "帮我记一条动态", "session_id": sessID})
	body = sseBody(t, res)
	actionID = cardFromSSE(t, body)
	res = doJSON(t, "POST", ts.URL+fmt.Sprintf("/api/v1/chat/proposals/%d/resolve", actionID), adminToken, map[string]any{"decision": "approve"})
	_ = json.NewDecoder(res.Body).Decode(&out)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || out.Status != "executed" {
		t.Fatalf("confirm execute: %d %+v", res.StatusCode, out)
	}

	var posts, audits, cancels, execAudits, notifs int
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM posts WHERE deleted_at IS NULL`).Scan(&posts)
	// 提案两个时点各记一条审计（出卡 pending + 裁决 cancelled/success）
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action='propose_post'`).Scan(&audits)
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action='propose_post' AND result='cancelled'`).Scan(&cancels)
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action='propose_post' AND result='success'`).Scan(&execAudits)
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE event_type='post_created'`).Scan(&notifs)
	if posts != 1 || audits != 5 || cancels != 1 || execAudits != 1 || notifs != 1 {
		t.Fatalf("post=%d audits=%d cancels=%d success=%d notif=%d (want 1/5/1/1/1)", posts, audits, cancels, execAudits, notifs)
	}
	// AI 代发快照必须含 operator：APP 通知列表按 `${s.operator} 发布了新动态` 渲染，
	// 缺字段会显示 undefined。
	var snap string
	_ = ts.db.QueryRow(`SELECT snapshot_json FROM notifications WHERE event_type='post_created'`).Scan(&snap)
	var snapMap map[string]any
	if err := json.Unmarshal([]byte(snap), &snapMap); err != nil {
		t.Fatalf("unmarshal snapshot %q: %v", snap, err)
	}
	if op, _ := snapMap["operator"].(string); op == "" {
		t.Fatalf("AI 代发快照缺 operator（前端会渲染 undefined）: %s", snap)
	}
	if snapMap["via"] != "ai" {
		t.Fatalf("AI 代发快照应带 via=ai: %s", snap)
	}

	// 历史：按 session_id 取（sessID 已在本会话首条消息时取到）
	res = doJSON(t, "GET", ts.URL+fmt.Sprintf("/api/v1/chat/history?session_id=%d", sessID), adminToken, nil)
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

// sessionIDFromSSE 从 message 的 SSE 流里取 session 事件的会话 id（契约）。
func sessionIDFromSSE(t *testing.T, body string) int64 {
	t.Helper()
	seen := false
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "event: session") {
			seen = true
			continue
		}
		if !seen || !strings.HasPrefix(line, "data: ") {
			if strings.HasPrefix(line, "event: ") {
				seen = false
			}
			continue
		}
		var sess struct {
			SessionID int64 `json:"session_id"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &sess); err == nil && sess.SessionID > 0 {
			return sess.SessionID
		}
	}
	t.Fatalf("no session event in SSE:\n%s", body)
	return 0
}

// TestChatSessionsLazyCreateAndDelete 多会话：懒创建（新消息自动建会话 + 标题摘要）→
// 会话列表 → 按会话隔离历史 → 硬删（消息与会话一起消失）→ 越权/不存在 404。
func TestChatSessionsLazyCreateAndDelete(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	fake := newFakeModel(t, `{"message":{"content":"你好呀"}}`)
	seedFakeProvider(t, ts, fake.server.URL)

	// 首条消息：懒创建会话，标题 = 首条消息前 20 字 + 省略号
	res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken,
		map[string]string{"content": "帮我记录一下今天下午我们去公园散步看到了好多樱花真的太美了"})
	body := sseBody(t, res)
	sid := sessionIDFromSSE(t, body)
	if sid == 0 {
		t.Fatal("session must be lazily created")
	}

	// 会话列表：标题为首条消息摘要
	res = doJSON(t, "GET", ts.URL+"/api/v1/chat/sessions", adminToken, nil)
	var list struct {
		Items []struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
		} `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&list)
	res.Body.Close()
	if len(list.Items) != 1 || list.Items[0].ID != sid {
		t.Fatalf("sessions: %+v", list.Items)
	}
	want := "帮我记录一下今天下午我们去公园散步看到了…" // 恰好 20 字 + 省略号
	if list.Items[0].Title != want {
		t.Fatalf("title = %q, want %q", list.Items[0].Title, want)
	}

	// 第二个会话 + 会话隔离：sid1 的历史不出现在 sid2
	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]any{"content": "另起一个话题", "session_id": 0})
	body2 := sseBody(t, res)
	sid2 := sessionIDFromSSE(t, body2)
	if sid2 == sid {
		t.Fatal("session_id=0 must create a new session")
	}
	res = doJSON(t, "GET", ts.URL+fmt.Sprintf("/api/v1/chat/history?session_id=%d", sid2), adminToken, nil)
	var hist2 struct {
		Items []struct {
			Content string `json:"content"`
		} `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&hist2)
	res.Body.Close()
	if len(hist2.Items) != 2 { // user + assistant
		t.Fatalf("session2 history = %d items (want 2)", len(hist2.Items))
	}
	for _, it := range hist2.Items {
		if strings.Contains(it.Content, "看电影") {
			t.Fatalf("session isolation broken: %+v", hist2.Items)
		}
	}

	// history 缺 session_id → 空列表（不再有「全局历史」口径）
	res = doJSON(t, "GET", ts.URL+"/api/v1/chat/history", adminToken, nil)
	var histAll struct {
		Items []any `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&histAll)
	res.Body.Close()
	if len(histAll.Items) != 0 {
		t.Fatalf("history without session_id must be empty, got %d", len(histAll.Items))
	}

	// 硬删：消息 + 会话一起消失
	res = doJSON(t, "DELETE", ts.URL+fmt.Sprintf("/api/v1/chat/sessions/%d", sid), adminToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("delete session: %d", res.StatusCode)
	}
	var msgN, sessN int
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM chat_messages WHERE session_id = ?`, sid).Scan(&msgN)
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM chat_sessions WHERE id = ?`, sid).Scan(&sessN)
	if msgN != 0 || sessN != 0 {
		t.Fatalf("hard delete left rows: msgs=%d sessions=%d", msgN, sessN)
	}
	// 再删 → 404
	res = doJSON(t, "DELETE", ts.URL+fmt.Sprintf("/api/v1/chat/sessions/%d", sid), adminToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("re-delete: want 404, got %d", res.StatusCode)
	}
}

// TestChatReasoningEvent 思考链：模型下发 reasoning_content → SSE reasoning 事件先于 text，
// 内容不落库（历史只有回答）。
func TestChatReasoningEvent(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	fake := newFakeModel(t, `{"message":{"content":"答案是晚安","reasoning_content":"用户在道晚安"}}`)
	seedFakeProvider(t, ts, fake.server.URL)

	res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]string{"content": "晚安"})
	body := sseBody(t, res)
	idxReasoning := strings.Index(body, "event: thinking.delta")
	idxText := strings.Index(body, "event: answer.delta")
	if idxReasoning < 0 || idxText < 0 || idxReasoning > idxText {
		t.Fatalf("reasoning must precede text:\n%s", body)
	}
	if !strings.Contains(body, "用户在道晚安") {
		t.Fatalf("reasoning content missing:\n%s", body)
	}
	sid := sessionIDFromSSE(t, body)
	res = doJSON(t, "GET", ts.URL+fmt.Sprintf("/api/v1/chat/history?session_id=%d", sid), adminToken, nil)
	var hist struct {
		Items []struct {
			Content string `json:"content"`
		} `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&hist)
	res.Body.Close()
	for _, it := range hist.Items {
		if strings.Contains(it.Content, "用户在道晚安") {
			t.Fatal("reasoning must not be persisted to history")
		}
	}
}

func TestChatReadToolInline(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	// 先用 REST 发一条动态
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{"content": "海边的日落真美"})
	res.Body.Close()

	// 假模型：先工具调用 find_posts，再纯文本回复
	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"s1","type":"function","function":{"name":"find_posts","arguments":"{\"query\": \"海边\"}"}}]}}`,
		`{"message":{"content":"找到了：海边的日落真美"}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]string{"content": "我们去过海边吗"})
	body := sseBody(t, res)
	if !strings.Contains(body, "海边的日落真美") || !strings.Contains(body, "event: turn.end") {
		t.Fatalf("inline read tool flow:\n%s", body)
	}
	if strings.Contains(body, "event: proposal") {
		t.Fatal("read tool must not produce confirm card")
	}
	if fake.calls.Load() != 2 {
		t.Fatalf("model calls: %d (want 2)", fake.calls.Load())
	}

	// 回归：AI 读工具路径的审计 result 仅记状态 success，不含工具输出全文。
	// 后台按 result == 'success' 等值判定，写 'ok'/输出正文都会误显示为「失败」。
	var audits int
	var result, params string
	var latency any
	if err := ts.db.QueryRow(`SELECT COUNT(*), result, params, latency_ms FROM audit_logs WHERE action='find_posts'`).
		Scan(&audits, &result, &params, &latency); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("find_posts 应落 1 条审计，实际 %d 条", audits)
	}
	if result != "success" {
		t.Fatalf("result=%q want success", result)
	}
	if strings.Contains(result, "日落") {
		t.Fatalf("result 不应回写工具输出正文：%q", result)
	}
	if latency == nil {
		t.Fatal("ai 行 latency_ms 不应为 null（AD7 口径：ai/pat 有值）")
	}
	// 回归：params 存人类可读摘要、params_digest 存原始 JSON（ AuditLog 口径）。
	// 旧实现 params 为空 → 后台回退展示原始 JSON。
	if params != `query: "海边"` {
		t.Fatalf("ai 行 params=%q want 人类可读摘要 %q", params, `query: "海边"`)
	}
	var digest string
	if err := ts.db.QueryRow(`SELECT params_digest FROM audit_logs WHERE action='find_posts'`).Scan(&digest); err != nil {
		t.Fatal(err)
	}
	if digest != `{"query": "海边"}` {
		t.Fatalf("params_digest=%q want 模型原始 JSON 串（不得规范化键序/空白）", digest)
	}
}
func TestChatMediaFlow(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	// 假模型：先创建带媒体的动态卡片
	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"propose_post","arguments":"{\"content\":\"去看了海\"}"}}]}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)
	var res *http.Response

	// 1) message 携带 media 引用（仅回显用）→ 历史 media 字段回显
	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]any{
		"content": "帮我发动态",
		"media": []map[string]any{
			{"name": "p1.jpg", "type": "image/jpeg", "size": 1234},
		},
	})
	body := sseBody(t, res)
	if !strings.Contains(body, "event: proposal") {
		t.Fatalf("media message card:\n%s", body)
	}
	actionID := cardFromSSE(t, body)
	if actionID == 0 {
		t.Fatal("action_id missing")
	}

	res = doJSON(t, "GET", ts.URL+fmt.Sprintf("/api/v1/chat/history?session_id=%d", sessionIDFromSSE(t, body)), adminToken, nil)
	var hist struct {
		Items []struct {
			Role  string          `json:"role"`
			Parts json.RawMessage `json:"parts"`
			Card  *struct {
				Attachments []struct {
					Name string `json:"name"`
				} `json:"attachments"`
			} `json:"card"`
		} `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&hist)
	res.Body.Close()
	var gotAttachment bool // 附件本机元数据随 user parts 落库（attachment part，无 ref）
	var gotRefs bool       // 提案附件落库：回显卡片恢复「N 张媒体」角标
	for _, it := range hist.Items {
		if it.Role == "user" && strings.Contains(string(it.Parts), `"attachment"`) {
			gotAttachment = true
		}
		if it.Card != nil {
			if len(it.Card.Attachments) == 1 {
				gotRefs = true
			}
		}
	}
	if !gotAttachment {
		t.Fatalf("attachment part missing in history: %+v", hist.Items)
	}
	if !gotRefs {
		t.Fatalf("card attachments missing in history: %+v", hist.Items)
	}

	// 2) 取消路径：media token 即使上传也不会落库（零垃圾）
	mediaToken, code := uploadMedia(t, ts.URL, adminToken, "photo1.jpg", genJPEG(t))
	if code != http.StatusOK {
		t.Fatalf("upload: %d", code)
	}
	res = doJSON(t, "POST", ts.URL+fmt.Sprintf("/api/v1/chat/proposals/%d/resolve", actionID), adminToken, map[string]any{
		"decision": "cancel", "media_tokens": []string{mediaToken},
	})
	res.Body.Close()
	var postCount int
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM posts WHERE deleted_at IS NULL`).Scan(&postCount)
	if postCount != 0 {
		t.Fatalf("cancel must not create post, got %d", postCount)
	}

	// 3) 确认路径：media token → 落库为媒体（随动态）
	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]string{"content": "再发一条"})
	body = sseBody(t, res)
	actionID = cardFromSSE(t, body)
	mediaToken, _ = uploadMedia(t, ts.URL, adminToken, "photo2.jpg", genJPEG(t))
	res = doJSON(t, "POST", ts.URL+fmt.Sprintf("/api/v1/chat/proposals/%d/resolve", actionID), adminToken, map[string]any{
		"decision": "approve", "media_tokens": []string{mediaToken},
	})
	var out struct {
		Status string `json:"status"`
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || out.Status != "executed" {
		t.Fatalf("confirm: %d %+v", res.StatusCode, out)
	}
	var mediaN int
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM media WHERE deleted_at IS NULL`).Scan(&mediaN)
	if mediaN != 1 {
		t.Fatalf("expect 1 media, got %d", mediaN)
	}
}

func TestChatAlbumTools(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	// 先准备一个动态媒体（REST 发布）
	mediaToken, _ := uploadMedia(t, ts.URL, adminToken, "p.jpg", genJPEG(t))
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{
		"content": "带图动态", "media_tokens": []string{mediaToken},
	})
	var pc struct {
		ID int64 `json:"id"`
	}
	_ = json.NewDecoder(res.Body).Decode(&pc)
	res.Body.Close()
	var mediaID int64
	_ = ts.db.QueryRow(`SELECT id FROM media WHERE post_id=?`, pc.ID).Scan(&mediaID)

	// 相册工具：create_album → confirm → 相册存在；add_photos → confirm → media 归入相册
	fakeScript := fmt.Sprintf(`{"message":{"content":"","tool_calls":[{"id":"a2","type":"function","function":{"name":"propose_album_members","arguments":"{\"album_id\":1,\"add_media_ids\":[%d]}"}}]}}`, mediaID)
	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"a1","type":"function","function":{"name":"propose_album","arguments":"{\"name\":\"旅行\"}"}}]}}`,
		fakeScript,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]string{"content": "建相册"})
	body := sseBody(t, res)
	actionID := cardFromSSE(t, body)
	res = doJSON(t, "POST", ts.URL+fmt.Sprintf("/api/v1/chat/proposals/%d/resolve", actionID), adminToken, map[string]any{"decision": "approve"})
	res.Body.Close()

	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]string{"content": "加照片"})
	body = sseBody(t, res)
	actionID = cardFromSSE(t, body)
	res = doJSON(t, "POST", ts.URL+fmt.Sprintf("/api/v1/chat/proposals/%d/resolve", actionID), adminToken, map[string]any{"decision": "approve"})
	res.Body.Close()

	var albumID, itemN int
	_ = ts.db.QueryRow(`SELECT id FROM albums WHERE name=?`, "旅行").Scan(&albumID)
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM album_items WHERE album_id=?`, albumID).Scan(&itemN)
	if albumID == 0 || itemN != 1 {
		t.Fatalf("album=%d items=%d (want 1/1)", albumID, itemN)
	}
}

// cardFromSSE 从 proposal 事件取提案 id（新契约：event: proposal / data.proposal.id）。
func cardFromSSE(t *testing.T, body string) int64 {
	t.Helper()
	for _, evt := range strings.Split(body, "\n\n") {
		if !strings.HasPrefix(evt, "event: proposal") {
			continue
		}
		idx := strings.Index(evt, "data: ")
		if idx < 0 {
			continue
		}
		var payload struct {
			Proposal struct {
				ID int64 `json:"id"`
			} `json:"proposal"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(evt[idx+6:])), &payload); err == nil && payload.Proposal.ID > 0 {
			return payload.Proposal.ID
		}
	}
	t.Fatalf("no proposal event in SSE:\n%s", body)
	return 0
}

// previewEvent 提案事件体（含 preview / warnings / attachments / summary）。
type previewPayload struct {
	ID      int64  `json:"id"`
	Intent  string `json:"intent"`
	Summary string `json:"summary"`
	Preview struct {
		Kind string         `json:"kind"`
		Body map[string]any `json:"body"`
	} `json:"preview"`
	Warnings    []string `json:"warnings"`
	Attachments []struct {
		Name string `json:"name"`
		Type string `json:"type"`
		Size int64  `json:"size"`
	} `json:"attachments"`
	ExpiresIn int `json:"expires_in"`
}

// proposalsFromSSE 全部 proposal 事件体。
func proposalsFromSSE(t *testing.T, body string) []previewPayload {
	t.Helper()
	var out []previewPayload
	for _, evt := range strings.Split(body, "\n\n") {
		if !strings.HasPrefix(evt, "event: proposal") {
			continue
		}
		idx := strings.Index(evt, "data: ")
		if idx < 0 {
			continue
		}
		var wrapper struct {
			Proposal previewPayload `json:"proposal"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(evt[idx+6:])), &wrapper); err != nil {
			t.Fatalf("解析 proposal 事件失败: %v", err)
		}
		out = append(out, wrapper.Proposal)
	}
	return out
}

// toolEndUI 从 tool.end 事件取 UI 块（新协议：结果卡片挂在 tool.end.ui）。
func toolEndUI(t *testing.T, body string) []struct {
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Records []struct {
		ID       int64  `json:"id"`
		Kind     string `json:"kind"`
		Text     string `json:"text"`
		Date     string `json:"date"`
		ThumbURL string `json:"thumb_url"`
		Badge    string `json:"badge"`
		GroupKey string `json:"group_key"`
	} `json:"records"`
	ResultID   string `json:"result_id"`
	Selectable bool   `json:"selectable"`
	SinglePick bool   `json:"single_pick"`
	More       int    `json:"more"`
} {
	t.Helper()
	var out []struct {
		Kind    string `json:"kind"`
		Title   string `json:"title"`
		Records []struct {
			ID       int64  `json:"id"`
			Kind     string `json:"kind"`
			Text     string `json:"text"`
			Date     string `json:"date"`
			ThumbURL string `json:"thumb_url"`
			Badge    string `json:"badge"`
			GroupKey string `json:"group_key"`
		} `json:"records"`
		ResultID   string `json:"result_id"`
		Selectable bool   `json:"selectable"`
		SinglePick bool   `json:"single_pick"`
		More       int    `json:"more"`
	}
	for _, evt := range strings.Split(body, "\n\n") {
		if !strings.HasPrefix(evt, "event: tool.end") {
			continue
		}
		idx := strings.Index(evt, "data: ")
		if idx < 0 {
			continue
		}
		var payload struct {
			UI *struct {
				Kind    string `json:"kind"`
				Title   string `json:"title"`
				Records []struct {
					ID       int64  `json:"id"`
					Kind     string `json:"kind"`
					Text     string `json:"text"`
					Date     string `json:"date"`
					ThumbURL string `json:"thumb_url"`
					Badge    string `json:"badge"`
					GroupKey string `json:"group_key"`
				} `json:"records"`
				ResultID   string `json:"result_id"`
				Selectable bool   `json:"selectable"`
				SinglePick bool   `json:"single_pick"`
				More       int    `json:"more"`
			} `json:"ui"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(evt[idx+6:])), &payload); err != nil {
			continue
		}
		if payload.UI != nil {
			out = append(out, *payload.UI)
		}
	}
	return out
}

// TestChatAlbumToolsReject：①随 resolve 上传 token 直接拒绝（媒体只能随动态发布）；
// ②所属动态已软删（回收站）的媒体拒绝入相册——两种情况下 album_items 均零变化。
func TestChatAlbumToolsReject(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	mediaToken, _ := uploadMedia(t, ts.URL, adminToken, "p.jpg", genJPEG(t))
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{
		"content": "带图动态", "media_tokens": []string{mediaToken},
	})
	var pc struct {
		ID int64 `json:"id"`
	}
	_ = json.NewDecoder(res.Body).Decode(&pc)
	res.Body.Close()
	var mediaID int64
	_ = ts.db.QueryRow(`SELECT id FROM media WHERE post_id=?`, pc.ID).Scan(&mediaID)

	// fake 脚本：[create_album, add_photos, add_photos]（第三次起复用最后一次脚本）
	script := fmt.Sprintf(`{"message":{"content":"","tool_calls":[{"id":"a2","type":"function","function":{"name":"propose_album_members","arguments":"{\"album_id\":1,\"add_media_ids\":[%d]}"}}]}}`, mediaID)
	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"a1","type":"function","function":{"name":"propose_album","arguments":"{\"name\":\"旅行\"}"}}]}}`,
		script,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]string{"content": "建相册"})
	body := sseBody(t, res)
	actionID := cardFromSSE(t, body)
	res = doJSON(t, "POST", ts.URL+fmt.Sprintf("/api/v1/chat/proposals/%d/resolve", actionID), adminToken, map[string]any{"decision": "approve"})
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
	res = doJSON(t, "POST", ts.URL+fmt.Sprintf("/api/v1/chat/proposals/%d/resolve", actionID), adminToken, map[string]any{
		"decision": "approve", "media_tokens": []string{mediaToken},
	})
	var out struct {
		Status string `json:"status"`
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	res.Body.Close()
	if out.Status != "failed" {
		t.Fatalf("confirm with media_tokens on add_photos: status=%q want failed", out.Status)
	}
	if n := countItems(); n != 0 {
		t.Fatalf("token confirm must not add items, got %d", n)
	}

	// ② 软删动态后 add_photos → 工具拒绝（媒体所属动态已删），无落库
	_ = ts.db.SoftDeletePost(t.Context(), pc.ID, store.ActorTypeUser, 1)
	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]string{"content": "加照片"})
	body = sseBody(t, res)
	actionID = cardFromSSE(t, body)
	res = doJSON(t, "POST", ts.URL+fmt.Sprintf("/api/v1/chat/proposals/%d/resolve", actionID), adminToken, map[string]any{"decision": "approve"})
	var out2 struct {
		Status string `json:"status"`
	}
	_ = json.NewDecoder(res.Body).Decode(&out2)
	res.Body.Close()
	if out2.Status != "failed" {
		t.Fatalf("confirm add_photos on soft-deleted post: status=%q want failed", out2.Status)
	}
	if n := countItems(); n != 0 {
		t.Fatalf("deleted-media confirm must not add items, got %d", n)
	}

	// 回归：失败路径的审计 result 必须形如 failed: <原因>。
	// 后台按 result == 'success' 判成败，故前缀必须是 failed；同时保留原因便于排查。
	var failResult string
	if err := ts.db.QueryRow(`SELECT result FROM audit_logs WHERE action='propose_album_members' AND result <> 'success' ORDER BY id DESC LIMIT 1`).
		Scan(&failResult); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(failResult, "failed: ") {
		t.Fatalf("失败审计 result=%q want 前缀 'failed: '", failResult)
	}
}

// turnIDFromSSE 从 message 的 SSE 流里取 turn 事件的本轮 id（契约）。
func turnIDFromSSE(t *testing.T, body string) string {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "data: ") || !strings.Contains(line, "turn_id") {
			continue
		}
		var ev struct {
			TurnID string `json:"turn_id"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); err == nil && ev.TurnID != "" {
			return ev.TurnID
		}
	}
	t.Fatalf("no turn event in SSE:\n%s", body)
	return ""
}

// TestChatAbortRollsBackWholeTurn ：显式中止按轮回滚——该轮的用户消息、回答、
// 未裁决卡片一并消失，且**不波及上一轮**（旧 withdraw-last「删最后一条」做不到这点）。
func TestChatAbortRollsBackWholeTurn(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	fake := newFakeModel(t, `{"message":{"content":"第一条回答"}}`, `{"message":{"content":"第二条回答"}}`)
	seedFakeProvider(t, ts, fake.server.URL)

	// 第一轮：正常答完，留下 1 user + 1 assistant
	sid1, body1 := chatTurn(t, ts, adminToken, 0, "第一轮")
	sessID := sid1
	turn1 := turnIDFromSSE(t, body1)

	// 第二轮：拿到 turn_id 后中止
	_, body2 := chatTurn(t, ts, adminToken, sessID, "第二轮要被中止")
	turn2 := turnIDFromSSE(t, body2)

	res := doJSON(t, "POST", ts.URL+"/api/v1/chat/turns/"+turn2+"/abort", adminToken, nil)
	var out struct {
		Aborted bool `json:"aborted"`
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || !out.Aborted {
		t.Fatalf("abort: %d %+v", res.StatusCode, out)
	}

	// 第一轮内容必须还在，第二轮内容必须消失
	var n1, n2 int
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM chat_messages WHERE turn_id = ?`, turn1).Scan(&n1)
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM chat_messages WHERE turn_id = ?`, turn2).Scan(&n2)
	if n1 == 0 {
		t.Fatal("abort 越界删掉了上一轮")
	}
	if n2 != 0 {
		t.Fatalf("本轮消息未被回滚，剩 %d 行", n2)
	}
	// 幂等：重复 abort 不再删东西，也不报错
	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/turns/"+turn2+"/abort", adminToken, nil)
	res.Body.Close()
	var again struct{ Aborted bool }
	_ = json.NewDecoder(res.Body).Decode(&again)
	if res.StatusCode != http.StatusOK || again.Aborted {
		t.Fatalf("重复 abort: %d %+v", res.StatusCode, again)
	}
	// 越权：别人的 turn_id 删不动（换个用户 token 拿不到，这里用不存在的 id 验证不误伤）
	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/turns/deadbeef/abort", adminToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("未知 turn_id 应静默 no-op，got %d", res.StatusCode)
	}
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM chat_messages WHERE turn_id = ?`, turn1).Scan(&n1)
	if n1 == 0 {
		t.Fatal("未知 turn_id 的 abort 误删了数据")
	}
}

// TestChatAbortCancelsPendingCard 中止本轮时该轮未裁决的确认卡置 cancelled：
// 卡片行留痕供审计（「AI 提议过但被中止」），但不可再被确认执行。
func TestChatAbortCancelsPendingCard(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	fake := newFakeModel(t, cardSSE("要中止的动态"))
	seedFakeProvider(t, ts, fake.server.URL)

	_, body := chatTurn(t, ts, adminToken, 0, "帮我发动态：要中止的动态")
	turnID := turnIDFromSSE(t, body)
	actionID := cardFromSSE(t, body)
	if actionID == 0 {
		t.Fatal("card expected")
	}

	res := doJSON(t, "POST", ts.URL+"/api/v1/chat/turns/"+turnID+"/abort", adminToken, nil)
	res.Body.Close()

	var status string
	if err := ts.db.QueryRow(`SELECT status FROM confirm_actions WHERE id = ?`, actionID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" {
		t.Fatalf("card status = %q, want cancelled", status)
	}
	// 卡片载体消息随轮回滚
	var n int
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM chat_messages WHERE turn_id = ?`, turnID).Scan(&n)
	if n != 0 {
		t.Fatalf("turn messages left: %d", n)
	}
}

// TestChatHistoryMarksInterruptedTurn 的另一半：意外断开时消息**不删**，
// 但历史回显必须把尾条无人回答的 user 消息标出来，否则界面会永远停在「等待回复」。
func TestChatHistoryMarksInterruptedTurn(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	fake := newFakeModel(t, `{"message":{"content":"有回答"}}`, `{"message":{"content":"也有回答"}}`)
	seedFakeProvider(t, ts, fake.server.URL)

	sid, _ := chatTurn(t, ts, adminToken, 0, "第一条")
	_, body := chatTurn(t, ts, adminToken, sid, "第二条")
	turn2 := turnIDFromSSE(t, body)

	finished := func() []map[string]any {
		res := doJSON(t, "GET", ts.URL+fmt.Sprintf("/api/v1/chat/history?session_id=%d", sid), adminToken, nil)
		var h struct {
			Items []map[string]any `json:"items"`
		}
		_ = json.NewDecoder(res.Body).Decode(&h)
		res.Body.Close()
		return h.Items
	}
	// 答完的轮次不该被标为中断
	items := finished()
	last := items[len(items)-1]
	if last["interrupted"] == true {
		t.Fatalf("正常答完的轮次被标为中断: %+v", last)
	}

	// 造「只剩用户消息」的中断残局：删掉本轮的回答行，模拟断线未答完
	if _, err := ts.db.Exec(`DELETE FROM chat_messages WHERE turn_id = ? AND role <> 'user'`, turn2); err != nil {
		t.Fatal(err)
	}
	items = finished()
	last = items[len(items)-1]
	if last["role"] != "user" || last["interrupted"] != true {
		t.Fatalf("尾条应为 interrupted 的 user 消息，got %+v", last)
	}
	// 关键：消息还在（不因网络状况删用户消息）
	var n int
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM chat_messages WHERE turn_id = ?`, turn2).Scan(&n)
	if n == 0 {
		t.Fatal("中断轮次的用户消息被删了")
	}
}

// TestChatBootstrap ：首屏一次拿全可用性 + 欢迎语 + chips；欢迎语模板随空间事实变化。
func TestChatBootstrap(t *testing.T) {
	ts, adminToken, girlToken := newPostTestEnv(t)

	// 无模型配置：available=false + 给出可执行指引
	res := doJSON(t, "GET", ts.URL+"/api/v1/chat/bootstrap", girlToken, nil)
	var b struct {
		Available bool     `json:"available"`
		Reason    string   `json:"reason"`
		Welcome   string   `json:"welcome"`
		Chips     []string `json:"chips"`
	}
	_ = json.NewDecoder(res.Body).Decode(&b)
	res.Body.Close()
	if b.Available || !strings.Contains(b.Reason, "后台") {
		t.Fatalf("未配模型时应不可用并指引后台: %+v", b)
	}
	if len(b.Chips) == 0 {
		t.Fatal("chips 缺失")
	}
	if b.Welcome != "想记点什么？" {
		t.Fatalf("无恋爱开始日应给中性文案, got %q", b.Welcome)
	}

	// 设恋爱开始日 → 出现「在一起第 N 天」。
	// 实现按本地时区解析日期，这里用 Go 生成日期值，避免 SQLite date('now') 的 UTC
	// 与本地时区在本地 0 点到 UTC 0 点之间错出一天（北京时间 0:00–8:00 必现）。
	loveStart := time.Now().AddDate(0, 0, -100).Format("2006-01-02")
	if _, err := ts.db.Exec(`UPDATE space SET love_start_date = ?`, loveStart); err != nil {
		t.Fatal(err)
	}
	res = doJSON(t, "GET", ts.URL+"/api/v1/chat/bootstrap", girlToken, nil)
	_ = json.NewDecoder(res.Body).Decode(&b)
	res.Body.Close()
	if !strings.Contains(b.Welcome, "第 101 天") {
		t.Fatalf("welcome = %q, want 含「第 101 天」（含首日 +1）", b.Welcome)
	}

	// 配好模型 → available=true；admin 不得用于 APP，但接口本身按 JWT 鉴权即可
	fake := newFakeModel(t, `{"message":{"content":"hi"}}`)
	seedFakeProvider(t, ts, fake.server.URL)
	res = doJSON(t, "GET", ts.URL+"/api/v1/chat/bootstrap", adminToken, nil)
	_ = json.NewDecoder(res.Body).Decode(&b)
	res.Body.Close()
	if !b.Available || b.Reason != "" {
		t.Fatalf("配好模型后应可用: %+v", b)
	}
}
