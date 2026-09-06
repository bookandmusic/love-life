package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Confirm 卡片流转 sentinel：Confirm 侧按 errors.Is 判断过期/重复，不匹配错误文案。
var (
	ErrConfirmExpired  = errors.New("confirm action expired")
	ErrConfirmResolved = errors.New("confirm action already resolved")
)

// ---- 模型配置 ----

type ModelConfig struct {
	ID        int64
	Name      string
	BaseURL   string
	APIKeyEnc string
	Model     string
	IsDefault bool
	IsBackup  bool
}

func (d *DB) SaveModelConfig(ctx context.Context, c *ModelConfig) (int64, error) {
	if c.ID == 0 {
		res, err := d.ExecContext(ctx,
			`INSERT INTO model_provider_configs (name, base_url, api_key_enc, model, is_default, is_backup) VALUES (?, ?, ?, ?, ?, ?)`,
			c.Name, c.BaseURL, c.APIKeyEnc, c.Model, c.IsDefault, c.IsBackup)
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	}
	res, err := d.ExecContext(ctx,
		`UPDATE model_provider_configs SET name=?, base_url=?, api_key_enc=?, model=?, is_default=?, is_backup=? WHERE id=?`,
		c.Name, c.BaseURL, c.APIKeyEnc, c.Model, c.IsDefault, c.IsBackup, c.ID)
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, ErrNotFound
	}
	return c.ID, nil
}

func (d *DB) DeleteModelConfig(ctx context.Context, id int64) error {
	res, err := d.ExecContext(ctx, `DELETE FROM model_provider_configs WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) ListModelConfigs(ctx context.Context) ([]*ModelConfig, error) {
	rows, err := d.QueryContext(ctx, `SELECT id, name, base_url, api_key_enc, model, is_default, is_backup
		FROM model_provider_configs ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*ModelConfig
	for rows.Next() {
		var c ModelConfig
		var def, backup int
		if err := rows.Scan(&c.ID, &c.Name, &c.BaseURL, &c.APIKeyEnc, &c.Model, &def, &backup); err != nil {
			return nil, err
		}
		c.IsDefault, c.IsBackup = def == 1, backup == 1
		list = append(list, &c)
	}
	return list, rows.Err()
}

func (d *DB) DefaultModelConfig(ctx context.Context) (*ModelConfig, error) {
	row := d.QueryRowContext(ctx, `SELECT id, name, base_url, api_key_enc, model, is_default, is_backup
		FROM model_provider_configs WHERE is_default = 1 LIMIT 1`)
	var c ModelConfig
	var def, backup int
	err := row.Scan(&c.ID, &c.Name, &c.BaseURL, &c.APIKeyEnc, &c.Model, &def, &backup)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c.IsDefault, c.IsBackup = def == 1, backup == 1
	return &c, nil
}

// ---- 对话历史 ----

type ChatMessage struct {
	ID        int64
	UserID    int64
	Role      string
	Content   string
	CardID    sql.NullInt64
	CreatedAt string
	Media     string // JSON [{name,type,size,data_url}] 仅回显用途（D8，不落媒体资源）
}

// SaveChatMessage 保存一条聊天消息（m.CardID 为 NULL 时落 NULL；m.Media 为 JSON 仅回显，D8）。
func (d *DB) SaveChatMessage(ctx context.Context, m *ChatMessage) (int64, error) {
	var card any
	if m.CardID.Valid && m.CardID.Int64 > 0 {
		card = m.CardID.Int64
	}
	res, err := d.ExecContext(ctx,
		`INSERT INTO chat_messages (user_id, role, content, card_id, media) VALUES (?, ?, ?, ?, ?)`,
		m.UserID, m.Role, m.Content, card, m.Media)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) ListChatMessages(ctx context.Context, userID int64, limit int) ([]*ChatMessage, error) {
	rows, err := d.QueryContext(ctx, `SELECT id, user_id, role, content, card_id, created_at, media
		FROM chat_messages WHERE user_id = ? ORDER BY id DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*ChatMessage
	for rows.Next() {
		var m ChatMessage
		if err := rows.Scan(&m.ID, &m.UserID, &m.Role, &m.Content, &m.CardID, &m.CreatedAt, &m.Media); err != nil {
			return nil, err
		}
		list = append(list, &m)
	}
	// 倒序取出后翻回时间正序
	for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
		list[i], list[j] = list[j], list[i]
	}
	return list, nil
}

// CleanExpiredChatMessages 按每用户保留时长清理（janitor 每小时）。
func (d *DB) CleanExpiredChatMessages(ctx context.Context) error {
	rows, err := d.QueryContext(ctx, `SELECT u.id, u.chat_retention_days FROM users WHERE u.chat_retention_days > 0`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type pair struct {
		id   int64
		days int
	}
	var pairs []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.id, &p.days); err != nil {
			return err
		}
		pairs = append(pairs, p)
	}
	for _, p := range pairs {
		cutoff := time.Now().AddDate(0, 0, -p.days).UTC().Format(time.RFC3339)
		if _, err := d.ExecContext(ctx, `DELETE FROM chat_messages WHERE user_id = ? AND created_at < ?`, p.id, cutoff); err != nil {
			return err
		}
	}
	return nil
}

// ---- 确认卡片 ----

type ConfirmAction struct {
	ID          int64
	UserID      int64
	ToolName    string
	ParamsJSON  string
	Status      string
	ExpiresAt   time.Time
	ResolvedAt  sql.NullString
	CreatedAt   string
}

func (d *DB) CreateConfirmAction(ctx context.Context, userID int64, toolName, paramsJSON string) (int64, error) {
	res, err := d.ExecContext(ctx,
		`INSERT INTO confirm_actions (user_id, tool_name, params_json, expires_at) VALUES (?, ?, ?, ?)`,
		userID, toolName, paramsJSON, time.Now().UTC().Add(15*time.Minute).Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) GetConfirmAction(ctx context.Context, id int64) (*ConfirmAction, error) {
	row := d.QueryRowContext(ctx, `SELECT id, user_id, tool_name, params_json, status, expires_at, resolved_at, created_at
		FROM confirm_actions WHERE id = ?`, id)
	var a ConfirmAction
	var expires string
	err := row.Scan(&a.ID, &a.UserID, &a.ToolName, &a.ParamsJSON, &a.Status, &expires, &a.ResolvedAt, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if a.ExpiresAt, err = time.Parse(time.RFC3339, expires); err != nil {
		return nil, err
	}
	return &a, nil
}

// ResolveConfirmAction 单次有效：仅 pending 且未过期可流转；过期置 expired。
func (d *DB) ResolveConfirmAction(ctx context.Context, id int64, to string) error {
	allowed := map[string]bool{"confirmed": true, "cancelled": true, "expired": true}
	if !allowed[to] {
		return errors.New("invalid status")
	}
	a, err := d.GetConfirmAction(ctx, id)
	if err != nil {
		return err
	}
	if a.Status != "pending" {
		return ErrConfirmResolved
	}
	if time.Now().After(a.ExpiresAt) && to != "expired" {
		_, _ = d.ExecContext(ctx, `UPDATE confirm_actions SET status='expired', resolved_at=? WHERE id=?`,
			time.Now().UTC().Format(time.RFC3339), id)
		return ErrConfirmExpired
	}
	_, err = d.ExecContext(ctx, `UPDATE confirm_actions SET status=?, resolved_at=? WHERE id=? AND status='pending'`,
		to, time.Now().UTC().Format(time.RFC3339), id)
	return err
}

// ---- 审计 ----

func (d *DB) InsertAuditLog(ctx context.Context, actorType string, actorID int64, action, digest, result string) error {
	_, err := d.ExecContext(ctx,
		`INSERT INTO audit_logs (actor_type, actor_id, action, params_digest, result) VALUES (?, ?, ?, ?, ?)`,
		actorType, actorID, action, digest, result)
	return err
}