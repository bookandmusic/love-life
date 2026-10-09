package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/logbuf"
)

// 服务日志端点：只有 admin 能读，且支持增量拉取（前端轮询靠它）。
func TestAdminLogsEndpoint(t *testing.T) {
	ts := newTestServer(t)

	// 缓冲里先放两条（模拟服务端已产生的日志）
	ts.logs.Append(logbuf.Entry{Time: "2026-10-09 00:18:26", Level: "ERROR", Msg: "模型调用失败",
		Attrs: map[string]string{"provider_id": "16", "api_key": logbuf.Redacted}})
	ts.logs.Append(logbuf.Entry{Time: "2026-10-09 00:18:27", Level: "INFO", Msg: "server listening"})

	adminToken, _ := loginToken(t, ts.URL, "admin", "admin-pw", "admin")

	res := doJSON(t, "GET", ts.URL+"/api/v1/admin/logs", adminToken, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	var body struct {
		Items []struct {
			Seq   int64             `json:"seq"`
			Time  string            `json:"time"`
			Level string            `json:"level"`
			Msg   string            `json:"msg"`
			Attrs map[string]string `json:"attrs"`
		} `json:"items"`
		LatestSeq int64 `json:"latestSeq"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(body.Items))
	}
	if body.Items[0].Level != "ERROR" || body.Items[1].Msg != "server listening" {
		t.Errorf("条目形状不符：%+v", body.Items)
	}
	if body.LatestSeq < 2 {
		t.Errorf("latestSeq = %d, want >= 2", body.LatestSeq)
	}

	// 增量：带上 latestSeq 应拿不到旧条目
	res2 := doJSON(t, "GET", ts.URL+"/api/v1/admin/logs?since="+strconv.FormatInt(body.LatestSeq, 10), adminToken, nil)
	defer res2.Body.Close()
	var body2 struct {
		Items []any `json:"items"`
	}
	if err := json.NewDecoder(res2.Body).Decode(&body2); err != nil {
		t.Fatalf("decode2: %v", err)
	}
	if len(body2.Items) != 0 {
		t.Errorf("增量拉取应为空，实得 %d 条", len(body2.Items))
	}

	// 新日志产生后，增量能拿到它
	ts.logs.Append(logbuf.Entry{Time: "2026-10-09 00:19:00", Level: "WARN", Msg: "又是一条"})
	res3 := doJSON(t, "GET", ts.URL+"/api/v1/admin/logs?since="+strconv.FormatInt(body.LatestSeq, 10), adminToken, nil)
	defer res3.Body.Close()
	var body3 struct {
		Items []struct {
			Msg string `json:"msg"`
		} `json:"items"`
	}
	if err := json.NewDecoder(res3.Body).Decode(&body3); err != nil {
		t.Fatalf("decode3: %v", err)
	}
	if len(body3.Items) != 1 || body3.Items[0].Msg != "又是一条" {
		t.Errorf("增量应恰好含新条目，实得 %+v", body3.Items)
	}
}

// 服务端重启后缓冲序号从 1 重来，而客户端游标还停在重启前的较大值。
// 不做处理的话 `Since(cursor)` 永远返回空集——页面看着「实时跟随」却再也不出新日志，
// 且不刷新页面无法自愈。api 层需把「游标 > 当前最大序号」当成换了一轮，全量重拉。
//
// 直接测 handler：router 在构造时就抓住了 buffer 引用，故不能靠换 ts.logs 模拟重启，
// 得用一个「序号较小」的缓冲来代表重启后的世界。
func TestAdminLogsRecoversAfterServerRestart(t *testing.T) {
	fresh := logbuf.New(64) // 重启后的缓冲：序号从 1 开始
	fresh.Append(logbuf.Entry{Msg: "重启后"})
	h := &logsAPI{buf: fresh}

	staleCursor := int64(5) // 重启前客户端记下的游标，比新缓冲的 latestSeq(1) 大
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/logs?since="+strconv.FormatInt(staleCursor, 10), nil)
	h.list(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Items []struct {
			Msg string `json:"msg"`
		} `json:"items"`
		LatestSeq int64 `json:"latestSeq"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].Msg != "重启后" {
		t.Errorf("重启后应回退为全量拉取并给出新日志，实得 %+v", body.Items)
	}
	if body.LatestSeq != 1 {
		t.Errorf("latestSeq = %d, want 1（新缓冲）", body.LatestSeq)
	}

	// 反向：游标在范围内时仍是增量（别把正常增量也当重启）
	rec2 := httptest.NewRecorder()
	h.list(rec2, httptest.NewRequest(http.MethodGet, "/api/v1/admin/logs?since=1", nil))
	var body2 struct {
		Items []any `json:"items"`
	}
	if err := json.NewDecoder(rec2.Body).Decode(&body2); err != nil {
		t.Fatalf("decode2: %v", err)
	}
	if len(body2.Items) != 0 {
		t.Errorf("游标已到最新时不应再有条目，实得 %d 条", len(body2.Items))
	}
}

// since 参数非法即 400（不静默当成 0，否则前端拼错会一直全量拉）。
func TestAdminLogsRejectsBadSince(t *testing.T) {
	ts := newTestServer(t)
	adminToken, _ := loginToken(t, ts.URL, "admin", "admin-pw", "admin")

	for _, bad := range []string{"abc", "-1"} {
		res := doJSON(t, "GET", ts.URL+"/api/v1/admin/logs?since="+bad, adminToken, nil)
		res.Body.Close()
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("since=%q status = %d, want 400", bad, res.StatusCode)
		}
	}
}

// 情侣用户读不到服务日志（与其它后台页同口径）。
func TestLogsForbiddenForCouple(t *testing.T) {
	ts := newTestServer(t)
	adminToken, _ := loginToken(t, ts.URL, "admin", "admin-pw", "admin")
	// 建一对情侣账号
	res := doJSON(t, "POST", ts.URL+"/api/v1/admin/users/couple", adminToken, map[string]any{
		"girl": map[string]string{"username": "g1", "nickname": "她", "password": "pw123456"},
		"boy":  map[string]string{"username": "b1", "nickname": "他", "password": "pw123456"},
	})
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("建情侣账号失败：%d", res.StatusCode)
	}
	girlToken, _ := loginToken(t, ts.URL, "g1", "pw123456", "girl")
	if girlToken == "" {
		t.Fatal("情侣账号登录失败")
	}

	res2 := doJSON(t, "GET", ts.URL+"/api/v1/admin/logs", girlToken, nil)
	defer res2.Body.Close()
	if res2.StatusCode == http.StatusOK {
		t.Error("非 admin 不得读到服务日志")
	}
}
