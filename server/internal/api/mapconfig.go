package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"math"
	"net"
	"net/http"
	"net/netip"
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
	MaxZoom       int    `json:"max_zoom"`
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
		TiandituLayer: s.TiandituLayer, MaxZoom: s.MaxZoom, HasKey: hasKey, Enabled: s.Enabled,
	}
}

type mapSourceRequest struct {
	Type          string `json:"type"`
	URLTemplate   string `json:"url_template"`
	TiandituKey   string `json:"tianditu_key"`
	TiandituLayer string `json:"tianditu_layer"`
	MaxZoom       int    `json:"max_zoom"` // 0 = 按类型默认（tianditu 14 / public 17）
}

func validSourceType(t string) bool { return t == "public" || t == "tianditu" }

// list GET /api/v1/admin/map-config 全部瓦片源（使用中的在前）。
func (a *mapConfigAPI) list(w http.ResponseWriter, r *http.Request) {
	sources, err := a.db.ListSources(r.Context())
	if err != nil {
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
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

// blockedTileHostPrefixes 代取瓦片时禁止指向的主机段：回环 / 内网 / 链路本地 / 保留。
// 瓦片模板由服务端代取，指向这些地址等于把服务端当内网探针。
var blockedTileHostPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/32"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fc00::/7"),
}

// errBlockedTileAddr 拨号目标落在回环/内网/链路本地/保留段。
// 瓦片模板与探测地址都来自可配置输入：配置时校验主机字符串不够——域名解析结果与 302
// 跳转目标都可能指向内网，必须按真正要连的 IP 再判一次。
var errBlockedTileAddr = errors.New("拒绝连接内网/保留地址")

// blockedTileAddr 判定一个已解析地址是否落在禁用网段（IPv4 映射写法按解映射后判定）。
func blockedTileAddr(ip netip.Addr) bool {
	ip = ip.Unmap()
	for _, p := range blockedTileHostPrefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

var tileDialer = &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}

// tileDialContext 先解析并校验全部候选 IP，再用解析结果直连：不重新解析，
// 避免「校验通过后 DNS 换成内网地址」的窗口。
func tileDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if ip, perr := netip.ParseAddr(host); perr == nil {
		if blockedTileAddr(ip) {
			return nil, fmt.Errorf("%w（%s）", errBlockedTileAddr, ip)
		}
		return tileDialer.DialContext(ctx, network, addr)
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		if blockedTileAddr(ip) {
			return nil, fmt.Errorf("%w（%s → %s）", errBlockedTileAddr, host, ip)
		}
	}
	var lastErr error
	for _, ip := range ips {
		conn, derr := tileDialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if derr == nil {
			return conn, nil
		}
		lastErr = derr
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("域名 %s 没有可用地址", host)
	}
	return nil, lastErr
}

// newTileHTTPClient 瓦片代理与探测共用：拨号前按解析 IP 复检内网，302 跳转目标同样受控。
func newTileHTTPClient(timeout time.Duration) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = tileDialContext
	return &http.Client{Timeout: timeout, Transport: tr}
}

// validatePublicTileURL 公开瓦片模板的取数边界：只允许 http/https 的公网主机。
// IPv4 映射写法（::ffff:127.0.0.1）按解映射后的地址判定。
// 本函数只拦配置时就能看出的目标；域名解析结果与 302 跳转目标由 tileDialContext 在拨号前复检。
func validatePublicTileURL(tpl string) error {
	u, err := url.Parse(strings.TrimSpace(tpl))
	if err != nil {
		return errors.New("地址模板不合法")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("地址模板必须以 http:// 或 https:// 开头")
	}
	host := u.Hostname()
	if host == "" {
		return errors.New("地址模板缺少主机名")
	}
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
		return errors.New("地址模板不得指向本机")
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return nil
	}
	ip = ip.Unmap()
	for _, p := range blockedTileHostPrefixes {
		if p.Contains(ip) {
			return fmt.Errorf("地址模板不得指向内网/保留地址（%s）", ip)
		}
	}
	return nil
}

// probe POST /api/v1/admin/map-config/probe 探测源的数据物理上限（不落库）。
// 表单流程：用户填完配置点「探测」→ 返回最高有真实数据的层级；探测值随创建请求提交，
// create 强校验必须已探测——杜绝拍脑袋配置导致顶档错乱/糊图。
func (a *mapConfigAPI) probe(w http.ResponseWriter, r *http.Request) {
	var req mapSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	if !validSourceType(req.Type) {
		writeError(w, http.StatusBadRequest, "type 必须为 public 或 tianditu")
		return
	}
	if req.Type == "public" {
		if !validTileTemplate(req.URLTemplate) {
			writeError(w, http.StatusBadRequest, "公开瓦片源必须提供含 {z}/{x}/{y} 占位的地址模板")
			return
		}
		if err := validatePublicTileURL(req.URLTemplate); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if req.Type == "tianditu" && req.TiandituKey == "" {
		writeError(w, http.StatusBadRequest, "天地图源必须提供 tk 密钥")
		return
	}
	maxZoom := probeDataMax(r, &req)
	if maxZoom <= 0 {
		writeError(w, http.StatusBadGateway, "探测失败：所有层级均无有效瓦片（检查密钥/模板/网络）")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"max_zoom": maxZoom})
}

// probeTileCities 探测坐标（北京/上海中心）：大城市数据覆盖最全，避免偏远区域误判为「该层无数据」。
var probeTileCities = [2][2]float64{{39.916, 116.397}, {31.236, 121.492}}

// probeProbeHTTP 探测专用短超时 client：正常瓦片 <1s 到达；无数据层每张要等满超时，
// 15s 默认会让 z20→3 的下探最坏花数分钟。2s 足够判「有/没有」，慢网络下宁可漏探到（返回更低层，
// 探测值偏保守只是少放大几级，不会错乱）。
var probeHTTP = newTileHTTPClient(2 * time.Second)

// probeDataMax 从 z20 向下探测首个「有真实瓦片」的最高层。
// 判真阈值 2KB：实测空底图 ~767B、代理占位 ~1.6KB，真实瓦片 ≥3KB；两层坐标任一成功即算该层有数据。
func probeDataMax(r *http.Request, req *mapSourceRequest) int {
	var key string
	if req.Type == "tianditu" {
		key = req.TiandituKey
	}
	layer := req.TiandituLayer
	if layer == "" {
		layer = "vec_w"
	}
	for z := 20; z >= 3; z-- {
		for _, c := range probeTileCities {
			lat, lon := c[0], c[1]
			n := 1 << z
			x := int((lon + 180.0) / 360.0 * float64(n))
			la := float64(lat) * math.Pi / 180
			y := int((1 - math.Log(math.Tan(la)+1/math.Cos(la))/math.Pi) / 2 * float64(n))
			var upstream string
			if req.Type == "tianditu" {
				upstream = tiandituTileURL(layer, key, x, y, z)
			} else {
				upstream = strings.ReplaceAll(req.URLTemplate, "{z}", strconv.Itoa(z))
				upstream = strings.ReplaceAll(upstream, "{x}", strconv.Itoa(x))
				upstream = strings.ReplaceAll(upstream, "{y}", strconv.Itoa(y))
			}
			if size := fetchUpstreamSize(r, upstream, req.Type); size >= 2048 {
				return z
			}
		}
	}
	return 0
}

// fetchUpstreamSize 拉上游瓦片返回字节数（失败 0）——探测专用，不落缓存。
func fetchUpstreamSize(r *http.Request, upstream, provider string) int {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstream, nil)
	if err != nil {
		return 0
	}
	applyUpstreamHeaders(req, provider, r.Header.Get("Referer"))
	res, err := probeHTTP.Do(req)
	if err != nil {
		return 0
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return 0
	}
	n, _ := io.Copy(io.Discard, io.LimitReader(res.Body, maxTileUpstreamBytes))
	return int(n)
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
	if req.Type == "public" {
		if !validTileTemplate(req.URLTemplate) {
			writeError(w, http.StatusBadRequest, "公开瓦片源必须提供含 {z}/{x}/{y} 占位的地址模板")
			return
		}
		if err := validatePublicTileURL(req.URLTemplate); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if req.Type == "tianditu" && req.TiandituKey == "" {
		writeError(w, http.StatusBadRequest, "天地图源必须提供 tk 密钥")
		return
	}
	// 探测门禁：max_zoom 必须是表单「探测」拿到的值（3-20）；未探测/越界一律拒绝
	if req.MaxZoom < 3 || req.MaxZoom > 20 {
		writeError(w, http.StatusBadRequest, "请先探测最大层级（探测值随创建提交）")
		return
	}
	enc := ""
	if req.TiandituKey != "" {
		var err error
		if enc, err = crypto.Encrypt(a.key, req.TiandituKey); err != nil {
			writeErrorf(w, r, http.StatusInternalServerError, "加密失败", err)
			return
		}
	}
	id, err := a.db.CreateSource(r.Context(), &store.MapSource{
		Type: req.Type, URLTemplate: req.URLTemplate, MaxZoom: req.MaxZoom,
		TiandituKey: enc, TiandituLayer: req.TiandituLayer,
	})
	if err != nil {
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
		return
	}
	s, err := a.db.GetSource(r.Context(), id)
	if err != nil {
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
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
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
		return
	}
	if s.Enabled {
		writeError(w, http.StatusBadRequest, "使用中的源不可删除，请先切换其他源")
		return
	}
	if err := a.db.DeleteSource(r.Context(), id); err != nil {
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
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
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
		return
	}
	switch s.Type {
	case "public":
		if !validTileTemplate(s.URLTemplate) {
			writeError(w, http.StatusBadRequest, "启用前需先填写含 {z}/{x}/{y} 占位的地址模板")
			return
		}
		if err := validatePublicTileURL(s.URLTemplate); err != nil {
			writeError(w, http.StatusBadRequest, "启用前校验失败："+err.Error())
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
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
		return
	}
	s, err = a.db.GetSource(r.Context(), id)
	if err != nil {
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
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
// （provider/tile_api 契约不变，新增 exp/sig）。
func (a *mapConfigAPI) config(w http.ResponseWriter, r *http.Request) {
	s, err := a.db.GetEnabledSource(r.Context())
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		// DB 故障与「未启用任何源」必须区分：前者是服务器错误，后者才是正常空态
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
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
		resp["max_zoom"] = s.MaxZoom
	}
	writeJSON(w, http.StatusOK, resp)
}

var mapTileHTTP = newTileHTTPClient(15 * time.Second)

// maxTileUpstreamBytes 单张瓦片的读取上限：正常瓦片几十 KB，8 MB 足够覆盖高分辨率 PNG，
// 同时挡住异常上游把服务端与客户端拖进无界读取。
const maxTileUpstreamBytes = 8 << 20

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
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
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
		upstream = tiandituTileURL(layer, key, x, y, z)
		// 注记层（路名/地标/POI 文字）与底图配对：vec_w→cva_w、img_w→cia_w、ter_w→cta_w。
		// 天地图的标注全部在注记层——单层底图就是「没有名字的道路」，三模式都不对的原因。
		// 注记层拉取/解码失败时静默回退纯底图（注记缺失可接受，底图不可用不可接受）。
		if anno, ok := tiandituAnnoLayer(layer); ok {
			if base := fetchUpstreamTile(r, upstream, "tianditu"); base != nil {
				if overlay := fetchUpstreamTile(r, tiandituTileURL(anno, key, x, y, z), "tianditu"); overlay != nil {
					if merged, merr := mergeTiles(base, overlay); merr == nil {
						base = merged
					}
				}
				serveTileImage(w, base)
				return
			}
			// 底图都没拉到 → 走下方统一的 502 路径
		}
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
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
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
	if resp.ContentLength > maxTileUpstreamBytes {
		writeError(w, http.StatusBadGateway, "瓦片体积超过上限")
		return
	}
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.Header().Set("Cache-Control", "public, max-age=86400") // 瓦片可缓存 1 天
	_, _ = io.Copy(w, io.LimitReader(resp.Body, maxTileUpstreamBytes))
}

// tiandituAnnoLayer 底图层 → 注记层配对（天地图官方双层用法；ter_w 无 cta 配对时返回 false 走纯底图）。
func tiandituAnnoLayer(base string) (string, bool) {
	switch base {
	case "vec_w":
		return "cva_w", true
	case "img_w":
		return "cia_w", true
	case "ter_w":
		return "cta_w", true
	default:
		return "", false
	}
}

// tiandituTileURL 天地图 DataServer 瓦片地址。
func tiandituTileURL(layer, key string, x, y, z int) string {
	return "https://t0.tianditu.gov.cn/DataServer?T=" + url.QueryEscape(layer) +
		"&tk=" + url.QueryEscape(key) +
		"&x=" + strconv.Itoa(x) + "&y=" + strconv.Itoa(y) + "&l=" + strconv.Itoa(z)
}

// fetchUpstreamTile 按源类型请求上游瓦片并解码为 image；失败返回 nil（调用方决定回退策略）。
func fetchUpstreamTile(r *http.Request, upstream, provider string) image.Image {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstream, nil)
	if err != nil {
		return nil
	}
	applyUpstreamHeaders(req, provider, r.Header.Get("Referer"))
	res, err := mapTileHTTP.Do(req)
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil
	}
	img, _, err := image.Decode(io.LimitReader(res.Body, maxTileUpstreamBytes))
	if err != nil {
		return nil
	}
	return img
}

// mergeTiles 底图 + 注记层合成为一张 PNG（尺寸不一致时以底图为准，注记拉伸对齐——同源同坐标瓦片理论等尺寸）。
func mergeTiles(base, overlay image.Image) (image.Image, error) {
	b := base.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), base, b.Min, draw.Src)
	if ob := overlay.Bounds(); ob.Dx() == b.Dx() && ob.Dy() == b.Dy() {
		// 正常路径：同坐标瓦片等尺寸，直接叠加
		draw.Draw(dst, dst.Bounds(), overlay, ob.Min, draw.Over)
	} else {
		// ponytail: 异常尺寸手写最近邻对齐（同源瓦片实际不会走到这；不引 x/image 依赖）
		for py := 0; py < b.Dy(); py++ {
			sy := ob.Min.Y + py*ob.Dy()/b.Dy()
			for px := 0; px < b.Dx(); px++ {
				sx := ob.Min.X + px*ob.Dx()/b.Dx()
				ro, go_, bo, ao := overlay.At(sx, sy).RGBA()
				if ao == 0 {
					continue
				}
				rb, gb, bb, _ := dst.At(px, py).RGBA()
				na := uint32(255 - ao>>24)
				dst.Set(px, py, color.RGBA{
					uint8((ro*ao/65535 + rb*na/255) >> 8),
					uint8((go_*ao/65535 + gb*na/255) >> 8),
					uint8((bo*ao/65535 + bb*na/255) >> 8),
					255,
				})
			}
		}
	}
	return dst, nil
}

// serveTileImage 合成结果编码 PNG 下发（天地图瓦片为 PNG 透明注记，合成后仍无损）。
func serveTileImage(w http.ResponseWriter, img image.Image) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		writeError(w, http.StatusBadGateway, "瓦片合成失败")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(buf.Bytes())
}
