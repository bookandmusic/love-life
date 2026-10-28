package store

import (
	"testing"
	"time"
)

// A1：改密后按 user 吊销全部 refresh token，且不误伤他人。
func TestDeleteRefreshTokensByUser(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	seedPostAuthor(t, db) // user 1
	if _, err := db.Exec(`INSERT INTO users (username, password_hash, role) VALUES ('b', 'x', 'boy')`); err != nil {
		t.Fatalf("seed user 2: %v", err)
	}
	ctx := t.Context()
	exp := time.Now().Add(24 * time.Hour)
	// user 1 两个设备、user 2 一个设备
	for _, h := range []string{"hash-1a", "hash-1b"} {
		if err := db.SaveRefreshToken(ctx, 1, h, exp); err != nil {
			t.Fatalf("save %s: %v", h, err)
		}
	}
	if err := db.SaveRefreshToken(ctx, 2, "hash-2a", exp); err != nil {
		t.Fatalf("save user2: %v", err)
	}

	n, err := db.DeleteRefreshTokensByUser(ctx, 1)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if n != 2 {
		t.Fatalf("应删除 user 1 的 2 行，实际 %d", n)
	}
	if _, err := db.GetRefreshToken(ctx, "hash-1a"); err != ErrNotFound {
		t.Fatalf("user 1 的 token 应已失效，err=%v", err)
	}
	if _, err := db.GetRefreshToken(ctx, "hash-2a"); err != nil {
		t.Fatalf("user 2 的 token 不应被误删，err=%v", err)
	}

	// 幂等：再删一次返回 0，不报错
	if n, err := db.DeleteRefreshTokensByUser(ctx, 1); err != nil || n != 0 {
		t.Fatalf("重复吊销应为 0 行且不报错，n=%d err=%v", n, err)
	}
}
