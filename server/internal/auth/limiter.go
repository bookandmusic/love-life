package auth

import (
	"sync"
	"time"
)

const (
	// loginMaxFails 触发锁定的连续失败次数。
	loginMaxFails = 5
	// loginLockWindow 锁定时长（安全基线：15 分钟）。
	loginLockWindow = 15 * time.Minute
)

// LoginLimiter 登录限速（安全基线三件套之一）：同一 用户名+IP 连续失败 loginMaxFails 次即锁定 loginLockWindow。
// 单进程内存态即可——重启清零对本场景可接受。
type LoginLimiter struct {
	mu          sync.Mutex
	fails       map[string]int
	lockedUntil map[string]time.Time
}

func NewLoginLimiter() *LoginLimiter {
	return &LoginLimiter{fails: map[string]int{}, lockedUntil: map[string]time.Time{}}
}

func (l *LoginLimiter) Allowed(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if until, ok := l.lockedUntil[key]; ok {
		if time.Now().Before(until) {
			return false
		}
		delete(l.lockedUntil, key)
		delete(l.fails, key)
	}
	return true
}

func (l *LoginLimiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fails[key]++
	if l.fails[key] >= loginMaxFails {
		l.lockedUntil[key] = time.Now().Add(loginLockWindow)
		delete(l.fails, key)
	}
}

func (l *LoginLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, key)
	delete(l.lockedUntil, key)
}
