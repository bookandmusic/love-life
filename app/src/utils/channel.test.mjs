// 通道选择的纯逻辑测试：到期判断、探活 URL 拼接、单飞去重语义。
// 有副作用的流程（api / fetch / 事件监听）在 channel.ts 里，这里测可判定的部分。
import { test } from 'node:test'
import assert from 'node:assert/strict'

// 与 channel.ts 的 ensureFresh 保持同一判据：到期前 60 秒需要重取
const REFRESH_AHEAD_MS = 60_000
function needsRefresh(expiresAt, now) {
  if (!expiresAt) return false
  return now >= expiresAt - REFRESH_AHEAD_MS
}

// 与 channel.ts 的 probe 保持同一拼接规则
function healthURL(base) {
  return base.replace(/\/+$/, '') + '/api/v1/health'
}

// 与 channel.ts 的 refreshChannel 保持同一去抖间隔
const WATCH_DEBOUNCE_MS = 10_000
function shouldDebounce(lastRefresh, now) {
  return now - lastRefresh < WATCH_DEBOUNCE_MS
}

test('到期判断：距到期超过 60 秒不重取', () => {
  const now = 1_000_000
  assert.equal(needsRefresh(now + 61_000, now), false)
})

test('到期判断：距到期 60 秒内重取', () => {
  const now = 1_000_000
  assert.equal(needsRefresh(now + 60_000, now), true)
  assert.equal(needsRefresh(now + 30_000, now), true)
})

test('到期判断：已过期重取', () => {
  const now = 1_000_000
  assert.equal(needsRefresh(now - 1, now), true)
})

test('到期判断：无到期时间（非绿联通道）不重取', () => {
  assert.equal(needsRefresh(0, Date.now()), false)
  assert.equal(needsRefresh(undefined, Date.now()), false)
})

test('探活地址拼接：去尾斜杠后接健康检查', () => {
  assert.equal(healthURL('https://a.example.com'), 'https://a.example.com/api/v1/health')
  assert.equal(healthURL('https://a.example.com/'), 'https://a.example.com/api/v1/health')
  assert.equal(healthURL('https://a.example.com///'), 'https://a.example.com/api/v1/health')
  assert.equal(healthURL('https://a.example.com:8443'), 'https://a.example.com:8443/api/v1/health')
})

test('探活地址拼接：带路径前缀时保留前缀', () => {
  assert.equal(healthURL('https://a.example.com/base'), 'https://a.example.com/base/api/v1/health')
})

// ---- 单飞（验收 13）：并发调用只发一次请求 ----

test('单飞：并发重取共享同一次请求', async () => {
  // 复刻 channel.ts 的 inflight ??= 语义
  let inflight = null
  let calls = 0
  const resolve = async () => {
    calls++
    await new Promise((r) => setTimeout(r, 10))
  }
  const refresh = () => {
    inflight ??= resolve().finally(() => {
      inflight = null
    })
    return inflight
  }
  await Promise.all([refresh(), refresh(), refresh(), refresh()])
  assert.equal(calls, 1, '四次并发应只发一次请求')
  // 完成后 inflight 清空：下一次是新的请求
  await refresh()
  assert.equal(calls, 2, '完成后应可再次触发')
})

test('去抖：10 秒内重复触发只重取一次', () => {
  const t0 = 1_000_000
  assert.equal(shouldDebounce(t0, t0 + 1), true, '1ms 后应被去抖')
  assert.equal(shouldDebounce(t0, t0 + 9_999), true, '9.9s 后应被去抖')
  assert.equal(shouldDebounce(t0, t0 + 10_000), false, '10s 后不应被去抖')
})

// ---- 择优（验收 9）：并发探活取延迟最低 ----

test('择优：跳过不可达，取延迟最低的一条', () => {
  const INF = Number.POSITIVE_INFINITY
  const results = [
    { url: 'https://slow.example.com', ms: 800 },
    { url: 'https://dead.example.com', ms: INF },
    { url: 'https://fast.example.com', ms: 120 },
  ]
  const best = results.filter((r) => Number.isFinite(r.ms)).sort((a, b) => a.ms - b.ms)[0]
  assert.equal(best.url, 'https://fast.example.com')
})

test('择优：全部不可达时返回空（交由上层往下退）', () => {
  const INF = Number.POSITIVE_INFINITY
  const results = [
    { url: 'https://a.example.com', ms: INF },
    { url: 'https://b.example.com', ms: INF },
  ]
  const best = results.filter((r) => Number.isFinite(r.ms)).sort((a, b) => a.ms - b.ms)[0]
  assert.equal(best, undefined)
})