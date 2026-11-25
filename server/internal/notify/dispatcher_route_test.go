package notify

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/store"
)

// dispatchFixture 起一个能记录每次推送正文的接收端，并建好「一对情侣 + 三条渠道」：
// 一条空间级（群 / 共享，owner NULL）与两人各自的私人渠道，全部订阅点赞事件。
type dispatchFixture struct {
	db     *store.DB
	liuID  int64
	wuID   int64
	server *httptest.Server

	mu   sync.Mutex
	body map[string]string // path → content
}

func newDispatchFixture(t *testing.T) *dispatchFixture {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	liuID, err := db.CreateUser(ctx, &store.User{Username: "xiaoliu", PasswordHash: "x", Role: "girl", Nickname: "小六"})
	if err != nil {
		t.Fatalf("create 小六: %v", err)
	}
	wuID, err := db.CreateUser(ctx, &store.User{Username: "xiaowu", PasswordHash: "x", Role: "boy", Nickname: "小五"})
	if err != nil {
		t.Fatalf("create 小五: %v", err)
	}

	f := &dispatchFixture{db: db, liuID: liuID, wuID: wuID, body: map[string]string{}}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var payload map[string]string
		_ = json.Unmarshal(raw, &payload)
		f.mu.Lock()
		f.body[r.URL.Path] = payload["content"]
		f.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(f.server.Close)

	for _, ch := range []*store.WebhookChannel{
		{Name: "群", URL: f.server.URL + "/group"},
		{Name: "小六私聊", URL: f.server.URL + "/liu", OwnerID: sql.NullInt64{Int64: liuID, Valid: true}},
		{Name: "小五私聊", URL: f.server.URL + "/wu", OwnerID: sql.NullInt64{Int64: wuID, Valid: true}},
	} {
		ch.Method = "POST"
		ch.Events = []string{"like"}
		ch.Headers = map[string]string{}
		if _, err := db.CreateWebhookChannel(ctx, ch); err != nil {
			t.Fatalf("create channel %s: %v", ch.Name, err)
		}
	}
	return f
}

func (f *dispatchFixture) received(path string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.body[path]
}

// 私人渠道只推给「本次收件人里正好是它主人」的那一个：小六赞小五的动态，
// 收件人只有小五，所以小六自己的私聊不能收到这条通知。
func TestDispatchPersonalChannelOnlyReachesOwner(t *testing.T) {
	f := newDispatchFixture(t)
	ctx := context.Background()

	d := New(f.db)
	d.Dispatch(ctx, []int64{f.wuID}, "post_liked", "post", 1,
		map[string]any{"operator": "小六", "author_id": f.wuID}, "收到点赞",
		Liked("小六", "小五", false))
	d.Wait()

	// 空间级渠道：旁观者视角，具名指明是谁的动态
	if got, want := f.received("/group"), "小六 赞了小五的动态"; got != want {
		t.Errorf("群渠道正文 = %q，want %q", got, want)
	}
	// 收件人的私人渠道：与 APP 站内通知同口径
	if got, want := f.received("/wu"), "小六 赞了你的动态"; got != want {
		t.Errorf("小五私聊正文 = %q，want %q", got, want)
	}
	// 操作者自己的私人渠道：不该收到
	if got := f.received("/liu"); got != "" {
		t.Errorf("小六私聊不应收到该通知，实得 %q", got)
	}
}

// 站内通知快照带渲染好的句子，客户端不再自己拼文案。
func TestDispatchSnapshotCarriesText(t *testing.T) {
	f := newDispatchFixture(t)
	ctx := context.Background()

	d := New(f.db)
	d.Dispatch(ctx, []int64{f.wuID}, "post_liked", "post", 7,
		map[string]any{"operator": "小六", "author_id": f.wuID}, "收到点赞",
		Liked("小六", "小五", false))
	d.Wait()

	list, err := f.db.ListNotifications(ctx, f.wuID, 10, 0)
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("小五应收 1 条站内通知，实得 %d", len(list))
	}
	var snap map[string]any
	if err := json.Unmarshal([]byte(list[0].Snapshot.String), &snap); err != nil {
		t.Fatalf("快照非法 JSON: %v", err)
	}
	if got, want := snap["text"], "小六 赞了你的动态"; got != want {
		t.Errorf("快照 text = %v，want %q", got, want)
	}
	// 操作者不收自己的动作
	if n := mustCount(t, f.db, f.liuID); n != 0 {
		t.Errorf("小六不应收到站内通知，实得 %d", n)
	}
}

// 没有收件人时私人渠道全部静默：否则「只给某人看」的通知会漏给无关的人。
func TestDispatchPersonalChannelSilentWithoutRecipients(t *testing.T) {
	f := newDispatchFixture(t)

	d := New(f.db)
	d.Dispatch(context.Background(), nil, "post_liked", "post", 1,
		map[string]any{"operator": "小六"}, "收到点赞", Liked("小六", "小五", false))
	d.Wait()

	if got := f.received("/wu"); got != "" {
		t.Errorf("无收件人时小五私聊不应收到，实得 %q", got)
	}
	if got := f.received("/liu"); got != "" {
		t.Errorf("无收件人时小六私聊不应收到，实得 %q", got)
	}
	// 空间级渠道不受收件人影响，照常推送
	if got := f.received("/group"); got != "小六 赞了小五的动态" {
		t.Errorf("群渠道应照常推送，实得 %q", got)
	}
}

// 未订阅该事件的渠道不收：订阅过滤与归属过滤是两道独立的闸。
func TestDispatchSkipsUnsubscribedEvent(t *testing.T) {
	f := newDispatchFixture(t)

	d := New(f.db)
	d.Dispatch(context.Background(), []int64{f.wuID}, "post_commented", "post", 1,
		map[string]any{"operator": "小六"}, "收到评论", Commented("小六", "小五", "好看", false))
	d.Wait()

	if got := f.received("/group"); got != "" {
		t.Errorf("未订阅 comment 的渠道不应收到，实得 %q", got)
	}
}

func mustCount(t *testing.T, db *store.DB, uid int64) int {
	t.Helper()
	n, err := db.UnreadNotificationCount(context.Background(), uid)
	if err != nil {
		t.Fatalf("count unread: %v", err)
	}
	return n
}

// 群视角文案里不允许出现第二人称：群里没有收件人，「你」无所指。
func TestGroupTextHasNoSecondPerson(t *testing.T) {
	for _, texts := range []Texts{
		Liked("小六", "小五", false),
		Commented("小六", "小五", "好看", false),
	} {
		if strings.Contains(texts.Group, "你") {
			t.Errorf("群视角不应出现「你」：%q", texts.Group)
		}
	}
}

// 渠道归属决定视角：空间级用群视角，个人渠道用收件人视角；
// 群视角缺失时回落收件人视角——不能让渠道收到空正文。
func TestForChannelPicksViewByOwner(t *testing.T) {
	shared := &store.WebhookChannel{}
	personal := &store.WebhookChannel{OwnerID: sql.NullInt64{Int64: 1, Valid: true}}
	both := Texts{Personal: "私", Group: "群"}

	if got := both.forChannel(shared); got != "群" {
		t.Errorf("空间级渠道应用群视角：%q", got)
	}
	if got := both.forChannel(personal); got != "私" {
		t.Errorf("个人渠道应用收件人视角：%q", got)
	}
	if got := (Texts{Personal: "私"}).forChannel(shared); got != "私" {
		t.Errorf("群视角缺失应回落收件人视角：%q", got)
	}
}
