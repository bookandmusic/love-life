package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/store"
)

// GET 渠道的 URL 可能已带自己的 query（如 ?key=xxx）：推送参数必须合并进原 query，
// 同名参数由推送字段覆盖；不能直接拼「?」，否则会产出 `?key=xxx?title=...` 的坏地址。
func TestPushGETMergesExistingQuery(t *testing.T) {
	reqCh := make(chan *http.Request, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCh <- r
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := &Dispatcher{client: srv.Client()}
	ch := &store.WebhookChannel{
		ID:      1,
		Method:  "GET",
		URL:     srv.URL + "/hook?key=secret&type=old",
		Headers: map[string]string{},
	}
	if err := d.pushErr(context.Background(), ch, "new_post", "新动态", "正文"); err != nil {
		t.Fatalf("pushErr: %v", err)
	}

	got := <-reqCh
	if got.URL.Path != "/hook" {
		t.Fatalf("路径 = %q，want /hook（URL 解析后应保留 path）", got.URL.Path)
	}
	q := got.URL.Query()
	if q.Get("key") != "secret" {
		t.Fatalf("原有 query 应保留：key = %q", q.Get("key"))
	}
	if q.Get("type") != "new_post" {
		t.Fatalf("同名参数应由推送字段覆盖：type = %q", q.Get("type"))
	}
	if q.Get("title") != "新动态" || q.Get("content") != "正文" {
		t.Fatalf("推送参数缺失：title = %q，content = %q", q.Get("title"), q.Get("content"))
	}
	// 只允许一个 "?"：合并实现下 RawQuery 里不会再出现未转义的 "?"。
	if got.URL.RawQuery == "" || got.URL.Query().Get("title") == "" {
		t.Fatalf("query 未生效：%q", got.URL.RawQuery)
	}
}

// 未配置模板时默认 body 必须是合法 JSON：标题/正文含控制字符（\t、\r）时，
// 手写转义会原样吐出这些字节，产出的 JSON 解析即失败——接收端拒收，
// 而推送失败只留一条日志，表现为「通知时好时坏」。
func TestDefaultBodyIsValidJSON(t *testing.T) {
	bodyCh := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodyCh <- string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	const title = "标题\t带制表符\r回车\"引号\\反斜杠"
	const content = "正文\n换行\t制表符"
	d := &Dispatcher{client: srv.Client()}
	ch := &store.WebhookChannel{ID: 1, Method: "POST", URL: srv.URL + "/hook", Headers: map[string]string{}}
	if err := d.pushErr(context.Background(), ch, "new_post", title, content); err != nil {
		t.Fatalf("pushErr: %v", err)
	}

	body := <-bodyCh
	var got map[string]string
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("默认 body 不是合法 JSON（控制字符未转义）：%v\nbody=%q", err, body)
	}
	if got["title"] != title {
		t.Fatalf("title = %q，应为原值 %q", got["title"], title)
	}
	if got["content"] != content {
		t.Fatalf("content = %q，应为原值 %q", got["content"], content)
	}
	if got["type"] != "new_post" {
		t.Fatalf("type = %q，应为 new_post", got["type"])
	}
}

// TestWaitDrainsInFlightPush Dispatch 起的后台推送必须在 Wait 返回前结束。
// 测试清理（关库 / 删临时目录）与进程关停都靠它：不等排空就关库，
// 推送会在库已关闭之后继续读写 SQLite（-race -count=N 下表现为清理失败）。
func TestWaitDrainsInFlightPush(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash, role, nickname) VALUES (1,'girl','x','girl','Girl')`); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	hit := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	if _, err := db.CreateWebhookChannel(t.Context(), &store.WebhookChannel{
		Name: "hook", Method: "POST", URL: srv.URL, Headers: map[string]string{},
		Events: []string{"new_post"},
	}); err != nil {
		t.Fatalf("create webhook channel: %v", err)
	}

	dp := New(db)
	t.Cleanup(dp.Wait) // 比 db.Close 先注册者后执行：Wait 在关库之前跑

	dp.Dispatch(t.Context(), []int64{1}, "post_created", "post", 1, `{}`, "新动态", "正文")
	dp.Wait()

	// Wait 返回即「推送已完成」：没有这一条，用例只是「没崩」，测不到排空语义
	select {
	case <-hit:
	default:
		t.Fatal("Wait 返回时推送仍未完成（后台 goroutine 未被等待）")
	}
	dp.Wait() // 已排空后再调用立即返回
}
