// CSV 导出工具：纯 TS，无 Vue / 无 DOM，便于被 .verify 脚本直接引入做回归。
// 公式注入防御：审计日志 params 含 AI 生成内容，属不可信输入。Excel / WPS 会把首字符为
// = + - @ 或制表符 / 回车的单元格当公式执行，故给这类值加 ' 前缀，使其按文本呈现。
// 校验：node .verify/admin-csv.mjs
export function csvCell(v: unknown): string {
  const s = String(v)
  return /^[=+\-@\t\r]/.test(s) ? `'${s}` : s
}

/**
 * 行 → CSV 文本：BOM 开头（Excel 直接打开不乱码），每格双引号包裹、
 * 内部 " 翻倍转义，格间 , 分隔，行间 \r\n 分隔。
 * 顺序与既有实现一致：先公式注入防御（csvCell），再引号转义。
 */
export function toCsv(header: string[], rows: unknown[][]): string {
  return (
    '\uFEFF' +
    [header, ...rows].map((r) => r.map((c) => `"${csvCell(c).replaceAll('"', '""')}"`).join(',')).join('\r\n')
  )
}
