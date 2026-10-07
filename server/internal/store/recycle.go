package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

// ---- 数据安全设置----
//
// 两项保留设置各自独立保存，PUT 为部分更新：请求体可只含本次变更字段，
// 服务端按字段合并，不得整体覆盖（ 已记录该语义）。

const (
	settingRecycleRetentionDays = "recycle_retention_days"

	// 回收站保留期限范围：7~365 天，默认 30
	RecycleRetentionMin = 7
	RecycleRetentionMax = 365
)

// SecuritySettings 数据安全设置。
type SecuritySettings struct {
	RecycleRetentionDays int64
}

// GetSecuritySettings 读保留策略；未设置时回退默认值（回收站 30 天 / 对话 30）。
func (d *DB) GetSecuritySettings(ctx context.Context) (*SecuritySettings, error) {
	s := &SecuritySettings{RecycleRetentionDays: 30}

	if v, err := d.GetSetting(ctx, settingRecycleRetentionDays); err == nil {
		if n, convErr := strconv.ParseInt(v, 10, 64); convErr == nil {
			s.RecycleRetentionDays = n
		}
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	return s, nil
}

// SaveSecuritySettings 部分更新：nil 字段不动（按字段合并，不整体覆盖）。
func (d *DB) SaveSecuritySettings(ctx context.Context, recycleDays *int64) error {
	if recycleDays != nil {
		if err := d.SetSetting(ctx, settingRecycleRetentionDays, strconv.FormatInt(*recycleDays, 10)); err != nil {
			return err
		}
	}
	return nil
}

// ---- 回收站管理----
//
// 可回收内容均以 deleted_at 软删除；删除者由 deleted_by / deleted_by_type 记录
// （user 记发起人 ID，ai 记为「AI 助手」）。恢复 = 清空 deleted_at 并写 restore_logs。
//
// 参与回收的表共六张（动态 / 评论 / 照片 / 相册 / 纪念日 / 地图点位），其中照片不对外
// 列出（listed=false）——对外呈现与筛选是五类（PRODUCT「后台管理」）。
//
// 全局唯一 ID：契约的列表项与恢复路径都只带一个整数 id，但真实库中六张表各自有
// 独立自增序列（动态 1 与评论 1 会并存）。故对外暴露「合成 ID」=
// 表序号 * recycleIDStride + 表内真实 id，恢复时反解回 (表, 真实 id)。
// 前端只做透传，契约形状不变。

// recycleIDStride 合成 ID 的每表区间宽度（远大于单表可达 id 数）。
const recycleIDStride = 1_000_000_000

// recycleSource 描述一张可回收表：中文类型名（契约 type）+ 摘要口径 + 是否对外列出。
// listed=false（照片）：媒体无独立生命周期，随所属动态一并进回收站、随动态恢复
// （PRODUCT「相册（独立底部 tab）」）；该表仍参与 purge 计数与日志，但不进列表、不可单独恢复。
type recycleSource struct {
	label   string
	table   string
	alias   string
	summary string // 无别名前缀的 SELECT 表达式
	listed  bool
}

// 摘要口径对齐 mock fixtures：动态/评论取正文，照片取文件名，相册/纪念日/点位取名称。
var recycleSources = []recycleSource{
	{"动态", "posts", "p", `CASE WHEN length(content) > 20 THEN substr(content, 1, 20) || '...' ELSE content END`, true},
	{"评论", "comments", "c", `CASE WHEN length(content) > 20 THEN substr(content, 1, 20) || '...' ELSE content END`, true},
	{"照片", "media", "m", `COALESCE(NULLIF(file_path, ''), '')`, false},
	{"相册", "albums", "a", `name`, true},
	{"纪念日", "anniversaries", "an", `name`, true},
	{"地图点位", "map_points", "mp", `name`, true},
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

// RestoreLogEntry 回收站操作日志条目（恢复 + 系统永久删除；契约：{time,operator,summary}）。
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

// ListRecycle 聚合五类已删除内容（照片不单列），按删除时间倒序。
func (d *DB) ListRecycle(ctx context.Context) ([]*RecycleItem, error) {
	parts := make([]string, 0, len(recycleSources))
	for i, s := range recycleSources {
		if !s.listed {
			continue
		}
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
	// Summaries 已恢复内容摘要（写回收站操作日志用）。
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
		if !s.listed {
			return nil, ErrNotFound // 照片等未列出类型：恢复入口不存在（校验前置，不做半恢复）
		}
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
		d.indexPost(ctx, p.ID, p.Content, p.LocationName.String)
	}
	return res, nil
}

// AppendRestoreLog 写回收站操作日志（恢复 + 系统超期永久删除；永久保留，不可删除）。
func (d *DB) AppendRestoreLog(ctx context.Context, operator, summary string) error {
	_, err := d.ExecContext(ctx,
		`INSERT INTO restore_logs (operator, summary) VALUES (?, ?)`, operator, summary)
	return err
}

// ListRestoreLogs 回收站操作日志，最新在前。
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

// PurgeResult 永久删除结果：各类型删除条数（键为 recycleSources 的 label）与待物理删除的媒体相对路径。
type PurgeResult struct {
	ByType     map[string]int64
	MediaPaths []string
}

// Total 删除总条数（六类实体之和）。
func (r *PurgeResult) Total() int64 {
	var n int64
	for _, c := range r.ByType {
		n += c
	}
	return n
}

// Summary 供回收站操作日志的确定性摘要（按 recycleSources 顺序）；无删除时返回空串。
func (r *PurgeResult) Summary() string {
	if r == nil || r.Total() == 0 {
		return ""
	}
	parts := make([]string, 0, len(recycleSources))
	for _, s := range recycleSources {
		if n := r.ByType[s.label]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", s.label, n))
		}
	}
	return "系统永久删除：" + strings.Join(parts, "、")
}

// recycleLabelOf 表名 → recycleSources 的中文类型名（保持单一来源，避免第二份表清单漂移）。
func recycleLabelOf(table string) string {
	for _, s := range recycleSources {
		if s.table == table {
			return s.label
		}
	}
	return table
}

// PurgeExpiredRecycle 永久删除回收站中超期内容（deleted_at < cutoff）。
// 这是唯一允许物理删除的路径：只由系统定时任务调用，无人工入口。
// cutoff 由调用方按「当前时间 - 保留期」以 UTC 计算（可注入，使「恰好到期」边界可确定性测试）。
// 子行按 id 集合无条件清理（不按 deleted_at 过滤）——live 评论、已单独恢复的媒体若漏删，
// 会在 foreign_keys(1) 下触发外键失败并回滚整轮。
// 返回各类型条数与待物理删除的媒体路径（主文件 + 缩略图）；文件删除由调用方负责。
func (d *DB) PurgeExpiredRecycle(ctx context.Context, cutoff time.Time) (*PurgeResult, error) {
	ts := cutoff.UTC().Format("2006-01-02 15:04:05")
	res := &PurgeResult{ByType: map[string]int64{}}

	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	const (
		// 归一化比较：历史库可能残留 RFC3339 格式（见 server/migrations/fix-time-format.sql），
		// 裸字符串比较会把同日更早的时间误判成更晚而漏删。牺牲索引换正确性（每日一轮、万行量级）。
		expiredAt            = `deleted_at IS NOT NULL AND datetime(deleted_at) < datetime(?)`
		belongsToExpiredPost = `post_id IN (SELECT id FROM posts WHERE ` + expiredAt + `)`
		mediaToDelete        = `(SELECT id FROM media WHERE ` + expiredAt + ` OR ` + belongsToExpiredPost + `)`
	)

	// 先取待删媒体路径（必须在删行之前）。
	rows, err := tx.QueryContext(ctx,
		`SELECT file_path, COALESCE(thumb_path, '') FROM media WHERE `+expiredAt+` OR `+belongsToExpiredPost, ts, ts)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var file, thumb string
		if err := rows.Scan(&file, &thumb); err != nil {
			rows.Close()
			return nil, err
		}
		if file != "" {
			res.MediaPaths = append(res.MediaPaths, file)
		}
		if thumb != "" {
			res.MediaPaths = append(res.MediaPaths, thumb)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	del := func(table, where string, args ...any) (int64, error) {
		r, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE "+where, args...)
		if err != nil {
			return 0, err
		}
		n, _ := r.RowsAffected()
		return n, nil
	}

	// 0) 相册封面指向待删媒体时置空，让相册回退到其它照片（避免留下指向已删媒体的破图）。
	if _, err := tx.ExecContext(ctx, `UPDATE albums SET cover_media_id = NULL WHERE cover_media_id IN `+mediaToDelete, ts, ts); err != nil {
		return nil, err
	}

	// 1) album_items：待删媒体 或 过期相册（须先于 media/albums）。
	if _, err := del("album_items",
		`media_id IN `+mediaToDelete+` OR album_id IN (SELECT id FROM albums WHERE `+expiredAt+`)`, ts, ts, ts); err != nil {
		return nil, err
	}

	// 2) comments：过期动态下的全部（含未软删）+ 自身超期。
	n, err := del("comments", belongsToExpiredPost+` OR (`+expiredAt+`)`, ts, ts)
	if err != nil {
		return nil, err
	}
	res.ByType[recycleLabelOf("comments")] = n

	// 3) 纯关联表：点赞 / 收藏（无回收站语义，随动态物理删除）。
	if _, err := del("likes", belongsToExpiredPost, ts); err != nil {
		return nil, err
	}
	if _, err := del("favorites", belongsToExpiredPost, ts); err != nil {
		return nil, err
	}

	// 4) media：自身超期 或 属于过期动态。
	n, err = del("media", `(`+expiredAt+`) OR `+belongsToExpiredPost, ts, ts)
	if err != nil {
		return nil, err
	}
	res.ByType[recycleLabelOf("media")] = n

	// 5) posts：自身超期。
	n, err = del("posts", expiredAt, ts)
	if err != nil {
		return nil, err
	}
	res.ByType[recycleLabelOf("posts")] = n

	// 6) albums：自身超期。
	n, err = del("albums", expiredAt, ts)
	if err != nil {
		return nil, err
	}
	res.ByType[recycleLabelOf("albums")] = n

	// 7) 无入向引用的两张表：纪念日 / 地图点位。
	n, err = del("anniversaries", expiredAt, ts)
	if err != nil {
		return nil, err
	}
	res.ByType[recycleLabelOf("anniversaries")] = n
	n, err = del("map_points", expiredAt, ts)
	if err != nil {
		return nil, err
	}
	res.ByType[recycleLabelOf("map_points")] = n

	for label, c := range res.ByType {
		if c == 0 {
			delete(res.ByType, label)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return res, nil
}
