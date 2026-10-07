package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

type Post struct {
	ID           int64
	AuthorID     int64
	Content      string
	Mood         sql.NullString
	LocationName sql.NullString
	PointID      sql.NullInt64
	CreatedAt    string
	DeletedAt    sql.NullString

	AuthorNickname sql.NullString // 联表
}

type MediaRow struct {
	ID        int64
	PostID    int64
	Kind      string
	FilePath  string
	ThumbPath sql.NullString
	Width     int
	Height    int
	Size      int64
	Duration  sql.NullFloat64
	TakenAt   sql.NullString
	CreatedAt string
	AuthorID  int64 // 经 JOIN posts 取（媒体无独立作者，mock 口径=动态作者）
}

// SortTime 时间轴排序键：taken_at 优先，回退 created_at（口径，mock 一致）。
func (m *MediaRow) SortTime() string {
	if m.TakenAt.Valid && m.TakenAt.String != "" {
		return m.TakenAt.String
	}
	return m.CreatedAt
}

// NullStr 构造可空字符串字段（供 API 与 AI 工具层使用）。
func NullStr(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// indexPost / unindexPost 转发索引钩子。索引是派生数据：
// 失败只告警，绝不反向阻断动态落库（未配置嵌入模型时钩子为空）。
func (d *DB) indexPost(ctx context.Context, id int64, content, location string) {
	if d.postIndex == nil {
		return
	}
	if err := d.postIndex.IndexPost(ctx, id, content, location); err != nil {
		slog.Warn("index post failed", "post_id", id, "err", err)
	}
}

func (d *DB) unindexPost(ctx context.Context, id int64) {
	if d.postIndex == nil {
		return
	}
	if err := d.postIndex.UnindexPost(ctx, id); err != nil {
		slog.Warn("unindex post failed", "post_id", id, "err", err)
	}
}

// CreatePostWithMedia 单事务创建动态与媒体行（媒体文件已由管线转正，失败由调用方回收）。
func (d *DB) CreatePostWithMedia(ctx context.Context, p *Post, medias []MediaRow) (int64, error) {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx,
		`INSERT INTO posts (author_id, content, mood, location_name, point_id) VALUES (?, ?, ?, ?, ?)`,
		p.AuthorID, p.Content, p.Mood, p.LocationName, p.PointID)
	if err != nil {
		return 0, err
	}
	postID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	for _, m := range medias {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO media (post_id, kind, file_path, thumb_path, width, height, size, duration, taken_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			postID, m.Kind, m.FilePath, m.ThumbPath, m.Width, m.Height, m.Size, m.Duration, m.TakenAt); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	d.indexPost(ctx, postID, p.Content, p.LocationName.String)
	return postID, nil
}

const postColumns = `p.id, p.author_id, p.content, p.mood, p.location_name, p.point_id, p.created_at, p.deleted_at, COALESCE(u.nickname, u.username)`

func scanPost(row interface{ Scan(...any) error }) (*Post, error) {
	var p Post
	err := row.Scan(&p.ID, &p.AuthorID, &p.Content, &p.Mood, &p.LocationName, &p.PointID, &p.CreatedAt, &p.DeletedAt, &p.AuthorNickname)
	return &p, err
}

// ListPosts 分页倒序；deleted=show 时含已删（后台回收站用，默认过滤）。
// memories=true 时只取「那年今日」——历年在今天同月同日发布的动态（**不含当年**，
// 「同月同日历史回忆」）；过滤发生在分页之前，total 为过滤后总数（契约同 mock）。
// 月/年判定按部署者本地时区（库内 created_at 为 UTC，见 timeutil.go 时间口径）。
func (d *DB) ListPosts(ctx context.Context, page, pageSize int, memories bool) ([]*Post, int, error) {
	// 两个查询共用 WHERE，故一律用 p. 限定列名（users 也有 created_at，不限定会 ambiguous）
	where := ` WHERE p.deleted_at IS NULL`
	if memories {
		where += ` AND strftime('%m-%d', p.created_at, 'localtime') = strftime('%m-%d', 'now', 'localtime')` +
			` AND strftime('%Y', p.created_at, 'localtime') < strftime('%Y', 'now', 'localtime')`
	}
	var total int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM posts p`+where).Scan(&total); err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	rows, err := d.QueryContext(ctx, `
		SELECT `+postColumns+` FROM posts p JOIN users u ON u.id = p.author_id`+where+` ORDER BY p.id DESC LIMIT ? OFFSET ?`, pageSize, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var posts []*Post
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, 0, err
		}
		posts = append(posts, p)
	}
	return posts, total, rows.Err()
}

// PostsByIDs 取未删动态，返回顺序与入参一致（供语义检索还原相似度序）；
// 已删或缺失的 id 直接跳过。
func (d *DB) PostsByIDs(ctx context.Context, ids []int64) ([]*Post, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := d.QueryContext(ctx, `
		SELECT `+postColumns+` FROM posts p JOIN users u ON u.id = p.author_id
		WHERE p.deleted_at IS NULL AND p.id IN (`+placeholders(len(ids))+`)`, int64Args(ids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := make(map[int64]*Post, len(ids))
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		byID[p.ID] = p
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	posts := make([]*Post, 0, len(byID))
	for _, id := range ids {
		if p, ok := byID[id]; ok {
			posts = append(posts, p)
		}
	}
	return posts, nil
}

func (d *DB) GetPost(ctx context.Context, id int64) (*Post, error) {
	row := d.QueryRowContext(ctx, `
		SELECT `+postColumns+` FROM posts p JOIN users u ON u.id = p.author_id
		WHERE p.id = ? AND p.deleted_at IS NULL`, id)
	p, err := scanPost(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

// SoftDeletePost 软删除动态并级联其媒体（媒体只能随动态存在， 口径）；
// actorType ∈ ('user','ai')、actorID 为发起人，供回收站「删除者」列。
func (d *DB) SoftDeletePost(ctx context.Context, id int64, actorType string, actorID int64) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx,
		`UPDATE posts SET deleted_at = datetime('now'), deleted_by = ?, deleted_by_type = ? WHERE id = ? AND deleted_at IS NULL`,
		actorID, actorType, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}

	// 级联媒体：随动态一并进回收站（各自作为「照片」条目，可单独恢复）
	if _, err := tx.ExecContext(ctx,
		`UPDATE media SET deleted_at = datetime('now'), deleted_by = ?, deleted_by_type = ? WHERE post_id = ? AND deleted_at IS NULL`,
		actorID, actorType, id); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	d.unindexPost(ctx, id)
	return nil
}

func (d *DB) GetMediaByPostIDs(ctx context.Context, postIDs []int64) (map[int64][]*MediaRow, error) {
	out := map[int64][]*MediaRow{}
	if len(postIDs) == 0 {
		return out, nil
	}
	placeholders := strings.Repeat("?,", len(postIDs))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(postIDs))
	for i, id := range postIDs {
		args[i] = id
	}
	rows, err := d.QueryContext(ctx, `SELECT m.id, m.post_id, m.kind, m.file_path, m.thumb_path, m.width, m.height, m.size,
			m.duration, m.taken_at, m.created_at, p.author_id
		FROM media m JOIN posts p ON p.id = m.post_id WHERE m.post_id IN (`+placeholders+`) AND m.deleted_at IS NULL ORDER BY m.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		m, err := scanMediaRow(rows)
		if err != nil {
			return nil, err
		}
		out[m.PostID] = append(out[m.PostID], m)
	}
	return out, rows.Err()
}

// scanMediaRow 统一 MediaRow 行扫描（12 列序见各查询 SELECT：id/post_id/kind/file_path/
// thumb_path/width/height/size/duration/taken_at/created_at/author_id）。
func scanMediaRow(row interface{ Scan(...any) error }) (*MediaRow, error) {
	var m MediaRow
	err := row.Scan(&m.ID, &m.PostID, &m.Kind, &m.FilePath, &m.ThumbPath, &m.Width, &m.Height, &m.Size,
		&m.Duration, &m.TakenAt, &m.CreatedAt, &m.AuthorID)
	return &m, err
}

// GetMediaByIDs 批量取媒体（reurl）：仅未删除的命中项，入参无效 id 自然跳过。
func (d *DB) GetMediaByIDs(ctx context.Context, ids []int64) ([]*MediaRow, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	ph := placeholders(len(ids))
	args := int64Args(ids)
	rows, err := d.QueryContext(ctx, `SELECT m.id, m.post_id, m.kind, m.file_path, m.thumb_path, m.width, m.height, m.size,
			m.duration, m.taken_at, m.created_at, p.author_id
		FROM media m JOIN posts p ON p.id = m.post_id WHERE m.id IN (`+ph+`) AND m.deleted_at IS NULL ORDER BY m.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*MediaRow
	for rows.Next() {
		m, err := scanMediaRow(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, m)
	}
	return list, rows.Err()
}

func (d *DB) GetMedia(ctx context.Context, id int64) (*MediaRow, error) {
	row := d.QueryRowContext(ctx, `SELECT m.id, m.post_id, m.kind, m.file_path, m.thumb_path, m.width, m.height, m.size,
			m.duration, m.taken_at, m.created_at, p.author_id
		FROM media m JOIN posts p ON p.id = m.post_id WHERE m.id = ? AND m.deleted_at IS NULL`, id)
	m, err := scanMediaRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return m, nil
}

// ListPostsOnMonthDay 那年今日（get_today_in_history）：历年在今天同月同日创建的动态，
// yearsBack 限制回看年数上限，按时间倒序。
// 月/年判定按**部署者本地时区**（库内 created_at 为 UTC，见 timeutil.go 时间口径）。
func (d *DB) ListPostsOnMonthDay(ctx context.Context, yearsBack int, limit int) ([]*Post, int, error) {
	if yearsBack <= 0 {
		yearsBack = 50
	}
	// 同一 WHERE 下推计数：列表卡的 total/more 要用真值，不能拿「命中数 == limit」猜
	// （AI模式设计「计数与翻页」：真实余量 total - len(records)，未知则两者都省略）。
	const where = ` FROM posts p
		WHERE p.deleted_at IS NULL
		  AND strftime('%m-%d', p.created_at, 'localtime') = strftime('%m-%d', 'now', 'localtime')
		  AND strftime('%Y', p.created_at, 'localtime') < strftime('%Y', 'now', 'localtime')
		  AND p.created_at >= datetime('now', ?)`
	since := fmt.Sprintf("-%d years", yearsBack)
	var total int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*)`+where, since).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := d.QueryContext(ctx, `SELECT `+postColumns+`
		FROM posts p JOIN users u ON u.id = p.author_id
		WHERE p.deleted_at IS NULL
		  AND strftime('%m-%d', p.created_at, 'localtime') = strftime('%m-%d', 'now', 'localtime')
		  AND strftime('%Y', p.created_at, 'localtime') < strftime('%Y', 'now', 'localtime')
		  AND p.created_at >= datetime('now', ?)
		ORDER BY p.created_at DESC LIMIT ?`, since, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var list []*Post
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, 0, err
		}
		list = append(list, p)
	}
	return list, total, rows.Err()
}

// ListPostsByPoint 某地点下的动态（get_location_posts）：动态带地点时写入的 point_id 反查。
func (d *DB) ListPostsByPoint(ctx context.Context, pointID int64, limit int) ([]*Post, error) {
	rows, err := d.QueryContext(ctx, `SELECT `+postColumns+`
		FROM posts p JOIN users u ON u.id = p.author_id
		WHERE p.deleted_at IS NULL AND p.point_id = ?
		ORDER BY p.id DESC LIMIT ?`, pointID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*Post
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	return list, rows.Err()
}

// TimelinePostCounts 时间线统计（get_timeline_summary）：无参按年分布；
// year 非空按该年月度分布；month(YYYY-MM) 非空按该月日度分布。
func (d *DB) TimelinePostCounts(ctx context.Context, year, month string) ([]PeriodCount, error) {
	// 已软删动态不进统计（PRODUCT「卡片体系」统计概览卡「数字准且即时」；
	// 与同工具 postTotal 的 deleted_at IS NULL 口径一致，否则同卡自相矛盾）。
	group := "strftime('%Y', created_at)"
	where := " WHERE deleted_at IS NULL"
	args := []any{}
	switch {
	case month != "":
		group = "strftime('%Y-%m-%d', created_at)"
		where = " WHERE deleted_at IS NULL AND strftime('%Y-%m', created_at) = ?"
		args = append(args, month)
	case year != "":
		group = "strftime('%Y-%m', created_at)"
		where = " WHERE deleted_at IS NULL AND strftime('%Y', created_at) = ?"
		args = append(args, year)
	}
	rows, err := d.QueryContext(ctx, `SELECT `+group+` AS period, COUNT(*) FROM posts`+where+`
		GROUP BY period ORDER BY period`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []PeriodCount
	for rows.Next() {
		var pc PeriodCount
		if err := rows.Scan(&pc.Period, &pc.Count); err != nil {
			return nil, err
		}
		list = append(list, pc)
	}
	return list, rows.Err()
}

// PeriodCount 时间段聚合行（get_timeline_summary 输出）。
type PeriodCount struct {
	Period string
	Count  int64
}
