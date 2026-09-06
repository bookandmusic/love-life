package store

import (
	"context"
	"database/sql"
	"errors"

	"modernc.org/sqlite"
)

var (
	ErrNotFound          = errors.New("not found")
	ErrDuplicateUsername = errors.New("username already exists")
)

// sqliteUniqueConstraint SQLite 扩展错误码 SQLITE_CONSTRAINT_UNIQUE；modernc 的 Error.Code() 返回扩展码而非主码 19。
const sqliteUniqueConstraint = 2067

type User struct {
	ID                int64
	Username          string
	PasswordHash      string
	Role              string
	Nickname          string
	Birthday          sql.NullString
	AIStyle           string
	ChatRetentionDays int
}

func (d *DB) CreateUser(ctx context.Context, u *User) (int64, error) {
	res, err := d.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, role, nickname) VALUES (?, ?, ?, ?)`,
		u.Username, u.PasswordHash, u.Role, u.Nickname)
	if err != nil {
		var se *sqlite.Error
		if errors.As(err, &se) && se.Code() == sqliteUniqueConstraint { // users.username UNIQUE
			return 0, ErrDuplicateUsername
		}
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	row := d.QueryRowContext(ctx, `SELECT id, username, password_hash, role, nickname, birthday, ai_style, chat_retention_days
		FROM users WHERE username = ?`, username)
	var u User
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.Nickname, &u.Birthday, &u.AIStyle, &u.ChatRetentionDays)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (d *DB) GetUserByID(ctx context.Context, id int64) (*User, error) {
	row := d.QueryRowContext(ctx, `SELECT id, username, password_hash, role, nickname, birthday, ai_style, chat_retention_days
		FROM users WHERE id = ?`, id)
	var u User
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.Nickname, &u.Birthday, &u.AIStyle, &u.ChatRetentionDays)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (d *DB) ListUsers(ctx context.Context) ([]*User, error) {
	rows, err := d.QueryContext(ctx, `SELECT id, username, password_hash, role, nickname, birthday, ai_style, chat_retention_days
		FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []*User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.Nickname, &u.Birthday, &u.AIStyle, &u.ChatRetentionDays); err != nil {
			return nil, err
		}
		users = append(users, &u)
	}
	return users, rows.Err()
}

func (d *DB) UpdateUserPassword(ctx context.Context, id int64, passwordHash string) error {
	res, err := d.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListPartnerIDs 情侣「另一方」收件人：非 admin 且非操作者（对齐 mock partnerIds，
// REQUIREMENTS §4.8/F7——任何一方动作都通知另一方，admin 不在情侣里）。
func (d *DB) ListPartnerIDs(ctx context.Context, excludeUserID int64) ([]int64, error) {
	rows, err := d.QueryContext(ctx, `SELECT id FROM users WHERE role != 'admin' AND id != ? ORDER BY id`, excludeUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (d *DB) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin'`).Scan(&n)
	return n, err
}
