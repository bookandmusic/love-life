package api

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/auth"
	"github.com/bookandmusic/love-life/server/internal/store"
)

// genJPEG 生成一张 64x48 测试图。
func genJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for x := 0; x < 64; x++ {
		for y := 0; y < 48; y++ {
			img.Set(x, y, color.RGBA{R: 200, G: 160, B: 130, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

func uploadMedia(t *testing.T, baseURL, token, filename string, content []byte) (string, int) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", filename)
	_, _ = fw.Write(content)
	_ = mw.Close()
	req, _ := http.NewRequest("POST", baseURL+"/api/v1/media/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	defer res.Body.Close()
	var out struct {
		MediaToken string `json:"media_token"`
		Kind       string `json:"kind"`
		Width      int    `json:"width"`
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	return out.MediaToken, res.StatusCode
}

func newPostTestEnv(t *testing.T) (*testEnv, string, string) {
	t.Helper()
	ts := newTestServer(t)
	adminToken, code := loginToken(t, ts.URL, "admin", "admin-pw", "app")
	if code != http.StatusOK {
		t.Fatalf("admin login: %d", code)
	}
	hash, _ := auth.HashPassword("girl-pw")
	if _, err := ts.db.CreateUser(t.Context(), &store.User{Username: "girl", PasswordHash: hash, Role: "girl", Nickname: "她"}); err != nil {
		t.Fatalf("create girl: %v", err)
	}
	girlToken, code := loginToken(t, ts.URL, "girl", "girl-pw", "app")
	if code != http.StatusOK {
		t.Fatalf("girl login: %d", code)
	}
	return ts, adminToken, girlToken
}

func TestPostsMediaLoop(t *testing.T) {
	ts, adminToken, girlToken := newPostTestEnv(t)

	// 1) 上传图片到临时区
	mediaToken, code := uploadMedia(t, ts.URL, adminToken, "photo1.jpg", genJPEG(t))
	if code != http.StatusOK || mediaToken == "" {
		t.Fatalf("upload: code=%d token=%q", code, mediaToken)
	}

	// 2) HEIC 拒绝
	_, code = uploadMedia(t, ts.URL, adminToken, "IMG_0001.heic", []byte("junk"))
	if code != http.StatusBadRequest {
		t.Fatalf("heic should be rejected: %d", code)
	}

	// 3) 发布动态（引用临时 token）
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{
		"content": "第一条动态", "mood": "温馨", "media_tokens": []string{mediaToken},
	})
	var created struct {
		ID int64 `json:"id"`
	}
	_ = json.NewDecoder(res.Body).Decode(&created)
	res.Body.Close()
	if res.StatusCode != http.StatusCreated || created.ID == 0 {
		t.Fatalf("create post: %d %+v", res.StatusCode, created)
	}

	// 4) 列表与详情
	res = doJSON(t, "GET", ts.URL+"/api/v1/posts?page=1&page_size=10", adminToken, nil)
	var list struct {
		Items []struct {
			ID     int64  `json:"id"`
			Author string `json:"author"`
			Media  []struct {
				ID       int64  `json:"id"`
				ThumbURL string `json:"thumb_url"`
			} `json:"media"`
		} `json:"items"`
		Total int `json:"total"`
	}
	_ = json.NewDecoder(res.Body).Decode(&list)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || list.Total != 1 || len(list.Items) != 1 {
		t.Fatalf("list posts: %d total=%d", res.StatusCode, list.Total)
	}
	if list.Items[0].Author != "管理员" || len(list.Items[0].Media) != 1 {
		t.Fatalf("post shape unexpected: %+v", list.Items[0])
	}

	// 5) 签名 URL 可取缩略图（无 Bearer）
	thumbURL := list.Items[0].Media[0].ThumbURL
	imgRes, err := http.Get(ts.URL + thumbURL)
	if err != nil || imgRes.StatusCode != http.StatusOK {
		t.Fatalf("fetch thumb via signed url: %v %d", err, imgRes.StatusCode)
	}
	if ct := imgRes.Header.Get("Content-Type"); ct != "image/jpeg" {
		t.Fatalf("thumb content-type: %s", ct)
	}
	imgRes.Body.Close()

	// 6) 签名被篡改 → 401
	badURL := thumbURL[:len(thumbURL)-4] + "beef"
	badRes, _ := http.Get(ts.URL + badURL)
	if badRes.StatusCode != http.StatusUnauthorized {
		t.Fatalf("tampered sig: want 401, got %d", badRes.StatusCode)
	}
	badRes.Body.Close()

	// 7) girl 删除 admin 的动态 → 通知入库（需求：删对方内容才通知）
	res = doJSON(t, "DELETE", ts.URL+"/api/v1/posts/1", girlToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("girl delete admin's post: %d", res.StatusCode)
	}
	var snap string
	_ = ts.db.QueryRow("SELECT snapshot_json FROM notifications WHERE event_type = 'post_deleted' AND recipient_id = 1").Scan(&snap)
	if snap == "" {
		t.Fatal("post_deleted notification missing")
	}

	// 8) 删除后列表为空、详情 404
	res = doJSON(t, "GET", ts.URL+"/api/v1/posts", adminToken, nil)
	var after struct {
		Total int `json:"total"`
	}
	_ = json.NewDecoder(res.Body).Decode(&after)
	res.Body.Close()
	if after.Total != 0 {
		t.Fatalf("after delete total: %d", after.Total)
	}
	res = doJSON(t, "GET", ts.URL+"/api/v1/posts/1", adminToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("deleted post detail: want 404, got %d", res.StatusCode)
	}
}
