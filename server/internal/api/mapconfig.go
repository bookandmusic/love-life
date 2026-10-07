package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bookandmusic/love-life/server/internal/crypto"
	"github.com/bookandmusic/love-life/server/internal/store"
)

// mapConfigAPI 地图瓦片源管理（多卡片，类型限 public/tianditu，至多一个使用中；
// 密钥加密存储、只写不读、永不下发 APP）。
type mapConfigAPI struct {
	db        *store.DB
	key       []byte
	sigSecret []byte // 瓦片短时签名密钥（= TokenManager 密钥，与媒体签名同源）
}

type mapSourceItem struct {
	ID            int64  `json:"id"`
	Type          string `json:"type"`
	URLTemplate   string `json:"url_template"`
	TiandituLayer string `json:"tianditu_layer"`
	HasKey        bool   `json:"has_key"`
	Enabled       bool   `json:"enabled"`
}

// item 密钥只写不读：回读仅含 has_key 标记，明文/密文均不下发。
func (a *mapConfigAPI) item(s *store.MapSource) mapSourceItem {
	hasKey := false
	if s.TiandituKey != "" {
		if _, err := crypto.Decrypt(a.key, s.TiandituKey); err == nil {
			hasKey = true
		}
	}
	return mapSourceItem{
		ID: s.ID, Type: s.Type, URLTemplate: s.URLTemplate,
		TiandituLayer: s.TiandituLayer, HasKey: hasKey, Enabled: s.Enabled,
	}
}

type mapSourceRequest struct {
	Type          string `json:"type"`
	URLTemplate   string `json:"url_template"`
	TiandituKey   string `json:"tianditu_key"`
	TiandituLayer string `json:"tianditu_layer"`
}

func validSourceType(t string) bool { return t == "public" || t == "tianditu" }

// list GET /api/v1/admin/map-config 全部瓦片源（使用中的在前）。
func (a *mapConfigAPI) list(w http.ResponseWriter, r *http.Request) {
	sources, err := a.db.ListSources(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	items := make([]mapSourceItem, 0, len(sources))
	for _, s := range sources {
		items = append(items, a.item(s))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// validTileTemplate 公开瓦片模板必须含全部 XYZ 占位，否则代理打不出有效瓦片。
func validTileTemplate(tpl string) bool {
	return strings.Contains(tpl, "{z}") && strings.Contains(tpl, "{x}") && strings.Contains(tpl, "{y}")
}

// create POST /api/v1/admin/map-config 新建瓦片源（默认不启用）。
// 源不支持编辑，创建时即要求配置完整：public 需含占位符的模板、tianditu 需密钥。
func (a *mapConfigAPI) create(w http.ResponseWriter, r *http.Request) {
	var req mapSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	if !validSourceType(req.Type) {
		writeError(w, http.StatusBadRequest, "type 必须为 public 或 tianditu")
		return
	}
	if req.Type == "public" && !validTileTemplate(req.URLTemplate) {
		writeError(w, http.StatusBadRequest, "公开瓦片源必须提供含 {z}/{x}/{y} 占位的地址模板")
		return
	}
	if req.Type == "tianditu" && req.TiandituKey == "" {
		writeError(w, http.StatusBadRequest, "天地图源必须提供 tk 密钥")
		return
	}
	enc := ""
	if req.TiandituKey != "" {
		var err error
		if enc, err = crypto.Encrypt(a.key, req.TiandituKey); err != nil {
			writeError(w, http.StatusInternalServerError, "加密失败")
			return
		}
	}
	id, err := a.db.CreateSource(r.Context(), &store.MapSource{
		Type: req.Type, URLTemplate: req.URLTemplate,
		TiandituKey: enc, TiandituLayer: req.TiandituLayer,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	s, err := a.db.GetSource(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusCreated, a.item(s))
}

// remove DELETE /api/v1/admin/map-config/{id}（使用中的源不可删，避免地图突然无底图）。
func (a *mapConfigAPI) remove(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidID)
		return
	}
	s, err := a.db.GetSource(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "瓦片源不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	if s.Enabled {
		writeError(w, http.StatusBadRequest, "使用中的源不可删除，请先切换其他源")
		return
	}
	if err := a.db.DeleteSource(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// enable PUT /api/v1/admin/map-config/{id}/enable 启用（至多一个使用中）。
// 启用校验：public 需模板非空、tianditu 需已配密钥——避免切到不可用的源。
func (a *mapConfigAPI) enable(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidID)
		return
	}
	s, err := a.db.GetSource(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "瓦片源不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	switch s.Type {
	case "public":
		if !validTileTemplate(s.URLTemplate) {
			writeError(w, http.StatusBadRequest, "启用前需先填写含 {z}/{x}/{y} 占位的地址模板")
			return
		}
	case "tianditu":
		if s.TiandituKey == "" {
			writeError(w, http.StatusBadRequest, "启用前需先配置天地图密钥")
			return
		}
		if _, err := crypto.Decrypt(a.key, s.TiandituKey); err != nil {
			writeError(w, http.StatusBadRequest, "天地图密钥解密失败，请重新配置")
			return
		}
	}
	if err := a.db.EnableSource(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	s, err = a.db.GetSource(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusOK, a.item(s))
}

// tileGrantTTL 瓦片短时签名有效期：一次签名覆盖全部坐标，APP 定时重取。
const tileGrantTTL = 30 * time.Minute

// tileSig 空间级签名：只绑 exp、不绑 z/x/y。瓦片坐标由客户端按视口实时算出，
// 逐张签名会让拖动视口不断触发重签，故一张签名覆盖全部瓦片。
func (a *mapConfigAPI) tileSig(exp int64) string {
	mac := hmac.New(sha256.New, a.sigSecret)
	fmt.Fprintf(mac, "map:tile:%d", exp)
	return hex.EncodeToString(mac.Sum(nil))
}

// tileGrant 签发当前时刻的 (exp, sig)。
func (a *mapConfigAPI) tileGrant() (int64, string) {
	exp := time.Now().Add(tileGrantTTL).Unix()
	return exp, a.tileSig(exp)
}

// verifyTileGrant 校验 exp 未过期且签名一致。
func (a *mapConfigAPI) verifyTileGrant(expStr, sig string) bool {
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	return hmac.Equal([]byte(a.tileSig(exp)), []byte(sig))
}

// config GET /api/v1/map/config 下发「使用中」的源与瓦片短时签名给 APP
// （provider/tile_api/url_template? 契约不变，新增 exp/sig）。
func (a *mapConfigAPI) config(w http.ResponseWriter, r *http.Request) {
	s, err := a.db.GetEnabledSource(r.Context())
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		// DB 故障与「未启用任何源」必须区分：前者是服务器错误，后者才是正常空态
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	exp, sig := a.tileGrant()
	resp := map[string]any{
		"provider": "",
		"tile_api": "/api/v1/map/tile",
		"exp":      exp,
		"sig":      sig,
	}
	if s != nil {
		resp["provider"] = s.Type
		if s.Type == "public" && s.URLTemplate != "" {
			resp["url_template"] = s.URLTemplate // 公开源可下发模板（无密钥）；APP 也可直连
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

var mapTileHTTP = &http.Client{Timeout: 15 * time.Second}

// mapTileUserAgent 转发到公开瓦片源时使用的 User-Agent。
// 必须显式标识应用而非使用 Go 默认的 "Go-http-client/1.1"——
// OSM 等公开源会按 UA 黑名单直接 403。自部署者若需自定义可改此常量。
var mapTileUserAgent = "love-life-server/0.1 (+https://github.com/bookandmusic/love-life; self-hosted)"

// mapTileBrowserUA 天地图专用浏览器 UA（实测 2026-09）：
// 天地图 WAF 对非浏览器 UA 一律 418「疑似攻击」拦截（即使 Referer 在 key 白名单内）。
var mapTileBrowserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

// applyUpstreamHeaders 设置转发上游的请求头。
//   - public 源：应用标识 UA；Referer 透传入站值（自部署页面的真实来源），缺时回退项目仓库页，
//     避免 OSM 等公开源因默认 Go UA 或缺失 Referer 403。
//   - tianditu 源：必须浏览器 UA（WAF 硬条件）；Referer 同样透传入站——浏览器端 tk 在
//     天地图控制台绑定了域名白名单，只有白名单内页面发来的真实 Referer 才放行
//     （不在白名单 418、完全无 Referer 403 code 301012），缺时回退天地图官方域。
func applyUpstreamHeaders(req *http.Request, provider, inboundReferer string) {
	req.Header.Set("Accept", "image/png,image/*;q=0.8,*/*;q=0.5")
	switch provider {
	case "tianditu":
		req.Header.Set("User-Agent", mapTileBrowserUA)
		if inboundReferer != "" {
			req.Header.Set("Referer", inboundReferer)
		} else {
			req.Header.Set("Referer", "https://map.tianditu.gov.cn/")
		}
	default:
		req.Header.Set("User-Agent", mapTileUserAgent)
		if inboundReferer != "" {
			req.Header.Set("Referer", inboundReferer)
		} else {
			req.Header.Set("Referer", "https://github.com/bookandmusic/love-life")
		}
	}
}

// tile GET /api/v1/map/tile?z=&x=&y=&exp=&sig= 服务端瓦片代理：按「使用中」的源转发，返回瓦片图。
// 鉴权走短时签名（img 标签无法带 header）：/map/config 下发 exp+sig，过期即 401。
func (a *mapConfigAPI) tile(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if !a.verifyTileGrant(q.Get("exp"), q.Get("sig")) {
		writeError(w, http.StatusUnauthorized, "签名无效或已过期")
		return
	}
	z, err1 := strconv.Atoi(q.Get("z"))
	x, err2 := strconv.Atoi(q.Get("x"))
	y, err3 := strconv.Atoi(q.Get("y"))
	if err1 != nil || err2 != nil || err3 != nil || z < 0 || z > 20 || x < 0 || y < 0 {
		writeError(w, http.StatusBadRequest, "无效的瓦片坐标")
		return
	}
	s, err := a.db.GetEnabledSource(r.Context())
	if errors.Is(err, store.ErrNotFound) {
		// 未配源与上游抓取失败必须区分：前者是服务不可用（需 admin 配置），后者才是 502 网关失败
		writeError(w, http.StatusServiceUnavailable, "未启用任何瓦片源，请管理员在后台配置")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	upstream := ""
	switch s.Type {
	case "tianditu":
		key, derr := crypto.Decrypt(a.key, s.TiandituKey)
		if derr != nil || key == "" {
			writeError(w, http.StatusBadGateway, "天地图密钥未配置")
			return
		}
		layer := s.TiandituLayer
		if layer == "" {
			layer = "vec_w"
		}
		upstream = "https://t0.tianditu.gov.cn/DataServer?T=" + url.QueryEscape(layer) +
			"&tk=" + url.QueryEscape(key) +
			"&x=" + strconv.Itoa(x) + "&y=" + strconv.Itoa(y) + "&l=" + strconv.Itoa(z)
	default:
		tpl := s.URLTemplate
		if tpl == "" {
			writeError(w, http.StatusBadGateway, "公开瓦片地址未配置")
			return
		}
		upstream = strings.ReplaceAll(tpl, "{z}", strconv.Itoa(z))
		upstream = strings.ReplaceAll(upstream, "{x}", strconv.Itoa(x))
		upstream = strings.ReplaceAll(upstream, "{y}", strconv.Itoa(y))
	}

	// 转发瓦片：按源设置上游请求头（实测见 「转发请求头」）
	// 用请求 context：客户端断开时取消上游请求
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstream, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	applyUpstreamHeaders(req, s.Type, r.Header.Get("Referer"))
	resp, err := mapTileHTTP.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "瓦片服务不可达")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		writeError(w, http.StatusBadGateway, "瓦片服务返回 "+strconv.Itoa(resp.StatusCode))
		return
	}
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.Header().Set("Cache-Control", "public, max-age=86400") // 瓦片可缓存 1 天
	_, _ = io.Copy(w, resp.Body)
}
