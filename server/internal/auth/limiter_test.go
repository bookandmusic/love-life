package auth

import (
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
