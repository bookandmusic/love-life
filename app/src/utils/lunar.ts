// 内置离线农历换算（无第三方依赖，社区通用 1900–2100 数据表 + 经典算法）。
// 说明：首版表驱动实现，2026-08 起入库；后续如需更高精度可在 docs/TODO.md 中登记扩充/校准（当前覆盖 2000–2049 常用区间）。
//
// 周期编码（每农历年的整型 info）：
//   低 4 位       = 闰月（1-12；0 表示无闰）
//   0x10000       = 闰月为大月(30 天)
//   0x10000>>m    = 第 m 个月为大月(30 天)（m=1..12）
//   其余月份为小月(29 天)

const LUNAR_INFO = [
  0x04bd8, 0x04ae0, 0x0a570, 0x054d5, 0x0d260, 0x0d950, 0x16554, 0x056a0, 0x09ad0, 0x055d2, // 1900
  0x04ae0, 0x0a5b6, 0x0a4d0, 0x0d250, 0x1d255, 0x0b540, 0x0d6a0, 0x0ada2, 0x095b0, 0x14977, // 1910
  0x04970, 0x0a4b0, 0x0b4b5, 0x06a50, 0x06d40, 0x1ab54, 0x02b60, 0x09570, 0x052f2, 0x04970, // 1920
  0x06566, 0x0d4a0, 0x0ea50, 0x06e95, 0x05ad0, 0x02b60, 0x186e3, 0x092e0, 0x1c8d7, 0x0c950, // 1930
  0x0d4a0, 0x1d8a6, 0x0b550, 0x056a0, 0x1a5b4, 0x025d0, 0x092d0, 0x0d2b2, 0x0a950, 0x0b557, // 1940
  0x06ca0, 0x0b550, 0x15355, 0x04da0, 0x0a5b0, 0x14573, 0x052b0, 0x0a9a8, 0x0e950, 0x06aa0, // 1950
  0x0aea6, 0x0ab50, 0x04b60, 0x0aae4, 0x0a570, 0x05260, 0x0f263, 0x0d950, 0x05b57, 0x056a0, // 1960
  0x096d0, 0x04dd5, 0x04ad0, 0x0a4d0, 0x0d4d4, 0x0d250, 0x0d558, 0x0b540, 0x0b6a0, 0x195a6, // 1970
  0x095b0, 0x049b0, 0x0a974, 0x0a4b0, 0x0b27a, 0x06a50, 0x06d40, 0x0af46, 0x0ab60, 0x09570, // 1980
  0x04af5, 0x04970, 0x064b0, 0x074a3, 0x0ea50, 0x06b58, 0x055c0, 0x0ab60, 0x096d5, 0x092e0, // 1990
  0x0c960, 0x0d954, 0x0d4a0, 0x0da50, 0x07552, 0x056a0, 0x0abb7, 0x025d0, 0x092d0, 0x0c5b5, // 2000
  0x0a950, 0x0b4a0, 0x0baa4, 0x0ad50, 0x055d9, 0x04ba0, 0x0a5b0, 0x15176, 0x052b0, 0x0a930, // 2010
  0x07954, 0x06aa0, 0x0ad50, 0x05b52, 0x04b60, 0x0a6e6, 0x0a4e0, 0x0d260, 0x0ea65, 0x0d530, // 2020
  0x05aa0, 0x076a3, 0x096d0, 0x04afb, 0x04ad0, 0x0a4d0, 0x1d0b6, 0x0d250, 0x0d520, 0x0dd45, // 2030
  0x0b5a0, 0x056d0, 0x055b2, 0x049b0, 0x0a577, 0x0a4b0, 0x0aa50, 0x1b255, 0x06d20, 0x0ada0, // 2040
  0x14b63, 0x09370, 0x049f8, 0x04970, 0x064b0, 0x168a6, 0x0ea50, 0x06b20, 0x1a6c4, 0x0aae0, // 2050
  0x092e0, 0x0d2e3, 0x0c960, 0x0d557, 0x0d4a0, 0x0da50, 0x05d55, 0x056a0, 0x0a6d0, 0x055d4, // 2060
  0x052d0, 0x0a9b8, 0x0a950, 0x0b4a0, 0x0b6a6, 0x0ad50, 0x055a0, 0x0aba4, 0x0a5b0, 0x052b0, // 2070
  0x0b273, 0x06930, 0x07337, 0x06aa0, 0x0ad50, 0x14b55, 0x04b60, 0x0a570, 0x054e4, 0x0d160, // 2080
  0x0e968, 0x0d520, 0x0daa0, 0x16aa6, 0x056d0, 0x04ae0, 0x0a9d4, 0x0a2d0, 0x0d150, 0x0f252, // 2090
  0x0d520, // 2100
]

const BASE_YEAR = 1900

function leapMonth(y: number): number {
  return LUNAR_INFO[y - BASE_YEAR] & 0xf
}

function leapDays(y: number): number {
  const lm = leapMonth(y)
  return lm ? (LUNAR_INFO[y - BASE_YEAR] & 0x10000 ? 30 : 29) : 0
}

function monthDays(y: number, m: number): number {
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
  // isLeap=false 时平月优先、闰月兜底（老行为：mock/存量无闰标记数据按此解读）
  const leaps = isLeap ? [true] : [false, true]
  for (let y = startYear; y <= BASE_YEAR + LUNAR_INFO.length - 1; y++) {
    for (const leap of leaps) {
      if (leap && leapMonth(y) !== m) continue
      const dt = lunarToSolar(y, m, d, leap)
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
  }
  return null
}

/** 农历月名（1-12：正月…冬月、腊月）——仅本模块使用（外部只需 lunarMonthText/lunarDayText） */
const LUNAR_MONTHS = ['', '正', '二', '三', '四', '五', '六', '七', '八', '九', '十', '冬', '腊']

/** 农历日名（1-30：初一…初十、十一…十九、二十、十一…廿九、三十）——农历日历写法 */
export const LUNAR_DAYS = ['', '初一', '初二', '初三', '初四', '初五', '初六', '初七', '初八', '初九', '初十', '十一', '十二', '十三', '十四', '十五', '十六', '十七', '十八', '十九', '二十', '廿一', '廿二', '廿三', '廿四', '廿五', '廿六', '廿七', '廿八', '廿九', '三十']

/** 农历月 → 中文（如“农历八月”） */
export function lunarMonthText(m: number): string {
  return `农历${LUNAR_MONTHS[m] || m}月`
}

/** 农历月/日 → 中文（如“农历八月十五”/“农历闰八月十五”） */
export function lunarDayText(m: number, d: number, isLeap = false): string {
  return `农历${isLeap ? '闰' : ''}${LUNAR_MONTHS[m] || m}月${LUNAR_DAYS[d] || d}`
}