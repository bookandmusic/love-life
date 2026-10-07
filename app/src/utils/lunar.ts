// 内置离线农历换算（无第三方依赖，社区通用 1900–2100 数据表 + 经典算法）。
// 说明：首版表驱动实现，2026-08 起入库；周期表是**单一数据源**（见下方 import），
// 与 Go 侧同源同表，改表只需改一份。
//
// 周期编码（每农历年的整型 info）：
//   低 4 位       = 闰月（1-12；0 表示无闰）
//   0x10000       = 闰月为大月(30 天)
//   0x10000>>m    = 第 m 个月为大月(30 天)（m=1..12）
//   其余月份为小月(29 天)

// 周期表是**单一数据源**：与 Go 侧（server/internal/lunar/lunar.go）embed 的是同一份 JSON，
// 故两侧不可能漂移（历史上是两份拷贝 + 一个读源码比对的测试，已废弃）。
import LUNAR_TABLE from '../../../server/internal/lunar/lunar_table.json' with { type: 'json' }

const LUNAR_INFO: number[] = LUNAR_TABLE

const BASE_YEAR = 1900

/** 该农历年的闰月（1-12；0 表示无闰） */
export function leapMonth(y: number): number {
  return LUNAR_INFO[y - BASE_YEAR] & 0xf
}

/** 该农历年闰月的天数（无闰返回 0） */
export function leapDays(y: number): number {
  const lm = leapMonth(y)
  return lm ? (LUNAR_INFO[y - BASE_YEAR] & 0x10000 ? 30 : 29) : 0
}

/** 农历某月天数（大月 30，小月 29） */
export function monthDays(y: number, m: number): number {
  return LUNAR_INFO[y - BASE_YEAR] & (0x10000 >> m) ? 30 : 29 // 大月 30，小月 29
}

function yearDays(y: number): number {
  let sum = 348
  for (let i = 0x8000; i > 0x8; i >>= 1) sum += LUNAR_INFO[y - BASE_YEAR] & i ? 1 : 0
  return sum + leapDays(y)
}

/** 农历 → 公历（返回 JS Date；超出表范围时返回 null） */
function lunarToSolar(y: number, m: number, d: number, isLeap = false): Date | null {
  if (y < BASE_YEAR || y > BASE_YEAR + LUNAR_INFO.length - 1) return null
  const lm = leapMonth(y)
  if (m < 1 || m > 12) return null
  if (d < 1 || d > monthDays(y, m)) return null // 小月无“三十”，该农历日不存在
  if (isLeap && lm !== m) return null
  // 闰月自身的天数与平月不同：日期上限用 leapDays
  if (isLeap && d > leapDays(y)) return null
  let offset = 0
  for (let i = BASE_YEAR; i < y; i++) offset += yearDays(i)
  for (let i = 1; i < m; i++) {
    offset += monthDays(y, i)
    if (i === lm) offset += leapDays(y)
  }
  // 闰 m 月排在平 m 月之后：先加平月 m 整月；d-1 自然落在闰月内
  if (isLeap) offset += monthDays(y, m)
  offset += d - 1
  const date = new Date(1900, 0, 31)
  date.setDate(date.getDate() + offset)
  return date
}

/** 农历某月某日的下一次（含今天）公历日期，返回 YYYY-MM-DD；找不到返回 null */
export function nextLunarOccurrence(m: number, d: number, isLeap = false, todayISO?: string): string | null {
  const today = todayISO ? new Date(todayISO + 'T00:00:00') : new Date()
  const startYear = today.getFullYear()
  // 闰月**只由 isLeap 决定**，不做「平月查不到就退到闰月」的兜底：
  //   - 兜底会让同一个纪念日在同一年内命中两次（先平月、再闰月），一年响两次；
  //   - 也会让 isLeap 失去意义（闰月年里 leap=false 与 leap=true 返回同一天），
  //     而用户在滚轮里选没选「闰X月」是明确的（闰月是独立选项）。
  // 与 Go 侧 NextLunarOccurrence 同口径（ARCHITECTURE「数据模型」Anniversary：闰月语义由 is_leap 承载）。
  for (let y = startYear; y <= BASE_YEAR + LUNAR_INFO.length - 1; y++) {
    if (isLeap && leapMonth(y) !== m) continue
    const dt = lunarToSolar(y, m, d, isLeap)
    if (!dt) continue
    // 与“今天”比较（忽略时分）；用换算结果自身的公历年（冬月/腊月会落到次年 1 月，取农历年 y 会差一年）
    const cmp = new Date(today.getFullYear(), today.getMonth(), today.getDate())
    if (dt >= cmp) {
      const Y = dt.getFullYear()
      const M = String(dt.getMonth() + 1).padStart(2, '0')
      const D = String(dt.getDate()).padStart(2, '0')
      return `${Y}-${M}-${D}`
    }
  }
  return null
}

/** 农历月名（1-12：正月…冬月、腊月）——仅本模块使用（外部只需 lunarMonthText/lunarDayText） */
const LUNAR_MONTHS = ['', '正', '二', '三', '四', '五', '六', '七', '八', '九', '十', '冬', '腊']

/** 农历日名（1-30：初一…初十、十一…十九、二十、十一…廿九、三十）——农历日历写法 */
export const LUNAR_DAYS = ['', '初一', '初二', '初三', '初四', '初五', '初六', '初七', '初八', '初九', '初十', '十一', '十二', '十三', '十四', '十五', '十六', '十七', '十八', '十九', '二十', '廿一', '廿二', '廿三', '廿四', '廿五', '廿六', '廿七', '廿八', '廿九', '三十']

/** 农历月 → 中文（如“农历八月”/“农历闰八月”） */
export function lunarMonthText(m: number, isLeap = false): string {
  return `农历${isLeap ? '闰' : ''}${LUNAR_MONTHS[m] || m}月`
}

/** 农历月/日 → 中文（如“农历八月十五”/“农历闰八月十五”） */
export function lunarDayText(m: number, d: number, isLeap = false): string {
  return `${lunarMonthText(m, isLeap)}${LUNAR_DAYS[d] || d}`
}