package api

// 提案有效期判定的端到端守卫（用户可见后果 = 删会话被误拒 409）。
//
// 回归的缺陷：expires_at 曾写 RFC3339，而判定是 `expires_at > datetime('now')` 的
// **字符串比较**——第 11 位 `'T' > ' '` 恒真，于是**同一天内已过期**的卡片仍算 pending，
// 删会话被 409 拒绝；而卡片其实已不可确认，等于会话永远删不掉。

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// TestDeleteSessionAllowsExpiredCard：卡片已过期 → 允许删会话（不再 409）。
func TestDeleteSessionAllowsExpiredCard(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	fake := newFakeModel(t, cardSSE("过期的动态"), `{"message":{"content":"好呀"}}`)
	seedFakeProvider(t, ts, fake.server.URL)

	sid, body := chatTurn(t, ts, adminToken, 0, "帮我发动态：过期的动态")
	actionID := cardFromSSE(t, body)
	if actionID == 0 {
		t.Fatal("前置：应有确认卡")
	}

	// 未过期时：删会话应被拒（守住原有语义，避免本项把守卫整个改没）
	res := doJSON(t, "DELETE", ts.URL+fmt.Sprintf("/api/v1/chat/sessions/%d", sid), adminToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("未过期 pending 卡：want 409，got %d", res.StatusCode)
	}

	// 置为「1 分钟前过期」——与「现在」同一天，正是原缺陷失效的场景
	past := time.Now().UTC().Add(-1 * time.Minute).Format("2006-01-02 15:04:05")
	if _, err := ts.db.Exec(`UPDATE confirm_actions SET expires_at = ? WHERE id = ?`, past, actionID); err != nil {
		t.Fatal(err)
	}

	res = doJSON(t, "DELETE", ts.URL+fmt.Sprintf("/api/v1/chat/sessions/%d", sid), adminToken, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("已过期卡片不应再阻止删会话：want 200，got %d（同一天内过期必须被判为过期）", res.StatusCode)
	}
}

// TestDeleteSessionAllowsLegacyRFC3339ExpiredCard：存量 RFC3339 行同样要判得对
// （老库升级后不迁移也应可删会话——比较处的 datetime() 归一化负责这条）。
func TestDeleteSessionAllowsLegacyRFC3339ExpiredCard(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	fake := newFakeModel(t, cardSSE("存量格式的动态"), `{"message":{"content":"好呀"}}`)
	seedFakeProvider(t, ts, fake.server.URL)

	sid, body := chatTurn(t, ts, adminToken, 0, "帮我发动态：存量格式的动态")
	actionID := cardFromSSE(t, body)
	if actionID == 0 {
		t.Fatal("前置：应有确认卡")
	}
	// 写成历史 RFC3339 形态且已过期
	past := time.Now().UTC().Add(-1 * time.Minute).Format(time.RFC3339)
	if _, err := ts.db.Exec(`UPDATE confirm_actions SET expires_at = ? WHERE id = ?`, past, actionID); err != nil {
		t.Fatal(err)
	}
	res := doJSON(t, "DELETE", ts.URL+fmt.Sprintf("/api/v1/chat/sessions/%d", sid), adminToken, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("存量 RFC3339 过期卡应可删会话：want 200，got %d", res.StatusCode)
	}
}
