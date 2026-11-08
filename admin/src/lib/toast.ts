/**
 * 全局轻提示（后台统一反馈通道）：成功/失败/信息。
 * 实现走模板的 Naive UI message（AppProvider 注册的 window.$message）。
 * 对外四个方法：success / error / info / warning；错误提示停留更久。
 */
type Kind = 'success' | 'error' | 'info' | 'warning'

function push(kind: Kind, message: string): void {
  window.$message?.[kind](message, {
    duration: kind === 'error' ? 4200 : 2600,
    closable: true
  });
}

export const toast = {
  success: (message: string) => push('success', message),
  error: (message: string) => push('error', message),
  info: (message: string) => push('info', message),
  warning: (message: string) => push('warning', message)
};
