// 本地存储键集中管理（`love-life-*` 键散落 4 个文件，统一在此维护）
// 薄转发已删：调用处直用 localStorage，此处只留键常量
export const KEYS = {
  BASE: 'love-life-base',
  BASE_LAN: 'love-life-base-lan',
  BASE_WAN: 'love-life-base-wan',
  TOKEN: 'love-life-token',
  REFRESH: 'love-life-refresh',
  THEME: 'love-life-theme',
  ACCENT: 'love-life-accent',
  MODE: 'love-life-mode',
  LAST_ROUTE: 'love-life-last-route', // 传统模式记住上次浏览的一级 tab
  WINDOW_STATE: 'love-life-window-state',
  // 长按手势的一次性提示（跨页面共享，本地记住后不再打扰）
  HINT_LONGPRESS: 'love-life-hint-longpress-del',
  // 应用内更新：GitHub 代理列表（JSON 数组，仅存自定义项，内置项恒在）与「稍后」记忆的 versionCode
  GH_PROXIES: 'love-life-gh-proxies',
  DISMISSED_UPDATE: 'love-life-dismissed-update',
} as const

/** 首次调用返回 true 并记住；之后（或隐私模式下不可写）返回 false */
export function onceHint(key: string): boolean {
  try {
    if (localStorage.getItem(key)) return false
    localStorage.setItem(key, '1')
    return true
  } catch {
    return false
  }
}