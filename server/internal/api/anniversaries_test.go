package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bookandmusic/love-life/server/internal/lunar"
	"github.com/bookandmusic/love-life/server/internal/notify"
)

// webhookReceiver 起一个真实 HTTP 接收端，记录收到的推送请求。
//
// 加锁：外部推送自 B1 起是异步的（goroutine 里发），接收端 handler 跑在
// httptest 的 serve goroutine 上，测试主体读 hits/body 与之并发——不加锁就是数据竞争。
type webhookReceiver struct {
	server *httptest.Server
	mu     sync.Mutex
	method string
	body   string
	query  string
	hits   int
	bodies []string // 全部请求体，便于断言「不该推的没推」
}

// snapshot 取一份加锁副本，供断言使用。
func (rc *webhookReceiver) snapshot() (hits int, method, query, body string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return rc.hits, rc.method, rc.query, rc.body
}

// waitHits 等异步推送到达（最多 timeout）；返回是否达到 want。
func (rc *webhookReceiver) waitHits(want int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if hits, _, _, _ := rc.snapshot(); hits >= want {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	hits, _, _, _ := rc.snapshot()
	return hits >= want
}

func newWebhookReceiver(t *testing.T) *webhookReceiver {
	t.Helper()
	rc := &webhookReceiver{}
	rc.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rc.mu.Lock()
		rc.hits++
		rc.method = r.Method
		rc.query = r.URL.RawQuery
		rc.body = string(b)
		rc.bodies = append(rc.bodies, rc.body)
		rc.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(rc.server.Close)
	return rc
}

func TestAnniversaryLunarType(t *testing.T) {
	ts, _, girlToken := newPostTestEnv(t)

	// 农历创建（2026 八月十五 → 对拍公历 2026-09-25）；日期可为公历不存在形状？这里合法即可
	res := doJSON(t, "POST", ts.URL+"/api/v1/anniversaries", girlToken, map[string]any{
		"name": "七夕", "date": "2026-08-15", "type": "lunar", "remind_days_before": 3,
	})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create lunar anniversary: %d", res.StatusCode)
	}

	// 未传 remind_days_before → 默认提前 1 天
	res = doJSON(t, "POST", ts.URL+"/api/v1/anniversaries", girlToken, map[string]any{
		"name": "缺省提醒", "date": "2026-11-11",
	})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create without remind: %d", res.StatusCode)
	}
	var defaultRemind int
	if err := ts.db.QueryRow(`SELECT remind_days_before FROM anniversaries WHERE name = '缺省提醒'`).Scan(&defaultRemind); err != nil {
		t.Fatal(err)
	}
	if defaultRemind != 1 {
		t.Fatalf("未传提醒天数应为 1（提前 1 天），got %d", defaultRemind)
	}

	// 显式 0 → 当天提醒，不得被缺省逻辑改写
	res = doJSON(t, "POST", ts.URL+"/api/v1/anniversaries", girlToken, map[string]any{
		"name": "当天提醒", "date": "2026-12-12", "remind_days_before": 0,
	})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create with remind=0: %d", res.StatusCode)
	}
	var zeroRemind int
	if err := ts.db.QueryRow(`SELECT remind_days_before FROM anniversaries WHERE name = '当天提醒'`).Scan(&zeroRemind); err != nil {
		t.Fatal(err)
	}
	if zeroRemind != 0 {
		t.Fatalf("显式 0 应保留为当天提醒，got %d", zeroRemind)
	}

	// 越界提醒天数（>90 / <0）→ 400，不静默改写（与 PATCH reminder 同口径）
	for _, bad := range []int{91, -1} {
		res = doJSON(t, "POST", ts.URL+"/api/v1/anniversaries", girlToken, map[string]any{
			"name": "越界提醒", "date": "2026-12-12", "remind_days_before": bad,
		})
		res.Body.Close()
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("越界提醒天数 %d 应 400，got %d", bad, res.StatusCode)
		}
	}

	// 非法 type 归一 solar（mock 宽容语义）
	res = doJSON(t, "POST", ts.URL+"/api/v1/anniversaries", girlToken, map[string]any{
		"name": "公历项", "date": "2026-12-25", "type": "lunarr",
	})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create with invalid type: %d", res.StatusCode)
	}

	// solar 需真实公历日：2026-02-30 公历不存在 → 400
	res = doJSON(t, "POST", ts.URL+"/api/v1/anniversaries", girlToken, map[string]any{
		"name": "假阳历", "date": "2026-02-30",
	})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid solar date: %d", res.StatusCode)
	}

	// lunar 仅格式校验：公历不存在的 2026-02-30 可入库；农历月越界 13-01 亦可（永远换算不到 → days_until=-1）
	res = doJSON(t, "POST", ts.URL+"/api/v1/anniversaries", girlToken, map[string]any{
		"name": "越界项", "date": "2101-13-01", "type": "lunar",
	})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("lunar tolerant date: %d", res.StatusCode)
	}

	// 格式校验仍生效
	res = doJSON(t, "POST", ts.URL+"/api/v1/anniversaries", girlToken, map[string]any{
		"name": "坏日期", "date": "2026/12/25",
	})
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad date shape: %d", res.StatusCode)
	}

	// 列表：type 回显 + lunar days_until 用农历换算（2026-09-05 → 2026-09-25 = 20 天）
	res = doJSON(t, "GET", ts.URL+"/api/v1/anniversaries", girlToken, nil)
	var listResp struct {
		Items []struct {
			ID        int64  `json:"id"`
			Name      string `json:"name"`
			Type      string `json:"type"`
			DaysUntil int    `json:"days_until"`
		} `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&listResp)
	res.Body.Close()
	byName := map[string]int{}
	for _, it := range listResp.Items {
		byName[it.Name] = it.DaysUntil
		if it.Type != "solar" && it.Type != "lunar" {
			t.Fatalf("type output: %q", it.Type)
		}
	}
	qixi, ok := byName["七夕"]
	if !ok {
		t.Fatal("lunar anniversary missing")
	}
	// 用同源 TS 换算的对拍值动态校验：农历八月十五下一次公历距今天数
	if qixi <= 0 || qixi > 400 {
		t.Fatalf("lunar days_until implausible: %d", qixi)
	}
	pub, ok := byName["公历项"]
	if !ok || pub <= 0 {
		t.Fatalf("solar days_until: %v", byName)
	}
	if d := byName["越界项"]; d != -1 {
		t.Fatalf("unresolvable lunar days_until: %d, want -1", d)
	}

	// /space：next_anniversary.date 为换算公历（不硬编码期望日，避免随真实日期翻年脆裂；
	// 换算正确性由 internal/lunar 的 TS 对拍黄金用例独立保证）。
	// 先删掉两个固定日期的对照夹具：真实日期越过中秋后，「下一次」会落在他俩头上
	// （2026-09-26 实测翻车——农历八月十五的下一次从 2026-09-25 翻到 2027-09-15，晚于 11-11）。
	for _, name := range []string{"缺省提醒", "当天提醒", "公历项"} {
		var id int64
		if err := ts.db.QueryRow(`SELECT id FROM anniversaries WHERE name = ?`, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		res = doJSON(t, "DELETE", ts.URL+"/api/v1/anniversaries/"+strconv.FormatInt(id, 10), girlToken, nil)
		res.Body.Close()
		if res.StatusCode != http.StatusNoContent {
			t.Fatalf("delete fixture %s: %d", name, res.StatusCode)
		}
	}
	res = doJSON(t, "GET", ts.URL+"/api/v1/space", girlToken, nil)
	var spaceResp struct {
		NextAnniversary struct {
			Name      string `json:"name"`
			Date      string `json:"date"`
			DaysUntil int    `json:"days_until"`
		} `json:"next_anniversary"`
	}
	_ = json.NewDecoder(res.Body).Decode(&spaceResp)
	res.Body.Close()
	wantDate := lunar.NextLunarOccurrence(8, 15, false, time.Now())
	if spaceResp.NextAnniversary.Name != "七夕" || spaceResp.NextAnniversary.Date != wantDate {
		t.Fatalf("space next_anniversary: %+v (want date %s)", spaceResp.NextAnniversary, wantDate)
	}
	if spaceResp.NextAnniversary.DaysUntil != qixi {
		t.Fatalf("space days_until %d != list %d", spaceResp.NextAnniversary.DaysUntil, qixi)
	}
}

func TestAnniversaryAndWebhook(t *testing.T) {
	ts, adminToken, girlToken := newPostTestEnv(t)
	rc := newWebhookReceiver(t)

	// girl（非 admin）不能配渠道 → 403
	res := doJSON(t, "POST", ts.URL+"/api/v1/admin/webhooks", girlToken, map[string]any{
		"name": "x", "method": "POST", "url": rc.server.URL, "events": []string{"new_post"},
	})
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("girl create channel: want 403, got %d", res.StatusCode)
	}

	// admin 配置一个订阅「新动态+纪念日」的 POST 渠道（自定义模板）
	res = doJSON(t, "POST", ts.URL+"/api/v1/admin/webhooks", adminToken, map[string]any{
		"name": "家庭 Bark", "method": "POST", "url": rc.server.URL,
		"headers":       map[string]string{"Authorization": "Bearer test-key"},
		"body_template": `{"t":"{{title}}","c":"{{content}}","ty":"{{type}}"}`,
		"events":        []string{"new_post", "anniversary"},
	})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create webhook: %d", res.StatusCode)
	}

	// admin 发动态 → 渠道收到外部推送（模板占位符已渲染）
	res = doJSON(t, "POST", ts.URL+"/api/v1/posts", adminToken, map[string]any{"content": "纪念日测试动态"})
	res.Body.Close()
	if !rc.waitHits(1, 2*time.Second) {
		hits, method, _, _ := rc.snapshot()
		t.Fatalf("webhook push 未在 2s 内到达：hits=%d method=%s", hits, method)
	}
	hits, method, _, body := rc.snapshot()
	if hits != 1 || method != "POST" {
		t.Fatalf("webhook push: hits=%d method=%s", hits, method)
	}
	if !strings.Contains(body, `"t":"新动态"`) || !strings.Contains(body, `"ty":"new_post"`) {
		t.Fatalf("template render: %s", body)
	}
	if strings.Contains(body, "{{") {
		t.Fatalf("unrendered placeholder: %s", body)
	}

	// 创建一个「3 天后」的纪念日，提醒提前 3 天 → 今天就该提醒
	future := time.Now().AddDate(0, 0, 3).Format("2006-01-02")
	res = doJSON(t, "POST", ts.URL+"/api/v1/anniversaries", adminToken, map[string]any{
		"name": "在一起纪念日", "date": future, "remind_days_before": 3,
	})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create anniversary: %d", res.StatusCode)
	}

	// cron 检查（幂等：跑两次只发一次）
	dp := notify.New(ts.db)
	dp.CheckAnniversaries(t.Context(), time.Now())
	dp.CheckAnniversaries(t.Context(), time.Now())

	if !rc.waitHits(2, 2*time.Second) {
		hits, _, _, body := rc.snapshot()
		t.Fatalf("anniversary push 未在 2s 内到达：hits=%d body=%s", hits, body)
	}
	if hits, _, _, body := rc.snapshot(); !strings.Contains(body, "在一起纪念日") {
		t.Fatalf("anniversary push 内容不符：hits=%d body=%s", hits, body)
	}
	// 站内提醒只发非 admin 用户：admin 不在情侣里、且不走 APP（投给它永远无人读）
	var count, adminCount int
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE event_type='anniversary_reminder'`).Scan(&count)
	_ = ts.db.QueryRow(`SELECT COUNT(*) FROM notifications n JOIN users u ON u.id = n.recipient_id
		WHERE n.event_type='anniversary_reminder' AND u.role='admin'`).Scan(&adminCount)
	if count != 1 || adminCount != 0 {
		t.Fatalf("anniversary in-app notifications: 共 %d 条、其中 admin %d 条（want 1/0）", count, adminCount)
	}

	// 列表带倒计时；调整提醒天数；软删除
	res = doJSON(t, "GET", ts.URL+"/api/v1/anniversaries", adminToken, nil)
	var list struct {
		Items []struct {
			DaysUntil  int `json:"days_until"`
			RemindDays int `json:"remind_days_before"`
		} `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&list)
	res.Body.Close()
	if len(list.Items) != 1 || list.Items[0].DaysUntil != 3 {
		t.Fatalf("anniversary list: %+v", list)
	}
	res = doJSON(t, "PATCH", ts.URL+"/api/v1/anniversaries/1/reminder", adminToken, map[string]int{"remind_days_before": 7})
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("update reminder: %d", res.StatusCode)
	}
	res = doJSON(t, "DELETE", ts.URL+"/api/v1/anniversaries/1", adminToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete anniversary: %d", res.StatusCode)
	}
	res = doJSON(t, "GET", ts.URL+"/api/v1/anniversaries", adminToken, nil)
	_ = json.NewDecoder(res.Body).Decode(&list)
	res.Body.Close()
	if len(list.Items) != 0 {
		t.Fatalf("after delete: %+v", list.Items)
	}

	// 发送测试（admin；不订阅过滤直接推）
	res = doJSON(t, "POST", ts.URL+"/api/v1/admin/webhooks/1/test", adminToken, nil)
	res.Body.Close()
	if !rc.waitHits(3, 2*time.Second) {
		hits, _, _, _ := rc.snapshot()
		t.Fatalf("测试推送未在 2s 内到达：hits=%d", hits)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("webhook test: status=%d", res.StatusCode)
	}

	// admin 删除渠道
	res = doJSON(t, "DELETE", ts.URL+"/api/v1/admin/webhooks/1", adminToken, nil)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("delete channel: %d", res.StatusCode)
	}
	res = doJSON(t, "GET", ts.URL+"/api/v1/admin/webhooks", adminToken, nil)
	var chs struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&chs)
	res.Body.Close()
	if len(chs.Items) != 0 {
		t.Fatalf("after delete channel list: %+v", chs.Items)
	}
}
