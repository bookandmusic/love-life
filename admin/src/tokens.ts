/**
 * 后台视觉令牌（唯一来源）：颜色、间距、圆角、字号、行高、阴影、布局尺寸。
 *
 * 用法：
 * - 组件样式写 `var(--color-ink)` 这类 CSS 变量，App.vue 在启动与明暗切换时写入 :root；
 * - Naive UI 的主题覆盖由主题设置生成（store/modules/theme/shared.ts 的 getNaiveTheme），
 *   主色与下面的 primary 同值，改主色两处同步。
 *
 * 守卫：scripts/guard-styles.mjs 以 buildCssVars 的键为白名单（未声明即失败），
 * 并反向校验每个键至少被一处 var(--x) 引用。
 */

/** 表面与文字阶梯：浆果红主题（深墨蓝侧栏 + 暖纸工作区，与 APP 品牌粉同源） */
export const color = {
  canvas: '#ffffff',
  subtle: '#f3f1ed',
  border: '#e7e4de',
  ink: '#1f2328',
  inkSecondary: '#5b6167',
  inkTertiary: '#9aa0a6',
  primary: '#b23a6f',
  primaryHover: '#c45082',
  primaryContrast: '#ffffff',
  success: '#18a058',
  successBg: '#e8f7ef',
  warning: '#f59e0b',
  warningBg: '#fdf3e1',
  error: '#e5484d',
  errorBg: '#fdebec',
  info: '#2e90fa',
  infoBg: '#e9f4fe',
} as const

/** 间距：4 的倍数，1..8 档 */
export const space = {
  1: '4px',
  2: '8px',
  3: '12px',
  4: '16px',
  5: '20px',
  6: '24px',
  7: '32px',
  8: '40px',
} as const

/** 圆角：Notion 的 xs4/sm6/md8/lg12/xl16 */
export const radius = {
  xs: '4px',
  sm: '8px',
  md: '8px',
  lg: '12px',
  xl: '16px',
} as const

/** 字号阶梯：后台是密集操作界面，正文 13，标题不超过 20 */
export const fontSize = {
  xs: '11px',
  sm: '12px',
  base: '13px',
  md: '14px',
  lg: '16px',
  xl: '20px',
  '2xl': '28px',
} as const

export const lineHeight = {
  tight: '1.25',
  normal: '1.5',
  relaxed: '1.7',
} as const

export const shadow = {
  sm: '0 1px 2px rgba(0, 0, 0, 0.04)',
  md: '0 8px 24px rgba(0, 0, 0, 0.08)',
} as const

/** 过渡：单一节奏，不做弹性动效（后台是密集操作界面） */
export const transition = {
  fast: '150ms ease',
} as const

export const layout = {
  controlSm: '160px',
  controlMd: '220px',
} as const

/** 深色令牌：跟随模板主题抽屉的暗黑模式（容器/工作区与 theme.tokens.dark 同源） */
export const colorDark = {
  canvas: '#1c1c1e',
  subtle: '#26262a',
  border: '#3a3a3f',
  ink: '#e8e6e3',
  inkSecondary: '#a8a49e',
  inkTertiary: '#6f6b66',
  primary: '#d4558f',
  primaryHover: '#e06da2',
  primaryContrast: '#ffffff',
  success: '#3dd68c',
  successBg: '#173425',
  warning: '#fbb03b',
  warningBg: '#3a2a12',
  error: '#ff6b70',
  errorBg: '#3d1c1e',
  info: '#4cb0f8',
  infoBg: '#12283a',
} as const

export const fontFamily = {
  sans: "'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'PingFang SC', 'Microsoft YaHei', sans-serif",
  mono: "ui-monospace, SFMono-Regular, 'SF Mono', Menlo, Consolas, monospace",
} as const

/**
 * 注入到 :root 的 CSS 变量。键名即组件里可用的 `var(--…)`，守卫脚本按此白名单比对。
 * 新增变量只改这里，不要绕开写裸值。色彩部分按明暗两套切换，其余令牌共用。
 */
export function buildCssVars(dark: boolean): Record<string, string> {
  const c = dark ? colorDark : color;
  return {
    '--color-canvas': c.canvas,
    '--color-subtle': c.subtle,
    '--color-border': c.border,
    '--color-ink': c.ink,
    '--color-ink-secondary': c.inkSecondary,
    '--color-ink-tertiary': c.inkTertiary,
    '--color-primary-hover': c.primaryHover,
    '--color-success': c.success,
    '--color-success-bg': c.successBg,
    '--color-warning': c.warning,
    '--color-warning-bg': c.warningBg,
    '--color-error': c.error,
    '--color-error-bg': c.errorBg,
    '--color-info': c.info,
  '--space-1': space[1],
  '--space-2': space[2],
  '--space-3': space[3],
  '--space-4': space[4],
  '--space-5': space[5],
  '--space-6': space[6],
  '--space-7': space[7],
  '--space-8': space[8],
  '--radius-xs': radius.xs,
  '--radius-sm': radius.sm,
  '--radius-md': radius.md,
  '--radius-lg': radius.lg,
  '--radius-xl': radius.xl,
  '--text-xs': fontSize.xs,
  '--text-sm': fontSize.sm,
  '--text-base': fontSize.base,
  '--text-md': fontSize.md,
  '--text-2xl': fontSize['2xl'],
  '--leading-tight': lineHeight.tight,
  '--leading-relaxed': lineHeight.relaxed,
  '--shadow-sm': shadow.sm,
  '--transition': transition.fast,
  '--font-mono': fontFamily.mono,
    '--control-sm': layout.controlSm,
    '--control-md': layout.controlMd,
  };
}

/** 把令牌写成 :root 上的 CSS 变量；启动时注入一次，主题明暗切换时随 theme store 重注入 */
export function injectCssVars(dark = false): void {
  const root = document.documentElement;
  for (const [name, value] of Object.entries(buildCssVars(dark))) {
    root.style.setProperty(name, value);
  }
}
