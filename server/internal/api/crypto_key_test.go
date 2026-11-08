package api

import (
	"encoding/json"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/auth"
	"github.com/bookandmusic/love-life/server/internal/crypto"
	"github.com/bookandmusic/love-life/server/internal/store"
)

// 静态加密只认 crypto_key，不认 JWT 签名密钥：轮换 jwt_secret 后，已存的供应商 Key
// 仍能解密，AI 可用性判定不受影响。
func TestCryptoKeyIsIndependentOfJWTSecret(t *testing.T) {
	encSeed := []byte("0123456789abcdef0123456789abcdef")
	ts := newTestServerWithKey(t, 500<<20, encSeed)
	hash, _ := auth.HashPassword("girl-pw")
	if _, err := ts.db.CreateUser(t.Context(), &store.User{Username: "girl", PasswordHash: hash, Role: "girl", Nickname: "她"}); err != nil {
		t.Fatalf("create girl: %v", err)
	}
	girlToken, code := loginToken(t, ts.URL, "girl", "girl-pw", "app")
	if code != 200 {
		t.Fatalf("girl login: %d", code)
	}

	// 与 router 同一把派生密钥加密的供应商 Key
	enc, err := crypto.Encrypt(crypto.KeyFrom(encSeed), "sk-test")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	res, err := ts.db.Exec(`INSERT INTO model_providers (api_type, name, base_url, api_key_enc) VALUES ('openai-chat','fake','http://127.0.0.1:1/v1',?)`, enc)
	if err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	if pid, err := res.LastInsertId(); err != nil {
		t.Fatal(err)
	} else if _, err := ts.db.Exec(`INSERT INTO provider_models (provider_id, kind, model_id, chain_order) VALUES (?, 'chat','fake-model',0)`, pid); err != nil {
		t.Fatalf("seed model: %v", err)
	}

	resp := doJSON(t, "GET", ts.URL+"/api/v1/chat/status", girlToken, nil)
	defer resp.Body.Close()
	var out struct {
		Available bool   `json:"available"`
		Reason    string `json:"reason"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if !out.Available {
		// 若服务端仍拿 jwt_secret 派生密钥，这里会是「模型 Key 解密失败」
		t.Fatalf("AI 应可用（用 crypto_key 解密），实际 available=%v reason=%q", out.Available, out.Reason)
	}
}
