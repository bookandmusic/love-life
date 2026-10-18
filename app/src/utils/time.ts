// 库内时间串的解析与展示。
//
// 服务端统一写 UTC：schema 的 `datetime('now')`（001_init.sql）与 store/timeutil.go 的
// FormatDBTime（t.UTC()）都是 UTC。DB 串形如 "2026-10-17 15:16:22"、不带时区后缀，
// 而 JS 的 `new Date('YYYY-MM-DDTHH:mm:ss')`（无后缀）按**本地**时区解析——直接解析会把
// UTC 钟面当本地时间显示（UTC+8 下整体慢 8 小时）。故解析时补 'Z'，展示再用 Date 的
// 本地时区 getter，任何时区的设备都显示本地时间。
//
// 兼容：历史 RFC3339 行自带时区后缀（Z 或 ±HH:mm），不重复补；解析失败返回 null，
// 由调用方兜底（不因格式异常清空列表）。

// 解析库内时间串为 Date（按 UTC 解释）；失败返回 null
export function parseDBTime(s: string): Date | null {
  const raw = (s ?? '').trim()
  if (!raw) return null
  const hasTZ = /(?:Z|[+-]\d{2}:?\d{2})$/i.test(raw)
  const d = new Date(raw.replace(' ', 'T') + (hasTZ ? '' : 'Z'))
  return Number.isNaN(d.getTime()) ? null : d
}

const p = (n: number) => String(n).padStart(2, '0')

// 日期时间展示："YYYY-MM-DD · HH:mm"（按本地时区）
export function fmtDateTime(s: string): string {
  const d = parseDBTime(s)
  if (!d) return (s ?? '').slice(0, 16).replace(/[T ]/, ' · ')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} · ${p(d.getHours())}:${p(d.getMinutes())}`
}

// 日期展示："YYYY-MM-DD"（按本地时区）
export function fmtDate(s: string): string {
  const d = parseDBTime(s)
  if (!d) return (s ?? '').slice(0, 10)
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}
