package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/crypto"
	"github.com/bookandmusic/love-life/server/internal/search"
	"github.com/bookandmusic/love-life/server/internal/store"
)

// 供应商 Key 以 KeyFrom(数据目录密钥) 加密（与 chat 服务、供应商接口一致）；
// 解析器若拿原始密钥解密会静默失败、让语义检索退回关键词——此测试钉住该口径。
func TestSearchResolverDecryptsProviderKey(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	seed := []byte("0123456789abcdef0123456789abcdef")

	// 未配置嵌入模型 → 视为关闭（检索回落关键词）
	if _, ok := searchResolver(db, seed)(ctx); ok {
		t.Fatal("未配置时 ok=true, want false")
	}

	enc, err := crypto.Encrypt(crypto.KeyFrom(seed), "sk-embed")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	res, err := db.Exec(`INSERT INTO model_providers (api_type, name, base_url, api_key_enc, default_model, sort_order) VALUES ('openai-chat', '本地', 'http://127.0.0.1:11434/v1', ?, '', 0)`, enc)
	if err != nil {
		t.Fatalf("insert provider: %v", err)
	}
	id, _ := res.LastInsertId()
	if err := db.SaveEmbeddingConfig(ctx, id, "bge-m3"); err != nil {
		t.Fatalf("save config: %v", err)
	}

	got, ok := searchResolver(db, seed)(ctx)
	if !ok {
		t.Fatal("ok=false, want configured")
	}
	if got.APIKey != "sk-embed" || got.Model != "bge-m3" || got.BaseURL != "http://127.0.0.1:11434/v1" {
		t.Fatalf("config = %+v", got)
	}
}

// fakeEmbeddingUpstream 假 /embeddings 上游：固定三维向量，并统计被调次数（用于验证回填不重复计费）。
func fakeEmbeddingUpstream(t *testing.T) (*httptest.Server, *int64) {
	t.Helper()
	var calls int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[1,0,0]}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// 回填：单轮触顶即停、已索引跳过（不重复计费）、无文本动态不占配额。
func TestBackfillOnceRespectsLimitAndSkipsIndexed(t *testing.T) {
	up, calls := fakeEmbeddingUpstream(t)
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO users (username, password_hash, role) VALUES ('a', 'x', 'girl')`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	ctx := context.Background()
	for _, content := range []string{"第一篇", "第二篇", "第三篇"} {
		if _, err := db.CreatePostWithMedia(ctx, &store.Post{AuthorID: 1, Content: content}, nil); err != nil {
			t.Fatalf("create post: %v", err)
		}
	}
	// 纯媒体动态（无文本）：不该占用回填配额，也不该触发嵌入
	if _, err := db.CreatePostWithMedia(ctx, &store.Post{AuthorID: 1, Content: ""}, nil); err != nil {
		t.Fatalf("create empty post: %v", err)
	}
	ix, err := search.Open(t.TempDir(), func(context.Context) (search.Config, bool) {
		return search.Config{BaseURL: up.URL, APIKey: "k", Model: "m"}, true
	})
	if err != nil {
		t.Fatalf("open index: %v", err)
	}

	filled, failed := backfillOnce(ctx, db, ix, 2)
	if filled != 2 || failed != 0 || atomic.LoadInt64(calls) != 2 {
		t.Fatalf("first round = %d,%d calls=%d, want 2,0,2", filled, failed, atomic.LoadInt64(calls))
	}
	filled, failed = backfillOnce(ctx, db, ix, 10)
	if filled != 1 || failed != 0 || atomic.LoadInt64(calls) != 3 {
		t.Fatalf("second round = %d,%d calls=%d, want 1,0,3", filled, failed, atomic.LoadInt64(calls))
	}
	filled, failed = backfillOnce(ctx, db, ix, 10)
	if filled != 0 || failed != 0 || atomic.LoadInt64(calls) != 3 {
		t.Fatalf("third round = %d,%d calls=%d, want 0,0,3（已索引不得重复计费）", filled, failed, atomic.LoadInt64(calls))
	}
}
