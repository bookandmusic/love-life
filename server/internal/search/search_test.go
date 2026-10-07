package search

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"
)

// fakeUpstream 假 embeddings 上游：按文本查表返回定长向量，并记录收到的请求，
// 供断言请求形状（路径 / input / model / Authorization）与并发行为。
type fakeUpstream struct {
	srv    *httptest.Server
	mu     sync.Mutex
	table  map[string][]float32
	inputs []string
	model  string

	inflight int           // 正在处理中的请求数
	maxIn    int           // 峰值并发，用于验证写入是否被串行
	delay    time.Duration // 每个请求停留时长，放大并发窗口
}

func newFakeUpstream(t *testing.T, table map[string][]float32) *fakeUpstream {
	t.Helper()
	up := &fakeUpstream{table: table}
	up.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Errorf("path = %q, want /embeddings", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want Bearer test-key", got)
		}
		var body struct {
			Input string `json:"input"`
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		up.mu.Lock()
		up.inflight++
		if up.inflight > up.maxIn {
			up.maxIn = up.inflight
		}
		up.inputs = append(up.inputs, body.Input)
		up.model = body.Model
		delay := up.delay
		vec, ok := up.table[body.Input]
		if !ok {
			vec = []float32{0, 0, 1}
		}
		up.mu.Unlock()
		if delay > 0 {
			time.Sleep(delay)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"embedding": vec}}})
		up.mu.Lock()
		up.inflight--
		up.mu.Unlock()
	}))
	t.Cleanup(up.srv.Close)
	return up
}

func (u *fakeUpstream) modelOf() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.model
}

func (u *fakeUpstream) maxInflight() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.maxIn
}

// setTable 中途换向量表（模拟供应商在同模型名下改了向量维度）。
func (u *fakeUpstream) setTable(table map[string][]float32) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.table = table
}

func fixedResolver(cfg Config, ok bool) Resolver {
	return func(context.Context) ([]Config, bool) {
		if !ok {
			return nil, false
		}
		return []Config{cfg}, true
	}
}

func TestIndexDisabled(t *testing.T) {
	ix, err := Open(t.TempDir(), fixedResolver(Config{}, false))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if ix.Enabled(ctx) {
		t.Fatal("未配置嵌入模型时应为关闭")
	}
	if _, err := ix.Query(ctx, "海边", 5); !errors.Is(err, ErrDisabled) {
		t.Fatalf("Query err = %v, want ErrDisabled", err)
	}
	if err := ix.Upsert(ctx, 1, "海边的日落真美"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("Upsert err = %v, want ErrDisabled", err)
	}
}

func TestIndexUpsertQueryRemove(t *testing.T) {
	up := newFakeUpstream(t, map[string][]float32{
		"海边的日落真美": {1, 0, 0},
		"看海":      {1, 0, 0},
		"山里的清晨":   {0, 1, 0},
	})
	ix, err := Open(t.TempDir(), fixedResolver(Config{BaseURL: up.srv.URL, APIKey: "test-key", Model: "fake-embed"}, true))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := ix.Upsert(ctx, 7, "海边的日落真美"); err != nil {
		t.Fatal(err)
	}
	if err := ix.Upsert(ctx, 8, "山里的清晨"); err != nil {
		t.Fatal(err)
	}
	if got := up.modelOf(); got != "fake-embed" {
		t.Fatalf("上游收到 model = %q, want fake-embed", got)
	}
	// 同义不同词：查询「看海」在假上游里与「海边的日落真美」同向量，应排第一。
	ids, err := ix.Query(ctx, "看海", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) == 0 || ids[0] != 7 {
		t.Fatalf("ids = %v, want 首条 7", ids)
	}
	if ok, err := ix.Has(ctx, 7); err != nil || !ok {
		t.Fatalf("Has(7) = %v,%v want true,nil", ok, err)
	}
	if ok, err := ix.Has(ctx, 999); err != nil || ok {
		t.Fatalf("Has(999) = %v,%v want false,nil", ok, err)
	}
	if err := ix.Remove(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if ok, _ := ix.Has(ctx, 7); ok {
		t.Fatal("移除后不应再有向量")
	}
	ids, _ = ix.Query(ctx, "看海", 5)
	for _, id := range ids {
		if id == 7 {
			t.Fatal("移除后不应再命中 7")
		}
	}
}

func TestIndexModelSwitch(t *testing.T) {
	up := newFakeUpstream(t, map[string][]float32{"海边": {1, 0, 0}})
	cfg := Config{BaseURL: up.srv.URL, APIKey: "test-key", Model: "m1"}
	ix, err := Open(t.TempDir(), func(context.Context) ([]Config, bool) { return []Config{cfg}, true })
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := ix.Upsert(ctx, 1, "海边"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := ix.Has(ctx, 1); !ok {
		t.Fatal("换模型前应有向量")
	}
	cfg.Model = "m2"
	if ok, _ := ix.Has(ctx, 1); ok {
		t.Fatal("换模型后应切到新集合（旧 ID 不在其中）")
	}
	// 空集合查询不报错、返回空（chromem 在 nResults 超集合条数时会报错，须收敛）。
	if ids, err := ix.Query(ctx, "海边", 10); err != nil || len(ids) != 0 {
		t.Fatalf("空集合 Query = %v,%v want 空,nil", ids, err)
	}
	if err := ix.Upsert(ctx, 2, "海边"); err != nil {
		t.Fatal(err)
	}
	ids, err := ix.Query(ctx, "海边", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != 2 {
		t.Fatalf("ids = %v, want [2]", ids)
	}
}

// 写入必须串行：并发 Upsert 时上游不应同时处理两个请求。
// 锁若缺失、或只加在取集合那一步（写操作落在锁外），这里的峰值并发就会 > 1。
func TestIndexSerializesWrites(t *testing.T) {
	up := newFakeUpstream(t, map[string][]float32{"动态": {1, 0, 0}})
	up.mu.Lock()
	up.delay = 30 * time.Millisecond
	up.mu.Unlock()
	ix, err := Open(t.TempDir(), fixedResolver(Config{BaseURL: up.srv.URL, APIKey: "test-key", Model: "m"}, true))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := int64(1); i <= 5; i++ {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			<-start
			if err := ix.Upsert(ctx, id, "动态"); err != nil {
				t.Errorf("Upsert(%d): %v", id, err)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	if got := up.maxInflight(); got != 1 {
		t.Fatalf("并发写入未被串行：上游峰值并发 = %d, want 1", got)
	}
}

// 维度变化（同模型名、供应商换了底层模型）：不同维度的向量不能混在一个集合里——
// chromem 的相似度计算不做长度校验，混维度会越界，故必须丢弃旧集合重建。
func TestIndexDimensionChangeResets(t *testing.T) {
	up := newFakeUpstream(t, map[string][]float32{"海边": {1, 0, 0}})
	ix, err := Open(t.TempDir(), fixedResolver(Config{BaseURL: up.srv.URL, APIKey: "test-key", Model: "m"}, true))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := ix.Upsert(ctx, 1, "海边"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := ix.Has(ctx, 1); !ok {
		t.Fatal("换维度前应有向量")
	}
	up.setTable(map[string][]float32{"海边": {1, 0}}) // 同模型名改为 2 维
	if err := ix.Upsert(ctx, 2, "海边"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := ix.Has(ctx, 1); ok {
		t.Fatal("维度变化后旧向量应已被丢弃，不能与新维度混存")
	}
	ids, err := ix.Query(ctx, "海边", 10)
	if err != nil {
		t.Fatalf("维度变化后 Query 不应报错: %v", err)
	}
	if len(ids) != 1 || ids[0] != 2 {
		t.Fatalf("ids = %v, want [2]", ids)
	}
}

// 换模型即换集合，旧集合须从库中删除，避免数据目录累积陈旧集合。
func TestIndexModelSwitchDropsOldCollection(t *testing.T) {
	up := newFakeUpstream(t, map[string][]float32{"海边": {1, 0, 0}})
	old := Config{BaseURL: up.srv.URL, APIKey: "test-key", Model: "m1"}
	cfg := old
	ix, err := Open(t.TempDir(), func(context.Context) ([]Config, bool) { return []Config{cfg}, true })
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := ix.Upsert(ctx, 1, "海边"); err != nil {
		t.Fatal(err)
	}
	oldName := collectionName(fingerprint(old))
	if _, ok := ix.db.ListCollections()[oldName]; !ok {
		t.Fatalf("旧集合应存在: %v", ix.db.ListCollections())
	}
	cfg.Model = "m2"
	if _, err := ix.Query(ctx, "海边", 5); err != nil {
		t.Fatal(err)
	}
	if _, ok := ix.db.ListCollections()[oldName]; ok {
		t.Fatalf("换模型后旧集合应被删除: %v", ix.db.ListCollections())
	}
}

// 相关性下限（2026-09-22）：低于 minSimilarity 的余弦相似度结果不得返回——
// 无阈值时无关查询会返回全部文档（chromem 无条件 top-k），「语义落空→关键词兜底」永不触发。
func TestIndexSimilarityThreshold(t *testing.T) {
	up := newFakeUpstream(t, map[string][]float32{
		"海边日落": {1, 0}, // 与查询同向（sim=1）
		"完全无关": {0, 1}, // 与查询垂直（sim=0 < 0.5）
		"查海":   {1, 0},
	})
	ix, err := Open(t.TempDir(), fixedResolver(Config{BaseURL: up.srv.URL, APIKey: "test-key", Model: "fake-embed"}, true))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := ix.Upsert(ctx, 1, "海边日落"); err != nil {
		t.Fatal(err)
	}
	if err := ix.Upsert(ctx, 2, "完全无关"); err != nil {
		t.Fatal(err)
	}
	ids, err := ix.Query(ctx, "查海", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != 1 {
		t.Fatalf("ids = %v, want 只有 [1]（id 2 相似度 0 应被阈值过滤）", ids)
	}
}

// 链首嵌入失败会下移到后续条目——此时向量写在**后续条目**的集合里。
// Remove 若按链首定位集合，collectionLocked 会因为 key 不同而**把真正存着向量的集合整个删掉**
// （不是只删目标那一条）：同批写入的其他向量会一起消失，属白删。
// 故断言「删掉目标后，同批的另一条仍在」——这是能区分两种实现的观测点。
func TestRemoveKeepsSiblingsInWrittenCollection(t *testing.T) {
	up := newFakeUpstream(t, map[string][]float32{"海边": {1, 0, 0}, "山里": {0, 1, 0}})
	dead := newFakeUpstream(t, nil)
	dead.srv.Close()

	cfgDead := Config{BaseURL: dead.srv.URL, APIKey: "test-key", Model: "m-dead"}
	cfgOK := Config{BaseURL: up.srv.URL, APIKey: "test-key", Model: "m-ok"}
	ix, err := Open(t.TempDir(), func(context.Context) ([]Config, bool) {
		return []Config{cfgDead, cfgOK}, true
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// 链首必然失败 → 两条都落在 cfgOK 的集合里
	if err := ix.Upsert(ctx, 7, "海边"); err != nil {
		t.Fatalf("upsert 7: %v", err)
	}
	if err := ix.Upsert(ctx, 8, "山里"); err != nil {
		t.Fatalf("upsert 8: %v", err)
	}
	if err := ix.Remove(ctx, 7); err != nil {
		t.Fatalf("remove: %v", err)
	}
	// 兄弟条目必须仍在：删错集合的实现会把整个集合换掉，8 也一起没了
	ids, err := ix.Query(ctx, "山里", 5)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if !slices.Contains(ids, int64(8)) {
		t.Fatalf("Remove 删错了集合（把同批的 8 也一起清了）：ids=%v", ids)
	}
	// 目标确实被删了
	ids7, err := ix.Query(ctx, "海边", 5)
	if err != nil {
		t.Fatalf("query 7: %v", err)
	}
	for _, id := range ids7 {
		if id == 7 {
			t.Fatalf("目标 7 应已删除：ids=%v", ids7)
		}
	}
}
