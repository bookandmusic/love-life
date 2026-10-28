// 按「最近一次使用时间」淘汰最旧条目的通用工具。
//
// 为什么单独成文件：调用方（媒体签名注册表）依赖 vue / localStorage，node 测试里
// 起不来；把纯计算拆出来才能用零依赖的 node --test 守住边界。
// 与 chatCache 的 pickEvictions 同一思路（按写入时间淘汰最旧），此处按 lastSeen。

export interface Evictable {
  lastSeen: number
}

/**
 * 挑出需要淘汰的键：表超过 max 时，按 lastSeen 从旧到新丢，直到回到 max。
 * 返回空数组表示无需淘汰。
 */
export function pickEvictions<K>(
  entries: Iterable<[K, Evictable]>,
  max: number,
): K[] {
  const all = [...entries]
  if (all.length <= max) return []
  const overflow = all.length - max
  return all
    .sort((a, b) => a[1].lastSeen - b[1].lastSeen)
    .slice(0, overflow)
    .map(([k]) => k)
}
