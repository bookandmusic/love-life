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
	"github.com/bookandmusic/love-life/server/internal/llm"
	"github.com/bookandmusic/love-life/server/internal/media"
	"github.com/bookandmusic/love-life/server/internal/notify"
	"github.com/bookandmusic/love-life/server/internal/search"
	"github.com/bookandmusic/love-life/server/internal/store"
	"github.com/bookandmusic/love-life/server/internal/tools"
	"github.com/bookandmusic/love-life/server/internal/webui"
)

// startJanitor 每小时例行：临时区孤儿媒体（24h 未发布）、过期 refresh token、
// 纪念日提醒检查（需求 §4.6 站内+外部同发；按「当日已发」幂等，每小时跑一次不会重复）。
func startJanitor(db *store.DB, mp *media.Pipeline, dp *notify.Dispatcher, ix *search.Index) {
	go func() {
		// 启动即回填一轮：首次配置嵌入模型或换模型后不必等满一小时。
		backfillSearch(context.Background(), db, ix)
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if n := mp.CleanTmp(24 * time.Hour); n > 0 {
				slog.Info("tmp media cleaned", "files", n)
			}
			if err := db.DeleteExpiredRefreshTokens(context.Background()); err != nil {
				slog.Warn("clean expired refresh tokens failed", "err", err)
			}
			dp.CheckAnniversaries(context.Background(), time.Now())
			dp.CheckMonthlyAlbum(context.Background(), time.Now())
			backfillSearch(context.Background(), db, ix)
			if err := db.CleanExpiredChatMessages(context.Background()); err != nil {
				slog.Warn("clean expired chat messages failed", "err", err)
			}
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

// searchResolver 组装一次嵌入调用所需的配置：后台选定的供应商 + 嵌入模型，
// 密钥解密后传入（明文只在内存）。任一步不成立都视为「未配置」→ 检索回落关键词。
func searchResolver(db *store.DB, seed []byte) search.Resolver {
	// 供应商 Key 用 KeyFrom(数据目录密钥) 加密（与 chat 服务、供应商接口同口径）
	key := crypto.KeyFrom(seed)
	return func(ctx context.Context) (search.Config, bool) {
		ec, err := db.GetEmbeddingConfig(ctx)
		if err != nil {
			slog.Warn("read embedding config failed", "err", err)
			return search.Config{}, false
		}
		if ec.ProviderID == 0 || ec.Model == "" {
			return search.Config{}, false
		}
		p, err := db.GetProvider(ctx, ec.ProviderID)
		if err != nil {
			slog.Warn("embedding provider unavailable, semantic search off", "provider_id", ec.ProviderID, "err", err)
			return search.Config{}, false
		}
		if p.APIType != llm.APITypeOpenAIChat {
			slog.Warn("embedding provider api_type unsupported, semantic search off", "provider_id", p.ID, "api_type", p.APIType)
			return search.Config{}, false
		}
		apiKey, err := crypto.Decrypt(key, p.APIKeyEnc)
		if err != nil {
			slog.Warn("embedding provider key decrypt failed", "provider_id", p.ID, "err", err)
			return search.Config{}, false
		}
		return search.Config{BaseURL: p.BaseURL, APIKey: apiKey, Model: ec.Model}, true
	}
}

const (
	backfillBatch = 200 // 单轮上限：换模型后集合被重建时，避免一轮打出上千个嵌入请求
	backfillPage  = 50
)

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
			if !search.Embeddable(p.Title.String, p.Content, p.LocationName.String) {
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
			if err := ix.IndexPost(ctx, p.ID, p.Title.String, p.Content, p.LocationName.String); err != nil {
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

// bootstrapAdmin 部署口径：admin 由环境变量内置（DESIGN §3）。仅当库中无 admin 时创建；
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
