package api

// AI 模式附件的传输契约（AI模式设计「图片处理与存储」）：
//   消息体只带 {name, mime, size} 元数据，**服务端不存文件、不存 base64**；
//   文件在「确认执行提案时」才从本机上传。本文件守住这条边界——
//   历史上曾额外上送 data_url(base64) 供服务端量体积，那是偏离设计的做法，已移除。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/chat"
)

// TestChatMediaPayloadHasNoFileContent：消息体带多余字段（如 data_url）应被忽略，
// 且落库的 attachment part 只含 {name, mime, size}——
// 服务端不因「传了内容」而存储或依赖它。
func TestChatMediaPayloadHasNoFileContent(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	fake := newFakeModel(t, `{"message":{"content":"好，这就发"}}`)
	seedFakeProvider(t, ts, fake.server.URL)

	// 故意带上 data_url（模拟旧客户端）：应被忽略而非报错
	res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]any{
		"content": "发张图",
		"media": []map[string]any{{
			"name": "海边.jpg", "type": "image/jpeg", "size": 2048,
			"data_url": "data:image/jpeg;base64," + strings.Repeat("A", 4096),
		}},
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("带 data_url 的请求应被正常受理（字段忽略），实得 %d", res.StatusCode)
	}
	body := sseBody(t, res)
	if !strings.Contains(body, "event: turn.end") {
		t.Fatalf("未正常收尾：\n%s", body)
	}

	// 落库的 parts：attachment 只有元数据，不出现 data_url / base64
	var parts string
	if err := ts.db.QueryRow(`SELECT parts FROM chat_messages WHERE role = 'user' ORDER BY id DESC LIMIT 1`).Scan(&parts); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(parts, "data_url") || strings.Contains(parts, "base64") {
		t.Fatalf("attachment part 不应含文件内容：%s", parts)
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(parts), &list); err != nil {
		t.Fatal(err)
	}
	var att map[string]any
	for _, p := range list {
		if p["type"] == "attachment" {
			att = p
		}
	}
	if att == nil {
		t.Fatalf("attachment part 缺失：%s", parts)
	}
	if att["name"] != "海边.jpg" || att["mime"] != "image/jpeg" || att["size"] != float64(2048) {
		t.Fatalf("attachment 应只含 {name, mime, size}：%+v", att)
	}
	// 允许的字段只有 part 类型标识 + 三项元数据（不得多出 data_url/内容类字段）
	allowed := map[string]bool{"type": true, "name": true, "mime": true, "size": true}
	for k := range att {
		if !allowed[k] {
			t.Fatalf("attachment 出现预期外字段 %q：%+v", k, att)
		}
	}
}

// TestChatMediaTotalSizeGuard：附件总量护栏按上报的 size 累加（不再依赖 base64 长度）。
func TestChatMediaTotalSizeGuard(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	fake := newFakeModel(t, `{"message":{"content":"收到"}}`)
	seedFakeProvider(t, ts, fake.server.URL)

	// 两个各 6MB 的附件 → 12MB，超 8MB 上限
	big := []map[string]any{
		{"name": "a.mp4", "type": "video/mp4", "size": 6 << 20},
		{"name": "b.mp4", "type": "video/mp4", "size": 6 << 20},
	}
	res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]any{
		"content": "带两个大视频", "media": big,
	})
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("附件总量超限应 400，实得 %d", res.StatusCode)
	}

	// 上限内（两个各 1MB）→ 放行
	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]any{
		"content": "带两个小文件",
		"media": []map[string]any{
			{"name": "a.jpg", "type": "image/jpeg", "size": 1 << 20},
			{"name": "b.jpg", "type": "image/jpeg", "size": 1 << 20},
		},
	})
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("上限内应放行，实得 %d", res.StatusCode)
	}
}

// TestChatMediaCountStillCapped：数量上限仍为 9（本轮未放开）。
func TestChatMediaCountStillCapped(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	media := make([]map[string]any, 0, chat.MaxChatMedia+1)
	for i := 0; i <= chat.MaxChatMedia; i++ {
		media = append(media, map[string]any{
			"name": fmt.Sprintf("p%d.jpg", i), "type": "image/jpeg", "size": 1024,
		})
	}
	res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]any{
		"content": "超过上限", "media": media,
	})
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("超过 %d 个应 400，实得 %d", chat.MaxChatMedia, res.StatusCode)
	}
}

// TestChatTurnEmitsSingleProposal：一轮只出一张卡（AI模式设计「预算与并行」：
// 写工具串行，一旦产出提案即停止本轮）。回归守卫——曾用 continue 导致同响应里的
// 第二个提案也出卡（实测一次出了「删除动态」+「新建相册」两张）。
func TestChatTurnEmitsSingleProposal(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{"content": "海边"})
	res.Body.Close()

	// 假上游一次响应里回两个提案 tool_call
	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[
			{"id":"p1","type":"function","function":{"name":"propose_post","arguments":"{\"content\":\"第一条\"}"}},
			{"id":"p2","type":"function","function":{"name":"propose_post","arguments":"{\"content\":\"第二条\"}"}}
		]}}`,
		`{"message":{"content":"好的"}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	res = doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]string{"content": "发两条动态"})
	body := sseBody(t, res)

	n := strings.Count(body, "event: proposal")
	if n != 1 {
		t.Fatalf("一轮只应出一张卡，实得 %d 张：\n%s", n, body)
	}
	// 库里也只该有 1 张 pending 卡
	var pending int
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM confirm_actions WHERE status = 'pending'`).Scan(&pending)
	if pending != 1 {
		t.Fatalf("pending 卡应为 1，实得 %d", pending)
	}
	if !strings.Contains(body, `"stop_reason":"proposal"`) {
		t.Fatalf("应以 proposal 收尾：\n%s", body)
	}
}

// TestReadToolUiPersistedInParts：读工具的结果卡（含 result_id 句柄）必须落进 parts，
// 否则刷新后卡片消失、无法继续勾选引用（AI模式设计「结果集句柄与点选优先」：
// 句柄随会话存活，刷新后仍可引用旧结果卡的条目）。
func TestReadToolUiPersistedInParts(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	for _, c := range []string{"海边的日落", "山里的清晨"} {
		res := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{"content": c})
		res.Body.Close()
	}
	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"f1","type":"function","function":{"name":"find_posts","arguments":"{\"query\":\"海边\"}"}}]}}`,
		`{"message":{"content":"找到 1 条"}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", adminToken, map[string]string{"content": "找海边的"})
	body := sseBody(t, res)
	// SSE 的 tool.end 带了 ui
	if !strings.Contains(body, `"result_id":"r_`) {
		t.Fatalf("tool.end 应带结果卡与句柄：\n%s", body)
	}

	var parts string
	if err := ts.db.QueryRow(`SELECT parts FROM chat_messages WHERE role = 'assistant' ORDER BY id DESC LIMIT 1`).Scan(&parts); err != nil {
		t.Fatal(err)
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(parts), &list); err != nil {
		t.Fatal(err)
	}
	var toolPart map[string]any
	for _, p := range list {
		if p["type"] == "tool" {
			toolPart = p
		}
	}
	if toolPart == nil {
		t.Fatalf("parts 里应有 tool 块：%s", parts)
	}
	ui, ok := toolPart["ui"].(map[string]any)
	if !ok {
		t.Fatalf("tool part 必须带 ui（否则刷新后结果卡消失）：%s", parts)
	}
	if ui["result_id"] == "" || ui["result_id"] == nil {
		t.Fatalf("ui 必须带 result_id 句柄：%+v", ui)
	}
	if recs, ok := ui["records"].([]any); !ok || len(recs) == 0 {
		t.Fatalf("ui 必须带 records：%+v", ui)
	}
}
