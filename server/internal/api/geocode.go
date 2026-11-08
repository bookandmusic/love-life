package api

import (
	"net/http"

	"github.com/bookandmusic/love-life/server/internal/geo"
)

// geocode GET /api/v1/geocode?q= 地名 → 候选列表（内置离线表，无第三方 POI 依赖——）；
// 返回 {candidates:[{name,city,lat,lng,display}]}，未匹配为空列表。
func geocode(w http.ResponseWriter, r *http.Request) {
	hits := geo.SearchPlaces(r.URL.Query().Get("q"), 8)
	if hits == nil {
		hits = []geo.Place{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"candidates": hits})
}
