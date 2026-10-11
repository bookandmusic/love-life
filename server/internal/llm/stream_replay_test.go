package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIChatStreamReplayNonSSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()
	var got []Delta
	finish, err := (openaiChatAdapter{}).StreamChat(context.Background(), httpClientFor(0), srv.URL, "k", "m", &ChatRequest{}, func(d Delta) {
		got = append(got, d)
	})
	if err != nil {
		t.Fatal(err)
	}
	if finish != "stop" || len(got) == 0 || got[0].Content != "ok" {
		t.Fatalf("finish=%q deltas=%+v", finish, got)
	}
}

// TestOpenAIChatStreamAccumulatesToolCallFragments 工具参数分片必须在 **adapter 内**
// 按 index 拼成一次完整调用后下发（AI模式设计「预算与并行」· tool_calls 增量累积：
// 「累积在 adapter 内部完成，Agent 层只看到完整的 tool_call」）。
// 回归的缺陷：adapter 曾把每帧分片原样抛出，由 Agent 层拼字符串——协议差异外泄到上层。
func TestOpenAIChatStreamAccumulatesToolCallFragments(t *testing.T) {
	frames := []string{
		// index 0：首片带 id/name + 参数前半截
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c0","type":"function","function":{"name":"find_posts","arguments":"{\"query\":"}}]},"finish_reason":""}]}`,
		// index 0：参数后半截（无 id/name）
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"海边\"}"}}]},"finish_reason":""}]}`,
		// index 1：另一路调用，整块到达
		`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"c1","type":"function","function":{"name":"find_albums","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		for _, f := range frames {
			fmt.Fprintf(w, "data: %s\n\n", f)
			if fl != nil {
				fl.Flush()
			}
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		if fl != nil {
			fl.Flush()
		}
	}))
	defer srv.Close()

	var got []Delta
	finish, err := (openaiChatAdapter{}).StreamChat(context.Background(), httpClientFor(0), srv.URL, "k", "m", &ChatRequest{}, func(d Delta) {
		got = append(got, d)
	})
	if err != nil {
		t.Fatal(err)
	}
	if finish != "tool_calls" {
		t.Fatalf("finish = %q, want tool_calls", finish)
	}
	var calls []*ToolCallDelta
	for _, d := range got {
		if d.ToolCall != nil {
			calls = append(calls, d.ToolCall)
		}
	}
	// 关键：三帧分片 → 只应有 2 次完整调用（不是 3 次半截）
	if len(calls) != 2 {
		t.Fatalf("tool_call 增量数 = %d，want 2（分片必须在 adapter 内拼完再发）: %+v", len(calls), calls)
	}
	if calls[0].ID != "c0" || calls[0].Name != "find_posts" {
		t.Fatalf("第 1 个调用形状不对: %+v", calls[0])
	}
	if calls[0].Arguments != `{"query":"海边"}` {
		t.Fatalf("参数未拼全: %q", calls[0].Arguments)
	}
	if calls[1].ID != "c1" || calls[1].Arguments != "{}" {
		t.Fatalf("第 2 个调用形状不对: %+v", calls[1])
	}
}
