package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/bookandmusic/love-life/server/internal/auth"
	"github.com/bookandmusic/love-life/server/internal/notify"
	"github.com/bookandmusic/love-life/server/internal/store"
	"github.com/bookandmusic/love-life/server/internal/textutil"
)

type interactionsAPI struct {
	db *store.DB
	dp *notify.Dispatcher
}

func postIDFromPath(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil
}

// notifyPartner 互动通知口径（需求 §4.8）：允许自赞/自藏，任何一方动作（含自己
// 赞/藏自己发布的动态）都通知另一方（传达「我在意这条内容」），评论同口径；
// 另一方不存在时站内无收件人，外部推送仍按渠道订阅照常发送。
func (a *interactionsAPI) notifyPartner(r *http.Request, eventType string, targetID int64, title, content string, snapshot map[string]any) {
	claims, ok := auth.UserFromContext(r.Context())
	if !ok {
		return
	}
	if snapshot == nil {
		snapshot = map[string]any{}
	}
	snapshot["operator"] = claims.Username
	b, _ := json.Marshal(snapshot)
	recipients, _ := a.db.ListPartnerIDs(r.Context(), claims.UserID)
	a.dp.Dispatch(r.Context(), recipients, eventType, "post", targetID, string(b), title, content)
}

func (a *interactionsAPI) toggleLike(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	id, ok := postIDFromPath(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "无效 ID")
		return
	}
	if _, err := a.db.GetPost(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "动态不存在")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	liked, err := a.db.ToggleLike(r.Context(), id, claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	if liked {
		a.notifyPartner(r, "post_liked", id, "收到点赞", claims.Username+" 赞了你的动态", nil)
	}
	writeJSON(w, http.StatusOK, map[string]any{"liked": liked})
}

func (a *interactionsAPI) toggleFavorite(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	id, ok := postIDFromPath(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "无效 ID")
		return
	}
	if _, err := a.db.GetPost(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "动态不存在")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	favorited, err := a.db.ToggleFavorite(r.Context(), id, claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	if favorited {
		a.notifyPartner(r, "post_favorited", id, "收到收藏", claims.Username+" 收藏了你的动态", nil)
	}
	writeJSON(w, http.StatusOK, map[string]any{"favorited": favorited})
}

func (a *interactionsAPI) createComment(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	id, ok := postIDFromPath(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "无效 ID")
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Content == "" {
		writeError(w, http.StatusBadRequest, "评论内容不能为空")
		return
	}
	if len([]rune(req.Content)) > 500 {
		writeError(w, http.StatusBadRequest, "评论最多 500 字")
		return
	}
	if _, err := a.db.GetPost(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "动态不存在")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	commentID, err := a.db.CreateComment(r.Context(), id, claims.UserID, req.Content)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	a.notifyPartner(r, "post_commented", id, "收到评论",
		claims.Username+"："+textutil.TruncMark(req.Content, 50), map[string]any{"content": textutil.TruncMark(req.Content, 50)})
	writeJSON(w, http.StatusCreated, map[string]any{"id": commentID})
}

func (a *interactionsAPI) listComments(w http.ResponseWriter, r *http.Request) {
	id, ok := postIDFromPath(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "无效 ID")
		return
	}
	comments, err := a.db.ListComments(r.Context(), id, 100, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	type item struct {
		ID        int64  `json:"id"`
		Author    string `json:"author"`
		Content   string `json:"content"`
		CreatedAt string `json:"created_at"`
	}
	items := make([]item, 0, len(comments))
	for _, c := range comments {
		items = append(items, item{ID: c.ID, Author: c.AuthorNickname.String, Content: c.Content, CreatedAt: c.CreatedAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *interactionsAPI) deleteComment(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效 ID")
		return
	}
	c, err := a.db.GetComment(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "评论不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	// 双方可删任意内容；删对方的评论 → 通知评论作者（自删静默）
	if c.AuthorID != claims.UserID {
		snapshot, _ := json.Marshal(map[string]any{"content": textutil.TruncMark(c.Content, 50), "deleted_by": claims.Username})
		_ = a.db.InsertNotification(r.Context(), c.AuthorID, "comment_deleted", "comment", id, string(snapshot))
	}
	if err := a.db.SoftDeleteComment(r.Context(), id, "user", claims.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---- 通知中心 ----

func (a *interactionsAPI) listNotifications(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	list, err := a.db.ListNotifications(r.Context(), claims.UserID, 50, (page-1)*50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	type item struct {
		ID         int64  `json:"id"`
		EventType  string `json:"event_type"`
		TargetType string `json:"target_type"`
		TargetID   int64  `json:"target_id"`
		Snapshot   string `json:"snapshot"`
		IsRead     bool   `json:"is_read"`
		CreatedAt  string `json:"created_at"`
	}
	items := make([]item, 0, len(list))
	for _, n := range list {
		items = append(items, item{
			ID: n.ID, EventType: n.EventType, TargetType: n.TargetType.String, TargetID: n.TargetID.Int64,
			Snapshot: n.Snapshot.String, IsRead: n.IsRead, CreatedAt: n.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *interactionsAPI) unreadCount(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	n, err := a.db.UnreadNotificationCount(r.Context(), claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": n})
}

func (a *interactionsAPI) markRead(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效 ID")
		return
	}
	if err := a.db.MarkNotificationRead(r.Context(), id, claims.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *interactionsAPI) markAllRead(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	if err := a.db.MarkAllNotificationsRead(r.Context(), claims.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
