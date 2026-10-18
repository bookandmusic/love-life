package store

import (
	"context"
	"database/sql"
	"encoding/json"
)

// InsertNotification 站内通知入库（现为 12+1 类；内容删除事件带快照、无跳转）。
func (d *DB) InsertNotification(ctx context.Context, recipientID int64, eventType, targetType string, targetID int64, snapshot string) error {
	_, err := d.ExecContext(ctx,
		`INSERT INTO notifications (recipient_id, event_type, target_type, target_id, snapshot_json) VALUES (?, ?, ?, ?, ?)`,
		recipientID, eventType, targetType, targetID, snapshot)
	return err
}

// DeletedNotice 内容删除站内通知的参数包。
// 「删除对方内容 → 通知创建者 / 自删静默 / 仅站内」这条规则集中由 NotifyContentDeleted 实现，
// 避免 5 类内容 × 2 通道各自手写守卫与快照拼装。
type DeletedNotice struct {
	CreatorID   int64  // 内容创建者（收件人）
	DeleterID   int64  // 删除者（发起人）；与 CreatorID 相同则静默
	DeleterName string // 删除者显示名（真实用户名；AI 代操作同口径）
	EventType   string // post_deleted / comment_deleted / album_deleted / anniversary_deleted / map_point_deleted
	TargetType  string // post / comment / album / anniversary / map_point
	TargetID    int64
	// ViaAI AI 代操作：快照写 via:"ai"（接收端据此判别来源，ARCHITECTURE「通知分发器」快照字段读法）
	ViaAI bool
	Extra map[string]any // 快照附加字段（title / content / name…）；deleted_by / via 由本方法写入
}

// NotifyContentDeleted 删除对方内容时发站内通知；自删静默。
// 内容删除仅站内，故直接落 notifications 表，不经 notify.Dispatcher。
func (d *DB) NotifyContentDeleted(ctx context.Context, n DeletedNotice) error {
	if n.CreatorID == 0 || n.CreatorID == n.DeleterID {
		return nil
	}
	snap := make(map[string]any, len(n.Extra)+2)
	for k, v := range n.Extra {
		snap[k] = v
	}
	snap["deleted_by"] = n.DeleterName
	if n.ViaAI {
		snap["via"] = "ai"
	}
	b, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	return d.InsertNotification(ctx, n.CreatorID, n.EventType, n.TargetType, n.TargetID, string(b))
}

type Notification struct {
	ID         int64
	EventType  string
	TargetType sql.NullString
	TargetID   sql.NullInt64
	Snapshot   sql.NullString
	CreatedAt  string
}

// ListNotifications 未消费通知（点击即删，列表内均为未读）；按 id 倒序。
func (d *DB) ListNotifications(ctx context.Context, recipientID int64, limit, offset int) ([]*Notification, error) {
	rows, err := d.QueryContext(ctx, `
		SELECT id, event_type, target_type, target_id, snapshot_json, created_at
		FROM notifications WHERE recipient_id = ? ORDER BY id DESC LIMIT ? OFFSET ?`,
		recipientID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*Notification
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.EventType, &n.TargetType, &n.TargetID, &n.Snapshot, &n.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, &n)
	}
	return list, rows.Err()
}

// UnreadNotificationCount 未消费条数：通知点击即删，剩余即未读。
func (d *DB) UnreadNotificationCount(ctx context.Context, recipientID int64) (int, error) {
	var n int
	err := d.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notifications WHERE recipient_id = ?`, recipientID).Scan(&n)
	return n, err
}

// DeleteNotification 消费单条：点击通知即删除（PRODUCT「通知中心」：临时消费品）。
func (d *DB) DeleteNotification(ctx context.Context, id, recipientID int64) error {
	_, err := d.ExecContext(ctx,
		`DELETE FROM notifications WHERE id = ? AND recipient_id = ?`, id, recipientID)
	return err
}

// DeleteAllNotifications 清空：删除收件人全部通知（「清空」按钮）。
func (d *DB) DeleteAllNotifications(ctx context.Context, recipientID int64) error {
	_, err := d.ExecContext(ctx,
		`DELETE FROM notifications WHERE recipient_id = ?`, recipientID)
	return err
}
