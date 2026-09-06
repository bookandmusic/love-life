package store

import (
	"context"
	"database/sql"
	"time"
)

// InsertNotification 站内通知入库（事件类型 6+1 见需求 §4.8；内容删除事件带快照、无跳转）。
func (d *DB) InsertNotification(ctx context.Context, recipientID int64, eventType, targetType string, targetID int64, snapshot string) error {
	_, err := d.ExecContext(ctx,
		`INSERT INTO notifications (recipient_id, event_type, target_type, target_id, snapshot_json) VALUES (?, ?, ?, ?, ?)`,
		recipientID, eventType, targetType, targetID, snapshot)
	return err
}

type Notification struct {
	ID         int64
	EventType  string
	TargetType sql.NullString
	TargetID   sql.NullInt64
	Snapshot   sql.NullString
	IsRead     bool
	CreatedAt  string
}

func (d *DB) ListNotifications(ctx context.Context, recipientID int64, limit, offset int) ([]*Notification, error) {
	rows, err := d.QueryContext(ctx, `
		SELECT id, event_type, target_type, target_id, snapshot_json, is_read, created_at
		FROM notifications WHERE recipient_id = ? ORDER BY id DESC LIMIT ? OFFSET ?`,
		recipientID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*Notification
	for rows.Next() {
		var n Notification
		var read int
		if err := rows.Scan(&n.ID, &n.EventType, &n.TargetType, &n.TargetID, &n.Snapshot, &read, &n.CreatedAt); err != nil {
			return nil, err
		}
		n.IsRead = read == 1
		list = append(list, &n)
	}
	return list, rows.Err()
}

func (d *DB) UnreadNotificationCount(ctx context.Context, recipientID int64) (int, error) {
	var n int
	err := d.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notifications WHERE recipient_id = ? AND is_read = 0`, recipientID).Scan(&n)
	return n, err
}

func (d *DB) MarkNotificationRead(ctx context.Context, id, recipientID int64) error {
	_, err := d.ExecContext(ctx,
		`UPDATE notifications SET is_read = 1, read_at = ? WHERE id = ? AND recipient_id = ?`,
		time.Now().UTC().Format(time.RFC3339), id, recipientID)
	return err
}

func (d *DB) MarkAllNotificationsRead(ctx context.Context, recipientID int64) error {
	_, err := d.ExecContext(ctx,
		`UPDATE notifications SET is_read = 1, read_at = ? WHERE recipient_id = ? AND is_read = 0`,
		time.Now().UTC().Format(time.RFC3339), recipientID)
	return err
}
