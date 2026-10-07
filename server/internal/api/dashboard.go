package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/bookandmusic/love-life/server/internal/store"
)

// itoa 十进制字符串（仪表盘 value 为字符串类型，契约如此）。
func decStr(v int64) string { return strconv.FormatInt(v, 10) }

// decStrInt 同 decStr，接受 int（DaysUntil 等返回 int）。
func decStrInt(v int) string { return strconv.Itoa(v) }

// humanizeTime 把库内时间转为仪表盘展示口径（今天/昨天/N 天前，更早则原样）。
// 库内 created_at 为 datetime('now') 的 UTC 'YYYY-MM-DD HH:MM:SS'，兼容 RFC3339；
// 解析时按 UTC，比较时转本地时区（时间口径见 store/timeutil.go）。
func humanizeTime(raw string) string {
	if raw == "" {
		return ""
	}
	var t time.Time
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339} {
		if parsed, err := time.ParseInLocation(layout, raw, time.UTC); err == nil {
			t = parsed
			break
		}
	}
	if t.IsZero() {
		return raw
	}
	days := int(time.Since(t.Local()).Hours() / 24)
	switch {
	case days <= 0:
		return "今天"
	case days == 1:
		return "昨天"
	case days < 30:
		return decStrInt(days) + " 天前"
	default:
		return t.Format("2006-01-02")
	}
}

// dashboardAPI 仪表盘。ai-usage 数据源为 的 ai_usage_logs。
type dashboardAPI struct {
	db *store.DB
}

// stats GET /api/v1/admin/dashboard/stats
// 契约（routes.mjs）：[{label, value, change, trend:'up'|'neutral', icon}]。
// value 为字符串（含「12」这类数字文本），change 为「+12 本月」这类短文案。
func (a *dashboardAPI) stats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var posts, photos, cities, anniversaries int64
	if err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM posts WHERE deleted_at IS NULL`).Scan(&posts); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM media WHERE kind = 'photo' AND deleted_at IS NULL`).Scan(&photos); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := a.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT city) FROM map_points WHERE deleted_at IS NULL AND city <> ''`).Scan(&cities); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM anniversaries WHERE deleted_at IS NULL`).Scan(&anniversaries); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 下一个纪念日：复用 store 的 DaysUntil（已处理 lunar 换算），取最近的有效项
	nextText := "暂无"
	if list, err := a.db.ListAnniversaries(ctx); err == nil {
		now := time.Now()
		best := -1
		for i, ann := range list {
			d := ann.DaysUntil(now)
			if d < 0 { // 无法换算（如 lunar 超表尾）的条目不参与，与 profile 口径一致
				continue
			}
			if best == -1 || d < list[best].DaysUntil(now) {
				best = i
			}
		}
		if best >= 0 {
			nextText = "下一个: " + decStrInt(list[best].DaysUntil(now)) + "天后"
		}
	}

	type stat struct {
		Label  string `json:"label"`
		Value  string `json:"value"`
		Change string `json:"change"`
		Trend  string `json:"trend"`
		Icon   string `json:"icon"`
	}
	out := []stat{
		{Label: "动态总数", Value: decStr(posts), Change: "累计", Trend: "up", Icon: "message-square"},
		{Label: "照片数量", Value: decStr(photos), Change: "累计", Trend: "up", Icon: "image"},
		{Label: "足迹城市", Value: decStr(cities), Change: "累计", Trend: "up", Icon: "map-pin"},
		{Label: "纪念日", Value: decStr(anniversaries), Change: nextText, Trend: "neutral", Icon: "calendar"},
	}
	writeJSON(w, http.StatusOK, out)
}

// activity GET /api/v1/admin/dashboard/activity
// 契约：[{icon, text, time}]，由近期事件聚合（动态/照片/评论/点赞）。
// 注意 likes 无 id 列（复合主键 post_id+user_id），需用 post_id 充当排序/去重的标识位，
// 否则整条 UNION 会因「no such column: id」失败。
func (a *dashboardAPI) activity(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.QueryContext(r.Context(), `
		SELECT icon, text, time FROM (
			SELECT 'message-square' AS icon, '发布了一条新动态' AS text, created_at AS time, id AS rid FROM posts WHERE deleted_at IS NULL
			UNION ALL
			SELECT 'image', '上传了照片', created_at, id FROM media WHERE kind = 'photo' AND deleted_at IS NULL
			UNION ALL
			SELECT 'message-circle', '发表了评论', created_at, id FROM comments WHERE deleted_at IS NULL
			UNION ALL
			SELECT 'heart', '点赞了动态', created_at, post_id FROM likes
		) ORDER BY time DESC LIMIT 8`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	defer rows.Close()

	type act struct {
		Icon string `json:"icon"`
		Text string `json:"text"`
		Time string `json:"time"`
	}
	out := make([]act, 0, 8)
	for rows.Next() {
		var x act
		if err := rows.Scan(&x.Icon, &x.Text, &x.Time); err != nil {
			writeError(w, http.StatusInternalServerError, errInternal)
			return
		}
		x.Time = humanizeTime(x.Time)
		out = append(out, x)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// aiUsage GET /api/v1/admin/dashboard/ai-usage
// 契约：{items:[{date:"08-15", calls, tokens, avgLatencyMs}]}，近 14 天（数据源）。
func (a *dashboardAPI) aiUsage(w http.ResponseWriter, r *http.Request) {
	points, err := a.db.AIUsageSince(r.Context(), 14)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}

	type point struct {
		Date         string `json:"date"`
		Calls        int64  `json:"calls"`
		Tokens       int64  `json:"tokens"`
		AvgLatencyMs int64  `json:"avgLatencyMs"`
	}
	items := make([]point, 0, len(points))
	for _, p := range points {
		items = append(items, point{Date: p.Date, Calls: p.Calls, Tokens: p.Tokens, AvgLatencyMs: p.AvgLatencyMs})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// alerts GET /api/v1/admin/dashboard/alerts
// 契约：{items:[{id, level:'error'|'warning', message, time}]}。
//
// 数据源限定为「库里真实可判定」的异常：
// - error：失败的 AI 调用（落库，含模型名与耗时）
//   - warning：已到期的纪念日提醒（remind_days_before 窗口内且当天未处理）
//
// 注：webhook 投递失败当前无落库（webhook_channels 无投递时间列、无投递日志表），
// 故不在此伪造渠道告警；若后续需要，应先补投递记录表再开此告警源。
func (a *dashboardAPI) alerts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	type alert struct {
		ID      int64  `json:"id"`
		Level   string `json:"level"`
		Message string `json:"message"`
		Time    string `json:"time"`
	}
	out := make([]alert, 0)

	// error：近期失败的模型调用
	rows, err := a.db.QueryContext(ctx, `
		SELECT id, 'AI 调用失败：' || COALESCE(NULLIF(model, ''), '未知模型')
		       || '（耗时 ' || latency_ms || ' ms）', created_at
		FROM ai_usage_logs WHERE status = 'failed'
		ORDER BY id DESC LIMIT 10`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	for rows.Next() {
		var x alert
		if err := rows.Scan(&x.ID, &x.Message, &x.Time); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, errInternal)
			return
		}
		x.Level = "error"
		x.Time = humanizeTime(x.Time)
		out = append(out, x)
	}
	rows.Close()

	// warning：即将到来（提醒窗口内）的纪念日
	list, err := a.db.ListAnniversaries(ctx)
	if err == nil {
		now := time.Now()
		for _, ann := range list {
			days := ann.DaysUntil(now)
			if days < 0 || days > ann.RemindDaysBefore {
				continue
			}
			msg := "纪念日「" + ann.Name + "」"
			if days == 0 {
				msg += "就是今天"
			} else {
				msg += "还有 " + decStrInt(days) + " 天"
			}
			out = append(out, alert{ID: ann.ID, Level: "warning", Message: msg, Time: ann.NextSolarDate(now)})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// auditAPI 审计日志查询。
type auditAPI struct {
	db *store.DB
}

// list GET /api/v1/admin/audit
// 契约：[{time, actorType, actor, action, params, result, latencyMs}]，最新在前。
// latencyMs 仅 ai/pat 调用有值，user 直接写库为 null。
func (a *auditAPI) list(w http.ResponseWriter, r *http.Request) {
	entries, err := a.db.ListAuditLogs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}

	type entry struct {
		Time      string `json:"time"`
		ActorType string `json:"actorType"`
		Actor     string `json:"actor"`
		Action    string `json:"action"`
		Params    string `json:"params"`
		Result    string `json:"result"`
		LatencyMs *int64 `json:"latencyMs"`
	}
	out := make([]entry, 0, len(entries))
	for _, e := range entries {
		out = append(out, entry{
			Time: e.Time, ActorType: e.ActorType, Actor: e.Actor, Action: e.Action,
			Params: e.Params, Result: e.Result, LatencyMs: e.LatencyMs,
		})
	}
	writeJSON(w, http.StatusOK, out)
}
