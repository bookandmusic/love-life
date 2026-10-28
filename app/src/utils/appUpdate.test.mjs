// 应用内更新纯逻辑测试：清单解析、tag 换算、代理拼接、强更与「稍后」记忆、体积人性化。
import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  addProxy,
  bodyToMeta,
  buildApiUrl,
  buildDownloadUrl,
  getDismissedVersionCode,
  getProxies,
  humanBytes,
  isForceUpdate,
  isUpdateAvailable,
  parseRelease,
  parseTagName,
  pickApkAsset,
  raceLatestRelease,
  removeProxy,
  setDismissedVersionCode,
  tagToVersionCode,
} from './appUpdate.ts'

// node 环境无 localStorage：挂内存实现（先例：chatCache.test.mjs 挂 globalThis.indexedDB）
const memStore = new Map()
globalThis.localStorage = {
  getItem: (k) => (memStore.has(k) ? memStore.get(k) : null),
  setItem: (k, v) => memStore.set(k, String(v)),
  removeItem: (k) => memStore.delete(k),
  clear: () => memStore.clear(),
}

const V011_RELEASE = {
  tag_name: 'v0.1.1',
  body: '**Full Changelog**: https://github.com/bookandmusic/love-life/compare/v0.1.0...v0.1.1',
  assets: [
    {
      name: 'love-life-0.1.1-app-universal-release.aab',
      browser_download_url: 'https://github.com/bookandmusic/love-life/releases/download/v0.1.1/x.aab',
      size: 17954376,
      digest: 'sha256:aaa',
    },
    {
      name: 'love-life-0.1.1-app-universal-release.apk',
      browser_download_url:
        'https://github.com/bookandmusic/love-life/releases/download/v0.1.1/love-life-0.1.1-app-universal-release.apk',
      size: 48338417,
      digest: 'sha256:aad54057e1ecb96f2c8d031ea92c4fcc38b0b49dc942f3f41dad47f9aa7ee811',
    },
  ],
}

test('tag 解析与 versionCode 换算（tauri-cli 规则：major*1e6+minor*1e3+patch，v0.1.1 产物实测=1001）', () => {
  assert.equal(parseTagName('v0.1.1'), '0.1.1')
  assert.equal(parseTagName('0.1.1'), '0.1.1')
  assert.equal(parseTagName('v1.0'), null)
  assert.equal(parseTagName('latest'), null)
  assert.equal(tagToVersionCode('0.1.1'), 1001)
  assert.equal(tagToVersionCode('1.99.5'), 1_099_005)
  assert.equal(tagToVersionCode('0.2.0'), 2_000)
  assert.equal(tagToVersionCode('abc'), null)
})

test('release 解析：挑出 APK asset（排除 AAB）并推导清单', () => {
  const r = parseRelease(V011_RELEASE)
  assert.equal(r.versionName, '0.1.1')
  assert.equal(r.versionCode, 1001)
  assert.equal(r.fileName, 'love-life-0.1.1-app-universal-release.apk')
  assert.equal(r.sizeBytes, 48338417)
  assert.equal(r.sha256, 'sha256:aad54057e1ecb96f2c8d031ea92c4fcc38b0b49dc942f3f41dad47f9aa7ee811')
  assert.equal(r.downloadUrl.includes('/releases/download/v0.1.1/'), true)
  assert.deepEqual(r.notes, [
    '**Full Changelog**: https://github.com/bookandmusic/love-life/compare/v0.1.0...v0.1.1',
  ])
  assert.equal(r.minVersionCode, 0)
})

test('release 解析：缺 APK / 坏 tag / 非对象都报明确错误', () => {
  const noApk = { ...V011_RELEASE, assets: V011_RELEASE.assets.filter((a) => !a.name.endsWith('.apk')) }
  assert.throws(() => parseRelease(noApk), /缺少 APK/)
  assert.throws(() => parseRelease({ tag_name: 'nope', assets: [] }), /tag 无法解析/)
  assert.throws(() => parseRelease(null), /不是对象/)
  assert.throws(() => parseRelease({ tag_name: 'v0.1.1', assets: 'x' }), /缺少 APK/)
})

test('body 约定行：min-version-code 提取且不进 notes；缺省为 0', () => {
  assert.deepEqual(
    bodyToMeta('新增应用内更新\nmin-version-code: 1001000\n修复 xx\n'),
    { notes: ['新增应用内更新', '修复 xx'], minVersionCode: 1_001_000 }
  )
  assert.deepEqual(bodyToMeta(''), { notes: [], minVersionCode: 0 })
  // 大小写不敏感
  assert.deepEqual(bodyToMeta('MIN-VERSION-CODE: 42'), { notes: [], minVersionCode: 42 })
})

test('pickApkAsset：前缀必须匹配版本号，串版本 asset 不被选中', () => {
  assert.equal(pickApkAsset(V011_RELEASE.assets, '0.1.1')?.name.includes('0.1.1'), true)
  assert.equal(pickApkAsset(V011_RELEASE.assets, '0.2.0'), null)
  assert.equal(pickApkAsset('not-array', '0.1.1'), null)
})

test('代理拼接：前缀去尾斜杠；API 与下载 URL 同规则（纯拼接，内置注入在 getProxies 层）', () => {
  assert.equal(
    buildApiUrl(''),
    'https://api.github.com/repos/bookandmusic/love-life/releases/latest'
  )
  assert.equal(
    buildApiUrl('https://gh-proxy.com/'),
    'https://gh-proxy.com/https://api.github.com/repos/bookandmusic/love-life/releases/latest'
  )
  const asset =
    'https://github.com/bookandmusic/love-life/releases/download/v0.1.1/love-life-0.1.1-app-universal-release.apk'
  assert.equal(buildDownloadUrl(asset, ''), asset)
  assert.equal(buildDownloadUrl(asset, 'https://gh-proxy.com'), `https://gh-proxy.com/${asset}`)
})

test('代理列表：默认仅内置；自定义追加去重；内置不可删', () => {
  assert.deepEqual(getProxies(), ['https://gh-proxy.com'])
  addProxy(' https://gh-proxy.net/// ')
  assert.deepEqual(getProxies(), ['https://gh-proxy.com', 'https://gh-proxy.net'])
  addProxy('https://gh-proxy.net') // 与已有自定义同值：忽略
  assert.equal(getProxies().length, 2)
  addProxy('https://gh-proxy.com') // 与内置同值：忽略
  assert.equal(getProxies().length, 2)
  removeProxy('https://gh-proxy.com') // 内置不可删
  assert.deepEqual(getProxies(), ['https://gh-proxy.com', 'https://gh-proxy.net'])
  removeProxy('https://gh-proxy.net')
  assert.deepEqual(getProxies(), ['https://gh-proxy.com'])
})

test('竞速：最快解析通过者胜出并取消其余；快而坏清单的不该赢', async () => {
  const realFetch = globalThis.fetch
  const aborted = []
  const okBody = JSON.stringify(V011_RELEASE)
  globalThis.fetch = (url, init = {}) =>
    new Promise((resolve, reject) => {
      init.signal?.addEventListener('abort', () => {
        aborted.push(String(url))
        reject(new Error('aborted'))
      })
      const u = String(url)
      if (u.includes('fast-bad')) setTimeout(() => resolve(new Response('{"broken"', { status: 200 })), 5)
      else if (u.includes('fast')) setTimeout(() => resolve(new Response(okBody, { status: 200 })), 10)
      else setTimeout(() => resolve(new Response(okBody, { status: 200 })), 120)
    })
  try {
    const raced = await raceLatestRelease(
      ['https://p-slow.example', 'https://p-fast-bad.example', 'https://p-fast.example'],
      5_000
    )
    assert.equal(raced.proxy, 'https://p-fast.example')
    assert.equal(raced.release.versionName, '0.1.1')
    await new Promise((r) => setTimeout(r, 160)) // 等 abort 传播
    assert.equal(aborted.some((u) => u.includes('p-slow')), true, '慢的一路应被取消')
    assert.equal(aborted.some((u) => u.includes('p-fast-bad')), true, '坏清单的一路应被取消')
  } finally {
    globalThis.fetch = realFetch
  }
})

test('竞速：全部失败抛「全部更新通道不可达」', async () => {
  const realFetch = globalThis.fetch
  globalThis.fetch = () => Promise.reject(new Error('boom'))
  try {
    await assert.rejects(
      raceLatestRelease(['https://a.example', 'https://b.example'], 5_000),
      /全部更新通道不可达/
    )
  } finally {
    globalThis.fetch = realFetch
  }
})

test('版本对比与强更判断', () => {
  assert.equal(isUpdateAvailable(1_001_000, 2_000_000), true)
  assert.equal(isUpdateAvailable(2_000_000, 1_001_000), false)
  assert.equal(isUpdateAvailable(1_001_000, 1_001_000), false)
  assert.equal(isForceUpdate(1_001_000, 2_000_000), true)
  assert.equal(isForceUpdate(2_000_000, 2_000_000), false)
  assert.equal(isForceUpdate(1, 0), false) // min=0 → 永不强更
})

test('「稍后」记忆：整数非负才被接受，异常回 0', () => {
  setDismissedVersionCode(2_000_000)
  assert.equal(getDismissedVersionCode(), 2_000_000)
  setDismissedVersionCode(0)
  assert.equal(getDismissedVersionCode(), 0)
})

test('humanBytes：体积人性化', () => {
  assert.equal(humanBytes(48338417), '46.1 MB')
  assert.equal(humanBytes(512), '512 B')
  assert.equal(humanBytes(0), '')
  assert.equal(humanBytes(Number.NaN), '')
})
