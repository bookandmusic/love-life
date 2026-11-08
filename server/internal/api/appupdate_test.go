package api

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"testing"
)

// check 失败原因走可读映射：原始错误含 GitHub/代理地址（可能是内网），不得下发给前端。
func TestAppUpdateCheckReasonMapped(t *testing.T) {
	ts := newTestServer(t)
	adminToken, code := loginToken(t, ts.URL, "admin", "admin-pw", "admin")
	if code != http.StatusOK {
		t.Fatalf("admin login: %d", code)
	}

	// 代理指向本机一个确定没有监听的端口：Go 原始错误会带 127.0.0.1:<port> 与 dial 细节。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	proxyAddr := ln.Addr().String()
	_ = ln.Close()

	res := doJSON(t, "PUT", ts.URL+"/api/v1/admin/appupdate/config", adminToken, map[string]any{
		"proxy_url": "http://" + proxyAddr, "use_proxy": true,
	})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("save proxy config: %d", res.StatusCode)
	}

	res = doJSON(t, "POST", ts.URL+"/api/v1/admin/appupdate/check", adminToken, nil)
	var body struct {
		OK     bool   `json:"ok"`
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(res.Body).Decode(&body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || body.OK {
		t.Fatalf("check 应 200 且 ok=false，实得 %d %+v", res.StatusCode, body)
	}
	if body.Reason != "连接被拒绝（该端口没有服务在监听）" {
		t.Fatalf("reason = %q，want 可读映射后的原因", body.Reason)
	}
	for _, leak := range []string{proxyAddr, "127.0.0.1", "dial tcp", "proxyconnect"} {
		if strings.Contains(body.Reason, leak) {
			t.Fatalf("reason 泄漏原始错误（%q）：%s", leak, body.Reason)
		}
	}
}
