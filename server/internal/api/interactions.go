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

// notifyPartner 互动通知口径：允许自赞/自藏，任何一方动作（含自己
// 赞/藏自己发布的动态）都通知另一方（传达「我在意这条内容」），评论同口径；
// 另一方不存在时站内无收件人，外部推送仍按渠道订阅照常发送。
// authorID 为目标动态作者：快照供客户端按「你的 / 自己的」渲染，content 同规则。
func (a *interactionsAPI) notifyPartner(r *http.Request, eventType string, targetID, authorID int64, title, content string, snapshot map[string]any) {
	claims, ok := auth.UserFromContext(r.Context())
	if !ok {
		return
	}
	if snapshot == nil {
		snapshot = map[string]any{}
	}
	snapshot["operator"] = claims.Username
	snapshot["author_id"] = authorID
	// 通知快照仅含字符串，必然可序列化。
	b, _ := json.Marshal(snapshot)
	recipients, _ := a.db.ListPartnerIDs(r.Context(), claims.UserID)
	a.dp.Dispatch(r.Context(), recipients, eventType, "post", targetID, string(b), title, content)
}

func (a *interactionsAPI) toggleLike(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	id, ok := postIDFromPath(r)
	if !ok {
		writeError(w, http.StatusBadRequest, errInvalidID)
		return
	}
	post, err := a.db.GetPost(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "动态不存在")
		return
	} else if err != nil {
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
		return
	}
	liked, err := a.db.ToggleLike(r.Context(), id, claims.UserID)
	if err != nil {
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
		return
	}
	if liked {
		a.notifyPartner(r, "post_liked", id, post.AuthorID, "收到点赞",
			claims.Username+" 赞了"+notify.PossessivePost(post.AuthorID, claims.UserID), nil)
	}
	writeJSON(w, http.StatusOK, map[string]any{"liked": liked})
}

func (a *interactionsAPI) toggleFavorite(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	id, ok := postIDFromPath(r)
	if !ok {
		writeError(w, http.StatusBadRequest, errInvalidID)
		return
	}
	post, err := a.db.GetPost(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "动态不存在")
		return
	} else if err != nil {
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
		return
	}
	favorited, err := a.db.ToggleFavorite(r.Context(), id, claims.UserID)
	if err != nil {
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
		return
	}
	if favorited {
		a.notifyPartner(r, "post_favorited", id, post.AuthorID, "收到收藏",
			claims.Username+" 收藏了"+notify.PossessivePost(post.AuthorID, claims.UserID), nil)
	}
	writeJSON(w, http.StatusOK, map[string]any{"favorited": favorited})
}

func (a *interactionsAPI) createComment(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	id, ok := postIDFromPath(r)
	if !ok {
		writeError(w, http.StatusBadRequest, errInvalidID)
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
	post, err := a.db.GetPost(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "动态不存在")
		return
	} else if err != nil {
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
		return
	}
	commentID, err := a.db.CreateComment(r.Context(), id, claims.UserID, req.Content)
	if err != nil {
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
		return
	}
	a.notifyPartner(r, "post_commented", id, post.AuthorID, "收到评论",
		claims.Username+" 评论了"+notify.PossessivePost(post.AuthorID, claims.UserID)+"："+textutil.TruncMark(req.Content, 50),
		map[string]any{"content": textutil.TruncMark(req.Content, 50)})
	// 返回完整评论对象（R7：补齐返回值，对齐契约）
	c, err := a.db.GetComment(r.Context(), commentID)
	if err != nil {
		writeJSON(w, http.StatusCreated, map[string]any{"id": commentID, "post_id": id, "author_id": claims.UserID, "content": req.Content})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         c.ID,
		"post_id":    c.PostID,
		"author_id":  c.AuthorID,
		"author":     nullStrText(c.AuthorNickname),
		"content":    c.Content,
		"created_at": c.CreatedAt,
	})
}

func (a *interactionsAPI) listComments(w http.ResponseWriter, r *http.Request) {
	id, ok := postIDFromPath(r)
	if !ok {
		writeError(w, http.StatusBadRequest, errInvalidID)
		return
	}
	comments, err := a.db.ListComments(r.Context(), id, 100, 0)
	if err != nil {
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
		return
	}
	type item struct {
		ID     int64  `json:"id"`
		Author string `json:"author"`
		// AuthorID 供 APP 判断「是否对方发布」：删对方评论时补「对方会收到一条通知。」
		// （客户端设计「删除交互统一规范」：删对方内容才提示，自删静默）
		AuthorID  int64  `json:"author_id"`
		Content   string `json:"content"`
		CreatedAt string `json:"created_at"`
	}
	items := make([]item, 0, len(comments))
	for _, c := range comments {
		items = append(items, item{
			ID: c.ID, Author: c.AuthorNickname.String, AuthorID: c.AuthorID,
			Content: c.Content, CreatedAt: c.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *interactionsAPI) deleteComment(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidID)
		return
	}
	c, err := a.db.GetComment(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "评论不存在")
		return
	}
	if err != nil {
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
		return
	}
	if err := a.db.SoftDeleteComment(r.Context(), id, "user", claims.UserID); err != nil {
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
		return
	}
	// 双方可删任意内容；删对方的评论 → 通知评论作者，自删静默
	_ = a.db.NotifyContentDeleted(r.Context(), store.DeletedNotice{
		CreatorID: c.AuthorID, DeleterID: claims.UserID, DeleterName: claims.Username,
		EventType: "comment_deleted", TargetType: "comment", TargetID: id,
		Extra: map[string]any{"content": textutil.TruncMark(c.Content, 50)},
	})
	// ：删除一律 204 No Content
	w.WriteHeader(http.StatusNoContent)
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
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
		return
	}
	type item struct {
		ID         int64  `json:"id"`
		EventType  string `json:"event_type"`
		TargetType string `json:"target_type"`
		TargetID   int64  `json:"target_id"`
		Snapshot   string `json:"snapshot"`
		CreatedAt  string `json:"created_at"`
	}
	items := make([]item, 0, len(list))
	for _, n := range list {
		items = append(items, item{
			ID: n.ID, EventType: n.EventType, TargetType: n.TargetType.String, TargetID: n.TargetID.Int64,
			Snapshot: n.Snapshot.String, CreatedAt: n.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *interactionsAPI) unreadCount(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	n, err := a.db.UnreadNotificationCount(r.Context(), claims.UserID)
	if err != nil {
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": n})
}

// consumeNotification 消费单条通知：点击即删除（PRODUCT「通知中心」：临时消费品）。
func (a *interactionsAPI) consumeNotification(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidID)
		return
	}
	if err := a.db.DeleteNotification(r.Context(), id, claims.UserID); err != nil {
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// clearNotifications 清空：删除收件人全部通知（「清空」按钮）。
func (a *interactionsAPI) clearNotifications(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	if err := a.db.DeleteAllNotifications(r.Context(), claims.UserID); err != nil {
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
