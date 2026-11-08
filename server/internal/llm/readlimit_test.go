package llm

import (
	"context"
	"io"
	"net/http"
	"testing"
)

// countingBody 一个上位机响应体：内容全是 'x'（非法 JSON），记录被读走的字节数。
type countingBody struct {
	remain int
	read   int
}

func (b *countingBody) Read(p []byte) (int, error) {
	if b.remain == 0 {
		return 0, io.EOF
	}
	n := len(p)
	if n > b.remain {
		n = b.remain
	}
	for i := 0; i < n; i++ {
		p[i] = 'x'
	}
	b.remain -= n
	b.read += n
	return n, nil
}

func (b *countingBody) Close() error { return nil }

// countingRT 拦截所有请求，直接回上面那个响应体。
type countingRT struct{ body *countingBody }

func (rt countingRT) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       rt.body,
	}, nil
}

// 上游响应体的读取必须有上限：整块响应与「忽略 stream 直接回 JSON」的兜底路径
// 都会先读全量再解析，不设上限时一个故障/恶意上游就能把进程内存吃满。
// 五处调用点各跑一遍，断言实际读走的字节不超过 maxUpstreamBody。
func TestUpstreamBodyReadIsLimited(t *testing.T) {
	total := maxUpstreamBody + (1 << 20) // 比上限多 1 MiB，够区分「读了全部」与「读到上限」
	cases := []struct{ name, apiType string }{
		{"openai-chat", APITypeOpenAIChat},
		{"openai-responses", APITypeOpenAIResponses},
		{"anthropic", APITypeAnthropic},
		{"gemini", APITypeGemini},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewForProtocol("https://upstream.invalid", "k", "m", tc.apiType, 0)
			if err != nil {
				t.Fatalf("NewForProtocol: %v", err)
			}
			body := &countingBody{remain: total}
			c.http = &http.Client{Transport: countingRT{body: body}}

			// 解析必然失败（截断的 'x' 不是 JSON）：本用例只看读走了多少字节
			if _, err := c.Chat(context.Background(), &ChatRequest{
				Messages: []Message{{Role: "user", Content: "hi"}},
			}); err == nil {
				t.Fatal("非法响应体应返回解析错误")
			}
			if body.read == 0 {
				t.Fatal("一个字节都没读：用例没走到读响应体那条路径")
			}
			if body.read > maxUpstreamBody {
				t.Fatalf("读走 %d 字节，超过上限 %d —— 无上限的上游响应会把进程内存吃满", body.read, maxUpstreamBody)
			}
		})
	}

	// sse.go：非事件流兜底（中转网关忽略 stream:true 直接回整块 JSON 的路径）
	t.Run("非事件流兜底", func(t *testing.T) {
		body := &countingBody{remain: total}
		res := &http.Response{StatusCode: http.StatusOK, Body: body}
		if _, err := streamFallbackReplay(res, parseOpenAIChatBody, func(Delta) {}); err == nil {
			t.Fatal("非法响应体应返回解析错误")
		}
		if body.read == 0 {
			t.Fatal("一个字节都没读：用例没走到兜底读体路径")
		}
		if body.read > maxUpstreamBody {
			t.Fatalf("兜底路径读走 %d 字节，超过上限 %d", body.read, maxUpstreamBody)
		}
	})
}
