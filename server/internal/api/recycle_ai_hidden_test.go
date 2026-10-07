package api

// B7 回收站对 AI 不可见的守卫（ARCHITECTURE「删除与恢复流程」：AI Agent 无法搜索 /
// 访问已删除内容；AI模式设计「安全层」：已删除内容过滤）。
// 缺陷点：get_record(kind=place) 与 propose_delete 的地点预览都走「含已删」的 GetPoint。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// decodeInto 解 JSON 响应体（测试内小工具，避免每处重复声明匿名结构）。
func decodeInto(t *testing.T, res *http.Response, v any) {
	t.Helper()
	defer res.Body.Close()
	_ = json.NewDecoder(res.Body).Decode(v)
}

// i64s int64 → 字符串（拼 URL 用）。
func i64s(v int64) string { return fmt.Sprintf("%d", v) }

// TestGetRecordPlaceHidesDeletedPoint：软删点位后 get_record 报「地点不存在」，
// 且不得复述其名称/详情。
func TestGetRecordPlaceHidesDeletedPoint(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	// 建一个点位（REST 手动创建）
	res := doJSON(t, "POST", ts.URL+"/api/v1/map/points", adminToken, map[string]any{
		"name": "秘密基地", "lat": 31.2, "lng": 121.5,
	})
	var created struct {
		ID int64 `json:"id"`
	}
	decodeInto(t, res, &created)
	if created.ID == 0 {
		t.Fatal("前置：未取到点位 id")
	}

	// 软删
	del := doJSON(t, "DELETE", ts.URL+"/api/v1/map/points/"+i64s(created.ID), adminToken, nil)
	del.Body.Close()
	if del.StatusCode >= 300 {
		t.Fatalf("软删点位失败：%d", del.StatusCode)
	}

	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"p1","type":"function","function":{"name":"get_record","arguments":"{\"kind\":\"place\",\"id\":`+i64s(created.ID)+`}"}}]}}`,
		`{"message":{"content":"这个地点我找不到"}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	_, body := chatTurn(t, ts, adminToken, 0, "查一下那个点位")
	// 工具失败：tool.end 应报失败，且任何发给模型的内容都不得含点位名
	if !strings.Contains(body, `"status":"failed"`) {
		t.Fatalf("已软删点位应报不存在，SSE：\n%s", body)
	}
	for _, m := range fake.lastMessages(t) {
		if c, ok := m["content"].(string); ok && strings.Contains(c, "秘密基地") {
			t.Fatalf("AI 不得看到回收站点位名，实收：%s", c)
		}
	}
}

// TestProposeDeletePlacePreviewHidesDeletedPoint：删除预览（placeSummaryRecord）也不得
// 显示已软删点位——否则用户在确认卡上看到回收站里的点位名。
func TestProposeDeletePlacePreviewHidesDeletedPoint(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)

	res := doJSON(t, "POST", ts.URL+"/api/v1/map/points", adminToken, map[string]any{
		"name": "消失的海湾", "lat": 30.1, "lng": 120.2,
	})
	var created struct {
		ID int64 `json:"id"`
	}
	decodeInto(t, res, &created)
	if created.ID == 0 {
		t.Fatal("前置：未取到点位 id")
	}
	doJSON(t, "DELETE", ts.URL+"/api/v1/map/points/"+i64s(created.ID), adminToken, nil).Body.Close()

	fake := newFakeModel(t,
		`{"message":{"content":"","tool_calls":[{"id":"d1","type":"function","function":{"name":"propose_delete","arguments":"{\"kind\":\"place\",\"ids\":[`+i64s(created.ID)+`]}"}}]}}`,
		`{"message":{"content":"好的"}}`,
	)
	seedFakeProvider(t, ts, fake.server.URL)

	_, body := chatTurn(t, ts, adminToken, 0, "把那个点位删掉")
	if strings.Contains(body, "消失的海湾") {
		t.Fatalf("删除预览不得显示回收站里的点位名，SSE：\n%s", body)
	}
}
