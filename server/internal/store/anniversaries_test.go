package store

import (
	"testing"
	"time"
)

// 提醒去重按**本地**日判重：created_at 存 UTC 串，本地 00:30 的记录其 UTC 日可能是前一天，
// 若只比 UTC 日就会在同一本地日重复发送。
func TestAnniversaryReminderSentTodayLocalDate(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	seedPostAuthor(t, db)
	ctx := t.Context()

	// 提醒记录发生在本地 00:30；now 取同日 08:00（两者相差不足一天，本地日相同）。
	created := time.Date(2026, 9, 13, 0, 30, 0, 0, time.Local)
	now := time.Date(2026, 9, 13, 8, 0, 0, 0, time.Local)
	if _, err := db.ExecContext(ctx,
		`INSERT INTO notifications (recipient_id, event_type, target_type, target_id, created_at)
		 VALUES (1, 'anniversary_reminder', 'anniversary', 7, ?)`, FormatDBTime(created)); err != nil {
		t.Fatalf("seed notification: %v", err)
	}

	sent, err := db.AnniversaryReminderSentToday(ctx, 7, now)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if !sent {
		t.Fatalf("本地同日（created_at=%s 本地 %s，now=%s）应判为已发送",
			FormatDBTime(created), created.Format("2006-01-02 15:04"), now.Format("2006-01-02 15:04"))
	}

	// 另一本地日不应误判为已发。
	nextDay, err := db.AnniversaryReminderSentToday(ctx, 7, now.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("query next day: %v", err)
	}
	if nextDay {
		t.Fatal("次日不应判为已发送")
	}
}
