package uglink

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---- RSA 加密 ----

func TestEncryptWithPublicKey(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	encoded := base64.StdEncoding.EncodeToString(pemBytes)

	enc, err := encryptWithPublicKey("hello-ugreen", encoded)
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		t.Fatalf("结果不是 base64: %v", err)
	}
	plain, err := rsa.DecryptPKCS1v15(rand.Reader, priv, raw)
	if err != nil {
		t.Fatalf("解密失败: %v", err)
	}
	if string(plain) != "hello-ugreen" {
		t.Fatalf("明文不符: %s", plain)
	}
}

func TestEncryptWithBadKey(t *testing.T) {
	if _, err := encryptWithPublicKey("x", "not-a-key"); err == nil {
		t.Fatal("非法公钥应当报错")
	}
}

// ---- 302 失效检测 ----

func TestIsSessionExpired(t *testing.T) {
	s := &Session{LoginOrigin: "https://abc.ug.link"}
	cases := []struct {
		name   string
		status int
		loc    string
		want   bool
	}{
		{"跳回登录页", http.StatusFound, "https://abc.ug.link/desktop/#/login/", true},
		{"临时重定向到登录页", http.StatusTemporaryRedirect, "https://abc.ug.link/desktop/#/login/", true},
		{"跳转到业务页不算失效", http.StatusFound, "https://abc.ug.link/desktop/#/home/", false},
		{"其它域不算", http.StatusFound, "https://evil.example/desktop/#/login/", false},
		{"200 不算失效", http.StatusOK, "", false},
		{"无 Location 不算", http.StatusFound, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := &http.Response{StatusCode: c.status, Header: http.Header{}}
			if c.loc != "" {
				res.Header.Set("Location", c.loc)
			}
			if got := IsSessionExpired(res, s); got != c.want {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestReplayable(t *testing.T) {
	for _, m := range []string{"GET", "HEAD", "OPTIONS", "get"} {
		if !Replayable(m) {
			t.Fatalf("%s 应可重放", m)
		}
	}
	for _, m := range []string{"POST", "PUT", "DELETE", "PATCH"} {
		if Replayable(m) {
			t.Fatalf("%s 不应自动重放", m)
		}
	}
}

// ---- 重登互斥 ----

// TestLoginSerialized 验证并发请求只触发一次登录。
// 通过替换包级 loginFn 注入计数桩，走真实的 Manager.login 路径（含锁）。
func TestLoginSerialized(t *testing.T) {
	var logins int32
	orig := loginFn
	loginFn = func(_ context.Context, _, _, _ string, _ int) (*loginResult, error) {
		atomic.AddInt32(&logins, 1)
		time.Sleep(20 * time.Millisecond) // 制造并发窗口
		return &loginResult{
			cookie:      "ugreen-proxy-token=x",
			origin:      "https://dev.ug.link",
			loginOrigin: "https://dev.ug.link",
		}, nil
	}
	defer func() { loginFn = orig }()

	m := &Manager{creds: func(context.Context) (*Credentials, error) {
		return &Credentials{UglinkID: "dev", Port: 8080, Username: "u", Password: "p"}, nil
	}}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := m.Session(context.Background()); err != nil {
				t.Errorf("Session 失败: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt32(&logins); got != 1 {
		t.Fatalf("并发登录次数 %d，期望恰好 1", got)
	}
}

// TestBackoff 验证连续失败后进入退避期。
func TestBackoff(t *testing.T) {
	m := &Manager{creds: func(context.Context) (*Credentials, error) {
		return &Credentials{UglinkID: "dev", Port: 8080, Username: "u", Password: "p"}, nil
	}}
	for i := 0; i < backoffSteps; i++ {
		m.recordFailure()
	}
	if m.backoffAt.IsZero() || time.Now().After(m.backoffAt) {
		t.Fatal("达到失败上限后应进入退避期")
	}
	_, err := m.login(context.Background())
	if !errors.Is(err, ErrBackoff) {
		t.Fatalf("退避期内应返回 ErrBackoff，实际 %v", err)
	}
}

// TestSessionCache 验证 TTL 内复用、过期后重新登录。
func TestSessionCache(t *testing.T) {
	m := &Manager{}
	m.session = &Session{BaseURL: "https://dev.ug.link"}
	m.loadedAt = time.Now()
	if m.cached() == nil {
		t.Fatal("TTL 内应命中缓存")
	}
	m.loadedAt = time.Now().Add(-2 * time.Hour)
	if m.cached() != nil {
		t.Fatal("超过 TTL 应失效")
	}
}

// ---- discovery 解析 ----

func TestDiscoverRejectsBadID(t *testing.T) {
	if _, err := Discover(context.Background(), "BAD_ID!"); err == nil {
		t.Fatal("非法 ID 应当报错")
	}
}

func TestDiscoverParsesOrigin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":200,"data":{"alias":"mydev","relayDomain":"cn.ug.link"}}`))
	}))
	defer srv.Close()

	old := discoveryEndpoints
	discoveryEndpoints = []string{srv.URL}
	defer func() { discoveryEndpoints = old }()

	origin, err := Discover(context.Background(), "mydev")
	if err != nil {
		t.Fatalf("发现失败: %v", err)
	}
	if origin != "https://mydev.cn.ug.link" {
		t.Fatalf("origin 不符: %s", origin)
	}
}

func TestDiscoverRejectsAliasMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":200,"data":{"alias":"other","relayDomain":"cn.ug.link"}}`))
	}))
	defer srv.Close()

	old := discoveryEndpoints
	discoveryEndpoints = []string{srv.URL}
	defer func() { discoveryEndpoints = old }()

	if _, err := Discover(context.Background(), "mydev"); err == nil {
		t.Fatal("设备标识不一致应当报错")
	}
}

// ---- 重放语义 ----

// TestExpiryReplayContract 断言失效判定与重放策略的组合行为：
// 失效响应 + 幂等方法 → 允许重放；失效响应 + 写方法 → 不重放。
func TestExpiryReplayContract(t *testing.T) {
	s := &Session{LoginOrigin: "https://dev.ug.link"}
	expired := &http.Response{
		StatusCode: http.StatusFound,
		Header:     http.Header{"Location": []string{"https://dev.ug.link/desktop/#/login/"}},
	}
	if !IsSessionExpired(expired, s) {
		t.Fatal("302 跳登录页应判为失效")
	}
	if !Replayable(http.MethodGet) {
		t.Fatal("GET 应允许重放")
	}
	if Replayable(http.MethodPost) {
		t.Fatal("POST 不应自动重放（可能重复提交）")
	}

	// 业务跳转（非登录页）不应误判为失效，否则会触发无谓重登
	business := &http.Response{
		StatusCode: http.StatusFound,
		Header:     http.Header{"Location": []string{"https://dev.ug.link/desktop/#/files/"}},
	}
	if IsSessionExpired(business, s) {
		t.Fatal("跳业务页不应判为失效")
	}
}

// ---- cookie 解析 ----

func TestCookiesFromHTML(t *testing.T) {
	html := `<script>document.cookie = "ugreen-proxy-token=abc123; path=/";</script>`
	got := cookiesFromHTML(html)
	if got["ugreen-proxy-token"] != "abc123" {
		t.Fatalf("未解析到 cookie: %v", got)
	}
}
