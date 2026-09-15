package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

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
		writeError(w, http.StatusUnauthorized, errUnauthorized)
		return
	}
	var req createPostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
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
			writeError(w, http.StatusInternalServerError, errInternal)
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
		writeError(w, http.StatusInternalServerError, errInternal)
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
	// 那年今日（REQUIREMENTS §4.1）：只取历年在今天同月同日发布的动态，不含当年；
	// 过滤在分页之前（契约同 mock），故 total 为过滤后总数。
	memories := r.URL.Query().Get("memories") == "1"
	posts, total, err := a.db.ListPosts(r.Context(), page, pageSize, memories)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	ids := make([]int64, 0, len(posts))
	for _, p := range posts {
		ids = append(ids, p.ID)
	}
	mediaMap, err := a.db.GetMediaByPostIDs(r.Context(), ids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	interMap, err := a.db.GetInteractions(r.Context(), ids, claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
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
		writeError(w, http.StatusBadRequest, errInvalidID)
		return
	}
	p, err := a.db.GetPost(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "动态不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	mediaMap, err := a.db.GetMediaByPostIDs(r.Context(), []int64{id})
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	interMap, err := a.db.GetInteractions(r.Context(), []int64{id}, claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusOK, a.buildPostResponse(p, mediaMap[id], interMap[id]))
}

func (a *postsAPI) deletePost(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, errUnauthorized)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidID)
		return
	}
	p, err := a.db.GetPost(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "动态不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	if err := a.db.SoftDeletePost(r.Context(), id, store.ActorTypeUser, claims.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	// 双方可删任意内容（需求 §7.1）；删对方发布的内容 → 站内通知，自删静默
	_ = a.db.NotifyContentDeleted(r.Context(), store.DeletedNotice{
		CreatorID: p.AuthorID, DeleterID: claims.UserID, DeleterName: claims.Username,
		EventType: "post_deleted", TargetType: "post", TargetID: id,
		Extra: map[string]any{"title": p.Title.String, "content": textutil.TruncMark(p.Content, 50)},
	})
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
		writeError(w, http.StatusInternalServerError, errInternal)
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
		writeError(w, http.StatusBadRequest, errInvalidID)
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
		writeError(w, http.StatusInternalServerError, errInternal)
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
			a.serveFile(w, r, m.ThumbPath.String, "image/jpeg", "thumb.jpg")
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
	// download=1：另存到本机（APP 查看器「保存」）。文件名由服务端生成——跨域下
	// <a download> 指定中文名会被浏览器忽略，必须靠 Content-Disposition。
	name := ""
	if r.URL.Query().Get("download") == "1" {
		name = a.downloadName(r.Context(), m)
		w.Header().Set("Content-Disposition", contentDisposition(name))
	}
	a.serveFile(w, r, m.FilePath, ct, name)
}

// serveFile 下发媒体字节。用 http.ServeContent 而非 io.Copy：自动获得 Range/206、
// Content-Length、Last-Modified —— 大视频/大音频另存可续传、播放器可拖进度，
// 且不必整份读进内存（*os.File 即 io.ReadSeeker）。
func (a *postsAPI) serveFile(w http.ResponseWriter, r *http.Request, relPath, contentType, name string) {
	f, err := a.mp.Open(relPath)
	if err != nil {
		writeError(w, http.StatusNotFound, "文件不存在")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=900")
	http.ServeContent(w, r, name, info.ModTime(), f)
}

// downloadName 生成另存文件名：{日期}_{照片|视频|录音}_{动态内序号}.{ext}
// 例：2026-08-15_照片_1.jpg。数据层没有原始文件名（Media 只有 file_path），
// 故按「拍摄时间优先、其次动态发布时间」+ 类型中文 + 该动态内序号拼。
// 日期口径：EXIF taken_at 原样；动态/媒体 created_at 为 UTC，转**本地时区**取日（见 store.LocalDate）。
func (a *postsAPI) downloadName(ctx context.Context, m *store.MediaRow) string {
	ext := fileExt(m.FilePath)
	kindWord, ok := map[string]string{"photo": "照片", "video": "视频", "audio": "录音"}[m.Kind]
	if !ok {
		kindWord = "媒体"
	}
	// taken_at 是 EXIF 墙钟时间（无时区），原样取日期；created_at 是 UTC，按本地时区取日期
	date := ""
	if m.TakenAt.Valid && len(m.TakenAt.String) >= 10 {
		date = m.TakenAt.String[:10]
	}
	if date == "" {
		if post, err := a.db.GetPost(ctx, m.PostID); err == nil && post.CreatedAt != "" {
			date = store.LocalDate(post.CreatedAt)
		} else {
			date = store.LocalDate(m.CreatedAt)
		}
	}
	seq := int(m.ID) // 兜底：拿不到同动态媒体列表时用媒体 id 保唯一
	if byPost, err := a.db.GetMediaByPostIDs(ctx, []int64{m.PostID}); err == nil {
		for i, x := range byPost[m.PostID] {
			if x.ID == m.ID {
				seq = i + 1
				break
			}
		}
	}
	if date == "" {
		return "love-life_" + kindWord + "_" + strconv.Itoa(seq) + ext
	}
	return date + "_" + kindWord + "_" + strconv.Itoa(seq) + ext
}

// contentDisposition 生成下载头：ASCII 兜底名 + RFC 5987 的 UTF-8 名。
// 中文文件名在 Chrome / Safari / Android WebView 上都按 filename* 取，
// filename= 只作老客户端兜底（去掉非 ASCII 字符并压掉重复下划线）。
func contentDisposition(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r >= 0x20 && r < 0x7f && r != '"' && r != 0x5c {
			b.WriteRune(r)
		}
	}
	ascii := b.String()
	for strings.Contains(ascii, "__") {
		ascii = strings.ReplaceAll(ascii, "__", "_")
	}
	if ascii == "" || strings.HasPrefix(ascii, ".") {
		ascii = "love-life" + fileExt(name)
	}
	return "attachment; filename=\"" + ascii + "\"; filename*=UTF-8''" + percentEncode(name)
}

// percentEncode RFC 5987 值编码：保留 attr-char，其余逐字节 %XX
func percentEncode(s string) string {
	const attrChar = "!#$&+-.^_|~"
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || strings.IndexByte(attrChar, c) >= 0 {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0xf])
		}
	}
	return b.String()
}

func fileExt(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '.' {
			return p[i:]
		}
	}
	return ""
}
