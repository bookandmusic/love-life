package auth

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// 登录限速（安全基线三件套之一）：同一「用户名 + IP」连续失败 5 次即锁定 15 分钟，
// 登录成功即复位。实现见 limiter.go；接入点 auth/service.go 的 Login。
func TestLoginLimiterLockAndReset(t *testing.T) {
	l := NewLoginLimiter()
	const key = "girl|10.0.0.1"

	// 前 4 次失败仍放行（阈值是「达到 5 次才锁」）
	for i := 1; i <= loginMaxFails-1; i++ {
		if !l.Allowed(key) {
			t.Fatalf("第 %d 次失败后不应锁定（阈值 %d）", i, loginMaxFails)
		}
		l.Fail(key)
	}
	if !l.Allowed(key) {
		t.Fatal("第 4 次失败后仍应放行")
	}

	// 第 5 次失败 → 锁定
	l.Fail(key)
	if l.Allowed(key) {
		t.Fatalf("连续失败 %d 次后应锁定", loginMaxFails)
	}
}

// 锁定期内即便密码正确也不放行；不同 key 互不影响（按用户名+IP 隔离）。
func TestLoginLimiterIsolatesKeys(t *testing.T) {
	l := NewLoginLimiter()
	locked := "girl|10.0.0.1"
	other := "girl|10.0.0.2"    // 同用户名不同 IP
	otherUser := "boy|10.0.0.1" // 同 IP 不同用户名

	for i := 0; i < loginMaxFails; i++ {
		l.Fail(locked)
	}
	if l.Allowed(locked) {
		t.Fatal("该 key 应已锁定")
	}
	if !l.Allowed(other) {
		t.Fatal("同用户名不同 IP 不应被连带锁定")
	}
	if !l.Allowed(otherUser) {
		t.Fatal("同 IP 不同用户名不应被连带锁定")
	}
}

// 成功登录复位计数：失败 4 次后成功一次，再失败 4 次不应触发锁定。
func TestLoginLimiterResetClearsFails(t *testing.T) {
	l := NewLoginLimiter()
	const key = "girl|10.0.0.1"
	for i := 0; i < loginMaxFails-1; i++ {
		l.Fail(key)
	}
	l.Reset(key) // 登录成功
	for i := 0; i < loginMaxFails-1; i++ {
		if !l.Allowed(key) {
			t.Fatalf("复位后第 %d 次失败不应锁定", i+1)
		}
		l.Fail(key)
	}
	// 复位后累计未达阈值，仍应放行
	if !l.Allowed(key) {
		t.Fatal("复位后累计失败数未达阈值，不应锁定")
	}
}

// 锁定到期自动解锁（把窗口改小以避免测试等待 15 分钟——直接验证 Allowed 的到期分支）。
func TestLoginLimiterUnlocksAfterWindow(t *testing.T) {
	l := NewLoginLimiter()
	const key = "girl|10.0.0.1"
	for i := 0; i < loginMaxFails; i++ {
		l.Fail(key)
	}
	if l.Allowed(key) {
		t.Fatal("应已锁定")
	}
	// 直接把锁定截止时间改到过去，模拟窗口到期（不真的等 15 分钟）
	l.mu.Lock()
	l.lockedUntil[key] = time.Now().Add(-time.Second)
	l.mu.Unlock()
	if !l.Allowed(key) {
		t.Fatal("锁定期满应自动解锁")
	}
	// 解锁同时清空计数：再失败 1 次不应立刻又锁
	l.Fail(key)
	if !l.Allowed(key) {
		t.Fatal("解锁后计数应已清零，单次失败不应重新锁定")
	}
}

// P2-20：令牌签发的限速 key 与登录是各自独立的命名空间。
// 不变量：refreshLimitKey 的产物**永不等于**任何 loginLimitKey 的产物——
// 共用命名空间会让「设备休眠后正常刷新失败累计」把该用户的密码登录一并锁死（可用性故障）。
// 这条不变量由 key 构造保证，故直接断言构造结果（而不是重跑一遍锁逻辑）。
func TestRefreshLimitKeyIsSeparateNamespace(t *testing.T) {
	// 取值用例：两侧 key 在现实取值下不得相等。
	// 注意「任意输入下都不同」并不成立（用户名与 IP 都可能含 "|"），此处只锁现实取值；
	// 若将来放宽 username / 地址的格式校验，需重新评估（见 service.go 中 refreshLimitKey 注释）。
	ips := []string{"10.0.0.1", "127.0.0.1", "192.168.1.23", ""}
	users := []string{"girl", "boy", "admin", ""}
	for _, ip := range ips {
		rk := refreshLimitKey(ip)
		for _, user := range users {
			lk := loginLimitKey(user, ip)
			if rk == lk {
				t.Fatalf("令牌签发 key %q 与登录 key %q 相撞（两把锁会互相污染）", rk, lk)
			}
			// 登录 key 恒含分隔符（用户名 + IP 两段）
			if !strings.Contains(lk, "|") {
				t.Fatalf("登录 key %q 形态异常：应含分隔符", lk)
			}
		}
	}
	// 计划明确要求「令牌签发侧加独立前缀」作为防污染措施（两把锁各自命名空间）：
	// 直接钉住这条契约——去掉前缀即失败。它同时是未来的护栏：若有人把用户名拼进
	// 刷新 key（回到「用户名 + IP」形态），前缀能保证仍不与登录 key 相等。
	if rk := refreshLimitKey("10.0.0.1"); !strings.HasPrefix(rk, "refresh:") {
		t.Fatalf("令牌签发 key %q 应带独立前缀 refresh:（两把锁不得共用命名空间）", rk)
	}

	// 同一 IP 的刷新计数与登录计数确实互不影响
	l := NewLoginLimiter()
	const ip = "10.0.0.1"
	rk, lk := refreshLimitKey(ip), loginLimitKey("girl", ip)
	for i := 0; i < loginMaxFails; i++ {
		l.Fail(rk)
	}
	if l.Allowed(rk) {
		t.Fatal("令牌签发侧应已锁定")
	}
	if !l.Allowed(lk) {
		t.Fatal("令牌签发被锁不得连带锁定密码登录")
	}
}

// 内存上界：大量互不相同的 key 不得让失败表 / 锁定表无界增长。
// 上界不是正确性约束——被淘汰的 key 等价于从未失败，重新计数即可。
func TestLoginLimiterBoundsKeys(t *testing.T) {
	l := NewLoginLimiter()

	// 只失败一次：压失败表。
	for i := 0; i < loginLimiterMaxKeys*2; i++ {
		l.Fail(fmt.Sprintf("solo-%d|10.0.0.1", i))
	}
	l.mu.Lock()
	fails, locked := len(l.fails), len(l.lockedUntil)
	l.mu.Unlock()
	if fails > loginLimiterMaxKeys {
		t.Fatalf("失败表 %d 条，应 ≤ 上界 %d", fails, loginLimiterMaxKeys)
	}
	if locked != 0 {
		t.Fatalf("单次失败不应产生锁定，实得 %d 条", locked)
	}

	// 每个 key 失败满阈值：压锁定表。
	for i := 0; i < loginLimiterMaxKeys*2; i++ {
		key := fmt.Sprintf("locked-%d|10.0.0.2", i)
		for n := 0; n < loginMaxFails; n++ {
			l.Fail(key)
		}
	}
	l.mu.Lock()
	fails, locked = len(l.fails), len(l.lockedUntil)
	l.mu.Unlock()
	if locked > loginLimiterMaxKeys {
		t.Fatalf("锁定表 %d 条，应 ≤ 上界 %d", locked, loginLimiterMaxKeys)
	}
	if fails > loginLimiterMaxKeys {
		t.Fatalf("失败表 %d 条，应 ≤ 上界 %d", fails, loginLimiterMaxKeys)
	}
}

// 失败计数超窗后重新计数：很久以前的失败不与当前失败累加。
func TestLoginLimiterFailWindowResetsCount(t *testing.T) {
	l := NewLoginLimiter()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }
	const key = "girl|10.0.0.1"

	for i := 0; i < loginMaxFails-1; i++ {
		l.Fail(key)
	}
	now = now.Add(loginFailWindow + time.Second)
	l.Fail(key) // 超窗：重新从 1 计数
	if !l.Allowed(key) {
		t.Fatalf("超窗后重新计数，未达阈值 %d 次不应锁定", loginMaxFails)
	}
	if rec := l.fails[key]; rec == nil || rec.count != 1 {
		t.Fatalf("超窗后失败计数应重置为 1，实得 %+v", rec)
	}

	// 锁定同样按注入时钟到期：到期即放行并清计数。
	for i := 1; i < loginMaxFails; i++ {
		l.Fail(key)
	}
	if l.Allowed(key) {
		t.Fatal("窗口内累计失败达到阈值应锁定")
	}
	now = now.Add(loginLockWindow + time.Second)
	if !l.Allowed(key) {
		t.Fatal("锁定期满应自动解锁")
	}
}

// 并发安全：登录限速被同一进程的多个请求并发调用。
func TestLoginLimiterConcurrentAccess(t *testing.T) {
	l := NewLoginLimiter()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				key := fmt.Sprintf("u%d|10.0.%d.%d", i%50, g, i%7)
				l.Allowed(key)
				if i%7 == 0 {
					l.Reset(key)
				}
				l.Fail(key)
			}
		}(g)
	}
	wg.Wait()
}
