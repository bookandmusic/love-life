// 强调色（暖色系预设色板）——外观 → 强调色
// 深浅模式各配主粉 + 深档粉；深档粉承接文字/图标对比度（WCAG AA 优先）
export interface AccentDef {
  label: string
  light: { primary: string; deep: string }
  dark: { primary: string; deep: string }
}

export const ACCENTS: Record<string, AccentDef> = {
  rose: { label: '柔雾粉', light: { primary: '#ff8fab', deep: '#f06292' }, dark: { primary: '#ff8fab', deep: '#ffb4c2' } },
  peach: { label: '蜜桃橙', light: { primary: '#ff9f7a', deep: '#e35f3d' }, dark: { primary: '#ff9d82', deep: '#ffc4a6' } },
  gold: { label: '落日金', light: { primary: '#f2b441', deep: '#cf8a1c' }, dark: { primary: '#f2b441', deep: '#ffd98b' } },
  dousha: { label: '豆沙红', light: { primary: '#e0786e', deep: '#b94d4d' }, dark: { primary: '#e0877f', deep: '#f0b0a9' } },
}

export const ACCENT_IDS = Object.keys(ACCENTS)