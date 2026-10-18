// 库内时间串的解析口径测试（node 内置 runner，零依赖）。
//
// 守的是一条跨端契约：服务端写 UTC（schema datetime('now') / store FormatDBTime），
// 客户端必须按 UTC 解析后再按本地时区展示。历史上 fmtDateTime 直接把 DB 串交给
// `new Date('YYYY-MM-DDTHH:mm:ss')`（无时区后缀按本地解析），UTC+8 用户看到的时间
// 整体慢 8 小时——这里用固定时区把该口径钉死。
//
// node --test 每个文件独立进程，文件内改 TZ 不影响其它用例文件。
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { fmtDate, fmtDateTime, parseDBTime } from './time.ts'

// 东八区：UTC 存储 15:16 → 本地应显示 23:16（修复前显示 15:16，即 Bug 形态）
process.env.TZ = 'Asia/Shanghai'

test('fmtDateTime：UTC 存储串按本地时区渲染', () => {
  assert.equal(fmtDateTime('2026-10-17 15:16:22'), '2026-10-17 · 23:16')
  assert.equal(fmtDateTime('2026-10-17T15:16:22Z'), '2026-10-17 · 23:16') // 已带 Z 不重复补
  // 历史 RFC3339 行自带显式偏移：按其自身偏移解析，东八区下即 15:16
  assert.equal(fmtDateTime('2026-10-17T15:16:22+08:00'), '2026-10-17 · 15:16')
})

test('fmtDateTime：UTC 晚于本地零点的串跨到次日', () => {
  assert.equal(fmtDateTime('2026-10-17 23:30:00'), '2026-10-18 · 07:30')
})

test('fmtDate：日期展示同样按本地时区（跨天归次日）', () => {
  assert.equal(fmtDate('2026-10-17 15:16:22'), '2026-10-17')
  assert.equal(fmtDate('2026-10-17 23:30:00'), '2026-10-18')
})

test('parseDBTime 与兜底：instant 正确、异常串返回 null / 原样降级', () => {
  assert.equal(parseDBTime('2026-10-17 15:16:22')?.toISOString(), '2026-10-17T15:16:22.000Z')
  assert.equal(parseDBTime('2026-10-17T15:16:22Z')?.toISOString(), '2026-10-17T15:16:22.000Z')
  assert.equal(parseDBTime(''), null)
  assert.equal(parseDBTime('not-a-date'), null)
  // 解析失败不抛异常：降级为原串切片（老口径），不因格式异常清空列表
  assert.equal(fmtDateTime('not-a-date'), 'not-a-date')
  assert.equal(fmtDate(''), '')
})
