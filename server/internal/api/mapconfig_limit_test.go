package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// 上游瓦片体积上限：转发按 LimitReader 截断；上游声明 Content-Length 超限时直接 502。
func TestMapTileUpstreamSizeLimit(t *testing.T) {
	allowLoopbackTileUpstreams(t)
	big := make([]byte, maxTileUpstreamBytes+(1<<20))

	t.Run("chunked 截断", func(t *testing.T) {
		api, db := newTestMapAPI(t)
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(big) // 不设 Content-Length → chunked，走 LimitReader 截断
		}))
		defer upstream.Close()
		enabledPublicSource(t, db, upstream.URL+"/{z}/{x}/{y}.png")

		w := httptest.NewRecorder()
		api.tile(w, httptest.NewRequest(http.MethodGet, "/api/v1/map/tile?"+tileQuery(api, "z=4&x=13&y=5"), nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d，body=%s", w.Code, w.Body.String())
		}
		if got := w.Body.Len(); got != maxTileUpstreamBytes {
			t.Fatalf("响应体 = %d 字节，want 截断到 %d", got, maxTileUpstreamBytes)
		}
	})

	t.Run("声明超限 502", func(t *testing.T) {
		api, db := newTestMapAPI(t)
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Content-Length", strconv.Itoa(len(big)))
			_, _ = w.Write(big)
		}))
		defer upstream.Close()
		enabledPublicSource(t, db, upstream.URL+"/{z}/{x}/{y}.png")

		w := httptest.NewRecorder()
		api.tile(w, httptest.NewRequest(http.MethodGet, "/api/v1/map/tile?"+tileQuery(api, "z=4&x=13&y=5"), nil))
		if w.Code != http.StatusBadGateway {
			t.Fatalf("status = %d，want 502（上游声明体积超限）", w.Code)
		}
	})
}
