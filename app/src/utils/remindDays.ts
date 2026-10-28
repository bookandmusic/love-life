// 纪念日提醒天数的取值口径（PRODUCT「纪念日」：0-90 天，默认 1 天；0 = 当天）。
//
// 为什么需要这层：后端接受 0-90 任意整数，AI 工具也能直接设 45 天。
// 下拉若只列固定档位，45 找不到匹配 option，浏览器会渲染成空白——用户看到「没选」。
// 故拆成「常用档 + 自定义」：预设命中就选档，否则落自定义并回填数值。
export const REMIND_PRESETS = [0, 1, 3, 7, 14, 30] as const

/** 提醒天数的合法范围（含两端），与 store.MaxRemindDaysBefore 同口径 */
export const REMIND_MIN = 0
export const REMIND_MAX = 90

/** 自定义档在下拉里的哨兵值（不与任何合法天数冲突，因为合法值都 >= 0） */
export const REMIND_CUSTOM = -1

export interface RemindSelection {
  /** 命中的预设档；未命中（需走自定义输入）时为 null */
  preset: number | null
  /** 回填到数字框的值（预设命中时也带上，切到自定义时不必重输） */
  custom: number
}

/**
 * 把库里的天数映射成下拉选择状态。
 * 非整数 / 非有限值 / 越界一律夹紧到合法范围——历史数据或异常输入不该让界面卡住。
 */
export function remindSelection(value: number): RemindSelection {
  const n = clampRemindDays(value)
  const hit = REMIND_PRESETS.find((p) => p === n)
  return { preset: hit ?? null, custom: n }
}

/** 夹紧到 0-90 的整数；NaN / Infinity / 小数按四舍五入后夹紧处理 */
export function clampRemindDays(n: number): number {
  if (!Number.isFinite(n)) return 1 // 默认 1 天（PRODUCT 口径）
  return Math.min(REMIND_MAX, Math.max(REMIND_MIN, Math.round(n)))
}

/**
 * 下拉选中值 → 最终天数。选中「自定义」时用输入框的值（已夹紧）。
 * 这样模板里只绑一个 select 值即可，不必在提交处再分支。
 */
export function resolveRemindDays(selected: number, custom: number): number {
  return selected === REMIND_CUSTOM ? clampRemindDays(custom) : selected
}

/** 下拉选项文案；0 单独说「当天」，其余说「提前 N 天」 */
export function remindOptionLabel(days: number): string {
  return days === 0 ? '当天提醒' : `提前 ${days} 天提醒`
}
