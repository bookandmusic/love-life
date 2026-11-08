package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/store"
)

// 瓦片模板的主机边界：服务端代取，不能指向回环/内网/链路本地/保留地址。
func TestValidatePublicTileURL(t *testing.T) {
	allowed := []string{
		"https://tile.openstreetmap.org/{z}/{x}/{y}.png",
		"http://tiles.example.com:8080/{z}/{x}/{y}.png",
		"https://[2606:4700:4700::1111]/{z}/{x}/{y}.png",
	}
	for _, tpl := range allowed {
		if err := validatePublicTileURL(tpl); err != nil {
			t.Errorf("%q 应允许，实得 %v", tpl, err)
		}
	}

	blocked := []struct{ name, tpl string }{
		{"回环 v4", "http://127.0.0.1/{z}/{x}/{y}.png"},
		{"内网 10/8", "http://10.1.2.3/{z}/{x}/{y}.png"},
		{"内网 172.16/12", "http://172.16.5.4/{z}/{x}/{y}.png"},
		{"内网 192.168/16", "http://192.168.1.10/{z}/{x}/{y}.png"},
		{"链路本地 169.254/16", "http://169.254.169.254/{z}/{x}/{y}.png"},
		{"未指定 0.0.0.0", "http://0.0.0.0/{z}/{x}/{y}.png"},
		{"回环 v6", "http://[::1]/{z}/{x}/{y}.png"},
		{"ULA fc00::/7", "http://[fd00::1]/{z}/{x}/{y}.png"},
		{"v4 映射写法", "http://[::ffff:127.0.0.1]/{z}/{x}/{y}.png"},
		{"localhost", "http://localhost:8080/{z}/{x}/{y}.png"},
		{"非 http(s)", "ftp://tiles.example.com/{z}/{x}/{y}.png"},
		{"缺 scheme", "//tiles.example.com/{z}/{x}/{y}.png"},
	}
	for _, c := range blocked {
		if err := validatePublicTileURL(c.tpl); err == nil {
			t.Errorf("%s：%q 应被拒绝", c.name, c.tpl)
		}
	}
}

// create 与 enable 都走同一校验：不合格模板 400 且带可读原因。
func TestMapSourceTileURLValidation(t *testing.T) {
	api, db := newTestMapAPI(t)
	mux := newMapMux(api)

	create := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/admin/map-config", strings.NewReader(body)))
		return w
	}

	for _, body := range []string{
		`{"type":"public","url_template":"http://10.0.0.5/{z}/{x}/{y}.png","max_zoom":15}`,
		`{"type":"public","url_template":"http://127.0.0.1:8080/{z}/{x}/{y}.png","max_zoom":15}`,
		`{"type":"public","url_template":"ftp://tiles.example.com/{z}/{x}/{y}.png","max_zoom":15}`,
	} {
		w := create(body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("create %s status = %d，want 400", body, w.Code)
			continue
		}
		if !strings.Contains(w.Body.String(), "地址模板") {
			t.Errorf("create %s 的 400 文案应说明模板问题，实得 %s", body, w.Body.String())
		}
	}

	// 公网 http(s) 模板允许创建
	w := create(`{"type":"public","url_template":"https://tile.openstreetmap.org/{z}/{x}/{y}.png","max_zoom":15}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("合法公网模板 status = %d，want 201，body=%s", w.Code, w.Body.String())
	}
	var created mapSourceItem
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create resp: %v", err)
	}

	// 已落库的内网模板（绕过 create 写入）不可启用
	unsafeID, err := db.CreateSource(t.Context(), &store.MapSource{
		Type: "public", URLTemplate: "http://192.168.1.2/{z}/{x}/{y}.png",
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/v1/admin/map-config/"+itoa(unsafeID)+"/enable", nil))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "内网") {
		t.Fatalf("启用内网模板 = %d %s，want 400 且说明内网", w.Code, w.Body.String())
	}

	// 合法模板可启用
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/v1/admin/map-config/"+itoa(created.ID)+"/enable", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("启用公网模板 status = %d，want 200，body=%s", w.Code, w.Body.String())
	}
}

// 拨号守卫：回环 / 内网 / 链路本地 / 保留段一律拒绝，公网地址放行。
func TestBlockedTileAddr(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "127.8.8.8", "10.0.0.1", "172.16.0.1", "192.168.1.1",
		"169.254.169.254", "0.0.0.0", "::1", "fd00::1", "::ffff:127.0.0.1",
	}
	for _, s := range blocked {
		if !blockedTileAddr(netip.MustParseAddr(s)) {
			t.Errorf("%s 应被判为内网/保留地址", s)
		}
	}
	for _, s := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if blockedTileAddr(netip.MustParseAddr(s)) {
			t.Errorf("%s 不应被判为禁用地址", s)
		}
	}
}

// 拨号守卫对 IP 字面量直接生效：不发起连接就先拒绝。
func TestTileDialRejectsInternal(t *testing.T) {
	if _, err := tileDialContext(context.Background(), "tcp", "127.0.0.1:80"); !errors.Is(err, errBlockedTileAddr) {
		t.Fatalf("拨号回环 err = %v，want errBlockedTileAddr", err)
	}
	if _, err := tileDialContext(context.Background(), "tcp", "169.254.169.254:80"); !errors.Is(err, errBlockedTileAddr) {
		t.Fatalf("拨号云元数据 err = %v，want errBlockedTileAddr", err)
	}
}

// 已落库的内网模板（绕过 create 校验）在取数阶段被拨号守卫拦住：上游一次都收不到。
func TestTileProxyBlocksInternalUpstream(t *testing.T) {
	api, db := newTestMapAPI(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("PNG-DATA"))
	}))
	defer srv.Close()
	enabledPublicSource(t, db, srv.URL+"/{z}/{x}/{y}.png")

	w := httptest.NewRecorder()
	api.tile(w, httptest.NewRequest(http.MethodGet, "/api/v1/map/tile?"+tileQuery(api, "z=4&x=13&y=5"), nil))
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d，want 502（内网目标被拒），body=%s", w.Code, w.Body.String())
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("上游收到 %d 次请求，want 0（应在拨号前拒绝）", n)
	}
}

// 探测接口和创建接口同口径：内网模板直接 400，不给「探测」留绕过口。
func TestProbeRejectsInternalTemplate(t *testing.T) {
	api, _ := newTestMapAPI(t)
	body := `{"type":"public","url_template":"http://127.0.0.1:9/{z}/{x}/{y}.png"}`
	w := httptest.NewRecorder()
	api.probe(w, httptest.NewRequest(http.MethodPost, "/api/v1/admin/map-config/probe", strings.NewReader(body)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("probe status = %d，want 400，body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "地址模板") {
		t.Fatalf("probe 400 文案应说明模板问题，实得 %s", w.Body.String())
	}
}
