package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/bookandmusic/love-life/server/internal/auth"
	"github.com/bookandmusic/love-life/server/internal/store"
)

// mapPointsAPI 地图点位（mock 契约：GET/POST /api/v1/map/points、DELETE /{id}、GET /cities；
// 同名复用返回 200 已有点位，新建 201；软删进回收站体系）。
type mapPointsAPI struct {
	db *store.DB
}

type mapPointRequest struct {
	Name        string  `json:"name"`
	Lat         float64 `json:"lat"`
	Lng         float64 `json:"lng"`
	VisitDate   string  `json:"visit_date"`
	Description string  `json:"description"`
}

type mapPointItem struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Lat         float64 `json:"lat"`
	Lng         float64 `json:"lng"`
	VisitDate   string  `json:"visit_date"`
	Description string  `json:"description"`
	Source      string  `json:"source"`
	CreatedAt   string  `json:"created_at"`
	CreatedBy   int64   `json:"created_by"`
}

func toMapPointItem(p *store.MapPoint) mapPointItem {
	return mapPointItem{
		ID: p.ID, Name: p.Name, Lat: p.Lat, Lng: p.Lng,
		VisitDate: p.VisitDate.String, Description: p.Description, Source: p.Source,
		CreatedAt: p.CreatedAt, CreatedBy: p.CreatedBy,
	}
}

// list GET /api/v1/map/points
func (a *mapPointsAPI) list(w http.ResponseWriter, r *http.Request) {
	list, err := a.db.ListPoints(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	items := make([]mapPointItem, 0, len(list))
	for _, p := range list {
		items = append(items, toMapPointItem(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

// create POST /api/v1/map/points
func (a *mapPointsAPI) create(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	var req mapPointRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name 不能为空")
		return
	}
	// source 由服务端按通道判定：REST 手动创建恒为 manual（AI 走 MCP 工具、动态同步为 post）
	p, created, err := a.db.FindOrCreatePoint(r.Context(), store.PointInput{
		Name: req.Name, Lat: req.Lat, Lng: req.Lng,
		VisitDate: req.VisitDate, Description: req.Description,
		Source: "manual", UserID: claims.UserID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	if created {
		writeJSON(w, http.StatusCreated, toMapPointItem(p))
		return
	}
	writeJSON(w, http.StatusOK, toMapPointItem(p)) // 同名复用
}

// remove DELETE /api/v1/map/points/{id}（软删，进回收站体系）
func (a *mapPointsAPI) remove(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效 ID")
		return
	}
	err = a.db.SoftDeletePoint(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "点位不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// cities GET /api/v1/map/points/cities 城市列表（city 写时判定落库后的自然统计）
func (a *mapPointsAPI) cities(w http.ResponseWriter, r *http.Request) {
	list, err := a.db.PointCities(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	if list == nil {
		list = []store.CityCount{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list, "total": len(list)})
}
