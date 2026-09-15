package tools

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/bookandmusic/love-life/server/internal/auth"
	"github.com/bookandmusic/love-life/server/internal/media"
	"github.com/bookandmusic/love-life/server/internal/notify"
	"github.com/bookandmusic/love-life/server/internal/store"
)

func findTool(t *testing.T, name string) *Tool {
	t.Helper()
	for _, tool := range Registry() {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q not registered", name)
	return nil
}

func newToolDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	// created_by 外键引用 users(id)
	hash, _ := auth.HashPassword("pw")
	if _, err := db.CreateUser(t.Context(), &store.User{Username: "girl", PasswordHash: hash, Role: "girl"}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return db
}

// create_map_point：创建 / 同名复用 / 审计落库 / 仅新建通知另一方；delete_map_point：软删 / 二次删报错。
func TestMapPointTools(t *testing.T) {
	db := newToolDB(t)
	d := &Deps{DB: db, DP: notify.New(db)}
	ctx := t.Context()
	// 收件人：newToolDB 只建了 girl(1)，补 boy(2) 才能观察「通知另一方」
	hash, _ := auth.HashPassword("pw")
	if _, err := db.CreateUser(ctx, &store.User{Username: "boy", PasswordHash: hash, Role: "boy"}); err != nil {
		t.Fatalf("create boy: %v", err)
	}

	create := findTool(t, "create_map_point")
	res, err := create.Exec(ctx, d, 1, map[string]any{"name": "杭州·西湖", "latitude": 30.2467, "longitude": 120.4599, "visit_date": "2026-09-03", "description": "湖边散步"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	data := res.Data.(map[string]any)
	if data["created"] != true || data["point_id"].(int64) == 0 {
		t.Fatalf("create data: %+v", data)
	}
	pointID := data["point_id"].(int64)

	// 新建 → 另一方收到 map_point_created，快照含真实 operator 与 via 标记
	assertMapPointNotif(t, db, 1)

	// 同名复用
	res, err = create.Exec(ctx, d, 1, map[string]any{"name": "杭州·西湖", "latitude": 1.0, "longitude": 1.0})
	if err != nil {
		t.Fatalf("reuse: %v", err)
	}
	data = res.Data.(map[string]any)
	if data["created"] != false || data["point_id"].(int64) != pointID {
		t.Fatalf("reuse data: %+v (want id=%d)", data, pointID)
	}
	// 复用不重复通知
	assertMapPointNotif(t, db, 1)

	// name 空 → 报错
	if _, err = create.Exec(ctx, d, 1, map[string]any{}); err == nil {
		t.Fatal("empty name: want error")
	}

	// 缺坐标 → 报错（DESIGN §6.3：latitude/longitude 必填）
	if _, err = create.Exec(ctx, d, 1, map[string]any{"name": "无名地"}); err == nil {
		t.Fatal("missing latitude/longitude: want error")
	}

	// RegistryForPAT：删除类（AppOnly）工具不出现在 PAT 通道工具表
	for _, tool := range RegistryForPAT() {
		if tool.AppOnly {
			t.Fatalf("RegistryForPAT leaked AppOnly tool %q", tool.Name)
		}
	}
	if findTool(t, "delete_map_point").AppOnly != true {
		t.Fatal("delete_map_point must be AppOnly")
	}

	// 删除 → 成功；再删 → 报错
	del := findTool(t, "delete_map_point")
	if res, err = del.Exec(ctx, d, 1, map[string]any{"point_id": float64(pointID)}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err = del.Exec(ctx, d, 1, map[string]any{"point_id": float64(pointID)}); err == nil {
		t.Fatal("re-delete: want error")
	}

	// 审计：写工具的审计现在由 chat confirm 流程统一处理，单元测试不再检查审计记录
}

// T1：get_next_anniversary 取最近一个，输出含 type 与 next_solar_date（DESIGN §6.4/§12.2）。
func TestNextAnniversaryTool(t *testing.T) {
	db := newToolDB(t)
	d := &Deps{DB: db, DP: notify.New(db)}
	ctx := t.Context()
	now := time.Now()
	if _, err := db.CreateAnniversary(ctx, "生日", now.AddDate(0, 0, 10).Format("2006-01-02"), "solar", false, 1, 1); err != nil {
		t.Fatalf("create a1: %v", err)
	}
	if _, err := db.CreateAnniversary(ctx, "领证", now.AddDate(0, 0, 3).Format("2006-01-02"), "solar", false, 1, 1); err != nil {
		t.Fatalf("create a2: %v", err)
	}
	res, err := findTool(t, "get_next_anniversary").Exec(ctx, d, 1, map[string]any{})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	data := res.Data.(map[string]any)
	if data["name"] != "领证" || data["type"] != "solar" {
		t.Fatalf("next anniversary: %+v", data)
	}
	if s, _ := data["next_solar_date"].(string); s == "" {
		t.Fatalf("next_solar_date missing: %+v", data)
	}
}

// S2：create_anniversary 的提醒天数——未传默认 1 天，显式 0 保留为当天提醒（契约同 REST/mock）。
func TestCreateAnniversaryRemindDefault(t *testing.T) {
	db := newToolDB(t)
	d := &Deps{DB: db, DP: notify.New(db)}
	ctx := t.Context()
	create := findTool(t, "create_anniversary")

	res, err := create.Exec(ctx, d, 1, map[string]any{"name": "缺省提醒", "date": "2026-11-11"})
	if err != nil {
		t.Fatalf("create without remind: %v", err)
	}
	id := res.Data.(map[string]any)["anniversary_id"].(int64)
	if got := anniversaryRemindDays(t, db, id); got != 1 {
		t.Fatalf("未传提醒天数应为 1（提前 1 天），got %d", got)
	}

	res, err = create.Exec(ctx, d, 1, map[string]any{"name": "当天提醒", "date": "2026-12-12", "remind_days_before": float64(0)})
	if err != nil {
		t.Fatalf("create with remind=0: %v", err)
	}
	id = res.Data.(map[string]any)["anniversary_id"].(int64)
	if got := anniversaryRemindDays(t, db, id); got != 0 {
		t.Fatalf("显式 0 应保留为当天提醒，got %d", got)
	}

	// GAP19：lunar + is_leap=true 应落库为 1（DESIGN §12.2 / REQUIREMENTS §4.6）
	res, err = create.Exec(ctx, d, 1, map[string]any{"name": "闰六月纪念日", "date": "2025-06-01", "type": "lunar", "is_leap": true})
	if err != nil {
		t.Fatalf("create lunar is_leap=true: %v", err)
	}
	id = res.Data.(map[string]any)["anniversary_id"].(int64)
	if got := anniversaryIsLeap(t, db, id); got != 1 {
		t.Fatalf("lunar is_leap=true 应落库 1，got %d", got)
	}
	// solar + is_leap=true 应被忽略（恒 0）
	res, err = create.Exec(ctx, d, 1, map[string]any{"name": "公历忽略闰月", "date": "2026-11-11", "type": "solar", "is_leap": true})
	if err != nil {
		t.Fatalf("create solar is_leap=true: %v", err)
	}
	id = res.Data.(map[string]any)["anniversary_id"].(int64)
	if got := anniversaryIsLeap(t, db, id); got != 0 {
		t.Fatalf("solar is_leap 应被忽略恒 0，got %d", got)
	}
}

func anniversaryIsLeap(t *testing.T, db *store.DB, id int64) int {
	t.Helper()
	var v int
	if err := db.QueryRow(`SELECT is_leap FROM anniversaries WHERE id = ?`, id).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// GAP20：create_post 的 location_name 入参应落库到 posts.location_name 列（DESIGN §6.1 / mock fixtures 契约）。
func TestCreatePostLocationName(t *testing.T) {
	db := newToolDB(t)
	d := &Deps{DB: db, DP: notify.New(db)}
	ctx := t.Context()
	hash, _ := auth.HashPassword("pw")
	if _, err := db.CreateUser(ctx, &store.User{Username: "poster", PasswordHash: hash, Role: "girl"}); err != nil {
		t.Fatal(err)
	}
	create := findTool(t, "create_post")

	// 带 location_name
	res, err := create.Exec(ctx, d, 1, map[string]any{"content": "海边散步", "location_name": "厦门 · 鼓浪屿"})
	if err != nil {
		t.Fatalf("create_post with location_name: %v", err)
	}
	id := res.Data.(map[string]any)["post_id"].(int64)
	var loc sql.NullString
	if err := db.QueryRow(`SELECT location_name FROM posts WHERE id = ?`, id).Scan(&loc); err != nil {
		t.Fatal(err)
	}
	if !loc.Valid || loc.String != "厦门 · 鼓浪屿" {
		t.Fatalf("location_name 落库 = %q valid=%v, want \"厦门 · 鼓浪屿\"", loc.String, loc.Valid)
	}

	// 不带 location_name → 落 NULL
	res, err = create.Exec(ctx, d, 1, map[string]any{"content": "无地点"})
	if err != nil {
		t.Fatalf("create_post without location_name: %v", err)
	}
	id = res.Data.(map[string]any)["post_id"].(int64)
	if err := db.QueryRow(`SELECT location_name FROM posts WHERE id = ?`, id).Scan(&loc); err != nil {
		t.Fatal(err)
	}
	if loc.Valid {
		t.Fatalf("未传 location_name 应落 NULL，got %q", loc.String)
	}
}

func anniversaryRemindDays(t *testing.T, db *store.DB, id int64) int {
	t.Helper()
	var days int
	if err := db.QueryRow(`SELECT remind_days_before FROM anniversaries WHERE id = ?`, id).Scan(&days); err != nil {
		t.Fatal(err)
	}
	return days
}

// GAP2（AI 通道）：AI 代删对方创建的内容 → 站内通知创建者；自删静默。覆盖相册/纪念日/足迹三类。
func TestAIDeleteNotifiesCreator(t *testing.T) {
	db := newToolDB(t)
	d := &Deps{DB: db, DP: notify.New(db)}
	ctx := t.Context()
	hash, _ := auth.HashPassword("pw")
	if _, err := db.CreateUser(ctx, &store.User{Username: "boy", PasswordHash: hash, Role: "boy"}); err != nil {
		t.Fatalf("create boy: %v", err)
	}
	notif := func(eventType string, targetID int64) int {
		t.Helper()
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE recipient_id = 1 AND event_type = ? AND target_id = ?`, eventType, targetID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// 相册：user1 建 → user2 代删 → user1 收到
	albumID, err := db.CreateAlbum(ctx, "manual", "AI删相册", "", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := findTool(t, "delete_album").Exec(ctx, d, 2, map[string]any{"album_id": float64(albumID)}); err != nil {
		t.Fatalf("ai delete album: %v", err)
	}
	if n := notif("album_deleted", albumID); n != 1 {
		t.Fatalf("AI 代删对方相册应通知创建者 1 条，got %d", n)
	}

	// 纪念日：user1 建 → user2 代删 → user1 收到
	annID, err := db.CreateAnniversary(ctx, "AI删纪念日", "2020-06-06", "solar", false, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := findTool(t, "delete_anniversary").Exec(ctx, d, 2, map[string]any{"anniversary_id": float64(annID)}); err != nil {
		t.Fatalf("ai delete anniversary: %v", err)
	}
	if n := notif("anniversary_deleted", annID); n != 1 {
		t.Fatalf("AI 代删对方纪念日应通知创建者 1 条，got %d", n)
	}

	// 足迹：user1 建 → user2 代删 → user1 收到
	res, err := findTool(t, "create_map_point").Exec(ctx, d, 1, map[string]any{"name": "AI删足迹", "latitude": 30.1, "longitude": 120.1})
	if err != nil {
		t.Fatal(err)
	}
	pointID := res.Data.(map[string]any)["point_id"].(int64)
	if _, err := findTool(t, "delete_map_point").Exec(ctx, d, 2, map[string]any{"point_id": float64(pointID)}); err != nil {
		t.Fatalf("ai delete point: %v", err)
	}
	if n := notif("map_point_deleted", pointID); n != 1 {
		t.Fatalf("AI 代删对方足迹应通知创建者 1 条，got %d", n)
	}

	// 自删静默：user2 建相册 → user2 自己删
	selfAlbum, err := db.CreateAlbum(ctx, "manual", "AI自删相册", "", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := findTool(t, "delete_album").Exec(ctx, d, 2, map[string]any{"album_id": float64(selfAlbum)}); err != nil {
		t.Fatalf("ai self delete album: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE event_type = 'album_deleted' AND target_id = ?`, selfAlbum).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("AI 自删应静默，got %d", n)
	}
}

// AppOnly 铁律：五个删除类工具必须全部标注 AppOnly（DESIGN §8.3 不对令牌开放）。
func TestDeleteToolsAppOnly(t *testing.T) {
	for _, name := range []string{"delete_post", "delete_comment", "delete_album", "delete_anniversary", "delete_map_point"} {
		tool := findTool(t, name)
		if !tool.AppOnly || !tool.IsWrite {
			t.Fatalf("%s: want AppOnly+IsWrite", name)
		}
	}
}

// delete_post：删对方动态通知作者（自删静默）；get_today_in_history 只含往年同月同日。
func TestDeletePostAndHistoryTools(t *testing.T) {
	db := newToolDB(t)
	d := &Deps{DB: db, DP: notify.New(db)}
	ctx := t.Context()
	hash, _ := auth.HashPassword("pw")
	boyID, err := db.CreateUser(ctx, &store.User{Username: "boy", PasswordHash: hash, Role: "boy"})
	if err != nil {
		t.Fatalf("create boy: %v", err)
	}
	postID, err := db.CreatePostWithMedia(ctx, &store.Post{AuthorID: 1, Content: "删除测试动态"}, nil)
	if err != nil {
		t.Fatalf("create post: %v", err)
	}
	del := findTool(t, "delete_post")
	if _, err := del.Exec(ctx, d, 1, map[string]any{"post_id": float64(postID)}); err != nil {
		t.Fatalf("self delete: %v", err)
	}
	var notif int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE recipient_id = 1 AND event_type = 'post_deleted'`).Scan(&notif)
	if notif != 0 {
		t.Fatalf("self delete must be silent, got %d", notif)
	}

	postID2, _ := db.CreatePostWithMedia(ctx, &store.Post{AuthorID: 1, Content: "boy 删 girl 的动态"}, nil)
	if _, err := del.Exec(ctx, d, boyID, map[string]any{"post_id": float64(postID2)}); err != nil {
		t.Fatalf("cross delete: %v", err)
	}
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE recipient_id = 1 AND event_type = 'post_deleted'`).Scan(&notif)
	if notif != 1 {
		t.Fatalf("author notification = %d, want 1", notif)
	}
	if _, err := del.Exec(ctx, d, boyID, map[string]any{"post_id": float64(postID2)}); err == nil {
		t.Fatal("re-delete: want error")
	}

	// 那年今日：当年动态不计入，去年同日动态计入
	postID3, _ := db.CreatePostWithMedia(ctx, &store.Post{AuthorID: 1, Content: "今天发的动态"}, nil)
	if _, err := db.ExecContext(ctx, `UPDATE posts SET created_at = datetime('now', '-1 year') WHERE id = ?`, postID3); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	res, err := findTool(t, "get_today_in_history").Exec(ctx, d, 1, map[string]any{})
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	hits := res.Data.([]PostHit)
	if len(hits) != 1 || hits[0].ID != postID3 {
		t.Fatalf("history hits: %+v (want post %d)", hits, postID3)
	}
}

// 相册类：create_album 通知口径 / remove/list/delete_album（月度相册不可删）与 suggest_photo_grouping 聚合。
func TestAlbumToolsExtra(t *testing.T) {
	db := newToolDB(t)
	d := &Deps{DB: db, MP: mediaPlaceholder(t), DP: notify.New(db)}
	ctx := t.Context()
	// 收件人：newToolDB 只建了 girl(1)，补 boy(2) 才能观察「通知另一方」
	hash, _ := auth.HashPassword("pw")
	if _, err := db.CreateUser(ctx, &store.User{Username: "boy", PasswordHash: hash, Role: "boy"}); err != nil {
		t.Fatalf("create boy: %v", err)
	}
	// 走 create_album 工具（而非直接落库），断言相册上新通知另一方的口径
	ca := findTool(t, "create_album")
	if _, err := ca.Exec(ctx, d, 1, map[string]any{"name": "旅行"}); err != nil {
		t.Fatalf("create album tool: %v", err)
	}
	albumID, err := db.CreateAlbum(ctx, "manual", "旧相册", "", "", 1)
	if err != nil {
		t.Fatalf("create album: %v", err)
	}
	var notifN int
	var notifSnap string
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*), COALESCE(MAX(snapshot_json),'') FROM notifications WHERE recipient_id = 2 AND event_type = 'album_created'").Scan(&notifN, &notifSnap); err != nil {
		t.Fatalf("query album notifications: %v", err)
	}
	if notifN != 1 {
		t.Fatalf("partner album notification = %d, want 1", notifN)
	}
	var albumSnap map[string]any
	if err := json.Unmarshal([]byte(notifSnap), &albumSnap); err != nil {
		t.Fatalf("unmarshal album snapshot %q: %v", notifSnap, err)
	}
	if albumSnap["operator"] != "girl" || albumSnap["via"] != "ai" || albumSnap["name"] != "旅行" {
		t.Fatalf("album snapshot = %+v, want operator=girl via=ai name=旅行", albumSnap)
	}
	if _, err := db.CreateMonthlyAlbum(ctx, "2026-09", "2026年9月", 1); err != nil {
		t.Fatalf("create monthly: %v", err)
	}
	// 分组候选 fixture：一条动态两张不同日期照片
	postID, _ := db.CreatePostWithMedia(ctx, &store.Post{AuthorID: 1, Content: "照片承载动态", LocationName: store.NullStr("杭州·西湖")}, nil)
	_, _ = db.ExecContext(ctx, `INSERT INTO media (post_id, kind, file_path, created_at, taken_at) VALUES
		(?, 'photo', 'a.jpg', datetime('now'), '2026-09-01 10:00:00'),
		(?, 'photo', 'b.jpg', datetime('now'), '2026-09-05 10:00:00')`, postID, postID)

	res, err := findTool(t, "suggest_photo_grouping").Exec(ctx, d, 1, map[string]any{})
	if err != nil {
		t.Fatalf("grouping: %v", err)
	}
	if n := countGroups(t, res.Data); n != 2 {
		t.Fatalf("grouping groups = %d, want 2", n)
	}
	// 日期过滤
	res, err = findTool(t, "suggest_photo_grouping").Exec(ctx, d, 1, map[string]any{
		"date_range": map[string]any{"from": "2026-09-01", "to": "2026-09-01"},
	})
	if err != nil {
		t.Fatalf("grouping filtered: %v", err)
	}
	if n := countGroups(t, res.Data); n != 1 {
		t.Fatalf("filtered groups = %d, want 1", n)
	}

	if _, err := findTool(t, "list_albums").Exec(ctx, d, 1, map[string]any{"view_type": "collection"}); err != nil {
		t.Fatalf("list albums: %v", err)
	}
	rm := findTool(t, "remove_photos_from_album")
	if _, err := rm.Exec(ctx, d, 1, map[string]any{"album_id": float64(albumID)}); err == nil {
		t.Fatal("empty photo_ids: want error")
	}
	del := findTool(t, "delete_album")
	if _, err := del.Exec(ctx, d, 1, map[string]any{"album_id": float64(albumID)}); err != nil {
		t.Fatalf("delete manual album: %v", err)
	}
}

// 地图查询类：search_locations 模糊命中 + get_location_posts 反查动态。
func TestMapSearchTools(t *testing.T) {
	db := newToolDB(t)
	d := &Deps{DB: db, DP: notify.New(db)}
	ctx := t.Context()
	p, _, err := db.FindOrCreatePoint(ctx, store.PointInput{Name: "杭州·西湖", Lat: 30.2, Lng: 120.1, Source: "ai", UserID: 1})
	if err != nil {
		t.Fatalf("create point: %v", err)
	}
	res, err := findTool(t, "search_locations").Exec(ctx, d, 1, map[string]any{"query": "西湖"})
	if err != nil {
		t.Fatalf("search_locations: %v", err)
	}
	if len(res.Data.([]map[string]any)) != 1 {
		t.Fatalf("search hits = %v", res.Data)
	}
	postID, _ := db.CreatePostWithMedia(ctx, &store.Post{
		AuthorID: 1, Content: "西湖边走走",
		PointID: sqlNullInt64(p.ID), LocationName: store.NullStr("杭州·西湖"),
	}, nil)
	res, err = findTool(t, "get_location_posts").Exec(ctx, d, 1, map[string]any{"location_id": float64(p.ID)})
	if err != nil {
		t.Fatalf("location posts: %v", err)
	}
	hits := res.Data.([]PostHit)
	if len(hits) != 1 || hits[0].ID != postID || hits[0].LocationName != "杭州·西湖" {
		t.Fatalf("location posts: %+v", hits)
	}
	if _, err := findTool(t, "get_location_posts").Exec(ctx, d, 1, map[string]any{"location_id": float64(999)}); err == nil {
		t.Fatal("unknown point: want error")
	}
}

// 互动类：收藏闭环 / 评论与通知口径 / 时间线统计。
func TestInteractionTools(t *testing.T) {
	db := newToolDB(t)
	d := &Deps{DB: db, DP: notify.New(db)}
	ctx := t.Context()
	hash, _ := auth.HashPassword("pw")
	if _, err := db.CreateUser(ctx, &store.User{Username: "boy", PasswordHash: hash, Role: "boy"}); err != nil {
		t.Fatalf("create boy: %v", err)
	}
	postID, _ := db.CreatePostWithMedia(ctx, &store.Post{AuthorID: 2, Content: "互动测试动态"}, nil)

	fav := findTool(t, "toggle_favorite")
	res, err := fav.Exec(ctx, d, 1, map[string]any{"post_id": float64(postID)})
	if err != nil || !res.Data.(map[string]any)["favorited"].(bool) {
		t.Fatalf("favorite: %v %v", err, res)
	}
	lf, err := findTool(t, "list_favorites").Exec(ctx, d, 1, map[string]any{})
	if err != nil {
		t.Fatalf("list favorites: %v", err)
	}
	meta := lf.Data.(map[string]any)
	if meta["total"].(int) != 1 {
		t.Fatalf("favorites total = %v", meta["total"])
	}
	if _, err := fav.Exec(ctx, d, 1, map[string]any{"post_id": float64(postID)}); err != nil {
		t.Fatalf("unfavorite: %v", err)
	}

	cc := findTool(t, "create_comment")
	if _, err := cc.Exec(ctx, d, 1, map[string]any{"post_id": float64(postID), "content": "好美的湖"}); err != nil {
		t.Fatalf("comment: %v", err)
	}
	var notif int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE recipient_id = 2 AND event_type = 'post_commented'`).Scan(&notif)
	if notif != 1 {
		t.Fatalf("partner comment notification = %d, want 1", notif)
	}
	dc := findTool(t, "delete_comment")
	if _, err := dc.Exec(ctx, d, 2, map[string]any{"comment_id": float64(1)}); err != nil {
		t.Fatalf("delete comment: %v", err)
	}
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE recipient_id = 1 AND event_type = 'comment_deleted'`).Scan(&notif)
	if notif != 1 {
		t.Fatalf("comment author notification = %d, want 1", notif)
	}
	lc, err := findTool(t, "list_comments").Exec(ctx, d, 1, map[string]any{"post_id": float64(postID)})
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	if lc.Data.(map[string]any)["total"].(int) != 0 {
		t.Fatalf("comments total after delete = %v", lc.Data)
	}

	ts, err := findTool(t, "get_timeline_summary").Exec(ctx, d, 1, map[string]any{})
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	if len(ts.Data.([]store.PeriodCount)) == 0 {
		t.Fatal("timeline years empty")
	}
	ym := time.Now().Format("2006-01")
	res2, err := findTool(t, "get_timeline_summary").Exec(ctx, d, 1, map[string]any{"month": ym})
	if err != nil {
		t.Fatalf("timeline month: %v", err)
	}
	if len(res2.Data.([]store.PeriodCount)) != 1 {
		t.Fatalf("timeline month groups = %v", res2.Data)
	}
}

// sqlNullInt64 构造（测试用）。
func sqlNullInt64(v int64) sql.NullInt64 {
	return sql.NullInt64{Int64: v, Valid: true}
}

// countGroups 分组结果经 JSON round-trip 计数（Data 为具名结构体切片）。
func countGroups(t *testing.T, data any) int {
	t.Helper()
	b, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal groups: %v", err)
	}
	var groups []map[string]any
	if err := json.Unmarshal(b, &groups); err != nil {
		t.Fatalf("unmarshal groups: %v", err)
	}
	return len(groups)
}

// assertMapPointNotif 校验「另一方的足迹通知」：count 条 map_point_created，
// 快照 operator = 真实用户名（非回退标签）、via = ai。
func assertMapPointNotif(t *testing.T, db *store.DB, count int) {
	t.Helper()
	var n int
	var snap string
	if err := db.QueryRowContext(t.Context(),
		"SELECT COUNT(*), COALESCE(MAX(snapshot_json),'') FROM notifications WHERE recipient_id = 2 AND event_type = 'map_point_created'").Scan(&n, &snap); err != nil {
		t.Fatalf("query map_point notifications: %v", err)
	}
	if n != count {
		t.Fatalf("partner map_point notification = %d, want %d", n, count)
	}
	var s map[string]any
	if err := json.Unmarshal([]byte(snap), &s); err != nil {
		t.Fatalf("unmarshal snapshot %q: %v", snap, err)
	}
	if s["operator"] != "girl" || s["via"] != "ai" || s["name"] != "杭州·西湖" {
		t.Fatalf("snapshot = %+v, want operator=girl via=ai name=杭州·西湖", s)
	}
}

// mediaPlaceholder Deps.MP 占位：list_albums 封面签名的 pipeline 依赖。
func mediaPlaceholder(t *testing.T) *media.Pipeline {
	t.Helper()
	tm, err := auth.LoadOrCreateSecret(t.TempDir())
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	return media.New(t.TempDir(), tm.Secret(), 500<<20)
}
