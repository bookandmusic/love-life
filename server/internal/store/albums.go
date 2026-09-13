package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

type Album struct {
	ID          int64
	Type        string
	Name        string
	Description sql.NullString
	MonthKey    sql.NullString
	IsHidden    bool
	CreatedAt   string
	// CreatedBy：创建者 ID。月度相册由系统生成、无创建者，故用 NullInt64
	// （若用 int64，月度相册读出来会是 0，与真实用户 ID 无法区分）。
	CreatedBy    sql.NullInt64
	ItemCount    int
	CoverMediaID sql.NullInt64
}

func (d *DB) CreateAlbum(ctx context.Context, typ, name, description, monthKey string, createdBy int64) (int64, error) {
	res, err := d.ExecContext(ctx,
		`INSERT INTO albums (type, name, description, month_key, created_by) VALUES (?, ?, ?, ?, ?)`,
		typ, name, description, monthKey, createdBy)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) ListAlbums(ctx context.Context, includeHidden bool) ([]*Album, error) {
	query := `SELECT a.id, a.type, a.name, a.description, a.month_key, a.is_hidden, a.created_at, a.created_by,
			CASE WHEN a.type = 'monthly' THEN
				-- 月度相册虚拟口径（D4）：该月未归属手动相册的媒体数
				(SELECT COUNT(*) FROM media m2 JOIN posts p2 ON p2.id = m2.post_id
				 WHERE m2.deleted_at IS NULL AND p2.deleted_at IS NULL
				   AND COALESCE(NULLIF(strftime('%Y-%m', m2.taken_at), ''), strftime('%Y-%m', m2.created_at, 'localtime')) = a.month_key
				   AND NOT EXISTS (SELECT 1 FROM album_items i2 JOIN albums a2 ON a2.id = i2.album_id
				     WHERE i2.media_id = m2.id AND a2.type = 'manual' AND a2.deleted_at IS NULL))
			ELSE
				(SELECT COUNT(*) FROM album_items i JOIN media m ON m.id = i.media_id WHERE i.album_id = a.id AND m.deleted_at IS NULL)
			END,
			a.cover_media_id
		FROM albums a WHERE a.deleted_at IS NULL`
	if !includeHidden {
		query += ` AND a.is_hidden = 0`
	}
	query += ` ORDER BY a.id DESC`
	rows, err := d.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*Album
	for rows.Next() {
		var a Album
		var hidden int
		if err := rows.Scan(&a.ID, &a.Type, &a.Name, &a.Description, &a.MonthKey, &hidden, &a.CreatedAt, &a.CreatedBy, &a.ItemCount, &a.CoverMediaID); err != nil {
			return nil, err
		}
		a.IsHidden = hidden == 1
		list = append(list, &a)
	}
	return list, rows.Err()
}

func (d *DB) GetAlbum(ctx context.Context, id int64) (*Album, error) {
	row := d.QueryRowContext(ctx, `SELECT id, type, name, description, month_key, is_hidden, created_at, created_by, 0, cover_media_id
		FROM albums WHERE id = ? AND deleted_at IS NULL`, id)
	var a Album
	var hidden int
	err := row.Scan(&a.ID, &a.Type, &a.Name, &a.Description, &a.MonthKey, &hidden, &a.CreatedAt, &a.CreatedBy, &a.ItemCount, &a.CoverMediaID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	a.IsHidden = hidden == 1
	return &a, nil
}

// SoftDeleteAlbum 仅手动相册可删（月度相册规则见需求 §4.5）；
// 同时释放媒体归属（对齐 mock「删除相册→媒体回未归属」，关系数据不进回收站）。
func (d *DB) SoftDeleteAlbum(ctx context.Context, id int64, actorType string, actorID int64) error {
	res, err := d.ExecContext(ctx,
		`UPDATE albums SET deleted_at = datetime('now'), deleted_by = ?, deleted_by_type = ? WHERE id = ? AND deleted_at IS NULL AND type = 'manual'`,
		actorID, actorType, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	_, err = d.ExecContext(ctx, `DELETE FROM album_items WHERE album_id = ?`, id)
	return err
}

func (d *DB) SetAlbumHidden(ctx context.Context, id int64, hidden bool) error {
	res, err := d.ExecContext(ctx, `UPDATE albums SET is_hidden = ? WHERE id = ? AND deleted_at IS NULL`, hidden, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// MediaOwnedError 媒体已归属其他手动相册（一文件一相册，需求 §4.5）。
// Error() 即 mock 契约 409 文案（AGENTS §4 mock 为事实源），API 层直接透传，格式串单源此处。
type MediaOwnedError struct {
	MediaID int64 // 触发冲突的媒体 id
	OwnedBy int64 // 已占用的相册 id
}

func (e *MediaOwnedError) Error() string {
	return fmt.Sprintf("媒体 %d 已归属相册 #%d，一文件只能归属一个手动相册", e.MediaID, e.OwnedBy)
}

// AddAlbumItems 加入相册：媒体存在且未软删预检 → 跨相册预检（活跃手动相册口径）→ 409；同相册重复幂等（INSERT OR IGNORE）。
// 预检与插入同事务：并发跨相册归属由事务串行排除预检-插入间隙（TOCTOU），album_items UNIQUE(media_id) 兜底。
func (d *DB) AddAlbumItems(ctx context.Context, albumID, userID int64, mediaIDs []int64) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, mid := range mediaIDs {
		// 媒体必须来自动态媒体池且所属动态未软删（媒体无独立软删，随动态进回收站：以 post.deleted_at 为准）
		var pd sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT p.deleted_at FROM media m JOIN posts p ON p.id = m.post_id WHERE m.id = ?`, mid).Scan(&pd)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("媒体 %d 不存在", mid)
		}
		if err != nil {
			return err
		}
		if pd.Valid && pd.String != "" {
			return fmt.Errorf("媒体 %d 所属动态已删除（回收站），不能加入相册", mid)
		}
		var owner int64
		err = tx.QueryRowContext(ctx, `SELECT i.album_id FROM album_items i
			JOIN albums a ON a.id = i.album_id
			WHERE i.media_id = ? AND a.type = 'manual' AND a.deleted_at IS NULL`, mid).Scan(&owner)
		if err == nil && owner != albumID {
			return &MediaOwnedError{MediaID: mid, OwnedBy: owner}
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	var sb strings.Builder
	args := make([]any, 0, len(mediaIDs)*3)
	for i, mid := range mediaIDs {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString("(?,?,?)")
		args = append(args, albumID, mid, userID)
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO album_items (album_id, media_id, added_by) VALUES `+sb.String(), args...); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DB) RemoveAlbumItems(ctx context.Context, albumID int64, mediaIDs []int64) error {
	ph := placeholders(len(mediaIDs))
	args := int64Args(mediaIDs)
	args = append([]any{albumID}, args...)
	_, err := d.ExecContext(ctx, `DELETE FROM album_items WHERE album_id = ? AND media_id IN (`+ph+`)`, args...)
	return err
}

// ListAlbumMedia 相册内媒体（未删除、按加入倒序）。
func (d *DB) ListAlbumMedia(ctx context.Context, albumID int64) ([]*MediaRow, error) {
	rows, err := d.QueryContext(ctx, `SELECT m.id, m.post_id, m.kind, m.file_path, m.thumb_path, m.width, m.height, m.size,
			m.duration, m.taken_at, m.created_at, p.author_id
		FROM album_items i JOIN media m ON m.id = i.media_id JOIN posts p ON p.id = m.post_id
		WHERE i.album_id = ? AND m.deleted_at IS NULL ORDER BY i.added_at DESC, m.id DESC`, albumID)
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

// 未归属手动相册的媒体条件（月度虚拟口径共用）：活跃手动相册不含该媒体。
const notInManualAlbum = `NOT EXISTS (SELECT 1 FROM album_items i JOIN albums a ON a.id = i.album_id
	  WHERE i.media_id = m.id AND a.type = 'manual' AND a.deleted_at IS NULL)`

// ListMonthlyMedia 月度相册虚拟详情：该月（taken_at||created_at 口径）未归属手动相册的媒体（D4）。
func (d *DB) ListMonthlyMedia(ctx context.Context, monthKey string) ([]*MediaRow, error) {
	rows, err := d.QueryContext(ctx, `SELECT m.id, m.post_id, m.kind, m.file_path, m.thumb_path, m.width, m.height, m.size,
			m.duration, m.taken_at, m.created_at, p.author_id
		FROM media m JOIN posts p ON p.id = m.post_id
		WHERE m.deleted_at IS NULL AND p.deleted_at IS NULL
		  AND COALESCE(NULLIF(strftime('%Y-%m', m.taken_at), ''), strftime('%Y-%m', m.created_at, 'localtime')) = ? AND `+notInManualAlbum+`
		ORDER BY COALESCE(NULLIF(m.taken_at,''), m.created_at) DESC, m.id DESC`, monthKey)
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

// ListTimelineMediaPaged 时间轴分页视图（D5）：mock 契约 page/size + 归属字段。
func (d *DB) ListTimelineMediaPaged(ctx context.Context, page, size int) ([]*MediaRow, int, error) {
	var total int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM media m JOIN posts p ON p.id = m.post_id
		WHERE m.deleted_at IS NULL AND p.deleted_at IS NULL`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := d.QueryContext(ctx, `SELECT m.id, m.post_id, m.kind, m.file_path, m.thumb_path, m.width, m.height, m.size,
			m.duration, m.taken_at, m.created_at, p.author_id
		FROM media m JOIN posts p ON p.id = m.post_id
		WHERE m.deleted_at IS NULL AND p.deleted_at IS NULL
		ORDER BY COALESCE(NULLIF(m.taken_at,''), m.created_at) DESC, m.id DESC LIMIT ? OFFSET ?`, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var list []*MediaRow
	for rows.Next() {
		m, err := scanMediaRow(rows)
		if err != nil {
			return nil, 0, err
		}
		list = append(list, m)
	}
	return list, total, rows.Err()
}

// AlbumRef 媒体当前归属的活跃手动相册（id+名称结伴出现，捆成小类型避免调用端强转）。
type AlbumRef struct {
	ID   int64
	Name string
}

// AlbumOwnership 媒体当前归属（活跃手动相册口径，供时间轴/详情输出 album_id/album_name）；
// 未归属媒体不在返回值中，首次归属胜出。
func (d *DB) AlbumOwnership(ctx context.Context, mediaIDs []int64) (map[int64]AlbumRef, error) {
	out := map[int64]AlbumRef{}
	if len(mediaIDs) == 0 {
		return out, nil
	}
	ph := placeholders(len(mediaIDs))
	args := int64Args(mediaIDs)
	rows, err := d.QueryContext(ctx, `SELECT i.media_id, i.album_id, a.name FROM album_items i
		JOIN albums a ON a.id = i.album_id
		WHERE a.type = 'manual' AND a.deleted_at IS NULL AND i.media_id IN (`+ph+`) ORDER BY i.added_at ASC, i.rowid ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var mid, aid int64
		var name string
		if err := rows.Scan(&mid, &aid, &name); err != nil {
			return nil, err
		}
		if _, ok := out[mid]; !ok { // 首次归属胜出
			out[mid] = AlbumRef{ID: aid, Name: name}
		}
	}
	return out, rows.Err()
}

// ListTimelineMedia 时间轴媒体（保留：cron 月度判定用；APP 侧走 ListTimelineMediaPaged）。
func (d *DB) ListTimelineMedia(ctx context.Context, year, month string, limit int) ([]*MediaRow, error) {
	query := `SELECT m.id, m.post_id, m.kind, m.file_path, m.thumb_path, m.width, m.height, m.size,
			m.duration, m.taken_at, m.created_at, p.author_id
		FROM media m JOIN posts p ON p.id = m.post_id
		WHERE m.deleted_at IS NULL AND p.deleted_at IS NULL`
	var args []any
	if year != "" {
		query += ` AND COALESCE(NULLIF(strftime('%Y', m.taken_at), ''), strftime('%Y', m.created_at, 'localtime')) = ?`
		args = append(args, year)
	}
	if month != "" {
		query += ` AND COALESCE(NULLIF(strftime('%Y-%m', m.taken_at), ''), strftime('%Y-%m', m.created_at, 'localtime')) = ?`
		args = append(args, month)
	}
	query += ` ORDER BY COALESCE(NULLIF(m.taken_at,''), m.created_at) DESC, m.id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := d.QueryContext(ctx, query, args...)
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

// GetMonthlyAlbum 按 month_key 取月度相册（cron 幂等）。
func (d *DB) GetMonthlyAlbum(ctx context.Context, monthKey string) (*Album, error) {
	row := d.QueryRowContext(ctx, `SELECT id, type, name, description, month_key, is_hidden, created_at, 0, cover_media_id
		FROM albums WHERE type = 'monthly' AND month_key = ? AND deleted_at IS NULL`, monthKey)
	var a Album
	var hidden int
	err := row.Scan(&a.ID, &a.Type, &a.Name, &a.Description, &a.MonthKey, &hidden, &a.CreatedAt, &a.ItemCount, &a.CoverMediaID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	a.IsHidden = hidden == 1
	return &a, nil
}

// CreateMonthlyAlbum 月度相册生成（D4 动态化）：仅建册行，不再物理聚合 album_items；
// 详情为虚拟口径（ListMonthlyMedia：该月未归属手动相册的媒体），月度行仅作隐藏开关。
func (d *DB) CreateMonthlyAlbum(ctx context.Context, monthKey, name string, _ int64) (int64, error) {
	res, err := d.ExecContext(ctx,
		`INSERT INTO albums (type, name, month_key) VALUES ('monthly', ?, ?)`, name, monthKey)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GroupCandidate 照片分组建议候选（suggest_photo_grouping）：媒体经所属动态过滤未删，
// 日期键取 taken_at 回退 created_at（SortTime 同口径）。
type GroupCandidate struct {
	MediaID  int64
	Date     string // YYYY-MM-DD
	Location string
}

// ListMediaGroupCandidates 按日期区间（YYYY-MM-DD 含头，可空）与地点名模糊（可空）列分组候选。
func (d *DB) ListMediaGroupCandidates(ctx context.Context, from, to, location string) ([]GroupCandidate, error) {
	where := ` WHERE m.deleted_at IS NULL AND p.deleted_at IS NULL`
	args := []any{}
	if from != "" {
		where += ` AND COALESCE(NULLIF(strftime('%Y-%m-%d', m.taken_at), ''), strftime('%Y-%m-%d', m.created_at, 'localtime')) >= ?`
		args = append(args, from)
	}
	if to != "" {
		where += ` AND COALESCE(NULLIF(strftime('%Y-%m-%d', m.taken_at), ''), strftime('%Y-%m-%d', m.created_at, 'localtime')) <= ?`
		args = append(args, to)
	}
	if location != "" {
		where += ` AND p.location_name LIKE ?`
		args = append(args, "%"+location+"%")
	}
	rows, err := d.QueryContext(ctx, `SELECT m.id,
			COALESCE(NULLIF(strftime('%Y-%m-%d', m.taken_at), ''), strftime('%Y-%m-%d', m.created_at, 'localtime')),
			COALESCE(p.location_name,'')
		FROM media m JOIN posts p ON p.id = m.post_id`+where+` ORDER BY 2 DESC, m.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []GroupCandidate
	for rows.Next() {
		var g GroupCandidate
		if err := rows.Scan(&g.MediaID, &g.Date, &g.Location); err != nil {
			return nil, err
		}
		list = append(list, g)
	}
	return list, rows.Err()
}
