package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/bookandmusic/love-life/server/internal/auth"
	"github.com/bookandmusic/love-life/server/internal/geo"
	"github.com/bookandmusic/love-life/server/internal/media"
	"github.com/bookandmusic/love-life/server/internal/notify"
	"github.com/bookandmusic/love-life/server/internal/store"
	"github.com/bookandmusic/love-life/server/internal/textutil"
)

type postsAPI struct {
	db *store.DB
	mp *media.Pipeline
	dp *notify.Dispatcher
}

func nullStr(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// ---- 媒体上传（两段式第一段：临时区） ----

func (a *postsAPI) uploadMedia(w http.ResponseWriter, r *http.Request) {
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "缺少 file 字段")
		return
	}
	defer file.Close()
	up, err := a.mp.SaveTemp(file, header.Filename)
	if err != nil {
		if errors.Is(err, media.ErrUnsupportedFormat) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, up)
}

// ---- 动态 ----

// locationInput 地点输入：地图选点/候选回填传对象（name+坐标），纯文本地名传 location_name。
type locationInput struct {
	Name string   `json:"name"`
	Lat  *float64 `json:"lat"`
	Lng  *float64 `json:"lng"`
}

type createPostRequest struct {
	Title       string         `json:"title"`
	Content     string         `json:"content"`
	Mood        string         `json:"mood"`
	Loc         *locationInput `json:"location"`
	Location    string         `json:"location_name"`
	MediaTokens []string       `json:"media_tokens"`
}

type mediaResponse struct {
	ID       int64  `json:"id"`
	Kind     string `json:"kind"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	URL      string `json:"url"`
	ThumbURL string `json:"thumb_url"`
}

type postResponse struct {
	ID           int64           `json:"id"`
	AuthorID     int64           `json:"author_id"`
	Author       string          `json:"author"`
	Title        string          `json:"title"`
	Content      string          `json:"content"`
	Mood         string          `json:"mood"`
	Location     string          `json:"location_name"`
	CreatedAt    string          `json:"created_at"`
	Media        []mediaResponse `json:"media"`
	LikeCount    int             `json:"like_count"`
	Liked        bool            `json:"liked"`
	FavCount     int             `json:"favorite_count"`
	Favorited    bool            `json:"favorited"`
	CommentCount int             `json:"comment_count"`
}

func (a *postsAPI) buildPostResponse(p *store.Post, medias []*store.MediaRow, inter *store.PostInteraction) postResponse {
	if inter == nil {
		inter = &store.PostInteraction{}
	}
	resp := postResponse{
		ID: p.ID, AuthorID: p.AuthorID, Author: p.AuthorNickname.String,
		Title: p.Title.String, Content: p.Content, Mood: p.Mood.String,
		Location: p.LocationName.String, CreatedAt: p.CreatedAt, Media: []mediaResponse{},
		LikeCount: inter.LikeCount, Liked: inter.Liked,
		FavCount: inter.FavoriteCount, Favorited: inter.Favorited,
		CommentCount: inter.CommentCount,
	}
	for _, m := range medias {
		resp.Media = append(resp.Media, mediaResponse{
			ID: m.ID, Kind: m.Kind, Width: m.Width, Height: m.Height,
			URL:      a.mp.SignedURL(m.ID, "raw"),
			ThumbURL: a.mp.SignedURL(m.ID, "thumb"),
		})
	}
	return resp
}

func (a *postsAPI) createPost(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "未认证")
		return
	}
	var req createPostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// 地点名解析：location 对象（含坐标）优先，其次 location_name 纯文本（对齐 mock 契约）
	locName := ""
	lat, lng := 0.0, 0.0
	if req.Loc != nil && req.Loc.Name != "" {
		locName = req.Loc.Name
		if req.Loc.Lat != nil {
			lat = *req.Loc.Lat
		}
		if req.Loc.Lng != nil {
			lng = *req.Loc.Lng
		}
	} else {
		locName = req.Location
	}
	if req.Content == "" && len(req.MediaTokens) == 0 && locName == "" && req.Mood == "" {
		writeError(w, http.StatusBadRequest, "请至少填写文字、心情、地点或媒体")
		return
	}
	if len(req.MediaTokens) > 18 {
		writeError(w, http.StatusBadRequest, "单条动态最多 18 个媒体文件")
		return
	}

	// 带地点时同步点位（双来源同池：同名复用、坐标以先创建者为准）；
	// 坐标缺失时查内置地名表回填（对齐 mock locatePlace）
	var pointID sql.NullInt64
	if locName != "" {
		if lat == 0 && lng == 0 {
			if hit, ok := geo.LocatePlace(locName); ok {
				lat, lng = hit.Lat, hit.Lng
			}
		}
		point, _, err := a.db.FindOrCreatePoint(r.Context(), store.PointInput{
			Name: locName, Lat: lat, Lng: lng, Source: "post", UserID: claims.UserID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "服务器错误")
			return
		}
		pointID = sql.NullInt64{Int64: point.ID, Valid: true}
	}

	medias := make([]store.MediaRow, 0, len(req.MediaTokens))
	promoted := make([]*media.Promoted, 0, len(req.MediaTokens))
	for _, token := range req.MediaTokens {
		p, err := a.mp.Promote(token)
		if err != nil {
			a.cleanupPromoted(promoted)
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		promoted = append(promoted, p)
		medias = append(medias, store.MediaRow{
			Kind: p.Kind, FilePath: p.FilePath, ThumbPath: nullStr(p.ThumbPath),
			Width: p.Width, Height: p.Height, Size: p.Size,
			Duration: sql.NullFloat64{Float64: p.Duration, Valid: p.Duration > 0},
			TakenAt:  nullStr(p.TakenAt),
		})
	}

	postID, err := a.db.CreatePostWithMedia(r.Context(), &store.Post{
		AuthorID: claims.UserID, Content: req.Content,
		Title: nullStr(req.Title), Mood: nullStr(req.Mood),
		LocationName: nullStr(locName), PointID: pointID,
	}, medias)
	if err != nil {
		a.cleanupPromoted(promoted)
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}

	// 新动态通知另一方（admin 不在情侣里，见 mock partnerIds 口径）+ 外部推送（订阅 new_post 的渠道）
	recipients, _ := a.db.ListPartnerIDs(r.Context(), claims.UserID)
	snapshot, _ := json.Marshal(map[string]any{
		"title": req.Title, "content": textutil.TruncMark(req.Content, 50), "operator": claims.Username,
	})
	pushTitle := req.Title
	if pushTitle == "" {
		pushTitle = "新动态"
	}
	a.dp.Dispatch(r.Context(), recipients, "post_created", "post", postID, string(snapshot),
		pushTitle, textutil.TruncMark(req.Content, 100))

	writeJSON(w, http.StatusCreated, map[string]any{"id": postID})
}

func (a *postsAPI) cleanupPromoted(promoted []*media.Promoted) {
	for _, p := range promoted {
		a.mp.Delete(p.FilePath, p.ThumbPath)
	}
}

func (a *postsAPI) listPosts(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 50 {
		pageSize = 50 // 夹紧上限（mock 无上限，有意收紧防 DoS）
	}
	posts, total, err := a.db.ListPosts(r.Context(), page, pageSize)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	ids := make([]int64, 0, len(posts))
	for _, p := range posts {
		ids = append(ids, p.ID)
	}
	mediaMap, err := a.db.GetMediaByPostIDs(r.Context(), ids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	interMap, err := a.db.GetInteractions(r.Context(), ids, claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	items := make([]postResponse, 0, len(posts))
	for _, p := range posts {
		items = append(items, a.buildPostResponse(p, mediaMap[p.ID], interMap[p.ID]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": page, "page_size": pageSize})
}

func (a *postsAPI) getPost(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效 ID")
		return
	}
	p, err := a.db.GetPost(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "动态不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	mediaMap, err := a.db.GetMediaByPostIDs(r.Context(), []int64{id})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	interMap, err := a.db.GetInteractions(r.Context(), []int64{id}, claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	writeJSON(w, http.StatusOK, a.buildPostResponse(p, mediaMap[id], interMap[id]))
}

func (a *postsAPI) deletePost(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "未认证")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效 ID")
		return
	}
	p, err := a.db.GetPost(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "动态不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	// 双方可删任意内容（需求 §7.1）；删对方发布的内容 → 站内通知（自删静默）
	if p.AuthorID != claims.UserID {
		snapshot, _ := json.Marshal(map[string]any{
			"title": p.Title.String, "content": textutil.TruncMark(p.Content, 50), "deleted_by": claims.Username,
		})
		_ = a.db.InsertNotification(r.Context(), p.AuthorID, "post_deleted", "post", id, string(snapshot))
	}
	if err := a.db.SoftDeletePost(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---- 媒体文件服务（签名 URL，无 Authorization 头） ----

// reurlMedia 批量重签媒体 URL（D1）：{ids ≤200} → {items:[{id,thumb_url,url}]}；
// 无效/已删 id 跳过不报错；签名 TTL 与现有媒体一致（15 分钟，DESIGN 4.5）。
func (a *postsAPI) reurlMedia(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "ids 不能为空")
		return
	}
	if len(req.IDs) > 200 {
		writeError(w, http.StatusBadRequest, "单次最多重签 200 个媒体")
		return
	}
	medias, err := a.db.GetMediaByIDs(r.Context(), req.IDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	type reurlItem struct {
		ID       int64  `json:"id"`
		URL      string `json:"url"`
		ThumbURL string `json:"thumb_url"`
	}
	items := make([]reurlItem, 0, len(medias))
	for _, m := range medias {
		items = append(items, reurlItem{ID: m.ID, URL: a.mp.SignedURL(m.ID, "raw"), ThumbURL: a.mp.SignedURL(m.ID, "thumb")})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *postsAPI) serveMediaFile(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效 ID")
		return
	}
	variant := r.URL.Query().Get("v")
	if variant != "raw" && variant != "thumb" {
		variant = "raw"
	}
	if !a.mp.VerifySig(id, variant, r.URL.Query().Get("exp"), r.URL.Query().Get("sig")) {
		writeError(w, http.StatusUnauthorized, "签名无效或已过期")
		return
	}
	m, err := a.db.GetMedia(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "媒体不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}

	if variant == "thumb" {
		// 视频/音频缩略图 = 占位图（零 ffmpeg，DESIGN 4.8；音频对齐 mock 蓝底 SVG）
		if m.Kind == "video" {
			ct, body := media.PlaceholderThumb()
			w.Header().Set("Content-Type", ct)
			w.Header().Set("Cache-Control", "private, max-age=900")
			_, _ = w.Write(body)
			return
		}
		if m.ThumbPath.Valid {
			a.serveFile(w, m.ThumbPath.String, "image/jpeg")
			return
		}
	}
	if variant == "thumb" && m.Kind == "audio" {
		ct, body := media.AudioPlaceholder(id)
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Cache-Control", "private, max-age=900")
		_, _ = w.Write(body)
		return
	}

	ext := fileExt(m.FilePath)
	ct := map[string]string{".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".webp": "image/webp", ".mp4": "video/mp4", ".mov": "video/quicktime", ".webm": "video/webm", ".mp3": "audio/mpeg", ".m4a": "audio/mp4", ".wav": "audio/wav"}[ext]
	if ct == "" {
		ct = "application/octet-stream"
	}
	a.serveFile(w, m.FilePath, ct)
}

func (a *postsAPI) serveFile(w http.ResponseWriter, relPath, contentType string) {
	f, err := a.mp.Open(relPath)
	if err != nil {
		writeError(w, http.StatusNotFound, "文件不存在")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=900")
	if _, err := io.Copy(w, f); err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		// 客户端断开等场景静默
	}
}

func fileExt(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '.' {
			return p[i:]
		}
	}
	return ""
}
