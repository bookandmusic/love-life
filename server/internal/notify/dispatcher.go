// Package notify 通知分发器：站内全量入库 + 外部按渠道事件订阅推送。
package notify

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bookandmusic/love-life/server/internal/store"
)

// 站内事件类型 → 外部订阅键（内容删除类仅站内）
var externalEventKey = map[string]string{
	"post_created":         "new_post",
	"post_liked":           "like",
	"post_commented":       "comment",
	"post_favorited":       "favorite",
	"anniversary_reminder": "anniversary",
	"map_point_created":    "footprint",
	"album_created":        "album",
}

type Dispatcher struct {
	db     *store.DB
	client *http.Client
}

func New(db *store.DB) *Dispatcher {
	return &Dispatcher{db: db, client: &http.Client{Timeout: 10 * time.Second}}
}

// Dispatch 站内通知 recipientIDs（全量）+ 外部推送（订阅对应事件的渠道）。
func (d *Dispatcher) Dispatch(ctx context.Context, recipientIDs []int64, eventType, targetType string, targetID int64, snapshot string, title, content string) {
	for _, uid := range recipientIDs {
		_ = d.db.InsertNotification(ctx, uid, eventType, targetType, targetID, snapshot)
	}
	key, ok := externalEventKey[eventType]
	if !ok {
		return
	}
	channels, err := d.db.ListWebhookChannelsByEvent(ctx, key)
	if err != nil {
		slog.Warn("list webhook channels failed", "err", err)
		return
	}
	for _, ch := range channels {
		d.push(ctx, ch, key, title, content)
	}
}

// PushTest 「发送测试」按钮：不订阅过滤，直接推测试事件。
func (d *Dispatcher) PushTest(ctx context.Context, ch *store.WebhookChannel) error {
	return d.pushErr(ctx, ch, "test", "测试通知", "来自情侣生活记录的测试推送")
}

func (d *Dispatcher) push(ctx context.Context, ch *store.WebhookChannel, event, title, content string) {
	if err := d.pushErr(ctx, ch, event, title, content); err != nil {
		slog.Warn("webhook push failed", "channel", ch.ID, "err", err)
	}
}

func (d *Dispatcher) pushErr(ctx context.Context, ch *store.WebhookChannel, event, title, content string) error {
	vars := map[string]string{
		"title":   title,
		"content": content,
		"time":    time.Now().Format("2006-01-02 15:04:05"),
		"type":    event,
	}

	var req *http.Request
	var err error
	switch ch.Method {
	case "GET":
		q := url.Values{}
		for k, v := range vars {
			q.Set(k, v)
		}
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, ch.URL+"?"+q.Encode(), nil)
	default:
		body := render(ch.BodyTemplate, vars)
		if body == "" {
			body = `{"title":"` + jsonEscape(vars["title"]) + `","content":"` + jsonEscape(vars["content"]) + `","time":"` + vars["time"] + `","type":"` + vars["type"] + `"}`
		}
		req, err = http.NewRequestWithContext(ctx, ch.Method, ch.URL, strings.NewReader(body))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
		}
	}
	if err != nil {
		return err
	}
	for k, v := range ch.Headers {
		req.Header.Set(k, v)
	}
	res, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return &httpError{status: res.StatusCode}
	}
	return nil
}

type httpError struct{ status int }

func (e *httpError) Error() string { return "webhook returned non-2xx status" }

// render 模板占位符替换：{{title}}/{{content}}/{{time}}/{{type}}。
func render(tpl string, vars map[string]string) string {
	for k, v := range vars {
		tpl = strings.ReplaceAll(tpl, "{{"+k+"}}", v)
	}
	return tpl
}

func jsonEscape(s string) string {
	var buf bytes.Buffer
	for _, r := range s {
		switch r {
		case '"', '\\':
			buf.WriteByte('\\')
			buf.WriteRune(r)
		case '\n':
			buf.WriteString(`\n`)
		default:
			buf.WriteRune(r)
		}
	}
	return buf.String()
}

// CheckAnniversaries 每日提醒检查（cron 幂等：当日已发跳过）。
func (d *Dispatcher) CheckAnniversaries(ctx context.Context, now time.Time) {
	list, err := d.db.ListAnniversaries(ctx)
	if err != nil {
		slog.Warn("list anniversaries failed", "err", err)
		return
	}
	// 收件人 = 情侣双方（全部非 admin 用户）：纪念日提醒由定时任务产生、没有操作者，
	// 不适用「操作者本人不收」，双方都要收；admin 不在情侣里且不走 APP（投给它永远无人读）。
	recipients, err := d.db.ListCoupleIDs(ctx)
	if err != nil {
		slog.Warn("list couple ids failed", "err", err)
		return
	}
	for _, a := range list {
		days := a.DaysUntil(now)
		if days != a.RemindDaysBefore {
			continue
		}
		sent, err := d.db.AnniversaryReminderSentToday(ctx, a.ID, now)
		if err != nil || sent {
			continue
		}
		d.Dispatch(ctx, recipients, "anniversary_reminder", "anniversary", a.ID,
			`{"name":"`+jsonEscape(a.Name)+`"}`,
			"纪念日提醒", a.Name+" 还有 "+strconv.Itoa(days)+" 天（"+a.Date+"）")
	}
}
