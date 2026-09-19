package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/auth"
	"github.com/bookandmusic/love-life/server/internal/store"
)

// 地图点位全链路：创建（含城市判定）/同名复用/软删/城市列表/geocode/动态同步。
func TestMapPoints(t *testing.T) {
	ts := newTestServer(t)
	token, code := loginToken(t, ts.URL, "admin", "admin-pw", "admin")
	if code != http.StatusOK {
		t.Fatalf("login: %d", code)
	}
	// 收件人：newTestServer 只建了 admin，补 girl 才能观察「通知另一方」
	hash, _ := auth.HashPassword("girl-pw")
	if _, err := ts.db.CreateUser(t.Context(), &store.User{Username: "girl", PasswordHash: hash, Role: "girl", Nickname: "她"}); err != nil {
		t.Fatalf("create girl: %v", err)
	}

	// 未认证 → 401
	res := doJSON(t, "GET", ts.URL+"/api/v1/map/points", "", nil)
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous: want 401, got %d", res.StatusCode)
	}

	// 创建点位（坐标命中杭州边界 → city 落库）
	res = doJSON(t, "POST", ts.URL+"/api/v1/map/points", token, map[string]any{
		"name": "杭州·西湖", "lat": 30.2467, "lng": 120.4599,
	})
	var created struct {
		ID     int64   `json:"id"`
		Name   string  `json:"name"`
		Lat    float64 `json:"lat"`
		Lng    float64 `json:"lng"`
		Source string  `json:"source"`
		City   string  `json:"city"`
	}
	_ = json.NewDecoder(res.Body).Decode(&created)
	res.Body.Close()
	if res.StatusCode != http.StatusCreated || created.ID == 0 {
		t.Fatalf("create: status=%d body=%+v", res.StatusCode, created)
	}
	if created.Source != "manual" {
		t.Errorf("source = %q, want manual", created.Source)
	}

	// 同名复用 → 200 同 ID 不新建
	res = doJSON(t, "POST", ts.URL+"/api/v1/map/points", token, map[string]any{
		"name": "杭州·西湖", "lat": 1, "lng": 1,
	})
	var reused struct {
		ID int64 `json:"id"`
	}
	_ = json.NewDecoder(res.Body).Decode(&reused)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || reused.ID != created.ID {
		t.Fatalf("reuse: status=%d id=%d want %d", res.StatusCode, reused.ID, created.ID)
	}

	// 新建点位 → 另一方收到足迹更新；同名复用不重复通知（admin 建、girl 收）
	var notifN int
	var notifSnap string
	if err := ts.db.QueryRow("SELECT COUNT(*), COALESCE(MAX(snapshot_json),'') FROM notifications WHERE event_type='map_point_created'").Scan(&notifN, &notifSnap); err != nil {
		t.Fatalf("query map_point notifications: %v", err)
	}
	if notifN != 1 {
		t.Fatalf("map_point_created notifications = %d, want 1 (复用不重复)", notifN)
	}
	var snap map[string]any
	if err := json.Unmarshal([]byte(notifSnap), &snap); err != nil {
		t.Fatalf("unmarshal snapshot %q: %v", notifSnap, err)
	}
	if snap["operator"] != "admin" || snap["name"] != "杭州·西湖" {
		t.Fatalf("snapshot = %+v, want operator=admin name=杭州·西湖", snap)
	}
	if _, hasVia := snap["via"]; hasVia {
		t.Fatalf("手动建点快照不应带 via 标记: %+v", snap)
	}

	// 自由命名无坐标 → 201；city 留空（前缀回退无「·」）
	res = doJSON(t, "POST", ts.URL+"/api/v1/map/points", token, map[string]any{"name": "家里"})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create freeform: %d", res.StatusCode)
	}

	// name 空 → 400
	res = doJSON(t, "POST", ts.URL+"/api/v1/map/points", token, map[string]any{"lat": 1, "lng": 2})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty name: want 400, got %d", res.StatusCode)
	}

	// 列表
	res = doJSON(t, "GET", ts.URL+"/api/v1/map/points", token, nil)
	var list struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	_ = json.NewDecoder(res.Body).Decode(&list)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || list.Total != 2 || len(list.Items) != 2 {
		t.Fatalf("list: status=%d total=%d", res.StatusCode, list.Total)
	}
	if _, hasCity := list.Items[0]["city"]; hasCity {
		t.Error("点位列表契约不含 city（城市经 cities 接口消费）")
	}

	// 城市列表：杭州入列（坐标判定），「家里」不入列
	res = doJSON(t, "GET", ts.URL+"/api/v1/map/points/cities", token, nil)
	var cities struct {
		Items []struct {
			City  string `json:"city"`
			Count int    `json:"count"`
		} `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&cities)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || len(cities.Items) != 1 || cities.Items[0].City != "杭州" {
		t.Fatalf("cities: %+v", cities.Items)
	}

	// 软删 → 204；重复删 → 404
	res = doJSON(t, "DELETE", ts.URL+"/api/v1/map/points/1", token, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", res.StatusCode)
	}
	res = doJSON(t, "DELETE", ts.URL+"/api/v1/map/points/1", token, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("re-delete: want 404, got %d", res.StatusCode)
	}

	// 删除后城市列表为空（杭州点位已软删）
	res = doJSON(t, "GET", ts.URL+"/api/v1/map/points/cities", token, nil)
	_ = json.NewDecoder(res.Body).Decode(&cities)
	res.Body.Close()
	if len(cities.Items) != 0 {
		t.Fatalf("cities after delete: %+v", cities.Items)
	}
}

// geocode：包含匹配命中、未匹配空列表。
func TestGeocode(t *testing.T) {
	ts := newTestServer(t)
	token, _ := loginToken(t, ts.URL, "admin", "admin-pw", "admin")

	res := doJSON(t, "GET", ts.URL+"/api/v1/geocode?q=鼓浪屿", token, nil)
	var got struct {
		Candidates []struct {
			Name string  `json:"name"`
			City string  `json:"city"`
			Lat  float64 `json:"lat"`
			Lng  float64 `json:"lng"`
		} `json:"candidates"`
	}
	_ = json.NewDecoder(res.Body).Decode(&got)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || len(got.Candidates) != 1 || got.Candidates[0].Name != "厦门·鼓浪屿" {
		t.Fatalf("geocode: %+v", got.Candidates)
	}

	res = doJSON(t, "GET", ts.URL+"/api/v1/geocode?q=不存在地名xyz", token, nil)
	_ = json.NewDecoder(res.Body).Decode(&got)
	res.Body.Close()
	if len(got.Candidates) != 0 {
		t.Fatalf("geocode miss: %+v", got.Candidates)
	}
}

// 动态带地点 → 自动同步点位（source=post）；纯文本地名走 geocode 回填坐标。
func TestPostLocationSync(t *testing.T) {
	ts := newTestServer(t)
	token, _ := loginToken(t, ts.URL, "admin", "admin-pw", "admin")

	// 带坐标的 location 对象
	res := doJSON(t, "POST", ts.URL+"/api/v1/posts", token, map[string]any{
		"content": "西湖走走", "location": map[string]any{"name": "杭州·西湖", "lat": 30.2467, "lng": 120.4599},
	})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("post with location: %d", res.StatusCode)
	}

	// 纯文本地名（无坐标 → 地名表回填上海外滩坐标）
	res = doJSON(t, "POST", ts.URL+"/api/v1/posts", token, map[string]any{
		"content": "外滩夜景", "location_name": "上海·外滩",
	})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("post with location_name: %d", res.StatusCode)
	}

	// 点位池：两条动态各建一点，source=post；外滩坐标已回填 → city=上海
	res = doJSON(t, "GET", ts.URL+"/api/v1/map/points", token, nil)
	var list struct {
		Items []struct {
			Name   string  `json:"name"`
			Lat    float64 `json:"lat"`
			Lng    float64 `json:"lng"`
			Source string  `json:"source"`
		} `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&list)
	res.Body.Close()
	if len(list.Items) != 2 {
		t.Fatalf("points: %+v", list.Items)
	}
	byName := map[string]struct {
		Lat    float64
		Source string
	}{}
	for _, p := range list.Items {
		byName[p.Name] = struct {
			Lat    float64
			Source string
		}{p.Lat, p.Source}
	}
	xihu, ok := byName["杭州·西湖"]
	if !ok || xihu.Source != "post" || xihu.Lat != 30.2467 {
		t.Errorf("杭州·西湖: %+v", xihu)
	}
	bund, ok := byName["上海·外滩"]
	if !ok || bund.Source != "post" || bund.Lat != 31.2397 {
		t.Errorf("上海·外滩（geocode 回填）: %+v", bund)
	}

	// 城市列表：杭州 + 上海
	res = doJSON(t, "GET", ts.URL+"/api/v1/map/points/cities", token, nil)
	var cities struct {
		Items []struct {
			City string `json:"city"`
		} `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&cities)
	res.Body.Close()
	if len(cities.Items) != 2 {
		t.Fatalf("cities: %+v", cities.Items)
	}
}
