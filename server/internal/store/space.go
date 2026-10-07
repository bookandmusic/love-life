package store

import (
	"context"
	"database/sql"
	"errors"
)

type Space struct {
	Name          string
	Signature     string
	LoveStartDate sql.NullString
}

// GetSpace 单行表（迁移 008 保证 id=1 存在）。
func (d *DB) GetSpace(ctx context.Context) (*Space, error) {
	var s Space
	err := d.QueryRowContext(ctx, `SELECT name, signature, love_start_date FROM space WHERE id = 1`).
		Scan(&s.Name, &s.Signature, &s.LoveStartDate)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// UpdateSpace 情侣空间信息属配置信息，双方均可改。
func (d *DB) UpdateSpace(ctx context.Context, s *Space) error {
	_, err := d.ExecContext(ctx,
		`UPDATE space SET name = ?, signature = ?, love_start_date = ? WHERE id = 1`,
		s.Name, s.Signature, s.LoveStartDate)
	return err
}

// UpdateUserSelf 用户自助字段：昵称/生日/AI 偏好（资料属配置信息；AI 偏好按用户生效）。
// nil = 不修改（对齐 mock PATCH /me 契约：字段缺省时不得覆盖已有值）。
// UpdateUserSelf APP 侧自助修改：仅 AI 偏好（输出风格）——昵称 / 生日等资料由后台维护
// （PRODUCT「设置」），PATCH /me 不再接收它们。
func (d *DB) UpdateUserSelf(ctx context.Context, id int64, aiStyle *string) error {
	var args []any
	set := "SET id = id"
	if aiStyle != nil {
		set += ", ai_style = ?"
		args = append(args, *aiStyle)
	}
	args = append(args, id)
	res, err := d.ExecContext(ctx, `UPDATE users `+set+` WHERE id = ?`, args...)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
