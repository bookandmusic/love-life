package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestSpaceAndProfile(t *testing.T) {
	ts, adminToken, girlToken := newPostTestEnv(t)

	// 默认空间信息 + 成员（girl/boy 双方；admin 不列）
	res := doJSON(t, "GET", ts.URL+"/api/v1/space", girlToken, nil)
	var space struct {
		Name          string `json:"name"`
		Members       []struct {
			Nickname string `json:"nickname"`
			Role     string `json:"role"`
		} `json:"members"`
		DaysTogether *int `json:"days_together"`
		NextAnniversary struct {
			Name string `json:"name"`
		} `json:"next_anniversary"`
	}
	_ = json.NewDecoder(res.Body).Decode(&space)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || len(space.Members) != 1 || space.Members[0].Role != "girl" {
		t.Fatalf("space: %d %+v", res.StatusCode, space)
	}
	if space.DaysTogether != nil {
		t.Fatal("未设恋爱开始日时 days_together 应为 null")
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

	// 个人资料与 AI 偏好
	res = doJSON(t, "PATCH", ts.URL+"/api/v1/me", girlToken, map[string]any{
		"nickname": "小美", "ai_style": "幽默", "chat_retention_days": 90,
	})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("update me: %d", res.StatusCode)
	}
	res = doJSON(t, "GET", ts.URL+"/api/v1/auth/me", girlToken, nil)
	var me struct {
		Nickname string `json:"nickname"`
	}
	_ = json.NewDecoder(res.Body).Decode(&me)
	res.Body.Close()
	if me.Nickname != "小美" {
		t.Fatalf("nickname after patch: %+v", me)
	}

	// 缺省字段不覆盖（D7 回归：APP 设置页只发偏好字段，不得清空昵称）
	res = doJSON(t, "PATCH", ts.URL+"/api/v1/me", girlToken, map[string]any{
		"ai_style": "文艺", "chat_retention_days": 30,
	})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("update me prefs only: %d", res.StatusCode)
	}
	res = doJSON(t, "GET", ts.URL+"/api/v1/auth/me", girlToken, nil)
	_ = json.NewDecoder(res.Body).Decode(&me)
	res.Body.Close()
	if me.Nickname != "小美" {
		t.Fatalf("nickname must survive pref-only patch: %+v", me)
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
