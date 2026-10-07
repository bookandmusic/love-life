package auth

import (
	"strings"
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
