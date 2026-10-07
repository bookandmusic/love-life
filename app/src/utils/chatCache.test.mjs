// 本地对话缓存上限与淘汰口径测试（node 内置 runner，零依赖）。
//
// 用最小 indexedDB 替身跑**真实的 saveCachedHistory / loadCachedHistory** 路径
// （不是测一个抽出来的纯函数）——要守的就是这条链路的行为：
//   ① 有容量上限，超出后最旧的被淘汰；
//   ② 淘汰只统计**本用户**的键（多账号共用一个 objectStore，误删别人既越权也丢数据）；
//   ③ 上限内的读写不受影响，旧形状条目仍可读回。
//
// 替身只实现本模块用到的那一小片 API：open→onsuccess、transaction、objectStore 的
// get/put/delete/openCursor、事务 oncomplete。够用即可，不追求规范化行为。
import { test, beforeEach } from 'node:test'
import assert from 'node:assert/strict'

/** 极简内存版 indexedDB：整个测试文件共用一个实例，每个用例前清空。 */
function installFakeIndexedDB() {
  const data = new Map()

  class FakeRequest {
    constructor() {
      this.result = undefined
      this.onsuccess = null
    }
    _succeed(v) {
      this.result = v
      queueMicrotask(() => this.onsuccess && this.onsuccess())
    }
  }

  function makeStore() {
    return {
      get: (k) => {
        const r = new FakeRequest()
        r._succeed(data.get(k))
        return r
      },
      put: (v, k) => {
        const r = new FakeRequest()
        data.set(k, v)
        r._succeed(k)
        return r
      },
      delete: (k) => {
        const r = new FakeRequest()
        data.delete(k)
        r._succeed(undefined)
        return r
      },
      openCursor: () => {
        const keys = [...data.keys()]
        let i = 0
        const r = new FakeRequest()
        const step = () => {
          if (i >= keys.length) {
            r.result = null
            queueMicrotask(() => r.onsuccess && r.onsuccess())
            return
          }
          const k = keys[i++]
          r.result = { key: k, value: data.get(k), continue: () => queueMicrotask(step) }
          queueMicrotask(() => r.onsuccess && r.onsuccess())
        }
        queueMicrotask(step)
        return r
      },
    }
  }

  function makeTx() {
    const tx = { oncomplete: null }
    queueMicrotask(() => tx.oncomplete && tx.oncomplete())
    return tx
  }

  globalThis.indexedDB = {
    open: () => {
      const r = new FakeRequest()
      const db = {
        objectStoreNames: { contains: () => true },
        createObjectStore: () => makeStore(),
        transaction: () => {
          const tx = makeTx()
          tx.objectStore = () => makeStore()
          return tx
        },
      }
      r._succeed(db)
      return r
    },
  }
  return data
}

const store = installFakeIndexedDB()
beforeEach(() => store.clear())

const { saveCachedHistory, loadCachedHistory, MAX_CACHED_SESSIONS: LIMIT } = await import('./chatCache.ts')

// 上限必须是有限正整数：为 0 / Infinity 都会让「有容量上限」这一要求落空
test('上限是有限正整数', () => {
  assert.ok(Number.isInteger(LIMIT), `上限必须是整数，实得 ${LIMIT}`)
  assert.ok(LIMIT > 0 && Number.isFinite(LIMIT), `上限必须是有限正整数，实得 ${LIMIT}`)
})

// 写入次数封顶：LIMIT 若被改成非有限值，"LIMIT + 5" 会让用例挂死而非失败（实测踩到过）
const bounded = (n) => Math.min(Number.isFinite(n) ? n : 0, 100)

test('超出上限后淘汰最旧的本用户条目', async () => {
  const uid = 1
  const total = bounded(LIMIT) + 5
  for (let i = 1; i <= total; i++) {
    await saveCachedHistory(uid, i, { n: i })
  }
  const keys = [...store.keys()].filter((k) => k.startsWith(`history:${uid}:`))
  assert.equal(keys.length, LIMIT, `本用户键数应收敛到上限 ${LIMIT}，实得 ${keys.length}`)
  // 第 1..5 条最旧，应已被淘汰
  for (let i = 1; i <= 5; i++) {
    assert.equal(await loadCachedHistory(uid, i), null, `第 ${i} 条（最旧）应已被淘汰`)
  }
  // 最后一条必须还在
  assert.deepEqual(await loadCachedHistory(uid, total), { n: total })
})

test('淘汰只统计本用户的键，不误删其他账号', async () => {
  const other = 999
  const mine = 7
  // 另一个账号先写入若干条（保持在上限内）
  for (let i = 1; i <= 5; i++) await saveCachedHistory(other, i, { other: i })
  // 本账号写满并超出上限（若实现按全表统计，会把 other 的键算进去并淘汰它们）
  for (let i = 1; i <= bounded(LIMIT) + 5; i++) await saveCachedHistory(mine, i, { mine: i })

  const otherKeys = [...store.keys()].filter((k) => k.startsWith(`history:${other}:`))
  assert.equal(otherKeys.length, 5, '另一个账号的缓存一条都不该被淘汰')
  for (let i = 1; i <= 5; i++) {
    assert.deepEqual(await loadCachedHistory(other, i), { other: i }, `另一个账号第 ${i} 条应完好`)
  }
  const mineKeys = [...store.keys()].filter((k) => k.startsWith(`history:${mine}:`))
  assert.equal(mineKeys.length, LIMIT, `本账号应被裁到上限 ${LIMIT}，实得 ${mineKeys.length}`)
})

test('上限内不受影响，且旧形状条目仍可读回', async () => {
  const uid = 42
  await saveCachedHistory(uid, 1, { hello: 'world' })
  assert.deepEqual(await loadCachedHistory(uid, 1), { hello: 'world' })
  assert.equal(await loadCachedHistory(uid, 2), null, '未写入的会话应返回 null')

  // 旧版本直接把 history 存进 value（无 savedAt 包装）——读路径必须兼容
  store.set(`history:${uid}:99`, ['legacy', 'shape'])
  assert.deepEqual(await loadCachedHistory(uid, 99), ['legacy', 'shape'], '旧形状条目应可读回')
})

test('indexedDB 不可用时读写都不抛出（隐私模式等场景不得影响主流程）', async () => {
  // dbPromise 是模块级缓存，故必须取一个**全新的模块实例**，否则会命中前面用例已打开的连接
  const real = globalThis.indexedDB
  globalThis.indexedDB = {
    open: () => {
      throw new Error('indexedDB 不可用')
    },
  }
  try {
    const fresh = await import('./chatCache.ts?unavailable')
    await assert.doesNotReject(() => fresh.saveCachedHistory(1, 1, { x: 1 }), '保存失败必须被吞掉')
    assert.equal(await fresh.loadCachedHistory(1, 1), null, '读失败应返回 null 而非抛出')
  } finally {
    globalThis.indexedDB = real
  }
})
