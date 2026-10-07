// APP 侧农历口径测试（node 内置 runner，零依赖）。
//
// 周期表已是**单一数据源**（server/internal/lunar/lunar_table.json，Go 侧 go:embed、此处 import），
// 故不再需要「两份表逐项比对」的护栏；这里守的是**算法**：同一组黄金用例在 APP 侧也要得出同样结果。
// 用例与 server/internal/lunar/lunar_test.go 共用同一组公共数据。
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { leapMonth, leapDays, monthDays, nextLunarOccurrence } from './lunar.ts'

test('闰月与月长：2025 闰六月，2026 无闰', () => {
  assert.equal(leapMonth(2025), 6)
  assert.equal(leapDays(2025), 29) // 2025 闰六月为小月
  assert.equal(leapMonth(2026), 0)
  assert.equal(leapDays(2026), 0)
  assert.equal(monthDays(2026, 8), 29) // 2026 农历八月为小月（无三十）
})

test('农历→公历：与 Go 侧同一组黄金用例', () => {
  // 用 nextLunarOccurrence 从「该农历日前一天」起步，得到的就是当年那次 occurrence
  const cases = [
    { m: 8, d: 15, leap: false, from: '2026-01-01', want: '2026-09-25' }, // 中秋
    { m: 5, d: 15, leap: false, from: '1996-01-01', want: '1996-06-30' }, // fixtures 她的生日
    { m: 6, d: 1, leap: false, from: '2025-01-01', want: '2025-06-25' }, // 闰六月的平六月
    { m: 6, d: 1, leap: true, from: '2025-01-01', want: '2025-07-25' }, // 2025 闰六月
    { m: 1, d: 1, leap: false, from: '1900-01-31', want: '1900-01-31' }, // 表头基准日（含今天）
    { m: 12, d: 15, leap: false, from: '2026-01-01', want: '2027-01-22' }, // 腊月跨公历年
    { m: 11, d: 3, leap: false, from: '2026-01-01', want: '2026-12-11' }, // 冬月
    { m: 2, d: 1, leap: false, from: '2043-01-01', want: '2043-03-11' }, // 表中后段
  ]
  for (const c of cases) {
    assert.equal(
      nextLunarOccurrence(c.m, c.d, c.leap, c.from),
      c.want,
      `农历 ${c.m}-${c.d}${c.leap ? '(闰)' : ''} 自 ${c.from}`,
    )
  }
})

test('下一次农历日：滚动与闰月回退口径', () => {
  const cases = [
    { m: 8, d: 15, leap: false, from: '2026-06-01', want: '2026-09-25' }, // 当年内
    { m: 8, d: 15, leap: false, from: '2026-10-01', want: '2027-09-15' }, // 滚到次年
    { m: 12, d: 15, leap: false, from: '2027-06-01', want: '2028-01-11' }, // 腊月跨年次年
    { m: 6, d: 1, leap: false, from: '2025-07-01', want: '2026-07-14' }, // 平六月已过 → 次年平六月（不回退闰月）
    { m: 6, d: 1, leap: true, from: '2025-08-01', want: '2036-07-23' }, // 闰六月已过 → 下一次闰六月
    { m: 5, d: 1, leap: true, from: '2028-07-01', want: '2039-06-22' }, // 闰五月已过 → 下一次闰五月
    { m: 8, d: 15, leap: false, from: '2101-01-01', want: null }, // 超表尾返回 null
  ]
  for (const c of cases) {
    assert.equal(
      nextLunarOccurrence(c.m, c.d, c.leap, c.from),
      c.want,
      `下一次 ${c.m}-${c.d}${c.leap ? '(闰)' : ''} 自 ${c.from}`,
    )
  }
})

test('不存在的农历日不可换算（小月无三十 / 无闰月却传闰）', () => {
  // 2026 农历二月为小月（29 天）→ 「二月三十」不存在，应越过当年落到下一年的二月三十（若有）
  const got = nextLunarOccurrence(2, 30, false, '2026-01-01')
  assert.notEqual(got, '2026-03-29', '小月不应凭空算出「三十」')
  // 2026 无闰六月：传 leap=true 时不应落在 2026 年内
  const leap6 = nextLunarOccurrence(6, 1, true, '2026-01-01')
  assert.ok(leap6 === null || !leap6.startsWith('2026-'), `2026 无闰六月，不应返回当年：${leap6}`)
})
