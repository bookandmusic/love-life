package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/bookandmusic/love-life/server/internal/crypto"
	"github.com/bookandmusic/love-life/server/internal/store"
	"github.com/bookandmusic/love-life/server/internal/uglink"
)

// uglinkAPI 暴露绿联通道给 APP 与后台。
//
// 两个面：
//   - APP 侧 GET /api/v1/uglink/channel：拿 {available, baseURL, cookie, expiresAt}
//   - 后台侧 /api/v1/admin/uglink/*：读写配置、测试连接
type uglinkAPI struct {
	store   *uglink.Store
	manager *uglink.Manager
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

// channel 下发通道信息给 APP。
//
// 未配置 / 停用 / 登录失败都返回 available:false + reason（照 /chat/status 先例），
// 让 APP 不必区分「没配」与「配了但连不上」——两种情况都该回落备用地址或内置地址。
func (a *uglinkAPI) channel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// 先看是否配置（不触发登录，避免 APP 一请求就撞绿联风控）
	creds, err := a.store.Load(ctx)
	if err != nil {
		// 原始错误只进服务端日志：可能含上游地址等内部信息，不透给客户端
		// （与「模型报错原文不落客户端」同一口径，见 ARCHITECTURE「明确不做与已否决项」）
		writeJSON(w, http.StatusOK, map[string]any{
			"available": false,
			"reason":    "绿联通道配置读取失败，请检查后台设置",
		})
		return
	}
	if creds == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"available": false,
			"reason":    "绿联通道未启用",
		})
		return
	}

	session, err := a.manager.Session(ctx)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"available": false,
			"reason":    uglinkReason(err),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"available": true,
		"baseURL":   session.BaseURL,
		"cookie":    session.Cookie,
		"expiresAt": session.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

// uglinkReason 把内部错误转成面向用户的可读原因。
// uglinkReason 把内部错误转成面向用户的可读原因。
// 不透原始错误文本：其中可能含发现接口地址、上游域名等内部信息。
func uglinkReason(err error) string {
	if err == uglink.ErrBackoff {
		return "绿联登录失败次数过多，暂时停用，稍后会自动重试"
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
	ok, expiresAt, failures, backoffUntil := a.manager.Status()
	resp := map[string]any{
		"configured":    true,
		"uglink_id":     stored.UglinkID,
		"port":          stored.Port,
		"username":      stored.Username,
		"enabled":       stored.Enabled,
		"has_password":  hasPassword,
		"logged_in":     ok,
		"failure_count": failures,
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
	stored := uglink.StoredCredentials{
		UglinkID: req.UglinkID,
		Port:     req.Port,
		Username: req.Username,
		Enabled:  req.Enabled,
	}
	if err := a.store.Save(r.Context(), stored, req.Password); err != nil {
		writeError(w, http.StatusInternalServerError, "保存失败")
		return
	}
	// 凭据可能变了：丢弃旧会话，下次请求重新登录
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
