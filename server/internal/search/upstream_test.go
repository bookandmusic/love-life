package search

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// newFailingUpstream 固定返回 500 的假上游。
func newFailingUpstream(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}
