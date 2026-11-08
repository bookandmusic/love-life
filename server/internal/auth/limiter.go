package auth

import (
	"sort"
	"sync"
	"time"
)

const (
	// loginMaxFails 触发锁定的连续失败次数。
	loginMaxFails = 5
	// loginLockWindow 锁定时长（安全基线：15 分钟）。
	loginLockWindow = 15 * time.Minute
	// loginFailWindow 失败计数的有效窗口：距最后一次失败超过该时长即清空重计，
	// 避免很久以前的零星失败与当前失败累加成锁定。
	loginFailWindow = 15 * time.Minute
	// loginLimiterMaxKeys 两张状态表各自的 key 上界（内存保护）。
	// 正常用量是个位数；上万条不同「用户名|IP」只可能来自恶意刷接口。
	loginLimiterMaxKeys = 4096
)

// failRecord 单个 key 的失败计数：count 次失败，最后一次发生在 seen。
type failRecord struct {
	count int
	seen  time.Time
}

// LoginLimiter 登录限速（安全基线三件套之一）：同一 用户名+IP 连续失败 loginMaxFails 次即锁定 loginLockWindow。
// 单进程内存态即可——重启清零对本场景可接受。
//
// 两张表都有 key 上界：命中上界时先扫过期，仍有压力就按最旧淘汰——内存保护，
// 不影响正确性（被淘汰的 key 等价于从未失败，重新开始计数即可）。
type LoginLimiter struct {
	mu          sync.Mutex
	now         func() time.Time
	fails       map[string]*failRecord
	lockedUntil map[string]time.Time
}

func NewLoginLimiter() *LoginLimiter {
	return &LoginLimiter{
		now:         time.Now,
		fails:       make(map[string]*failRecord),
		lockedUntil: make(map[string]time.Time),
	}
}

func (l *LoginLimiter) Allowed(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if until, ok := l.lockedUntil[key]; ok {
		if l.now().Before(until) {
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
	now := l.now()
	rec, ok := l.fails[key]
	// 首次失败或超出计数窗口：重新计数（窗口以最后一次失败为起点）。
	if !ok || now.Sub(rec.seen) > loginFailWindow {
		if len(l.fails) >= loginLimiterMaxKeys {
			l.trimLocked(now)
		}
		rec = &failRecord{}
		l.fails[key] = rec
	}
	rec.count++
	rec.seen = now
	if rec.count < loginMaxFails {
		return
	}
	if _, locked := l.lockedUntil[key]; !locked && len(l.lockedUntil) >= loginLimiterMaxKeys {
		l.trimLocked(now)
	}
	l.lockedUntil[key] = now.Add(loginLockWindow)
	delete(l.fails, key)
}

func (l *LoginLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, key)
	delete(l.lockedUntil, key)
}

// trimLocked 释放内存：先清过期条目，仍超上界则按「最旧先出」淘汰到上界的 3/4。
// 调用方必须持有 l.mu，且只在命中上界时调用（正常路径零开销）。
func (l *LoginLimiter) trimLocked(now time.Time) {
	for k, until := range l.lockedUntil {
		if !now.Before(until) {
			delete(l.lockedUntil, k)
			delete(l.fails, k)
		}
	}
	for k, rec := range l.fails {
		if now.Sub(rec.seen) > loginFailWindow {
			delete(l.fails, k)
		}
	}
	lowWater := loginLimiterMaxKeys * 3 / 4
	if len(l.fails) >= loginLimiterMaxKeys {
		keys := make([]string, 0, len(l.fails))
		for k := range l.fails {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			left, right := l.fails[keys[i]].seen, l.fails[keys[j]].seen
			if left.Equal(right) {
				return keys[i] < keys[j]
			}
			return left.Before(right)
		})
		for _, k := range keys[:len(keys)-lowWater] {
			delete(l.fails, k)
		}
	}
	if len(l.lockedUntil) >= loginLimiterMaxKeys {
		keys := make([]string, 0, len(l.lockedUntil))
		for k := range l.lockedUntil {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			left, right := l.lockedUntil[keys[i]], l.lockedUntil[keys[j]]
			if left.Equal(right) {
				return keys[i] < keys[j]
			}
			return left.Before(right)
		})
		for _, k := range keys[:len(keys)-lowWater] {
			delete(l.lockedUntil, k)
		}
	}
}
