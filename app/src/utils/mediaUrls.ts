// 媒体签名 URL 定时重签（AP3）：签名 15 分钟 TTL，每 5 分钟对已注册媒体批量
// POST /media/reurl 静默换新，避免长时间停留在页面上图片/音视频裂图。
//
// 响应式设计：注册表用普通 Map（渲染中读写都安全），另设 version ref —— mediaUrl()
// 读 version 建立依赖，重签成功后 bump version 让所有引用处自动重渲染；避免了
// 「渲染中写 reactive 容器 → 触发重渲染 → 再写」的循环。
import { ref } from 'vue'
import { api } from '../api/client'
import { mediaIdOf, needsResign, rawOrThumb } from './mediaSign'

interface MediaLike {
  id?: number | string
  url?: string
  thumb_url?: string
}

interface Entry {
  thumb: string
  raw: string
  lastSeen: number
}

const registry = new Map<string, Entry>()
const version = ref(0)
const REFRESH_INTERVAL = 5 * 60 * 1000

function derive(m: MediaLike): { thumb: string; raw: string } {
  return { thumb: m.thumb_url ?? '', raw: rawOrThumb(m) }
}

// mediaUrl 取媒体当前签名地址（未注册/未重签时返回由 m 派生的原值，行为与直接
// 拼 m.thumb_url / m.url 等价）。在渲染中调用即完成注册。
export function mediaUrl(m: MediaLike): { thumb: string; raw: string } {
  void version.value // 响应式依赖：重签成功后触发各引用处刷新（C11：显式丢弃表达式值，过 no-unused-expressions）
  const fallback = derive(m)
  const id = mediaIdOf(m)
  if (id === undefined || id === '') return fallback
  const key = String(id)
  const e = registry.get(key)
  if (!e) {
    registry.set(key, { ...fallback, lastSeen: Date.now() })
    // 注册即需重签（地址已过期 / 只有 thumb 换不出 raw）：立刻换一轮，
    // 不等下一个 5 分钟周期——否则这段时间里缩略图与查看器一直是黑屏。
    if (needsResign(m, Date.now())) void refreshNow()
    return fallback
  }
  e.lastSeen = Date.now()
  return { thumb: e.thumb || fallback.thumb, raw: e.raw || fallback.raw }
}

// refreshMediaUrl 单个媒体立刻换新签名（另存到本机前调用：签名 15 分钟 TTL 已过时
// 直链会 401，重签一次再下）。返回换新后的 raw 地址，失败抛错由调用方兜底。
export async function refreshMediaUrl(id: number | string): Promise<string> {
  const res = await api<{ items: { id: number; thumb_url: string; url: string }[] }>('/api/v1/media/reurl', {
    method: 'POST',
    body: JSON.stringify({ ids: [Number(id)] }),
  })
  const it = res.items?.[0]
  if (!it?.url) throw new Error('reurl 未返回地址')
  const e = registry.get(String(it.id))
  if (e) {
    if (it.thumb_url) e.thumb = it.thumb_url
    e.raw = it.url
    e.lastSeen = Date.now()
  } else {
    registry.set(String(it.id), { thumb: it.thumb_url ?? '', raw: it.url, lastSeen: Date.now() })
  }
  version.value++
  return it.url
}

async function refreshSignedUrls(): Promise<void> {
  const all = [...registry.keys()]
  if (!all.length) return
  for (let i = 0; i < all.length; i += 200) {
    const ids = all.slice(i, i + 200)
    try {
      const res = await api<{ items: { id: number; thumb_url: string; url: string }[] }>(
        '/api/v1/media/reurl',
        { method: 'POST', body: JSON.stringify({ ids: ids.map(Number) }) },
      )
      for (const it of res.items) {
        const e = registry.get(String(it.id))
        if (e) {
          if (it.thumb_url) e.thumb = it.thumb_url
          if (it.url) e.raw = it.url
          e.lastSeen = Date.now()
        }
      }
      version.value++
    } catch {
      // 本轮失败静默（未登录/离线/瞬断），下轮再试；签名未过期前无感
    }
  }
}

// refreshing 防重入：渲染中注册会触发重签，一次渲染可能带出多个过期媒体，
// 不设标志就会并发打出多份重复的 reurl 请求（同一批 id）。
// pending 记「本轮跑动期间又来了新的重签请求」——一次渲染的多个过期媒体常落在
// 首次请求已在途时注册，只跑一轮会漏掉它们（要等下一个 5 分钟周期，期间仍是黑屏）。
let refreshing = false
let pending = false

// refreshNow 注册到需重签的地址时立刻换新（不等下一个 5 分钟周期）。
async function refreshNow(): Promise<void> {
  if (refreshing) {
    pending = true
    return
  }
  refreshing = true
  try {
    do {
      pending = false
      await refreshSignedUrls()
    } while (pending)
  } finally {
    refreshing = false
  }
}

let started = false

// startMediaRefresh 全局启动定时重签（App.vue 挂载时调用一次）。
export function startMediaRefresh(): void {
  if (started) return
  started = true
  // 走 refreshNow 复用防重入：定时轮与「注册即重签」撞上时不会对同一批 id 重复请求
  window.setInterval(() => void refreshNow(), REFRESH_INTERVAL)
}
