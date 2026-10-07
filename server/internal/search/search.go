// Package search 动态的向量索引：chromem-go 持久库 + 复用既有模型供应商的 OpenAI 兼容
// embeddings 接口。
//
// 向量属派生数据——可从动态正文重建，故不纳入备份语义；库落在数据目录内。
package search

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	chromem "github.com/philippgille/chromem-go"
)

// ErrDisabled 未配置嵌入模型（调用方据此回落关键词检索）。
var ErrDisabled = errors.New("语义检索未配置嵌入模型")

// minSimilarity 语义检索相关性下限（余弦相似度，[-1,1]）：0.5 为常用起点，后续按实测调整。
// 设为后台可配置项的增量需求另立项（当前为代码常量）。
const minSimilarity float32 = 0.5

// Config 一次嵌入调用所需信息；APIKey 为明文，由调用方解密后传入。
type Config struct {
	BaseURL string
	APIKey  string
	Model   string
}

// Resolver 取当前嵌入链（按链序排列）；ok=false 表示链为空。配置可在运行时变更（后台可改），
// 故每次操作都取一遍——模型条目个位数量级，读取代价可忽略，省掉缓存失效逻辑。
type Resolver func(ctx context.Context) ([]Config, bool)

// UsageRecorder 嵌入调用的用量记录端口（由 main 注入 store 侧实现，避免 search 依赖 store）。
// 只记调用次数、耗时与成败——token 不可靠（见 embed 注释）。
type UsageRecorder interface {
	EmbeddingCalled(latency time.Duration, ok bool)
}

// Index 向量索引。零值不可用，须经 Open 构造。
type Index struct {
	resolve Resolver
	usage   UsageRecorder // 可选：未注入时不记用量
	db      *chromem.DB
	dimPath string // 当前集合的向量维度落盘位置（跨重启校验维度变化）

	// mu 串行化「取集合 + 整段读写」：chromem 对同一集合文件的并发写未串行
	// （AddDocument 解锁后才落盘，Delete 则边持锁边删文件），而 REST 发动态、
	// AI 工具发动态与 janitor 回填可能同时写；Query 也用它避开 Count→Query 的竞态。
	// ponytail: 一次嵌入也在这把锁内（已加超时兜底）——2 人自用够用；若上游变慢，
	// 再把嵌入移到锁外改用 QueryEmbedding，代价是并发写只能靠 review 保证。
	mu  sync.Mutex
	col *chromem.Collection
	key string
	dim int // 已知的向量维度；0 = 本进程尚未成功嵌入过
	// writeCfg 本进程**实际写入过向量**的嵌入配置（Upsert 成功嵌入时记下）。
	// 供 Remove 定位「向量真正落在哪个集合」：链首失败会下移到后续条目，
	// 那时按链首去删会落到另一个集合上（白删）。零值 = 本进程还没写过。
	writeCfg Config
}

// Open 打开（必要时创建）向量库目录；dir 一般取数据目录下的 vectors。
// 维度记录写在 dir + ".dim"——放在 chromem 目录外侧，避免干扰它对集合目录的扫描。
func Open(dir string, resolve Resolver) (*Index, error) {
	db, err := chromem.NewPersistentDB(dir, false)
	if err != nil {
		return nil, fmt.Errorf("打开向量库失败: %w", err)
	}
	ix := &Index{resolve: resolve, db: db, dimPath: dir + ".dim"}
	if b, readErr := os.ReadFile(ix.dimPath); readErr == nil {
		if n, convErr := strconv.Atoi(string(b)); convErr == nil {
			ix.dim = n
		}
	}
	return ix, nil
}

// SetUsageRecorder 注入嵌入用量记录器；未注入（测试/单测）时静默跳过。
// 非并发安全：须在启动后台任务与 HTTP 服务前调用一次。
func (ix *Index) SetUsageRecorder(r UsageRecorder) { ix.usage = r }

// Enabled 当前嵌入链是否非空。
func (ix *Index) Enabled(ctx context.Context) bool {
	_, ok := ix.resolve(ctx)
	return ok
}

// embedChainLocked 按链序逐个尝试嵌入，首个成功即返回其配置与向量；
// 链上全部失败才报错（调用方据此回落关键词检索，ARCHITECTURE「语义检索」）。
// 调用方必须已持有 ix.mu：嵌入在写锁内进行，保持「同一时刻至多一个上游调用」的串行语义。
func (ix *Index) embedChainLocked(ctx context.Context, cfgs []Config, text string) (Config, []float32, error) {
	var lastErr error
	for _, cfg := range cfgs {
		vec, err := ix.embed(ctx, cfg, text)
		if err == nil {
			return cfg, vec, nil
		}
		lastErr = err
		slog.Warn("embedding chain entry failed, trying next", "model", cfg.Model, "err", err)
	}
	if lastErr == nil {
		lastErr = ErrDisabled
	}
	return Config{}, nil, lastErr
}

// embedText 拼装一条动态参与嵌入的文本：正文、地点按序拼接，跳过空段
// （部分供应商按 token 计费，空段只稀释语义）。
func embedText(content, location string) string {
	parts := make([]string, 0, 2)
	for _, s := range []string{content, location} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n")
}

// Upsert 写入或覆盖一条动态的向量。
func (ix *Index) Upsert(ctx context.Context, postID int64, content string) error {
	cfgs, ok := ix.resolve(ctx)
	if !ok {
		return ErrDisabled
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	cfg, vec, err := ix.embedChainLocked(ctx, cfgs, content)
	if err != nil {
		return err
	}
	ix.writeCfg = cfg // 记下实际生效的配置：Remove 要照它定位集合
	col, err := ix.collectionLocked(ctx, cfg, len(vec))
	if err != nil {
		return err
	}
	return col.AddDocument(ctx, chromem.Document{ID: postIDKey(postID), Embedding: vec, Content: content})
}

// Remove 移除一条动态的向量（软删或永久删除时调用）。
// 维度未知（本进程未成功嵌入过）时无可操作对象，按无事返回。
func (ix *Index) Remove(ctx context.Context, postID int64) error {
	cfgs, ok := ix.resolve(ctx)
	if !ok {
		return ErrDisabled
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if ix.dim == 0 {
		return nil
	}
	// 用「本进程写入过向量的那个配置」定位集合：链首失败下移时，按链首删会删错集合
	// （白删——真正存着向量的集合被换掉）。本进程还没写过（如重启后首次调用）就回退链首，
	// 那种情况下的漏删由 janitor 回填兜底（向量是派生数据，可重建）。
	cfg := cfgs[0]
	if ix.writeCfg != (Config{}) {
		cfg = ix.writeCfg
	}
	col, err := ix.collectionLocked(ctx, cfg, ix.dim)
	if err != nil {
		return err
	}
	return col.Delete(ctx, nil, nil, postIDKey(postID))
}

// IndexPost / UnindexPost 是 store.PostIndexer 的实现：store 只转发动态原始字段，
// 嵌入文本口径留在这里。未配置嵌入模型是**默认态而非失败**，故吞掉 ErrDisabled，
// 避免默认部署下每次发/删动态都刷一条告警；其余错误照常上抛，由 store 记录。
func (ix *Index) IndexPost(ctx context.Context, id int64, content, location string) error {
	if !Embeddable(content, location) {
		return nil // 纯媒体/纯心情动态没有可嵌入文本：跳过，别让上游对空输入报错
	}
	if err := ix.Upsert(ctx, id, embedText(content, location)); err != nil && !errors.Is(err, ErrDisabled) {
		return err
	}
	return nil
}

// Embeddable 该动态是否有可嵌入文本；回填据此跳过（否则这类动态会永久占住回填配额）。
func Embeddable(content, location string) bool {
	return embedText(content, location) != ""
}

func (ix *Index) UnindexPost(ctx context.Context, id int64) error {
	if err := ix.Remove(ctx, id); err != nil && !errors.Is(err, ErrDisabled) {
		return err
	}
	return nil
}

// Has 该动态是否已建向量；janitor 回填据此跳过，避免重复计费。
// 维度未知时保守返回 false（交给回填补齐）。
func (ix *Index) Has(ctx context.Context, postID int64) (bool, error) {
	cfgs, ok := ix.resolve(ctx)
	if !ok {
		return false, ErrDisabled
	}
	cfg := cfgs[0]
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if ix.dim == 0 {
		return false, nil
	}
	col, err := ix.collectionLocked(ctx, cfg, ix.dim)
	if err != nil {
		return false, err
	}
	if _, err := col.GetByID(ctx, postIDKey(postID)); err != nil {
		return false, nil
	}
	return true, nil
}

// Query 返回与 text 最相似的至多 k 条动态 ID，相似度降序。
func (ix *Index) Query(ctx context.Context, text string, k int) ([]int64, error) {
	cfgs, ok := ix.resolve(ctx)
	if !ok {
		return nil, ErrDisabled
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	cfg, vec, err := ix.embedChainLocked(ctx, cfgs, text)
	if err != nil {
		return nil, err
	}
	col, err := ix.collectionLocked(ctx, cfg, len(vec))
	if err != nil {
		return nil, err
	}
	// chromem 在 nResults 大于集合文档数时直接报错，故按集合实际条数收敛；空集合直接返回空。
	if n := col.Count(); k > n {
		k = n
	}
	if k <= 0 {
		return []int64{}, nil
	}
	results, err := col.QueryEmbedding(ctx, vec, k, nil, nil)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(results))
	for _, r := range results {
		// 相关性下限（2026-09-22）：低于阈值的相似度视为不相关——chromem 无条件返回 top-k，
		// 无阈值时无关查询也返回全部文档（「搜的不对」感来源），且关键词兜底永不触发
		if r.Similarity < minSimilarity {
			continue
		}
		if id, convErr := strconv.ParseInt(r.ID, 10, 64); convErr == nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// collectionLocked 返回当前配置与维度对应的集合；调用方必须已持有 ix.mu。
// 集合名按「端点 + 模型」指纹划分（换模型即换集合，天然不会维度不匹配）；
// 同模型名而维度变了（供应商悄悄换模型）则丢弃旧集合重建——chromem 的相似度
// 计算不做长度校验，混维度会越界，宁可重建后由 janitor 回填。
func (ix *Index) collectionLocked(ctx context.Context, cfg Config, dim int) (*chromem.Collection, error) {
	key := fingerprint(cfg)
	name := collectionName(key)
	if ix.col != nil && ix.key == key && ix.dim == dim {
		return ix.col, nil
	}
	// 换模型：旧集合不再使用，直接删除，避免数据目录累积陈旧集合
	if ix.col != nil && ix.key != key {
		_ = ix.db.DeleteCollection(ix.col.Name)
		ix.col = nil
	}
	if ix.dim != 0 && ix.dim != dim {
		_ = ix.db.DeleteCollection(name)
		ix.col = nil
	}
	col, err := ix.db.GetOrCreateCollection(name, nil, embeddingFunc(cfg))
	if err != nil {
		return nil, fmt.Errorf("打开向量集合失败: %w", err)
	}
	if ix.dim != dim {
		ix.dim = dim
		ix.writeDim(dim)
	}
	ix.col, ix.key = col, key
	return col, nil
}

// writeDim 记录维度供下次启动校验；写失败只影响重启后的判定，不影响本次检索。
func (ix *Index) writeDim(dim int) {
	_ = os.WriteFile(ix.dimPath, []byte(strconv.Itoa(dim)), 0o600)
}

// embeddingFunc chromem 自带的 OpenAI 兼容嵌入函数（无需自写适配器）。
// 每次调用重建：Normalized 的自检状态随之重置，代价可忽略。
func embeddingFunc(cfg Config) chromem.EmbeddingFunc {
	// base_url 尾斜杠会拼出 //embeddings，与供应商请求同口径做归一
	return chromem.NewEmbeddingFuncOpenAICompat(strings.TrimSuffix(cfg.BaseURL, "/"), cfg.APIKey, cfg.Model, nil)
}

// embedTimeout 单次嵌入的上限。chromem 自带的嵌入函数默认不设超时（依赖调用方 ctx），
// 上游卡住会连带拖死调用方与那把写锁，故这里统一加界。
const embedTimeout = 10 * time.Second

// embed 做一次嵌入（HTTP），并记一次用量（次数 + 耗时 + 成败）。
// 不记 token：OpenAI 兼容 /embeddings 的 usage 字段跨供应商不保证，强记 0 会误导。
func (ix *Index) embed(ctx context.Context, cfg Config, text string) ([]float32, error) {
	ctx, cancel := context.WithTimeout(ctx, embedTimeout)
	defer cancel()
	started := time.Now()
	vec, err := embeddingFunc(cfg)(ctx, text)
	if ix.usage != nil {
		ix.usage.EmbeddingCalled(time.Since(started), err == nil)
	}
	if err != nil {
		return nil, fmt.Errorf("嵌入失败: %w", err)
	}
	return vec, nil
}

// collectionName chromem 集合名：posts-<指纹前 12 位>。
func collectionName(key string) string { return "posts-" + key[:12] }

// fingerprint 配置指纹 = baseURL + model；Key 轮换不换集合（同一模型同一空间）。
func fingerprint(cfg Config) string {
	sum := sha256.Sum256([]byte(cfg.BaseURL + "\x00" + cfg.Model))
	return hex.EncodeToString(sum[:])
}

func postIDKey(id int64) string { return strconv.FormatInt(id, 10) }
