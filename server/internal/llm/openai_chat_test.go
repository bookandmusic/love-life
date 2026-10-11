package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeChatUpstream 起假 Chat Completions 上游，记录收到的请求体与方法/路径/鉴权头，
// 按脚本依次回放（脚本元素即完整响应体）。每次调用推进脚本索引。
func fakeChatUpstream(t *testing.T, script ...string) (*httptest.Server, **http.Request, *[]byte) {
	t.Helper()
	var got *http.Request
	var gotBody []byte
	idx := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		gotBody, _ = io.ReadAll(r.Body)
		i := idx
		if i >= len(script) {
			i = len(script) - 1
		}
		idx++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(script[i]))
	}))
	t.Cleanup(srv.Close)
	return srv, &got, &gotBody
}

func TestOpenAIChatAdapter_RequestShape(t *testing.T) {
	// 单条纯文本响应即可，重点验证请求形状
	srv, gotP, bodyP := fakeChatUpstream(t, `{"choices":[{"message":{"content":"hi"},"finish_reason":"stop"}]}`)

	adp := openaiChatAdapter{}
	req := &ChatRequest{
		Messages: []Message{{Role: "user", Content: "ping"}},
		Tools: []Tool{{Type: "function", Function: ToolFunction{
			Name: "get_weather", Description: "d", Parameters: map[string]any{"type": "object"},
		}}},
	}
	resp, err := adp.Chat(context.Background(), httpClientFor(0), srv.URL, "sk-test", "gpt-4o", req)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if *gotP == nil {
		t.Fatal("upstream request not captured")
	}
	r := *gotP

	// 方法、路径、鉴权头
	if r.Method != http.MethodPost {
		t.Errorf("method = %q, want POST", r.Method)
	}
	if r.URL.Path != "/chat/completions" {
		t.Errorf("path = %q, want /chat/completions", r.URL.Path)
	}
	if r.Header.Get("Authorization") != "Bearer sk-test" {
		t.Errorf("Authorization = %q, want Bearer sk-test", r.Header.Get("Authorization"))
	}
	if r.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", r.Header.Get("Content-Type"))
	}

	// 请求体形状：model / messages / tools 三字段
	var sent map[string]any
	if err := json.Unmarshal(*bodyP, &sent); err != nil {
		t.Fatalf("unmarshal sent body: %v: %s", err, string(*bodyP))
	}
	if sent["model"] != "gpt-4o" {
		t.Errorf("body.model = %v, want gpt-4o", sent["model"])
	}
	msgs, _ := sent["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("body.messages len = %d, want 1", len(msgs))
	}
	first, _ := msgs[0].(map[string]any)
	if first["role"] != "user" || first["content"] != "ping" {
		t.Errorf("body.messages[0] = %v, want role=user content=ping", first)
	}
	tools, _ := sent["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("body.tools len = %d, want 1", len(tools))
	}

	// 响应规范化映射
	if len(resp.Choices) != 1 || resp.Choices[0].Message.Content != "hi" {
		t.Fatalf("resp = %+v, want choices[0].content=hi", resp)
	}
}

func TestOpenAIChatAdapter_ToolCallMapping(t *testing.T) {
	body := `{"choices":[{"message":{"content":"","tool_calls":[` +
		`{"id":"c1","type":"function","function":{"name":"create_post","arguments":"{\"content\":\"hi\",\"mood\":\"开心\"}"}}` +
		`]},"finish_reason":"tool_calls"}]}`
	srv, _, _ := fakeChatUpstream(t, body)

	resp, err := openaiChatAdapter{}.Chat(context.Background(), httpClientFor(0), srv.URL, "k", "m", &ChatRequest{
		Messages: []Message{{Role: "user", Content: "记一条"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	tc := resp.Choices[0].Message.ToolCalls
	if len(tc) != 1 || tc[0].ID != "c1" || tc[0].Type != "function" {
		t.Fatalf("tool_calls = %+v", tc)
	}
	if tc[0].Function.Name != "create_post" {
		t.Errorf("function.name = %q, want create_post", tc[0].Function.Name)
	}
	// arguments 必须是 JSON 字符串（OpenAI 协议特性）
	if tc[0].Function.Arguments != `{"content":"hi","mood":"开心"}` {
		t.Errorf("function.arguments = %q, want escaped JSON string", tc[0].Function.Arguments)
	}
}

func TestOpenAIChatAdapter_Errors(t *testing.T) {
	t.Run("上游 out.Error 非空", func(t *testing.T) {
		srv, _, _ := fakeChatUpstream(t, `{"choices":[{"message":{"content":"x"}}],"error":{"message":"invalid api key"}}`)
		_, err := openaiChatAdapter{}.Chat(context.Background(), httpClientFor(0), srv.URL, "k", "m", &ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}},
		})
		if err == nil || !strings.Contains(err.Error(), "模型服务错误") || !strings.Contains(err.Error(), "invalid api key") {
			t.Fatalf("err = %v, want 模型服务错误 + invalid api key", err)
		}
	})

	t.Run("choices 为空", func(t *testing.T) {
		srv, _, _ := fakeChatUpstream(t, `{"choices":[]}`)
		_, err := openaiChatAdapter{}.Chat(context.Background(), httpClientFor(0), srv.URL, "k", "m", &ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}},
		})
		if err == nil || !strings.Contains(err.Error(), "模型返回空响应") {
			t.Fatalf("err = %v, want 模型返回空响应", err)
		}
	})

	t.Run("响应体非 JSON", func(t *testing.T) {
		srv, _, _ := fakeChatUpstream(t, `not-json`)
		_, err := openaiChatAdapter{}.Chat(context.Background(), httpClientFor(0), srv.URL, "k", "m", &ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}},
		})
		if err == nil || !strings.Contains(err.Error(), "decode model response") {
			t.Fatalf("err = %v, want decode model response", err)
		}
	})

	t.Run("上游不可达", func(t *testing.T) {
		// 起一个立刻关闭的服务器模拟连接错误
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		srv.Close()
		_, err := openaiChatAdapter{}.Chat(context.Background(), httpClientFor(0), srv.URL, "k", "m", &ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}},
		})
		if err == nil {
			t.Fatal("want connection error")
		}
	})
}

// TestClientChatDelegates 验证 Client.Chat 正确委托给 adapter（端到端走薄壳）。
func TestClientChatDelegates(t *testing.T) {
	srv, _, _ := fakeChatUpstream(t, `{"choices":[{"message":{"content":"ok"}}]}`)
	c, err := NewForProtocol(srv.URL, "k", "m", APITypeOpenAIChat, 0)
	if err != nil {
		t.Fatalf("NewForProtocol: %v", err)
	}
	resp, err := c.Chat(context.Background(), &ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Choices[0].Message.Content != "ok" {
		t.Fatalf("content = %q, want ok", resp.Choices[0].Message.Content)
	}
}
