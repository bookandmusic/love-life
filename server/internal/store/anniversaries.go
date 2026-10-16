package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/bookandmusic/love-life/server/internal/lunar"
)

// MaxRemindDaysBefore 提前提醒天数的上限（PRODUCT「纪念日」：0-90 天，默认 1 天）。
const MaxRemindDaysBefore = 90

type Anniversary struct {
	ID               int64
	Name             string
	Date             string // YYYY-MM-DD；type=lunar 时为农历日期（统一契约，不存 lunar_* 冗余）
	Type             string // solar | lunar
	IsLeap           bool   // 仅 type=lunar 时有意义（true=闰月纪念日）；solar 恒 false
	RemindDaysBefore int
	CreatedAt        string
	CreatedBy        int64
}

func (d *DB) CreateAnniversary(ctx context.Context, name, date, annivType string, isLeap bool, remindDaysBefore int, createdBy int64) (int64, error) {
	res, err := d.ExecContext(ctx,
		`INSERT INTO anniversaries (name, date, type, is_leap, remind_days_before, created_by) VALUES (?, ?, ?, ?, ?, ?)`,
		name, date, annivType, isLeapToInt(isLeap), remindDaysBefore, createdBy)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// isLeapToInt bool → INTEGER 0/1（schema 列语义；对齐 SQLite 无原生 bool）。
func isLeapToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// GetAnniversary 单个纪念日（删除/提醒配置前校验用）；不存在或已删返回 ErrNotFound。
func (d *DB) GetAnniversary(ctx context.Context, id int64) (*Anniversary, error) {
	row := d.QueryRowContext(ctx, `SELECT id, name, date, type, is_leap, remind_days_before, created_at, created_by
		FROM anniversaries WHERE id = ? AND deleted_at IS NULL`, id)
	var a Anniversary
	var isLeap int
	if err := row.Scan(&a.ID, &a.Name, &a.Date, &a.Type, &isLeap, &a.RemindDaysBefore, &a.CreatedAt, &a.CreatedBy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	a.IsLeap = isLeap == 1
	return &a, nil
}

func (d *DB) ListAnniversaries(ctx context.Context) ([]*Anniversary, error) {
	rows, err := d.QueryContext(ctx, `SELECT id, name, date, type, is_leap, remind_days_before, created_at, created_by
		FROM anniversaries WHERE deleted_at IS NULL ORDER BY date`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*Anniversary
	for rows.Next() {
		var a Anniversary
		var isLeap int
		if err := rows.Scan(&a.ID, &a.Name, &a.Date, &a.Type, &isLeap, &a.RemindDaysBefore, &a.CreatedAt, &a.CreatedBy); err != nil {
			return nil, err
		}
		a.IsLeap = isLeap == 1
		list = append(list, &a)
	}
	return list, rows.Err()
}

// PickNextAnniversary 下一次纪念日（含今天），从已取出的列表里挑：无法换算日期（如农历超表尾）
// 的条目不参与；全部无效或列表为空返回 ok=false（调用方省略该项，不得编造）。
// 纯函数，供所有调用方复用同一套挑选口径（列表由调用方按需从 ListAnniversaries 取）。
func PickNextAnniversary(list []*Anniversary, now time.Time) (*Anniversary, bool) {
	var best *Anniversary
	bestDays := 0
	for _, a := range list {
		d := a.DaysUntil(now) // 逐条算一次（lunar 换算有成本，不要对同一项重复算）
		if d < 0 {
			continue
		}
		if best == nil || d < bestDays {
			best, bestDays = a, d
		}
	}
	return best, best != nil
}

func (d *DB) UpdateAnniversaryReminder(ctx context.Context, id int64, daysBefore int) error {
	res, err := d.ExecContext(ctx,
		`UPDATE anniversaries SET remind_days_before = ? WHERE id = ? AND deleted_at IS NULL`, daysBefore, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) SoftDeleteAnniversary(ctx context.Context, id int64, actorType string, actorID int64) error {
	return softDeleteKind(ctx, d, "anniversary", id, actorType, actorID)
}

// NextOccurrence 下一次日期（含今天）：solar 按公历逐年重复；lunar 由农历日期换算
// 下一次公历（冬月/腊月常落次年，用换算结果自身的公历年）。解析失败返回零值。
func (a *Anniversary) NextOccurrence(now time.Time) time.Time {
	if a.Type == "lunar" {
		m, d := lunarMonthDay(a.Date)
		if m == 0 {
			return time.Time{}
		}
		next, _ := time.ParseInLocation("2006-01-02", lunar.NextLunarOccurrence(m, d, a.IsLeap, now), now.Location())
		return next
	}
	anniv, err := time.ParseInLocation("2006-01-02", a.Date, now.Location())
	if err != nil {
		return time.Time{}
	}
	thisYear := time.Date(now.Year(), anniv.Month(), anniv.Day(), 0, 0, 0, 0, now.Location())
	if thisYear.Before(time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())) {
		thisYear = thisYear.AddDate(1, 0, 0)
	}
	return thisYear
}

// lunarMonthDay 从 YYYY-MM-DD 取月/日（lunar 语义）；格式不符返回 0,0。
func lunarMonthDay(date string) (int, int) {
	if len(date) != 10 || date[4] != '-' || date[7] != '-' {
		return 0, 0
	}
	m, d := 0, 0
	for _, c := range date[5:7] {
		if c < '0' || c > '9' {
			return 0, 0
		}
		m = m*10 + int(c-'0')
	}
	for _, c := range date[8:10] {
		if c < '0' || c > '9' {
			return 0, 0
		}
		d = d*10 + int(c-'0')
	}
	return m, d
}

// DaysUntil 下次纪念日距今天数（当天为 0；lunar 找不到下一次为 -1）。
func (a *Anniversary) DaysUntil(now time.Time) int {
	next := a.NextOccurrence(now)
	if next.IsZero() {
		return -1
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return int(next.Sub(today).Hours() / 24)
}

// NextSolarDate 下一次公历日期：solar=原 date；lunar=农历换算的下一次公历 YYYY-MM-DD
// （对齐 mock annivNextDate，/space 展示用）；无法换算返回 ""。
func (a *Anniversary) NextSolarDate(now time.Time) string {
	if a.Type != "lunar" {
		return a.Date
	}
	next := a.NextOccurrence(now)
	if next.IsZero() {
		return ""
	}
	return next.Format("2006-01-02")
}

// AnniversaryReminderSentToday 当日同目标已发过提醒则跳过（cron 幂等）。
func (d *DB) AnniversaryReminderSentToday(ctx context.Context, anniversaryID int64, now time.Time) (bool, error) {
	var n int
	err := d.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notifications WHERE event_type = 'anniversary_reminder' AND target_id = ? AND date(created_at) = date(?)`,
		anniversaryID, FormatDBTime(now)).Scan(&n)
	return n > 0, err
}
