package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

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

	// boy 点赞 girl 的动态（互为情侣 → girl 收到通知）；liked/like_count 生效
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
		t.Fatalf("girl view: like_count=%d liked=%v", detail.LikeCount, detail.Liked)
	}

	// 自赞也通知另一方：girl 给自己点赞 → boy 收到
	res = doJSON(t, "POST", ts.URL+"/api/v1/posts/1/like", adminToken, nil)
	res.Body.Close()

	// boy 评论 girl 的动态 → girl 收到
	res = doJSON(t, "POST", ts.URL+"/api/v1/posts/1/comments", girlToken, map[string]string{"content": "真好呀"})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("comment: %d", res.StatusCode)
	}

	// girl 的通知：boy 的赞/评都进（互为情侣）= 2 条
	res = doJSON(t, "GET", ts.URL+"/api/v1/notifications", adminToken, nil)
	var notif struct {
		Items []struct {
			EventType string `json:"event_type"`
		} `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&notif)
	res.Body.Close()
	if len(notif.Items) != 2 {
		t.Fatalf("girl notifications: want 2 (boy 赞+评), got %d %+v", len(notif.Items), notif.Items)
	}

	// 未读数（girl 2 条）
	res = doJSON(t, "GET", ts.URL+"/api/v1/notifications/unread-count", adminToken, nil)
	var unread struct {
		Count int `json:"count"`
	}
	_ = json.NewDecoder(res.Body).Decode(&unread)
	res.Body.Close()
	if unread.Count != 2 {
		t.Fatalf("unread: %d, want 2（boy 的赞+评）", unread.Count)
	}

	// boy 的通知：girl 发的新动态 + girl 自赞的通知 = 2 条
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
		AuthorID int64  `json:"author_id"`
		Text     string `json:"text"`
	}
	for _, it := range girlNotif.Items {
		switch it.EventType {
		case "post_created":
			createdCount++
		case "post_liked":
			likedCount++
			likedID = it.ID
			// 自赞快照：author_id = 操作者本人，text 按「自己的动态」渲染（不能用「你的」）
			if err := json.Unmarshal([]byte(it.Snapshot), &likedSnap); err != nil {
				t.Fatalf("parse liked snapshot: %v", err)
			}
		}
	}
	if len(girlNotif.Items) != 2 || createdCount != 1 || likedCount != 1 {
		t.Fatalf("girl notifications: %d items (%+v), want created=1 liked=1", len(girlNotif.Items), girlNotif.Items)
	}
	var boyID int64
	_ = ts.db.QueryRow(`SELECT id FROM users WHERE username = 'boy'`).Scan(&boyID)
	if likedSnap.AuthorID != boyID {
		t.Fatalf("post_liked snapshot author_id = %d, want boy %d（自赞归属）", likedSnap.AuthorID, boyID)
	}
	if !strings.Contains(likedSnap.Text, "自己的动态") {
		t.Fatalf("自赞快照 text = %q，应写「自己的动态」", likedSnap.Text)
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

	// 评论是 girl 发的：非作者删除被拒（仅作者可删）
	res = doJSON(t, "DELETE", ts.URL+"/api/v1/comments/1", adminToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("delete others' comment: %d, want 403", res.StatusCode)
	}
	// 评论仍在
	res = doJSON(t, "GET", ts.URL+"/api/v1/posts/1/comments", adminToken, nil)
	var beforeOwn struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&beforeOwn)
	res.Body.Close()
	if len(beforeOwn.Items) != 1 {
		t.Fatalf("comment should survive forbidden delete: %d", len(beforeOwn.Items))
	}

	// 作者删自己的评论 → 204；自删静默（无 comment_deleted 通知）
	res = doJSON(t, "DELETE", ts.URL+"/api/v1/comments/1", girlToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete own comment: %d", res.StatusCode)
	}
	var girlSnap string
	_ = ts.db.QueryRow("SELECT snapshot_json FROM notifications WHERE event_type='comment_deleted' AND recipient_id=(SELECT id FROM users WHERE username='girl')").Scan(&girlSnap)
	if girlSnap != "" {
		t.Fatal("self-delete should be silent, got comment_deleted notification")
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
	gotAuthor, ok := listed.Items[0]["author_id"]
	if !ok {
		t.Fatalf("评论列表缺 author_id：%+v", listed.Items[0])
	}
	var girlID int64
	_ = ts.db.QueryRow(`SELECT id FROM users WHERE username = 'girl'`).Scan(&girlID)
	if int64(gotAuthor.(float64)) != girlID {
		t.Fatalf("author_id = %v，want girl %d", gotAuthor, girlID)
	}
}

// TestInteractionNotifyD6 口径：任何一方动作（含自操作）都通知另一方；
// 覆盖 自赞/自藏/自评 + 互赞四情形（双方各一次点赞均产生通知，取消不产生）。
// 需 girl+boy 两位非 admin 用户才能表达「双方」——admin 动作→girl/boy 收，
// girl/boy 动作→彼此收（操作者被排除，admin 不在情侣里）。
func TestInteractionNotifyD6(t *testing.T) {
	// env 第一返回值=boy、第二返回值=girl；D6 需要 girl 发帖 → 用第二值
	ts, _, girlToken := newPostTestEnv(t)
	boyToken, _ := loginToken(t, ts.URL, "boy", "boy-pw", "app")

	// girl 发动态（作者=girl）
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", girlToken, map[string]any{"content": "D6 通知口径"})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create post: %d", res.StatusCode)
	}

	// ① 自赞：girl 赞自己的动态 → 另一方 boy 收到（收件人是另一方，与作者是否为操作者无关）
	res = doJSON(t, "POST", ts.URL+"/api/v1/posts/1/like", girlToken, nil)
	res.Body.Close()
	// ② 自评：girl 评论自己的动态 → boy 收到
	res = doJSON(t, "POST", ts.URL+"/api/v1/posts/1/comments", girlToken, map[string]string{"content": "自我欣赏"})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("comment: %d", res.StatusCode)
	}

	var boyCount int
	if err := ts.db.QueryRow(`SELECT COUNT(*) FROM notifications n JOIN users u ON u.id = n.recipient_id
		WHERE u.username = 'boy' AND n.event_type IN ('post_liked','post_commented')`).Scan(&boyCount); err != nil {
		t.Fatalf("query boy notifs: %v", err)
	}
	if boyCount != 2 {
		t.Fatalf("boy interaction notifications: %d (want 2: 自赞+自评)", boyCount)
	}
	// girl 自己的动作不通知自己（收件人排除操作者）
	var girlSelf int
	if err := ts.db.QueryRow(`SELECT COUNT(*) FROM notifications n JOIN users u ON u.id = n.recipient_id
		WHERE u.username = 'girl' AND n.event_type IN ('post_liked','post_commented')`).Scan(&girlSelf); err != nil {
		t.Fatalf("query girl self: %v", err)
	}
	if girlSelf != 0 {
		t.Fatalf("girl self notifications: %d (want 0：操作者不在收件人之列)", girlSelf)
	}
	// admin 不在情侣里：girl 的动作不通知 admin
	var adminCount int
	if err := ts.db.QueryRow(`SELECT COUNT(*) FROM notifications n JOIN users u ON u.id = n.recipient_id
		WHERE u.username = 'admin' AND n.event_type IN ('post_liked','post_commented')`).Scan(&adminCount); err != nil {
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

	// ⑤ 取消不产生通知：girl 取消对动态 2 的赞（无通知），toggle 响应契约维持 {liked}
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
}

// TestInteractionNotifyPartnerScope 边界：ListPartnerIDs 收件人域——
// 非 admin 且非操作者（admin 不在情侣里）。
func TestInteractionNotifyPartnerScope(t *testing.T) {
	ts, _, _ := newPostTestEnv(t)
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

// 评论列表分页：默认条数不变（100 以内全回、has_more=false）；limit 收窄后多取一条判 has_more。
func TestListCommentsLimitAndHasMore(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{"content": "评论分页"})
	var post struct {
		ID int64 `json:"id"`
	}
	_ = json.NewDecoder(res.Body).Decode(&post)
	res.Body.Close()
	if post.ID == 0 {
		t.Fatal("前置：未取到动态 id")
	}
	for i := 0; i < 5; i++ {
		r := doJSON(t, "POST", ts.URL+"/api/v1/posts/"+decStr(post.ID)+"/comments", adminToken,
			map[string]any{"content": "评论 " + decStrInt(i)})
		r.Body.Close()
	}

	list := func(query string) (int, bool) {
		res := doJSON(t, "GET", ts.URL+"/api/v1/posts/"+decStr(post.ID)+"/comments"+query, adminToken, nil)
		defer res.Body.Close()
		var body struct {
			Items   []map[string]any `json:"items"`
			HasMore bool             `json:"has_more"`
		}
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
			t.Fatalf("decode comments: %v", err)
		}
		return len(body.Items), body.HasMore
	}

	// 默认：5 条全回，没有更多
	if n, more := list(""); n != 5 || more {
		t.Fatalf("默认列表 = %d,has_more=%v，want 5,false", n, more)
	}
	// limit=3：回 3 条且提示还有更多
	if n, more := list("?limit=3"); n != 3 || !more {
		t.Fatalf("limit=3 = %d,has_more=%v，want 3,true", n, more)
	}
}

// notifyRecorder 按 URL 路径记录推送正文：同一次事件要区分落到群渠道还是某个成员的私人渠道。
type notifyRecorder struct {
	srv    *httptest.Server
	mu     sync.Mutex
	byPath map[string][]string
}

func newNotifyRecorder(t *testing.T) *notifyRecorder {
	t.Helper()
	r := &notifyRecorder{byPath: map[string][]string{}}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		var payload map[string]string
		_ = json.Unmarshal(b, &payload)
		r.mu.Lock()
		r.byPath[req.URL.Path] = append(r.byPath[req.URL.Path], payload["content"])
		r.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *notifyRecorder) texts(path string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.byPath[path]...)
}

// waitFor 等某条路径累计到 n 条推送（外部推送是后台 goroutine，只能等不能立刻断言）。
func (r *notifyRecorder) waitFor(path string, n int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if len(r.texts(path)) >= n {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// 群渠道用第三人称具名、私人渠道用收件人视角，且私人渠道只推给「本次收件人」。
// 三条语义独立，任何一条错了都会让通知发错人或读起来指代不明。
func TestInteractionNotifyTwoViews(t *testing.T) {
	ts, _, girlToken := newPostTestEnv(t)
	boyToken, code := loginToken(t, ts.URL, "boy", "boy-pw", "app")
	if code != http.StatusOK {
		t.Fatalf("boy login: %d", code)
	}
	realAdmin, _ := loginToken(t, ts.URL, "admin", "admin-pw", "admin")
	rec := newNotifyRecorder(t)

	var girlID, boyID int64
	_ = ts.db.QueryRow(`SELECT id FROM users WHERE username = 'girl'`).Scan(&girlID)
	_ = ts.db.QueryRow(`SELECT id FROM users WHERE username = 'boy'`).Scan(&boyID)

	for _, c := range []struct {
		path  string
		owner *int64
	}{
		{"/group", nil},
		{"/girl", &girlID},
		{"/boy", &boyID},
	} {
		res := doJSON(t, "POST", ts.URL+"/api/v1/admin/webhooks", realAdmin, map[string]any{
			"name": c.path, "url": rec.srv.URL + c.path,
			"events": []string{"new_post", "comment"}, "owner_id": c.owner,
		})
		res.Body.Close()
		if res.StatusCode != http.StatusCreated {
			t.Fatalf("create channel %s: %d", c.path, res.StatusCode)
		}
	}

	// boy 发动态 → 收件人 girl
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", boyToken, map[string]any{"content": "晚霞"})
	var created struct {
		ID int64 `json:"id"`
	}
	_ = json.NewDecoder(res.Body).Decode(&created)
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create post: %d", res.StatusCode)
	}
	if !rec.waitFor("/group", 1, 3*time.Second) || !rec.waitFor("/girl", 1, 3*time.Second) {
		t.Fatalf("新动态未推送：群=%v girl=%v", rec.texts("/group"), rec.texts("/girl"))
	}

	// girl 评论 boy 的动态 → 收件人 boy
	res = doJSON(t, "POST", fmt.Sprintf("%s/api/v1/posts/%d/comments", ts.URL, created.ID), girlToken, map[string]any{"content": "真好看"})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("comment: %d", res.StatusCode)
	}
	if !rec.waitFor("/group", 2, 3*time.Second) || !rec.waitFor("/boy", 1, 3*time.Second) {
		t.Fatalf("评论未推送：群=%v boy=%v", rec.texts("/group"), rec.texts("/boy"))
	}

	// 环境里 girl 昵称「她」、boy 昵称「他」
	if got, want := rec.texts("/group")[0], "他 发布了新动态：晚霞"; got != want {
		t.Errorf("群·新动态 = %q，want %q", got, want)
	}
	if got, want := rec.texts("/group")[1], "她 评论了他的动态：真好看"; got != want {
		t.Errorf("群·评论 = %q，want %q", got, want)
	}
	if got, want := rec.texts("/girl")[0], "他 发布了新动态：晚霞"; got != want {
		t.Errorf("girl 私聊·新动态 = %q，want %q", got, want)
	}
	if got, want := rec.texts("/boy")[0], "她 评论了你的动态：真好看"; got != want {
		t.Errorf("boy 私聊·评论 = %q，want %q", got, want)
	}
	// 私人渠道与本次收件人不匹配时必须静默：girl 收不到「自己在对方动态下的评论」
	if got := rec.texts("/girl"); len(got) != 1 {
		t.Errorf("girl 私聊只该有新动态 1 条，实得 %v", got)
	}
}

// 通知里的称呼昵称优先、回退用户名：同一句话里操作者与内容作者必须同口径，
// 否则会拼出「xiaowu 评论了小六的动态」这种半截用户名。
func TestNotificationNamePrefersNickname(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()
	hash, _ := auth.HashPassword("pw")
	nickID, err := ts.db.CreateUser(ctx, &store.User{Username: "nickuser", PasswordHash: hash, Role: "girl", Nickname: "小名"})
	if err != nil {
		t.Fatalf("create nick user: %v", err)
	}
	plainID, err := ts.db.CreateUser(ctx, &store.User{Username: "plainuser", PasswordHash: hash, Role: "boy"})
	if err != nil {
		t.Fatalf("create plain user: %v", err)
	}

	if got := displayName(ctx, ts.db, nickID, "兜底"); got != "小名" {
		t.Errorf("有昵称时应用昵称：%q", got)
	}
	if got := displayName(ctx, ts.db, plainID, "兜底"); got != "plainuser" {
		t.Errorf("无昵称时应回退用户名：%q", got)
	}
	if got := displayName(ctx, ts.db, 99999, "兜底"); got != "兜底" {
		t.Errorf("查不到用户时应回退 fallback：%q", got)
	}
}

// 评论可附图片：带 media_tokens 发表 → 评论与图一起落库、响应与列表都带 media；
// 图片与文字至少一项（纯图片评论合法）；超过上限 / 非图片被拒。
func TestCommentWithMedia(t *testing.T) {
	ts, boyToken, girlToken := newPostTestEnv(t)

	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", boyToken, map[string]any{"content": "海边的一天"})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create post: %d", res.StatusCode)
	}

	// 上传两张图，拿 media_token
	imgBytes := genJPEG(t)
	tok1, code := uploadMedia(t, ts.URL, girlToken, "a.jpg", imgBytes)
	if code != http.StatusOK || tok1 == "" {
		t.Fatalf("upload 1: code=%d token=%q", code, tok1)
	}
	tok2, code := uploadMedia(t, ts.URL, girlToken, "b.jpg", imgBytes)
	if code != http.StatusOK || tok2 == "" {
		t.Fatalf("upload 2: code=%d token=%q", code, tok2)
	}

	// 文字 + 两张图
	res = doJSON(t, "POST", ts.URL+"/api/v1/posts/1/comments", girlToken, map[string]any{
		"content": "好看", "media_tokens": []string{tok1, tok2},
	})
	var created struct {
		ID    int64 `json:"id"`
		Media []struct {
			ID     int64  `json:"id"`
			Kind   string `json:"kind"`
			URL    string `json:"url"`
			Author int64  `json:"author_id"`
		} `json:"media"`
	}
	_ = json.NewDecoder(res.Body).Decode(&created)
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("comment with media: %d", res.StatusCode)
	}
	if len(created.Media) != 2 {
		t.Fatalf("响应应带 2 张图，实得 %d", len(created.Media))
	}
	for _, m := range created.Media {
		if m.Kind != "photo" {
			t.Fatalf("评论图 kind 应为 photo，实得 %s", m.Kind)
		}
		if m.URL == "" {
			t.Fatal("评论图应带签名 URL")
		}
	}

	// 列表回显同样带 media
	res = doJSON(t, "GET", ts.URL+"/api/v1/posts/1/comments", boyToken, nil)
	var list struct {
		Items []struct {
			ID    int64 `json:"id"`
			Media []struct {
				ID int64 `json:"id"`
			} `json:"media"`
		} `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&list)
	res.Body.Close()
	if len(list.Items) != 1 || len(list.Items[0].Media) != 2 {
		t.Fatalf("列表应回显 2 张图，实得 %+v", list.Items)
	}

	// 动态自己的九宫格不含评论图
	res = doJSON(t, "GET", ts.URL+"/api/v1/posts", boyToken, nil)
	var posts struct {
		Items []struct {
			ID    int64 `json:"id"`
			Media []struct {
				ID int64 `json:"id"`
			} `json:"media"`
		} `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&posts)
	res.Body.Close()
	if len(posts.Items) != 1 || len(posts.Items[0].Media) != 0 {
		t.Fatalf("动态 media 不应含评论图，实得 %+v", posts.Items)
	}

	// 纯图片评论（content 为空）合法
	tok3, code := uploadMedia(t, ts.URL, girlToken, "c.jpg", imgBytes)
	if code != http.StatusOK {
		t.Fatalf("upload 3: %d", code)
	}
	res = doJSON(t, "POST", ts.URL+"/api/v1/posts/1/comments", girlToken, map[string]any{
		"content": "", "media_tokens": []string{tok3},
	})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("纯图片评论应成功，实得 %d", res.StatusCode)
	}

	// 文字与图片都没有 → 400
	res = doJSON(t, "POST", ts.URL+"/api/v1/posts/1/comments", girlToken, map[string]any{"content": ""})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("空评论应 400，实得 %d", res.StatusCode)
	}
}

// 评论只收图片：非 photo 的 token 被拒，且已转正的文件要被清理（不留孤儿）。
func TestCommentRejectsNonPhoto(t *testing.T) {
	ts, boyToken, girlToken := newPostTestEnv(t)
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", boyToken, map[string]any{"content": "动态"})
	res.Body.Close()

	tok, code := uploadMedia(t, ts.URL, girlToken, "voice.mp3", []byte("fake-audio-bytes"))
	if code != http.StatusOK {
		t.Fatalf("upload audio: %d", code)
	}
	res = doJSON(t, "POST", ts.URL+"/api/v1/posts/1/comments", girlToken, map[string]any{
		"content": "听这个", "media_tokens": []string{tok},
	})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("音频评论应 400，实得 %d", res.StatusCode)
	}
	// 评论未落库
	var n int
	if err := ts.db.QueryRow(`SELECT COUNT(*) FROM comments`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("拒绝后不应留下评论，实得 %d 条", n)
	}
}

// 超过单条评论图片上限 → 400。
func TestCommentMediaLimit(t *testing.T) {
	ts, boyToken, girlToken := newPostTestEnv(t)
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", boyToken, map[string]any{"content": "动态"})
	res.Body.Close()

	toks := make([]string, 0, 10)
	for i := 0; i < 10; i++ {
		tok, code := uploadMedia(t, ts.URL, girlToken, fmt.Sprintf("p%d.jpg", i), genJPEG(t))
		if code != http.StatusOK {
			t.Fatalf("upload %d: %d", i, code)
		}
		toks = append(toks, tok)
	}
	res = doJSON(t, "POST", ts.URL+"/api/v1/posts/1/comments", girlToken, map[string]any{
		"content": "太多了", "media_tokens": toks,
	})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("超过上限应 400，实得 %d", res.StatusCode)
	}
}

// 评论图可单独删除：作者能删、对方不能删；删掉纯图评论的最后一张时响应带 comment_deleted。
func TestDeleteCommentMedia(t *testing.T) {
	ts, boyToken, girlToken := newPostTestEnv(t)
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", boyToken, map[string]any{"content": "动态"})
	res.Body.Close()

	tok1, _ := uploadMedia(t, ts.URL, girlToken, "a.jpg", genJPEG(t))
	tok2, _ := uploadMedia(t, ts.URL, girlToken, "b.jpg", genJPEG(t))
	res = doJSON(t, "POST", ts.URL+"/api/v1/posts/1/comments", girlToken, map[string]any{
		"content": "", "media_tokens": []string{tok1, tok2},
	})
	var created struct {
		ID    int64 `json:"id"`
		Media []struct {
			ID int64 `json:"id"`
		} `json:"media"`
	}
	_ = json.NewDecoder(res.Body).Decode(&created)
	res.Body.Close()
	if len(created.Media) != 2 {
		t.Fatalf("应创建 2 张图，实得 %d", len(created.Media))
	}

	// 对方（动态作者 boy）不能删 girl 评论里的图
	res = doJSON(t, "DELETE", fmt.Sprintf("%s/api/v1/media/%d", ts.URL, created.Media[0].ID), boyToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("删他人评论图应 403，实得 %d", res.StatusCode)
	}

	// 本人删第一张：评论仍有图 → comment_deleted=false
	res = doJSON(t, "DELETE", fmt.Sprintf("%s/api/v1/media/%d", ts.URL, created.Media[0].ID), girlToken, nil)
	var delRes struct {
		PostDeleted    bool `json:"post_deleted"`
		CommentDeleted bool `json:"comment_deleted"`
	}
	_ = json.NewDecoder(res.Body).Decode(&delRes)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || delRes.CommentDeleted || delRes.PostDeleted {
		t.Fatalf("删一张图：code=%d comment_deleted=%v post_deleted=%v", res.StatusCode, delRes.CommentDeleted, delRes.PostDeleted)
	}

	// 本人删最后一张（评论无正文）→ comment_deleted=true
	res = doJSON(t, "DELETE", fmt.Sprintf("%s/api/v1/media/%d", ts.URL, created.Media[1].ID), girlToken, nil)
	_ = json.NewDecoder(res.Body).Decode(&delRes)
	res.Body.Close()
	if !delRes.CommentDeleted {
		t.Fatalf("删掉纯图评论最后一张应连带评论，实得 comment_deleted=%v", delRes.CommentDeleted)
	}
	// 评论列表已空
	res = doJSON(t, "GET", ts.URL+"/api/v1/posts/1/comments", girlToken, nil)
	var list struct {
		Items []any `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&list)
	res.Body.Close()
	if len(list.Items) != 0 {
		t.Fatalf("评论应已被删，实得 %d 条", len(list.Items))
	}
}

// 评论图不能加进相册：这是调用方输入问题，必须 400 而不是 500。
func TestCommentMediaCannotJoinAlbumAPI(t *testing.T) {
	ts, boyToken, girlToken := newPostTestEnv(t)
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", boyToken, map[string]any{"content": "动态"})
	res.Body.Close()

	tok, _ := uploadMedia(t, ts.URL, girlToken, "c.jpg", genJPEG(t))
	res = doJSON(t, "POST", ts.URL+"/api/v1/posts/1/comments", girlToken, map[string]any{
		"content": "", "media_tokens": []string{tok},
	})
	var created struct {
		Media []struct {
			ID int64 `json:"id"`
		} `json:"media"`
	}
	_ = json.NewDecoder(res.Body).Decode(&created)
	res.Body.Close()
	if len(created.Media) != 1 {
		t.Fatalf("评论应带 1 张图，实得 %d", len(created.Media))
	}

	res = doJSON(t, "POST", ts.URL+"/api/v1/albums", girlToken, map[string]any{"name": "相册"})
	var album struct {
		ID int64 `json:"id"`
	}
	_ = json.NewDecoder(res.Body).Decode(&album)
	res.Body.Close()

	res = doJSON(t, "POST", fmt.Sprintf("%s/api/v1/albums/%d/photos", ts.URL, album.ID), girlToken,
		map[string]any{"media_ids": []int64{created.Media[0].ID}})
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("评论图加入相册应 400，实得 %d：%s", res.StatusCode, body)
	}
	if !strings.Contains(string(body), "评论里的图片") {
		t.Fatalf("错误文案应说明是评论图，实得：%s", body)
	}
}

// 非图片被拒后，已转正的文件必须清理（不留磁盘孤儿）。
// 混合 token（先图后音频）时，先前转正的图也要一起清掉。
func TestCommentRejectsNonPhotoCleansPromoted(t *testing.T) {
	ts, boyToken, girlToken := newPostTestEnv(t)
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", boyToken, map[string]any{"content": "动态"})
	res.Body.Close()

	imgTok, _ := uploadMedia(t, ts.URL, girlToken, "ok.jpg", genJPEG(t))
	audioTok, _ := uploadMedia(t, ts.URL, girlToken, "bad.mp3", []byte("fake-audio"))

	res = doJSON(t, "POST", ts.URL+"/api/v1/posts/1/comments", girlToken, map[string]any{
		"content": "混合", "media_tokens": []string{imgTok, audioTok},
	})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("应 400，实得 %d", res.StatusCode)
	}

	var n int
	if err := ts.db.QueryRow(`SELECT COUNT(*) FROM media`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("拒绝后不应留下 media 行，实得 %d", n)
	}
	if err := ts.db.QueryRow(`SELECT COUNT(*) FROM comments`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("拒绝后不应留下评论，实得 %d", n)
	}
}

// REST 评论接口的字数上限（500 字）必须与服务端契约一致。
func TestCommentContentLengthLimit(t *testing.T) {
	ts, boyToken, girlToken := newPostTestEnv(t)
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", boyToken, map[string]any{"content": "动态"})
	res.Body.Close()

	res = doJSON(t, "POST", ts.URL+"/api/v1/posts/1/comments", girlToken, map[string]any{
		"content": strings.Repeat("字", 501),
	})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("超 500 字应 400，实得 %d", res.StatusCode)
	}
	res = doJSON(t, "POST", ts.URL+"/api/v1/posts/1/comments", girlToken, map[string]any{
		"content": strings.Repeat("字", 500),
	})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("500 字应通过，实得 %d", res.StatusCode)
	}
}

// 删掉最后一张图但评论有正文时，评论必须保留（只删图）。
func TestDeleteLastCommentMediaKeepsCommentWithContent(t *testing.T) {
	ts, boyToken, girlToken := newPostTestEnv(t)
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", boyToken, map[string]any{"content": "动态"})
	res.Body.Close()

	tok, _ := uploadMedia(t, ts.URL, girlToken, "only.jpg", genJPEG(t))
	res = doJSON(t, "POST", ts.URL+"/api/v1/posts/1/comments", girlToken, map[string]any{
		"content": "有正文", "media_tokens": []string{tok},
	})
	var created struct {
		ID    int64 `json:"id"`
		Media []struct {
			ID int64 `json:"id"`
		} `json:"media"`
	}
	_ = json.NewDecoder(res.Body).Decode(&created)
	res.Body.Close()

	res = doJSON(t, "DELETE", fmt.Sprintf("%s/api/v1/media/%d", ts.URL, created.Media[0].ID), girlToken, nil)
	var delRes struct {
		CommentDeleted bool `json:"comment_deleted"`
	}
	_ = json.NewDecoder(res.Body).Decode(&delRes)
	res.Body.Close()
	if delRes.CommentDeleted {
		t.Fatal("评论有正文，删最后一张图不应连带删评论")
	}
	res = doJSON(t, "GET", ts.URL+"/api/v1/posts/1/comments", girlToken, nil)
	var list struct {
		Items []struct {
			Content string `json:"content"`
			Media   []any  `json:"media"`
		} `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&list)
	res.Body.Close()
	if len(list.Items) != 1 || list.Items[0].Content != "有正文" || len(list.Items[0].Media) != 0 {
		t.Fatalf("评论应保留且无图，实得 %+v", list.Items)
	}
}

// 另外三类「媒体不能进相册」的拒绝同样必须是 400（不是 500）。
func TestAlbumRejectionsAre400(t *testing.T) {
	ts, boyToken, girlToken := newPostTestEnv(t)
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", boyToken, map[string]any{"content": "动态"})
	res.Body.Close()

	res = doJSON(t, "POST", ts.URL+"/api/v1/albums", girlToken, map[string]any{"name": "相册"})
	var album struct {
		ID int64 `json:"id"`
	}
	_ = json.NewDecoder(res.Body).Decode(&album)
	res.Body.Close()

	// 1) 媒体不存在
	res = doJSON(t, "POST", fmt.Sprintf("%s/api/v1/albums/%d/photos", ts.URL, album.ID), girlToken,
		map[string]any{"media_ids": []int64{99999}})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("不存在的媒体应 400，实得 %d", res.StatusCode)
	}

	// 2) 媒体已软删
	tok, _ := uploadMedia(t, ts.URL, girlToken, "del.jpg", genJPEG(t))
	res = doJSON(t, "POST", ts.URL+"/api/v1/posts/1/comments", girlToken, map[string]any{
		"content": "x", "media_tokens": []string{tok},
	})
	var created struct {
		Media []struct {
			ID int64 `json:"id"`
		} `json:"media"`
	}
	_ = json.NewDecoder(res.Body).Decode(&created)
	res.Body.Close()
	doJSON(t, "DELETE", fmt.Sprintf("%s/api/v1/media/%d", ts.URL, created.Media[0].ID), girlToken, nil)
	res = doJSON(t, "POST", fmt.Sprintf("%s/api/v1/albums/%d/photos", ts.URL, album.ID), girlToken,
		map[string]any{"media_ids": []int64{created.Media[0].ID}})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("已软删的媒体应 400，实得 %d", res.StatusCode)
	}
}

// 给不存在的动态发评论 → 404（不是 500，也不能静默建出孤儿评论）。
func TestCommentOnMissingPost(t *testing.T) {
	ts, _, girlToken := newPostTestEnv(t)
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts/99999/comments", girlToken, map[string]any{
		"content": "评论一条不存在的动态",
	})
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("应对不存在的动态返回 404，实得 %d：%s", res.StatusCode, body)
	}
	var n int
	if err := ts.db.QueryRow(`SELECT COUNT(*) FROM comments`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("不应留下孤儿评论，实得 %d 条", n)
	}
}
