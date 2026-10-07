package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// 模型供应商：api_type 决定协议适配；用哪个模型、按什么顺序重试由模型条目决定——
// provider_models.kind 决定属于 chat / embedding 哪条链，chain_order 是同类型内的全局链序
// （跨供应商）。temperature/max_tokens 挂模型级，NULL = 调用不传（chat 组装请求时消费）。

type ProviderModel struct {
	ID          int64
	ProviderID  int64
	Kind        string // chat / embedding：决定属于哪条链
	ModelID     string
	Temperature sql.NullFloat64
	MaxTokens   sql.NullInt64
	ChainOrder  int // 同类型内的全局链序（跨供应商，重试即按它依次走）
}

type ModelProvider struct {
	ID        int64
	APIType   string
	Name      string
	BaseURL   string
	APIKeyEnc string
	Models    []ProviderModel
}

const providerColumns = `id, api_type, name, base_url, api_key_enc`

func scanProvider(row interface{ Scan(...any) error }) (*ModelProvider, error) {
	var p ModelProvider
	if err := row.Scan(&p.ID, &p.APIType, &p.Name, &p.BaseURL, &p.APIKeyEnc); err != nil {
		return nil, err
	}
	return &p, nil
}

// ChainEntry 一条模型链上的可调用条目：供应商 + 模型条目（AI模式设计「模型网关与配置中心」）。
type ChainEntry struct {
	Provider *ModelProvider
	Model    ProviderModel
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
		`SELECT id, provider_id, kind, model_id, temperature, max_tokens, chain_order
			FROM provider_models WHERE provider_id IN (`+strings.Join(marks, ",")+`) ORDER BY provider_id, kind, chain_order, id`,
		args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	byProvider := map[int64][]ProviderModel{}
	for rows.Next() {
		var m ProviderModel
		if err := rows.Scan(&m.ID, &m.ProviderID, &m.Kind, &m.ModelID, &m.Temperature, &m.MaxTokens, &m.ChainOrder); err != nil {
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

// listProvidersBy 按可选 WHERE 查询供应商并附上模型条目（共享查询骨架）。
func (d *DB) listProvidersBy(ctx context.Context, where string, args ...any) ([]*ModelProvider, error) {
	rows, err := d.QueryContext(ctx,
		`SELECT `+providerColumns+` FROM model_providers`+where+` ORDER BY id`, args...)
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

// ListProviders 全部供应商（admin 列表用，按创建序）。
func (d *DB) ListProviders(ctx context.Context) ([]*ModelProvider, error) {
	return d.listProvidersBy(ctx, "")
}

// ListModels 按类型取模型链（chain_order, id 为同类型内的全局链序，跨供应商）。
// chat 链只含已配 Key 的供应商；嵌入链仅 openai-chat 供应商（写入侧校验，读取侧再兜一道）。
func (d *DB) ListModels(ctx context.Context, kind string) ([]ChainEntry, error) {
	where := ` WHERE pm.kind = ?`
	if kind == "chat" {
		where += ` AND p.api_key_enc != ''`
	} else if kind == "embedding" {
		where += ` AND p.api_type = 'openai-chat' AND p.api_key_enc != ''`
	}
	rows, err := d.QueryContext(ctx,
		`SELECT pm.id, pm.provider_id, pm.kind, pm.model_id, pm.temperature, pm.max_tokens, pm.chain_order,
			p.id, p.api_type, p.name, p.base_url, p.api_key_enc
		 FROM provider_models pm JOIN model_providers p ON p.id = pm.provider_id`+where+`
		 ORDER BY pm.chain_order, pm.id`, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChainEntry
	for rows.Next() {
		var m ProviderModel
		p := &ModelProvider{}
		if err := rows.Scan(&m.ID, &m.ProviderID, &m.Kind, &m.ModelID, &m.Temperature, &m.MaxTokens, &m.ChainOrder,
			&p.ID, &p.APIType, &p.Name, &p.BaseURL, &p.APIKeyEnc); err != nil {
			return nil, err
		}
		m.ProviderID = p.ID
		out = append(out, ChainEntry{Provider: p, Model: m})
	}
	return out, rows.Err()
}

// GetChatHead chat 链首（首个可用条目），供 /chat/status 判断 AI 可用性。
func (d *DB) GetChatHead(ctx context.Context) (*ChainEntry, error) {
	list, err := d.ListModels(ctx, "chat")
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return &list[0], nil
}

// ---- 写路径（管理面）----

func insertProviderModels(tx *sql.Tx, providerID int64, models []ProviderModel) error {
	// chain_order 尾追：同一 kind 内接在既有条目之后（本调用内按传入顺序递增）
	nextOrder := map[string]int{}
	rows, err := tx.Query(`SELECT kind, COALESCE(MAX(chain_order), -1) + 1 FROM provider_models GROUP BY kind`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			rows.Close()
			return err
		}
		nextOrder[k] = n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, m := range models {
		var temp, maxTok any
		if m.Temperature.Valid {
			temp = m.Temperature.Float64
		}
		if m.MaxTokens.Valid {
			maxTok = m.MaxTokens.Int64
		}
		order, ok := nextOrder[m.Kind]
		if !ok {
			order = 0
		}
		nextOrder[m.Kind] = order + 1
		if _, err := tx.Exec(`INSERT INTO provider_models (provider_id, kind, model_id, temperature, max_tokens, chain_order) VALUES (?, ?, ?, ?, ?, ?)`,
			providerID, m.Kind, m.ModelID, temp, maxTok, order); err != nil {
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
	res, err := tx.Exec(`INSERT INTO model_providers (api_type, name, base_url, api_key_enc) VALUES (?, ?, ?, ?)`,
		p.APIType, p.Name, p.BaseURL, p.APIKeyEnc)
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
func (d *DB) UpdateProvider(ctx context.Context, id int64, apiType, name, baseURL string, models []ProviderModel, replaceModels bool) error {
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
	if _, err := tx.Exec(`UPDATE model_providers SET api_type = ?, name = ?, base_url = ? WHERE id = ?`, apiType, name, baseURL, id); err != nil {
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

// ReorderModels 同类型内按给定 id 序重写 chain_order（0..n-1）。
// ids 应已由调用方校验恰好覆盖该 kind 的全部条目。
func (d *DB) ReorderModels(ctx context.Context, kind string, ids []int64) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, id := range ids {
		if _, err := tx.Exec(`UPDATE provider_models SET chain_order = ? WHERE id = ? AND kind = ?`, i, id, kind); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CreateModel 新建一条模型条目（chain_order 尾追同类型链尾）。
func (d *DB) CreateModel(ctx context.Context, m *ProviderModel) (int64, error) {
	var order int
	if err := d.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(chain_order), -1) + 1 FROM provider_models WHERE kind = ?`, m.Kind).Scan(&order); err != nil {
		return 0, err
	}
	m.ChainOrder = order
	var temp, maxTok any
	if m.Temperature.Valid {
		temp = m.Temperature.Float64
	}
	if m.MaxTokens.Valid {
		maxTok = m.MaxTokens.Int64
	}
	res, err := d.ExecContext(ctx,
		`INSERT INTO provider_models (provider_id, kind, model_id, temperature, max_tokens, chain_order) VALUES (?, ?, ?, ?, ?, ?)`,
		m.ProviderID, m.Kind, m.ModelID, temp, maxTok, order)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateModel 更新模型条目（模型名与参数；kind 不可改——换链删了重加）。
func (d *DB) UpdateModel(ctx context.Context, id int64, modelID string, temperature sql.NullFloat64, maxTokens sql.NullInt64) error {
	res, err := d.ExecContext(ctx,
		`UPDATE provider_models SET model_id = ?, temperature = ?, max_tokens = ? WHERE id = ?`,
		modelID, temperature, maxTokens, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteModel 删除模型条目；链序空洞容忍（ReorderModels 重编号即可）。
func (d *DB) DeleteModel(ctx context.Context, id int64) error {
	res, err := d.ExecContext(ctx, `DELETE FROM provider_models WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
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
