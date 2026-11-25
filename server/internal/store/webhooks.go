package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// WebhookChannel 外部通知渠道（admin 后台配置，按订阅事件推送）。
//
// OwnerID 决定投递范围与文案视角：NULL 为空间级渠道（群 / 共享），订阅即推送、
// 文案用第三人称具名；非 NULL 为该用户的私人渠道，仅当此人属于本次通知收件人
// 时推送，文案用第二人称（与 APP 站内通知同口径）。
type WebhookChannel struct {
	ID           int64
	Name         string
	Method       string
	URL          string
	Headers      map[string]string
	BodyTemplate string
	Events       []string
	OwnerID      sql.NullInt64
}

// IsShared 空间级渠道（群 / 共享）：投递不按收件人过滤。
func (c *WebhookChannel) IsShared() bool { return !c.OwnerID.Valid }

func (c *WebhookChannel) scan(row interface{ Scan(...any) error }) error {
	var headers, events string
	if err := row.Scan(&c.ID, &c.Name, &c.Method, &c.URL, &headers, &c.BodyTemplate, &events, &c.OwnerID); err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(headers), &c.Headers); err != nil {
		c.Headers = map[string]string{}
	}
	if err := json.Unmarshal([]byte(events), &c.Events); err != nil {
		c.Events = nil
	}
	return nil
}

const webhookColumns = `id, name, method, url, headers_json, body_template, events_json, owner_id`

func (d *DB) CreateWebhookChannel(ctx context.Context, c *WebhookChannel) (int64, error) {
	headers, _ := json.Marshal(c.Headers)
	events, _ := json.Marshal(c.Events)
	res, err := d.ExecContext(ctx,
		`INSERT INTO webhook_channels (name, method, url, headers_json, body_template, events_json, owner_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.Name, c.Method, c.URL, string(headers), c.BodyTemplate, string(events), c.OwnerID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) UpdateWebhookChannel(ctx context.Context, c *WebhookChannel) error {
	headers, _ := json.Marshal(c.Headers)
	events, _ := json.Marshal(c.Events)
	res, err := d.ExecContext(ctx,
		`UPDATE webhook_channels SET name=?, method=?, url=?, headers_json=?, body_template=?, events_json=?, owner_id=? WHERE id=?`,
		c.Name, c.Method, c.URL, string(headers), c.BodyTemplate, string(events), c.OwnerID, c.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) DeleteWebhookChannel(ctx context.Context, id int64) error {
	res, err := d.ExecContext(ctx, `DELETE FROM webhook_channels WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) ListWebhookChannels(ctx context.Context) ([]*WebhookChannel, error) {
	rows, err := d.QueryContext(ctx, `SELECT `+webhookColumns+` FROM webhook_channels ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*WebhookChannel
	for rows.Next() {
		c := &WebhookChannel{}
		if err := c.scan(rows); err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	return list, rows.Err()
}

// GetWebhookChannel 单取（发送测试用）。
func (d *DB) GetWebhookChannel(ctx context.Context, id int64) (*WebhookChannel, error) {
	row := d.QueryRowContext(ctx, `SELECT `+webhookColumns+` FROM webhook_channels WHERE id = ?`, id)
	c := &WebhookChannel{}
	if err := c.scan(row); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return c, nil
}

// ListWebhookChannelsByEvent 订阅了某事件的全部渠道（外部推送用）。
func (d *DB) ListWebhookChannelsByEvent(ctx context.Context, event string) ([]*WebhookChannel, error) {
	rows, err := d.QueryContext(ctx, `SELECT `+webhookColumns+` FROM webhook_channels WHERE events_json LIKE ?`, `%"`+event+`"%`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*WebhookChannel
	for rows.Next() {
		c := &WebhookChannel{}
		if err := c.scan(rows); err != nil {
			return nil, err
		}
		if containsEvent(c.Events, event) {
			list = append(list, c)
		}
	}
	return list, rows.Err()
}

func containsEvent(events []string, event string) bool {
	for _, e := range events {
		if e == event {
			return true
		}
	}
	return false
}
