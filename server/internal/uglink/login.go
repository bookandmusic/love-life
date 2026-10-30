// Package uglink 实现绿联 UGREENlink 通道的服务端客户端：
// 服务端持凭据完成五步握手，拿到 ugreen-proxy-token 后下发给 APP。
//
// 协议细节对照参考实现 Leonis-Q-F/uglink-worker-nas 的 session-service.ts。
// 绿联侧为私有接口，字段与路径随时可能变更；本包把协议收敛在一处，
// 变更时只改这里，不影响上层（通道接口、后台配置）。
package uglink

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// discoveryTimeout 单次发现请求超时（参考实现 5s）
	discoveryTimeout = 5 * time.Second
	// authTimeout 握手各步超时（参考实现 15s）
	authTimeout = 15 * time.Second
	// redirectTimeout 换 cookie 的跳转链超时（参考实现 30s）
	redirectTimeout = 30 * time.Second
	// maxAuthResponse 认证响应体上限，防止异常响应撑爆内存
	maxAuthResponse = 64 * 1024
	// maxHandoffRedirects 激活跳转最多跟随次数
	maxHandoffRedirects = 4

	// proxyCookieName 最终要拿到的会话 cookie 名
	proxyCookieName = "ugreen-proxy-token"
)

// discoveryEndpoints 四个区域的发现接口；轮询取第一个成功的。
var discoveryEndpoints = []string{
	"https://api-zh.ugnas.com/api/p2p/v2/ta/nodeInfo/byAlias",
	"https://api-us.ugnas.com/api/p2p/v2/ta/nodeInfo/byAlias",
	"https://api-eur.ugnas.com/api/p2p/v2/ta/nodeInfo/byAlias",
	"https://api-aar.ugnas.com/api/p2p/v2/ta/nodeInfo/byAlias",
}

// Discover 用 UGREENlink ID 换取设备的真实入口地址（https://<id>.<relayDomain>）。
func Discover(ctx context.Context, uglinkID string) (string, error) {
	if !validUglinkID(uglinkID) {
		return "", errors.New("UGREENlink ID 格式不合法")
	}
	var lastErr error
	for _, endpoint := range discoveryEndpoints {
		origin, err := discoverAt(ctx, endpoint, uglinkID)
		if err == nil {
			return origin, nil
		}
		lastErr = err
	}
	return "", fmt.Errorf("绿联发现失败：%w", lastErr)
}

func discoverAt(ctx context.Context, endpoint, uglinkID string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()

	body, _ := json.Marshal(map[string]string{"alias": uglinkID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://www.ug.link")
	req.Header.Set("Referer", "https://www.ug.link/")
	req.Header.Set("lang", "zh-CN")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("发现接口返回 %d", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxAuthResponse))
	if err != nil {
		return "", err
	}
	var payload struct {
		Code int `json:"code"`
		Data struct {
			Alias       string `json:"alias"`
			RelayDomain string `json:"relayDomain"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", fmt.Errorf("发现响应不是合法 JSON：%w", err)
	}
	if payload.Code != 200 {
		return "", fmt.Errorf("发现接口返回 code %d", payload.Code)
	}
	// 设备标识必须与请求一致，防止拿到别台设备的中转域
	if !strings.EqualFold(payload.Data.Alias, uglinkID) {
		return "", errors.New("发现结果与请求的设备不一致")
	}
	domain := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(payload.Data.RelayDomain)), ".")
	if !strings.HasSuffix(domain, ".ug.link") {
		return "", fmt.Errorf("发现结果的中转域不合法：%s", domain)
	}
	return fmt.Sprintf("https://%s.%s", uglinkID, domain), nil
}

// loginResult 一次完整握手的产物。
type loginResult struct {
	cookie      string // ugreen-proxy-token 及同域其它 cookie 的 Cookie 头
	origin      string // 转发请求的目标源（中转地址）
	loginOrigin string // 登录发生时的源（用于判定 302 是否跳回登录页）
}

// Login 完成五步握手，返回可用的会话 cookie。
func Login(ctx context.Context, uglinkID, username, password string, port int) (*loginResult, error) {
	loginOrigin, err := Discover(ctx, uglinkID)
	if err != nil {
		return nil, err
	}
	return loginAt(ctx, loginOrigin, username, password, port)
}

func loginAt(ctx context.Context, loginOrigin, username, password string, port int) (*loginResult, error) {
	// 第 2 步：verify/check 取 RSA 公钥（响应头 x-rsa-token，Base64 的 PEM）
	checkCtx, cancel := context.WithTimeout(ctx, authTimeout)
	defer cancel()
	checkBody, _ := json.Marshal(map[string]string{"username": username})
	checkReq, err := http.NewRequestWithContext(checkCtx, http.MethodPost, loginOrigin+"/ugreen/v1/verify/check", strings.NewReader(string(checkBody)))
	if err != nil {
		return nil, err
	}
	checkReq.Header.Set("Accept", "*/*")
	checkReq.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	setBrowserHeaders(checkReq, loginOrigin)
	checkRes, err := http.DefaultClient.Do(checkReq)
	if err != nil {
		return nil, fmt.Errorf("无法连接绿联登录服务：%w", err)
	}
	defer checkRes.Body.Close()
	if checkRes.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("获取加密公钥失败（HTTP %d）", checkRes.StatusCode)
	}
	rsaToken := checkRes.Header.Get("x-rsa-token")
	if rsaToken == "" {
		return nil, errors.New("绿联未返回加密公钥")
	}

	// 第 3 步：verify/login（密码 RSA PKCS#1 v1.5 加密）
	encPassword, err := encryptWithPublicKey(password, rsaToken)
	if err != nil {
		return nil, fmt.Errorf("密码加密失败：%w", err)
	}
	loginCtx, cancelLogin := context.WithTimeout(ctx, authTimeout)
	defer cancelLogin()
	loginPayload := map[string]any{
		"username":  username,
		"password":  encPassword,
		"keepalive": false,
		"otp":       true,
		"is_simple": true,
	}
	loginBody, _ := json.Marshal(loginPayload)
	loginReq, err := http.NewRequestWithContext(loginCtx, http.MethodPost, loginOrigin+"/ugreen/v1/verify/login", strings.NewReader(string(loginBody)))
	if err != nil {
		return nil, err
	}
	loginReq.Header.Set("Content-Type", "application/json")
	loginReq.Header.Set("UG-Client-Id", clientID(username))
	setBrowserHeaders(loginReq, loginOrigin)
	loginRes, err := http.DefaultClient.Do(loginReq)
	if err != nil {
		return nil, fmt.Errorf("登录请求失败：%w", err)
	}
	defer loginRes.Body.Close()
	if loginRes.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("登录被拒绝（HTTP %d）", loginRes.StatusCode)
	}
	loginCookies := loginRes.Cookies()
	rawLogin, err := io.ReadAll(io.LimitReader(loginRes.Body, maxAuthResponse))
	if err != nil {
		return nil, err
	}
	var loginData struct {
		Data struct {
			Token     string `json:"token"`
			PublicKey string `json:"publicKey"`
			AuthType  string `json:"authType"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rawLogin, &loginData); err != nil {
		return nil, fmt.Errorf("登录响应不是合法 JSON：%w", err)
	}
	if loginData.Data.Token == "" {
		return nil, errors.New("登录成功但未返回 token（可能账号密码错误）")
	}

	// 第 4 步：dockerToken 换 redirect_url
	tokenCtx, cancelToken := context.WithTimeout(ctx, authTimeout)
	defer cancelToken()
	dockerURL := fmt.Sprintf("%s/ugreen/v1/gateway/proxy/dockerToken?port=%d", loginOrigin, port)
	if loginData.Data.AuthType == "url" {
		dockerURL += "&token=" + url.QueryEscape(loginData.Data.Token)
	}
	dockerReq, err := http.NewRequestWithContext(tokenCtx, http.MethodGet, dockerURL, nil)
	if err != nil {
		return nil, err
	}
	setBrowserHeaders(dockerReq, loginOrigin)
	dockerReq.Header.Set("X-Ugreen-Security-Key", md5Hex(loginData.Data.Token))
	if loginData.Data.AuthType != "url" {
		encToken, encErr := encryptWithPublicKey(loginData.Data.Token, loginData.Data.PublicKey)
		if encErr != nil {
			return nil, fmt.Errorf("token 加密失败：%w", encErr)
		}
		dockerReq.Header.Set("X-Ugreen-Token", encToken)
	}
	setCookieHeader(dockerReq, loginCookies)
	dockerRes, err := http.DefaultClient.Do(dockerReq)
	if err != nil {
		return nil, fmt.Errorf("换取转发凭据失败：%w", err)
	}
	defer dockerRes.Body.Close()
	if dockerRes.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("换取转发凭据失败（HTTP %d）", dockerRes.StatusCode)
	}
	rawDocker, err := io.ReadAll(io.LimitReader(dockerRes.Body, maxAuthResponse))
	if err != nil {
		return nil, err
	}
	var dockerData struct {
		Data struct {
			RedirectURL string `json:"redirect_url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rawDocker, &dockerData); err != nil {
		return nil, fmt.Errorf("转发凭据响应不是合法 JSON：%w", err)
	}
	if dockerData.Data.RedirectURL == "" {
		return nil, errors.New("未返回转发入口地址")
	}

	// 第 5 步：访问 redirect_url 换 ugreen-proxy-token
	cookie, origin, err := handoff(ctx, loginOrigin, dockerData.Data.RedirectURL, loginCookies)
	if err != nil {
		return nil, err
	}
	return &loginResult{cookie: cookie, origin: origin, loginOrigin: loginOrigin}, nil
}

// handoff 跟随跳转链，收集 cookie 直到拿到 ugreen-proxy-token。
func handoff(ctx context.Context, loginOrigin, redirectURL string, loginCookies []*http.Cookie) (cookie, origin string, err error) {
	ctx, cancel := context.WithTimeout(ctx, redirectTimeout)
	defer cancel()

	target, err := url.Parse(redirectURL)
	if err != nil {
		return "", "", fmt.Errorf("转发入口地址不合法：%w", err)
	}
	origin = target.Scheme + "://" + target.Host

	cookies := map[string]string{}
	for _, c := range loginCookies {
		cookies[c.Name] = c.Value
	}

	current := redirectURL
	for hop := 0; hop <= maxHandoffRedirects; hop++ {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, current, nil)
		if reqErr != nil {
			return "", "", reqErr
		}
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		req.Header.Set("Referer", loginOrigin+"/desktop/")
		req.Header.Set("Cookie", cookieHeader(cookies))

		res, resErr := noRedirectClient.Do(req)
		if resErr != nil {
			return "", "", fmt.Errorf("激活转发会话失败：%w", resErr)
		}
		for _, c := range res.Cookies() {
			cookies[c.Name] = c.Value
		}
		// 页面内 document.cookie 也可能下发凭据，扫一遍 HTML
		body, _ := io.ReadAll(io.LimitReader(res.Body, maxAuthResponse))
		res.Body.Close()
		for k, v := range cookiesFromHTML(string(body)) {
			cookies[k] = v
		}

		if _, ok := cookies[proxyCookieName]; ok {
			return cookieHeader(cookies), origin, nil
		}
		loc := res.Header.Get("Location")
		if loc == "" || res.StatusCode < 300 || res.StatusCode >= 400 {
			break
		}
		next, parseErr := url.Parse(loc)
		if parseErr != nil {
			break
		}
		resolved := req.URL.ResolveReference(next)
		if resolved.Scheme != "https" || resolved.Host != target.Host {
			return "", "", errors.New("激活跳转试图离开中转域，已中止")
		}
		current = resolved.String()
	}
	return "", "", errors.New("未拿到转发会话凭据")
}

// noRedirectClient 手动处理跳转，避免跟随到登录页而丢失 cookie。
var noRedirectClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}
