// 媒体签名注册表淘汰口径：守住「浏览过几千张后不会每 5 分钟重签全表」。
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { pickEvictions } from './lruEvict.ts'

function makeEntries(n, startAt = 0) {
  const m = new Map()
  for (let i = 0; i < n; i++) m.set(String(i), { lastSeen: startAt + i })
  return m
}

test('未超上限时不淘汰', () => {
  assert.deepEqual(pickEvictions(makeEntries(10), 10), [])
  assert.deepEqual(pickEvictions(makeEntries(5), 10), [])
})

test('超上限时按 lastSeen 从旧到新淘汰，数量恰为超出部分', () => {
  const evicted = pickEvictions(makeEntries(12), 10)
  assert.deepEqual(evicted, ['0', '1'], '最旧的两条应被淘汰')
})

test('大量超出：只保留最新的 max 条', () => {
  const entries = makeEntries(1000)
  const evicted = pickEvictions(entries, 500)
  assert.equal(evicted.length, 500)
  // 留下的是 500..999
  const kept = [...entries.keys()].filter((k) => !evicted.includes(k))
  assert.equal(kept.length, 500)
  assert.equal(kept[0], '500')
  assert.equal(kept[kept.length - 1], '999')
})

test('lastSeen 相同也稳定淘汰（不重复、不遗漏）', () => {
  const m = new Map()
  for (let i = 0; i < 5; i++) m.set(`k${i}`, { lastSeen: 0 })
  const evicted = pickEvictions(m, 2)
  assert.equal(evicted.length, 3)
  assert.equal(new Set(evicted).size, 3, '不应重复淘汰同一个键')
})

test('空表与零上限边界', () => {
  assert.deepEqual(pickEvictions(new Map(), 10), [])
  assert.equal(pickEvictions(makeEntries(3), 0).length, 3)
})
