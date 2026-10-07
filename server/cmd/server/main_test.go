package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bookandmusic/love-life/server/internal/crypto"
	"github.com/bookandmusic/love-life/server/internal/media"
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
	res, err := db.Exec(`INSERT INTO model_providers (api_type, name, base_url, api_key_enc) VALUES ('openai-chat', '本地', 'http://127.0.0.1:11434/v1', ?)`, enc)
	if err != nil {
		t.Fatalf("insert provider: %v", err)
	}
	id, _ := res.LastInsertId()
	if _, err := db.CreateModel(ctx, &store.ProviderModel{ProviderID: id, Kind: "embedding", ModelID: "bge-m3"}); err != nil {
		t.Fatalf("create embedding model: %v", err)
	}

	got, ok := searchResolver(db, seed)(ctx)
	if !ok {
		t.Fatal("ok=false, want configured")
	}
	if got[0].APIKey != "sk-embed" || got[0].Model != "bge-m3" || got[0].BaseURL != "http://127.0.0.1:11434/v1" {
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
	ix, err := search.Open(t.TempDir(), func(context.Context) ([]search.Config, bool) {
		return []search.Config{{BaseURL: up.URL, APIKey: "k", Model: "m"}}, true
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

// 保留期换算截止时刻：库内时间列为 UTC，故统一按 UTC 计算。
func TestPurgeCutoff(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	if got, want := purgeCutoff(now, 30), time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("purgeCutoff = %v, want %v", got, want)
	}
	// 保留期边界值：7 与 365 天（ 值域）
	if got, want := purgeCutoff(now, 7), time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("purgeCutoff(7) = %v, want %v", got, want)
	}
}

// 下一次本地 0 点：0 点整也指向次日（严格不含当前时刻）。
func TestNextPurgeAt(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	want := time.Date(2026, 9, 15, 0, 0, 0, 0, loc)
	for _, now := range []time.Time{
		time.Date(2026, 9, 14, 0, 0, 0, 0, loc),
		time.Date(2026, 9, 14, 0, 0, 30, 0, loc),
		time.Date(2026, 9, 14, 23, 59, 59, 0, loc),
	} {
		if got := nextPurgeAt(now); !got.Equal(want) {
			t.Fatalf("nextPurgeAt(%v) = %v, want %v", now, got, want)
		}
	}
}

// runPurge 编排链路：过期行删除 + 媒体物理删除 + 回收站操作日志。
func TestRunPurgeDeletesExpiredMediaAndLogs(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec("INSERT INTO users (username, password_hash, role) VALUES ('a', 'x', 'girl')"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	res, err := db.Exec("INSERT INTO posts (author_id, content, deleted_at, deleted_by_type) VALUES (1, '过期动态', datetime('now','-40 days'), 'user')")
	if err != nil {
		t.Fatalf("seed post: %v", err)
	}
	postID, _ := res.LastInsertId()
	if _, err := db.Exec("INSERT INTO media (post_id, kind, file_path, thumb_path, deleted_at, deleted_by_type) VALUES (?, 'photo', 'media/e2e.jpg', 'media/e2e-thumb.jpg', datetime('now','-40 days'), 'user')", postID); err != nil {
		t.Fatalf("seed media: %v", err)
	}
	for _, rel := range []string{"media/e2e.jpg", "media/e2e-thumb.jpg"} {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mp := media.New(dir, []byte("0123456789abcdef0123456789abcdef"), 1<<20)
	runPurge(db, mp)

	for _, rel := range []string{"media/e2e.jpg", "media/e2e-thumb.jpg"} {
		if _, err := os.Stat(filepath.Join(dir, rel)); !os.IsNotExist(err) {
			t.Errorf("%s 应已物理删除", rel)
		}
	}
	var posts, mediaRows, logs int
	_ = db.QueryRow("SELECT COUNT(*) FROM posts").Scan(&posts)
	_ = db.QueryRow("SELECT COUNT(*) FROM media").Scan(&mediaRows)
	_ = db.QueryRow("SELECT COUNT(*) FROM restore_logs WHERE operator = '系统定时任务'").Scan(&logs)
	if posts != 0 || mediaRows != 0 {
		t.Errorf("posts=%d media=%d, want 0/0", posts, mediaRows)
	}
	if logs != 1 {
		t.Errorf("restore_logs=%d, want 1", logs)
	}
}

// 保留期越界（历史脏值/直接改库）：跳过本轮，绝不误删。
func TestRunPurgeSkipsOutOfRangeRetention(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec("INSERT INTO users (username, password_hash, role) VALUES ('a', 'x', 'girl')"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := db.Exec("INSERT INTO posts (author_id, content, deleted_at, deleted_by_type) VALUES (1, '过期动态', datetime('now','-40 days'), 'user')"); err != nil {
		t.Fatalf("seed post: %v", err)
	}
	zero := int64(0)
	if err := db.SaveSecuritySettings(context.Background(), &zero); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	mp := media.New(dir, []byte("0123456789abcdef0123456789abcdef"), 1<<20)
	runPurge(db, mp)

	var posts, logs int
	_ = db.QueryRow("SELECT COUNT(*) FROM posts").Scan(&posts)
	_ = db.QueryRow("SELECT COUNT(*) FROM restore_logs").Scan(&logs)
	if posts != 1 || logs != 0 {
		t.Fatalf("保留期越界必须跳过：posts=%d（want 1）logs=%d（want 0）", posts, logs)
	}
}

// embeddingUsage 落库：嵌入调用写入 ai_usage_logs（次数与成败可见，token 恒 0）。
func TestEmbeddingUsageRecordsToAIUsageLogs(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// 先落嵌入配置：模型名须「调用时实时解析」（启动时固化会记成兜底名 embedding）
	res, err := db.Exec("INSERT INTO model_providers (api_type, name, base_url, api_key_enc) VALUES ('openai-chat', '本地', 'http://127.0.0.1:1/v1', 'x')")
	if err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	pid, _ := res.LastInsertId()
	if _, err := db.CreateModel(t.Context(), &store.ProviderModel{ProviderID: pid, Kind: "embedding", ModelID: "bge-m3"}); err != nil {
		t.Fatalf("create embedding model: %v", err)
	}

	u := embeddingUsage{db: db}
	u.EmbeddingCalled(120*time.Millisecond, true)
	u.EmbeddingCalled(30*time.Millisecond, false)

	var n, success, failed int
	_ = db.QueryRow("SELECT COUNT(*) FROM ai_usage_logs WHERE model = 'bge-m3'").Scan(&n)
	_ = db.QueryRow("SELECT COUNT(*) FROM ai_usage_logs WHERE model = 'bge-m3' AND status = 'success'").Scan(&success)
	_ = db.QueryRow("SELECT COUNT(*) FROM ai_usage_logs WHERE model = 'bge-m3' AND status = 'failed'").Scan(&failed)
	if n != 2 || success != 1 || failed != 1 {
		t.Fatalf("rows=%d success=%d failed=%d, want 2/1/1", n, success, failed)
	}
	var tokens int
	_ = db.QueryRow("SELECT COALESCE(SUM(total_tokens),0) FROM ai_usage_logs WHERE model = 'bge-m3'").Scan(&tokens)
	if tokens != 0 {
		t.Fatalf("total_tokens = %d, want 0（嵌入不记 token）", tokens)
	}
}
