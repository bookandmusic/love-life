package store

import (
	"context"
	"time"
)

// ---- AI 调用统计 ----
//
// 每次模型调用落一行（tokens / latency / 状态），供仪表盘 ai-usage 近 14 天聚合
// 与审计日志的 latencyMs 消费（/）。

// AIUsageLog 一行调用统计。
type AIUsageLog struct {
	ProviderID       int64
	Model            string
	PromptTokens     int64
	CompletionTokens int64
	TotalTokens      int64
	LatencyMs        int64
	Status           string // success | failed
}

// InsertAIUsage 记录一次模型调用。
func (d *DB) InsertAIUsage(ctx context.Context, l *AIUsageLog) error {
	_, err := d.ExecContext(ctx,
		`INSERT INTO ai_usage_logs (provider_id, model, prompt_tokens, completion_tokens, total_tokens, latency_ms, status)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		l.ProviderID, l.Model, l.PromptTokens, l.CompletionTokens, l.TotalTokens, l.LatencyMs, l.Status)
	return err
}

// AIUsagePoint 仪表盘时序点（契约 routes.mjs /admin/dashboard/ai-usage：
// {date:"08-15", calls, tokens, avgLatencyMs}）。
type AIUsagePoint struct {
	Date         string
	Calls        int64
	Tokens       int64
	AvgLatencyMs int64
}

// AIUsageSince 按日聚合最近 days 天（含今天）的调用量。
// 无调用的日期不补零——仪表盘按返回点绘制，与 mock 契约一致。
func (d *DB) AIUsageSince(ctx context.Context, days int) ([]*AIUsagePoint, error) {
	since := time.Now().UTC().AddDate(0, 0, -(days - 1)).Format("2006-01-02")
	rows, err := d.QueryContext(ctx, `
		SELECT strftime('%m-%d', created_at) AS day,
		       COUNT(*),
		       COALESCE(SUM(total_tokens), 0),
		       COALESCE(CAST(AVG(latency_ms) AS INTEGER), 0)
		FROM ai_usage_logs
		WHERE date(created_at) >= date(?)
		GROUP BY day
		ORDER BY day`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*AIUsagePoint, 0)
	for rows.Next() {
		p := &AIUsagePoint{}
		if err := rows.Scan(&p.Date, &p.Calls, &p.Tokens, &p.AvgLatencyMs); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ---- 审计日志查询 ----

// AuditEntry 后台审计条目（契约 routes.mjs /admin/audit）。
type AuditEntry struct {
	Time      string
	ActorType string
	Actor     string
	Action    string
	Params    string
	Result    string
	LatencyMs *int64
}

// ListAuditLogs 审计日志列表，最新在前；actor 解析为显示名。
//
// 注意 actor_id 的语义按类型不同：user 与 ai 存的是「发起人用户 ID」
// （刻意修正：读工具丢失发起人关联），pat 存的是令牌 ID。
// 故 ai 行解析为发起人显示名并前置「AI ·」以区分通道，不能拿它去 JOIN
// model_providers——用户 ID 与供应商 ID 数值空间重叠，会串出错误的供应商名。
func (d *DB) ListAuditLogs(ctx context.Context) ([]*AuditEntry, error) {
	rows, err := d.QueryContext(ctx, `
		SELECT a.created_at, a.actor_type,
		       CASE a.actor_type
		         WHEN 'pat' THEN COALESCE(NULLIF(p.name, ''), '未知令牌')
		         WHEN 'ai'  THEN 'AI · ' || COALESCE(NULLIF(u.nickname, ''), u.username, '未知用户')
		         ELSE COALESCE(NULLIF(u.nickname, ''), u.username, '未知用户')
		       END AS actor,
		       a.action,
		       CASE WHEN a.params <> '' THEN a.params ELSE a.params_digest END AS params,
		       a.result, a.latency_ms
		FROM audit_logs a
		LEFT JOIN users u ON a.actor_type IN ('user', 'ai') AND u.id = a.actor_id
		LEFT JOIN personal_access_tokens p ON a.actor_type = 'pat' AND p.id = a.actor_id
		ORDER BY a.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*AuditEntry, 0)
	for rows.Next() {
		e := &AuditEntry{}
		if err := rows.Scan(&e.Time, &e.ActorType, &e.Actor, &e.Action, &e.Params, &e.Result, &e.LatencyMs); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
