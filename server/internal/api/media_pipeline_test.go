package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
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
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
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
	var out struct {
		ID int64 `json:"id"`
	}
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
	var errOut struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(res.Body).Decode(&errOut)
	if !strings.Contains(errOut.Error, "一文件只能归属一个手动相册") ||
		!strings.Contains(errOut.Error, "媒体 "+itoa(mediaID)+" 已归属相册 #") {
		t.Fatalf("409 body（须 mock 同款含媒体 id）: %+v", errOut)
	}

	// 删除甲 → 媒体回未归属 → 可入乙
	res = doJSON(t, "DELETE", ts.URL+"/api/v1/albums/"+itoa(alA), adminToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
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

// TestMonthGroupAggregation 月度分组聚合口径：详情=该月未归属媒体（查询即时聚合，非实体）；
// 媒体入手动相册后即时从月度组消失。
func TestMonthGroupAggregation(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	mediaID := newMediaPost(t, ts, adminToken)
	cur := time.Now().Format("2006-01")

	// 分组列表出现本月组，详情含该媒体
	res := doJSON(t, "GET", ts.URL+"/api/v1/albums/months", adminToken, nil)
	var groups struct {
		Items []struct {
			MonthKey  string `json:"month_key"`
			ItemCount int    `json:"item_count"`
		} `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&groups)
	res.Body.Close()
	if len(groups.Items) != 1 || groups.Items[0].MonthKey != cur || groups.Items[0].ItemCount != 1 {
		t.Fatalf("month groups: %+v", groups.Items)
	}

	res = doJSON(t, "GET", ts.URL+"/api/v1/albums/months/"+cur, adminToken, nil)
	var detail struct {
		MonthKey string `json:"month_key"`
		Media    []struct {
			ID int64 `json:"id"`
		} `json:"media"`
	}
	_ = json.NewDecoder(res.Body).Decode(&detail)
	res.Body.Close()
	if len(detail.Media) != 1 || detail.Media[0].ID != mediaID {
		t.Fatalf("month media detail: %+v", detail)
	}

	// 媒体加入手动相册后，月度组即时消失（归档即移出，无需任何任务）
	al := createAlbum(t, ts, adminToken, "手动册")
	res = doJSON(t, "POST", ts.URL+"/api/v1/albums/"+itoa(al)+"/photos", adminToken, map[string]any{"media_ids": []int64{mediaID}})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("add to manual: %d", res.StatusCode)
	}
	res = doJSON(t, "GET", ts.URL+"/api/v1/albums/months", adminToken, nil)
	_ = json.NewDecoder(res.Body).Decode(&groups)
	res.Body.Close()
	if len(groups.Items) != 0 {
		t.Fatalf("month group must vanish when all archived: %+v", groups.Items)
	}
}

// TestIndependentMediaGone 独立媒体接口不存在（PRODUCT「相册（独立底部 tab）」：媒体仅随动态）。
func TestIndependentMediaGone(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	res := doJSON(t, "POST", ts.URL+"/api/v1/media", adminToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("POST /media: %d, want 404", res.StatusCode)
	}

	res = doJSON(t, "DELETE", ts.URL+"/api/v1/media/1", adminToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("DELETE /media/1: %d, want 404", res.StatusCode)
	}
}

// TestReurl 批量重签：命中 + 无效跳过；签名 URL 可直接取回媒体。
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

// TestTimelinePaged 时间轴分页：has_more/total/page/size + item 全字段。
func TestTimelinePaged(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	mediaID := newMediaPost(t, ts, adminToken)

	res := doJSON(t, "GET", ts.URL+"/api/v1/media/timeline?page=1&size=20", adminToken, nil)
	var out struct {
		Items []struct {
			ID       int64    `json:"id"`
			Kind     string   `json:"kind"`
			URL      string   `json:"url"`
			ThumbURL string   `json:"thumb_url"`
			PostID   int64    `json:"post_id"`
			TakenAt  string   `json:"taken_at"`
			AuthorID int64    `json:"author_id"`
			AlbumID  *int64   `json:"album_id"`
			Duration *float64 `json:"duration"`
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

// TestMediaDiscard 主动撤销：上传者本人可撤销（幂等），他人 token 不越权，非法 token 跳过。
func TestMediaDiscard(t *testing.T) {
	ts, adminToken, girlToken := newPostTestEnv(t)

	tok, code := uploadMedia(t, ts.URL, adminToken, "a.jpg", genJPEG(t))
	if code != http.StatusOK || tok == "" {
		t.Fatalf("upload: %d token=%q", code, tok)
	}

	// 他人撤销：归属不符 → discarded=0，文件仍在
	res := doJSON(t, "POST", ts.URL+"/api/v1/media/discard", girlToken, map[string]any{"tokens": []string{tok}})
	var out struct {
		Discarded int `json:"discarded"`
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || out.Discarded != 0 {
		t.Fatalf("cross-user discard: %d %+v", res.StatusCode, out)
	}
	// 本人撤销：discarded=1；重复调用幂等（discarded=0，不报错）
	for i, want := range []int{1, 0} {
		res = doJSON(t, "POST", ts.URL+"/api/v1/media/discard", adminToken, map[string]any{"tokens": []string{tok, "not-a-token", "zzz"}})
		var out2 struct {
			Discarded int `json:"discarded"`
		}
		_ = json.NewDecoder(res.Body).Decode(&out2)
		res.Body.Close()
		if res.StatusCode != http.StatusOK || out2.Discarded != want {
			t.Fatalf("discard #%d: %d %+v, want discarded=%d", i, res.StatusCode, out2, want)
		}
	}
}
