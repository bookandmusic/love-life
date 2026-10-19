package store

import (
	"fmt"
	"time"
)

// 时间口径：库内时间列一律存 UTC 的 `datetime` 形态（与 `datetime('now')` 同形）；
// 读侧兼容历史行残留的 RFC3339（见 ParseDBTime）。**用户可见的「日期」按部署者本地时区判定**
// （进程 TZ，可用环境变量 `TZ=Asia/Shanghai` 覆盖）。EXIF taken_at 是相机墙钟时间、
// 无时区信息，不得参与本换算。

// dbTimeLayout 库内时间列的规范形态：UTC、与 SQLite `datetime('now')` 同形。
// 写入一律用它——历史上曾写 RFC3339（`2026-10-01T10:00:00Z`），
// 而 SQL 比较（`expires_at > datetime('now')`）是**字符串比较**，
// 第 11 位 `'T'(0x54) > ' '(0x20)` 恒真，导致已过期条目被判为未过期。
const dbTimeLayout = "2006-01-02 15:04:05"

// dbTimeLayouts 库内可能出现的两种时间串格式：规范形态在前，历史 RFC3339 在后。
var dbTimeLayouts = []string{dbTimeLayout, time.RFC3339}

// FormatDBTime 统一库内时间写法（UTC 的 `datetime` 形态）。
func FormatDBTime(t time.Time) string {
	return t.UTC().Format(dbTimeLayout)
}

// ParseDBTime 解析库内时间串：兼容规范形态与历史 RFC3339 行（老库升级后仍可读）。
func ParseDBTime(raw string) (time.Time, error) {
	for _, layout := range dbTimeLayouts {
		if t, err := time.ParseInLocation(layout, raw, time.UTC); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("无法解析库内时间: %q", raw)
}

// LocalDate 把库内 UTC 时间串转为本地时区的 `YYYY-MM-DD`。
// 解析失败时回退为原串前 10 字符（保持显示不断档）。
func LocalDate(raw string) string {
	if raw == "" {
		return ""
	}
	if t, err := ParseDBTime(raw); err == nil {
		return t.Local().Format("2006-01-02")
	}
	if len(raw) >= 10 {
		return raw[:10]
	}
	return raw
}

// LocalMonth 把库内 UTC 时间串转为本地时区的 `YYYY-MM`。
// 与按月聚合的 SQL 口径（`strftime('%Y-%m', created_at, 'localtime')`）同一份时区换算，
// 供客户端按同一个键分组与跳详情——否则标题按 UTC 截月、详情按本地月查，跨 UTC 月界的照片会点进错的月。
// taken_at 是 EXIF 墙钟时间（无时区），原样取前 7 位即本地月，不参与换算。
func LocalMonth(raw string) string {
	if raw == "" {
		return ""
	}
	if t, err := ParseDBTime(raw); err == nil {
		return t.Local().Format("2006-01")
	}
	if len(raw) >= 7 {
		return raw[:7]
	}
	return raw
}
