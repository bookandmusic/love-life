package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bookandmusic/love-life/server/internal/api"
	"github.com/bookandmusic/love-life/server/internal/auth"
	"github.com/bookandmusic/love-life/server/internal/config"
	"github.com/bookandmusic/love-life/server/internal/media"
	"github.com/bookandmusic/love-life/server/internal/notify"
	"github.com/bookandmusic/love-life/server/internal/store"
	"github.com/bookandmusic/love-life/server/internal/webui"
)

// startJanitor 每小时例行：临时区孤儿媒体（24h 未发布）、过期 refresh token、
// 纪念日提醒检查（需求 §4.6 站内+外部同发；按「当日已发」幂等，每小时跑一次不会重复）。
func startJanitor(db *store.DB, mp *media.Pipeline, dp *notify.Dispatcher) {
	go func() {
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
			if err := db.CleanExpiredChatMessages(context.Background()); err != nil {
				slog.Warn("clean expired chat messages failed", "err", err)
			}
		}
	}()
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
	startJanitor(db, mp, dp)

	webuiFS, err := webui.FS()
	if err != nil {
		slog.Error("webui fs failed", "err", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           api.NewRouter(cfg, db, tm, mp, dp, webuiFS),
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
