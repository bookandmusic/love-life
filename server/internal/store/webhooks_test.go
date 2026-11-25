package store

import (
	"context"
	"database/sql"
	"testing"
)

func newWebhookTestDB(t *testing.T) (*DB, int64, int64) {
	t.Helper()
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	liu, err := db.CreateUser(ctx, &User{Username: "xiaoliu", PasswordHash: "x", Role: "girl", Nickname: "小六"})
	if err != nil {
		t.Fatalf("create 小六: %v", err)
	}
	wu, err := db.CreateUser(ctx, &User{Username: "xiaowu", PasswordHash: "x", Role: "boy", Nickname: "小五"})
	if err != nil {
		t.Fatalf("create 小五: %v", err)
	}
	return db, liu, wu
}

// 渠道归属的三种状态都要能往返读写：空间级（owner 为 NULL）、指定成员、清空归属。
// 归属落 user id 而非名字——改昵称不该让已有渠道串位。
func TestWebhookChannelOwnerRoundTrip(t *testing.T) {
	db, liuID, wuID := newWebhookTestDB(t)
	ctx := context.Background()

	sharedID, err := db.CreateWebhookChannel(ctx, &WebhookChannel{
		Name: "群", Method: "POST", URL: "https://example.invalid/group", Events: []string{"like"},
	})
	if err != nil {
		t.Fatalf("create shared: %v", err)
	}
	liuChID, err := db.CreateWebhookChannel(ctx, &WebhookChannel{
		Name: "小六私聊", Method: "POST", URL: "https://example.invalid/liu", Events: []string{"like"},
		OwnerID: sql.NullInt64{Int64: liuID, Valid: true},
	})
	if err != nil {
		t.Fatalf("create personal: %v", err)
	}

	// 读取：空间级 Valid=false，私人渠道带回 id
	got, err := db.GetWebhookChannel(ctx, sharedID)
	if err != nil {
		t.Fatalf("get shared: %v", err)
	}
	if !got.IsShared() || got.OwnerID.Valid {
		t.Fatalf("空间级渠道应无归属：%+v", got.OwnerID)
	}
	got, err = db.GetWebhookChannel(ctx, liuChID)
	if err != nil {
		t.Fatalf("get personal: %v", err)
	}
	if got.IsShared() || !got.OwnerID.Valid || got.OwnerID.Int64 != liuID {
		t.Fatalf("私人渠道归属应为 #%d：%+v", liuID, got.OwnerID)
	}

	// 改归属：小六 → 小五
	got.OwnerID = sql.NullInt64{Int64: wuID, Valid: true}
	if err := db.UpdateWebhookChannel(ctx, got); err != nil {
		t.Fatalf("update owner: %v", err)
	}
	got, _ = db.GetWebhookChannel(ctx, liuChID)
	if got.OwnerID.Int64 != wuID {
		t.Fatalf("归属改后 = %d，want %d", got.OwnerID.Int64, wuID)
	}

	// 清空归属即转为空间级：编辑渠道时省略 owner_id 就是这个结果
	got.OwnerID = sql.NullInt64{}
	if err := db.UpdateWebhookChannel(ctx, got); err != nil {
		t.Fatalf("clear owner: %v", err)
	}
	got, _ = db.GetWebhookChannel(ctx, liuChID)
	if !got.IsShared() {
		t.Fatalf("清空归属后应为空间级：%+v", got.OwnerID)
	}
}

// 订阅过滤只看事件，不看归属：归属过滤在投递侧做（notify.Dispatcher），
// 两者是两道独立的闸，查询层混进来会让「群 + 私人」同事件的两个渠道互相顶掉。
func TestListWebhookChannelsByEventIgnoresOwner(t *testing.T) {
	db, liuID, _ := newWebhookTestDB(t)
	ctx := context.Background()

	specs := []*WebhookChannel{
		{Name: "群", URL: "https://example.invalid/group", Events: []string{"like", "comment"}},
		{Name: "小六", URL: "https://example.invalid/liu", Events: []string{"like"}, OwnerID: sql.NullInt64{Int64: liuID, Valid: true}},
		{Name: "别的", URL: "https://example.invalid/other", Events: []string{"album"}},
	}
	for _, s := range specs {
		s.Method = "POST"
		if _, err := db.CreateWebhookChannel(ctx, s); err != nil {
			t.Fatalf("create %s: %v", s.Name, err)
		}
	}

	like, err := db.ListWebhookChannelsByEvent(ctx, "like")
	if err != nil {
		t.Fatalf("list like: %v", err)
	}
	if len(like) != 2 {
		t.Fatalf("like 订阅应命中 2 条（群 + 小六），实得 %d", len(like))
	}
	album, _ := db.ListWebhookChannelsByEvent(ctx, "album")
	if len(album) != 1 || album[0].Name != "别的" {
		t.Fatalf("album 订阅应只有 1 条：%+v", album)
	}
	none, _ := db.ListWebhookChannelsByEvent(ctx, "footprint")
	if len(none) != 0 {
		t.Fatalf("无人订阅 footprint，实得 %d", len(none))
	}
}
