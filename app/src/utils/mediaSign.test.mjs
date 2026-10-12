// 签名媒体地址解析口径的不变量测试（node 内置 runner，零依赖）。
//
// 守两条最容易写错、坏了就黑屏的口径（客户端设计「签名与重签」）：
//   ① 重签的键必须从 URL 路径解析——结果集记录的 id 是**记录自己的 id**
//      （动态记录给的是动态 id），拿它去重签会换错对象；
//   ② 过期地址（历史回显读的库内快照）与「只有 thumb」都要在注册时立刻重签，
//      否则要等下一个 5 分钟周期，期间缩略图与查看器一直黑屏。
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { mediaIdFromUrl, mediaIdOf, needsResign, rawOrThumb, signedExpMs } from './mediaSign.ts'

const URL_42 = '/api/v1/media/file/42?v=thumb&exp=1800000000&sig=abc'

test('媒体 id 从 URL 路径解析，而不是取记录自身的 id', () => {
  assert.equal(mediaIdFromUrl(URL_42), 42)
  assert.equal(mediaIdFromUrl('/api/v1/media/file/7?v=raw&exp=1&sig=x'), 7)
  // 非媒体地址（动态/相册封面走别的路径）解析不出，交由调用方退回记录 id
  assert.equal(mediaIdFromUrl('/api/v1/avatar/3?exp=1&sig=x'), undefined)
  assert.equal(mediaIdFromUrl(undefined), undefined)
  assert.equal(mediaIdFromUrl(''), undefined)
})

test('mediaIdOf 优先用 URL 路径的媒体 id，解析不出才退回记录 id', () => {
  // 关键：动态记录 id=7 而缩略图指向媒体 42 —— 必须取 42，否则重签错对象
  assert.equal(mediaIdOf({ id: 7, thumb_url: URL_42 }), 42)
  // 缩略图地址不是媒体路径（如相册封面）→ 退回记录 id
  assert.equal(mediaIdOf({ id: 7, thumb_url: '/api/v1/avatar/3?exp=1&sig=x' }), 7)
  // url 字段也参与解析（查看器条目可能只有 url）
  assert.equal(mediaIdOf({ url: URL_42 }), 42)
  assert.equal(mediaIdOf({}), undefined)
  assert.equal(mediaIdOf({ id: null }), undefined)
})

test('过期时刻按秒解析成毫秒；无 exp 视为未知', () => {
  assert.equal(signedExpMs(URL_42), 1800000000 * 1000)
  assert.equal(signedExpMs('/api/v1/media/file/42?v=thumb'), undefined)
  assert.equal(signedExpMs(undefined), undefined)
})

test('已过期的签名地址必须立刻重签（历史回显的库内快照）', () => {
  // exp=1800000000（2027-01）之后才过期 → 以它为界两侧各测一次
  const before = 1800000000 * 1000 - 1
  const after = 1800000000 * 1000 + 1
  assert.equal(needsResign({ thumb_url: URL_42, url: URL_42 }, before), false)
  assert.equal(needsResign({ thumb_url: URL_42, url: URL_42 }, after), true)
  // 边界：恰好到期即算过期（服务端 VerifySig 用 now > exp，故 exp 当秒仍可用；
  // 这里取保守侧，多换一次签名无副作用）
  assert.equal(needsResign({ thumb_url: URL_42, url: URL_42 }, 1800000000 * 1000), true)
})

test('只有 thumb、没有 raw 时必须立刻重签（v=thumb 换不出有效 raw）', () => {
  // 签名覆盖 variant，把 v=thumb 改成 v=raw 会验签失败（必然 401）——
  // 故这种记录不能等定时重签，注册即换新以补出真实 raw 地址
  assert.equal(needsResign({ thumb_url: URL_42 }, 0), true)
  // 有 raw 且未过期 → 不重签（避免结果卡每次渲染都打一次 reurl）
  assert.equal(needsResign({ thumb_url: URL_42, url: URL_42 }, 0), false)
  // 无任何地址：交给调用方，不在此判定
  assert.equal(needsResign({}, 0), true)
})

test('raw 缺省回落 thumb 本身，绝不改写成 v=raw', () => {
  const raw = '/api/v1/media/file/42?v=raw&exp=1&sig=deadbeef'
  assert.equal(rawOrThumb({ url: raw, thumb_url: URL_42 }), raw)
  // 关键：只有 thumb 时返回 thumb 原值（能打开），而不是 v=raw 的伪造地址（必 401）
  assert.equal(rawOrThumb({ thumb_url: URL_42 }), URL_42)
  assert.ok(!rawOrThumb({ thumb_url: URL_42 }).includes('v=raw'))
  assert.equal(rawOrThumb({}), '')
})
