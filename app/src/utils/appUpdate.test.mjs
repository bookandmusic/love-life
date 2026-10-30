// 应用内更新纯逻辑测试：服务端清单解析、版本换算、强更与「稍后」记忆、体积人性化。
import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  bodyToMeta,
  getDismissedVersionCode,
  humanBytes,
  isForceUpdate,
  isUpdateAvailable,
  parseRelease,
  setDismissedVersionCode,
  UPDATE_LATEST_PATH,
} from './appUpdate.ts'

// node 环境无 localStorage：挂内存实现（先例：chatCache.test.mjs 挂 globalThis.indexedDB）
const memStore = new Map()
globalThis.localStorage = {
  getItem: (k) => (memStore.has(k) ? memStore.get(k) : null),
  setItem: (k, v) => memStore.set(k, String(v)),
  removeItem: (k) => memStore.delete(k),
  clear: () => memStore.clear(),
}

// 服务端 /api/v1/app/update/latest 的典型响应
const SERVER_OK = {
  available: true,
  version_name: '0.1.4',
  version_code: 104,
  notes: '### 更新内容\n- 修了某个问题\n\nmin-version-code: 10200\n',
  min_version_code: 10200,
  asset_name: 'love-life-0.1.4-app-universal-release.apk',
  asset_size: 48338417,
  download_path: '/api/v1/app/update/download',
}

test('清单解析：从服务端响应取出发布信息', () => {
  const r = parseRelease(SERVER_OK)
  assert.equal(r.versionName, '0.1.4')
  assert.equal(r.versionCode, 104)
  assert.equal(r.fileName, 'love-life-0.1.4-app-universal-release.apk')
  assert.equal(r.sizeBytes, 48338417)
  assert.equal(r.minVersionCode, 10200)
  // 下载地址是相对路径；调用处拼上当前 base
  assert.equal(r.downloadUrl, '/api/v1/app/update/download')
})

test('清单解析：available:false 转成可读错误', () => {
  assert.throws(() => parseRelease({ available: false, reason: '暂时无法获取更新信息' }), /无法获取更新信息/)
  assert.throws(() => parseRelease({ available: false }), /暂时无法获取更新信息/)
})

test('清单解析：缺版本 / 缺凭据 / 非对象都报明确错误', () => {
  assert.throws(() => parseRelease(null), /不是对象/)
  assert.throws(() => parseRelease({ available: true, asset_name: 'a.apk', download_token: 't' }), /缺少版本信息/)
  assert.throws(() => parseRelease({ available: true, version_name: '0.1.4', version_code: 104 }), /缺少下载地址/)
  assert.throws(
    () => parseRelease({ available: true, version_name: '0.1.4', version_code: 104, download_path: '/d' }),
    /缺少下载地址/
  )
})

test('body 约定行：min-version-code 提取且不进 notes；缺省为 0', () => {
  const got = bodyToMeta('### 内容\n- 一条\nmin-version-code: 105\n- 两条')
  assert.equal(got.minVersionCode, 105)
  assert.deepEqual(got.notes, ['### 内容', '- 一条', '- 两条'])
  const none = bodyToMeta('没有约定行\n')
  assert.equal(none.minVersionCode, 0)
  assert.deepEqual(none.notes, ['没有约定行'])
})

test('强更门槛：服务端字段优先，缺失时回落到说明里的约定行', () => {
  const withField = parseRelease({ ...SERVER_OK, min_version_code: 200 })
  assert.equal(withField.minVersionCode, 200)
  const fallback = parseRelease({ ...SERVER_OK, min_version_code: 0 })
  assert.equal(fallback.minVersionCode, 10200) // 来自 notes 里的 min-version-code
})

test('检查更新的请求路径是相对业务 base 的', () => {
  assert.equal(UPDATE_LATEST_PATH, '/api/v1/app/update/latest')
})

test('版本对比与强更判断', () => {
  assert.equal(isUpdateAvailable(1001, 1002), true)
  assert.equal(isUpdateAvailable(1001, 1001), false)
  assert.equal(isUpdateAvailable(1001, 1000), false)
  assert.equal(isForceUpdate(1001, 1002), true)
  assert.equal(isForceUpdate(1001, 0), false) // 未标注 = 不强更
  assert.equal(isForceUpdate(1002, 1002), false)
})

test('「稍后」记忆：整数非负才被接受，异常回 0', () => {
  memStore.clear()
  assert.equal(getDismissedVersionCode(), 0)
  setDismissedVersionCode(1002)
  assert.equal(getDismissedVersionCode(), 1002)
  memStore.set('love-life-dismissed-update', 'abc')
  assert.equal(getDismissedVersionCode(), 0)
  memStore.set('love-life-dismissed-update', '-5')
  assert.equal(getDismissedVersionCode(), 0)
})

test('humanBytes：体积人性化', () => {
  assert.equal(humanBytes(48338417), '46.1 MB')
  assert.equal(humanBytes(512), '512 B')
  assert.equal(humanBytes(0), '—')
  assert.equal(humanBytes(Number.NaN), '—')
})