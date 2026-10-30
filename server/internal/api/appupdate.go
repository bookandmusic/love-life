package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/bookandmusic/love-life/server/internal/appupdate"
)

// openFile / modTime 供 ServeContent 使用（单独包一层便于测试替位）。
func openFile(path string) (*os.File, error) { return os.Open(path) }

func modTime(path string) time.Time {
	fi, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

// appupdateAPI 应用内更新中转。
//
// 模型：**服务端预存安装包，APP 只下载本地已有的**。
// 服务端不在 APP 请求时去 GitHub 现拉（慢速链路下 46MB 要十几分钟，任何客户端都会超时）；
// 安装包由后台手动触发下载，落盘后 APP 才能看到「可更新」。
//
// 因此 APP 侧判定很简单：本地有已就绪的包 → 可更新；没有 → 当作不需要更新。
type appupdateAPI struct {
	svc *appupdate.Service

	// 下载是后台长任务：一把锁防重复触发，状态字段供后台轮询。
	dlMu      sync.Mutex
	dlRunning bool
	dlVersion string
	dlError   string
	dlStarted time.Time
}

// latest 下发「本地已就绪」的最新版本。
// 没有已就绪的包时返回 available:false——APP 据此判定无需更新（不是错误）。
func (a *appupdateAPI) latest(w http.ResponseWriter, r *http.Request) {
	rel := a.svc.ReadyRelease(r.Context())
	if rel == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"available": false,
			"reason":    "服务端暂无已就绪的安装包",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"available":        true,
		"version_name":     rel.VersionName,
		"version_code":     rel.VersionCode,
		"notes":            rel.Notes,
		"min_version_code": rel.MinVersion,
		"asset_name":       rel.AssetName,
		"asset_size":       rel.AssetSize,
		// 相对路径：APP 拼上当前 base（与业务请求同一地址，含绿联通道）
		"download_path": "/api/v1/app/update/download",
	})
}

// download 下发本地已就绪的安装包；纯本地文件，立即开始并支持 Range 续传。
func (a *appupdateAPI) download(w http.ResponseWriter, r *http.Request) {
	rel := a.svc.ReadyRelease(r.Context())
	if rel == nil {
		writeError(w, http.StatusNotFound, "服务端暂无已就绪的安装包")
		return
	}
	path := a.svc.LocalAPK(rel)
	if path == "" {
		writeError(w, http.StatusNotFound, "服务端暂无已就绪的安装包")
		return
	}
	f, err := openFile(path)
	if err != nil {
		slog.Error("appupdate: 打开安装包失败", "path", path, "err", err)
		writeError(w, http.StatusInternalServerError, "安装包读取失败")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	http.ServeContent(w, r, rel.AssetName, modTime(path), f)
}

// ---- 后台 ----

type appupdateConfigReq struct {
	ProxyURL      string `json:"proxy_url"`
	ProxyUser     string `json:"proxy_user"`
	ProxyPassword string `json:"proxy_password"`
	ClearPassword bool   `json:"clear_password"`
}

// getConfig 返回代理配置、缓存状态与下载任务状态。
func (a *appupdateAPI) getConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg, hasPassword, err := a.svc.LoadConfigForDisplay(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取配置失败")
		return
	}
	usage := a.svc.Usage(ctx)

	a.dlMu.Lock()
	running, dlVersion, dlError := a.dlRunning, a.dlVersion, a.dlError
	a.dlMu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"proxy_url":    cfg.ProxyURL,
		"proxy_user":   cfg.ProxyUser,
		"has_password": hasPassword,
		"cache":        usage,
		"downloading":  running,
		"download_of":  dlVersion,
		"download_err": dlError,
	})
}

// putConfig 保存代理配置。
func (a *appupdateAPI) putConfig(w http.ResponseWriter, r *http.Request) {
	var req appupdateConfigReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求体格式错误")
		return
	}
	cfg := appupdate.Config{ProxyURL: req.ProxyURL, ProxyUser: req.ProxyUser}
	if err := a.svc.SaveConfig(r.Context(), cfg, req.ProxyPassword, req.ClearPassword); err != nil {
		writeError(w, http.StatusBadRequest, "保存失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// check 查 GitHub 最新版本（不下载），供后台展示「有没有可下载的新版」。
func (a *appupdateAPI) check(w http.ResponseWriter, r *http.Request) {
	rel, err := a.svc.Release(r.Context(), true)
	if err != nil {
		slog.Error("appupdate: 检查最新版本失败", "err", err)
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "reason": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"version_name": rel.VersionName,
		"asset_size":   rel.AssetSize,
		"ready":        a.svc.LocalAPK(rel) != "",
	})
}

// startDownload 后台下载安装包（长任务，立即返回）。
// 慢速链路下可能跑十几分钟，故不阻塞请求；进度由 getConfig 的 downloading 字段反映。
func (a *appupdateAPI) startDownload(w http.ResponseWriter, r *http.Request) {
	a.dlMu.Lock()
	if a.dlRunning {
		version := a.dlVersion
		a.dlMu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "reason": "已有下载任务进行中（" + version + "）"})
		return
	}
	a.dlRunning = true
	a.dlVersion = ""
	a.dlError = ""
	a.dlStarted = time.Now()
	a.dlMu.Unlock()

	// 后台执行：先查最新版本再下载。用独立 context（不随请求取消），
	// 否则请求一返回下载就被中断。
	go func() {
		ctx := context.Background()
		rel, err := a.svc.Release(ctx, true)
		if err == nil {
			a.dlMu.Lock()
			a.dlVersion = rel.VersionName
			a.dlMu.Unlock()
			_, err = a.svc.Download(ctx, rel)
		}
		a.dlMu.Lock()
		a.dlRunning = false
		if err != nil {
			a.dlError = err.Error()
			slog.Error("appupdate: 后台下载失败", "err", err)
		} else if rel != nil {
			slog.Info("appupdate: 后台下载完成", "version", rel.VersionName)
		}
		a.dlMu.Unlock()
	}()

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// remove 删除已缓存的安装包与元数据。
func (a *appupdateAPI) remove(w http.ResponseWriter, r *http.Request) {
	if err := a.svc.Cleanup(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
