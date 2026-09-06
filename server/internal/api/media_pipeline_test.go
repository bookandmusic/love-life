package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bookandmusic/love-life/server/internal/notify"
)

// newMediaPost 发一条带单图动态，返回 mediaID。
func newMediaPost(t *testing.T, ts *testEnv, token string) int64 {
	t.Helper()
	mediaToken, code := uploadMedia(t, ts.URL, token, "p.jpg", genJPEG(t))
	if code != http.StatusOK {
		t.Fatalf("upload: %d", code)
	}
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", token, map[string]any{
		"content": "带图动态", "media_tokens": []string{mediaToken},
	})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create post: %d", res.StatusCode)
	}
	var tl struct {
		Items []struct{ ID int64 `json:"id"` } `json:"items"`
	}
	res = doJSON(t, "GET", ts.URL+"/api/v1/media/timeline", token, nil)
	_ = json.NewDecoder(res.Body).Decode(&tl)
	res.Body.Close()
	if len(tl.Items) == 0 {
		t.Fatal("timeline empty")
	}
	return tl.Items[0].ID
}

func createAlbum(t *testing.T, ts *testEnv, token, name string) int64 {
	t.Helper()
	res := doJSON(t, "POST", ts.URL+"/api/v1/albums", token, map[string]string{"name": name})
	var out struct{ ID int64 `json:"id"` }
	_ = json.NewDecoder(res.Body).Decode(&out)
	res.Body.Close()
	if res.StatusCode != http.StatusCreated || out.ID == 0 {
		t.Fatalf("create album %s: %d", name, res.StatusCode)
	}
	return out.ID
}

// TestMediaOwnershipRules 一文件一相册：跨相册 409、同相册幂等、删除相册释放归属。
func TestMediaOwnershipRules(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	mediaID := newMediaPost(t, ts, adminToken)
	alA := createAlbum(t, ts, adminToken, "甲")
	alB := createAlbum(t, ts, adminToken, "乙")

	// 加入甲
	res := doJSON(t, "POST", ts.URL+"/api/v1/albums/"+itoa(alA)+"/photos", adminToken, map[string]any{"media_ids": []int64{mediaID}})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("add to A: %d", res.StatusCode)
	}

	// 同相册重复 → 幂等 200
	res = doJSON(t, "POST", ts.URL+"/api/v1/albums/"+itoa(alA)+"/photos", adminToken, map[string]any{"media_ids": []int64{mediaID}})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("same album re-add must be idempotent: %d", res.StatusCode)
	}

	// 跨相册 → 409（mock 同款文案）
	res = doJSON(t, "POST", ts.URL+"/api/v1/albums/"+itoa(alB)+"/photos", adminToken, map[string]any{"media_ids": []int64{mediaID}})
	defer res.Body.Close()
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("cross album: want 409, got %d", res.StatusCode)
	}
	var errOut struct{ Error string `json:"error"` }
	_ = json.NewDecoder(res.Body).Decode(&errOut)
	if !strings.Contains(errOut.Error, "一文件只能归属一个手动相册") ||
		!strings.Contains(errOut.Error, "媒体 "+itoa(mediaID)+" 已归属相册 #") {
		t.Fatalf("409 body（须 mock 同款含媒体 id）: %+v", errOut)
	}

	// 删除甲 → 媒体回未归属 → 可入乙
	res = doJSON(t, "DELETE", ts.URL+"/api/v1/albums/"+itoa(alA), adminToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("delete album: %d", res.StatusCode)
	}
	res = doJSON(t, "POST", ts.URL+"/api/v1/albums/"+itoa(alB)+"/photos", adminToken, map[string]any{"media_ids": []int64{mediaID}})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("add to B after A deleted: %d", res.StatusCode)
	}
}

func itoa(n int64) string {
	return strings.TrimSpace(strings.Replace(jsonNumber(n), "json:", "", 1))
}

func jsonNumber(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// TestMonthlyAlbumVirtual 月度相册虚拟口径（D4）：详情=该月未归属媒体；
// 媒体入手动相册后从月度视图消失；月度行不再占用归属（item_count 同步）。
func TestMonthlyAlbumVirtual(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	mediaID := newMediaPost(t, ts, adminToken)

	// cron 生成月度相册（下月 1 日视角 → 上月=本月）
	now := time.Now()
	dp := notify.New(ts.db)
	dp.CheckMonthlyAlbum(t.Context(), time.Date(now.Year(), now.Month()+1, 1, 9, 0, 0, 0, time.Local))

	// 找月度相册 id
	res := doJSON(t, "GET", ts.URL+"/api/v1/albums?include_hidden=1", adminToken, nil)
	var list struct {
		Items []struct {
			ID        int64  `json:"id"`
			Type      string `json:"type"`
			ItemCount int    `json:"item_count"`
		} `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&list)
	res.Body.Close()
	var monthlyID int64
	for _, al := range list.Items {
		if al.Type == "monthly" {
			monthlyID = al.ID
		}
	}
	if monthlyID == 0 {
		t.Fatal("monthly album not generated")
	}

	// 虚拟详情：未归属的本月媒体在列
	res = doJSON(t, "GET", ts.URL+"/api/v1/albums/"+itoa(monthlyID), adminToken, nil)
	var detail struct {
		Album struct{ ItemCount int `json:"item_count"` } `json:"album"`
		Media []struct{ ID int64 `json:"id"` } `json:"media"`
	}
	_ = json.NewDecoder(res.Body).Decode(&detail)
	res.Body.Close()
	if len(detail.Media) != 1 || detail.Media[0].ID != mediaID || detail.Album.ItemCount != 1 {
		t.Fatalf("monthly virtual detail: %+v", detail)
	}

	// 媒体加入手动相册后，月度视图应不再包含（且不被「已归属」卡死）
	al := createAlbum(t, ts, adminToken, "手动册")
	res = doJSON(t, "POST", ts.URL+"/api/v1/albums/"+itoa(al)+"/photos", adminToken, map[string]any{"media_ids": []int64{mediaID}})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("add to manual after monthly existed: %d", res.StatusCode)
	}
	res = doJSON(t, "GET", ts.URL+"/api/v1/albums/"+itoa(monthlyID), adminToken, nil)
	_ = json.NewDecoder(res.Body).Decode(&detail)
	res.Body.Close()
	if len(detail.Media) != 0 || detail.Album.ItemCount != 0 {
		t.Fatalf("monthly must exclude owned media: %+v", detail)
	}
}

// TestIndependentMediaGone 独立媒体接口 410（需求 §4.5/166）。
func TestIndependentMediaGone(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	res := doJSON(t, "POST", ts.URL+"/api/v1/media", adminToken, nil)
	var up struct{ Error, Message string }
	_ = json.NewDecoder(res.Body).Decode(&up)
	res.Body.Close()
	if res.StatusCode != http.StatusGone || up.Error != "INDEPENDENT_MEDIA_DISABLED" {
		t.Fatalf("POST /media: %d %+v", res.StatusCode, up)
	}

	res = doJSON(t, "DELETE", ts.URL+"/api/v1/media/1", adminToken, nil)
	var del struct{ Error string }
	_ = json.NewDecoder(res.Body).Decode(&del)
	res.Body.Close()
	if res.StatusCode != http.StatusGone || del.Error != "INDEPENDENT_MEDIA_DISABLED" {
		t.Fatalf("DELETE /media/1: %d %+v", res.StatusCode, del)
	}
}

// TestReurl 批量重签（D1）：命中 + 无效跳过；签名 URL 可直接取回媒体。
func TestReurl(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	good := newMediaPost(t, ts, adminToken)

	res := doJSON(t, "POST", ts.URL+"/api/v1/media/reurl", adminToken, map[string]any{"ids": []int64{good, 99999}})
	var out struct {
		Items []struct {
			ID       int64  `json:"id"`
			URL      string `json:"url"`
			ThumbURL string `json:"thumb_url"`
		} `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || len(out.Items) != 1 || out.Items[0].ID != good {
		t.Fatalf("reurl: %d %+v", res.StatusCode, out.Items)
	}
	if !strings.Contains(out.Items[0].URL, "sig=") || !strings.Contains(out.Items[0].ThumbURL, "sig=") {
		t.Fatalf("reurl must return signed urls: %+v", out.Items[0])
	}

	// 签名 URL 可用（GET raw 200；SignedURL 为相对路径，拼测试服地址）
	res2, err := http.Get(ts.URL + out.Items[0].URL)
	if err != nil {
		t.Fatal(err)
	}
	res2.Body.Close()
	if res2.StatusCode != http.StatusOK {
		t.Fatalf("signed url fetch: %d", res2.StatusCode)
	}

	// 超 200 → 400
	ids := make([]int64, 201)
	for i := range ids {
		ids[i] = int64(i + 1)
	}
	res = doJSON(t, "POST", ts.URL+"/api/v1/media/reurl", adminToken, map[string]any{"ids": ids})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("reurl 201 ids: want 400, got %d", res.StatusCode)
	}

	// 未认证 → 401
	res = doJSON(t, "POST", ts.URL+"/api/v1/media/reurl", "", map[string]any{"ids": []int64{good}})
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("reurl unauth: %d", res.StatusCode)
	}
}

// TestTimelinePaged 时间轴分页（D5）：has_more/total/page/size + item 全字段。
func TestTimelinePaged(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	mediaID := newMediaPost(t, ts, adminToken)

	res := doJSON(t, "GET", ts.URL+"/api/v1/media/timeline?page=1&size=20", adminToken, nil)
	var out struct {
		Items []struct {
			ID        int64   `json:"id"`
			Kind      string  `json:"kind"`
			URL       string  `json:"url"`
			ThumbURL  string  `json:"thumb_url"`
			PostID    int64   `json:"post_id"`
			TakenAt   string  `json:"taken_at"`
			AuthorID  int64   `json:"author_id"`
			AlbumID   *int64  `json:"album_id"`
			Duration  *float64 `json:"duration"`
		} `json:"items"`
		Total   int  `json:"total"`
		Page    int  `json:"page"`
		Size    int  `json:"size"`
		HasMore bool `json:"has_more"`
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	res.Body.Close()
	if len(out.Items) != 1 || out.Total != 1 || out.HasMore {
		t.Fatalf("timeline paged: %+v", out)
	}
	it := out.Items[0]
	if it.ID != mediaID || it.Kind != "photo" || it.PostID != 1 || it.AuthorID == 0 {
		t.Fatalf("timeline item fields: %+v", it)
	}
	if it.AlbumID != nil || it.Duration != nil {
		t.Fatalf("photo must not carry album_id/duration: %+v", it)
	}
	if it.TakenAt == "" {
		t.Fatal("taken_at must fall back to created_at")
	}
	if out.Page != 1 || out.Size != 20 {
		t.Fatalf("page/size echo: %d/%d", out.Page, out.Size)
	}

	// size 夹紧：size=100 → 生效 50
	res = doJSON(t, "GET", ts.URL+"/api/v1/media/timeline?page=1&size=100", adminToken, nil)
	var cap struct {
		Size int `json:"size"`
	}
	_ = json.NewDecoder(res.Body).Decode(&cap)
	res.Body.Close()
	if cap.Size != 50 {
		t.Fatalf("size cap: %d, want 50", cap.Size)
	}
}