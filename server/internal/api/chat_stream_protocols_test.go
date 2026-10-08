package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// ---- 四协议原生流式的端到端：增量下发、断连取舍、跨帧累积 ----
//
// 假上游按「脚本」回 SSE 帧；与 chat_protocols_test.go 的整块 JSON 假上游互补。
// 这些用例覆盖的是 adapter 与服务端在**流式**下的契约，整块用例覆盖不到。

// sseUpstream 起一个按帧序列回 SSE 的假上游；帧体不含 "data: " 前缀，
// 以 "event:" 开头或为空串的行按原样写出（供 anthropic / responses 的事件名）。
// failAfter 为「发够 N 帧后硬断连接」的触发帧数（0 = 不断）。
func sseUpstream(t *testing.T, frames []string, failAfter int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		for i, f := range frames {
			if failAfter > 0 && i >= failAfter {
				// 已下发的帧先落到 socket，再断——模拟上游中途挂掉
				if fl != nil {
					fl.Flush()
				}
				if hj, ok := w.(http.Hijacker); ok {
					conn, _, err := hj.Hijack()
					if err == nil {
						_ = conn.Close()
						return
					}
				}
				panic(http.ErrAbortHandler)
			}
			if f == "" || strings.HasPrefix(f, "event:") {
				fmt.Fprintf(w, "%s\n", f)
			} else {
				fmt.Fprintf(w, "data: %s\n\n", f)
			}
			if fl != nil {
				fl.Flush()
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// protocolSSEScript 返回某协议下「正文分两帧」的帧序列。
func protocolSSEScript(apiType, text1, text2 string) []string {
	switch apiType {
	case "openai-chat":
		return []string{
			`{"choices":[{"delta":{"content":"` + text1 + `"}}]}`,
			`{"choices":[{"delta":{"content":"` + text2 + `"}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		}
	case "openai-responses":
		return []string{
			`{"type":"response.output_text.delta","item_id":"m1","delta":"` + text1 + `"}`,
			`{"type":"response.output_text.delta","item_id":"m1","delta":"` + text2 + `"}`,
			`{"type":"response.completed","response":{"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}`,
		}
	case "anthropic":
		return []string{
			`{"type":"message_start","message":{"usage":{"input_tokens":1,"output_tokens":0}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"` + text1 + `"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"` + text2 + `"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
			`{"type":"message_stop"}`,
		}
	case "gemini":
		return []string{
			`{"candidates":[{"content":{"parts":[{"text":"` + text1 + `"}]},"finishReason":"STOP"}]}`,
			`{"candidates":[{"content":{"parts":[{"text":"` + text2 + `"}]},"finishReason":"STOP"}]}`,
			`{"candidates":[{"content":{"parts":[]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":2,"totalTokenCount":3}}`,
		}
	}
	return nil
}

// framesBeforeFirstDelta 返回「首个正文增量之前」的帧数（含该增量帧本身，
// 即 failAfter = 该值 时正好已发出一个正文增量）。anthropic 的 message_start /
// content_block_start 不带正文，若按固定帧数断连会退化成「首帧前失败」。
func framesBeforeFirstDelta(apiType string) int {
	switch apiType {
	case "anthropic":
		return 3 // message_start + content_block_start + 第 1 个 text_delta
	default:
		return 1
	}
}

// protocolToolScript 返回某协议下「工具参数跨多帧」的帧序列（参数拼起来是 {"content":"x"}）。
func protocolToolScript(apiType string) []string {
	switch apiType {
	case "openai-chat":
		return []string{
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c0","type":"function","function":{"name":"propose_post","arguments":"{\"content\":"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"今天去看了电影\"}"}}]},"finish_reason":"tool_calls"}]}`,
		}
	case "openai-responses":
		return []string{
			`{"type":"response.output_item.added","item":{"type":"function_call","id":"fc0","call_id":"call_0","name":"propose_post","arguments":""}}`,
			`{"type":"response.function_call_arguments.delta","item_id":"fc0","delta":"{\"content\":"}`,
			`{"type":"response.function_call_arguments.delta","item_id":"fc0","delta":"\"今天去看了电影\"}"}`,
			`{"type":"response.output_item.done","item":{"type":"function_call","id":"fc0","call_id":"call_0","name":"propose_post","arguments":"{\"content\":\"今天去看了电影\"}"}}`,
		}
	case "anthropic":
		return []string{
			`{"type":"message_start","message":{"usage":{"input_tokens":1,"output_tokens":0}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_0","name":"propose_post"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"content\":"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"今天去看了电影\"}"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":2}}`,
		}
	case "gemini":
		// Gemini 的参数不分片（args 是 object），整块给出
		return []string{
			`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"propose_post","args":{"content":"今天去看了电影","mood":"开心"}}}]},"finishReason":"STOP"}]}`,
		}
	}
	return nil
}

func TestChatStreamingPerProtocol(t *testing.T) {
	for _, apiType := range []string{"openai-chat", "openai-responses", "anthropic", "gemini"} {
		t.Run(apiType, func(t *testing.T) {
			ts, token, _ := newPostTestEnv(t)
			up := sseUpstream(t, protocolSSEScript(apiType, "今天", "很好"), 0)
			seedFakeProviderType(t, ts, apiType, "m1", up.URL)

			res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", token, map[string]string{"content": "在吗"})
			body := sseBody(t, res)
			// 分帧内容必须都到，且拼起来是完整句子
			if !strings.Contains(body, "今天") || !strings.Contains(body, "很好") {
				t.Fatalf("SSE 缺少分帧正文:\n%s", body)
			}
			if !strings.Contains(body, "event: turn.end") {
				t.Fatalf("SSE 缺少 turn.end:\n%s", body)
			}
			if strings.Contains(body, "event: error") {
				t.Fatalf("不该报错:\n%s", body)
			}
		})
	}
}

// TestChatStreamingToolCallPerProtocol 工具参数跨帧到达时，
// 卡片必须只出现一次且参数完整（分片在 adapter 内拼完，不产生半截调用）。
func TestChatStreamingToolCallPerProtocol(t *testing.T) {
	for _, apiType := range []string{"openai-chat", "openai-responses", "anthropic", "gemini"} {
		t.Run(apiType, func(t *testing.T) {
			ts, token, _ := newPostTestEnv(t)
			up := sseUpstream(t, protocolToolScript(apiType), 0)
			seedFakeProviderType(t, ts, apiType, "m1", up.URL)

			res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", token, map[string]string{"content": "帮我记一条动态"})
			body := sseBody(t, res)
			if n := strings.Count(body, "event: proposal"); n != 1 {
				t.Fatalf("proposal 事件数 = %d, want 1（分片不得产生多次调用）:\n%s", n, body)
			}
			if !strings.Contains(body, `"intent":"propose_post"`) {
				t.Fatalf("缺少 propose_post 卡片:\n%s", body)
			}
		})
	}
}

// TestChatStreamingFailAfterFirstDeltaNoFailover 链头已下发增量后断连 →
// **不换链**（否则用户会看到半句话被另一个模型接下去），按 interrupted 落库。
func TestChatStreamingFailAfterFirstDeltaNoFailover(t *testing.T) {
	for _, apiType := range []string{"openai-chat", "openai-responses", "anthropic", "gemini"} {
		t.Run(apiType, func(t *testing.T) {
			ts, token, _ := newPostTestEnv(t)
			// 链头：发 2 帧后断连
			head := sseUpstream(t, protocolSSEScript(apiType, "半句", "话"), framesBeforeFirstDelta(apiType))
			headID := seedNamedProvider(t, ts, apiType, "head", "m1", head.URL, 0)
			// 链尾：本可正常回复，但不应被调用
			tail := sseUpstream(t, protocolSSEScript(apiType, "链尾", "顶上"), 0)
			tailID := seedNamedProvider(t, ts, apiType, "tail", "m1", tail.URL, 1)

			res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", token, map[string]string{"content": "在吗"})
			body := sseBody(t, res)
			if strings.Contains(body, "顶上") {
				t.Fatalf("已下发增量后不得换链续写:\n%s", body)
			}
			// 链尾不应产生 success 记录
			for _, r := range usageRows(t, ts) {
				if r.ProviderID == tailID && r.Status == "success" {
					t.Fatal("链尾被调用并成功——已下发增量后不得换链")
				}
			}
			// 链头应留下 failed 记录
			sawHeadFail := false
			for _, r := range usageRows(t, ts) {
				if r.ProviderID == headID && r.Status == "failed" {
					sawHeadFail = true
				}
			}
			if !sawHeadFail {
				t.Fatal("链头未记录 failed")
			}
		})
	}
}

// TestChatStreamingFailBeforeFirstDeltaFailover 链头在**首个增量之前**失败 →
// 换链是安全且必要的（用户还没看到任何内容）。
func TestChatStreamingFailBeforeFirstDeltaFailover(t *testing.T) {
	for _, apiType := range []string{"openai-chat", "openai-responses", "anthropic", "gemini"} {
		t.Run(apiType, func(t *testing.T) {
			ts, token, _ := newPostTestEnv(t)
			// 链头：立刻 500，无任何帧
			head := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "boom", http.StatusInternalServerError)
			}))
			t.Cleanup(head.Close)
			headID := seedNamedProvider(t, ts, apiType, "head", "m1", head.URL, 0)
			tail := sseUpstream(t, protocolSSEScript(apiType, "链尾", "顶上"), 0)
			tailID := seedNamedProvider(t, ts, apiType, "tail", "m1", tail.URL, 1)

			res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", token, map[string]string{"content": "在吗"})
			body := sseBody(t, res)
			if !strings.Contains(body, "链尾") || !strings.Contains(body, "顶上") {
				t.Fatalf("链尾未顶上:\n%s", body)
			}
			if !strings.Contains(body, "event: turn.end") {
				t.Fatalf("缺少 turn.end:\n%s", body)
			}

			rows := usageRows(t, ts)
			if len(rows) != 2 {
				t.Fatalf("usage 行数 = %d, want 2: %+v", len(rows), rows)
			}
			if rows[0].ProviderID != headID || rows[0].Status != "failed" {
				t.Errorf("rows[0] = (%d,%s), want (%d,failed)", rows[0].ProviderID, rows[0].Status, headID)
			}
			if rows[1].ProviderID != tailID || rows[1].Status != "success" {
				t.Errorf("rows[1] = (%d,%s), want (%d,success)", rows[1].ProviderID, rows[1].Status, tailID)
			}
		})
	}
}

// TestChatStreamingGeminiSignatureRoundTrip gemini 工具往返：第一轮响应带 part 级
// thoughtSignature → 工具结果回灌后**同一轮内的第二次上游调用**必须原样带回签名
// （真实 gemini 3 缺失即 400）。这是「配了 Gemini 3 走工具链就挂」的直接回归守卫。
//
// 用读工具（find_posts）而非写工具：写工具的提案是 pending 卡片，按设计不进模型上下文，
// 而读工具的 tool 结果在同一轮内就回灌，签名必须跟着走。
func TestChatStreamingGeminiSignatureRoundTrip(t *testing.T) {
	ts, token, _ := newPostTestEnv(t)

	var mu sync.Mutex
	var bodies []string
	call := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		call++
		n := call
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if n == 1 {
			// 第一轮：读工具调用 + **part 级**签名（与 functionCall 平级）
			fmt.Fprint(w, "data: "+`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"find_posts","args":{"query":"海边"}},"thoughtSignature":"sig-e2e"}]},"finishReason":"STOP"}]}`+"\n\n")
		} else {
			fmt.Fprint(w, "data: "+`{"candidates":[{"content":{"parts":[{"text":"查到了"}]},"finishReason":"STOP"}]}`+"\n\n")
		}
	}))
	t.Cleanup(up.Close)
	seedFakeProviderType(t, ts, "gemini", "gemini-3.1-pro-preview", up.URL)

	res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", token, map[string]string{"content": "找找海边的照片"})
	body := sseBody(t, res)
	if !strings.Contains(body, "查到了") {
		t.Fatalf("第二轮回复未下发")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) < 2 {
		t.Fatalf("上游调用数 = %d, want ≥2（工具结果需回灌）", len(bodies))
	}
	second := bodies[1]
	if !strings.Contains(second, "thoughtSignature") {
		t.Fatalf("第二次调用未回传 thoughtSignature")
	}
	if !strings.Contains(second, "sig-e2e") {
		t.Fatalf("回传的签名值不对")
	}
	// 必须是 part 级（与 functionCall 平级），不能嵌进 functionCall 内
	if strings.Contains(second, `"functionCall":{"thoughtSignature"`) ||
		strings.Contains(second, `"functionCall": {"thoughtSignature"`) {
		t.Fatalf("签名被嵌进 functionCall 内（层级错误）")
	}
}

// TestChatStreamingOpenAIChatNoSignatureLeak openai-chat 直传 Message，
// gemini 签名不得随历史泄漏给该协议的上游（未知字段可能被拒或误当参数）。
func TestChatStreamingOpenAIChatNoSignatureLeak(t *testing.T) {
	ts, token, _ := newPostTestEnv(t)

	var mu sync.Mutex
	var bodies []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(b)
		mu.Lock()
		bodies = append(bodies, string(b))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(up.Close)
	seedFakeProviderType(t, ts, "openai-chat", "gpt-5.5", up.URL)

	// 直接往上下文塞一条带签名的 assistant 行（模拟曾经用 gemini 后切到 openai-chat）
	if _, err := ts.db.Exec(`INSERT INTO chat_sessions (user_id, title) VALUES (1, 's')`); err != nil {
		t.Fatal(err)
	}
	var sid int64
	if err := ts.db.QueryRow(`SELECT id FROM chat_sessions ORDER BY id DESC LIMIT 1`).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	if err := ts.db.AppendContext(context.Background(), 1, sid, "1", "user", "在吗", "", "", "", 1, "dialogue", false); err != nil {
		t.Fatal(err)
	}
	// tool_calls_json 里带 signature 字段（落库形状）
	tcJSON := `[{"id":"c1","type":"function","signature":"sig-leak","function":{"name":"propose_post","arguments":"{}"}}]`
	if err := ts.db.AppendContext(context.Background(), 1, sid, "1", "assistant", "", tcJSON, "", "", 1, "dialogue", false); err != nil {
		t.Fatal(err)
	}

	res := doJSON(t, "POST", ts.URL+"/api/v1/chat/message", token, map[string]any{"content": "在吗", "session_id": sid})
	_ = sseBody(t, res)

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) == 0 {
		t.Fatalf("上游未被调用")
	}
	for _, b := range bodies {
		if strings.Contains(b, "sig-leak") {
			t.Fatalf("签名泄漏到 openai-chat 上游")
		}
	}
}
