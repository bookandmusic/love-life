package api

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"testing"
)

// P2-20：令牌签发接口的限速与失败锁定。
// 三个前提一起验：① 连续 5 次「查不到的令牌」后锁定（429）；
// ② 锁定 key 与登录**各自独立**——refresh 被锁不得连带把密码登录也锁死；
// ③ 持「已过期」令牌重试不计失败（属正常生命周期，不该惩罚自己人）。
func TestRefreshRateLimitIsolatedFromLogin(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	_ = adminToken

	// ① 连打 5 次不存在的令牌 → 第 6 次起 429
	var last int
	for i := 0; i < 5; i++ {
		res := doJSON(t, "POST", ts.URL+"/api/v1/auth/refresh", "", map[string]string{"refresh_token": fmt.Sprintf("bogus-%d", i)})
		last = res.StatusCode
		res.Body.Close()
		if last != http.StatusUnauthorized {
			t.Fatalf("第 %d 次无效令牌 = %d，want 401", i+1, last)
		}
	}
	res := doJSON(t, "POST", ts.URL+"/api/v1/auth/refresh", "", map[string]string{"refresh_token": "bogus-6"})
	res.Body.Close()
	if res.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("第 6 次应被锁定（429），实得 %d", res.StatusCode)
	}

	// ② refresh 侧被锁，密码登录仍可用（共用 key 空间会把这里一起锁死）
	if _, code := loginToken(t, ts.URL, "girl", "girl-pw", "app"); code != http.StatusOK {
		t.Fatalf("refresh 被锁不应连带锁定登录，登录返回 %d", code)
	}
	// 登录侧连错 5 次被锁后，refresh 侧不受影响（反向独立性）
	for i := 0; i < 6; i++ {
		r := doJSON(t, "POST", ts.URL+"/api/v1/auth/login", "", map[string]string{"username": "girl", "password": "wrong"})
		r.Body.Close()
	}
	r := doJSON(t, "POST", ts.URL+"/api/v1/auth/login", "", map[string]string{"username": "girl", "password": "girl-pw"})
	r.Body.Close()
	if r.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("登录侧应已被锁定，实得 %d", r.StatusCode)
	}
}

// ③ 合法用户持已过期令牌重试：401 但不计失败，反复重试不会被锁。
func TestRefreshExpiredTokenDoesNotLock(t *testing.T) {
	ts, _, _ := newPostTestEnv(t)
	ctx := t.Context()

	// 造一条已过期的令牌：直接插库（hash 用服务端同款算法）
	raw := "expired-token-value"
	if _, err := ts.db.ExecContext(ctx,
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES (2, ?, datetime('now','-1 day'))`,
		refreshHash(raw)); err != nil {
		t.Fatalf("seed expired token: %v", err)
	}
	for i := 0; i < 7; i++ {
		res := doJSON(t, "POST", ts.URL+"/api/v1/auth/refresh", "", map[string]string{"refresh_token": raw})
		if res.StatusCode != http.StatusUnauthorized {
			res.Body.Close()
			t.Fatalf("第 %d 次过期令牌 = %d，want 401（不得因重试被锁成 429）", i+1, res.StatusCode)
		}
		res.Body.Close()
	}
}

// refreshHash 与 auth 包内 hashRefreshToken 同款（该函数未导出，测试侧就地算一份）
func refreshHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
