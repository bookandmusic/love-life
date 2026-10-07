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
	ID           int64
	Username     string
	PasswordHash string
	Role         string
	Nickname     string
	Birthday     sql.NullString
	AvatarPath   sql.NullString // 独立头像通道：相对数据目录的文件路径；空则无头像
	AIStyle      string
}

func (d *DB) CreateUser(ctx context.Context, u *User) (int64, error) {
	res, err := d.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, role, nickname) VALUES (?, ?, ?, ?)`,
		u.Username, u.PasswordHash, u.Role, u.Nickname)
	if err != nil {
		if isUniqueConstraint(err) { // users.username UNIQUE
			return 0, ErrDuplicateUsername
		}
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	row := d.QueryRowContext(ctx, `SELECT id, username, password_hash, role, nickname, birthday, avatar_path, ai_style
		FROM users WHERE username = ?`, username)
	var u User
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.Nickname, &u.Birthday, &u.AvatarPath, &u.AIStyle)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (d *DB) GetUserByID(ctx context.Context, id int64) (*User, error) {
	row := d.QueryRowContext(ctx, `SELECT id, username, password_hash, role, nickname, birthday, avatar_path, ai_style
		FROM users WHERE id = ?`, id)
	var u User
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.Nickname, &u.Birthday, &u.AvatarPath, &u.AIStyle)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (d *DB) ListUsers(ctx context.Context) ([]*User, error) {
	rows, err := d.QueryContext(ctx, `SELECT id, username, password_hash, role, nickname, birthday, avatar_path, ai_style
		FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []*User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.Nickname, &u.Birthday, &u.AvatarPath, &u.AIStyle); err != nil {
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
// F7——任何一方动作都通知另一方，admin 不在情侣里）。
func (d *DB) ListPartnerIDs(ctx context.Context, excludeUserID int64) ([]int64, error) {
	return d.listIDs(ctx, `SELECT id FROM users WHERE role != 'admin' AND id != ? ORDER BY id`, excludeUserID)
}

// ListCoupleIDs 情侣双方（全部非 admin 用户）：用于**没有操作者**的事件（定时任务发的纪念日提醒），
// 那类事件不适用「操作者本人不收」，双方都该收到；admin 不在情侣里且不走 APP。
func (d *DB) ListCoupleIDs(ctx context.Context) ([]int64, error) {
	return d.listIDs(ctx, `SELECT id FROM users WHERE role != 'admin' ORDER BY id`)
}

// listIDs 收件人 id 读取的单一点（上面两个查询只差 WHERE 与参数）。
func (d *DB) listIDs(ctx context.Context, query string, args ...any) ([]int64, error) {
	rows, err := d.QueryContext(ctx, query, args...)
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

// CreateCouple 情侣账户原子创建：girl/boy 两用户同事务落库，
// 任一失败整体回滚；用户名 UNIQUE 冲突返回 ErrDuplicateUsername（校验顺序由 API 层按 mock 契约先行）。
func (d *DB) CreateCouple(ctx context.Context, girl, boy *User) (girlID, boyID int64, err error) {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	insert := func(u *User) (int64, error) {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO users (username, password_hash, role, nickname) VALUES (?, ?, ?, ?)`,
			u.Username, u.PasswordHash, u.Role, u.Nickname)
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	}
	if girlID, err = insert(girl); err != nil {
		if isUniqueConstraint(err) {
			return 0, 0, ErrDuplicateUsername
		}
		return 0, 0, err
	}
	if boyID, err = insert(boy); err != nil {
		if isUniqueConstraint(err) {
			return 0, 0, ErrDuplicateUsername
		}
		return 0, 0, err
	}
	return girlID, boyID, tx.Commit()
}

// isUniqueConstraint modernc *sqlite.Error 扩展码 2067 = SQLITE_CONSTRAINT_UNIQUE。
func isUniqueConstraint(err error) bool {
	var se *sqlite.Error
	return errors.As(err, &se) && se.Code() == sqliteUniqueConstraint
}

// UpdateUserNickname 仅昵称更新（用户名/角色为登录身份与数据归属，不可修改）。
func (d *DB) UpdateUserNickname(ctx context.Context, id int64, nickname string) error {
	res, err := d.ExecContext(ctx, `UPDATE users SET nickname = ? WHERE id = ?`, nickname, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateUserAvatar 设置用户头像路径（独立通道；空串 = 清除头像）。
func (d *DB) UpdateUserAvatar(ctx context.Context, id int64, avatarPath string) error {
	res, err := d.ExecContext(ctx, `UPDATE users SET avatar_path = NULLIF(?, '') WHERE id = ?`, avatarPath, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateUserBirthday 设置用户生日（空串 = 清除生日）。仅后台维护，不下发 APP。
func (d *DB) UpdateUserBirthday(ctx context.Context, id int64, birthday string) error {
	res, err := d.ExecContext(ctx, `UPDATE users SET birthday = NULLIF(?, '') WHERE id = ?`, birthday, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
