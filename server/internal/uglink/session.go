package uglink

import (
	"context"
	"errors"
	"sync"
	"time"
)

const (
	// sessionTTL 会话缓存上限。绿联真实 cookie 有效期不固定，TTL 只是缓存时长；
	// 真正的失效判定靠响应级检测（见 IsSessionExpired）。
	sessionTTL = time.Hour
	// failureBackoff 登录失败后的退避时长（参考实现三级：10 分钟 / 30 分钟 / 1 小时）。
	// 这里简化为按连续失败次数递进，避免撞绿联风控把自己锁死。
	backoffSteps = 3
)

// Credentials 绿联登录凭据（存服务端，加密落库）。
type Credentials struct {
	UglinkID string
	Port     int
	Username string
	Password string
}

// Session 一次成功握手的会话。
type Session struct {
	Cookie      string    // 转发请求要带的 Cookie 头
	BaseURL     string    // 中转入口地址（https://<id>.<domain>）
	LoginOrigin string    // 登录源，用于判定 302 是否跳回登录页
	ExpiresAt   time.Time // 建议的续期时间点
}

// ErrBackoff 处于失败退避期，不应再尝试登录。
var ErrBackoff = errors.New("绿联登录处于退避期，稍后再试")

// loginFn 实际执行握手的函数。抽成变量便于测试注入（生产用 Login）。
var loginFn = Login

// Manager 维护单条绿联通道的会话与登录节流。
//
// 并发保护：同一时刻只允许一个登录流程（loginMu）；
// 其它请求等待结果而不是各自重登——并发重登会触发绿联风控。
type Manager struct {
	creds func(context.Context) (*Credentials, error) // 取凭据（可能来自加密存储）

	loginMu   sync.Mutex
	session   *Session
	loadedAt  time.Time
	failures  int
	backoffAt time.Time
}

// NewManager 构造管理器；creds 回调负责从存储读取并解密凭据。
func NewManager(creds func(context.Context) (*Credentials, error)) *Manager {
	return &Manager{creds: creds}
}

// Session 返回可用会话；缓存有效则直接复用，否则登录。
func (m *Manager) Session(ctx context.Context) (*Session, error) {
	if s := m.cached(); s != nil {
		return s, nil
	}
	return m.login(ctx)
}

// Refresh 强制重新登录（响应级失效检测触发时调用）。
func (m *Manager) Refresh(ctx context.Context) (*Session, error) {
	m.invalidate()
	return m.login(ctx)
}

// Invalidate 丢弃当前会话（转发失败时调用）。
func (m *Manager) Invalidate() { m.invalidate() }

// Status 返回会话状态供后台展示（不触发登录）。
func (m *Manager) Status() (ok bool, expiresAt time.Time, failures int, backoffUntil time.Time) {
	m.loginMu.Lock()
	defer m.loginMu.Unlock()
	if m.session == nil || time.Since(m.loadedAt) >= sessionTTL {
		return false, time.Time{}, m.failures, m.backoffAt
	}
	return true, m.session.ExpiresAt, m.failures, m.backoffAt
}

func (m *Manager) cached() *Session {
	m.loginMu.Lock()
	defer m.loginMu.Unlock()
	if m.session == nil {
		return nil
	}
	if time.Since(m.loadedAt) >= sessionTTL {
		m.session = nil
		return nil
	}
	return m.session
}

func (m *Manager) invalidate() {
	m.loginMu.Lock()
	defer m.loginMu.Unlock()
	m.session = nil
}

// login 执行一次握手。持锁期间其它调用者等待，避免并发重登。
func (m *Manager) login(ctx context.Context) (*Session, error) {
	m.loginMu.Lock()
	defer m.loginMu.Unlock()

	// 等锁期间可能已有别的请求登录成功，直接复用
	if m.session != nil && time.Since(m.loadedAt) < sessionTTL {
		return m.session, nil
	}
	// 退避期内不重试
	if !m.backoffAt.IsZero() && time.Now().Before(m.backoffAt) {
		return nil, ErrBackoff
	}

	creds, err := m.creds(ctx)
	if err != nil {
		return nil, err
	}
	if creds == nil || creds.UglinkID == "" || creds.Username == "" {
		return nil, errors.New("绿联通道未配置")
	}

	port := creds.Port
	if port <= 0 {
		port = 8080
	}
	res, err := loginFn(ctx, creds.UglinkID, creds.Username, creds.Password, port)
	if err != nil {
		m.recordFailure()
		return nil, err
	}

	now := time.Now()
	m.session = &Session{
		Cookie:      res.cookie,
		BaseURL:     res.origin,
		LoginOrigin: res.loginOrigin,
		// 提前 60 秒到期，给 APP 留出续期窗口
		ExpiresAt: now.Add(sessionTTL - time.Minute),
	}
	m.loadedAt = now
	m.failures = 0
	m.backoffAt = time.Time{}
	return m.session, nil
}

// recordFailure 记录失败并按次数递增退避（10 分钟 / 30 分钟 / 1 小时）。
func (m *Manager) recordFailure() {
	m.failures++
	if m.failures >= backoffSteps {
		m.backoffAt = time.Now().Add(time.Hour)
		return
	}
	backoff := time.Duration(10*m.failures) * time.Minute
	m.backoffAt = time.Now().Add(backoff)
}
