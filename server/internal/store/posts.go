package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type Post struct {
	ID           int64
	AuthorID     int64
	Title        sql.NullString
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

// SortTime 时间轴排序键：taken_at 优先，回退 created_at（A1c 口径，mock 一致）。
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

// CreatePostWithMedia 单事务创建动态与媒体行（媒体文件已由管线转正，失败由调用方回收）。
func (d *DB) CreatePostWithMedia(ctx context.Context, p *Post, medias []MediaRow) (int64, error) {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx,
		`INSERT INTO posts (author_id, title, content, mood, location_name, point_id) VALUES (?, ?, ?, ?, ?, ?)`,
		p.AuthorID, p.Title, p.Content, p.Mood, p.LocationName, p.PointID)
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
	return postID, nil
}

const postColumns = `p.id, p.author_id, p.title, p.content, p.mood, p.location_name, p.created_at, p.deleted_at, COALESCE(u.nickname, u.username)`

func scanPost(row interface{ Scan(...any) error }) (*Post, error) {
	var p Post
	err := row.Scan(&p.ID, &p.AuthorID, &p.Title, &p.Content, &p.Mood, &p.LocationName, &p.CreatedAt, &p.DeletedAt, &p.AuthorNickname)
	return &p, err
}

// ListPosts 分页倒序；deleted=show 时含已删（后台回收站用，默认过滤）。
func (d *DB) ListPosts(ctx context.Context, page, pageSize int) ([]*Post, int, error) {
	var total int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM posts WHERE deleted_at IS NULL`).Scan(&total); err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	rows, err := d.QueryContext(ctx, `
		SELECT `+postColumns+` FROM posts p JOIN users u ON u.id = p.author_id
		WHERE p.deleted_at IS NULL ORDER BY p.id DESC LIMIT ? OFFSET ?`, pageSize, offset)
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

func (d *DB) SoftDeletePost(ctx context.Context, id int64) error {
	res, err := d.ExecContext(ctx,
		`UPDATE posts SET deleted_at = ? WHERE id = ? AND deleted_at IS NULL`,
		time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
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

// GetMediaByIDs 批量取媒体（reurl，D1）：仅未删除的命中项，入参无效 id 自然跳过。
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
