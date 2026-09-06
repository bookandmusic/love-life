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
		userID, tokenHash, expiresAt.UTC().Format(time.RFC3339))
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
	t.ExpiresAt, err = time.Parse(time.RFC3339, expires)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (d *DB) DeleteRefreshToken(ctx context.Context, tokenHash string) error {
	_, err := d.ExecContext(ctx, `DELETE FROM refresh_tokens WHERE token_hash = ?`, tokenHash)
	return err
}

func (d *DB) DeleteExpiredRefreshTokens(ctx context.Context) error {
	_, err := d.ExecContext(ctx, `DELETE FROM refresh_tokens WHERE expires_at < ?`, time.Now().UTC().Format(time.RFC3339))
	return err
}
