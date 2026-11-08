package crypto

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// 首启用旧密钥材料落盘一份：升级前后密钥相同，历史密文照旧可解。
func TestLoadKeyCreatesFromLegacySeed(t *testing.T) {
	dir := t.TempDir()
	seed := bytes.Repeat([]byte("A"), 32)
	got, err := LoadKey(dir, seed)
	if err != nil {
		t.Fatalf("LoadKey: %v", err)
	}
	if !bytes.Equal(got, seed) {
		t.Fatalf("返回的密钥材料与 legacySeed 不一致")
	}
	fi, err := os.Stat(filepath.Join(dir, "crypto_key"))
	if err != nil {
		t.Fatalf("crypto_key 未落盘: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("crypto_key 权限 %o，want 0600", perm)
	}
}

// 落盘后与 JWT 签名密钥解耦：换了 jwt_secret，加密密钥不变（密文仍可解）。
func TestLoadKeyIgnoresLegacySeedAfterFirstRun(t *testing.T) {
	dir := t.TempDir()
	oldSeed := bytes.Repeat([]byte("A"), 32)
	if _, err := LoadKey(dir, oldSeed); err != nil {
		t.Fatalf("首次 LoadKey: %v", err)
	}
	rotated := bytes.Repeat([]byte("B"), 32)
	got, err := LoadKey(dir, rotated)
	if err != nil {
		t.Fatalf("二次 LoadKey: %v", err)
	}
	if bytes.Equal(got, rotated) {
		t.Fatal("jwt_secret 轮换后加密密钥跟着变了——两者未解耦")
	}
	if !bytes.Equal(got, oldSeed) {
		t.Fatal("加密密钥应保持首次落盘时的那份")
	}
}

// crypto_key 存在但内容不成形时报错，不静默换一把新密钥（否则历史密文永久解不开）。
func TestLoadKeyRejectsShortFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "crypto_key"), []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadKey(dir, bytes.Repeat([]byte("A"), 32)); err == nil {
		t.Fatal("内容过短应报错")
	}
}

// 既没有 crypto_key 也没有可继承的旧密钥 → 报错，不生成随机密钥。
func TestLoadKeyWithoutAnySeed(t *testing.T) {
	if _, err := LoadKey(t.TempDir(), []byte("short")); err == nil {
		t.Fatal("无可继承密钥时应报错")
	}
}
