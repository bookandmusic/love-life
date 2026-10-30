package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bookandmusic/love-life/server/internal/store"
)

// newID 生成带前缀的随机标识（备用地址仅本地使用，无需全局唯一约束）。
func newID(prefix string) string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return prefix + "-" + hex.EncodeToString([]byte(time.Now().Format("150405.000")))
	}
	return prefix + "-" + hex.EncodeToString(buf)
}

// backupAddrSettingsKey 备用地址列表在 app_settings 里的键名。
const backupAddrSettingsKey = "backup_addresses"

// BackupAddress 一条备用服务地址。
//
// 服务端**不代理**这些地址，只存取与探活：反代、证书、DNS 由部署者自理
// （见 PRODUCT「远程访问」）。因此这里只有数据，没有协议。
// BackupAddress 一条备用服务地址。
//
// 没有优先级字段：APP 会并发探活所有启用地址，按实测延迟选最快的
// （固定优先级在切网后就会过时——家里最快的地址出门后可能完全不可达）。
type BackupAddress struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	URL     string `json:"url"`
	Enabled bool   `json:"enabled"`
}

// backupAddrAPI 备用地址的读写与探活。
type backupAddrAPI struct {
	db *store.DB
}

// load 读取列表（无配置时返回空切片而非 nil，便于前端直接遍历）。
func (a *backupAddrAPI) load(ctx context.Context) ([]BackupAddress, error) {
	raw, err := a.db.GetSetting(ctx, backupAddrSettingsKey)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return []BackupAddress{}, nil
		}
		return nil, err
	}
	var list []BackupAddress
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil, err
	}
	return list, nil
}

func (a *backupAddrAPI) save(ctx context.Context, list []BackupAddress) error {
	raw, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return a.db.SetSetting(ctx, backupAddrSettingsKey, string(raw))
}

// listForApp 下发启用中的地址（APP 并发探活，按实测延迟择优）。
func (a *backupAddrAPI) listForApp(w http.ResponseWriter, r *http.Request) {
	list, err := a.load(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取备用地址失败")
		return
	}
	out := make([]BackupAddress, 0, len(list))
	for _, item := range list {
		if item.Enabled {
			out = append(out, item)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// listAdmin 后台列表（含停用项）。
func (a *backupAddrAPI) listAdmin(w http.ResponseWriter, r *http.Request) {
	list, err := a.load(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取备用地址失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

type backupAddrReq struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Enabled bool   `json:"enabled"`
}

// validateURL 只接受 HTTPS：这些地址承载登录凭据，明文传输不可接受。
func validateBackupURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("请填写地址")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("地址格式不合法")
	}
	if u.Scheme != "https" {
		return "", errors.New("只接受 HTTPS 地址（登录凭据不可明文传输）")
	}
	if u.Host == "" {
		return "", errors.New("地址缺少域名")
	}
	// 统一去掉尾部斜杠，避免拼接出双斜杠
	return strings.TrimRight(u.String(), "/"), nil
}

// create 新增一条。
func (a *backupAddrAPI) create(w http.ResponseWriter, r *http.Request) {
	var req backupAddrReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求体格式错误")
		return
	}
	normalized, err := validateBackupURL(req.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	list, err := a.load(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取备用地址失败")
		return
	}
	for _, item := range list {
		if strings.EqualFold(item.URL, normalized) {
			writeError(w, http.StatusBadRequest, "该地址已存在")
			return
		}
	}
	list = append(list, BackupAddress{
		ID:      newID("ba"),
		Name:    strings.TrimSpace(req.Name),
		URL:     normalized,
		Enabled: req.Enabled,
	})
	if err := a.save(r.Context(), list); err != nil {
		writeError(w, http.StatusInternalServerError, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// update 修改一条。
func (a *backupAddrAPI) update(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req backupAddrReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求体格式错误")
		return
	}
	normalized, err := validateBackupURL(req.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	list, err := a.load(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取备用地址失败")
		return
	}
	found := false
	for i := range list {
		if list[i].ID != id {
			continue
		}
		found = true
		list[i].Name = strings.TrimSpace(req.Name)
		list[i].URL = normalized
		list[i].Enabled = req.Enabled
	}
	if !found {
		writeError(w, http.StatusNotFound, "地址不存在")
		return
	}
	if err := a.save(r.Context(), list); err != nil {
		writeError(w, http.StatusInternalServerError, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// remove 删除一条。
func (a *backupAddrAPI) remove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	list, err := a.load(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取备用地址失败")
		return
	}
	out := make([]BackupAddress, 0, len(list))
	removed := false
	for _, item := range list {
		if item.ID == id {
			removed = true
			continue
		}
		out = append(out, item)
	}
	if !removed {
		writeError(w, http.StatusNotFound, "地址不存在")
		return
	}
	if err := a.save(r.Context(), out); err != nil {
		writeError(w, http.StatusInternalServerError, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// probeReason 把探活失败转成可读原因。
//
// 不透 Go 原始错误（形如 `Get "https://…": http: server gave HTTP response to HTTPS client`），
// 但保留「协议不匹配」「域名解析不了」这类**可行动**信息——部署者据此知道该改什么。
func probeReason(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "HTTP response to HTTPS client"):
		return "该地址不是 HTTPS 服务（端口可能只提供 HTTP）"
	case strings.Contains(msg, "no such host"):
		return "域名无法解析"
	case strings.Contains(msg, "certificate"):
		return "证书校验失败"
	case strings.Contains(msg, "connection refused"):
		return "连接被拒绝（该端口没有服务在监听）"
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(msg, "timeout"):
		return "连接超时"
	default:
		return "无法连接"
	}
}

// test 服务端探活单条地址：GET {url}/api/v1/health。
// 只验证服务端视角可达；APP 侧可达性由 APP 自己探活（网络环境不同）。
func (a *backupAddrAPI) test(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求体格式错误")
		return
	}
	normalized, err := validateBackupURL(req.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	reqHTTP, err := http.NewRequestWithContext(ctx, http.MethodGet, normalized+"/api/v1/health", nil)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "reason": "地址无法解析"})
		return
	}
	client := &http.Client{Timeout: 5 * time.Second}
	res, err := client.Do(reqHTTP)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "reason": probeReason(err)})
		return
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "reason": "健康检查返回 HTTP " + res.Status})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
