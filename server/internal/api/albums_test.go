package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bookandmusic/love-life/server/internal/notify"
)

// TestAlbumCreatedWebhook 相册上新外推（D10）：手动相册与月度相册均走 Dispatch（站内 + 外推）。
func TestAlbumCreatedWebhook(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	rc := newWebhookReceiver(t)

	// admin 配置订阅「相册上新」的渠道（默认模板）
	res := doJSON(t, "POST", ts.URL+"/api/v1/admin/webhooks", adminToken, map[string]any{
		"name": "album-hook", "method": "POST", "url": rc.server.URL, "events": []string{"album"},
	})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create webhook: %d", res.StatusCode)
	}

	// admin 建手动相册 → 渠道收到 type=album 外推（girl 收站内通知、自建静默语义不变）
	res = doJSON(t, "POST", ts.URL+"/api/v1/albums", adminToken, map[string]string{"name": "海边合集"})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create album: %d", res.StatusCode)
	}
	if rc.hits != 1 {
		t.Fatalf("manual album webhook hits: %d", rc.hits)
	}
	albumPush := "\"type\":\"album\""
	if !strings.Contains(rc.body, albumPush) || !strings.Contains(rc.body, "海边合集") {
		t.Fatalf("webhook body: %s", rc.body)
	}

	// 铺垫本月一条带媒体动态（月度相册只聚合有照片的月份）
	mediaToken, code := uploadMedia(t, ts.URL, adminToken, "p.jpg", genJPEG(t))
	if code != http.StatusOK {
		t.Fatalf("upload: %d", code)
	}
	res = doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{
		"content": "月度测试", "media_tokens": []string{mediaToken},
	})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create post: %d", res.StatusCode)
	}

	// 月度相册 cron 同样外推（通知双方）：用「下月 1 日」视角使上月=本月（含测试媒体）
	now := time.Now()
	dp := notify.New(ts.db)
	dp.CheckMonthlyAlbum(t.Context(), time.Date(now.Year(), now.Month()+1, 1, 9, 0, 0, 0, time.Local))
	if rc.hits != 2 {
		t.Fatalf("monthly album webhook hits: %d", rc.hits)
	}
	if !strings.Contains(rc.body, albumPush) {
		t.Fatalf("monthly webhook body: %s", rc.body)
	}
}

func TestAlbums(t *testing.T) {
	ts, adminToken, girlToken := newPostTestEnv(t)

	// admin 发带图动态（媒体进入照片池）
	mediaToken, code := uploadMedia(t, ts.URL, adminToken, "p.jpg", genJPEG(t))
	if code != http.StatusOK {
		t.Fatalf("upload: %d", code)
	}
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{
		"content": "相册测试", "media_tokens": []string{mediaToken},
	})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create post: %d", res.StatusCode)
	}

	// 时间轴视图含该媒体
	res = doJSON(t, "GET", ts.URL+"/api/v1/media/timeline", adminToken, nil)
	var tl struct {
		Items []struct {
			ID     int64  `json:"id"`
			Kind   string `json:"kind"`
			PostID int64  `json:"post_id"`
		} `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&tl)
	res.Body.Close()
	if len(tl.Items) != 1 || tl.Items[0].PostID != 1 {
		t.Fatalf("timeline: %+v", tl.Items)
	}
	mediaID := tl.Items[0].ID

	// 创建手动相册 + 加照片（admin 建 → girl 收到相册上新站内通知）
	res = doJSON(t, "POST", ts.URL+"/api/v1/albums", adminToken, map[string]string{"name": "海边合集", "description": "夏天"})
	var created struct{ ID int64 `json:"id"` }
	_ = json.NewDecoder(res.Body).Decode(&created)
	res.Body.Close()
	if res.StatusCode != http.StatusCreated || created.ID == 0 {
		t.Fatalf("create album: %d", res.StatusCode)
	}
	var albumNotif int
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE event_type='album_created'`).Scan(&albumNotif)
	if albumNotif != 1 {
		t.Fatalf("album_created notification: %d (want 1, admin 自建静默)", albumNotif)
	}
	res = doJSON(t, "POST", ts.URL+"/api/v1/albums/1/photos", adminToken, map[string]any{"media_ids": []int64{mediaID}})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("add photos: %d", res.StatusCode)
	}

	// 详情：照片在册、封面自动取首图
	res = doJSON(t, "GET", ts.URL+"/api/v1/albums/1", adminToken, nil)
	var detail struct {
		Album struct {
			ItemCount int    `json:"item_count"`
			CoverURL  string `json:"cover_url"`
		} `json:"album"`
		Media []struct{ ID int64 `json:"id"` } `json:"media"`
	}
	_ = json.NewDecoder(res.Body).Decode(&detail)
	res.Body.Close()
	if detail.Album.ItemCount != 1 || len(detail.Media) != 1 || detail.Album.CoverURL == "" {
		t.Fatalf("album detail: %+v", detail)
	}

	// girl 也能加/移出（双方对等）
	res = doJSON(t, "DELETE", ts.URL+"/api/v1/albums/1/photos", girlToken, map[string]any{"media_ids": []int64{mediaID}})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("remove photos: %d", res.StatusCode)
	}

	// 月度相册：cron 在每月 1 日生成「上月」相册——用「下月 1 日」跑，使上月=本月（含测试媒体）；跑两次验证幂等
	now := time.Now()
	firstOfNextMonth := time.Date(now.Year(), now.Month()+1, 1, 9, 0, 0, 0, time.Local)
	dp := notify.New(ts.db)
	dp.CheckMonthlyAlbum(t.Context(), firstOfNextMonth)
	dp.CheckMonthlyAlbum(t.Context(), firstOfNextMonth)

	res = doJSON(t, "GET", ts.URL+"/api/v1/albums?include_hidden=1", adminToken, nil)
	var albums struct {
		Items []struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&albums)
	res.Body.Close()
	var monthlyCount int
	for _, al := range albums.Items {
		if al.Type == "monthly" {
			monthlyCount++
		}
	}
	if monthlyCount != 1 {
		t.Fatalf("monthly album generated: %d (want 1, idempotent)", monthlyCount)
	}

	// 月度相册不可删除
	res = doJSON(t, "DELETE", ts.URL+"/api/v1/albums/2", adminToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("delete monthly album: want 403, got %d", res.StatusCode)
	}

	// 隐藏后默认列表不显示，include_hidden=1 可见
	res = doJSON(t, "PATCH", ts.URL+"/api/v1/albums/2/hidden", adminToken, map[string]bool{"hidden": true})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("hide album: %d", res.StatusCode)
	}
	res = doJSON(t, "GET", ts.URL+"/api/v1/albums", adminToken, nil)
	_ = json.NewDecoder(res.Body).Decode(&albums)
	res.Body.Close()
	for _, al := range albums.Items {
		if al.Type == "monthly" {
			t.Fatal("hidden monthly album should not appear in default list")
		}
	}
	res = doJSON(t, "GET", ts.URL+"/api/v1/albums?include_hidden=1", adminToken, nil)
	var all struct {
		Items []struct{ Type string `json:"type"` } `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&all)
	res.Body.Close()
	found := false
	for _, al := range all.Items {
		if al.Type == "monthly" {
			found = true
		}
	}
	if !found {
		t.Fatal("include_hidden=1 should show hidden monthly album")
	}

	// 月度相册生成 → 双方各收到 1 条相册上新通知
	var monthlyNotif int
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE event_type='album_created' AND snapshot_json LIKE '%monthly%'`).Scan(&monthlyNotif)
	if monthlyNotif != 2 {
		t.Fatalf("monthly album_created notifications: %d (want 2)", monthlyNotif)
	}

	// 手动相册可软删
	res = doJSON(t, "DELETE", ts.URL+"/api/v1/albums/1", adminToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("delete manual album: %d", res.StatusCode)
	}
}
