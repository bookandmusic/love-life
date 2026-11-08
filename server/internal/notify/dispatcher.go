// Package notify 通知分发器：站内全量入库 + 外部按渠道事件订阅推送。
package notify

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bookandmusic/love-life/server/internal/store"
)

// 站内事件类型 → 外部订阅键（内容删除类仅站内）
var externalEventKey = map[string]string{
	"post_created":         "new_post",
	"post_liked":           "like",
	"post_commented":       "comment",
	"anniversary_reminder": "anniversary",
	"map_point_created":    "footprint",
	"album_created":        "album",
}

// pushBudget 一次异步推送的总预算（覆盖所有渠道串行）。
// 比单渠道 10s 的 client 超时宽裕：多配几个渠道时不应被第一个卡死整批。
const pushBudget = 30 * time.Second

type Dispatcher struct {
	db     *store.DB
	client *http.Client
	// wg 跟踪在途的外部推送 goroutine。Wait 供「进程关停」与「测试清理」等待排空：
	// 推送要写库（读渠道、记日志），库或临时目录在它写完之前被关掉就会失败
	// （测试里表现为 TempDir 清理报 "directory not empty"）。
	wg sync.WaitGroup
}

// Wait 等待所有在途外部推送结束。已排空后再调用立即返回。
func (d *Dispatcher) Wait() { d.wg.Wait() }

func New(db *store.DB) *Dispatcher {
	return &Dispatcher{db: db, client: &http.Client{Timeout: 10 * time.Second}}
}

// PossessivePost 互动通知归属口径：目标动态作者是操作者本人 → 「自己的动态」；
// 作者是对方（收件人）→ 「你的动态」。单情侣模型下作者只可能是这两种。
// REST 与 AI 代操作两条路径共用，保证站内快照与外部推送文案一致。
func PossessivePost(authorID, operatorID int64) string {
	if authorID == operatorID {
		return "自己的动态"
	}
	return "你的动态"
}

// Dispatch 站内通知 recipientIDs（全量）+ 外部推送（订阅对应事件的渠道）。
//
// 站内通知同步写库（廉价、且失败不影响主流程）；外部推送**异步**——它是副作用，
// 不该出现在业务请求的关键路径上。渠道故障时同步推送会让每次发布都多等一个
// http.Client.Timeout（10s）× 渠道数，而用户只是发了一条动态。
//
// 用 context.WithoutCancel：请求结束后 r.Context() 立刻取消，直接沿用会让推送
// 全部变成 context canceled。独立超时兜住「上游挂着不返回」。
//
// ponytail: 推送不落盘、失败不重试（仅记日志），30s 总预算覆盖全部渠道串行；
// 要求必达投递时升级为持久化队列。
//
// 异步部分由 Wait 收敛：关停或测试清理前先等它排空，否则推送会在库关掉之后继续写。
func (d *Dispatcher) Dispatch(ctx context.Context, recipientIDs []int64, eventType, targetType string, targetID int64, snapshot string, title, content string) {
	for _, uid := range recipientIDs {
		if err := d.db.InsertNotification(ctx, uid, eventType, targetType, targetID, snapshot); err != nil {
			slog.Warn("insert notification failed", "user_id", uid, "event", eventType, "err", err)
		}
	}
	key, ok := externalEventKey[eventType]
	if !ok {
		return
	}
	base := context.WithoutCancel(ctx)
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		ctx, cancel := context.WithTimeout(base, pushBudget)
		defer cancel()
		channels, err := d.db.ListWebhookChannelsByEvent(ctx, key)
		if err != nil {
			slog.Warn("list webhook channels failed", "err", err)
			return
		}
		for _, ch := range channels {
			d.push(ctx, ch, key, title, content)
		}
	}()
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
		// 渠道 URL 可能已带 query（如 ?key=xxx）：先解析再合并，同名参数由推送字段覆盖，
		// 直接拼 "?" 会产出 `?key=xxx?title=...` 这样的坏地址。
		u, perr := url.Parse(ch.URL)
		if perr != nil {
			return perr
		}
		q := u.Query()
		for k, v := range vars {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	default:
		body := render(ch.BodyTemplate, vars)
		if body == "" {
			body, err = defaultBody(vars)
			if err != nil {
				return err
			}
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

// defaultBody 未配置模板时的默认请求体（JSON 对象）。
//
// 用 json.Marshal 组装而不是手写转义：手写转义只会处理引号、反斜杠与换行，
// 标题/正文里出现 \t、\r 等控制字符时原样写出，产出的就是非法 JSON（接收端直接拒）。
func defaultBody(vars map[string]string) (string, error) {
	b, err := json.Marshal(map[string]string{
		"title":   vars["title"],
		"content": vars["content"],
		"time":    vars["time"],
		"type":    vars["type"],
	})
	if err != nil {
		return "", err
	}
	return string(b), nil
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
		// 站内快照同样是一段 JSON：名字走 json.Marshal 转义，控制字符不会破坏结构
		snapshot, err := json.Marshal(map[string]string{"name": a.Name})
		if err != nil {
			slog.Warn("纪念日快照序列化失败", "anniversary_id", a.ID, "err", err)
			continue
		}
		d.Dispatch(ctx, recipients, "anniversary_reminder", "anniversary", a.ID, string(snapshot),
			"纪念日提醒", a.Name+" 还有 "+strconv.Itoa(days)+" 天（"+a.Date+"）")
	}
}
