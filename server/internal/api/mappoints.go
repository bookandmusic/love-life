package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/bookandmusic/love-life/server/internal/auth"
	"github.com/bookandmusic/love-life/server/internal/notify"
	"github.com/bookandmusic/love-life/server/internal/store"
)

// mapPointsAPI 地图点位（mock 契约：GET/POST /api/v1/map/points、DELETE /{id}、GET /cities；
// 同名复用返回 200 已有点位，新建 201；软删进回收站体系）。
type mapPointsAPI struct {
	db *store.DB
	dp *notify.Dispatcher
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
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
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
		writeError(w, http.StatusBadRequest, errBadRequest)
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
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
		return
	}
	if created {
		// 足迹更新：手动/AI 报点通知另一方（同名复用静默；动态带地点自动建点不发，该次发布已有 post_created）
		snapshot, _ := json.Marshal(map[string]any{
			"name": req.Name, "latitude": req.Lat, "longitude": req.Lng, "operator": claims.Username,
		})
		ids, _ := a.db.ListPartnerIDs(r.Context(), claims.UserID)
		a.dp.Dispatch(r.Context(), ids, "map_point_created", "map_point", p.ID, string(snapshot),
			"足迹更新", claims.Username+"把「"+req.Name+"」记到了地图上")
		writeJSON(w, http.StatusCreated, toMapPointItem(p))
		return
	}
	writeJSON(w, http.StatusOK, toMapPointItem(p)) // 同名复用
}

// remove DELETE /api/v1/map/points/{id}（软删，进回收站体系）
func (a *mapPointsAPI) remove(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidID)
		return
	}
	// 先取行：删除后拿不到名称与创建者（也用于把 404 判定提前）
	p, err := a.db.GetPoint(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "点位不存在")
		return
	}
	if err != nil {
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
		return
	}
	err = a.db.SoftDeletePoint(r.Context(), id, store.ActorTypeUser, claims.UserID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "点位不存在")
		return
	}
	if err != nil {
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
		return
	}
	// 删除对方创建的足迹 → 站内通知创建者（自删静默；内容删除仅站内）
	_ = a.db.NotifyContentDeleted(r.Context(), store.DeletedNotice{
		CreatorID: p.CreatedBy, DeleterID: claims.UserID, DeleterName: claims.Username,
		EventType: "map_point_deleted", TargetType: "map_point", TargetID: id,
		Extra: map[string]any{"name": p.Name},
	})
	w.WriteHeader(http.StatusNoContent)
}

// cities GET /api/v1/map/points/cities 城市列表（city 写时判定落库后的自然统计）
func (a *mapPointsAPI) cities(w http.ResponseWriter, r *http.Request) {
	list, err := a.db.PointCities(r.Context())
	if err != nil {
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
		return
	}
	if list == nil {
		list = []store.CityCount{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list, "total": len(list)})
}
