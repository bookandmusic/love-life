package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// GAP2：删除对方创建的内容 → 站内通知创建者；自删静默。覆盖相册 / 纪念日 / 足迹三类（REST 通道）。
func TestDeleteContentNotifiesCreator(t *testing.T) {
	ts, adminToken, girlToken := newPostTestEnv(t)

	notifCount := func(eventType string, targetID int64) int {
		t.Helper()
		var n int
		if err := ts.db.QueryRow(
			`SELECT COUNT(*) FROM notifications WHERE event_type = ? AND target_id = ?`, eventType, targetID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	createID := func(path, token string, body map[string]any) int64 {
		t.Helper()
		res := doJSON(t, "POST", ts.URL+path, token, body)
		var out struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(res.Body).Decode(&out)
		res.Body.Close()
		if res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusOK {
			t.Fatalf("create %s: %d", path, res.StatusCode)
		}
		return out.ID
	}

	// ---- 相册 ----
	alOther := createID("/api/v1/albums", girlToken, map[string]any{"name": "GAP2 对方相册"})
	res := doJSON(t, "DELETE", ts.URL+"/api/v1/albums/"+itoa(alOther), adminToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete other album: %d", res.StatusCode)
	}
	if n := notifCount("album_deleted", alOther); n != 1 {
		t.Fatalf("对方删相册应通知创建者 1 条，got %d", n)
	}

	alSelf := createID("/api/v1/albums", girlToken, map[string]any{"name": "GAP2 自删相册"})
	res = doJSON(t, "DELETE", ts.URL+"/api/v1/albums/"+itoa(alSelf), girlToken, nil)
	res.Body.Close()
	if n := notifCount("album_deleted", alSelf); n != 0 {
		t.Fatalf("自删相册应静默，got %d", n)
	}

	// ---- 纪念日 ----
	annOther := createID("/api/v1/anniversaries", girlToken, map[string]any{"name": "GAP2 对方纪念日", "date": "2020-03-03", "type": "solar"})
	res = doJSON(t, "DELETE", ts.URL+"/api/v1/anniversaries/"+itoa(annOther), adminToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete other anniversary: %d", res.StatusCode)
	}
	if n := notifCount("anniversary_deleted", annOther); n != 1 {
		t.Fatalf("对方删纪念日应通知创建者 1 条，got %d", n)
	}

	annSelf := createID("/api/v1/anniversaries", girlToken, map[string]any{"name": "GAP2 自删纪念日", "date": "2020-04-04", "type": "solar"})
	res = doJSON(t, "DELETE", ts.URL+"/api/v1/anniversaries/"+itoa(annSelf), girlToken, nil)
	res.Body.Close()
	if n := notifCount("anniversary_deleted", annSelf); n != 0 {
		t.Fatalf("自删纪念日应静默，got %d", n)
	}

	// ---- 足迹 ----
	mpOther := createID("/api/v1/map/points", girlToken, map[string]any{"name": "GAP2 对方足迹", "lat": 31.2, "lng": 121.5})
	res = doJSON(t, "DELETE", ts.URL+"/api/v1/map/points/"+itoa(mpOther), adminToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete other point: %d", res.StatusCode)
	}
	if n := notifCount("map_point_deleted", mpOther); n != 1 {
		t.Fatalf("对方删足迹应通知创建者 1 条，got %d", n)
	}

	mpSelf := createID("/api/v1/map/points", girlToken, map[string]any{"name": "GAP2 自删足迹", "lat": 31.3, "lng": 121.6})
	res = doJSON(t, "DELETE", ts.URL+"/api/v1/map/points/"+itoa(mpSelf), girlToken, nil)
	res.Body.Close()
	if n := notifCount("map_point_deleted", mpSelf); n != 0 {
		t.Fatalf("自删足迹应静默，got %d", n)
	}

	// 删除通知仅站内：不进外部 Webhook（无渠道时也不应报错，此处断言 Dispatch 未被使用——
	// 通过通知表行数已覆盖；外部渠道订阅不含内容删除，见 REQUIREMENTS §4.8）
}

// 相册 / 纪念日列表必须回传 created_by，APP 才能判断是否提示「对方会收到站内通知」。
func TestListExposesCreatedBy(t *testing.T) {
	ts, _, girlToken := newPostTestEnv(t)

	res := doJSON(t, "POST", ts.URL+"/api/v1/albums", girlToken, map[string]any{"name": "GAP2 creator"})
	res.Body.Close()
	res = doJSON(t, "GET", ts.URL+"/api/v1/albums", girlToken, nil)
	var albums struct {
		Items []struct {
			CreatedBy int64 `json:"created_by"`
		} `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&albums)
	res.Body.Close()
	if len(albums.Items) == 0 || albums.Items[0].CreatedBy == 0 {
		t.Fatalf("albums[0].created_by 缺失：%+v", albums.Items)
	}

	res = doJSON(t, "POST", ts.URL+"/api/v1/anniversaries", girlToken, map[string]any{"name": "GAP2 creator", "date": "2020-05-05", "type": "solar"})
	res.Body.Close()
	res = doJSON(t, "GET", ts.URL+"/api/v1/anniversaries", girlToken, nil)
	var annivs struct {
		Items []struct {
			CreatedBy int64 `json:"created_by"`
		} `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&annivs)
	res.Body.Close()
	if len(annivs.Items) == 0 || annivs.Items[0].CreatedBy == 0 {
		t.Fatalf("anniversaries[0].created_by 缺失：%+v", annivs.Items)
	}
}
