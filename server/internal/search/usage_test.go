package search

import (
	"context"
	"testing"
	"time"
)

// UsageRecorder 记录嵌入调用的用量（次数/耗时，不含 token：/embeddings 的 usage 字段跨供应商不可靠）。
type fakeUsage struct {
	calls   int
	latency []time.Duration
	failed  int
}

func (f *fakeUsage) EmbeddingCalled(d time.Duration, ok bool) {
	f.calls++
	if !ok {
		f.failed++
	}
	f.latency = append(f.latency, d)
}

// 每次嵌入调用（upsert / query）都应记一次用量；失败也要记（否则「嵌入在报错」不可见）。
func TestIndexRecordsEmbeddingUsage(t *testing.T) {
	up := newFakeUpstream(t, map[string][]float32{"海边": {1, 0, 0}})
	usage := &fakeUsage{}
	cfg := Config{BaseURL: up.srv.URL, APIKey: "test-key", Model: "m"}
	ix, err := Open(t.TempDir(), func(context.Context) (Config, bool) { return cfg, true })
	if err != nil {
		t.Fatal(err)
	}
	ix.SetUsageRecorder(usage)
	ctx := context.Background()

	if err := ix.Upsert(ctx, 1, "海边"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, err := ix.Query(ctx, "海边", 5); err != nil {
		t.Fatalf("query: %v", err)
	}
	if usage.calls != 2 {
		t.Fatalf("calls = %d, want 2（upsert + query）", usage.calls)
	}
	if usage.failed != 0 {
		t.Fatalf("failed = %d, want 0", usage.failed)
	}
}

// 未注入 recorder（如测试/未配置）时不得 panic。
func TestIndexUsageRecorderOptional(t *testing.T) {
	up := newFakeUpstream(t, map[string][]float32{"海边": {1, 0, 0}})
	cfg := Config{BaseURL: up.srv.URL, APIKey: "test-key", Model: "m"}
	ix, err := Open(t.TempDir(), func(context.Context) (Config, bool) { return cfg, true })
	if err != nil {
		t.Fatal(err)
	}
	if err := ix.Upsert(context.Background(), 1, "海边"); err != nil {
		t.Fatalf("upsert without recorder: %v", err)
	}
}

// 嵌入失败（上游 500）也应记一次 failed，便于仪表盘暴露故障。
func TestIndexRecordsFailedEmbedding(t *testing.T) {
	bad := newFailingUpstream(t)
	usage := &fakeUsage{}
	cfg := Config{BaseURL: bad, APIKey: "k", Model: "m"}
	ix, err := Open(t.TempDir(), func(context.Context) (Config, bool) { return cfg, true })
	if err != nil {
		t.Fatal(err)
	}
	ix.SetUsageRecorder(usage)
	if err := ix.Upsert(context.Background(), 1, "海边"); err == nil {
		t.Fatal("上游 500 应返回错误")
	}
	if usage.calls != 1 || usage.failed != 1 {
		t.Fatalf("calls=%d failed=%d, want 1/1", usage.calls, usage.failed)
	}
}
