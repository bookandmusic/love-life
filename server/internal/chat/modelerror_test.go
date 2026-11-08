package chat

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/llm"
)

// 面向用户的模型报错文案：只认类型与状态码分类，且**不得把原始错误透给用户**。
//
// 触发本组测试的真实事故：AI 模式直接显示
// `Post "http://192.0.2.10:3001/v1/chat/completions": net/http: timeout awaiting response headers`
// ——用户看到的是 Go 错误串与内网地址，既读不懂也不该外泄。
func TestModelErrorText(t *testing.T) {
	// 典型的连接层失败：*url.Error 包着底层错误（Go 的 http.Client 就是这么返回的）
	connErr := &url.Error{
		Op:  "Post",
		URL: "http://192.0.2.10:3001/v1/chat/completions",
		Err: &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect: connection refused")},
	}
	timeoutErr := &url.Error{
		Op:  "Post",
		URL: "http://192.0.2.10:3001/v1/chat/completions",
		Err: timeoutNetErr{},
	}

	cases := []struct {
		name          string
		err           error
		wantRetryable bool
		// 文案里绝不允许出现的东西（原始错误串、地址、Go 术语）
		mustNotContain []string
	}{
		{"超时（net.Error.Timeout）", timeoutErr, true, nil},
		{"连不上（*url.Error）", connErr, false, nil},
		{"上游 401 凭据无效", &llm.UpstreamError{StatusCode: 401, Message: "invalid api key"}, false, nil},
		{"上游 403", &llm.UpstreamError{StatusCode: 403}, false, nil},
		{"上游 429 限流", &llm.UpstreamError{StatusCode: 429}, true, nil},
		{"上游 400 参数被拒", &llm.UpstreamError{StatusCode: 400, Message: "Unknown parameter: 'max_tokens'."}, false, nil},
		{"上游 500", &llm.UpstreamError{StatusCode: 500}, true, nil},
		{"上游 502", &llm.UpstreamError{StatusCode: 502}, true, nil},
		{"流式帧内错误（无状态码）", &llm.UpstreamError{Message: "overloaded"}, true, nil},
		{"协议不支持", llm.ErrUnsupportedAPIType, false, nil},
		{"未知错误", errors.New("some internal failure"), true, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 包一层模拟真实链路（chatCallStream 返回的 err 往往已被 %w 包过）
			err := fmt.Errorf("call model: %w", tc.err)
			text, retryable := modelErrorText(err)

			if text == "" {
				t.Fatal("文案不得为空")
			}
			if retryable != tc.wantRetryable {
				t.Errorf("retryable = %v, want %v（文案：%s）", retryable, tc.wantRetryable, text)
			}
			// 核心断言：不得把原始错误串或内网地址透给用户
			for _, bad := range append([]string{
				"192.0.2.10", "net/http", "http://", "https://", "Post \"",
				"connect:", "connection refused", "some internal failure",
				"Unknown parameter", "invalid api key", "overloaded", "call model:",
			}, tc.mustNotContain...) {
				if strings.Contains(text, bad) {
					t.Errorf("用户文案不得包含 %q，实得：%s", bad, text)
				}
			}
		})
	}
}

// TestModelErrorTextNeverLeaksRawError 对每个分支都断言「文案 != 原始错误」，
// 防止将来有人图省事把 err.Error() 又塞回去。
func TestModelErrorTextNeverLeaksRawError(t *testing.T) {
	errs := []error{
		&url.Error{Op: "Post", URL: "http://192.0.2.10:3001/v1/chat/completions", Err: timeoutNetErr{}},
		&llm.UpstreamError{StatusCode: 400, Message: "Unknown parameter: 'max_tokens'."},
		llm.ErrUnsupportedAPIType,
		context.DeadlineExceeded,
	}
	for _, e := range errs {
		text, _ := modelErrorText(e)
		if text == e.Error() {
			t.Errorf("文案与原始错误相同，等于直出：%s", text)
		}
	}
}

// timeoutNetErr 满足 net.Error 且 Timeout() 为真。
type timeoutNetErr struct{}

func (timeoutNetErr) Error() string   { return "net/http: timeout awaiting response headers" }
func (timeoutNetErr) Timeout() bool   { return true }
func (timeoutNetErr) Temporary() bool { return true }
