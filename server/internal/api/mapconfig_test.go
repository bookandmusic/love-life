package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/crypto"
	"github.com/bookandmusic/love-life/server/internal/store"
)

// 测试用内存库（临时文件落盘，跑完整迁移）
func newTestMapDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func newTestMapAPI(t *testing.T) (*mapConfigAPI, *store.DB) {
	t.Helper()
	db := newTestMapDB(t)
	return &mapConfigAPI{db: db, key: crypto.KeyFrom([]byte("test-secret-1234567890abcdef"))}, db
}

// newMapMux 按路由表注册地图 admin 接口（{id} PathValue 须经 ServeMux 解析）。
func newMapMux(api *mapConfigAPI) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/admin/map-config", api.list)
	mux.HandleFunc("POST /api/v1/admin/map-config", api.create)
	mux.HandleFunc("DELETE /api/v1/admin/map-config/{id}", api.remove)
	mux.HandleFunc("PUT /api/v1/admin/map-config/{id}/enable", api.enable)
	return mux
}

// 测试 store：建源/列表排序/启用互斥（部分唯一索引）/删除
func TestMapSourceStore(t *testing.T) {
	db := newTestMapDB(t)
	ctx := t.Context()

	id1, err := db.CreateSource(ctx, &store.MapSource{Type: "public", URLTemplate: "https://a.example.com/{z}/{x}/{y}.png"})
	if err != nil {
		t.Fatalf("create public: %v", err)
	}
	id2, err := db.CreateSource(ctx, &store.MapSource{Type: "tianditu", TiandituKey: "enc", TiandituLayer: ""})
	if err != nil {
		t.Fatalf("create tianditu: %v", err)
	}

	// 空层回退默认 vec_w
	s2, _ := db.GetSource(ctx, id2)
	if s2.TiandituLayer != "vec_w" {
		t.Errorf("layer = %q, want 默认 vec_w", s2.TiandituLayer)
	}

	// 启用第一个 → 使用中；再启用第二个 → 互斥（第一个自动停用）
	if err := db.EnableSource(ctx, id1); err != nil {
		t.Fatalf("enable 1: %v", err)
	}
	if s, _ := db.GetEnabledSource(ctx); s.ID != id1 {
		t.Fatalf("enabled = %d, want %d", s.ID, id1)
	}
	if err := db.EnableSource(ctx, id2); err != nil {
		t.Fatalf("enable 2: %v", err)
	}
	got, _ := db.GetEnabledSource(ctx)
	if got.ID != id2 {
		t.Errorf("enabled = %d, want %d（启用应互斥）", got.ID, id2)
	}
	s1, _ := db.GetSource(ctx, id1)
	if s1.Enabled {
		t.Error("旧源应已停用")
	}

	// 删除
	if err := db.DeleteSource(ctx, id1); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := db.GetSource(ctx, id1); err != store.ErrNotFound {
		t.Errorf("deleted get err = %v, want ErrNotFound", err)
	}
}

// 测试 admin API：创建/更新（密钥只写不读、留空保持）
func TestMapSourceAdminAPI(t *testing.T) {
	api, _ := newTestMapAPI(t)
	mux := newMapMux(api)

	// 创建天地图源（带密钥）→ 201；回读仅 has_key，不得含密钥
	body := `{"type":"tianditu","url_template":"","tianditu_key":"tk-secret-abc","tianditu_layer":"img_w"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/map-config", strings.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", w.Code, w.Body.String())
	}
	var item map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &item); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if item["type"] != "tianditu" || item["enabled"] != false {
		t.Errorf("item = %v", item)
	}
	if _, has := item["tianditu_key"]; has {
		t.Error("admin 回读不得下发 tianditu_key（密钥只写不读）")
	}
	if item["has_key"] != true {
		t.Errorf("has_key = %v, want true", item["has_key"])
	}
	if item["tianditu_layer"] != "img_w" {
		t.Errorf("layer = %v, want img_w", item["tianditu_layer"])
	}

	// 非法 type → 400
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/map-config", strings.NewReader(`{"type":"baidu"}`))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("bad type status = %d, want 400", w.Code)
	}

	// public 缺模板/模板无占位符 → 400（源不支持编辑，创建即须完整）
	for _, body := range []string{`{"type":"public"}`, `{"type":"public","url_template":"https://a.example.com/4/13/5.png"}`} {
		req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/map-config", strings.NewReader(body))
		w = httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("public bad template status = %d, want 400 (body=%s)", w.Code, body)
		}
	}

	// tianditu 缺密钥 → 400
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/map-config", strings.NewReader(`{"type":"tianditu","url_template":""}`))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("tianditu no key status = %d, want 400", w.Code)
	}

}

// 测试启用校验与删除保护
func TestMapSourceEnableAndDelete(t *testing.T) {
	api, db := newTestMapAPI(t)
	mux := newMapMux(api)
	ctx := t.Context()

	// public 无模板 → 不可启用
	_, _ = db.CreateSource(ctx, &store.MapSource{Type: "public"})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/map-config/1/enable", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("enable public(空模板) status = %d, want 400", w.Code)
	}

	// tianditu 无密钥 → 不可启用
	_, _ = db.CreateSource(ctx, &store.MapSource{Type: "tianditu"})
	req = httptest.NewRequest(http.MethodPut, "/api/v1/admin/map-config/2/enable", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("enable tianditu(无密钥) status = %d, want 400", w.Code)
	}

	// 带模板的 public → 启用成功
	idPub, _ := db.CreateSource(ctx, &store.MapSource{Type: "public", URLTemplate: "https://a.example.com/{z}/{x}/{y}.png"})
	req = httptest.NewRequest(http.MethodPut, "/api/v1/admin/map-config/3/enable", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("enable public status = %d, body=%s", w.Code, w.Body.String())
	}
	var item map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &item)
	if item["enabled"] != true {
		t.Errorf("enabled = %v, want true", item["enabled"])
	}

	// 使用中的源不可删除
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/map-config/3", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("delete enabled status = %d, want 400", w.Code)
	}

	// 未启用的源可删除
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/map-config/1", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Errorf("delete status = %d, want 204", w.Code)
	}

	// list：使用中的源在前
	s, _ := db.GetEnabledSource(ctx)
	if s.ID != idPub {
		t.Errorf("enabled = %d, want %d", s.ID, idPub)
	}
}

// 测试 APP 配置接口：下发启用源、不下发密钥；无启用源时 provider 为空
func TestMapConfigAppAPI(t *testing.T) {
	api, db := newTestMapAPI(t)
	ctx := t.Context()

	// 无启用源
	req := httptest.NewRequest(http.MethodGet, "/api/v1/map/config", nil)
	w := httptest.NewRecorder()
	api.config(w, req)
	var got map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got["provider"] != "" {
		t.Errorf("无启用源 provider = %v, want 空串", got["provider"])
	}

	// 启用天地图源（带密钥）→ provider=tianditu，无密钥泄露
	enc, _ := crypto.Encrypt(api.key, "secret-key")
	id, _ := db.CreateSource(ctx, &store.MapSource{Type: "tianditu", TiandituKey: enc})
	_ = db.EnableSource(ctx, id)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/map/config", nil)
	w = httptest.NewRecorder()
	api.config(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("config status = %d", w.Code)
	}
	got = nil
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got["provider"] != "tianditu" {
		t.Errorf("provider = %v", got["provider"])
	}
	if _, hasKey := got["tianditu_key"]; hasKey {
		t.Error("APP 配置接口不得下发 tianditu_key")
	}
	if got["tile_api"] != "/api/v1/map/tile" {
		t.Errorf("tile_api = %v", got["tile_api"])
	}
}

// 测试瓦片代理：非法坐标拒绝
func TestMapTileBadCoords(t *testing.T) {
	api, _ := newTestMapAPI(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/map/tile?z=-1&x=0&y=0", nil)
	w := httptest.NewRecorder()
	api.tile(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("bad z status = %d, want 400", w.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/map/tile?z=5&x=abc&y=0", nil)
	w2 := httptest.NewRecorder()
	api.tile(w2, req2)
	if w2.Code != http.StatusBadRequest {
		t.Errorf("bad x status = %d, want 400", w2.Code)
	}
}

// 测试瓦片代理：无启用源 / 未配置完整 → 502
func TestMapTileNoUpstream(t *testing.T) {
	api, db := newTestMapAPI(t)
	ctx := t.Context()

	// 空表：未启用任何源
	req := httptest.NewRequest(http.MethodGet, "/api/v1/map/tile?z=4&x=13&y=5", nil)
	w := httptest.NewRecorder()
	api.tile(w, req)
	if w.Code != http.StatusBadGateway {
		t.Errorf("no source status = %d, want 502", w.Code)
	}

	// public 但 URL 模板为空（直接落库启用，绕过 enable 校验）
	id, _ := db.CreateSource(ctx, &store.MapSource{Type: "public"})
	_ = db.EnableSource(ctx, id)
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/map/tile?z=4&x=13&y=5", nil)
	w2 := httptest.NewRecorder()
	api.tile(w2, req2)
	if w2.Code != http.StatusBadGateway {
		t.Errorf("empty template status = %d, want 502", w2.Code)
	}

	// tianditu 但无密钥
	id2, _ := db.CreateSource(ctx, &store.MapSource{Type: "tianditu"})
	_ = db.EnableSource(ctx, id2)
	req3 := httptest.NewRequest(http.MethodGet, "/api/v1/map/tile?z=4&x=13&y=5", nil)
	w3 := httptest.NewRecorder()
	api.tile(w3, req3)
	if w3.Code != http.StatusBadGateway {
		t.Errorf("no tianditu key status = %d, want 502", w3.Code)
	}
}

// 假上游：记录首个入站请求的 UA / Referer / Accept，便于断言转发行为。
type capturedReq struct {
	mu      sync.Mutex
	gotUA   string
	gotRef  string
	gotAcc  string
	gotPath string
}

func newCapturingUpstream(t *testing.T) (*httptest.Server, *capturedReq) {
	t.Helper()
	cap := &capturedReq{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.mu.Lock()
		cap.gotUA = r.Header.Get("User-Agent")
		cap.gotRef = r.Header.Get("Referer")
		cap.gotAcc = r.Header.Get("Accept")
		cap.gotPath = r.URL.Path
		cap.mu.Unlock()
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("PNG-DATA"))
	}))
	t.Cleanup(srv.Close)
	return srv, cap
}

// enabledPublicSource 建一个启用的 public 源指向假上游。
func enabledPublicSource(t *testing.T, db *store.DB, tpl string) {
	t.Helper()
	ctx := t.Context()
	id, err := db.CreateSource(ctx, &store.MapSource{Type: "public", URLTemplate: tpl})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	if err := db.EnableSource(ctx, id); err != nil {
		t.Fatalf("enable source: %v", err)
	}
}

// 断言：转发到上游时设了正确 UA、Accept，并转发入站 Referer。
func TestMapTileForwardsHeaders(t *testing.T) {
	api, db := newTestMapAPI(t)

	srv, cap := newCapturingUpstream(t)
	enabledPublicSource(t, db, srv.URL+"/{z}/{x}/{y}.png")

	in := httptest.NewRequest(http.MethodGet, "/api/v1/map/tile?z=4&x=13&y=5", nil)
	in.Header.Set("Referer", "http://10.10.2.2:1420/map")
	w := httptest.NewRecorder()
	api.tile(w, in)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if cap.gotUA == "" || strings.Contains(cap.gotUA, "Go-http-client") {
		t.Errorf("User-Agent = %q, must be non-default", cap.gotUA)
	}
	if !strings.Contains(cap.gotUA, "love-life-server") {
		t.Errorf("User-Agent = %q, want to contain love-life-server", cap.gotUA)
	}
	if cap.gotRef != "http://10.10.2.2:1420/map" {
		t.Errorf("Referer = %q, want forwarded inbound Referer", cap.gotRef)
	}
	if !strings.Contains(cap.gotAcc, "image/png") {
		t.Errorf("Accept = %q, want to declare image", cap.gotAcc)
	}
	if !strings.Contains(cap.gotPath, "/4/13/5.png") {
		t.Errorf("upstream path = %q, want /4/13/5.png", cap.gotPath)
	}
	if got := w.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("forwarded Content-Type = %q, want image/png", got)
	}
}

// 断言：入站无 Referer 时回退到项目仓库页而非 http://localhost。
func TestMapTileRefererFallback(t *testing.T) {
	api, db := newTestMapAPI(t)

	srv, cap := newCapturingUpstream(t)
	enabledPublicSource(t, db, srv.URL+"/{z}/{x}/{y}.png")

	// 入站不设 Referer
	in := httptest.NewRequest(http.MethodGet, "/api/v1/map/tile?z=4&x=13&y=5", nil)
	w := httptest.NewRecorder()
	api.tile(w, in)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if cap.gotRef != "https://github.com/bookandmusic/love-life" {
		t.Errorf("Referer fallback = %q, want https://github.com/bookandmusic/love-life", cap.gotRef)
	}
}

// 天地图分支请求头（实测 2026-09，见 DESIGN §4.7）：
// WAF 对非浏览器 UA 一律 418，即使 Referer 在 key 白名单内；Referer 必须透传页面真实来源。
func TestMapTileTiandituHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/map/tile?z=4&x=13&y=5", nil)
	out := httptest.NewRequest(http.MethodGet, "https://t0.tianditu.gov.cn/DataServer?T=vec_w&x=13&y=5&l=4&tk=k", nil)

	applyUpstreamHeaders(out, "tianditu", req.Header.Get("Referer"))
	if out.Header.Get("User-Agent") != mapTileBrowserUA {
		t.Errorf("tianditu UA = %q, want browser UA (WAF 418 otherwise)", out.Header.Get("User-Agent"))
	}
	if out.Header.Get("Referer") != "https://map.tianditu.gov.cn/" {
		t.Errorf("tianditu Referer fallback = %q, want https://map.tianditu.gov.cn/", out.Header.Get("Referer"))
	}

	applyUpstreamHeaders(out, "tianditu", "http://127.0.0.1:5173/map")
	if out.Header.Get("Referer") != "http://127.0.0.1:5173/map" {
		t.Errorf("tianditu Referer passthrough = %q, want inbound value", out.Header.Get("Referer"))
	}

	// public 分支保持原逻辑：应用标识 UA + 透传/回退 github
	applyUpstreamHeaders(out, "public", "")
	if out.Header.Get("User-Agent") != mapTileUserAgent {
		t.Errorf("public UA = %q, want %q", out.Header.Get("User-Agent"), mapTileUserAgent)
	}
	if out.Header.Get("Referer") != "https://github.com/bookandmusic/love-life" {
		t.Errorf("public Referer fallback = %q", out.Header.Get("Referer"))
	}
}
