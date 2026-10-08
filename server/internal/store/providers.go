package store

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
)

// 模型供应商：api_type 决定协议适配；用哪个模型、按什么顺序重试由模型条目决定——
// provider_models.kind 决定属于 chat / embedding 哪条链，chain_order 是同类型内的全局链序
// （跨供应商）。max_tokens / context_window 挂模型级，NULL = 调用不传（chat 组装请求时消费）。

type ProviderModel struct {
	ID         int64
	ProviderID int64
	Kind       string // chat / embedding：决定属于哪条链
	ModelID    string
	MaxTokens  sql.NullInt64
	// Thinking 思考档位：off（缺省）/ low / medium / high。现役模型关不掉思考，故 off = 该协议的最低档
	// （仍可能产生少量思考 token）。存在的理由：开启思考会产生思考 token 计费，故由用户按模型决定。
	Thinking string
	// ThinkingLevels 该模型支持的思考档位（"off,low,high" 之类，逗号分隔、固定顺序），缺省 "off"。
	// 放配置面而不在代码里判模型名：档位取值是模型属性（Gemini 3 Pro 只接受 low/high），
	// 而供应商别名会让名称匹配落空，落空即 400 或静默失效。
	ThinkingLevels string
	// ContextWindow 模型上下文窗口（token），NULL = 用服务端兜底预算；消费者是上下文组装的裁剪预算。
	ContextWindow sql.NullInt64
	ChainOrder    int // 同类型内的全局链序（跨供应商，重试即按它依次走）
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
		`SELECT id, provider_id, kind, model_id, max_tokens, thinking, thinking_levels, context_window, chain_order
			FROM provider_models WHERE provider_id IN (`+strings.Join(marks, ",")+`) ORDER BY provider_id, kind, chain_order, id`,
		args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	byProvider := map[int64][]ProviderModel{}
	for rows.Next() {
		var m ProviderModel
		if err := rows.Scan(&m.ID, &m.ProviderID, &m.Kind, &m.ModelID, &m.MaxTokens, &m.Thinking, &m.ThinkingLevels, &m.ContextWindow, &m.ChainOrder); err != nil {
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
		`SELECT pm.id, pm.provider_id, pm.kind, pm.model_id, pm.max_tokens, pm.thinking, pm.thinking_levels, pm.context_window, pm.chain_order,
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
		if err := rows.Scan(&m.ID, &m.ProviderID, &m.Kind, &m.ModelID, &m.MaxTokens, &m.Thinking, &m.ThinkingLevels, &m.ContextWindow, &m.ChainOrder,
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
		var maxTok, ctxWin any
		if m.MaxTokens.Valid {
			maxTok = m.MaxTokens.Int64
		}
		if m.ContextWindow.Valid {
			ctxWin = m.ContextWindow.Int64
		}
		order, ok := nextOrder[m.Kind]
		if !ok {
			order = 0
		}
		nextOrder[m.Kind] = order + 1
		if _, err := tx.Exec(`INSERT INTO provider_models (provider_id, kind, model_id, max_tokens, thinking, thinking_levels, context_window, chain_order) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			providerID, m.Kind, m.ModelID, maxTok, normalizeThinking(m.Thinking), normalizeThinkingLevels(m.ThinkingLevels), ctxWin, order); err != nil {
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
	var maxTok, ctxWin any
	if m.MaxTokens.Valid {
		maxTok = m.MaxTokens.Int64
	}
	if m.ContextWindow.Valid {
		ctxWin = m.ContextWindow.Int64
	}
	res, err := d.ExecContext(ctx,
		`INSERT INTO provider_models (provider_id, kind, model_id, max_tokens, thinking, thinking_levels, context_window, chain_order) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ProviderID, m.Kind, m.ModelID, maxTok, normalizeThinking(m.Thinking), normalizeThinkingLevels(m.ThinkingLevels), ctxWin, order)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateModel 更新模型条目（模型名与参数；kind 不可改——换链删了重加）。
func (d *DB) UpdateModel(ctx context.Context, id int64, modelID string, maxTokens sql.NullInt64, thinking, thinkingLevels string, contextWindow sql.NullInt64) error {
	res, err := d.ExecContext(ctx,
		`UPDATE provider_models SET model_id = ?, max_tokens = ?, thinking = ?, thinking_levels = ?, context_window = ? WHERE id = ?`,
		modelID, maxTokens, normalizeThinking(thinking), normalizeThinkingLevels(thinkingLevels), contextWindow, id)
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

// 思考档位取值（与 provider_models.thinking 的注释、admin 下拉一致）。
const (
	ThinkingOff    = "off"
	ThinkingLow    = "low"
	ThinkingMedium = "medium"
	ThinkingHigh   = "high"
)

// NormalizeThinking 归一思考档位：空串（客户端未传）→ off 且合法；已知档位 → 原值且合法；
// 其它取值 → off 且 ok=false（调用方据此拒绝，而不是把错值静默改成 off）。
//
// 空串必须落 off：update 路径的部分更新会传空串进来，若不归一，库里就出现空串这第三种表示，
// 与「off 是 off 的唯一表示」自相矛盾（列上还有 DEFAULT 'off'）。
// 取值面只在**这里**列一遍，API 侧调本函数而不是自己再 switch 一次。
func NormalizeThinking(v string) (string, bool) {
	switch v {
	case "", ThinkingOff:
		return ThinkingOff, true
	case ThinkingLow, ThinkingMedium, ThinkingHigh:
		return v, true
	default:
		return ThinkingOff, false
	}
}

// normalizeThinking 写入侧兜底：未知取值一律落 off（调用方已负责拒绝非法值，此处只防脏值入库）。
func normalizeThinking(v string) string {
	n, _ := NormalizeThinking(v)
	return n
}

// thinkingOrder 是档位的规范顺序，同时是「支持档位」集合的排序与去重依据。
var thinkingOrder = []string{ThinkingOff, ThinkingLow, ThinkingMedium, ThinkingHigh}

// NormalizeThinkingLevels 归一「该模型支持的思考档位」：逗号分隔的档位子集 → 去重、
// 按 off/low/medium/high 固定顺序拼接；空串 → off；含未知档位 → off 且 ok=false。
//
// 归一成固定顺序是为了让「同一集合」只有一种落库形态：否则 "low,high" 与 "high,low"
// 会写成两行不同字符串，比较（校验档位是否在集合内）就得先排序，白白复杂。
func NormalizeThinkingLevels(v string) (string, bool) {
	seen := map[string]bool{}
	for _, part := range strings.Split(v, ",") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue // 容忍 "low, high" 与末尾逗号，不算非法
		}
		if !slices.Contains(thinkingOrder, p) {
			return ThinkingOff, false
		}
		seen[p] = true
	}
	if len(seen) == 0 {
		return ThinkingOff, true
	}
	// off 恒在集合内：档位为 off 时必须可配置（关是任何模型的合法选择）。
	seen[ThinkingOff] = true
	kept := make([]string, 0, len(seen))
	for _, lv := range thinkingOrder {
		if seen[lv] {
			kept = append(kept, lv)
		}
	}
	return strings.Join(kept, ","), true
}

// normalizeThinkingLevels 写入侧兜底：未知取值一律落 off（调用方已负责拒绝非法值）。
func normalizeThinkingLevels(v string) string {
	n, _ := NormalizeThinkingLevels(v)
	return n
}

// ThinkingLevelAllowed 报告档位是否在该模型声明的支持集合内。
// 校验与注入分离：adapter 只注入被选中的档位，不参与「这个模型支持什么」的判断。
// 前置条件：levels 与 level 均已归一（NormalizeThinkingLevels / NormalizeThinking），
// 即逗号分隔、无空格——非归一的输入会得到 false。
func ThinkingLevelAllowed(levels, level string) bool {
	return slices.Contains(strings.Split(levels, ","), level)
}
