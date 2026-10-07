package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
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

// DaysTogether 在一起天数（首日算第 1 天）；未设开始日或格式非法返回 ok=false
// （调用方留空或省略该项，不得编造「0 天」）。纯函数：开始日由调用方从**同一次**空间读取给出，
// 避免同一次响应里「开始日」与「天数」来自两次读。口径单一来源——AI 提示词事实、欢迎语、
// 统计概览卡、空间信息接口共用。
// 注：开始日在未来属文档未定义的第三态（PRODUCT 只写「未设置时不显示天数」），
// 当前不特殊处置，待需求侧定夺（拒绝写入 / 或明确写成不给数字）。
func DaysTogether(loveStartDate string, now time.Time) (int, bool) {
	if loveStartDate == "" {
		return 0, false
	}
	start, err := time.ParseInLocation("2006-01-02", loveStartDate, time.Local)
	if err != nil {
		return 0, false
	}
	return int(now.Sub(start).Hours()/24) + 1, true
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
