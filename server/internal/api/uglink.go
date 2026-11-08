package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/bookandmusic/love-life/server/internal/crypto"
	"github.com/bookandmusic/love-life/server/internal/store"
	"github.com/bookandmusic/love-life/server/internal/uglink"
)

// uglinkAPI 暴露绿联通道给 APP 与后台。
//
// 后台侧 /api/v1/admin/uglink/*：读写配置、测试连接（APP 选路统一走 /api/v1/channels）。
type uglinkAPI struct {
	store   *uglink.Store
	manager *uglink.Manager
	// backup 统一选路时要列出备用地址（选路接口与备用地址管理共用同一份数据）
	backup *backupAddrAPI
}

// newUglinkAPI 构造；manager 的凭据回调读加密存储，未配置时返回 nil。
func newUglinkAPI(db *store.DB, secret []byte) *uglinkAPI {
	st := uglink.NewStore(db, crypto.KeyFrom(secret))
	api := &uglinkAPI{store: st}
	api.manager = uglink.NewManager(func(ctx context.Context) (*uglink.Credentials, error) {
		return st.Load(ctx)
	})
	return api
}

// uglinkReason 把内部错误转成面向用户的可读原因。
// 不透原始错误文本：其中可能含发现接口地址、上游域名等内部信息。
// 确定性失败（权限/凭据）直说根因——含糊的「暂时不可用」让管理员无从下手。
func uglinkReason(err error) string {
	if err == uglink.ErrBackoff {
		return "绿联登录失败次数过多，暂时停用，稍后会自动重试"
	}
	if errors.Is(err, uglink.ErrPerm) {
		return "绿联账号无管理员权限：请在 NAS 侧给该账号提权，或在后台改用管理员账号"
	}
	if errors.Is(err, uglink.ErrBadCreds) {
		return "绿联账号或密码错误：请在后台「外部集成 · 远程访问」修改凭据"
	}
	return "绿联通道暂时不可用，可在后台「外部集成 · 远程访问」查看状态或测试连接"
}

// ---- 后台配置 ----

type uglinkConfigReq struct {
	UglinkID string `json:"uglink_id"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"` // 留空表示沿用旧密码
	Enabled  bool   `json:"enabled"`
	Weight   *int   `json:"weight"` // 缺省保留原值（新建时按 50）
}

// getConfig 返回配置（密码不回显，只给 has_password）。
func (a *uglinkAPI) getConfig(w http.ResponseWriter, r *http.Request) {
	stored, hasPassword, err := a.store.LoadForDisplay(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取配置失败")
		return
	}
	if stored == nil {
		writeJSON(w, http.StatusOK, map[string]any{"configured": false})
		return
	}
	ok, expiresAt, failures, backoffUntil, configError := a.manager.Status()
	resp := map[string]any{
		"configured":    true,
		"uglink_id":     stored.UglinkID,
		"port":          stored.Port,
		"username":      stored.Username,
		"enabled":       stored.Enabled,
		"weight":        normalizeWeight(stored.Weight),
		"has_password":  hasPassword,
		"logged_in":     ok,
		"failure_count": failures,
	}
	if configError != "" {
		// 确定性配置错误（权限/凭据）：后台据此显示「配置错误」而非「未登录」
		resp["config_error"] = configError
	}
	if ok {
		resp["expires_at"] = expiresAt.UTC().Format(time.RFC3339)
	}
	if !backoffUntil.IsZero() && time.Now().Before(backoffUntil) {
		resp["backoff_until"] = backoffUntil.UTC().Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, resp)
}

// putConfig 保存配置；保存后立即丢弃旧会话（凭据可能已变）。
func (a *uglinkAPI) putConfig(w http.ResponseWriter, r *http.Request) {
	var req uglinkConfigReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求体格式错误")
		return
	}
	if req.Enabled {
		if req.UglinkID == "" || req.Username == "" {
			writeError(w, http.StatusBadRequest, "启用绿联通道需要填写设备标识与用户名")
			return
		}
		// 首次启用必须有密码；已有密码时允许留空沿用
		_, hasPassword, err := a.store.LoadForDisplay(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取配置失败")
			return
		}
		if req.Password == "" && !hasPassword {
			writeError(w, http.StatusBadRequest, "首次启用需要填写密码")
			return
		}
	}
	if req.Port <= 0 {
		req.Port = 8080
	}
	// 权重：缺省沿用已存值（新建时 50），显式传值校验 1–999 且不得为内置保留的 100
	weight := defaultChannelWeight
	if prev, _, err := a.store.LoadForDisplay(r.Context()); err == nil && prev != nil {
		weight = normalizeWeight(prev.Weight)
	}
	if req.Weight != nil {
		if msg := checkWeightRange(*req.Weight); msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		weight = *req.Weight
	}
	stored := uglink.StoredCredentials{
		UglinkID: req.UglinkID,
		Port:     req.Port,
		Username: req.Username,
		Enabled:  req.Enabled,
		Weight:   weight,
	}
	if err := a.store.Save(r.Context(), stored, req.Password); err != nil {
		writeError(w, http.StatusInternalServerError, "保存失败")
		return
	}
	// 凭据可能变了：丢弃旧会话，下次请求重新登录
	a.manager.Invalidate()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// deleteConfig 清除绿联通道配置（回到未配置态）；同时丢弃缓存会话。
func (a *uglinkAPI) deleteConfig(w http.ResponseWriter, r *http.Request) {
	if err := a.store.Delete(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "删除失败")
		return
	}
	a.manager.Invalidate()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// test 现场登录自检（会真实握手，注意风控退避）。
func (a *uglinkAPI) test(w http.ResponseWriter, r *http.Request) {
	session, err := a.manager.Refresh(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":     false,
			"reason": uglinkReason(err),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"base_url":   session.BaseURL,
		"expires_at": session.ExpiresAt.UTC().Format(time.RFC3339),
	})
}
