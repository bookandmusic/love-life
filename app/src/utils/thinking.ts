// 思考区的分段与秒数口径（纯函数）。
//
// 为什么单独成模块：这套口径有**两处调用**（流式逐帧增量 / 历史整条 part），
// 而「两侧同一算法」正是它的全部要害——散在组件里改一处漏一处就会退化成
// 「刷新后与流式期间形态不一致」。纯 TS 模块可在现有设施下单测（node 内置 runner，
// 见 thinking.test.mjs），.vue 单文件组件则不能。
//
// 口径正文见 AI模式设计「过程收起（客户端渲染口径）」。

/** 轮与轮之间的分隔：一轮内逐字原样累积，轮间空一行。 */
const THINK_SEP = '\n\n'

/** 折叠条秒数下限：服务端落的是下取整，不足 1 秒会落 0，而模板把 0 当空值渲染成「已思考 」。 */
const MIN_THINK_SECONDS = 1

/** 追加一段思考文本。continuing = 仍属同一轮（不补空行），否则视为新一轮。 */
export function appendThink(prev: string, chunk: string, continuing: boolean): string {
  if (!chunk) return prev
  return prev && !continuing ? `${prev}${THINK_SEP}${chunk}` : `${prev}${chunk}`
}

/**
 * 各轮秒数之和 → 折叠条显示的秒数。
 *
 * 入参必须是**原值之和**：下限只在求和后抬这一次。逐条抬会让轮1 的 0 变成 1 再加轮2，
 * 与另一侧对不上（服务端落库值不动，两侧求和后才比较）。
 */
export function displayThinkSeconds(rawSum: number): number {
  return Math.max(MIN_THINK_SECONDS, rawSum)
}
