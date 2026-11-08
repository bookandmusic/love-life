package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// WebhookChannel 空间级外部通知渠道（admin 后台配置，订阅事件的渠道全局推送）。
type WebhookChannel struct {
	ID           int64
	Name         string
	Method       string
	URL          string
	Headers      map[string]string
	BodyTemplate string
	Events       []string
}

func (c *WebhookChannel) scan(row interface{ Scan(...any) error }) error {
	var headers, events string
	if err := row.Scan(&c.ID, &c.Name, &c.Method, &c.URL, &headers, &c.BodyTemplate, &events); err != nil {
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

const webhookColumns = `id, name, method, url, headers_json, body_template, events_json`

func (d *DB) CreateWebhookChannel(ctx context.Context, c *WebhookChannel) (int64, error) {
	headers, _ := json.Marshal(c.Headers)
	events, _ := json.Marshal(c.Events)
	res, err := d.ExecContext(ctx,
		`INSERT INTO webhook_channels (name, method, url, headers_json, body_template, events_json)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		c.Name, c.Method, c.URL, string(headers), c.BodyTemplate, string(events))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) UpdateWebhookChannel(ctx context.Context, c *WebhookChannel) error {
	headers, _ := json.Marshal(c.Headers)
	events, _ := json.Marshal(c.Events)
	res, err := d.ExecContext(ctx,
		`UPDATE webhook_channels SET name=?, method=?, url=?, headers_json=?, body_template=?, events_json=? WHERE id=?`,
		c.Name, c.Method, c.URL, string(headers), c.BodyTemplate, string(events), c.ID)
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
