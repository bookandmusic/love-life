package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

type Album struct {
	ID           int64
	Name         string
	Description  sql.NullString
	CreatedAt    string
	CreatedBy    sql.NullInt64
	ItemCount    int
	CoverMediaID sql.NullInt64
}

func (d *DB) CreateAlbum(ctx context.Context, name, description string, createdBy int64) (int64, error) {
	return createAlbumTx(ctx, d, name, description, createdBy)
}

// createAlbumTx 建册的事务体（执行口可为直连或外层事务）。
func createAlbumTx(ctx context.Context, ex sqlExec, name, description string, createdBy int64) (int64, error) {
	res, err := ex.ExecContext(ctx,
		`INSERT INTO albums (name, description, created_by) VALUES (?, ?, ?)`,
		name, description, createdBy)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) ListAlbums(ctx context.Context) ([]*Album, error) {
	query := `SELECT a.id, a.name, a.description, a.created_at, a.created_by,
				(SELECT COUNT(*) FROM album_items i JOIN media m ON m.id = i.media_id WHERE i.album_id = a.id AND m.deleted_at IS NULL),
				a.cover_media_id
		FROM albums a WHERE a.deleted_at IS NULL ORDER BY a.id DESC`
	rows, err := d.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*Album
	for rows.Next() {
		var a Album
		if err := rows.Scan(&a.ID, &a.Name, &a.Description, &a.CreatedAt, &a.CreatedBy, &a.ItemCount, &a.CoverMediaID); err != nil {
			return nil, err
		}
		list = append(list, &a)
	}
	return list, rows.Err()
}

func (d *DB) GetAlbum(ctx context.Context, id int64) (*Album, error) {
	row := d.QueryRowContext(ctx, `SELECT id, name, description, created_at, created_by, 0, cover_media_id
		FROM albums WHERE id = ? AND deleted_at IS NULL`, id)
	var a Album
	err := row.Scan(&a.ID, &a.Name, &a.Description, &a.CreatedAt, &a.CreatedBy, &a.ItemCount, &a.CoverMediaID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// SoftDeleteAlbum 仅手动相册可删（月）；
// 同时释放媒体归属（对齐 mock「删除相册→媒体回未归属」，关系数据不进回收站），
// 且更新与释放同事务——只落一半会留下「已软删但归属仍被占用」的相册。
func (d *DB) SoftDeleteAlbum(ctx context.Context, id int64, actorType string, actorID int64) error {
	return d.softDeleteKindTx(ctx, "album", id, actorType, actorID)
}

// CountAlbumItems 手动相册中未删媒体数（addPhotos/removePhotos 返回 item_count 用）。
func (d *DB) CountAlbumItems(ctx context.Context, albumID int64) (int, error) {
	var n int
	err := d.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM album_items i JOIN media m ON m.id = i.media_id WHERE i.album_id = ? AND m.deleted_at IS NULL`,
		albumID).Scan(&n)
	return n, err
}

// MediaOwnedError 媒体已归属其他手动相册（一文件一相册）。
// Error 即 mock 契约 409 文案（ mock 为事实源），API 层直接透传，格式串单源此处。
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
	if err := addAlbumItemsTx(ctx, tx, albumID, userID, mediaIDs); err != nil {
		return err
	}
	return tx.Commit()
}

// addAlbumItemsTx 加入相册的事务体（由调用方决定事务边界：单次加入，或建册 / 整理相册的同一事务）。
func addAlbumItemsTx(ctx context.Context, tx *sql.Tx, albumID, userID int64, mediaIDs []int64) error {
	if len(mediaIDs) == 0 {
		return nil
	}
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
			WHERE i.media_id = ? AND a.deleted_at IS NULL`, mid).Scan(&owner)
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
	return nil
}

func (d *DB) RemoveAlbumItems(ctx context.Context, albumID int64, mediaIDs []int64) error {
	return removeAlbumItemsTx(ctx, d, albumID, mediaIDs)
}

// removeAlbumItemsTx 移出相册的事务体（执行口可为直连或外层事务）。
func removeAlbumItemsTx(ctx context.Context, ex sqlExec, albumID int64, mediaIDs []int64) error {
	ph := placeholders(len(mediaIDs))
	args := append([]any{albumID}, int64Args(mediaIDs)...)
	_, err := ex.ExecContext(ctx, `DELETE FROM album_items WHERE album_id = ? AND media_id IN (`+ph+`)`, args...)
	return err
}

// CreateAlbumWithItems 建册 + 首批媒体加入同一事务：加媒体失败则相册也不落，
// 不留「已建册但没有媒体」的半成品（AI 一次确认 = 一个原子步）。
func (d *DB) CreateAlbumWithItems(ctx context.Context, name, description string, createdBy int64, mediaIDs []int64) (int64, error) {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	albumID, err := createAlbumTx(ctx, tx, name, description, createdBy)
	if err != nil {
		return 0, err
	}
	if err := addAlbumItemsTx(ctx, tx, albumID, createdBy, mediaIDs); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return albumID, nil
}

// UpdateAlbumMembers 相册增删成员同一事务：加入的预检失败时移出也不落（一次确认 = 一个原子步）。
func (d *DB) UpdateAlbumMembers(ctx context.Context, albumID, userID int64, addIDs, removeIDs []int64) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if len(addIDs) > 0 {
		if err := addAlbumItemsTx(ctx, tx, albumID, userID, addIDs); err != nil {
			return err
		}
	}
	if len(removeIDs) > 0 {
		if err := removeAlbumItemsTx(ctx, tx, albumID, removeIDs); err != nil {
			return err
		}
	}
	return tx.Commit()
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

// notInAlbum 未归属相册的媒体条件（时间轴与月度分组共用口径）；alias = 媒体表别名。
func notInAlbum(alias string) string {
	return `NOT EXISTS (SELECT 1 FROM album_items i JOIN albums a ON a.id = i.album_id
	  WHERE i.media_id = ` + alias + `.id AND a.deleted_at IS NULL)`
}

// ListMonthlyMedia 月度相册虚拟详情：该月（taken_at||created_at 口径）未归属手动相册的媒体。
func (d *DB) ListMonthlyMedia(ctx context.Context, monthKey string) ([]*MediaRow, error) {
	rows, err := d.QueryContext(ctx, `SELECT m.id, m.post_id, m.kind, m.file_path, m.thumb_path, m.width, m.height, m.size,
			m.duration, m.taken_at, m.created_at, p.author_id
		FROM media m JOIN posts p ON p.id = m.post_id
		WHERE m.deleted_at IS NULL AND p.deleted_at IS NULL
		  AND COALESCE(NULLIF(strftime('%Y-%m', m.taken_at), ''), strftime('%Y-%m', m.created_at, 'localtime')) = ? AND `+notInAlbum("m")+`
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

// ListTimelineMediaPaged 时间轴分页视图（mock 契约 page/size + 归属字段）。
// 口径（2026-09-22 确认）：时间轴 = 未归档媒体——已归属任一活跃手动相册的媒体不出现；
// 从相册移出（解除归属）即回到时间轴。includeFiled=true 供选图器场景（需全部媒体做灰显）。
func (d *DB) ListTimelineMediaPaged(ctx context.Context, page, size int, includeFiled bool) ([]*MediaRow, int, error) {
	where := `m.deleted_at IS NULL AND p.deleted_at IS NULL`
	if !includeFiled {
		where += ` AND ` + notInAlbum("m")
	}
	var total int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM media m JOIN posts p ON p.id = m.post_id
		WHERE `+where).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := d.QueryContext(ctx, `SELECT m.id, m.post_id, m.kind, m.file_path, m.thumb_path, m.width, m.height, m.size,
			m.duration, m.taken_at, m.created_at, p.author_id
		FROM media m JOIN posts p ON p.id = m.post_id
		WHERE `+where+`
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

// AlbumOwnership 媒体当前归属（活跃相册口径，供时间轴/详情输出 album_id/album_name）；
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
		WHERE a.deleted_at IS NULL AND i.media_id IN (`+ph+`) ORDER BY i.added_at ASC, i.rowid ASC`, args...)
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

// GroupCandidate 照片分组候选（find_media group_by）：媒体经所属动态过滤未删，
// 日期键取 taken_at 回退 created_at（SortTime 同口径）。
type GroupCandidate struct {
	MediaID  int64
	Kind     string
	Date     string // YYYY-MM-DD
	Location string
}

// ListMediaGroupCandidates 按日期区间（YYYY-MM-DD 含头，可空）、地点名模糊（可空）与
// 媒体类型（可空）列分组候选。只取**未归档**媒体（与时间轴口径一致）：已归属手动相册的
// 媒体不得再被推荐入册——一文件一相册（UNIQUE(media_id)），推荐它们只会撞 409。
func (d *DB) ListMediaGroupCandidates(ctx context.Context, from, to, location, mediaType string) ([]GroupCandidate, error) {
	where := ` WHERE m.deleted_at IS NULL AND p.deleted_at IS NULL AND ` + notInAlbum("m")
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
	if mediaType != "" {
		where += ` AND m.kind = ?`
		args = append(args, mediaType)
	}
	rows, err := d.QueryContext(ctx, `SELECT m.id, m.kind,
			COALESCE(NULLIF(strftime('%Y-%m-%d', m.taken_at), ''), strftime('%Y-%m-%d', m.created_at, 'localtime')),
			COALESCE(p.location_name,'')
		FROM media m JOIN posts p ON p.id = m.post_id`+where+` ORDER BY 3 DESC, m.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []GroupCandidate
	for rows.Next() {
		var g GroupCandidate
		if err := rows.Scan(&g.MediaID, &g.Kind, &g.Date, &g.Location); err != nil {
			return nil, err
		}
		list = append(list, g)
	}
	return list, rows.Err()
}

// MediaFilter 媒体查询过滤（find_media 用）；全部下推 SQL，禁「取 N 条再 Go 侧过滤」。
type MediaFilter struct {
	Unfiled   bool   // true = 仅未归档（时间轴口径，notInManualAlbum）
	AlbumID   int64  // 非零 = 指定相册内的媒体（find_media(album_id)，AI模式设计「工具契约」）
	From, To  string // YYYY-MM-DD 本地日期，基于 taken_at 回退 created_at
	Place     string // 所属动态地点名模糊
	MediaType string // photo / video / audio
}

// mediaFilterWhere find_media 过滤条件共用骨架（列表与 COUNT 下推同一条 WHERE）。
// AlbumID 非零 = 指定相册内媒体（已归档也能查，补上 propose_album_members 的 remove 路径）。
func mediaFilterWhere(f MediaFilter) (string, []any, error) {
	where := ` WHERE m.deleted_at IS NULL AND p.deleted_at IS NULL`
	if f.AlbumID > 0 {
		where += fmt.Sprintf(` AND EXISTS (SELECT 1 FROM album_items i WHERE i.album_id = %d AND i.media_id = m.id)`, f.AlbumID)
	} else if f.Unfiled {
		where += ` AND ` + notInAlbum("m")
	}
	args := []any{}
	if f.From != "" {
		where += ` AND COALESCE(NULLIF(strftime('%Y-%m-%d', m.taken_at), ''), strftime('%Y-%m-%d', m.created_at, 'localtime')) >= ?`
		args = append(args, f.From)
	}
	if f.To != "" {
		where += ` AND COALESCE(NULLIF(strftime('%Y-%m-%d', m.taken_at), ''), strftime('%Y-%m-%d', m.created_at, 'localtime')) <= ?`
		args = append(args, f.To)
	}
	if f.Place != "" {
		where += ` AND p.location_name LIKE ?`
		args = append(args, "%"+f.Place+"%")
	}
	if f.MediaType != "" {
		where += ` AND m.kind = ?`
		args = append(args, f.MediaType)
	}
	return where, args, nil
}

// CountMediaFiltered 与 ListMediaFiltered 同 WHERE 的命中总数（find_media 真实 total）。
func (d *DB) CountMediaFiltered(ctx context.Context, f MediaFilter) (int, error) {
	where, args, err := mediaFilterWhere(f)
	if err != nil {
		return 0, err
	}
	var n int
	err = d.QueryRowContext(ctx, `SELECT COUNT(*) FROM media m JOIN posts p ON p.id = m.post_id`+where, args...).Scan(&n)
	return n, err
}

// ListMediaFiltered 按过滤条件查媒体本体（时间轴视图），按拍摄/创建时间倒序；offset 翻页。
func (d *DB) ListMediaFiltered(ctx context.Context, f MediaFilter, limit, offset int) ([]*MediaRow, error) {
	where, args, err := mediaFilterWhere(f)
	if err != nil {
		return nil, err
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := d.QueryContext(ctx, `SELECT m.id, m.post_id, m.kind, m.file_path, m.thumb_path, m.width, m.height, m.size,
			m.duration, m.taken_at, m.created_at, p.author_id
		FROM media m JOIN posts p ON p.id = m.post_id`+where+`
		ORDER BY COALESCE(NULLIF(m.taken_at,''), m.created_at) DESC, m.id DESC LIMIT ? OFFSET ?`,
		append(args, limit, offset)...)
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
