package mcp

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/store"
	"github.com/bookandmusic/love-life/server/internal/tools"
)

// ---- 安全回归（自对抗探针固化）----
// 这些用例锁定外部通道的鉴权边界：哈希冒充、已吊销令牌、畸形 Authorization 头
// 均不得通过；PAT 审计必须按令牌 ID 归因（而非用户 ID）。
func newMCPEnv(t *testing.T) (*store.DB, *Service) {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.Exec(`INSERT INTO users (id,username,password_hash,role,nickname) VALUES (1,'admin','x','admin','Admin')`)
	db.Exec(`INSERT INTO users (id,username,password_hash,role,nickname) VALUES (2,'girl','x','girl','Girl')`)
	return db, NewService(db, &tools.Deps{DB: db})
}

// 安全探针 1：用「存储的哈希」当明文令牌提交，必须失败（不能二次哈希撞上）。
func TestMCPHashSubmittedAsTokenRejected(t *testing.T) {
	db, svc := newMCPEnv(t)
	plain, tok, err := db.CreatePAT(t.Context(), "t", 2, "readonly")
	if err != nil {
		t.Fatal(err)
	}
	_ = plain
	// 取库内哈希
	var hash string
	if err := db.QueryRow(`SELECT token_hash FROM personal_access_tokens WHERE id=?`, tok.ID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Authorization", "Bearer "+hash)
	rec := httptest.NewRecorder()
	svc.Handle(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("提交哈希当令牌竟然通过鉴权！body=%s", rec.Body.String())
	}
}

// 安全探针 2：已吊销令牌必须 401。

// 安全探针 2：已吊销令牌必须 401。
func TestMCPRevokedTokenRejected(t *testing.T) {
	db, svc := newMCPEnv(t)
	plain, tok, err := db.CreatePAT(t.Context(), "t", 2, "readonly")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.RevokePAT(t.Context(), tok.ID); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Authorization", "Bearer "+plain)
	rec := httptest.NewRecorder()
	svc.Handle(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token: want 401, got %d", rec.Code)
	}
}

// 安全探针 3：小写 bearer / 空 / 畸形头不得绕过，也不得 panic。

// 安全探针 3：小写 bearer / 空 / 畸形头不得绕过，也不得 panic。
func TestMCPMalformedAuthHeadersRejected(t *testing.T) {
	_, svc := newMCPEnv(t)
	for _, h := range []string{"", "Bearer", "Bearer ", "bearer llp_x", "Basic abc", "llp_plain_no_prefix", "Bearer  ", "Bearer llp_"} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		if h != "" {
			req.Header.Set("Authorization", h)
		}
		rec := httptest.NewRecorder()
		svc.Handle(rec, req)
		if rec.Code == http.StatusOK {
			t.Fatalf("header %q must not authenticate", h)
		}
	}
}

// 安全探针 4：穷举所有档位 × 所有工具，删除类工具绝不可见/不可调用。

// 安全探针 6：pat 审计的 actor_id 必须是令牌 id（不是用户 id）。
func TestMCPAuditAttributesToTokenID(t *testing.T) {
	db, svc := newMCPEnv(t)
	plain, tok, _ := db.CreatePAT(t.Context(), "MyAgent", 2, "readonly")
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_space_summary","arguments":{}}}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+plain)
	svc.Handle(rec, req)

	var actorID int64
	var actorType string
	if err := db.QueryRow(`SELECT actor_id, actor_type FROM audit_logs ORDER BY id DESC LIMIT 1`).Scan(&actorID, &actorType); err != nil {
		t.Fatal(err)
	}
	if actorType != "pat" {
		t.Fatalf("actor_type=%s want pat", actorType)
	}
	if actorID != tok.ID {
		t.Fatalf("actor_id=%d want token id %d", actorID, tok.ID)
	}
	// 且列表解析出令牌名
	list, err := db.ListAuditLogs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 || list[0].Actor != "MyAgent" {
		t.Fatalf("pat actor 解析失败: %+v", list)
	}
}
