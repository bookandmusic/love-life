// 提醒天数取值口径的用例：守住「AI 设 45 天后回显不空白」这个真实缺陷。
import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  REMIND_PRESETS,
  REMIND_CUSTOM,
  REMIND_MIN,
  REMIND_MAX,
  remindSelection,
  clampRemindDays,
  resolveRemindDays,
  remindOptionLabel,
} from './remindDays.ts'

test('预设值原样识别为对应档', () => {
  for (const p of REMIND_PRESETS) {
    const s = remindSelection(p)
    assert.equal(s.preset, p, `预设 ${p} 应命中`)
    assert.equal(s.custom, p)
  }
})

test('非预设值（AI 可设的 45）落自定义并回填数值', () => {
  const s = remindSelection(45)
  assert.equal(s.preset, null, '45 不在预设内，应走自定义')
  assert.equal(s.custom, 45, '自定义框必须回填 45，否则界面显示空白')
})

test('边界 0 与 90：0 是预设（当天），90 是自定义', () => {
  assert.equal(remindSelection(0).preset, 0)
  assert.equal(remindSelection(REMIND_MAX).preset, null)
  assert.equal(remindSelection(REMIND_MAX).custom, REMIND_MAX)
})

test('越界夹紧到 0-90', () => {
  assert.equal(clampRemindDays(-5), REMIND_MIN)
  assert.equal(clampRemindDays(120), REMIND_MAX)
  assert.equal(clampRemindDays(45.6), 46, '小数四舍五入')
})

test('非法值回退到默认 1 天', () => {
  assert.equal(clampRemindDays(Number.NaN), 1)
  assert.equal(clampRemindDays(Number.POSITIVE_INFINITY), 1)
})

test('resolveRemindDays：预设直接用选中值，自定义用输入值', () => {
  assert.equal(resolveRemindDays(7, 45), 7, '选预设时忽略输入框')
  assert.equal(resolveRemindDays(REMIND_CUSTOM, 45), 45)
  assert.equal(resolveRemindDays(REMIND_CUSTOM, 999), REMIND_MAX, '自定义也要夹紧')
  assert.equal(resolveRemindDays(REMIND_CUSTOM, Number.NaN), 1)
})

test('选项文案：0 说「当天」，其余说「提前 N 天」', () => {
  assert.equal(remindOptionLabel(0), '当天提醒')
  assert.equal(remindOptionLabel(1), '提前 1 天提醒')
  assert.equal(remindOptionLabel(90), '提前 90 天提醒')
})

test('自定义哨兵不与任何合法天数冲突', () => {
  assert.ok(REMIND_CUSTOM < REMIND_MIN, '哨兵必须落在合法范围外')
  assert.ok(!REMIND_PRESETS.includes(REMIND_CUSTOM))
})
