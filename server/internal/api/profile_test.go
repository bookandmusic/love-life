package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestSpaceAndProfile(t *testing.T) {
	ts, adminToken, girlToken := newPostTestEnv(t)

	// 默认空间信息 + 成员（girl/boy 双方；admin 不列）
	res := doJSON(t, "GET", ts.URL+"/api/v1/space", girlToken, nil)
	var space struct {
		Name    string `json:"name"`
		Members []struct {
			Nickname string `json:"nickname"`
			Role     string `json:"role"`
		} `json:"members"`
		DaysTogether    *int `json:"days_together"`
		NextAnniversary struct {
			Name string `json:"name"`
		} `json:"next_anniversary"`
		AnniversaryCount int `json:"anniversary_count"`
		AlbumCount       int `json:"album_count"`
	}
	_ = json.NewDecoder(res.Body).Decode(&space)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || len(space.Members) != 1 || space.Members[0].Role != "girl" {
		t.Fatalf("space: %d %+v", res.StatusCode, space)
	}
	if space.DaysTogether != nil {
		t.Fatal("未设恋爱开始日时 days_together 应为 null")
	}
	// 全新库计数为 0（APP 首页据此判定「全新部署空态」，不依赖 next_anniversary.name）
	if space.AnniversaryCount != 0 || space.AlbumCount != 0 {
		t.Fatalf("空库计数应为 0: anniv=%d album=%d", space.AnniversaryCount, space.AlbumCount)
	}

	// girl 也能改空间信息（双方对等）+ 设恋爱开始日 + 建纪念日
	res = doJSON(t, "PUT", ts.URL+"/api/v1/space", girlToken, map[string]string{
		"name": "小美与小明", "signature": "慢慢来", "love_start_date": "2020-05-20",
	})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("update space: %d", res.StatusCode)
	}
	res = doJSON(t, "POST", ts.URL+"/api/v1/anniversaries", adminToken, map[string]any{
		"name": "恋爱纪念日", "date": "2020-05-20", "remind_days_before": 1,
	})
	res.Body.Close()

	res = doJSON(t, "GET", ts.URL+"/api/v1/space", adminToken, nil)
	_ = json.NewDecoder(res.Body).Decode(&space)
	res.Body.Close()
	if space.Name != "小美与小明" || space.DaysTogether == nil || *space.DaysTogether < 2000 {
		t.Fatalf("space after update: %+v", space)
	}
	if space.NextAnniversary.Name != "恋爱纪念日" {
		t.Fatalf("next anniversary: %+v", space.NextAnniversary)
	}
	if space.AnniversaryCount != 1 {
		t.Fatalf("建纪念日后计数应为 1: %d", space.AnniversaryCount)
	}
	// 相册同理：建一个手动相册 → album_count 跟随
	res = doJSON(t, "POST", ts.URL+"/api/v1/albums", girlToken, map[string]string{"name": "海边合集"})
	res.Body.Close()
	res = doJSON(t, "GET", ts.URL+"/api/v1/space", girlToken, nil)
	_ = json.NewDecoder(res.Body).Decode(&space)
	res.Body.Close()
	if space.AlbumCount != 1 {
		t.Fatalf("建相册后 album_count 应为 1: %d", space.AlbumCount)
	}

	// AI 偏好（ai_style 合法性 = 风格库中存在）；昵称 / 生日由后台维护，/me 不再接收
	res = doJSON(t, "PATCH", ts.URL+"/api/v1/me", girlToken, map[string]any{
		"ai_style": "不存在的风格",
	})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("update me with unknown style: want 400, got %d", res.StatusCode)
	}
	res = doJSON(t, "PATCH", ts.URL+"/api/v1/me", girlToken, map[string]any{
		"nickname": "小美", "ai_style": "幽默",
	})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("update me: %d", res.StatusCode)
	}
	res = doJSON(t, "GET", ts.URL+"/api/v1/auth/me", girlToken, nil)
	var me struct {
		Nickname string `json:"nickname"`
		AIStyle  string `json:"ai_style"`
	}
	_ = json.NewDecoder(res.Body).Decode(&me)
	res.Body.Close()
	if me.AIStyle != "幽默" {
		t.Fatalf("ai_style after patch: %+v", me)
	}
	if me.Nickname == "小美" {
		t.Fatal("PATCH /me 不得再改昵称（资料由后台维护）——nickname 字段应被忽略")
	}
	// 非 ai_style 字段一律忽略（含已拆除的字段名）
	res = doJSON(t, "PATCH", ts.URL+"/api/v1/me", girlToken, map[string]any{
		"ai_style": "简洁", "nickname": "小美", "birthday": "2000-01-01",
	})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("update me prefs only: %d", res.StatusCode)
	}
	res = doJSON(t, "GET", ts.URL+"/api/v1/auth/me", girlToken, nil)
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	for _, gone := range []string{"chat_retention_days", "birthday"} {
		if strings.Contains(string(raw), gone) {
			t.Fatalf("/me 不得再下发 %s：%s", gone, raw)
		}
	}
	_ = json.Unmarshal(raw, &me)
	if me.AIStyle != "简洁" || me.Nickname != "她" {
		t.Fatalf("pref-only patch: %+v", me)
	}

	// 非法枚举拒绝
	res = doJSON(t, "PATCH", ts.URL+"/api/v1/me", girlToken, map[string]any{"ai_style": "毒舌"})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid ai_style: %d", res.StatusCode)
	}
	res = doJSON(t, "PUT", ts.URL+"/api/v1/space", adminToken, map[string]string{"love_start_date": "2020-13-99"})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid love date: %d", res.StatusCode)
	}
}
