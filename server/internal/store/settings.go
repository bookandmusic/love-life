package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
)

// ErrStyleNameTaken 风格名称重复（output_styles.name UNIQUE）。
var ErrStyleNameTaken = errors.New("风格名称已存在")

// ---- app_settings（全局单值配置）----

// GetSetting 读单值；不存在 → ErrNotFound。
func (d *DB) GetSetting(ctx context.Context, key string) (string, error) {
	var v string
	err := d.QueryRowContext(ctx, `SELECT value FROM app_settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return v, err
}

// SetSetting 写单值（upsert）。
func (d *DB) SetSetting(ctx context.Context, key, value string) error {
	_, err := d.ExecContext(ctx,
		`INSERT INTO app_settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value)
	return err
}

// ---- 输出风格库（内置固化 + 后台自定义；default_style_id 存 app_settings）----

type Style struct {
	ID      int64
	Name    string
	Prompt  string
	Builtin bool
}

const styleColumns = `id, name, prompt, builtin`

func scanStyle(row interface{ Scan(...any) error }) (*Style, error) {
	var s Style
	var builtin int
	if err := row.Scan(&s.ID, &s.Name, &s.Prompt, &builtin); err != nil {
		return nil, err
	}
	s.Builtin = builtin == 1
	return &s, nil
}

// DefaultStyleID 全局默认风格 id；未配置 → 0。
func (d *DB) DefaultStyleID(ctx context.Context) (int64, error) {
	v, err := d.GetSetting(ctx, "default_style_id")
	if errors.Is(err, ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	id, _ := strconv.ParseInt(v, 10, 64)
	return id, nil
}

// SetDefaultStyle 切换全局默认风格；风格不存在 → ErrNotFound。
func (d *DB) SetDefaultStyle(ctx context.Context, id int64) error {
	if _, err := d.GetStyle(ctx, id); err != nil {
		return err
	}
	return d.SetSetting(ctx, "default_style_id", strconv.FormatInt(id, 10))
}

// ListStyles 全部风格（内置 + 自定义），按 sort_order, id。
func (d *DB) ListStyles(ctx context.Context) ([]*Style, error) {
	rows, err := d.QueryContext(ctx, `SELECT `+styleColumns+` FROM output_styles ORDER BY sort_order, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*Style
	for rows.Next() {
		s, err := scanStyle(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, s)
	}
	return list, rows.Err()
}

// GetStyle 单个风格；不存在 → ErrNotFound。
func (d *DB) GetStyle(ctx context.Context, id int64) (*Style, error) {
	s, err := scanStyle(d.QueryRowContext(ctx, `SELECT `+styleColumns+` FROM output_styles WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return s, err
}

// CreateStyle 新建自定义风格；名称重复 → ErrStyleNameTaken。
func (d *DB) CreateStyle(ctx context.Context, name, prompt string) (int64, error) {
	res, err := d.ExecContext(ctx,
		`INSERT INTO output_styles (name, prompt, builtin) VALUES (?, ?, 0)`, name, prompt)
	if isUniqueConstraint(err) {
		return 0, ErrStyleNameTaken
	}
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateStyle 更新自定义风格（字段指针 nil = 不更新）；builtin 风格由调用方拒绝，此处不改 builtin。
func (d *DB) UpdateStyle(ctx context.Context, id int64, name, prompt *string) error {
	var n int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM output_styles WHERE id = ? AND builtin = 0`, id).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		// 区分：不存在 vs builtin
		_, err := d.GetStyle(ctx, id)
		if err != nil {
			return err // ErrNotFound
		}
		return ErrBuiltinStyle
	}
	if name != nil {
		if _, err := d.ExecContext(ctx, `UPDATE output_styles SET name = ? WHERE id = ?`, *name, id); err != nil {
			if isUniqueConstraint(err) {
				return ErrStyleNameTaken
			}
			return err
		}
	}
	if prompt != nil {
		if _, err := d.ExecContext(ctx, `UPDATE output_styles SET prompt = ? WHERE id = ?`, *prompt, id); err != nil {
			return err
		}
	}
	return nil
}

// ErrBuiltinStyle 内置风格不可修改/删除。
var ErrBuiltinStyle = errors.New("内置风格不可修改")

// DeleteStyle 删除自定义风格；builtin → ErrBuiltinStyle；不存在 → ErrNotFound。
func (d *DB) DeleteStyle(ctx context.Context, id int64) error {
	var builtin int
	err := d.QueryRowContext(ctx, `SELECT builtin FROM output_styles WHERE id = ?`, id).Scan(&builtin)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if builtin == 1 {
		return ErrBuiltinStyle
	}
	_, err = d.ExecContext(ctx, `DELETE FROM output_styles WHERE id = ?`, id)
	return err
}
