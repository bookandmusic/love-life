package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/auth"
	"github.com/bookandmusic/love-life/server/internal/store"
)

func TestInteractionsAndNotifications(t *testing.T) {
	ts, adminToken, girlToken := newPostTestEnv(t)

	// admin 发一条动态
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{"content": "海边的一天"})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create post: %d", res.StatusCode)
	}

	// girl 点赞（girl 的另一方在本环境不存在——admin 不在情侣里，不产生站内通知）；liked/like_count 生效
	res = doJSON(t, "POST", ts.URL+"/api/v1/posts/1/like", girlToken, nil)
	var like struct {
		Liked bool `json:"liked"`
	}
	_ = json.NewDecoder(res.Body).Decode(&like)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || !like.Liked {
		t.Fatalf("like: %d %+v", res.StatusCode, like)
	}

	res = doJSON(t, "GET", ts.URL+"/api/v1/posts/1", adminToken, nil)
	var detail struct {
		LikeCount    int  `json:"like_count"`
		Liked        bool `json:"liked"`
		CommentCount int  `json:"comment_count"`
	}
	_ = json.NewDecoder(res.Body).Decode(&detail)
	res.Body.Close()
	if detail.LikeCount != 1 || detail.Liked {
		t.Fatalf("admin view: like_count=%d liked=%v", detail.LikeCount, detail.Liked)
	}

	// 自赞也通知另一方：admin 给自己点赞 → girl 收到
	res = doJSON(t, "POST", ts.URL+"/api/v1/posts/1/like", adminToken, nil)
	res.Body.Close()

	// girl 评论（同上，无另一方可通知）
	res = doJSON(t, "POST", ts.URL+"/api/v1/posts/1/comments", girlToken, map[string]string{"content": "真好呀"})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("comment: %d", res.StatusCode)
	}

	// admin 的通知：girl 的赞/评不进 admin（admin 不在情侣里）= 0 条
	res = doJSON(t, "GET", ts.URL+"/api/v1/notifications", adminToken, nil)
	var notif struct {
		Items []struct {
			EventType string `json:"event_type"`
		} `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&notif)
	res.Body.Close()
	if len(notif.Items) != 0 {
		t.Fatalf("admin notifications: want 0, got %d %+v", len(notif.Items), notif.Items)
	}

	// 未读数（admin 0 条）
	res = doJSON(t, "GET", ts.URL+"/api/v1/notifications/unread-count", adminToken, nil)
	var unread struct {
		Count int `json:"count"`
	}
	_ = json.NewDecoder(res.Body).Decode(&unread)
	res.Body.Close()
	if unread.Count != 0 {
		t.Fatalf("unread: %d", unread.Count)
	}

	// girl 的通知：admin 发的新动态 + admin 自赞的通知 = 2 条
	res = doJSON(t, "GET", ts.URL+"/api/v1/notifications", girlToken, nil)
	var girlNotif struct {
		Items []struct {
			ID        int64  `json:"id"`
			EventType string `json:"event_type"`
			Snapshot  string `json:"snapshot"`
		} `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&girlNotif)
	res.Body.Close()
	var createdCount, likedCount int
	var likedID int64
	var likedSnap struct {
		AuthorID int64 `json:"author_id"`
	}
	for _, it := range girlNotif.Items {
		switch it.EventType {
		case "post_created":
			createdCount++
		case "post_liked":
			likedCount++
			likedID = it.ID
			// 自赞快照必须带 author_id（= 操作者本人）：客户端据此渲染「自己的动态」而非「你的动态」
			if err := json.Unmarshal([]byte(it.Snapshot), &likedSnap); err != nil {
				t.Fatalf("parse liked snapshot: %v", err)
			}
		}
	}
	if len(girlNotif.Items) != 2 || createdCount != 1 || likedCount != 1 {
		t.Fatalf("girl notifications: %d items (%+v), want created=1 liked=1", len(girlNotif.Items), girlNotif.Items)
	}
	var adminID int64
	_ = ts.db.QueryRow(`SELECT id FROM users WHERE username = 'admin'`).Scan(&adminID)
	if likedSnap.AuthorID != adminID {
		t.Fatalf("post_liked snapshot author_id = %d, want admin %d（自赞归属）", likedSnap.AuthorID, adminID)
	}

	// 点击即删：DELETE 单条 → 列表移除、未读数减 1
	res = doJSON(t, "DELETE", fmt.Sprintf("%s/api/v1/notifications/%d", ts.URL, likedID), girlToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("delete notification: %d", res.StatusCode)
	}
	res = doJSON(t, "GET", ts.URL+"/api/v1/notifications/unread-count", girlToken, nil)
	_ = json.NewDecoder(res.Body).Decode(&unread)
	res.Body.Close()
	if unread.Count != 1 {
		t.Fatalf("unread after delete one: %d, want 1", unread.Count)
	}

	// 清空：DELETE 全部 → 列表空、计数 0
	res = doJSON(t, "DELETE", ts.URL+"/api/v1/notifications", girlToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("clear notifications: %d", res.StatusCode)
	}
	res = doJSON(t, "GET", ts.URL+"/api/v1/notifications/unread-count", girlToken, nil)
	_ = json.NewDecoder(res.Body).Decode(&unread)
	res.Body.Close()
	if unread.Count != 0 {
		t.Fatalf("unread after clear: %d", unread.Count)
	}
	res = doJSON(t, "GET", ts.URL+"/api/v1/notifications", girlToken, nil)
	var girlNotifAfterClear struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&girlNotifAfterClear)
	res.Body.Close()
	if len(girlNotifAfterClear.Items) != 0 {
		t.Fatalf("girl notifications after clear: %d, want 0", len(girlNotifAfterClear.Items))
	}

	// 收藏切换：girl 收 → 取消收
	res = doJSON(t, "POST", ts.URL+"/api/v1/posts/1/favorite", girlToken, nil)
	var fav struct {
		Favorited bool `json:"favorited"`
	}
	_ = json.NewDecoder(res.Body).Decode(&fav)
	res.Body.Close()
	if !fav.Favorited {
		t.Fatal("favorite on")
	}
	res = doJSON(t, "POST", ts.URL+"/api/v1/posts/1/favorite", girlToken, nil)
	_ = json.NewDecoder(res.Body).Decode(&fav)
	res.Body.Close()
	if fav.Favorited {
		t.Fatal("favorite off")
	}

	// 评论列表 + girl 删 admin 的？不——评论是 girl 自己发的；admin 删 girl 的评论 → 通知 girl
	res = doJSON(t, "DELETE", ts.URL+"/api/v1/comments/1", adminToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete comment: %d", res.StatusCode)
	}
	var girlSnap string
	_ = ts.db.QueryRow("SELECT snapshot_json FROM notifications WHERE event_type='comment_deleted' AND recipient_id=(SELECT id FROM users WHERE username='girl')").Scan(&girlSnap)
	if girlSnap == "" {
		t.Fatal("comment_deleted notification for girl missing")
	}

	// 删后评论列表为空、comment_count 归零
	res = doJSON(t, "GET", ts.URL+"/api/v1/posts/1/comments", adminToken, nil)
	var comments struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&comments)
	res.Body.Close()
	if len(comments.Items) != 0 {
		t.Fatalf("comments after delete: %d", len(comments.Items))
	}
	res = doJSON(t, "GET", ts.URL+"/api/v1/posts/1", adminToken, nil)
	_ = json.NewDecoder(res.Body).Decode(&detail)
	res.Body.Close()
	if detail.CommentCount != 0 {
		t.Fatalf("comment_count after delete: %d", detail.CommentCount)
	}

	// 列表必须带 author_id：APP 据此判断「是否对方发布」，删对方评论才补「对方会收到通知。」
	// （缺该字段时 c.author_id 恒为 undefined，提示永不出现——客户端设计「删除交互统一规范」）
	res = doJSON(t, "POST", ts.URL+"/api/v1/posts/1/comments", girlToken, map[string]string{"content": "带 author_id 的评论"})
	res.Body.Close()
	var listed struct {
		Items []map[string]any `json:"items"`
	}
	res = doJSON(t, "GET", ts.URL+"/api/v1/posts/1/comments", adminToken, nil)
	_ = json.NewDecoder(res.Body).Decode(&listed)
	res.Body.Close()
	if len(listed.Items) != 1 {
		t.Fatalf("评论列表应 1 条，实得 %d", len(listed.Items))
	}
	var girlID int64
	_ = ts.db.QueryRow(`SELECT id FROM users WHERE username = 'girl'`).Scan(&girlID)
	gotAuthor, ok := listed.Items[0]["author_id"]
	if !ok {
		t.Fatalf("评论列表缺 author_id：%+v", listed.Items[0])
	}
	if int64(gotAuthor.(float64)) != girlID {
		t.Fatalf("author_id = %v，want %d", gotAuthor, girlID)
	}
}

// TestInteractionNotifyD6 口径：任何一方动作（含自操作）都通知另一方；
// 覆盖 自赞/自藏/自评 + 互赞四情形（双方各一次点赞均产生通知，取消不产生）。
// 需 girl+boy 两位非 admin 用户才能表达「双方」——admin 动作→girl/boy 收，
// girl/boy 动作→彼此收（操作者被排除，admin 不在情侣里）。
func TestInteractionNotifyD6(t *testing.T) {
	ts, adminToken, girlToken := newPostTestEnv(t)
	hash, _ := auth.HashPassword("boy-pw")
	if _, err := ts.db.CreateUser(t.Context(), &store.User{Username: "boy", PasswordHash: hash, Role: "boy", Nickname: "他"}); err != nil {
		t.Fatalf("create boy: %v", err)
	}
	boyToken, code := loginToken(t, ts.URL, "boy", "boy-pw", "app")
	if code != http.StatusOK {
		t.Fatalf("boy login: %d", code)
	}

	// girl 发动态（作者=girl）
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", girlToken, map[string]any{"content": "D6 通知口径"})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create post: %d", res.StatusCode)
	}

	// ① 自赞：girl 赞自己的动态 → 另一方 boy 收到（作者=自己不再是收件人条件）
	res = doJSON(t, "POST", ts.URL+"/api/v1/posts/1/like", girlToken, nil)
	res.Body.Close()
	// ② 自藏：girl 藏自己的动态 → boy 收到
	res = doJSON(t, "POST", ts.URL+"/api/v1/posts/1/favorite", girlToken, nil)
	res.Body.Close()
	// ③ 自评：girl 评论自己的动态 → boy 收到
	res = doJSON(t, "POST", ts.URL+"/api/v1/posts/1/comments", girlToken, map[string]string{"content": "自我欣赏"})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("comment: %d", res.StatusCode)
	}

	var boyCount int
	if err := ts.db.QueryRow(`SELECT COUNT(*) FROM notifications n JOIN users u ON u.id = n.recipient_id
		WHERE u.username = 'boy' AND n.event_type IN ('post_liked','post_favorited','post_commented')`).Scan(&boyCount); err != nil {
		t.Fatalf("query boy notifs: %v", err)
	}
	if boyCount != 3 {
		t.Fatalf("boy interaction notifications: %d (want 3: 自赞+自藏+自评)", boyCount)
	}
	// girl 自己的动作不通知自己（收件人排除操作者）
	var girlSelf int
	if err := ts.db.QueryRow(`SELECT COUNT(*) FROM notifications n JOIN users u ON u.id = n.recipient_id
		WHERE u.username = 'girl' AND n.event_type IN ('post_liked','post_favorited','post_commented')`).Scan(&girlSelf); err != nil {
		t.Fatalf("query girl self: %v", err)
	}
	if girlSelf != 0 {
		t.Fatalf("girl self notifications: %d (want 0：操作者不在收件人之列)", girlSelf)
	}
	// admin 不在情侣里：girl 的动作不通知 admin
	var adminCount int
	if err := ts.db.QueryRow(`SELECT COUNT(*) FROM notifications n JOIN users u ON u.id = n.recipient_id
		WHERE u.username = 'admin' AND n.event_type IN ('post_liked','post_favorited','post_commented')`).Scan(&adminCount); err != nil {
		t.Fatalf("query admin: %v", err)
	}
	if adminCount != 0 {
		t.Fatalf("admin interaction notifications: %d (want 0：admin 不在情侣里)", adminCount)
	}

	// ④ 互赞：boy 赞 girl 的动态 1 → girl 收到；boy 建动态 2，girl 赞回
	// → boy 再收到一次（互赞双方各自产生通知，均与作者无关）
	res = doJSON(t, "POST", ts.URL+"/api/v1/posts/1/like", boyToken, nil)
	res.Body.Close()
	res = doJSON(t, "POST", ts.URL+"/api/v1/posts", boyToken, map[string]any{"content": "boy 的动态"})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("boy create post: %d", res.StatusCode)
	}
	res = doJSON(t, "POST", ts.URL+"/api/v1/posts/2/like", girlToken, nil)
	res.Body.Close()

	var girlLiked, boyLiked int
	if err := ts.db.QueryRow(`SELECT COUNT(*) FROM notifications n JOIN users u ON u.id = n.recipient_id
		WHERE u.username = 'girl' AND n.event_type = 'post_liked'`).Scan(&girlLiked); err != nil {
		t.Fatalf("query girl liked: %v", err)
	}
	if err := ts.db.QueryRow(`SELECT COUNT(*) FROM notifications n JOIN users u ON u.id = n.recipient_id
		WHERE u.username = 'boy' AND n.event_type = 'post_liked'`).Scan(&boyLiked); err != nil {
		t.Fatalf("query boy liked: %v", err)
	}
	if girlLiked != 1 || boyLiked != 2 {
		t.Fatalf("mutual like: girl=%d boy=%d (want girl=1 boy=2：自赞+girl 赞回)", girlLiked, boyLiked)
	}

	// ⑤ 取消不产生通知：girl 取消对动态 2 的赞（无通知），toggle 响应契约维持 {liked}/{favorited}
	res = doJSON(t, "POST", ts.URL+"/api/v1/posts/2/like", girlToken, nil)
	var like struct {
		Liked bool `json:"liked"`
	}
	_ = json.NewDecoder(res.Body).Decode(&like)
	res.Body.Close()
	if like.Liked {
		t.Fatal("unlike must return liked=false")
	}
	var boyLiked2 int
	if err := ts.db.QueryRow(`SELECT COUNT(*) FROM notifications n JOIN users u ON u.id = n.recipient_id
		WHERE u.username = 'boy' AND n.event_type = 'post_liked'`).Scan(&boyLiked2); err != nil {
		t.Fatalf("query boy liked2: %v", err)
	}
	if boyLiked2 != 2 {
		t.Fatalf("boy post_liked after unlike: %d (want 2：取消不产生通知)", boyLiked2)
	}
	_ = adminToken // admin 仅参与环境与上方反断言
}

// TestInteractionNotifyPartnerScope 边界：ListPartnerIDs 收件人域——
// 非 admin 且非操作者（admin 不在情侣里）。
func TestInteractionNotifyPartnerScope(t *testing.T) {
	ts, _, _ := newPostTestEnv(t)
	hash, _ := auth.HashPassword("boy-pw")
	if _, err := ts.db.CreateUser(t.Context(), &store.User{Username: "boy", PasswordHash: hash, Role: "boy", Nickname: "他"}); err != nil {
		t.Fatalf("create boy: %v", err)
	}
	// 操作者=admin（id=1）→ 收件人=girl+boy
	ids, err := ts.db.ListPartnerIDs(t.Context(), 1)
	if err != nil {
		t.Fatalf("ListPartnerIDs: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("partner ids for admin: %v (want girl+boy)", ids)
	}
	// 操作者=girl → 收件人=boy（排除自己与 admin）
	var girlID int64
	if err := ts.db.QueryRow(`SELECT id FROM users WHERE username='girl'`).Scan(&girlID); err != nil {
		t.Fatalf("query girl id: %v", err)
	}
	ids, err = ts.db.ListPartnerIDs(t.Context(), girlID)
	if err != nil {
		t.Fatalf("ListPartnerIDs(girl): %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("partner ids for girl: %v (want only boy)", ids)
	}
	var role string
	if err := ts.db.QueryRow(`SELECT role FROM users WHERE id = ?`, ids[0]).Scan(&role); err != nil {
		t.Fatalf("query role: %v", err)
	}
	if role != "boy" {
		t.Fatalf("partner role: %s (want boy)", role)
	}
}
