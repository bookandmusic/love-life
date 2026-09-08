// 本地存储键集中管理（阶段5.4 B18：`love-life-*` 键散落 4 个文件，统一在此维护）
export const KEYS = {
  BASE: 'love-life-base',
  BASE_LAN: 'love-life-base-lan',
  BASE_WAN: 'love-life-base-wan',
  TOKEN: 'love-life-token',
  REFRESH: 'love-life-refresh',
  THEME: 'love-life-theme',
  ACCENT: 'love-life-accent',
  MODE: 'love-life-mode',
  WINDOW_STATE: 'love-life-window-state',
} as const

export function getItem(key: string): string | null {
  return localStorage.getItem(key)
}

export function setItem(key: string, value: string) {
  localStorage.setItem(key, value)
}

export function removeItem(key: string) {
  localStorage.removeItem(key)
}