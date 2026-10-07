package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestAlbumCreatedWebhook 相册上新外推：手动相册与月度相册均走 Dispatch（站内 + 外推）。
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
	var created struct {
		ID int64 `json:"id"`
	}
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

	// 时间轴口径（2026-09-22 确认）：加入相册后媒体离开时间轴；移出后回到时间轴
	res = doJSON(t, "GET", ts.URL+"/api/v1/media/timeline", adminToken, nil)
	var tlAfterAdd struct {
		Items []any `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&tlAfterAdd)
	res.Body.Close()
	if len(tlAfterAdd.Items) != 0 {
		t.Fatalf("filed media must leave timeline: %+v", tlAfterAdd.Items)
	}
	// scope=all（选图器灰显场景）：归册媒体仍在
	res = doJSON(t, "GET", ts.URL+"/api/v1/media/timeline?scope=all", adminToken, nil)
	var tlAll struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&tlAll)
	res.Body.Close()
	if len(tlAll.Items) != 1 {
		t.Fatalf("scope=all must include filed media: %+v", tlAll.Items)
	}

	// 详情：照片在册、封面自动取首图
	res = doJSON(t, "GET", ts.URL+"/api/v1/albums/1", adminToken, nil)
	var detail struct {
		Album struct {
			ItemCount int    `json:"item_count"`
			CoverURL  string `json:"cover_url"`
		} `json:"album"`
		Media []struct {
			ID int64 `json:"id"`
		} `json:"media"`
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

	// 移出相册 → 回到时间轴（未归档口径闭环）
	res = doJSON(t, "GET", ts.URL+"/api/v1/media/timeline", adminToken, nil)
	var tlAfterRemove struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&tlAfterRemove)
	res.Body.Close()
	if len(tlAfterRemove.Items) != 1 || tlAfterRemove.Items[0].ID != mediaID {
		t.Fatalf("removed media must return to timeline: %+v", tlAfterRemove.Items)
	}

	// 月度分组：查询即时聚合（非实体）——该月未归属媒体归入月度组；
	// 媒体加入手动相册后即时移出月度组；无照片的月份不出现。
	res = doJSON(t, "GET", ts.URL+"/api/v1/albums/months", adminToken, nil)
	var months struct {
		Items []struct {
			MonthKey  string `json:"month_key"`
			ItemCount int    `json:"item_count"`
		} `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&months)
	res.Body.Close()
	if len(months.Items) == 0 {
		t.Fatal("month groups must follow unfiled media")
	}
	cur := time.Now().Format("2006-01")
	var g *struct {
		MonthKey  string `json:"month_key"`
		ItemCount int    `json:"item_count"`
	}
	for i := range months.Items {
		if months.Items[i].MonthKey == cur {
			g = &months.Items[i]
		}
	}
	if g == nil || g.ItemCount != 1 {
		t.Fatalf("current month group = %+v, want 本月 1 条未归属媒体", months.Items)
	}

	// 月度详情端点
	res = doJSON(t, "GET", ts.URL+"/api/v1/albums/months/"+cur, adminToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("month media: %d", res.StatusCode)
	}

	// 把媒体加入手动相册 → 本月分组即时消失（归档即移出月度组，无需任何任务）
	res = doJSON(t, "POST", ts.URL+"/api/v1/albums/1/photos", adminToken, map[string]any{"media_ids": []int64{mediaID}})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("add to manual album: %d", res.StatusCode)
	}
	res = doJSON(t, "GET", ts.URL+"/api/v1/albums/months", adminToken, nil)
	_ = json.NewDecoder(res.Body).Decode(&months)
	res.Body.Close()
	for _, item := range months.Items {
		if item.MonthKey == cur {
			t.Fatalf("archived media must leave month group immediately: %+v", months.Items)
		}
	}

	// 月度分组不是实体：无隐藏端点、不可删除（404 = 路由不存在）
	res = doJSON(t, "PATCH", ts.URL+"/api/v1/albums/1/hidden", adminToken, map[string]bool{"hidden": true})
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("hidden endpoint must be gone: %d", res.StatusCode)
	}

	// 手动相册可软删
	res = doJSON(t, "DELETE", ts.URL+"/api/v1/albums/1", adminToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete manual album: %d", res.StatusCode)
	}
}
