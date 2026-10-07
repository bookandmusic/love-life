package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// PostInteraction 一条动态的互动聚合（含当前查看者视角的 liked/favorited）。
type PostInteraction struct {
	LikeCount     int
	Liked         bool
	FavoriteCount int
	Favorited     bool
	CommentCount  int
}

func (d *DB) ToggleLike(ctx context.Context, postID, userID int64) (liked bool, err error) {
	var exists int
	if err = d.QueryRowContext(ctx, `SELECT COUNT(*) FROM likes WHERE post_id = ? AND user_id = ?`, postID, userID).Scan(&exists); err != nil {
		return false, err
	}
	if exists > 0 {
		_, err = d.ExecContext(ctx, `DELETE FROM likes WHERE post_id = ? AND user_id = ?`, postID, userID)
		return false, err
	}
	_, err = d.ExecContext(ctx, `INSERT INTO likes (post_id, user_id) VALUES (?, ?)`, postID, userID)
	return true, err
}

func (d *DB) ToggleFavorite(ctx context.Context, postID, userID int64) (favorited bool, err error) {
	var exists int
	if err = d.QueryRowContext(ctx, `SELECT COUNT(*) FROM favorites WHERE post_id = ? AND user_id = ?`, postID, userID).Scan(&exists); err != nil {
		return false, err
	}
	if exists > 0 {
		_, err = d.ExecContext(ctx, `DELETE FROM favorites WHERE post_id = ? AND user_id = ?`, postID, userID)
		return false, err
	}
	_, err = d.ExecContext(ctx, `INSERT INTO favorites (post_id, user_id) VALUES (?, ?)`, postID, userID)
	return true, err
}

// SetLike / SetFavorite 幂等「设为」语义（propose_reaction 用）：
// 与目标状态一致时不动（changed=false），不一致时置为目标状态。
// toggle 语义下模型不知道当前状态，「点个赞」有约 50% 概率变成取消——设为语义从接口形状上消除。
func (d *DB) SetLike(ctx context.Context, postID, userID int64, want bool) (changed bool, err error) {
	var exists int
	if err = d.QueryRowContext(ctx, `SELECT COUNT(*) FROM likes WHERE post_id = ? AND user_id = ?`, postID, userID).Scan(&exists); err != nil {
		return false, err
	}
	has := exists > 0
	if has == want {
		return false, nil
	}
	if want {
		_, err = d.ExecContext(ctx, `INSERT INTO likes (post_id, user_id) VALUES (?, ?)`, postID, userID)
	} else {
		_, err = d.ExecContext(ctx, `DELETE FROM likes WHERE post_id = ? AND user_id = ?`, postID, userID)
	}
	return err == nil, err
}

func (d *DB) SetFavorite(ctx context.Context, postID, userID int64, want bool) (changed bool, err error) {
	var exists int
	if err = d.QueryRowContext(ctx, `SELECT COUNT(*) FROM favorites WHERE post_id = ? AND user_id = ?`, postID, userID).Scan(&exists); err != nil {
		return false, err
	}
	has := exists > 0
	if has == want {
		return false, nil
	}
	if want {
		_, err = d.ExecContext(ctx, `INSERT INTO favorites (post_id, user_id) VALUES (?, ?)`, postID, userID)
	} else {
		_, err = d.ExecContext(ctx, `DELETE FROM favorites WHERE post_id = ? AND user_id = ?`, postID, userID)
	}
	return err == nil, err
}

// GetInteractions 批量取互动聚合（动态列表/详情共用）。
func (d *DB) GetInteractions(ctx context.Context, postIDs []int64, viewerID int64) (map[int64]*PostInteraction, error) {
	out := map[int64]*PostInteraction{}
	if len(postIDs) == 0 {
		return out, nil
	}
	for _, id := range postIDs {
		out[id] = &PostInteraction{}
	}
	ph := placeholders(len(postIDs))
	args := int64Args(postIDs)

	var err error
	if err = d.queryCounts(ctx, out, `SELECT post_id, COUNT(*) FROM likes WHERE post_id IN (`+ph+`) GROUP BY post_id`, args); err != nil {
		return nil, err
	}
	if err = d.queryCounts(ctx, out, `SELECT post_id, COUNT(*) FROM favorites WHERE post_id IN (`+ph+`) GROUP BY post_id`, args); err != nil {
		return nil, err
	}
	if err = d.queryCounts(ctx, out, `SELECT post_id, COUNT(*) FROM comments WHERE post_id IN (`+ph+`) AND deleted_at IS NULL GROUP BY post_id`, args); err != nil {
		return nil, err
	}

	viewerFilterArgs := make([]any, 0, len(postIDs)+1)
	viewerFilterArgs = append(viewerFilterArgs, viewerID)
	for _, id := range postIDs {
		viewerFilterArgs = append(viewerFilterArgs, id)
	}

	likedRows, err := d.QueryContext(ctx, `SELECT post_id FROM likes WHERE user_id = ? AND post_id IN (`+ph+`)`,
		viewerFilterArgs...)
	if err != nil {
		return nil, err
	}
	defer likedRows.Close()
	for likedRows.Next() {
		var pid int64
		if err := likedRows.Scan(&pid); err != nil {
			return nil, err
		}
		out[pid].Liked = true
	}

	favRows, err := d.QueryContext(ctx, `SELECT post_id FROM favorites WHERE user_id = ? AND post_id IN (`+ph+`)`,
		viewerFilterArgs...)
	if err != nil {
		return nil, err
	}
	defer favRows.Close()
	for favRows.Next() {
		var pid int64
		if err := favRows.Scan(&pid); err != nil {
			return nil, err
		}
		out[pid].Favorited = true
	}
	return out, nil
}

func (d *DB) queryCounts(ctx context.Context, out map[int64]*PostInteraction, query string, args []any) error {
	rows, err := d.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var pid, n int64
		if err := rows.Scan(&pid, &n); err != nil {
			return err
		}
		switch {
		case strings.Contains(query, "FROM likes"):
			out[pid].LikeCount = int(n)
		case strings.Contains(query, "FROM favorites"):
			out[pid].FavoriteCount = int(n)
		default:
			out[pid].CommentCount = int(n)
		}
	}
	return rows.Err()
}

func placeholders(n int) string {
	p := ""
	for i := 0; i < n; i++ {
		if i > 0 {
			p += ","
		}
		p += "?"
	}
	return p
}

func int64Args(ids []int64) []any {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return args
}

// ---- 评论 ----

type Comment struct {
	ID             int64
	PostID         int64
	AuthorID       int64
	Content        string
	CreatedAt      string
	AuthorNickname sql.NullString
}

func (d *DB) CreateComment(ctx context.Context, postID, authorID int64, content string) (int64, error) {
	res, err := d.ExecContext(ctx,
		`INSERT INTO comments (post_id, author_id, content) VALUES (?, ?, ?)`, postID, authorID, content)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) ListComments(ctx context.Context, postID int64, limit, offset int) ([]*Comment, error) {
	rows, err := d.QueryContext(ctx, `
		SELECT c.id, c.post_id, c.author_id, c.content, c.created_at, COALESCE(u.nickname, u.username)
		FROM comments c JOIN users u ON u.id = c.author_id
		WHERE c.post_id = ? AND c.deleted_at IS NULL
		ORDER BY c.id DESC LIMIT ? OFFSET ?`, postID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*Comment
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.PostID, &c.AuthorID, &c.Content, &c.CreatedAt, &c.AuthorNickname); err != nil {
			return nil, err
		}
		list = append(list, &c)
	}
	return list, rows.Err()
}

// CountComments 某动态未删评论总数（list_comments 分页 total 用）。
func (d *DB) CountComments(ctx context.Context, postID int64) (int, error) {
	var n int
	err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM comments WHERE post_id = ? AND deleted_at IS NULL`, postID).Scan(&n)
	return n, err
}

func (d *DB) GetComment(ctx context.Context, id int64) (*Comment, error) {
	row := d.QueryRowContext(ctx, `
		SELECT c.id, c.post_id, c.author_id, c.content, c.created_at, COALESCE(u.nickname, u.username)
		FROM comments c JOIN users u ON u.id = c.author_id
		WHERE c.id = ? AND c.deleted_at IS NULL`, id)
	var c Comment
	err := row.Scan(&c.ID, &c.PostID, &c.AuthorID, &c.Content, &c.CreatedAt, &c.AuthorNickname)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (d *DB) SoftDeleteComment(ctx context.Context, id int64, actorType string, actorID int64) error {
	return softDeleteKind(ctx, d, "comment", id, actorType, actorID)
}

// ListFavoritePostsPaged 我收藏的动态（list_favorites）：收藏时间倒序 + 总数，
// 分页口径与媒体时间轴一致（size 由调用方夹紧）。
func (d *DB) ListFavoritePostsPaged(ctx context.Context, viewerID int64, page, size int) ([]*Post, int, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	var total int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM favorites f JOIN posts p ON p.id = f.post_id
		WHERE f.user_id = ? AND p.deleted_at IS NULL`, viewerID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := d.QueryContext(ctx, `SELECT `+postColumns+`
		FROM favorites f
		JOIN posts p ON p.id = f.post_id
		JOIN users u ON u.id = p.author_id
		WHERE f.user_id = ? AND p.deleted_at IS NULL
		ORDER BY f.created_at DESC, f.post_id DESC LIMIT ? OFFSET ?`, viewerID, size, (page-1)*size)
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
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return list, total, nil
}
