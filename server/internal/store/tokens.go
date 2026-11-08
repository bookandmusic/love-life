package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type RefreshToken struct {
	ID        int64
	UserID    int64
	TokenHash string
	ExpiresAt time.Time
}

func (d *DB) SaveRefreshToken(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error {
	_, err := d.ExecContext(ctx,
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES (?, ?, ?)`,
		userID, tokenHash, FormatDBTime(expiresAt))
	return err
}

func (d *DB) GetRefreshToken(ctx context.Context, tokenHash string) (*RefreshToken, error) {
	row := d.QueryRowContext(ctx, `SELECT id, user_id, token_hash, expires_at FROM refresh_tokens WHERE token_hash = ?`, tokenHash)
	var t RefreshToken
	var expires string
	err := row.Scan(&t.ID, &t.UserID, &t.TokenHash, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	t.ExpiresAt, err = ParseDBTime(expires)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (d *DB) DeleteRefreshToken(ctx context.Context, tokenHash string) error {
	_, err := d.ExecContext(ctx, `DELETE FROM refresh_tokens WHERE token_hash = ?`, tokenHash)
	return err
}

// DeleteRefreshTokensByUser 吊销某用户的全部 refresh token，返回删除行数。
// 用于改密后让该用户的所有已登录设备失效——Access Token 有 1 小时寿命且服务端不查库，
// 故「立即掉线」只能靠删 refresh：已签发的 Access Token 最多再用 1 小时，之后无法续期。
func (d *DB) DeleteRefreshTokensByUser(ctx context.Context, userID int64) (int64, error) {
	res, err := d.ExecContext(ctx, `DELETE FROM refresh_tokens WHERE user_id = ?`, userID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (d *DB) DeleteExpiredRefreshTokens(ctx context.Context) error {
	_, err := d.ExecContext(ctx, `DELETE FROM refresh_tokens WHERE datetime(expires_at) < ?`, FormatDBTime(time.Now()))
	return err
}
