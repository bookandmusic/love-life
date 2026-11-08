package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestHeaderTimeoutConfigurable 验收「供应商响应头超时可配」：慢上游 + 短配置超时 →
// 按配置时间（而非默认 30s）判死；0 = 默认。响应头不出，连接期超时即触发。
func TestHeaderTimeoutConfigurable(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second) // 比任何被测超时都长：响应头永远等不到
		w.WriteHeader(200)
	}))
	defer slow.Close()

	cases := []struct {
		name    string
		timeout time.Duration
		wantMax time.Duration // 实测耗时必须明显小于它
	}{
		{"配置1s按1s判死", 1 * time.Second, 2 * time.Second},
		{"0回退默认30s不被3s慢上游触发", 0, 5 * time.Second}, // 3s 的慢上游不应杀死默认 30s
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewForProtocol(slow.URL, "k", "m", APITypeOpenAIChat, tc.timeout)
			if err != nil {
				t.Fatalf("NewForProtocol: %v", err)
			}
			start := time.Now()
			req := &ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}}
			_, err = c.StreamChat(context.Background(), req, func(Delta) {})
			elapsed := time.Since(start)
			if err == nil {
				t.Fatalf("慢上游应报错，却成功了")
			}
			if elapsed >= tc.wantMax {
				t.Fatalf("耗时 %v 超过预期上限 %v（超时配置未生效）", elapsed, tc.wantMax)
			}
			if tc.timeout > 0 && elapsed < tc.timeout {
				t.Fatalf("耗时 %v 短于配置超时 %v，不像按配置判死", elapsed, tc.timeout)
			}
		})
	}
}

// TestHTTPClientForShared 按超时值共享客户端：同值同实例、异值异实例、0 与默认同池。
func TestHTTPClientForShared(t *testing.T) {
	a := httpClientFor(time.Second)
	b := httpClientFor(time.Second)
	if a != b {
		t.Fatalf("同超时值应返回同一客户端实例")
	}
	if a.Timeout != 0 {
		t.Fatalf("流式请求不得设请求总超时")
	}
	if a.Transport.(*http.Transport).ResponseHeaderTimeout != time.Second {
		t.Fatalf("ResponseHeaderTimeout 应等于配置值")
	}
	if httpClientFor(0).Transport.(*http.Transport).ResponseHeaderTimeout != headerTimeoutDefault {
		t.Fatalf("0 应回退默认 %v", headerTimeoutDefault)
	}
}
