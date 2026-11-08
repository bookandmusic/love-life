package api

// 会话置顶：置顶是个人偏好，只改自己的列表顺序；
// 列表「置顶在前，其后按 updated_at 倒序」，接口幂等切换并回当前态。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

type sessRow struct {
	ID     int64  `json:"id"`
	Title  string `json:"title"`
	Pinned bool   `json:"pinned"`
}

func listSessions(t *testing.T, ts *testEnv, token string) []sessRow {
	t.Helper()
	res := doJSON(t, http.MethodGet, ts.URL+"/api/v1/chat/sessions", token, nil)
	defer res.Body.Close()
	var page struct {
		Items []sessRow `json:"items"`
	}
	if err := json.NewDecoder(res.Body).Decode(&page); err != nil {
		t.Fatalf("解析会话列表: %v", err)
	}
	return page.Items
}

// togglePin 调置顶接口，返回接口回的当前态。
func togglePin(t *testing.T, ts *testEnv, token string, sid int64) bool {
	t.Helper()
	res := doJSON(t, http.MethodPost, fmt.Sprintf("%s/api/v1/chat/sessions/%d/pin", ts.URL, sid), token, map[string]any{})
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("置顶接口 want 200, got %d", res.StatusCode)
	}
	var out struct {
		Pinned bool `json:"pinned"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("解析置顶响应: %v", err)
	}
	return out.Pinned
}

// TestChatSessionPinTogglesAndOrders：置顶 → 列表首项且 pinned=true → 再点回 false 且恢复时间序。
func TestChatSessionPinTogglesAndOrders(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	fake := newFakeModel(t, fakeAnswer("一"), fakeAnswer("二"))
	seedFakeProvider(t, ts, fake.server.URL)

	first, _ := chatTurn(t, ts, adminToken, 0, "第一个会话")
	second, _ := chatTurn(t, ts, adminToken, 0, "第二个会话")

	// 默认按 updated_at 倒序：后建的在前
	items := listSessions(t, ts, adminToken)
	if len(items) != 2 || items[0].ID != second || items[1].ID != first {
		t.Fatalf("初始顺序应为 updated_at 倒序（%d,%d），实得 %+v", second, first, items)
	}
	if items[0].Pinned || items[1].Pinned {
		t.Fatalf("默认不该有置顶：%+v", items)
	}

	// 置顶较早的那个 → 排到最前
	if !togglePin(t, ts, adminToken, first) {
		t.Fatal("置顶接口应返回 pinned=true")
	}
	items = listSessions(t, ts, adminToken)
	if items[0].ID != first || !items[0].Pinned {
		t.Fatalf("置顶会话应排首位且 pinned=true：%+v", items)
	}

	// 再点一次 → 取消置顶，顺序回到时间序
	if togglePin(t, ts, adminToken, first) {
		t.Fatal("二次调用应返回 pinned=false")
	}
	items = listSessions(t, ts, adminToken)
	if items[0].ID != second || items[0].Pinned || items[1].Pinned {
		t.Fatalf("取消置顶后应回到时间序：%+v", items)
	}
}

// TestChatSessionPinGuards：不存在 / 别人的会话一律 404，不能靠置顶接口探测别人的会话。
func TestChatSessionPinGuards(t *testing.T) {
	ts, adminToken, girlToken := newPostTestEnv(t)
	fake := newFakeModel(t, fakeAnswer("一"))
	seedFakeProvider(t, ts, fake.server.URL)

	sid, _ := chatTurn(t, ts, adminToken, 0, "只有我知道的会话")

	for _, tc := range []struct {
		name  string
		token string
		id    int64
	}{
		{"不存在", adminToken, 999999},
		{"别人的会话", girlToken, sid},
	} {
		res := doJSON(t, http.MethodPost, fmt.Sprintf("%s/api/v1/chat/sessions/%d/pin", ts.URL, tc.id), tc.token, map[string]any{})
		res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("%s：want 404, got %d", tc.name, res.StatusCode)
		}
	}
	// 别人的会话状态没被改
	items := listSessions(t, ts, adminToken)
	if len(items) != 1 || items[0].Pinned {
		t.Fatalf("越权调用不该改动置顶态：%+v", items)
	}
}
