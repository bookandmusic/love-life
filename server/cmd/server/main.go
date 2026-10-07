package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/bookandmusic/love-life/server/internal/api"
	"github.com/bookandmusic/love-life/server/internal/auth"
	"github.com/bookandmusic/love-life/server/internal/config"
	"github.com/bookandmusic/love-life/server/internal/crypto"
	"github.com/bookandmusic/love-life/server/internal/media"
	"github.com/bookandmusic/love-life/server/internal/notify"
	"github.com/bookandmusic/love-life/server/internal/search"
	"github.com/bookandmusic/love-life/server/internal/store"
	"github.com/bookandmusic/love-life/server/internal/tools"
	"github.com/bookandmusic/love-life/server/internal/webui"
)

// tmpMediaTTL 临时区孤儿媒体的保留时长（上传后未发布即清理）。
const tmpMediaTTL = 24 * time.Hour

// startJanitor 每小时例行：临时区孤儿媒体（tmpMediaTTL 未发布）、过期 refresh token、
// 纪念日提醒检查（ 站内+外部同发；按「当日已发」幂等，每小时跑一次不会重复）。
func startJanitor(db *store.DB, mp *media.Pipeline, dp *notify.Dispatcher, ix *search.Index) {
	go func() {
		// 启动即回填一轮：首次配置嵌入模型或换模型后不必等满一小时。
		backfillSearch(context.Background(), db, ix)
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if n := mp.CleanTmp(tmpMediaTTL); n > 0 {
				slog.Info("tmp media cleaned", "files", n)
			}
			if err := db.DeleteExpiredRefreshTokens(context.Background()); err != nil {
				slog.Warn("clean expired refresh tokens failed", "err", err)
			}
			dp.CheckAnniversaries(context.Background(), time.Now())
			backfillSearch(context.Background(), db, ix)
		}
	}()
}

// purgeCutoff 保留期截止时刻：库内时间列为 UTC（见 store 时间口径），故统一按 UTC 计算。
func purgeCutoff(now time.Time, retentionDays int64) time.Time {
	return now.UTC().AddDate(0, 0, -int(retentionDays))
}

// nextPurgeAt now 之后（不含）第一个本地 0 点。
// 注：夏令时跳变日若本地 0 点不存在，time.Date 会归一到 01:00，可能提前补跑一轮；
// 清理幂等（不会多删、不会多写日志），属已知可接受行为。
func nextPurgeAt(now time.Time) time.Time {
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, 1)
}

// startDailyPurge 每日本地 0 点执行回收站超期永久删除。
// 启动先跑一轮：0 点整服务未运行时，同日启动即补齐漏跑（判定幂等，不会重复删除）。
func startDailyPurge(db *store.DB, mp *media.Pipeline) {
	go func() {
		runPurge(db, mp)
		for {
			time.Sleep(time.Until(nextPurgeAt(time.Now())))
			runPurge(db, mp)
		}
	}()
}

// runPurge 执行一轮永久删除：读保留期 → 删库 → 删媒体文件 → 写回收站操作日志。
// 失败只告警、不阻断服务；不做向量清理（软删时已移除，见 store.PurgeExpiredRecycle 注释）。
func runPurge(db *store.DB, mp *media.Pipeline) {
	ctx := context.Background()
	settings, err := db.GetSecuritySettings(ctx)
	if err != nil {
		slog.Warn("read security settings failed", "err", err)
		return
	}
	days := settings.RecycleRetentionDays
	if days < store.RecycleRetentionMin || days > store.RecycleRetentionMax {
		// 读侧兜底：越界值只可能来自直接改库（写入侧已校验）。不可逆操作宁可跳过一轮。
		slog.Error("recycle retention out of range, skip purge", "days", days, "min", store.RecycleRetentionMin, "max", store.RecycleRetentionMax)
		return
	}
	res, err := db.PurgeExpiredRecycle(ctx, purgeCutoff(time.Now(), days))
	if err != nil {
		slog.Warn("purge expired recycle failed", "err", err)
		return
	}
	if res.Total() == 0 {
		return
	}
	// 库行已删；文件删除失败只会留下孤儿文件，不影响正确性。
	mp.Delete(res.MediaPaths...)
	if err := db.AppendRestoreLog(ctx, "系统定时任务", res.Summary()); err != nil {
		slog.Error("write purge log failed", "err", err)
	}
	slog.Info("purge expired recycle", "deleted", res.Total(), "media", len(res.MediaPaths))
}

func main() {
	cfg := config.Load()
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		slog.Error("create data dir failed", "dir", cfg.DataDir, "err", err)
		os.Exit(1)
	}

	db, err := store.Open(cfg.DataDir)
	if err != nil {
		slog.Error("open store failed", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := bootstrapAdmin(db, cfg); err != nil {
		slog.Error("bootstrap admin failed", "err", err)
		os.Exit(1)
	}

	tm, err := auth.LoadOrCreateSecret(cfg.DataDir)
	if err != nil {
		slog.Error("load jwt secret failed", "err", err)
		os.Exit(1)
	}

	mp := media.New(cfg.DataDir, tm.Secret(), cfg.MediaMaxBytes)
	dp := notify.New(db)

	// 语义检索：向量库是数据目录内的派生数据（可从动态正文重建，不入备份语义）。
	// 向量库打不开只告警——检索回落关键词匹配，不影响记录功能。
	ix, ixErr := search.Open(filepath.Join(cfg.DataDir, "vectors"), searchResolver(db, tm.Secret()))
	if ixErr != nil {
		slog.Warn("语义检索不可用，检索回落关键词匹配", "err", ixErr)
	}
	var searchPort tools.SearchIndex
	if ix != nil {
		// 嵌入调用计入 AI 用量（次数/耗时，不含 token——见 search.embed 注释），
		// 否则仪表盘会给人「语义检索零成本」的错觉。
		ix.SetUsageRecorder(embeddingUsage{db: db})
		searchPort = ix
		db.SetPostIndex(ix)
	}
	startJanitor(db, mp, dp, ix)
	startDailyPurge(db, mp)

	webuiFS, err := webui.FS()
	if err != nil {
		slog.Error("webui fs failed", "err", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           api.NewRouter(cfg, db, tm, mp, dp, webuiFS, searchPort),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		slog.Info("server listening", "addr", cfg.Listen, "data_dir", cfg.DataDir)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server exited", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("graceful shutdown failed", "err", err)
	}
	slog.Info("server stopped")
}

// searchResolver 组装嵌入链：kind=embedding 的模型条目按链序（chain_order, id）逐个给出，
// 密钥解密后传入（明文只在内存）。链为空或全部 Key 解密失败都视为「未配置」→ 检索回落关键词。
// 嵌入条目只允许 openai-chat 供应商由写入侧校验，这里不再重复判定。
func searchResolver(db *store.DB, seed []byte) search.Resolver {
	// 供应商 Key 用 KeyFrom(数据目录密钥) 加密（与 chat 服务、供应商接口同口径）
	key := crypto.KeyFrom(seed)
	return func(ctx context.Context) ([]search.Config, bool) {
		chain, err := db.ListModels(ctx, "embedding")
		if err != nil {
			slog.Warn("read embedding chain failed", "err", err)
			return nil, false
		}
		cfgs := make([]search.Config, 0, len(chain))
		for _, e := range chain {
			apiKey, derr := crypto.Decrypt(key, e.Provider.APIKeyEnc)
			if derr != nil {
				slog.Warn("embedding provider key decrypt failed", "provider_id", e.Provider.ID, "err", derr)
				continue
			}
			cfgs = append(cfgs, search.Config{BaseURL: e.Provider.BaseURL, APIKey: apiKey, Model: e.Model.ModelID})
		}
		if len(cfgs) == 0 {
			return nil, false
		}
		return cfgs, true
	}
}

const (
	backfillBatch = 200 // 单轮上限：换模型后集合被重建时，避免一轮打出上千个嵌入请求
	backfillPage  = 50
)

// embeddingUsage 把嵌入调用落进 ai_usage_logs（search.UsageRecorder 的实现）。
// 失败只告警：用量统计是派生数据，不能反过来影响检索与写入。
type embeddingUsage struct {
	db *store.DB
}

func (u embeddingUsage) EmbeddingCalled(latency time.Duration, ok bool) {
	status := tools.AuditSuccess
	if !ok {
		status = "failed"
	}
	// 模型名每次实时解析：后台可随时改嵌入链，启动时固化会让用量归类失真。
	model := "embedding"
	if chain, err := u.db.ListModels(context.Background(), "embedding"); err == nil && len(chain) > 0 {
		model = chain[0].Model.ModelID
	}
	if err := u.db.InsertAIUsage(context.Background(), &store.AIUsageLog{
		Model:     model,
		LatencyMs: latency.Milliseconds(),
		Status:    status,
	}); err != nil {
		slog.Warn("record embedding usage failed", "err", err)
	}
}

// backfillSearch 增量回填：逐页扫描未删动态，缺索引的补建、已有的跳过（避免重复计费）。
func backfillSearch(ctx context.Context, db *store.DB, ix *search.Index) {
	if ix == nil || !ix.Enabled(ctx) {
		return
	}
	filled, failed := backfillOnce(ctx, db, ix, backfillBatch)
	if failed > 0 {
		slog.Warn("backfill partially failed, will retry next round", "failed", failed)
	}
	if filled > 0 {
		slog.Info("backfill indexed posts", "count", filled)
	}
}

// backfillOnce 回填至多 limit 条：单条失败只跳过并计数，不让一条坏数据挡住后面全部历史；
// 无文本可嵌入的动态不计入配额（否则它们会永久占位，排在后面的动态永远补不到）。
func backfillOnce(ctx context.Context, db *store.DB, ix *search.Index, limit int) (filled, failed int) {
	for page := 1; filled+failed < limit; page++ {
		posts, total, err := db.ListPosts(ctx, page, backfillPage, false)
		if err != nil {
			slog.Warn("backfill list posts failed", "err", err)
			return filled, failed
		}
		for _, p := range posts {
			if !search.Embeddable(p.Content, p.LocationName.String) {
				continue
			}
			ok, err := ix.Has(ctx, p.ID)
			if err != nil {
				slog.Warn("backfill has check failed", "post_id", p.ID, "err", err)
				failed++
				continue
			}
			if ok {
				continue
			}
			if err := ix.IndexPost(ctx, p.ID, p.Content, p.LocationName.String); err != nil {
				slog.Warn("backfill index post failed", "post_id", p.ID, "err", err)
				failed++
				continue
			}
			filled++
			if filled+failed >= limit {
				return filled, failed
			}
		}
		if page*backfillPage >= total {
			return filled, failed
		}
	}
	return filled, failed
}

// bootstrapAdmin 部署口径：admin 由环境变量内置。仅当库中无 admin 时创建；
// 已存在则忽略环境变量（改密走后台）。
func bootstrapAdmin(db *store.DB, cfg config.Config) error {
	n, err := db.CountAdmins(context.Background())
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if cfg.AdminUser == "" || cfg.AdminPass == "" {
		return errors.New("ADMIN_USER/ADMIN_PASS 未设置且库中无 admin——首次启动必须提供")
	}
	hash, err := auth.HashPassword(cfg.AdminPass)
	if err != nil {
		return err
	}
	_, err = db.CreateUser(context.Background(), &store.User{
		Username: cfg.AdminUser, PasswordHash: hash, Role: "admin", Nickname: "管理员",
	})
	if err != nil {
		return err
	}
	slog.Info("admin user created", "username", cfg.AdminUser)
	return nil
}
