package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
)

// ---- AD6 数据安全设置（DESIGN §9 / 需求 §8.3）----
//
// 两项保留设置各自独立保存，PUT 为部分更新：请求体可只含本次变更字段，
// 服务端按字段合并，不得整体覆盖（DESIGN §9 已记录该语义）。

const (
	settingRecycleRetentionDays = "recycle_retention_days"
	settingChatRetention        = "chat_retention"

	// 回收站保留期限范围：7~365 天，默认 30（DESIGN §5 / 需求 §8.3）
	RecycleRetentionMin = 7
	RecycleRetentionMax = 365
)

// SecuritySettings 数据安全设置。
type SecuritySettings struct {
	RecycleRetentionDays int64
	ChatRetention        string
}

// chatRetentionAllowed AI 对话记录保留时长档位（含「永久」）。
var chatRetentionAllowed = map[string]bool{
	"7": true, "30": true, "90": true, "permanent": true,
}

// ValidChatRetention 校验对话保留档位。
func ValidChatRetention(v string) bool { return chatRetentionAllowed[v] }

// GetSecuritySettings 读保留策略；未设置时回退默认值（回收站 30 天 / 对话 30）。
func (d *DB) GetSecuritySettings(ctx context.Context) (*SecuritySettings, error) {
	s := &SecuritySettings{RecycleRetentionDays: 30, ChatRetention: "30"}

	if v, err := d.GetSetting(ctx, settingRecycleRetentionDays); err == nil {
		if n, convErr := strconv.ParseInt(v, 10, 64); convErr == nil {
			s.RecycleRetentionDays = n
		}
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	if v, err := d.GetSetting(ctx, settingChatRetention); err == nil && v != "" {
		s.ChatRetention = v
	} else if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	return s, nil
}

// SaveSecuritySettings 部分更新：nil 字段不动（按字段合并，不整体覆盖）。
func (d *DB) SaveSecuritySettings(ctx context.Context, recycleDays *int64, chatRetention *string) error {
	if recycleDays != nil {
		if err := d.SetSetting(ctx, settingRecycleRetentionDays, strconv.FormatInt(*recycleDays, 10)); err != nil {
			return err
		}
	}
	if chatRetention != nil {
		if err := d.SetSetting(ctx, settingChatRetention, *chatRetention); err != nil {
			return err
		}
	}
	return nil
}

// ---- AD5 回收站管理（DESIGN §5 / §9）----
//
// 六类可回收内容均以 deleted_at 软删除；删除者由 deleted_by / deleted_by_type 记录
// （user 记发起人 ID，ai 记为「AI 助手」）。恢复 = 清空 deleted_at 并写 restore_logs。
//
// 全局唯一 ID：契约的列表项与恢复路径都只带一个整数 id，但真实库中六张表各自有
// 独立自增序列（动态 1 与评论 1 会并存）。故对外暴露「合成 ID」=
// 表序号 * recycleIDStride + 表内真实 id，恢复时反解回 (表, 真实 id)。
// 前端只做透传，契约形状不变。

// recycleIDStride 合成 ID 的每表区间宽度（远大于单表可达 id 数）。
const recycleIDStride = 1_000_000_000

// recycleSource 描述一张可回收表：中文类型名（契约 type）+ 摘要口径。
type recycleSource struct {
	label   string
	table   string
	alias   string
	summary string // 无别名前缀的 SELECT 表达式
}

// 摘要口径对齐 mock fixtures：动态/评论取正文，照片取文件名，相册/纪念日/点位取名称。
var recycleSources = []recycleSource{
	{"动态", "posts", "p", `CASE WHEN length(content) > 20 THEN substr(content, 1, 20) || '...' ELSE content END`},
	{"评论", "comments", "c", `CASE WHEN length(content) > 20 THEN substr(content, 1, 20) || '...' ELSE content END`},
	{"照片", "media", "m", `COALESCE(NULLIF(file_path, ''), '')`},
	{"相册", "albums", "a", `name`},
	{"纪念日", "anniversaries", "an", `name`},
	{"地图点位", "map_points", "mp", `name`},
}

func recycleSourceByLabel(label string) (int, *recycleSource) {
	for i := range recycleSources {
		if recycleSources[i].label == label {
			return i, &recycleSources[i]
		}
	}
	return -1, nil
}

// encodeRecycleID 合成全局唯一 ID。
func encodeRecycleID(tableIndex int, realID int64) int64 {
	return int64(tableIndex+1)*recycleIDStride + realID
}

// decodeRecycleID 反解合成 ID；非法返回 ErrNotFound。
func decodeRecycleID(id int64) (int, int64, error) {
	if id <= 0 {
		return 0, 0, ErrNotFound
	}
	idx := int(id/recycleIDStride) - 1
	real := id % recycleIDStride
	if idx < 0 || idx >= len(recycleSources) || real <= 0 {
		return 0, 0, ErrNotFound
	}
	return idx, real, nil
}

// RecycleItem 回收站条目（契约 routes.mjs：{id,type,summary,deletedAt,deletedBy}）。
type RecycleItem struct {
	ID        int64
	Type      string
	Summary   string
	DeletedAt string
	DeletedBy string
}

// RestoreLogEntry 恢复日志条目（契约：{time,operator,summary}）。
type RestoreLogEntry struct {
	Time     string
	Operator string
	Summary  string
}

// baseName 取路径末段（照片摘要按文件名展示，对齐 mock fixtures）。
func baseName(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 && i+1 < len(p) {
		return p[i+1:]
	}
	return p
}

// ListRecycle 聚合六类已删除内容，按删除时间倒序。
func (d *DB) ListRecycle(ctx context.Context) ([]*RecycleItem, error) {
	parts := make([]string, 0, len(recycleSources))
	for i, s := range recycleSources {
		al := s.alias
		parts = append(parts, fmt.Sprintf(`SELECT
				%d + %s.id AS id, '%s' AS type, %s AS summary,
				COALESCE(%s.deleted_at, '') AS deleted_at,
				CASE WHEN %s.deleted_by_type = 'ai' THEN 'AI 助手'
				     ELSE COALESCE(NULLIF(u.nickname, ''), u.username, '') END AS deleted_by
			FROM %s %s
			LEFT JOIN users u ON u.id = %s.deleted_by
			WHERE %s.deleted_at IS NOT NULL`,
			int64(i+1)*recycleIDStride, al, s.label, s.summary, al, al, s.table, al, al, al))
	}

	query := strings.Join(parts, " UNION ALL ") + ` ORDER BY deleted_at DESC`

	rows, err := d.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*RecycleItem, 0)
	for rows.Next() {
		var it RecycleItem
		if err := rows.Scan(&it.ID, &it.Type, &it.Summary, &it.DeletedAt, &it.DeletedBy); err != nil {
			return nil, err
		}
		if it.Type == "照片" {
			it.Summary = baseName(it.Summary)
		}
		out = append(out, &it)
	}
	return out, rows.Err()
}

// RestoreResult 批量恢复结果。
type RestoreResult struct {
	Restored int64
	// Summaries 已恢复内容摘要（写恢复日志用）。
	Summaries []string
}

// RestoreRecycle 按合成 ID 批量恢复。id 非空、未命中 → ErrNotFound。
func (d *DB) RestoreRecycle(ctx context.Context, ids []int64) (*RestoreResult, error) {
	res := &RestoreResult{}
	if len(ids) == 0 {
		return res, nil
	}

	// 按表分组：合成 ID 反解回 (表, 真实 id)，避免跨表误恢复。
	grouped := map[int][]int64{}
	var restoredPostIDs []int64
	for _, id := range ids {
		idx, real, err := decodeRecycleID(id)
		if err != nil {
			return nil, ErrNotFound
		}
		grouped[idx] = append(grouped[idx], real)
	}

	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	for idx, realIDs := range grouped {
		s := &recycleSources[idx]
		ph := placeholders(len(realIDs))
		args := make([]any, 0, len(realIDs))
		for _, r := range realIDs {
			args = append(args, r)
		}

		// 先取摘要与真实 id：恢复后 deleted_at 清空，条件不再命中。
		sumRows, err := tx.QueryContext(ctx, fmt.Sprintf(
			`SELECT id, %s FROM %s WHERE id IN (%s) AND deleted_at IS NOT NULL`, s.summary, s.table, ph), args...)
		if err != nil {
			return nil, err
		}
		var summaries []string
		var restoredIDs []int64
		for sumRows.Next() {
			var id int64
			var sum string
			if err := sumRows.Scan(&id, &sum); err != nil {
				sumRows.Close()
				return nil, err
			}
			if s.label == "照片" {
				sum = baseName(sum)
			}
			summaries = append(summaries, sum)
			restoredIDs = append(restoredIDs, id)
		}
		sumRows.Close()
		if err := sumRows.Err(); err != nil {
			return nil, err
		}

		r, err := tx.ExecContext(ctx, fmt.Sprintf(
			`UPDATE %s SET deleted_at = NULL, deleted_by = NULL, deleted_by_type = NULL WHERE id IN (%s) AND deleted_at IS NOT NULL`,
			s.table, ph), args...)
		if err != nil {
			return nil, err
		}
		n, _ := r.RowsAffected()
		if n > 0 {
			res.Restored += n
			for _, sum := range summaries {
				res.Summaries = append(res.Summaries, fmt.Sprintf("%s「%s」", s.label, sum))
			}

			// 恢复动态须连带恢复其级联删除的媒体：SoftDeletePost 把媒体一并软删，
			// 若此处只恢复动态本身，动态会「复活」但照片仍不可见（媒体标记未清）。
			if s.table == "posts" {
				// 只回填本次真正恢复的行（含已未删的 id 会多打一次嵌入）
				restoredPostIDs = append(restoredPostIDs, restoredIDs...)
				if _, err := tx.ExecContext(ctx, fmt.Sprintf(
					`UPDATE media SET deleted_at = NULL, deleted_by = NULL, deleted_by_type = NULL
					 WHERE post_id IN (%s) AND deleted_at IS NOT NULL`, ph), args...); err != nil {
					return nil, err
				}
			}
		}
	}

	if res.Restored == 0 {
		return nil, ErrNotFound
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// 恢复已生效，随后重建向量索引；索引失败只告警，不回滚恢复。
	for _, id := range restoredPostIDs {
		p, err := d.GetPost(ctx, id)
		if err != nil {
			slog.Warn("reindex restored post failed", "post_id", id, "err", err)
			continue
		}
		d.indexPost(ctx, p.ID, p.Title.String, p.Content, p.LocationName.String)
	}
	return res, nil
}

// AppendRestoreLog 写恢复日志（永久保留，不可删除）。
func (d *DB) AppendRestoreLog(ctx context.Context, operator, summary string) error {
	_, err := d.ExecContext(ctx,
		`INSERT INTO restore_logs (operator, summary) VALUES (?, ?)`, operator, summary)
	return err
}

// ListRestoreLogs 恢复日志，最新在前。
func (d *DB) ListRestoreLogs(ctx context.Context) ([]*RestoreLogEntry, error) {
	rows, err := d.QueryContext(ctx,
		`SELECT created_at, operator, summary FROM restore_logs ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*RestoreLogEntry, 0)
	for rows.Next() {
		var e RestoreLogEntry
		if err := rows.Scan(&e.Time, &e.Operator, &e.Summary); err != nil {
			return nil, err
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}
