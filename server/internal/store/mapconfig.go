package store

import (
	"context"
	"database/sql"
	"errors"
)

// MapSource 瓦片源（DESIGN §4.7：多卡片管理，类型限 public/tianditu，至多一个使用中；
// 密钥加密存储、永不下发 APP）。
type MapSource struct {
	ID            int64
	Type          string // public | tianditu
	URLTemplate   string // public 源模板，含 {z}/{x}/{y} 占位
	TiandituKey   string // AES-GCM 加密存储
	TiandituLayer string // 天地图底图类型：vec_w / img_w / ter_w
	Enabled       bool   // 使用中
	CreatedAt     string
}

const mapSourceColumns = `id, type, url_template, tianditu_key, tianditu_layer, enabled, created_at`

func scanMapSource(row interface{ Scan(...any) error }) (*MapSource, error) {
	var s MapSource
	var enabled int
	if err := row.Scan(&s.ID, &s.Type, &s.URLTemplate, &s.TiandituKey, &s.TiandituLayer, &enabled, &s.CreatedAt); err != nil {
		return nil, err
	}
	s.Enabled = enabled == 1
	return &s, nil
}

// ListSources 全部瓦片源（使用中的在前，其余按创建时间倒序）。
func (d *DB) ListSources(ctx context.Context) ([]*MapSource, error) {
	rows, err := d.QueryContext(ctx,
		`SELECT `+mapSourceColumns+` FROM map_sources ORDER BY enabled DESC, created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*MapSource
	for rows.Next() {
		s, err := scanMapSource(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, s)
	}
	return list, rows.Err()
}

// GetSource 单个瓦片源。
func (d *DB) GetSource(ctx context.Context, id int64) (*MapSource, error) {
	s, err := scanMapSource(d.QueryRowContext(ctx,
		`SELECT `+mapSourceColumns+` FROM map_sources WHERE id = ?`, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return s, nil
}

// GetEnabledSource 当前使用中的源；无启用返回 ErrNotFound。
func (d *DB) GetEnabledSource(ctx context.Context) (*MapSource, error) {
	s, err := scanMapSource(d.QueryRowContext(ctx,
		`SELECT `+mapSourceColumns+` FROM map_sources WHERE enabled = 1`))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return s, nil
}

// CreateSource 新建瓦片源（默认不启用）。
func (d *DB) CreateSource(ctx context.Context, s *MapSource) (int64, error) {
	if s.TiandituLayer == "" {
		s.TiandituLayer = "vec_w"
	}
	res, err := d.ExecContext(ctx,
		`INSERT INTO map_sources (type, url_template, tianditu_key, tianditu_layer) VALUES (?, ?, ?, ?)`,
		s.Type, s.URLTemplate, s.TiandituKey, s.TiandituLayer)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// EnableSource 启用指定源并停用其余（事务保证全局至多一个使用中）；不存在返回 ErrNotFound。
func (d *DB) EnableSource(ctx context.Context, id int64) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE map_sources SET enabled = 0 WHERE enabled = 1`); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE map_sources SET enabled = 1 WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// DeleteSource 删除瓦片源；不存在返回 ErrNotFound。
func (d *DB) DeleteSource(ctx context.Context, id int64) error {
	res, err := d.ExecContext(ctx, `DELETE FROM map_sources WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
