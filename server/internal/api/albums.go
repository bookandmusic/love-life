package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/bookandmusic/love-life/server/internal/auth"
	"github.com/bookandmusic/love-life/server/internal/media"
	"github.com/bookandmusic/love-life/server/internal/notify"
	"github.com/bookandmusic/love-life/server/internal/store"
)

type albumsAPI struct {
	db *store.DB
	mp *media.Pipeline
	dp *notify.Dispatcher
}

type albumResponse struct {
	ID          int64  `json:"id"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MonthKey    string `json:"month_key,omitempty"`
	IsHidden    bool   `json:"is_hidden"`
	ItemCount   int    `json:"item_count"`
	CoverURL    string `json:"cover_url,omitempty"`
	// CreatedBy 供 APP 判断「删的是不是对方创建的」（决定是否提示会通知对方）；
	// 用户 ID 从 1 起，0 表示月度相册（无创建者）→ omitempty 省略该字段。
	CreatedBy int64 `json:"created_by,omitempty"`
}

func (a *albumsAPI) toResponse(ctx context.Context, al *store.Album) albumResponse {
	resp := albumResponse{
		ID: al.ID, Type: al.Type, Name: al.Name, Description: al.Description.String,
		MonthKey: al.MonthKey.String, IsHidden: al.IsHidden, ItemCount: al.ItemCount,
		CreatedBy: al.CreatedBy.Int64, // Valid=false（月度相册）时为 0 → omitempty 省略
	}
	if al.CoverMediaID.Valid {
		resp.CoverURL = a.mp.SignedURL(al.CoverMediaID.Int64, "thumb")
	} else if items, err := a.db.ListAlbumMedia(ctx, al.ID); err == nil && len(items) > 0 {
		resp.CoverURL = a.mp.SignedURL(items[0].ID, "thumb")
	}
	return resp
}

func (a *albumsAPI) list(w http.ResponseWriter, r *http.Request) {
	includeHidden := r.URL.Query().Get("include_hidden") == "1"
	list, err := a.db.ListAlbums(r.Context(), includeHidden)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	items := make([]albumResponse, 0, len(list))
	for _, al := range list {
		items = append(items, a.toResponse(r.Context(), al))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *albumsAPI) create(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "相册名称不能为空")
		return
	}
	id, err := a.db.CreateAlbum(r.Context(), "manual", req.Name, req.Description, "", claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}

	// 相册上新：站内通知另一方（自建静默，admin 不在情侣里）+ 按订阅外推（需求 §4.8）
	snapshot, _ := json.Marshal(map[string]any{"name": req.Name, "operator": claims.Username})
	ids, _ := a.db.ListPartnerIDs(r.Context(), claims.UserID)
	a.dp.Dispatch(r.Context(), ids, "album_created", "album", id, string(snapshot),
		"相册上新", claims.Username+" 创建了相册「"+req.Name+"」")

	// 返回完整相册对象（R5：补齐返回值，对齐契约）
	al, err := a.db.GetAlbum(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusCreated, map[string]any{"id": id})
		return
	}
	writeJSON(w, http.StatusCreated, a.toResponse(r.Context(), al))
}

func (a *albumsAPI) get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidID)
		return
	}
	al, err := a.db.GetAlbum(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "相册不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	var medias []*store.MediaRow
	if al.Type == "monthly" {
		// 月度相册虚拟口径（D4）：该月未归属手动相册的媒体
		medias, err = a.db.ListMonthlyMedia(r.Context(), al.MonthKey.String)
	} else {
		medias, err = a.db.ListAlbumMedia(r.Context(), id)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	items, err := a.mediaItemsOut(r.Context(), medias)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	resp := a.toResponse(r.Context(), al)
	resp.ItemCount = len(items)
	writeJSON(w, http.StatusOK, map[string]any{"album": resp, "media": items})
}

// mediaItem mock timelineItemOut 契约：mediaOut + post_id/taken_at/author_id/album_id/album_name；
// D5 归属字段供 APP 选图器区分「已在本相册/已归属他相册」；duration 仅音频（DESIGN 12）。
type mediaItem struct {
	ID        int64    `json:"id"`
	Kind      string   `json:"kind"`
	Width     int      `json:"width"`
	Height    int      `json:"height"`
	URL       string   `json:"url"`
	ThumbURL  string   `json:"thumb_url"`
	PostID    int64    `json:"post_id"`
	TakenAt   string   `json:"taken_at"`
	AuthorID  int64    `json:"author_id"`
	Duration  *float64 `json:"duration,omitempty"`
	AlbumID   *int64   `json:"album_id"`
	AlbumName *string  `json:"album_name,omitempty"`
}

// mediaItemsOut 统一媒体项输出（timeline 与相册详情共用，避免 Duplicated Code）；
// 归属查询失败上抛（不静默降级，防归属展示无声出错）。
func (a *albumsAPI) mediaItemsOut(ctx context.Context, medias []*store.MediaRow) ([]mediaItem, error) {
	ids := make([]int64, 0, len(medias))
	for _, m := range medias {
		ids = append(ids, m.ID)
	}
	ownership, err := a.db.AlbumOwnership(ctx, ids)
	if err != nil {
		return nil, err
	}
	items := make([]mediaItem, 0, len(medias))
	for _, m := range medias {
		item := mediaItem{
			ID: m.ID, Kind: m.Kind, Width: m.Width, Height: m.Height,
			URL:      a.mp.SignedURL(m.ID, "raw"),
			ThumbURL: a.mp.SignedURL(m.ID, "thumb"),
			PostID:   m.PostID, TakenAt: m.SortTime(), AuthorID: m.AuthorID,
		}
		if m.Duration.Valid {
			d := m.Duration.Float64
			item.Duration = &d
		}
		if ref, ok := ownership[m.ID]; ok {
			item.AlbumID, item.AlbumName = &ref.ID, &ref.Name
		}
		items = append(items, item)
	}
	return items, nil
}

func (a *albumsAPI) remove(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidID)
		return
	}
	al, err := a.db.GetAlbum(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "相册不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	// 月度相册不可删除、仅可隐藏（需求 §4.5）
	if al.Type == "monthly" {
		writeError(w, http.StatusForbidden, "月度相册不可删除，仅可隐藏")
		return
	}
	if err := a.db.SoftDeleteAlbum(r.Context(), id, store.ActorTypeUser, claims.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	// 删除对方创建的手动相册 → 站内通知创建者（自删静默）。内容删除仅站内，
	// 不走 dp.Dispatch（那会触发外部 Webhook 推送，REQUIREMENTS §4.8）。
	if al.CreatedBy.Valid {
		_ = a.db.NotifyContentDeleted(r.Context(), store.DeletedNotice{
			CreatorID: al.CreatedBy.Int64, DeleterID: claims.UserID, DeleterName: claims.Username,
			EventType: "album_deleted", TargetType: "album", TargetID: id,
			Extra: map[string]any{"name": al.Name},
		})
	}
	// DESIGN §13.3：删除一律 204 No Content
	w.WriteHeader(http.StatusNoContent)
}

func (a *albumsAPI) setHidden(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidID)
		return
	}
	var req struct {
		Hidden bool `json:"hidden"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	if err := a.db.SetAlbumHidden(r.Context(), id, req.Hidden); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "相册不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *albumsAPI) addPhotos(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidID)
		return
	}
	var req struct {
		MediaIDs []int64 `json:"media_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.MediaIDs) == 0 {
		writeError(w, http.StatusBadRequest, "media_ids 不能为空")
		return
	}
	if _, err := a.db.GetAlbum(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "相册不存在")
		return
	}
	if err := a.db.AddAlbumItems(r.Context(), id, claims.UserID, req.MediaIDs); err != nil {
		var owned *store.MediaOwnedError
		if errors.As(err, &owned) {
			// 文案单源于 store（mock 契约同款，含媒体 id）
			writeError(w, http.StatusConflict, owned.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	itemCount, _ := a.db.CountAlbumItems(r.Context(), id)
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "item_count": itemCount})
}

func (a *albumsAPI) removePhotos(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidID)
		return
	}
	var req struct {
		MediaIDs []int64 `json:"media_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.MediaIDs) == 0 {
		writeError(w, http.StatusBadRequest, "media_ids 不能为空")
		return
	}
	if err := a.db.RemoveAlbumItems(r.Context(), id, req.MediaIDs); err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	itemCount, _ := a.db.CountAlbumItems(r.Context(), id)
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "item_count": itemCount})
}

// timeline 媒体时间轴分页（D5，mock 契约）：page/size(size≤50) → {items,total,page,size,has_more}；
// item=timelineItemOut 全字段；排序 taken_at||created_at 倒序。year/month 参数不再暴露（cron 走 store 原方法）。
func (a *albumsAPI) timeline(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 50 {
		size = 50
	}
	medias, total, err := a.db.ListTimelineMediaPaged(r.Context(), page, size)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	items, err := a.mediaItemsOut(r.Context(), medias)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": total, "page": page, "size": size,
		"has_more": page*size < total,
	})
}
