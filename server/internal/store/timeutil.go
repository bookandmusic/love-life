package store

import "time"

// 时间口径（DESIGN §12）：库内 created_at/deleted_at 一律存 UTC（`datetime('now')`，
// 形如 `YYYY-MM-DD HH:MM:SS` 或 RFC3339）；**用户可见的「日期」按部署者本地时区判定**
// （进程 TZ，可用环境变量 `TZ=Asia/Shanghai` 覆盖）。EXIF taken_at 是相机墙钟时间、
// 无时区信息，不得参与本换算。

// localDateLayouts 库内可能出现的两种时间串格式。
var localDateLayouts = []string{"2006-01-02 15:04:05", time.RFC3339}

// LocalDate 把库内 UTC 时间串转为本地时区的 `YYYY-MM-DD`。
// 解析失败时回退为原串前 10 字符（保持显示不断档）。
func LocalDate(raw string) string {
	if raw == "" {
		return ""
	}
	for _, layout := range localDateLayouts {
		if t, err := time.ParseInLocation(layout, raw, time.UTC); err == nil {
			return t.Local().Format("2006-01-02")
		}
	}
	if len(raw) >= 10 {
		return raw[:10]
	}
	return raw
}
