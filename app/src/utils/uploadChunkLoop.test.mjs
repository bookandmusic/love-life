// 分片上传循环的收敛性测试（node 内置 runner，零依赖）。
//
// 跑的是**真实实现** uploadChunkLoop.ts（不是副本——副本会随实现漂移；
// 它不 import api/client，故 node 可直接加载）。
//
// 分工（照真实结构）：**片内重试在传输层**（uploadMedia.ts 的 sendChunk），
// 循环只管推进与重同步。故这里的假传输要把两种语义都模拟出来：
//   - 瞬时失败（网络抖）→ 传输层自己重试后返回正常响应
//   - 永久失败 → 传输层抛 4xx，循环直接上抛（不重试）
//
// 守循环的收敛性：三种剧本下请求次数必须有界且最终拿到 token。
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { runChunkLoop } from './uploadChunkLoop.ts'

const CHUNK = 1024

/** 造一个只带 size 的假 File（循环只用 file.size 判断片边界） */
function fakeFile(size) {
  return { size }
}

/**
 * 假服务端 + 假传输。
 * 传输层语义：对可重试错误（本模拟用 `retriable: true`）在内部重试；
 * 对 409 直接上抛（循环负责重同步）；对其它 4xx 上抛。
 * 调用计数 `calls` 记的是**循环发起的次数**（不含传输层内部重试），
 * 另用 `wireCalls` 记线上实际请求数。
 */
function makeTransport(opts = {}) {
  const state = { received: 0, uploadId: 'up1', finalized: false, calls: [], wireCalls: 0 }
  const { loseFinalOnce = false, mismatchBeforeFinal = false, retriableOnce = false, alwaysFail = false } = opts
  let lostFinal = false
  let mismatchLeft = mismatchBeforeFinal ? 1 : 0
  let retriableLeft = retriableOnce ? 1 : 0

  async function once(file, offset, isFinal) {
    state.wireCalls++
    if (alwaysFail) throw new Error('network')
    if (retriableLeft > 0) {
      retriableLeft--
      throw Object.assign(new Error('transient'), { retriable: true })
    }
    if (mismatchLeft > 0) {
      mismatchLeft--
      state.received = file.size
      throw Object.assign(new Error('offset mismatch'), { status: 409, received: state.received })
    }
    if (isFinal && state.finalized) return { media_token: 'tok-final' }
    if (offset !== state.received) {
      throw Object.assign(new Error('offset mismatch'), { status: 409, received: state.received })
    }
    const n = Math.min(CHUNK, file.size - offset)
    state.received = offset + n
    if (!isFinal) return { upload_id: state.uploadId, received: state.received }
    if (loseFinalOnce && !lostFinal) {
      lostFinal = true
      state.finalized = true
      throw Object.assign(new Error('response lost'), { retriable: true })
    }
    state.finalized = true
    return { media_token: 'tok-final' }
  }

  return {
    state,
    async sendChunk(file, offset, uploadId, isFinal) {
      state.calls.push({ offset, isFinal })
      // 传输层：可重试错误在此重试（最多 3 次尝试），其余直接上抛
      let lastErr
      for (let a = 0; a < 3; a++) {
        try {
          return await once(file, offset, isFinal)
        } catch (e) {
          lastErr = e
          if (!e.retriable) throw e
        }
      }
      throw lastErr
    },
  }
}

test('正常多片：请求次数 = 片数，拿到 token', async () => {
  const t = makeTransport()
  const out = await runChunkLoop(fakeFile(CHUNK * 3), t, CHUNK)
  assert.equal(out.media_token, 'tok-final')
  assert.equal(t.state.calls.length, 3)
})

test('首片即末片：单次请求直接完成（小文件路径）', async () => {
  const t = makeTransport()
  const out = await runChunkLoop(fakeFile(100), t, CHUNK)
  assert.equal(out.media_token, 'tok-final')
  assert.equal(t.state.calls.length, 1)
  assert.equal(t.state.calls[0].isFinal, true, '小文件必须一次请求即末片')
})

test('末片响应丢失：传输层重试命中服务端幂等，取回结果（请求有界）', async () => {
  const t = makeTransport({ loseFinalOnce: true })
  const out = await runChunkLoop(fakeFile(CHUNK * 2), t, CHUNK)
  assert.equal(out.media_token, 'tok-final')
  assert.equal(t.state.calls.length, 2, `循环只发 2 片，重试在传输层：${JSON.stringify(t.state.calls)}`)
  assert.ok(t.state.wireCalls <= 4, `线上请求应有界，实际 ${t.state.wireCalls}`)
})

test('瞬时网络抖：传输层重试后成功，循环不受影响', async () => {
  const t = makeTransport({ retriableOnce: true })
  const out = await runChunkLoop(fakeFile(CHUNK * 2), t, CHUNK)
  assert.equal(out.media_token, 'tok-final')
  assert.equal(t.state.calls.length, 2)
})

test('服务端已收齐（409 报 size）：重同步到末尾再发末片，不从头重传', async () => {
  const t = makeTransport({ mismatchBeforeFinal: true })
  const out = await runChunkLoop(fakeFile(CHUNK * 3), t, CHUNK)
  assert.equal(out.media_token, 'tok-final')
  const offsets = t.state.calls.map((c) => c.offset)
  assert.equal(offsets.filter((o) => o === 0).length, 1, `offset=0 只应出现在首次探测：${offsets}`)
  assert.ok(offsets.includes(CHUNK * 3), `应重同步到文件末尾：${offsets}`)
})

test('409 重同步：用服务端回的 received 推进（中间位置）', async () => {
  const t = makeTransport()
  t.state.received = CHUNK // 服务端已收一片但客户端不知道
  const out = await runChunkLoop(fakeFile(CHUNK * 3), t, CHUNK)
  assert.equal(out.media_token, 'tok-final')
  const offsets = t.state.calls.map((c) => c.offset)
  assert.equal(offsets[0], 0, '首片仍从 0 探测')
  assert.equal(offsets[1], CHUNK, `第二次应重同步到服务端已收位置：${offsets}`)
})

test('永久失败（4xx）：直接上抛，不无限重试', async () => {
  const t = makeTransport({ alwaysFail: true })
  await assert.rejects(() => runChunkLoop(fakeFile(CHUNK * 2), t, CHUNK), /network/)
  assert.equal(t.state.calls.length, 1, `循环只发一次即上抛，实际 ${t.state.calls.length}`)
})

test('新客户端 + 旧服务端：size 不符即报错，不静默截断', async () => {
  // 旧服务端不认分片头，把首片当完整文件收下并回 media_token（size=本片大小）。
  // 客户端若直接采信，20MB 视频只传了 8MB 还报成功——用户无从察觉。
  const size = CHUNK * 3
  const staleServer = {
    async sendChunk() {
      return { media_token: 'tok', kind: 'video', size: CHUNK }
    },
  }
  await assert.rejects(
    () => runChunkLoop(fakeFile(size), staleServer, CHUNK),
    /上传未完成/,
    '回执 size 小于文件必须报错，不得当成成功',
  )
})
