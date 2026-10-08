// 思考区分段与秒数口径的不变量测试（node 内置 runner，零依赖）。
//
// 守的是「流式与回显同一算法」这条口径本身：分段规则、下限只抬一次。
// 这两条一旦被改坏，表现是「刷新前/后不一样」——.vue 组件没有单测设施，故口径抽到
// thinking.ts 由这里守住（AI模式设计「过程收起（客户端渲染口径）」）。
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { appendThink, displayThinkSeconds } from './thinking.ts'

test('轮内逐帧原样累积：不插分隔', () => {
  // 原生流式每帧 1 字，逐帧累积必须还原成整段（逐帧插空行会变成「一字一行」）
  let s = ''
  for (const ch of '用户说') s = appendThink(s, ch, true)
  assert.equal(s, '用户说')
})

test('轮与轮之间恰一个空行，且轮内已有换行不误判', () => {
  // 轮1：两帧；轮2：首帧 continuing=false → 补一个空行；轮2 后续帧不补
  let s = ''
  s = appendThink(s, '第一轮', true)
  s = appendThink(s, '思考', true)
  s = appendThink(s, '第二轮', false)
  s = appendThink(s, '思考', true)
  assert.equal(s, '第一轮思考\n\n第二轮思考')
  // 再起一轮仍恰一个空行（不是两个、也不粘连）
  assert.equal(appendThink(s, '第三轮', false), '第一轮思考\n\n第二轮思考\n\n第三轮')
})

test('空增量不改写已有文本（不产生悬空分隔）', () => {
  assert.equal(appendThink('已有', '', false), '已有')
  assert.equal(appendThink('', '', false), '')
  assert.equal(appendThink('', '首段', false), '首段')
})

test('秒数下限只在求和后抬一次：逐条抬会与另一侧对不上', () => {
  // 回显侧：轮1 落 0、轮2 落 4 → 求和 4（若逐条抬成 1+4=5 就与服务端不符）
  assert.equal(displayThinkSeconds(0 + 4), 4)
  // 流式侧同理：各轮 thinking.end 的 seconds 之和
  assert.equal(displayThinkSeconds(4 + 3), 7)
  // 全为 0（思考不足 1 秒就结束 / 中途被打断未发 thinking.end）→ 抬到 1，折叠条不空着
  assert.equal(displayThinkSeconds(0), 1)
  assert.equal(displayThinkSeconds(0 + 0), 1)
})
