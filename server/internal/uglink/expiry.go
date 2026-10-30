package uglink

import (
	"net/http"
	"net/url"
	"strings"
)

// IsSessionExpired 判定转发响应是否表示会话失效。
//
// 绿联在会话过期时把请求 302 到登录页（/desktop/#/login/）。
// 只靠 TTL 不够：绿联侧可能提前失效（服务端重启、账号在别处登录），
// 那时 TTL 还没到，只能靠响应特征识别。
func IsSessionExpired(res *http.Response, s *Session) bool {
	if res == nil || s == nil {
		return false
	}
	switch res.StatusCode {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
	default:
		return false
	}
	loc := res.Header.Get("Location")
	if loc == "" {
		return false
	}
	target, err := url.Parse(loc)
	if err != nil {
		return false
	}
	loginURL, err := url.Parse(s.LoginOrigin)
	if err != nil {
		return false
	}
	// 必须回到同一设备域，且路径落在登录页
	if target.Host != loginURL.Host {
		return false
	}
	return strings.HasPrefix(target.Path, "/desktop/") &&
		strings.Contains(target.Fragment, "/login")
}

// Replayable 判断请求能否安全重放。
// 只有幂等方法可自动重放；写操作重放可能产生重复数据。
func Replayable(method string) bool {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}
