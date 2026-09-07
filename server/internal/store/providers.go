package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// 模型供应商（DESIGN §12.1）：api_type 决定协议适配（L2），sort_order = 故障切换链顺序（L3），
// default_model 指向 provider_models.model_id 字符串（供应商内唯一）；temperature/max_tokens
// 挂模型级，NULL = 调用不传（L4 消费）。

type ProviderModel struct {
	ID          int64
	ProviderID  int64
	ModelID     string
	Temperature sql.NullFloat64
	MaxTokens   sql.NullInt64
}

type ModelProvider struct {
	ID           int64
	APIType      string
	Name         string
	BaseURL      string
	APIKeyEnc    string
	DefaultModel string
	SortOrder    int
	Models       []ProviderModel
}

const providerColumns = `id, api_type, name, base_url, api_key_enc, default_model, sort_order`

func scanProvider(row interface{ Scan(...any) error }) (*ModelProvider, error) {
	var p ModelProvider
	if err := row.Scan(&p.ID, &p.APIType, &p.Name, &p.BaseURL, &p.APIKeyEnc, &p.DefaultModel, &p.SortOrder); err != nil {
		return nil, err
	}
	return &p, nil
}

// attachModels 一次查询取全部模型条目并按供应商分组（供应商个数为个位数，无需分页）。
func (d *DB) attachModels(ctx context.Context, providers []*ModelProvider) error {
	if len(providers) == 0 {
		return nil
	}
	marks := make([]string, len(providers))
	args := make([]any, len(providers))
	for i, p := range providers {
		marks[i] = "?"
		args[i] = p.ID
	}
	rows, err := d.QueryContext(ctx,
		`SELECT id, provider_id, model_id, temperature, max_tokens
			FROM provider_models WHERE provider_id IN (`+strings.Join(marks, ",")+`) ORDER BY provider_id, id`,
		args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	byProvider := map[int64][]ProviderModel{}
	for rows.Next() {
		var m ProviderModel
		if err := rows.Scan(&m.ID, &m.ProviderID, &m.ModelID, &m.Temperature, &m.MaxTokens); err != nil {
			return err
		}
		byProvider[m.ProviderID] = append(byProvider[m.ProviderID], m)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, p := range providers {
		p.Models = byProvider[p.ID]
	}
	return nil
}

// ListProviders 全部供应商，按故障切换链序（sort_order, id）。
func (d *DB) ListProviders(ctx context.Context) ([]*ModelProvider, error) {
	rows, err := d.QueryContext(ctx,
		`SELECT `+providerColumns+` FROM model_providers ORDER BY sort_order, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*ModelProvider
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := d.attachModels(ctx, list); err != nil {
		return nil, err
	}
	return list, nil
}

// GetChatProvider 对话用供应商：链头起第一个已配 Key 的供应商（按 sort_order, id；
// 协议适配已全量落地——四协议 adapter 均已实现）。无可用 → ErrNotFound。
func (d *DB) GetChatProvider(ctx context.Context) (*ModelProvider, error) {
	p, err := scanProvider(d.QueryRowContext(ctx,
		`SELECT `+providerColumns+` FROM model_providers
		 WHERE api_key_enc != ''		 ORDER BY sort_order, id LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := d.attachModels(ctx, []*ModelProvider{p}); err != nil {
		return nil, err
	}
	return p, nil
}

// ---- 写路径（AD1 管理面）----

func insertProviderModels(tx *sql.Tx, providerID int64, models []ProviderModel) error {
	for _, m := range models {
		var temp, maxTok any
		if m.Temperature.Valid {
			temp = m.Temperature.Float64
		}
		if m.MaxTokens.Valid {
			maxTok = m.MaxTokens.Int64
		}
		if _, err := tx.Exec(`INSERT INTO provider_models (provider_id, model_id, temperature, max_tokens) VALUES (?, ?, ?, ?)`,
			providerID, m.ModelID, temp, maxTok); err != nil {
			return err
		}
	}
	return nil
}

// CreateProvider 事务写入供应商与模型条目；返回新 ID。
func (d *DB) CreateProvider(ctx context.Context, p *ModelProvider, models []ProviderModel) (int64, error) {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO model_providers (api_type, name, base_url, api_key_enc, default_model, sort_order) VALUES (?, ?, ?, ?, ?, ?)`,
		p.APIType, p.Name, p.BaseURL, p.APIKeyEnc, p.DefaultModel, p.SortOrder)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := insertProviderModels(tx, id, models); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// GetProvider 单个供应商（含模型条目）。
func (d *DB) GetProvider(ctx context.Context, id int64) (*ModelProvider, error) {
	p, err := scanProvider(d.QueryRowContext(ctx,
		`SELECT `+providerColumns+` FROM model_providers WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := d.attachModels(ctx, []*ModelProvider{p}); err != nil {
		return nil, err
	}
	return p, nil
}

// UpdateProvider 事务更新供应商字段（四字段传最终值）并可选整表替换模型条目。
func (d *DB) UpdateProvider(ctx context.Context, id int64, apiType, name, baseURL, defaultModel string, models []ProviderModel, replaceModels bool) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM model_providers WHERE id = ?`, id).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(`UPDATE model_providers SET api_type = ?, name = ?, base_url = ?, default_model = ? WHERE id = ?`, apiType, name, baseURL, defaultModel, id); err != nil {
		return err
	}
	if replaceModels {
		if _, err := tx.Exec(`DELETE FROM provider_models WHERE provider_id = ?`, id); err != nil {
			return err
		}
		if err := insertProviderModels(tx, id, models); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// UpdateProviderKey 单独更新加密后的 Key（只写不读）。
func (d *DB) UpdateProviderKey(ctx context.Context, id int64, keyEnc string) error {
	res, err := d.ExecContext(ctx, `UPDATE model_providers SET api_key_enc = ? WHERE id = ?`, keyEnc, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ReorderProviders 按给定 id 序重写 sort_order（0..n-1）。ids 应已由调用方校验恰好覆盖全部。
func (d *DB) ReorderProviders(ctx context.Context, ids []int64) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, id := range ids {
		if _, err := tx.Exec(`UPDATE model_providers SET sort_order = ? WHERE id = ?`, i, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteProvider 删除供应商（FK 级联清模型条目）。
func (d *DB) DeleteProvider(ctx context.Context, id int64) error {
	res, err := d.ExecContext(ctx, `DELETE FROM model_providers WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
