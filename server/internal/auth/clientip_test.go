package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func reqFrom(remoteAddr string, headers map[string]string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	r.RemoteAddr = remoteAddr
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

// 未配置受信反代时，转发头一律不采信：否则任何客户端自己填 X-Forwarded-For
// 就能换出一个新 key，登录限速等于不存在。
func TestClientIPIgnoresForwardedHeadersByDefault(t *testing.T) {
	s := NewService(nil, nil)
	r := reqFrom("10.0.0.9:5555", map[string]string{
		"X-Forwarded-For": "1.2.3.4",
		"X-Real-Ip":       "5.6.7.8",
	})
	if got := s.clientIP(r); got != "10.0.0.9" {
		t.Fatalf("未配置受信反代时应取 RemoteAddr，得到 %q", got)
	}
}

// 反代终结 TLS 后所有请求的 RemoteAddr 都是反代 IP：必须取真实客户端，
// 否则 refresh 限速退化成全局单键，一次爆破就锁死全站。
func TestClientIPUsesForwardedForBehindTrustedProxy(t *testing.T) {
	s := NewService(nil, nil, "127.0.0.1", "10.0.0.0/8")
	r := reqFrom("127.0.0.1:5555", map[string]string{
		"X-Forwarded-For": "203.0.113.7, 10.1.2.3, 10.0.0.9",
	})
	if got := s.clientIP(r); got != "203.0.113.7" {
		t.Fatalf("应取最右侧不受信跳 203.0.113.7，得到 %q", got)
	}
}

// 客户端可以自己往 X-Forwarded-For 左边追加内容，只有反代追加的右侧才可信；
// 右侧条目若也是受信网段，继续往左找。
func TestClientIPSkipsTrustedHopsInForwardedFor(t *testing.T) {
	s := NewService(nil, nil, "10.0.0.0/8")
	r := reqFrom("10.0.0.9:5555", map[string]string{
		"X-Forwarded-For": "198.51.100.4, 10.9.9.9",
	})
	if got := s.clientIP(r); got != "198.51.100.4" {
		t.Fatalf("应跳过受信跳取 198.51.100.4，得到 %q", got)
	}
}

// 反代只给了 X-Real-Ip（单值）时同样采信；值不是合法 IP 就退回 RemoteAddr，
// 不能让一个畸形头变成「无限速的 key」。
func TestClientIPFallsBackToRemoteAddrOnGarbage(t *testing.T) {
	s := NewService(nil, nil, "127.0.0.1")
	r := reqFrom("127.0.0.1:5555", map[string]string{
		"X-Forwarded-For": "not-an-ip",
		"X-Real-Ip":       "also-bad",
	})
	if got := s.clientIP(r); got != "127.0.0.1" {
		t.Fatalf("畸形转发头应退回 RemoteAddr，得到 %q", got)
	}
	r2 := reqFrom("127.0.0.1:5555", map[string]string{"X-Real-Ip": "203.0.113.9"})
	if got := s.clientIP(r2); got != "203.0.113.9" {
		t.Fatalf("应采信 X-Real-Ip，得到 %q", got)
	}
}

// 单 IP 与 CIDR 两种写法都要能解析；写错的条目忽略而不是 panic。
func TestParseTrustedProxies(t *testing.T) {
	blocks := parseTrustedProxies([]string{"127.0.0.1", "10.0.0.0/8", "", "nonsense", "::1"})
	if len(blocks) != 3 {
		t.Fatalf("应解析出 3 个网段，得到 %d", len(blocks))
	}
	s := NewService(nil, nil, "::1")
	if !s.isTrustedProxy("::1") {
		t.Fatal("IPv6 单地址应视为 /128")
	}
	if s.isTrustedProxy("10.0.0.1") {
		t.Fatal("未列入的地址不应受信")
	}
}
