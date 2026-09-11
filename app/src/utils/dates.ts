// 日期小工具（APP 展示层）：纪念日「下一次出现」推算 + 时间轴月份分组。
// 语义与 mock（tools/mock/routes.mjs daysUntilDate）保持一致：以**今年**的月/日为准，已过则顺延一年。

/** 解析 YYYY-MM-DD 的年/月/日；不可解析时为 NaN（调用方用 Number.isFinite 判定） */
export function ymdOf(date: string): { y: number; m: number; d: number } {
  const [y, m, d] = (date || '').split('-').map(Number)
  return { y, m, d }
}

/** 「每年重复」的月/日从今天起的下一次出现（含今天）；日期不可解析时返回 null */
export function nextOccurrence(dateStr: string): Date | null {
  const { m, d } = ymdOf(dateStr)
  if (!Number.isFinite(m) || !Number.isFinite(d)) return null
  const today = new Date()
  today.setHours(0, 0, 0, 0)
  const next = new Date(today.getFullYear(), m - 1, d)
  next.setHours(0, 0, 0, 0)
  if (Number.isNaN(next.getTime())) return null
  if (next.getTime() < today.getTime()) next.setFullYear(today.getFullYear() + 1)
  return next
}

/** 距下一次出现还有几天；不可解析返回 NaN（排序时需自行置底） */
export function daysUntilDate(dateStr: string): number {
  const next = nextOccurrence(dateStr)
  if (!next) return NaN
  const today = new Date()
  today.setHours(0, 0, 0, 0)
  return Math.round((next.getTime() - today.getTime()) / 86400000)
}

/** 下一次出现的 YYYY-MM-DD；不可解析返回 '' */
export function nextSolarDate(dateStr: string): string {
  const next = nextOccurrence(dateStr)
  if (!next) return ''
  const p = (n: number) => String(n).padStart(2, '0')
  return `${next.getFullYear()}-${p(next.getMonth() + 1)}-${p(next.getDate())}`
}

/** 月份分组键：'YYYY-MM'；无日期归入 'unknown' */
export function monthKey(dateStr: string): string {
  return (dateStr || '').slice(0, 7) || 'unknown'
}

/** 月份分组标题：'2026年8月'；无日期为「其他」 */
export function monthLabel(key: string): string {
  if (key === 'unknown') return '其他'
  return `${Number(key.slice(0, 4))}年${Number(key.slice(5, 7))}月`
}

export interface MonthGroup<T> {
  key: string
  label: string
  items: T[]
}

/** 按月份倒序分组（组内保持入参顺序，无日期的排最后）——时间轴/选择页共用 */
export function groupByMonth<T>(items: T[], dateOf: (item: T) => string): MonthGroup<T>[] {
  const groups = new Map<string, T[]>()
  for (const it of items) {
    const k = monthKey(dateOf(it))
    const arr = groups.get(k)
    if (arr) arr.push(it)
    else groups.set(k, [it])
  }
  return [...groups.entries()]
    .sort((a, b) => (a[0] === 'unknown' ? 1 : b[0] === 'unknown' ? -1 : b[0].localeCompare(a[0])))
    .map(([key, items]) => ({ key, label: monthLabel(key), items }))
}
