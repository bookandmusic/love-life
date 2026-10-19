/**
 * 统一开发 mock 后端 —— 路由定义（零依赖）。
 *
 * 所有路由 handler 定义在此，通过 fixtures 模块共享内存状态。
 * 辅助函数（ok/created/placeholder/daysBetween/spaceResponse/postOut）也在此。
 */

import { createHmac, randomBytes } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { nextLunarOccurrence } from '../../app/src/utils/lunar.ts'
import { mediaKindLabel } from '../../app/src/utils/mediaKind.ts'
import MEDIA_LIMITS from '../../server/internal/limits/media.json' with { type: 'json' }
import {
  USERS, tokenStore, space, posts, comments, likes, favorites,
  albums, timelineMedia, anniversaries, notifications, mapSources, mapPoints,
  GEOCODE_PLACES,
  chatHistory, chatSessions, actions, seq, now,
  dashboardStats, recentActivity, aiUsage, alerts, modelProviders, systemPrompt, styles, styleDefaults,
  tools, recycle, restoreLog, security, audit, tokens, webhooks, adminUsers,
  newToken, userIdFromToken, userById,
} from './fixtures.mjs'

// 确认卡片有效期（默认 15 分钟，与 /chat/history 的 expires_in 同口径）。
// MOCK_ACTION_TTL_MS=0 供过期路径自动化回归用（.verify/mock-expiry-410.mjs）
const ACTION_TTL_MS = Number(process.env.MOCK_ACTION_TTL_MS ?? 900000)
const ACTION_TTL_SEC = Math.max(0, Math.round(ACTION_TTL_MS / 1000))

const ok = (data) => ({ _s: 200, d: data })
const created = (data) => ({ _s: 201, d: data })
const noContent = () => ({ _s: 204, d: null })
const badRequest = (msg) => ({ _s: 400, d: { error: msg } })
const unauthorized = (msg) => ({ _s: 401, d: { error: msg } })
const forbidden = (msg) => ({ _s: 403, d: { error: msg } })
const notFound = (msg) => ({ _s: 404, d: { error: msg } })

// 占位图：直接返回原始 SVG 字符串（而非 data URI，避免此前 base64 解码错误）
const placeholder = (id, w = 512, h = 512) =>
  `<svg xmlns="http://www.w3.org/2000/svg" width="${w}" height="${h}">` +
  `<rect width="100%" height="100%" fill="${['#ff8fab', '#5aa7d4', '#34c759', '#e8a800', '#8a6dff', '#ffb4c2', '#a0c4ff', '#caffbf'][id % 8]}"/>` +
  `<text x="50%" y="50%" font-size="28" fill="#fff" text-anchor="middle" dominant-baseline="middle">媒体 ${id}</text>` +
  `</svg>`

// 已上传的临时媒体 token → {kind}（F1/F3：独立/随动态 finalize 时按 token 落地）
const uploads = new Map()

// ---------- 辅助函数 ----------
function daysBetween(d1, d2) {
  return Math.floor((d2 - d1) / 86400000) + 1
}

function daysUntilDate(dateStr) {
  const today = new Date()
  const d = new Date(dateStr)
  const next = new Date(today.getFullYear(), d.getMonth(), d.getDate())
  if (next < today) next.setFullYear(today.getFullYear() + 1)
  return Math.round((next - today) / 86400000)
}

/** 从 YYYY-MM-DD 取月/日（lunar 时按农历语义） */
function ymdFromDate(s) {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(s || '')) return null
  const [, m, d] = s.split('-').map(Number)
  return { m, d }
}

/**
 * 纪念日「距下一次」天数（2026-08-26 统一契约：date=YYYY-MM-DD + type；lunar 时 date 为农历日期）：
 * - solar：按公历 date 每年重复；
 * - lunar：由 date 取农历月/日用 nextLunarOccurrence 换算（不存/不依赖固定公历，每年公历不同）。
 */
function annivDaysUntil(a) {
  if ((a.type || 'solar') === 'lunar') {
    const ym = ymdFromDate(a.date)
    if (ym) {
      const occ = nextLunarOccurrence(ym.m, ym.d, a.is_leap === true)
      return occ ? daysUntilDate(occ) : -1
    }
    return -1
  }
  return a.date ? daysUntilDate(a.date) : -1
}

/** 纪念日「下一次」公历日期（solar=今年正日；lunar=农历换算的下一次公历；/space 展示用） */
function annivNextDate(a) {
  if ((a.type || 'solar') === 'lunar') {
    const ym = ymdFromDate(a.date)
    return ym ? nextLunarOccurrence(ym.m, ym.d, a.is_leap === true) || '' : ''
  }
  return a.date || ''
}

function nextAnniversaryInfo() {
  let best = null
  let bestDays = Infinity
  for (const a of anniversaries) {
    const days = annivDaysUntil(a)
    if (days >= 0 && days < bestDays) { bestDays = days; best = a }
  }
  return best ? { name: best.name, date: annivNextDate(best), days_until: bestDays } : { name: '', date: '', days_until: -1 }
}

function spaceResponse() {
  const today = new Date()
  let daysTogether = null
  if (space.love_start_date) {
    daysTogether = daysBetween(new Date(space.love_start_date), today)
  }
  const na = nextAnniversaryInfo()
  const members = USERS.filter((u) => u.role !== 'admin').map((u) => ({ id: u.id, nickname: u.nickname, role: u.role, avatar: u.avatar || '' }))
  // 单靠 next_anniversary 无法区分「没有纪念日」与「纪念日存在但日期无效」，
  // 首页空态需要计数（即时统计，不落库）
  return {
    name: space.name,
    signature: space.signature,
    love_start_date: space.love_start_date,
    members,
    days_together: daysTogether,
    next_anniversary: na,
    anniversary_count: anniversaries.length,
    album_count: albums.length,
  }
}

// 媒体输出：对齐真实 mediaResponse {id,kind,width,height,url,thumb_url}
function mediaOut(m) {
  const out = {
    id: m.id, kind: m.kind, width: 0, height: 0,
    url: m.url || `/api/v1/media/file/${m.id}?v=raw`,
    thumb_url: m.thumb_url || `/api/v1/media/file/${m.id}?v=thumb`,
  }
  // 时长（秒）：音频与视频都带（视频角标用），照片不带
  if ((m.kind === 'audio' || m.kind === 'video') && typeof m.duration === 'number') out.duration = m.duration
  return out
}

// 媒体归属的手动相册名（F2：选图器/时间轴展示「已归属」）
function mediaAlbumName(m) {
  if (m.album_id == null) return undefined
  const a = albums.find((x) => x.id === m.album_id)
  return a ? a.name : undefined
}

// 时间轴输出：对齐真实契约 + 分页后缀 + F2 album_name/album_id（前端区分「本相册/他相册」，向后兼容）
function timelineItemOut(m) {
  // month_key：按月分组的键（真实后端按本地时区算好下发；mock 的 fixture 时间已是本地形态，直接截取）
  const monthKey = String(m.taken_at ?? '').slice(0, 7) || String(m.created_at ?? '').slice(0, 7)
  return { ...mediaOut(m), post_id: m.post_id, taken_at: m.taken_at, month_key: monthKey, author_id: m.author_id, album_id: m.album_id ?? null, album_name: mediaAlbumName(m) }
}

function postOut(p, viewerId) {
  const author = USERS.find((u) => u.id === p.author_id)
  const media = (p.media || []).map((m) => mediaOut(m))
  return {
    id: p.id, author_id: p.author_id, author: author?.nickname || '',
    content: p.content, mood: p.mood, location_name: p.location_name,
    media,
    like_count: [...likes].filter((k) => k.startsWith(p.id + ':')).length,
    comment_count: comments.filter((c) => c.post_id === p.id).length,
    liked: viewerId ? likes.has(p.id + ':' + viewerId) : false,
    favorited: viewerId ? favorites.has(p.id + ':' + viewerId) : false,
    favorite_count: [...favorites].filter((k) => k.startsWith(p.id + ':')).length,
    created_at: p.created_at,
  }
}

// 追加站内通知（统一构造 snapshot 字符串）
// 通知为临时消费品（PRODUCT「通知中心」）：无 is_read 字段，列表内均为未消费，剩余即未读数。
function addNotification(recipientId, eventType, targetType, targetId, snapshot, minutesAgo = 0) {
  notifications.push({
    id: ++seq.notification,
    recipient_id: recipientId,
    event_type: eventType,
    target_type: targetType,
    target_id: targetId,
    snapshot: typeof snapshot === 'string' ? snapshot : JSON.stringify(snapshot),
    created_at: new Date(now - minutesAgo * 60000).toISOString(),
  })
}

// 情侣另一方（非 admin、非操作者）——F7「任何一方动作都通知另一方」
function partnerIds(uid) {
  return USERS.filter((u) => u.role !== 'admin' && u.id !== uid).map((u) => u.id)
}
function operatorName(uid) {
  const u = USERS.find((x) => x.id === uid)
  return u ? u.nickname || u.username : ''
}

// 从 URL 中提取数字路径参数（未匹配返回 null）
const parseIntParam = (url, re) => {
  const m = url.match(re)
  return m ? parseInt(m[1]) : null
}
// 解析 query 参数为对象
const queryParams = (url) => Object.fromEntries(new URLSearchParams(url.split('?')[1] || ''))

// 地理编码表匹配（名称/城市/展示名模糊，返回首个命中）
function locatePlace(name) {
  const q = (name || '').trim().toLowerCase()
  if (!q) return null
  return GEOCODE_PLACES.find((p) =>
    p.name.toLowerCase().includes(q) || p.city.toLowerCase().includes(q) || p.display.toLowerCase().includes(q)
  ) || null
}

// ---------- 路由 ----------
const routes = []

// 健康检查：客户端双地址探测（AP1）ping 的就是这里，无鉴权。
// 此前 mock 缺该路由 → 探测恒 404，登录页状态点恒显示「不可达」。
routes.push({ method: 'GET', pattern: /^\/api\/v1\/health$/, handler: () => ok({ status: 'ok' }) })

// --- Auth ---
routes.push({ method: 'POST', pattern: /^\/api\/v1\/auth\/login$/, handler: (url, body, headers) => {
  const { username, password, client } = JSON.parse(body || '{}')
  const user = USERS.find((u) => u.username === username && u.password === password)
  if (!user) return unauthorized('用户名或密码错误')
  if (client === 'admin' && user.role !== 'admin') return forbidden('仅 admin 角色可登录后台')
  if (client === 'app' && user.role === 'admin') return forbidden('管理员账号请登录后台，不支持在 APP 使用')
  const { access, refresh } = newToken(user.id)
  return ok({ token: access, refresh_token: refresh, user: userById(user.id) })
}})

routes.push({ method: 'GET', pattern: /^\/api\/v1\/auth\/me$/, handler: (url, body, headers) => {
  const uid = userIdFromToken(headers)
  if (!uid) return unauthorized()
  return ok(userById(uid))
}})

// 刷新：refresh_token 走 body（与真实服务端 server/internal/auth/service.go 一致，客户端不带 Authorization）；
// 签发新对并作废旧 refresh（rotation）——旧 refresh 复用即 401
routes.push({ method: 'POST', pattern: /^\/api\/v1\/auth\/refresh$/, handler: (url, body) => {
  const rt = String(JSON.parse(body || '{}').refresh_token || '')
  const uid = rt ? tokenStore.get(rt) : null
  if (!uid) return unauthorized('refresh_token 无效或已过期')
  tokenStore.delete(rt)
  const { access, refresh } = newToken(uid)
  return ok({ token: access, refresh_token: refresh })
}})

routes.push({ method: 'POST', pattern: /^\/api\/v1\/auth\/logout$/, handler: (url, body, headers) => {
  return ok({})
}})

// --- Space / Me ---
routes.push({ method: 'GET', pattern: /^\/api\/v1\/space$/, handler: () => ok(spaceResponse()) })

routes.push({ method: 'PUT', pattern: /^\/api\/v1\/space$/, handler: (url, body, headers) => {
  // 空间配置仅 admin 可改（与后端 requireAdmin 同口径；GET 仍对登录用户开放）
  const uid = userIdFromToken(headers)
  if (!uid) return unauthorized()
  if (userById(uid)?.role !== 'admin') return forbidden('仅 admin 可访问')
  const b = JSON.parse(body || '{}')
  if (b.name !== undefined) space.name = b.name
  if (b.signature !== undefined) space.signature = b.signature
  if (b.love_start_date !== undefined) space.love_start_date = b.love_start_date
  return ok({ status: 'ok' })
}})

routes.push({ method: 'PATCH', pattern: /^\/api\/v1\/me$/, handler: (url, body, headers) => {
  const uid = userIdFromToken(headers)
  if (!uid) return unauthorized()
  const b = JSON.parse(body || '{}')
  const u = USERS.find((x) => x.id === uid)
  if (!u) return notFound()
  if (b.nickname !== undefined) u.nickname = b.nickname
  if (b.birthday !== undefined) u.birthday = b.birthday
  if (b.ai_style !== undefined) u.ai_style = b.ai_style
  return ok({ status: 'ok' })
}})

// --- Posts ---
routes.push({ method: 'GET', pattern: /^\/api\/v1\/posts$/, handler: (url, body, headers) => {
  const uid = userIdFromToken(headers)
  const q = queryParams(url)
  const page = parseInt(q.page) || 1
  const pageSize = parseInt(q.page_size) || 20
  let sorted = [...posts].sort((a, b) => new Date(b.created_at) - new Date(a.created_at))
 // 那年今日：先按同月同日过滤，再分页——否则只能在最新一页里碰运气
  if (q.memories === '1') {
    // 日期口径（与服务端 store/timeutil.go 一致）：created_at 落库为 UTC，但「今天」按**本地时区**判定；
    // 且只取**历史**年份——当年今天发的动态不算「那年今日」（与服务端 ListPostsOnMonthDay 同口径）。
    const pad = (n) => String(n).padStart(2, '0')
    const today = new Date()
    const todayMMDD = `${pad(today.getMonth() + 1)}-${pad(today.getDate())}`
    sorted = sorted.filter((p) => {
      const d = new Date(p.created_at)
      if (Number.isNaN(d.getTime())) return false
      return `${pad(d.getMonth() + 1)}-${pad(d.getDate())}` === todayMMDD && d.getFullYear() < today.getFullYear()
    })
  }
  // media=1/0：只看带媒体 / 不带媒体（首页回忆精选分层抽样用，向真实后端对齐）
  if (q.media === '1' || q.media === '0') {
    const want = q.media === '1'
    sorted = sorted.filter((p) => (timelineMedia.some((m) => m.post_id === p.id) === want))
  }
  const total = sorted.length
  // sample=1：在**全量候选**里随机取（不分页；首页「回忆精选」用，向真实后端对齐）
  if (q.sample === '1') {
    sorted = [...sorted].sort(() => Math.random() - 0.5)
    return ok({ items: sorted.slice(0, pageSize).map((p) => postOut(p, uid)), total, page: 1 })
  }
  const pageItems = sorted.slice((page - 1) * pageSize, page * pageSize)
  return ok({ items: pageItems.map((p) => postOut(p, uid)), total })
}})

routes.push({ method: 'POST', pattern: /^\/api\/v1\/posts$/, handler: (url, body, headers) => {
  const uid = userIdFromToken(headers)
  if (!uid) return unauthorized()
  const b = JSON.parse(body || '{}')
  const content = (b.content || '').trim()
  const mood = (b.mood || '').trim()
  const tokens = Array.isArray(b.media_tokens) ? b.media_tokens : []
  const loc = b.location && typeof b.location === 'object' ? b.location : (b.location_name ? { name: b.location_name } : null)
  const locationName = (loc?.name || '').trim()
  // 发布条件：文字/媒体/地点/心情 至少一项非空（纯媒体可发）
  if (!content && tokens.length === 0 && !locationName && !mood) return badRequest('请至少填写文字、心情、地点或媒体')
  const id = ++seq.post
  // 地点：同名复用同一点位；否则自动建点（发动态选地点即生成，A7/1.5）
  // 坐标来源：请求显式 lat/lng 优先，其次查内置 geocode 表（不再用 0,0 占位）
  let pointId = null
  if (locationName) {
    let lat = Number(loc?.lat)
    let lng = Number(loc?.lng)
    if (!Number.isFinite(lat) && !Number.isFinite(lng) || (Number.isFinite(lat) && Number.isFinite(lng) && lat === 0 && lng === 0)) {
      const hitP = locatePlace(locationName)
      if (hitP) { lat = hitP.lat; lng = hitP.lng }
    }
    if (!Number.isFinite(lat)) lat = 0
    if (!Number.isFinite(lng)) lng = 0
    const hit = mapPoints.find((p) => p.deleted_at === null && p.name === locationName)
    if (hit) pointId = hit.id
    else {
      const np = { id: ++seq.mapPoint, name: locationName, lat, lng, visit_date: null, description: '', source: 'post', created_at: new Date().toISOString(), deleted_at: null, created_by: uid }
      mapPoints.push(np)
      pointId = np.id
    }
  }
  const newPost = {
    id, author_id: uid, content, mood,
    point_id: pointId, location_name: locationName, media: [],
    created_at: new Date().toISOString(),
  }
  // 媒体 token → 落库 timelineMedia（post_id=本动态）
  if (b.media_tokens && Array.isArray(b.media_tokens)) {
    for (const t of b.media_tokens) {
      const kind = uploads.get(t) || 'photo'
      const m = { id: ++seq.media, post_id: id, kind, author_id: uid, album_id: null, taken_at: new Date().toISOString().slice(0, 10), thumb_url: `/api/v1/media/file/${seq.media}?v=thumb`, url: `/api/v1/media/file/${seq.media}?v=raw` }
      timelineMedia.push(m)
      newPost.media.push(m)
    }
  }
  posts.unshift(newPost)
  // F7：新动态通知另一方（admin 不在情侣里，partnerIds 排除 admin）
  for (const r of partnerIds(uid)) addNotification(r, 'post_created', 'post', id, { operator: operatorName(uid), content: (newPost.content || '').slice(0, 50) })
  return created({ id })
}})

routes.push({ method: 'GET', pattern: /^\/api\/v1\/posts\/\d+$/, handler: (url, body, headers) => {
  const uid = userIdFromToken(headers)
  const id = parseIntParam(url, /\/posts\/(\d+)/)
  if (!id) return notFound()
  const p = posts.find((x) => x.id === id)
  if (!p) return notFound()
  return ok(postOut(p, uid))
}})

routes.push({ method: 'DELETE', pattern: /^\/api\/v1\/posts\/\d+$/, handler: (url, body, headers) => {
  const uid = userIdFromToken(headers)
  if (!uid) return unauthorized()
  const id = parseIntParam(url, /\/posts\/(\d+)/)
  if (!id) return notFound()
  const idx = posts.findIndex((x) => x.id === id)
  if (idx === -1) return notFound()
  const p = posts[idx]
 // F7 自删静默 + ：内容删除仅通知被删内容作者（非本人删除时），其余人静默
  if (p.author_id !== uid) {
    addNotification(p.author_id, 'post_deleted', 'post', id, { deleted_by: operatorName(uid), content: (p.content || '').slice(0, 50) })
  }
  posts.splice(idx, 1)
  return noContent()
}})

// --- Likes ---
routes.push({ method: 'POST', pattern: /^\/api\/v1\/posts\/\d+\/like$/, handler: (url, body, headers) => {
  const uid = userIdFromToken(headers)
  if (!uid) return unauthorized()
  const id = parseIntParam(url, /\/posts\/(\d+)\/like/)
  if (!id) return notFound()
  const p = posts.find((x) => x.id === id)
  if (!p) return notFound()
  const key = id + ':' + uid
  let liked
  if (likes.has(key)) { likes.delete(key); liked = false } else { likes.add(key); liked = true }
  // F7：任何一方动作（含自操作）都通知另一方
  if (liked) {
    const op = operatorName(uid)
    for (const r of partnerIds(uid)) addNotification(r, 'post_liked', 'post', id, { operator: op })
  }
  // 响应维持 {liked}，计数由 APP 本地增减
  return ok({ liked })
}})

// --- Favorites ---
routes.push({ method: 'POST', pattern: /^\/api\/v1\/posts\/\d+\/favorite$/, handler: (url, body, headers) => {
  const uid = userIdFromToken(headers)
  if (!uid) return unauthorized()
  const id = parseIntParam(url, /\/posts\/(\d+)\/favorite/)
  if (!id) return notFound()
  const p = posts.find((x) => x.id === id)
  if (!p) return notFound()
  const key = id + ':' + uid
  let favorited
  if (favorites.has(key)) { favorites.delete(key); favorited = false } else { favorites.add(key); favorited = true }
  // F7：任何一方动作（含自操作）都通知另一方
  if (favorited) {
    const op = operatorName(uid)
    for (const r of partnerIds(uid)) addNotification(r, 'post_favorited', 'post', id, { operator: op })
  }
  return ok({ favorited })
}})

// --- Comments ---
routes.push({ method: 'GET', pattern: /^\/api\/v1\/posts\/\d+\/comments$/, handler: (url) => {
  const postId = parseIntParam(url, /\/posts\/(\d+)\/comments/)
  if (!postId) return notFound()
  const list = comments.filter((c) => c.post_id === postId).map((c) => {
    const author = USERS.find((u) => u.id === c.author_id)
    return { id: c.id, post_id: c.post_id, author_id: c.author_id, author: author?.nickname || '', content: c.content, created_at: c.created_at }
  })
  return ok({ items: list })
}})

routes.push({ method: 'POST', pattern: /^\/api\/v1\/posts\/\d+\/comments$/, handler: (url, body, headers) => {
  const uid = userIdFromToken(headers)
  if (!uid) return unauthorized()
  const postId = parseIntParam(url, /\/posts\/(\d+)\/comments/)
  if (!postId) return notFound()
  const p = posts.find((x) => x.id === postId)
  if (!p) return notFound()
  const b = JSON.parse(body || '{}')
  const id = ++seq.comment
  const c = { id, post_id: postId, author_id: uid, content: b.content || '', created_at: new Date().toISOString() }
  comments.push(c)
  // F7：任何一方动作（含自评）都通知另一方（评论删除通知见 DELETE，仅通知评论作者）
  const op = operatorName(uid)
  for (const r of partnerIds(uid)) addNotification(r, 'post_commented', 'post', postId, { operator: op, content: (b.content || '').slice(0, 50) })
  const author = USERS.find((u) => u.id === uid)
  return created({ id, post_id: postId, author_id: uid, author: author?.nickname || '', content: c.content, created_at: c.created_at })
}})

routes.push({ method: 'DELETE', pattern: /^\/api\/v1\/comments\/\d+$/, handler: (url, body, headers) => {
  const uid = userIdFromToken(headers)
  if (!uid) return unauthorized()
  const id = parseIntParam(url, /\/comments\/(\d+)/)
  if (!id) return notFound()
  const c = comments.find((x) => x.id === id)
  if (!c) return notFound()
 // F7 自删静默 + ：评论删除仅通知评论作者（非本人删除时），其余人静默
  if (c.author_id !== uid) {
    addNotification(c.author_id, 'comment_deleted', 'comment', id, { deleted_by: operatorName(uid), content: (c.content || '').slice(0, 50) })
  }
  comments.splice(comments.indexOf(c), 1)
  return noContent()
}})

// --- Albums ---
function liveTimelineMedia() {
  // mock 简化为无软删；过滤已删除动态的随动态媒体
  return timelineMedia.filter((m) => {
    if (m.post_id != null && !posts.find((p) => p.id === m.post_id)) return false
    return true
  })
}

routes.push({ method: 'GET', pattern: /^\/api\/v1\/albums$/, handler: () => {
  const list = albums.map((a) => ({
    ...a,
    item_count: liveTimelineMedia().filter((m) => m.album_id === a.id).length,
  }))
  return ok({ items: list })
}})

routes.push({ method: 'GET', pattern: /^\/api\/v1\/albums\/months\/([\d-]{7})$/, handler: (url) => {
  const key = decodeURIComponent(url.split('/').pop() ?? '')
  if (!/^\d{4}-\d{2}$/.test(key)) return notFound()
  const media = liveTimelineMedia()
    .filter((m) => m.album_id == null && (m.taken_at || m.created_at || '').slice(0, 7) === key)
    .map(timelineItemOut)
  return ok({ month_key: key, media })
}})

routes.push({ method: 'POST', pattern: /^\/api\/v1\/albums$/, handler: (url, body, headers) => {
  const uid = userIdFromToken(headers)
  if (!uid) return unauthorized()
  const b = JSON.parse(body || '{}')
  const id = ++seq.album
  const a = { id, name: b.name || '新相册', description: b.description || '', cover_url: '', item_count: 0, created_by: uid }
  albums.push(a)
 // 通知另一方「相册上新」（按 ：对方创建相册时通知，自建静默）
  for (const r of partnerIds(uid)) addNotification(r, 'album_created', 'album', id, { operator: operatorName(uid), name: a.name })
  return created(a)
}})

routes.push({ method: 'GET', pattern: /^\/api\/v1\/albums\/\d+$/, handler: (url) => {
  const id = parseIntParam(url, /\/albums\/(\d+)/)
  if (!id) return notFound()
  const a = albums.find((x) => x.id === id)
  if (!a) return notFound()
  const media = liveTimelineMedia().filter((m) => m.album_id === id).map(timelineItemOut)
  return ok({ album: { ...a, item_count: media.length }, media })
}})

routes.push({ method: 'DELETE', pattern: /^\/api\/v1\/albums\/\d+$/, handler: (url, body, headers) => {
  const id = parseIntParam(url, /\/albums\/(\d+)/)
  if (!id) return notFound()
  const idx = albums.findIndex((x) => x.id === id)
  if (idx === -1) return notFound()
 // 删除对方创建的手动相册 → 通知创建者（自删静默；内容删除仅站内）
  const uid = userIdFromToken(headers)
  if (uid && albums[idx].created_by && albums[idx].created_by !== uid) {
    addNotification(albums[idx].created_by, 'album_deleted', 'album', id, { name: albums[idx].name, deleted_by: operatorName(uid) })
  }
  // 手动相册删除 → 释放媒体归属，媒体回到「未归属」状态
  for (const m of timelineMedia) if (m.album_id === id) m.album_id = null
  albums.splice(idx, 1)
  return noContent()
}})

routes.push({ method: 'POST', pattern: /^\/api\/v1\/albums\/\d+\/photos$/, handler: (url, body) => {
  const id = parseIntParam(url, /\/albums\/(\d+)\/photos/)
  if (!id) return notFound()
  const a = albums.find((x) => x.id === id)
  if (!a) return notFound()
  const b = JSON.parse(body || '{}')
  const ids = Array.isArray(b.media_ids) ? b.media_ids : []
  // F2 一文件一相册：已归属任一手动手动相册的文件不能再添加
  for (const mid of ids) {
    const m = timelineMedia.find((x) => x.id === mid)
    if (!m) continue
    if (m.album_id != null && m.album_id !== id) {
      return { _s: 409, d: { error: `媒体 ${mid} 已归属相册 #${m.album_id}，一文件只能归属一个手动相册` } }
    }
  }
  for (const mid of ids) {
    const m = timelineMedia.find((x) => x.id === mid)
    if (m) m.album_id = id
  }
  return ok({ status: 'ok', item_count: liveTimelineMedia().filter((m) => m.album_id === a.id).length })
}})

routes.push({ method: 'DELETE', pattern: /^\/api\/v1\/albums\/\d+\/photos$/, handler: (url, body) => {
  const id = parseIntParam(url, /\/albums\/(\d+)\/photos/)
  if (!id) return notFound()
  const a = albums.find((x) => x.id === id)
  if (!a) return notFound()
  const b = JSON.parse(body || '{}')
  const ids = Array.isArray(b.media_ids) ? b.media_ids : []
  for (const mid of ids) {
    const m = timelineMedia.find((x) => x.id === mid)
    if (m && m.album_id === id) m.album_id = null
  }
  return ok({ status: 'ok', item_count: liveTimelineMedia().filter((m) => m.album_id === a.id).length })
}})

// --- Media ---
// 主动撤销：放弃 / 取消 / 把关作废时清临时文件（幂等，mock 记录 token 归属）
const tmpTokens = new Map()
routes.push({ method: 'POST', pattern: /^\/api\/v1\/media\/discard$/, handler: (url, body, headers) => {
  const uid = userIdFromToken(headers)
  if (!uid) return unauthorized()
  const b = JSON.parse(body || '{}')
  if (!Array.isArray(b.tokens)) return badRequest('tokens 必填（数组）')
  if (b.tokens.length > 100) return badRequest('单次最多撤销 100 个临时文件')
  let discarded = 0
  for (const tk of b.tokens) {
    const owner = tmpTokens.get(tk)
    if (owner !== undefined && owner === uid) {
      tmpTokens.delete(tk)
      discarded++
    }
  }
  return ok({ discarded })
}})


// mock 简易 multipart 解析：仅取 filename 与 content-type 子段，不解码二进制
function parseMultipartMeta(rawBody, contentType) {
  const m = /filename="([^"]+)"/i.exec(rawBody || '')
  const filename = m ? m[1] : ''
  const ext = (filename.split('.').pop() || '').toLowerCase()
  let kind = 'photo'
  if (['mp3', 'm4a', 'wav'].includes(ext)) kind = 'audio'
  else if (['mp4', 'mov', 'webm'].includes(ext)) kind = 'video'
  else if (['jpg', 'jpeg', 'png', 'webp', 'gif'].includes(ext)) kind = 'photo'
  // 也按 Content-Type 兜底
  const ct = (contentType || '').toLowerCase()
  if (ct.startsWith('audio/')) kind = 'audio'
  else if (ct.startsWith('video/')) kind = 'video'
  return { filename, kind }
}

routes.push({ method: 'GET', pattern: /^\/api\/v1\/media\/timeline$/, handler: (url) => {
  const q = queryParams(url)
  const page = parseInt(q.page) || 1
  const size = Math.min(parseInt(q.size) || 20, 50)
  // 口径（2026-09-22 确认）：默认只返回未归档媒体（时间轴=未归档）；scope=all 返回全部（选图器灰显用）
  // F1/F2：列表项带 album_name；F2 一文件一相册由 album_id 决定
  let list = liveTimelineMedia()
  if (q.scope !== 'all') list = list.filter((m) => m.album_id == null)
  list = list.slice().sort((a, b) => {
    const ta = a.taken_at || a.created_at || ''
    const tb = b.taken_at || b.created_at || ''
    return tb.localeCompare(ta)
  })
  const total = list.length
  const items = list.slice((page - 1) * size, page * size).map(timelineItemOut)
  return ok({ items, total, page, size, has_more: page * size < total })
}})

routes.push({ method: 'POST', pattern: /^\/api\/v1\/media\/upload$/, handler: (url, body, headers) => {
  const { kind, filename } = parseMultipartMeta(body, headers['content-type'] || '')
  const uid = userIdFromToken(headers)
  if (!uid) return unauthorized()
  const token = 'mock-media-' + randomBytes(6).toString('hex')
  uploads.set(token, kind)
  tmpTokens.set(token, uid) // discard 归属校验
  return ok({ media_token: token, kind, filename })
}})

// reurl 批量重签：{ids ≤200} → {items:[{id,thumb_url,url}]}；无效/已删 id 跳过
routes.push({ method: 'POST', pattern: /^\/api\/v1\/media\/reurl$/, handler: (url, body, headers) => {
  const uid = userIdFromToken(headers)
  if (!uid) return unauthorized()
  const b = JSON.parse(body || '{}')
  const ids = Array.isArray(b.ids) ? b.ids : []
  if (!ids.length) return badRequest('ids 不能为空')
  if (ids.length > 200) return badRequest('单次最多重签 200 个媒体')
  const items = ids
    .filter((id) => Number.isFinite(id) && liveTimelineMedia().some((m) => m.id === id))
    .map((id) => ({ id, url: `/api/v1/media/file/${id}?v=raw`, thumb_url: `/api/v1/media/file/${id}?v=thumb` }))
  return ok({ items })
}})


// 媒体文件下发（契约对齐服务端 GET /api/v1/media/file/{id}）：
//   v=thumb → 占位 SVG（零素材）；v=raw → fixtures/ 里的真实可播放样本（照片 JPEG / 视频 VP8 WebM / 录音 WAV）
//   download=1 → Content-Disposition，文件名同服务端规则 {日期}_{照片|视频|录音}_{序号}.{ext}，中文走 RFC 5987
//   Range → 206/Content-Range（大文件续传、播放器拖进度；服务端走 http.ServeContent 同语义）
// 真实样本素材（tools/mock/fixtures/，均在本地离线生成，浏览器可直接播放）：
//   照片 1200×800 JPEG（Pillow）；录音 8kHz 单声道 3s WAV（python wave）；视频 640×360 VP8 WebM（Chrome MediaRecorder 录屏）
//   视频用 WebM 而不是 MP4：开发机没有 ffmpeg 也没有外网，只能在浏览器里录一段真视频；播放器代码路径与真实 H.264/MP4 相同。
//   读取失败退回内联占位字节，保证 mock 在缺素材时仍能启动。
const FIXTURE_DIR = resolve(dirname(fileURLToPath(import.meta.url)), 'fixtures')
function fixture(name, fallback) {
  try { return readFileSync(resolve(FIXTURE_DIR, name)) } catch { return fallback }
}
const MOCK_RAW = {
  photo: { ext: '.jpg', ct: 'image/jpeg', buf: fixture('sample-photo.jpg', Buffer.from('/9j/4AAQSkZJRgABAQEAYABgAAD/2wBDAAgGBgcGBQgHBwcJCQgKDBQNDAsLDBkSEw8UHRofHh0aHBwgJC4nICIsIxwcKDcpLDAxNDQ0Hyc5PTgyPC4zNDL/wAALCAABAAEBAREA/8QAFAABAAAAAAAAAAAAAAAAAAAACf/EABQQAQAAAAAAAAAAAAAAAAAAAAD/2gAIAQEAAD8AKp//2Q==', 'base64')) },
  video: { ext: '.webm', ct: 'video/webm', buf: fixture('sample-video.webm', Buffer.alloc(4096, 0x21)) },
  audio: { ext: '.wav', ct: 'audio/wav', buf: fixture('sample-audio.wav', Buffer.alloc(4096, 0x42)) },
}

// rfc5987：attr-char 之外逐字节 %XX（与 Go 侧 percentEncode 同口径）
function rfc5987(s) {
  const attrChar = '!#$&+-.^_|~'
  let out = ''
  const bytes = Buffer.from(s, 'utf8')
  for (const c of bytes) {
    const ch = String.fromCharCode(c)
    out += /[A-Za-z0-9]/.test(ch) || attrChar.includes(ch)
      ? ch
      : '%' + c.toString(16).toUpperCase().padStart(2, '0')
  }
  return out
}

// 另存文件名（服务端 downloadName 的 mock 同款）：日期优先 taken_at、其次所属动态 created_at
function mockDownloadName(mediaId, kind) {
  const spec = MOCK_RAW[kind] || MOCK_RAW.photo
  const row = timelineMedia.find((x) => x.id === mediaId)
  const post = posts.find((p) => (p.media || []).some((mm) => mm.id === mediaId))
  const date = (row?.taken_at || post?.created_at || new Date().toISOString()).slice(0, 10)
  let seq = mediaId
  const siblings = post ? post.media || [] : []
  const at = siblings.findIndex((mm) => mm.id === mediaId)
  if (at >= 0) seq = at + 1
  // 词表与客户端共用一个来源（app/src/utils/mediaKind.ts），避免两处抄出不同口径
  return `${date}_${mediaKindLabel(kind)}_${seq}${spec.ext}`
}

function asciiFallback(name) {
  const stripped = name.replace(/[^\x20-\x7e]/g, '').replace(/"|\\/g, '')
  return stripped.replace(/__+/g, '_') || 'love-life'
}

routes.push({ method: 'GET', pattern: /^\/api\/v1\/media\/file\/\d+$/, handler: (url, _body, headers) => {
  const id = parseIntParam(url, /\/media\/file\/(\d+)/) || 1
  const m = timelineMedia.find((x) => x.id === id)
  const kind = m?.kind || 'photo'
  const q = queryParams(url)
  const variant = q.v || 'raw'

  if (variant === 'thumb') {
    const svg = kind === 'audio'
      ? `<svg xmlns="http://www.w3.org/2000/svg" width="512" height="512"><rect width="100%" height="100%" fill="#a0c4ff"/><text x="50%" y="50%" font-size="28" fill="#fff" text-anchor="middle" dominant-baseline="middle">音频 ${id}</text></svg>`
      : placeholder(id)
    return (res) => {
      res.setHeader('Content-Type', 'image/svg+xml;charset=utf-8')
      res.setHeader('Cache-Control', 'public, max-age=86400')
      res.statusCode = 200
      res.end(svg)
    }
  }

  const spec = MOCK_RAW[kind] || MOCK_RAW.photo
  const buf = spec.buf
  const disposition = q.download === '1'
    ? `attachment; filename="${asciiFallback(mockDownloadName(id, kind))}"; filename*=UTF-8''${rfc5987(mockDownloadName(id, kind))}`
    : ''
  const range = /^bytes=(\d*)-(\d*)$/.exec((headers?.range || '').trim())
  return (res) => {
    if (disposition) res.setHeader('Content-Disposition', disposition)
    res.setHeader('Content-Type', spec.ct)
    res.setHeader('Cache-Control', 'private, max-age=900')
    res.setHeader('Accept-Ranges', 'bytes')
    if (range) {
      const start = range[1] ? Number(range[1]) : 0
      const end = range[2] ? Number(range[2]) : buf.length - 1
      if (Number.isNaN(start) || start >= buf.length || end < start) {
        res.statusCode = 416
        res.setHeader('Content-Range', `bytes */${buf.length}`)
        res.end()
        return
      }
      const stop = Math.min(end, buf.length - 1)
      const slice = buf.subarray(start, stop + 1)
      res.statusCode = 206
      res.setHeader('Content-Range', `bytes ${start}-${stop}/${buf.length}`)
      res.setHeader('Content-Length', String(slice.length))
      res.end(slice)
      return
    }
    res.statusCode = 200
    res.setHeader('Content-Length', String(buf.length))
    res.end(buf)
  }
}})

// --- Anniversaries ---
routes.push({ method: 'GET', pattern: /^\/api\/v1\/anniversaries$/, handler: () => ok({ items: anniversaries.map((a) => ({ ...a, type: a.type || 'solar', days_until: annivDaysUntil(a) })) }) })

routes.push({ method: 'POST', pattern: /^\/api\/v1\/anniversaries$/, handler: (url, body, headers) => {
  const uid = userIdFromToken(headers)
  if (!uid) return unauthorized()
  const b = JSON.parse(body || '{}')
  // 提醒天数越界即拒（与后端同口径：0-90，默认 1；显式 0=当天提醒）
  if (b.remind_days_before !== undefined && (b.remind_days_before < 0 || b.remind_days_before > 90)) {
    return badRequest('提前天数应为 0-90')
  }
  const id = ++seq.anniversary
  const type = b.type === 'lunar' ? 'lunar' : 'solar'
  // 统一契约：date 一律 YYYY-MM-DD，type 决定语义（solar=公历；lunar=农历日期）；闰月语义由 is_leap 标记承载
  const date = /^\d{4}-\d{2}-\d{2}$/.test(b.date || '') ? b.date : ''
  const a = {
    id,
    name: (b.name || '').trim() || '新纪念日',
    date,
    type,
    is_leap: type === 'lunar' ? b.is_leap === true : undefined,
 remind_days_before: b.remind_days_before ?? 1, // 默认 1 天；显式 0=当天提醒，不得被 || 改写
    created_by: uid,
  }
  anniversaries.push(a)
  return created(a)
}
})

routes.push({ method: 'DELETE', pattern: /^\/api\/v1\/anniversaries\/\d+$/, handler: (url, body, headers) => {
  const id = parseIntParam(url, /\/anniversaries\/(\d+)/)
  if (!id) return notFound()
  const idx = anniversaries.findIndex((x) => x.id === id)
  if (idx === -1) return notFound()
 // 删除对方创建的纪念日 → 通知创建者（自删静默；内容删除仅站内）
  const uid = userIdFromToken(headers)
  if (uid && anniversaries[idx].created_by && anniversaries[idx].created_by !== uid) {
    addNotification(anniversaries[idx].created_by, 'anniversary_deleted', 'anniversary', id, { name: anniversaries[idx].name, deleted_by: operatorName(uid) })
  }
  anniversaries.splice(idx, 1)
  return noContent()
}})

routes.push({ method: 'PATCH', pattern: /^\/api\/v1\/anniversaries\/\d+\/reminder$/, handler: (url, body) => {
  const id = parseIntParam(url, /\/anniversaries\/(\d+)\/reminder/)
  if (!id) return notFound()
  const a = anniversaries.find((x) => x.id === id)
  if (!a) return notFound()
  const b = JSON.parse(body || '{}')
  // 越界即拒（与后端 PATCH reminder 同口径）
  if (b.remind_days_before !== undefined && (b.remind_days_before < 0 || b.remind_days_before > 90)) {
    return badRequest('提前天数应为 0-90')
  }
  if (b.remind_days_before !== undefined) a.remind_days_before = b.remind_days_before
  return ok({ status: 'ok' })
}})

// --- Notifications ---
routes.push({ method: 'GET', pattern: /^\/api\/v1\/notifications$/, handler: (url, body, headers) => {
  const uid = userIdFromToken(headers)
  if (!uid) return ok({ items: [] })
  const list = notifications.filter((n) => n.recipient_id === uid).sort((a, b) => new Date(b.created_at) - new Date(a.created_at))
  return ok({ items: list })
}})

routes.push({ method: 'GET', pattern: /^\/api\/v1\/notifications\/unread-count$/, handler: (url, body, headers) => {
  const uid = userIdFromToken(headers)
  if (!uid) return ok({ count: 0 })
  // 通知为临时消费品：无已读态，列表内剩余条数即未读数（与服务端 UnreadNotificationCount 同口径）
  const count = notifications.filter((n) => n.recipient_id === uid).length
  return ok({ count })
}})

// 点击即删（PRODUCT「通知中心」：通知是临时消费品）；仅能删自己的
routes.push({ method: 'DELETE', pattern: /^\/api\/v1\/notifications\/\d+$/, handler: (url, body, headers) => {
  const uid = userIdFromToken(headers)
  const id = parseIntParam(url, /\/notifications\/(\d+)/)
  if (!id) return notFound()
  const idx = notifications.findIndex((x) => x.id === id && x.recipient_id === uid)
  if (idx < 0) return notFound()
  notifications.splice(idx, 1)
  return ok({ status: 'ok' })
}})

// 清空：删除收件人全部通知
routes.push({ method: 'DELETE', pattern: /^\/api\/v1\/notifications$/, handler: (url, body, headers) => {
  const uid = userIdFromToken(headers)
  for (let i = notifications.length - 1; i >= 0; i--) {
    if (notifications[i].recipient_id === uid) notifications.splice(i, 1)
  }
  return ok({ status: 'ok' })
}})

// --- Chat ---
routes.push({ method: 'GET', pattern: /^\/api\/v1\/chat\/status$/, handler: () => ok({ available: true, reason: '' }) })

// 多会话：会话工具函数（懒创建 + 摘要命名，与后端 sessionTitleFrom 同口径）
const sessionTitleFrom = (content) => {
  const t = (content || '').trim()
  if (!t) return '新的会话'
  return t.length > 20 ? t.slice(0, 20) + '…' : t
}
function ensureSession(uid, wanted, firstMsg) {
  if (wanted > 0) {
    const s = chatSessions.find((x) => x.id === wanted && x.user_id === uid)
    if (!s) return null
    s.updated_at = new Date().toISOString()
    return s
  }
  const s = { id: ++seq.session, user_id: uid, title: sessionTitleFrom(firstMsg), created_at: new Date().toISOString(), updated_at: new Date().toISOString() }
  chatSessions.push(s)
  return s
}
const touchSession = (sid) => {
  const s = chatSessions.find((x) => x.id === sid)
  if (s) s.updated_at = new Date().toISOString()
}

// 会话列表 / 删除（硬删，消息与会话一起删）
routes.push({ method: 'GET', pattern: /^\/api\/v1\/chat\/sessions$/, handler: (url, body, headers) => {
  const uid = userIdFromToken(headers) || 2
  return ok({ items: chatSessions.filter((s) => s.user_id === uid)
    .sort((a, b) => new Date(b.updated_at) - new Date(a.updated_at)) })
}})
routes.push({ method: 'DELETE', pattern: /^\/api\/v1\/chat\/sessions\/(\d+)$/, handler: (url, body, headers) => {
  const uid = userIdFromToken(headers) || 2
  const sid = Number(url.match(/\/chat\/sessions\/(\d+)$/)[1])
  const idx = chatSessions.findIndex((s) => s.id === sid && s.user_id === uid)
  if (idx < 0) return notFound('会话不存在')
  chatSessions.splice(idx, 1)
  for (let i = chatHistory.length - 1; i >= 0; i--) {
    if (chatHistory[i].session_id === sid) chatHistory.splice(i, 1)
  }
  return ok({ deleted: true })
}})

// 首屏一次拿全（可用性 + 欢迎语 + chips），与后端 /chat/bootstrap 同契约。
// 欢迎语由空间事实模板拼装（不调模型）；天数含首日 +1，与 /space 的 days_together 同口径。
routes.push({ method: 'GET', pattern: /^\/api\/v1\/chat\/status$/, handler: () => ok({ available: true, reason: '' }) })

routes.push({ method: 'GET', pattern: /^\/api\/v1\/chat\/bootstrap$/, handler: () => {
  let welcome = '想记点什么？'
  if (space.love_start_date) {
    const days = Math.floor((Date.now() - new Date(space.love_start_date).getTime()) / 86400000) + 1
    const recent = posts.filter((p) => !p.deleted_at && Date.now() - new Date(p.created_at).getTime() < 7 * 86400000).length
    welcome = recent > 0
      ? `今天是你们在一起的第 ${days} 天。这周已经记了 ${recent} 条，想再记点什么？`
      : `今天是你们在一起的第 ${days} 天。想记点什么？`
  }
  return ok({ available: true, reason: '', welcome, chips: ['帮我发动态', '整理最近的照片', '我们记过什么'] })
}})

// 显式中止 → 整轮回滚（取代 withdraw-last）。按 turn_id 精确删，不波及上一轮；
// 该轮未裁决的确认卡置 cancelled（留痕供审计，不可再确认）。意外断线不自动删。
routes.push({ method: 'POST', pattern: /^\/api\/v1\/chat\/turns\/([\w-]+)\/abort$/, handler: (url) => {
  const turn = url.match(/\/chat\/turns\/([\w-]+)\/abort/)[1]
  const before = chatHistory.length
  for (let i = chatHistory.length - 1; i >= 0; i--) {
    if (chatHistory[i].turn_id !== turn) continue
    const cid = chatHistory[i].card_id
    const act = cid ? actions.get(cid) : null
    if (act && !act.resolved) act.resolved = 'cancelled'
    chatHistory.splice(i, 1)
  }
  return ok({ aborted: chatHistory.length < before })
}})

// 历史回显：提案卡读 preview（工具自构造），不回放执行参数。
// tool 行带 tool_text / tool_ui（含 result_id，刷新后仍可勾选引用）。
routes.push({ method: 'GET', pattern: /^\/api\/v1\/chat\/history$/, handler: (url, _body, _headers, u) => {
  const sid = Number(u.searchParams.get('session_id')) || 0
  if (sid <= 0) return ok({ items: [] })
  // parts 契约（AI模式设计「消息 parts 契约」）：一条消息一行，parts 有序数组直出
  const items = []
  for (const h of chatHistory.filter((x) => x.session_id === sid)) {
    if (h.role === 'tool_result') continue
    const base = { id: h.id, role: h.role, parts: h.parts ?? [], created_at: h.created_at, quoted: h.quoted ?? null }
    if (h.card_id) {
      const act = actions.get(h.card_id)
      let status = 'expired'
      let expiresIn = 0
      if (act) {
        if (act.resolved) status = act.resolved === 'cancelled' ? 'cancelled' : (act.resolved === 'failed' ? 'failed' : 'executed')
        else if (Date.now() > act.created_at + ACTION_TTL_MS) status = 'expired'
        else { status = 'pending'; expiresIn = Math.max(1, Math.ceil((act.created_at + ACTION_TTL_MS - Date.now()) / 1000)) }
      }
      items.push({ ...base, card: { action_id: h.card_id, intent: act?.tool || '', summary: act?.preview?.summary || '', preview: act?.preview?.preview || null, warnings: act?.preview?.warnings || [], attachments: act?.preview?.attachments || [], status, expires_in: expiresIn, result: act?.result || '' } })
      continue
    }
    items.push(base)
  }
  if (items.length && items[items.length - 1].role === 'user') items[items.length - 1].interrupted = true
  return ok({ items })
}})

// 语义事件：session（仅懒创建）/ turn.start / thinking.* / answer.* / tool.* / proposal / turn.end。
// quoted 原样落库并回显；结果集句柄挂在 tool.end.ui.result_id。
routes.push({ method: 'POST', pattern: /^\/api\/v1\/chat\/message$/, handler: (url, body, headers) => {
  const uid = userIdFromToken(headers) || 2
  const b = JSON.parse(body || '{}')
  const msg = (b.content || '').trim()
  const attach = Array.isArray(b.media) ? b.media.filter((m) => m && m.data_url) : []
  const mediaRefs = attach.map((m) => ({ name: m.name || '', type: m.type || '', size: m.size || 0 }))
  const created = !(Number(b.session_id) > 0)
  const sess = ensureSession(uid, Number(b.session_id) || 0, msg)
  if (!sess) return notFound('会话不存在')
  const sessionId = sess.id
  touchSession(sessionId)
  const turnId = 't' + randomBytes(4).toString('hex')
  chatHistory.push({ id: ++seq.chat, session_id: sessionId, turn_id: turnId, role: 'user', parts: [{ type: 'text', text: msg }, ...attach.map((a) => ({ type: 'attachment', name: a.name, mime: a.type, size: a.size })), ...(b.quoted ? [{ type: 'quote', refs: b.quoted }] : [])], quoted: b.quoted || null, created_at: new Date().toISOString() })

  // 意图分流：读工具出 tool.end；写意图出 proposal。都不出旧的 text/card/result。
  let answer = null
  let tool = null
  let proposal = null
  const resultId = 'r_' + (seq.action + 1)
  if (/搜索|搜一下|搜搜|查找|查一下|找一下|找找/.test(msg)) {
    const query = msg.replace(/^(帮我|帮忙)?\s*(搜索|搜一下|搜搜|查找|查一下|找一下|找找)(一下)?/, '').replace(/[「」"']/g, '').trim()
    const hits = posts.filter((p) => !p.deleted_at && (!query || (p.content || '').includes(query))).slice(0, 6)
    tool = { label: '搜索动态', text: hits.length ? `找到 ${hits.length} 条动态` : '没有找到内容', ui: { kind: 'record_list', title: '动态', result_id: resultId, selectable: true, records: hits.map((p) => ({ id: p.id, kind: 'post', text: (p.content || '').slice(0, 40) })) } }
    answer = tool.text
  } else if (/建相册|新建相册|创建相册/.test(msg)) {
    const name = msg.match(/(?:建|新建|创建)相册[：:]?\s*(\S+)?/)?.[1] || '新相册'
    proposal = makeProposal('propose_album', `新建相册「${name}」`, { kind: 'album', body: { name, count: 0 } }, { name })
  } else if (msg.includes('纪念日') || msg.includes('周年')) {
    const name = msg.match(/纪念日[：:]\s*(.+?)(?:\s|$)/)?.[1]?.trim() || '新纪念日'
    const date = msg.match(/(\d{4}[-/]\d{1,2}[-/]\d{1,2})/)?.[1] || ''
    proposal = makeProposal('propose_anniversary', `新建纪念日「${name}」`, { kind: 'anniversary', body: { name, date } }, { name, date })
  } else if (msg.includes('动态') || msg.includes('记') || msg.includes('发') || (attach.length && /(相册|发)/.test(msg))) {
    const content = msg.replace(/帮(我|你)(记|发|创建)/, '').trim() || '记录一下今天'
    proposal = makeProposal('propose_post', `发布动态：「${content.slice(0, 20)}」`, { kind: 'post', body: { content, attachments: mediaRefs } }, { content }, mediaRefs)
  } else {
    answer = attach.length
      ? `收到 ${attach.length} 个附件${msg ? `（${msg}）` : ''}。可以说「发条带图动态」或「建个相册」。`
      : '好的。你可以说「帮我记一条动态」或「找找海边的照片」。'
  }
  const thinking = /思考/.test(msg) ? `用户说「${msg}」，我先梳理一下意图再作答。` : ''

  return (res) => {
    res.statusCode = 200
    res.setHeader('Content-Type', 'text/event-stream;charset=utf-8')
    res.setHeader('Cache-Control', 'no-cache')
    const send = (ev, data) => res.write(`event: ${ev}\ndata: ${JSON.stringify(data)}\n\n`)
    if (created) send('session', { session_id: sessionId, title: sess.title })
    send('turn.start', { turn_id: turnId })
    if (thinking) {
      for (const ch of thinking) send('thinking.delta', { text: ch })
      send('thinking.end', { seconds: 1 })
    }
    if (tool) {
      send('tool.start', { call_id: 'c1', label: tool.label })
      send('tool.end', { call_id: 'c1', status: 'ok', text: tool.text, ui: tool.ui })
      chatHistory.push({ id: ++seq.chat, session_id: sessionId, turn_id: turnId, role: 'assistant', parts: [{ type: 'tool', call_id: tool.call_id, label: tool.label || '', status: 'ok', text: tool.text, ui: tool.ui }], created_at: new Date().toISOString() })
    }
    if (proposal) {
      const id = ++seq.action
      actions.set(id, { tool: proposal.intent, params: proposal.params, preview: proposal, user_id: uid, created_at: Date.now(), resolved: null })
      proposal.id = id
      send('proposal', { proposal })
      chatHistory.push({ id: ++seq.chat, session_id: sessionId, turn_id: turnId, role: 'assistant', parts: [{ type: 'card', action_id: id }], card_id: id, created_at: new Date().toISOString() })
      send('turn.end', { stop_reason: 'proposal' })
    } else {
      for (const ch of answer) send('answer.delta', { text: ch })
      send('answer.end', {})
      chatHistory.push({ id: ++seq.chat, session_id: sessionId, turn_id: turnId, role: 'assistant', parts: [{ type: 'text', text: answer }], created_at: new Date().toISOString() })
      send('turn.end', { stop_reason: 'answer' })
    }
    res.end()
  }
}})

function makeProposal(intent, summary, preview, params, attachments) {
  return { intent, summary, preview, warnings: [], attachments: attachments || [], expires_in: ACTION_TTL_SEC, params }
}

// 提案裁决：approve 执行 / cancel 取消。重复 409，过期 410。
routes.push({ method: 'POST', pattern: /^\/api\/v1\/chat\/proposals\/(\d+)\/resolve$/, handler: (url, body) => {
  const id = Number(url.match(/\/proposals\/(\d+)\/resolve/)[1])
  const b = JSON.parse(body || '{}')
  const action = actions.get(id)
  if (!action) return notFound('确认卡片不存在')
  if (action.resolved) return { _s: 409, d: { error: '卡片已处理过（单次有效）' } }
  if (Date.now() > action.created_at + ACTION_TTL_MS) return { _s: 410, d: { error: '确认卡片已过期，请重新发起' } }
  // 入参把关分两段，顺序与真实服务端一致（取消在最前、不受入参把关影响）：
  // · 结构性上限（媒体条数）在**认领之前**拒绝，卡片保持待裁决、可重试——认领是消耗性的，
  //   先认领再发现入参不合法，卡片就被吃掉却从未执行；
  // · 业务规则（相册不收新附件）在**认领之后**判失败，卡片置 failed 并回原因——响应是
  //   200 + status:failed 而非 400，历史回显据此显示「未执行 + 原因」而不是「已执行」。
  if (b.decision !== 'approve') {
    action.resolved = 'cancelled'
    return ok({ status: 'cancelled' })
  }
  const tokens = Array.isArray(b.media_tokens) ? b.media_tokens.map(String) : []
  if (tokens.length > MEDIA_LIMITS.maxMediaPerChatMessage) {
    return badRequest(`单条动态最多 ${MEDIA_LIMITS.maxMediaPerChatMessage} 个媒体文件`)
  }
  if (action.tool === 'propose_album_members' && tokens.length) {
    // 原因写进 action.result：卡片列表（本文件上方 status 映射处）回显的就是它，
    // 不写的话刷新后只剩「失败」两个字、看不到为什么。
    action.resolved = 'failed'
    action.result = '新附件不能直接加入相册：媒体只能随动态发布，请先发布动态再选入相册'
    return ok({ status: 'failed', error: action.result })
  }
  action.resolved = 'executed'
  const done = (result) => { action.result = result; return ok({ status: 'executed', result }) }
  if (action.tool === 'propose_post') {
    const pid = ++seq.post
    const media = tokens.map((tk) => ({ id: ++seq.media, post_id: pid, kind: uploads.get(tk) || 'photo', thumb_url: `/api/v1/media/file/${seq.media}?v=thumb` }))
    posts.unshift({ id: pid, author_id: action.user_id, content: action.params.content || '', mood: action.params.mood || '', media, created_at: new Date().toISOString() })
    return done(`动态 #${pid} 已发布`)
  }
  if (action.tool === 'propose_anniversary') {
    anniversaries.push({ id: ++seq.anniversary, name: action.params.name || '新纪念日', date: action.params.date || '2024-01-01', type: 'solar', remind_days_before: 1, created_by: action.user_id })
    return done(`纪念日「${action.params.name}」已创建`)
  }
  if (action.tool === 'propose_album') {
    albums.push({ id: ++seq.album, name: action.params.name || '新相册', description: '', cover_url: '', item_count: 0, created_by: action.user_id })
    return done(`相册「${action.params.name}」已创建`)
  }
  return done('已执行')
}})


// --- Map ---
// 瓦片短时签名：与真实服务端口径一致——只绑 exp 的空间级签名（一次覆盖全部坐标）
const TILE_GRANT_TTL_SEC = 1800
const TILE_SIG_SECRET = 'mock-tile-secret'
const tileSig = (exp) => createHmac('sha256', TILE_SIG_SECRET).update(`map:tile:${exp}`).digest('hex')

// APP 配置：下发「使用中」的源与瓦片短时签名（provider/tile_api + exp/sig，无密钥、不含 URL 模板）
routes.push({ method: 'GET', pattern: /^\/api\/v1\/map\/config$/, handler: () => {
  const s = mapSources.find((x) => x.enabled)
  const exp = Math.floor(Date.now() / 1000) + TILE_GRANT_TTL_SEC
  const out = { provider: s?.type ?? '', tile_api: '/api/v1/map/tile', exp, sig: tileSig(exp) }
  return ok(out)
} })

// admin 瓦片源多卡片（密钥只存服务端、永不下发：回读仅 has_key 标记）
const sourceOut = (s) => {
  const { tianditu_key, ...rest } = s
  return { ...rest, has_key: Boolean(tianditu_key) }
}
const findSource = (url) => mapSources.find((x) => x.id === parseIntParam(url, /\/map-config\/(\d+)/))

routes.push({ method: 'GET', pattern: /^\/api\/v1\/admin\/map-config$/, handler: () => ok({ items: mapSources.map(sourceOut) }) })

routes.push({ method: 'POST', pattern: /^\/api\/v1\/admin\/map-config$/, handler: (url, body) => {
  const b = JSON.parse(body || '{}')
  if (b.type !== 'public' && b.type !== 'tianditu') return badRequest('type 必须为 public 或 tianditu')
  if (b.type === 'public' && !(b.url_template || '').includes('{z}')) return badRequest('公开瓦片源必须提供含 {z}/{x}/{y} 占位的地址模板')
  if (b.type === 'tianditu' && !b.tianditu_key) return badRequest('天地图源必须提供 tk 密钥')
  const s = { id: ++seq.mapSource, type: b.type, url_template: b.url_template || '', tianditu_key: b.tianditu_key || '', tianditu_layer: b.tianditu_layer || 'vec_w', enabled: false, created_at: new Date().toISOString() }
  mapSources.push(s)
  return created(sourceOut(s))
}})

routes.push({ method: 'DELETE', pattern: /^\/api\/v1\/admin\/map-config\/\d+$/, handler: (url) => {
  const s = findSource(url)
  if (!s) return notFound()
  if (s.enabled) return badRequest('使用中的源不可删除，请先切换其他源')
  mapSources.splice(mapSources.indexOf(s), 1)
  return noContent()
}})

routes.push({ method: 'PUT', pattern: /^\/api\/v1\/admin\/map-config\/\d+\/enable$/, handler: (url) => {
  const s = findSource(url)
  if (!s) return notFound()
  if (s.type === 'public' && !s.url_template) return badRequest('启用前需先填写瓦片地址模板')
  if (s.type === 'tianditu' && !s.tianditu_key) return badRequest('启用前需先配置天地图密钥')
  mapSources.forEach((x) => { x.enabled = x.id === s.id })
  return ok(sourceOut(s))
}})

routes.push({ method: 'GET', pattern: /^\/api\/v1\/map\/tile$/, handler: (url) => {
  const q = queryParams(url)
  const exp = Number(q.exp)
  if (!exp || exp < Math.floor(Date.now() / 1000) || q.sig !== tileSig(exp)) {
    return unauthorized('签名无效或已过期')
  }
  const z = q.z || 4
  const x = q.x || 13
  const y = q.y || 5
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="256" height="256">` +
    `<rect width="100%" height="100%" fill="#eef3e8"/>` +
    `<text x="50%" y="50%" font-size="14" fill="#999" text-anchor="middle" dominant-baseline="middle">${z}/${x}/${y}</text>` +
    `<rect x="0" y="0" width="256" height="256" fill="none" stroke="#ccc" stroke-width="0.5"/>` +
    `</svg>`
  // 非 JSON 响应：直接返回写 response 的函数
  return (res) => {
    res.setHeader('Content-Type', 'image/svg+xml;charset=utf-8')
    res.setHeader('Cache-Control', 'public, max-age=86400')
    res.statusCode = 200
    res.end(svg)
  }
}})

// 地理编码：GET /api/v1/geocode?q=杭州 → 候选列表（内置离线表，无第三方依赖）
routes.push({ method: 'GET', pattern: /^\/api\/v1\/geocode$/, handler: (url) => {
  const q = (queryParams(url).q || '').trim().toLowerCase()
  if (!q) return ok({ candidates: [] })
  const candidates = GEOCODE_PLACES
    .filter((p) => p.name.toLowerCase().includes(q) || p.city.toLowerCase().includes(q) || p.display.toLowerCase().includes(q))
    .slice(0, 8)
    .map((p) => ({ name: p.name, lat: p.lat, lng: p.lng, display: p.display }))
  return ok({ candidates })
}})

// A7 地图点位端点（无 merged_into；同名复用，坐标以先创建者为准）
routes.push({ method: 'GET', pattern: /^\/api\/v1\/map\/points$/, handler: (url) => {
  const list = mapPoints.filter((p) => p.deleted_at === null).slice().sort((a, b) => new Date(b.created_at) - new Date(a.created_at))
  return ok({ items: list, total: list.length })
}})

routes.push({ method: 'POST', pattern: /^\/api\/v1\/map\/points$/, handler: (url, body, headers) => {
  const uid = userIdFromToken(headers)
  if (!uid) return unauthorized()
  const b = JSON.parse(body || '{}')
  const name = (b.name || '').trim()
  if (!name) return badRequest('name 不能为空')
  // 同名复用：返回已有点位（200 不新建）；不同名 → 建点
  const hit = mapPoints.find((p) => p.deleted_at === null && p.name === name)
  if (hit) return ok(hit)
  // source 服务端判定：REST 手动创建恒为 manual
  const p = { id: ++seq.mapPoint, name, lat: Number(b.lat) || 0, lng: Number(b.lng) || 0, visit_date: b.visit_date || null, description: b.description || '', source: 'manual', created_at: new Date().toISOString(), deleted_at: null, created_by: uid }
  mapPoints.push(p)
  // 足迹更新：手动建点通知另一方（同名复用静默；动态带地点自动建点不发，已有 post_created）
  for (const r of partnerIds(uid)) addNotification(r, 'map_point_created', 'map_point', p.id, { operator: operatorName(uid), name: p.name, latitude: p.lat, longitude: p.lng })
  return created(p)
}})

routes.push({ method: 'DELETE', pattern: /^\/api\/v1\/map\/points\/\d+$/, handler: (url, body, headers) => {
  const id = parseIntParam(url, /\/map\/points\/(\d+)/)
  if (!id) return notFound()
  const p = mapPoints.find((x) => x.id === id)
  if (!p || p.deleted_at) return notFound()
 // 删除对方创建的足迹 → 通知创建者（自删静默；内容删除仅站内）
  const uid = userIdFromToken(headers)
  if (uid && p.created_by && p.created_by !== uid) {
    addNotification(p.created_by, 'map_point_deleted', 'map_point', id, { name: p.name, deleted_by: operatorName(uid) })
  }
  // mock 软删除：标记 deleted_at
  p.deleted_at = new Date().toISOString()
  return noContent()
}})

routes.push({ method: 'GET', pattern: /^\/api\/v1\/map\/points\/cities$/, handler: () => {
  // 城市统计：从点位 name 第一段（地名前缀·城市名）抽取。mock 简化按「·」前作城市。
  const live = mapPoints.filter((p) => p.deleted_at === null)
  const m = new Map()
  for (const p of live) {
    const city = (p.name.split('·')[0] || p.name).trim()
    m.set(city, (m.get(city) || 0) + 1)
  }
  const items = [...m.entries()].map(([city, count]) => ({ city, count })).sort((a, b) => b.count - a.count)
  return ok({ items, total: items.length })
}})

// ---------- 管理后台（admin） ----------
// 取路径末段数字为 id（v1 版本号自带数字，必须取末段）
const idOf = (url) => { const m = url.split('?')[0].match(/\d+/g); return m ? Number(m[m.length - 1]) : NaN }
const nowText = () => new Date().toISOString().slice(0, 19).replace('T', ' ')

// 空间成员：单空间三角色（admin/girl/boy）；情侣账户必须一次性同时创建，建立后仅可改昵称/重置密码
routes.push({ method: 'GET', pattern: /^\/api\/v1\/admin\/users$/, handler: () => ok(adminUsers) })

// 情侣账户原子创建：双方信息一次提交，任一情侣角色已存在则拒绝（规则由服务端强制）
// 不提供单人创建端点：情侣两方必须一次性建出，与 PRODUCT「后台管理」一致。
routes.push({ method: 'POST', pattern: /^\/api\/v1\/admin\/users\/couple$/, handler: (url, body) => {
  const b = JSON.parse(body || '{}')
  const girl = b.girl || {}
  const boy = b.boy || {}
  if (!girl.username || !girl.password || !boy.username || !boy.password) {
    return badRequest('双方 username/password 均必填')
  }
  if (girl.username === boy.username) return badRequest('双方用户名不能相同')
  // 格式校验先于存在性判断：情侣已存在时格式错误的入参也应得到格式错误
  for (const p of [girl, boy]) {
    if (p.username.length < 2 || p.username.length > 32) return badRequest('用户名长度 2-32 个字符')
    if (p.password.length < 6) return badRequest('密码强度不足：至少 6 位')
  }
  if (adminUsers.some((u) => u.role === 'girl' || u.role === 'boy')) {
    return badRequest('情侣账户已存在，仅可修改昵称或重置密码')
  }
  if (adminUsers.some((u) => u.username === girl.username || u.username === boy.username)) {
    return badRequest('用户名已存在')
  }
  const createdUsers = [
    { id: ++seq.user, username: girl.username, role: 'girl', nickname: girl.nickname || '' },
    { id: ++seq.user, username: boy.username, role: 'boy', nickname: boy.nickname || '' },
  ]
  adminUsers.push(...createdUsers)
  return created({ girl: createdUsers[0], boy: createdUsers[1] })
} })

// 仅允许更新昵称；用户名/角色为登录身份与数据归属，不可修改
routes.push({ method: 'PUT', pattern: /^\/api\/v1\/admin\/users\/\d+$/, handler: (url, body) => {
  const user = adminUsers.find((u) => u.id === idOf(url))
  if (!user) return notFound('用户不存在')
  const b = JSON.parse(body || '{}')
  if (typeof b.nickname !== 'string' || !b.nickname.trim()) return badRequest('nickname 必填')
  user.nickname = b.nickname.trim()
  return ok(user)
} })

routes.push({ method: 'POST', pattern: /^\/api\/v1\/admin\/users\/\d+\/reset-password$/, handler: () => ok({}) })

// 头像上传（mock 仅回执，不持久化文件）
routes.push({ method: 'POST', pattern: /^\/api\/v1\/admin\/users\/\d+\/avatar$/, handler: (url) => {
  const user = adminUsers.find((u) => u.id === idOf(url))
  if (!user) return notFound('用户不存在')
  user.avatar = `/api/v1/avatar/${user.id}?exp=0&sig=mock`
  return ok({ status: 'ok', avatar: user.avatar })
} })

// 生日（仅后台维护，空串清除）
routes.push({ method: 'PUT', pattern: /^\/api\/v1\/admin\/users\/\d+\/birthday$/, handler: (url, body) => {
  const user = adminUsers.find((u) => u.id === idOf(url))
  if (!user) return notFound('用户不存在')
  const b = JSON.parse(body || '{}')
  if (b.birthday && !/^\d{4}-\d{2}-\d{2}$/.test(b.birthday)) return badRequest('生日格式应为 YYYY-MM-DD')
  user.birthday = b.birthday || ''
  return ok({ status: 'ok' })
} })

// 头像公开读取（签名即凭证，mock 不校验签名）
routes.push({ method: 'GET', pattern: /^\/api\/v1\/avatar\/\d+$/, handler: () => {
  // 占位 1x1 PNG，前端真实展示由 SignedURL 驱动
  return {
    status: 200,
    headers: { 'content-type': 'image/png' },
    body: Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==', 'base64'),
  }
} })

// 通知渠道：CRUD（ARCHITECTURE 第 9 章：GET/POST/PUT/DELETE + test）
routes.push({ method: 'GET', pattern: /^\/api\/v1\/admin\/webhooks$/, handler: () => ok({ items: webhooks }) })
routes.push({ method: 'POST', pattern: /^\/api\/v1\/admin\/webhooks$/, handler: (url, body) => {
  const b = JSON.parse(body || '{}')
  if (!b.url) return badRequest('url 必填')
  const item = { id: ++seq.webhook, name: '', method: 'POST', headers: {}, body_template: '', events: [], ...b }
  webhooks.push(item)
  return created(item)
} })
routes.push({ method: 'PUT', pattern: /^\/api\/v1\/admin\/webhooks\/\d+$/, handler: (url, body) => {
  const item = webhooks.find((w) => w.id === idOf(url))
  if (!item) return notFound('渠道不存在')
  const b = JSON.parse(body || '{}')
  if ('url' in b && !b.url) return badRequest('url 必填')
  Object.assign(item, b)
  return ok(item)
} })
routes.push({ method: 'DELETE', pattern: /^\/api\/v1\/admin\/webhooks\/\d+$/, handler: (url) => {
  const i = webhooks.findIndex((w) => w.id === idOf(url))
  if (i < 0) return notFound('渠道不存在')
  webhooks.splice(i, 1)
  return noContent()
} })
routes.push({ method: 'POST', pattern: /^\/api\/v1\/admin\/webhooks\/\d+\/test$/, handler: (url) => {
  if (!webhooks.some((w) => w.id === idOf(url))) return notFound('渠道不存在')
  return ok({})
} })

// 仪表盘：内容统计 / AI 调用量与 Token / 异常告警（ARCHITECTURE 第 9 章三项）
routes.push({ method: 'GET', pattern: /^\/api\/v1\/admin\/dashboard\/stats$/, handler: () => ok(dashboardStats) })
routes.push({ method: 'GET', pattern: /^\/api\/v1\/admin\/dashboard\/activity$/, handler: () => ok(recentActivity) })
routes.push({ method: 'GET', pattern: /^\/api\/v1\/admin\/dashboard\/ai-usage$/, handler: () => ok({ items: aiUsage }) })
routes.push({ method: 'GET', pattern: /^\/api\/v1\/admin\/dashboard\/alerts$/, handler: () => ok({ items: alerts }) })

// AI 供应商（协议化配置）：列表有序 = 故障切换链（从第一个开始、失败依次向下）
// 模型清单只列现役代：退市模型配了也调不通，留着只会误导（与真实供应商的可用清单同步）。
const API_PROTOCOLS = {
  'openai-chat': ['gpt-5.5', 'gpt-5.5-mini', 'deepseek-chat', 'kimi-k2', 'qwen3-max'],
  'openai-responses': ['gpt-5.5', 'gpt-5.5-mini', 'gpt-5', 'o4-mini'],
  'anthropic': ['claude-fable-5-1', 'claude-opus-5-5', 'claude-sonnet-5-5', 'claude-haiku-5-5'],
  'gemini': ['gemini-3.1-pro-preview', 'gemini-3.5-flash'],
}
const protocolInvalid = (t) => badRequest('api_type 不支持（openai-chat / openai-responses / anthropic / gemini）')

// THINKING_ORDER 档位规范顺序，同时是集合排序与成员判断的依据。
const THINKING_ORDER = ['off', 'low', 'medium', 'high']

// normalizeThinking 归一单个思考档位：空串 → 'off'（缺省只有关）；非法取值 → null（调用方报错）。
// 注意与 normalizeThinkingLevels 的区别：那是**集合**（逗号分隔、恒含 off），这是**单值**——
// 单值走集合归一会被拼成 'off,medium' 这种带逗号的串，后续成员判断永远不成立。
const normalizeThinking = (v) => {
  if (v === undefined || v === null || String(v).trim() === '') return 'off'
  const p = String(v).trim()
  return THINKING_ORDER.includes(p) ? p : null
}

// normalizeThinkingLevels 归一思考档位集合：去重、按 off/low/medium/high 固定顺序拼接、恒含 off。
// 非法取值 → null（调用方报错）；空串 → 'off'（缺省只有关，与服务端同口径）。
const normalizeThinkingLevels = (v) => {
  if (v === undefined || v === null || String(v).trim() === '') return 'off'
  const set = new Set()
  for (const part of String(v).split(',')) {
    const p = part.trim()
    if (p === '') continue
    if (!THINKING_ORDER.includes(p)) return null
    set.add(p)
  }
  if (set.size === 0) return 'off'
  set.add('off')
  return THINKING_ORDER.filter((lv) => set.has(lv)).join(',')
}

// models 条目校验：{ id 必填, kind=chat|embedding, max_tokens|null, thinking, thinking_levels, context_window|null }
// 参数挂条目级，null = 调用不传。不设 temperature：现役模型的思考参数恒开，与它互斥、同传即 400。
// checkThinkingAllowed 思考档位与协议的一致性（与 server 同口径的服务端校验）。
// OpenAI Chat 协议没有统一的思考参数（是否回思考由供应商自定），故该协议下不允许配置档位：
// 声明集与所选档位都只能是 off。只在下拉上限制不够——接口可被直接调用，
// 写进去的值最终会被 adapter 忽略，形成「配了不生效」的静默错觉。
const checkThinkingAllowed = (apiType, models) => {
  if (apiType !== 'openai-chat') return null
  for (const m of models) {
    if ((m.thinking ?? 'off') !== 'off' || (m.thinking_levels ?? 'off') !== 'off') {
      return 'OpenAI Chat 协议没有统一的思考参数，思考档位只能为关'
    }
  }
  return null
}

// 嵌入只允许挂 openai-chat 供应商（与 server 同口径）。返回首个越协议的嵌入条目，无则 null。
const embeddingOffProtocol = (apiType, models) => {
  if (apiType === 'openai-chat') return null
  return models.find((m) => m.kind === 'embedding') ?? null
}

const normalizedModels = (raw) => {
  if (!Array.isArray(raw)) return { error: 'models 必填（数组）' }
  const entries = []
  for (const m of raw) {
    const id = String(m?.id ?? '').trim()
    if (!id) return { error: 'models 中存在模型名为空的条目' }
    const kind = String(m?.kind ?? '').trim()
    if (kind !== 'chat' && kind !== 'embedding') return { error: `模型 ${id} 的 kind 需为 chat 或 embedding` }
    // ''/undefined 一律先归一为 null（不设置），再做区间校验
    const mt = m.max_tokens === '' || m.max_tokens === undefined ? null : m.max_tokens
    if (mt !== null && (!Number.isInteger(Number(mt)) || Number(mt) < 256 || Number(mt) > 32768)) return { error: `模型 ${id} 的 max_tokens 需为 256~32768 整数，或不设置` }
    const cw = m.context_window === '' || m.context_window === undefined ? null : m.context_window
    if (cw !== null && (!Number.isInteger(Number(cw)) || Number(cw) < 1024 || Number(cw) > 2000000)) return { error: `模型 ${id} 的 context_window 需为 1024~2000000 整数，或不设置` }
    if (mt !== null && cw !== null && Number(mt) > Number(cw)) return { error: `模型 ${id} 的 max_tokens 不得超过 context_window` }
    const thinking = normalizeThinking(m.thinking)
    if (thinking === null) return { error: `模型 ${id} 的 thinking 需为 off / low / medium / high` }
    const levels = normalizeThinkingLevels(m.thinking_levels)
    if (levels === null) return { error: `模型 ${id} 的 thinking_levels 需为 off / low / medium / high 的逗号分隔子集` }
    if (!levels.split(',').includes(thinking)) return { error: `模型 ${id} 的 thinking 需在 thinking_levels 声明的档位内` }
    entries.push({ id, kind, max_tokens: mt === null ? null : Number(mt), thinking,
      thinking_levels: levels, context_window: cw === null ? null : Number(cw) })
  }
  return { entries }
}

// 模型链视图：从供应商条目摊平出两条链（数组顺序即链序）
const modelChains = () => {
  const out = { chat: [], embedding: [] }
  for (const p of modelProviders) {
    for (const m of p.models) {
      if (!out[m.kind]) continue
      out[m.kind].push({
        id: m.row_id ?? `${p.id}:${m.id}:${m.kind}`,
        provider_id: p.id, provider_name: p.name, api_type: p.api_type,
        kind: m.kind, model_id: m.id, max_tokens: m.max_tokens, thinking: m.thinking ?? 'off',
        thinking_levels: m.thinking_levels ?? 'off', context_window: m.context_window ?? null,
      })
    }
  }
  return out
}

routes.push({ method: 'GET', pattern: /^\/api\/v1\/admin\/model-providers$/, handler: () => ok(modelProviders) })

// 模型链（chat / embedding 两条）：数组顺序即链序
routes.push({ method: 'GET', pattern: /^\/api\/v1\/admin\/models$/, handler: () => ok(modelChains()) })

routes.push({ method: 'POST', pattern: /^\/api\/v1\/admin\/models$/, handler: (url, body) => {
  const b = JSON.parse(body || '{}')
  const provider = modelProviders.find((p) => p.id === Number(b.provider_id))
  if (!provider) return notFound('供应商不存在')
  if (b.kind !== 'chat' && b.kind !== 'embedding') return badRequest('kind 需为 chat 或 embedding')
  if (embeddingOffProtocol(provider.api_type, [{ kind: b.kind }])) return badRequest('嵌入模型只能挂 openai-chat 供应商')
  const m = normalizedModels([{ id: b.model_id, kind: b.kind, max_tokens: b.max_tokens,
    thinking: b.thinking, thinking_levels: b.thinking_levels, context_window: b.context_window }])
  if (m.error) return badRequest(m.error)
  const terr = checkThinkingAllowed(provider.api_type, m.entries)
  if (terr) return badRequest(terr)
  provider.models.push(m.entries[0])
  return created(modelChains())
} })

// 链内重排：kind + 有序 id 数组（须恰好覆盖该 kind 全部条目）
routes.push({ method: 'PUT', pattern: /^\/api\/v1\/admin\/models\/order$/, handler: (url, body) => {
  const b = JSON.parse(body || '{}')
  const chain = modelChains()[b.kind]
  if (!chain) return badRequest('kind 需为 chat 或 embedding')
  const ids = (b.ids ?? []).map((x) => String(x))
  if (ids.length !== chain.length || !ids.every((id) => chain.some((r) => String(r.id) === id)) || new Set(ids).size !== ids.length) {
    return badRequest('ids 必须恰好包含该类型的全部模型条目')
  }
  const byId = Object.fromEntries(chain.map((r) => [String(r.id), r]))
  for (const p of modelProviders) {
    p.models = p.models.filter((m) => m.kind !== b.kind)
  }
  for (const id of ids) {
    const row = byId[id]
    const provider = modelProviders.find((p) => p.id === row.provider_id)
    provider.models.push({ id: row.model_id, kind: row.kind, max_tokens: row.max_tokens,
      thinking: row.thinking, thinking_levels: row.thinking_levels, context_window: row.context_window })
  }
  return ok(modelChains())
} })

routes.push({ method: 'PUT', pattern: /^\/api\/v1\/admin\/models\/[^/]+$/, handler: (url, body) => {
  const rowId = decodeURIComponent(url.split('/').pop())
  const chains = modelChains()
  let hit = null
  for (const kind of ['chat', 'embedding']) {
    const row = chains[kind].find((r) => String(r.id) === rowId)
    if (row) { hit = { row, kind }; break }
  }
  if (!hit) return notFound('模型条目不存在')
  const b = JSON.parse(body || '{}')
  const provider = modelProviders.find((p) => p.id === hit.row.provider_id)
  const entry = provider.models.find((m) => m.id === hit.row.model_id && m.kind === hit.kind)
  if (!entry) return notFound('模型条目不存在')
  const modelId = String(b.model_id ?? hit.row.model_id).trim() || hit.row.model_id
  if (modelId !== entry.id && provider.models.some((m) => m.kind === entry.kind && m.id === modelId)) {
    return badRequest('该供应商已有同名同类型模型条目')
  }
  // 部分更新：model_id / thinking / thinking_levels 未传则保留原值。
  // max_tokens / context_window 是**整值替换**——省略或 null 都落 null（清空），
  // 因为这两个字段要支持「清掉已配的上限」；admin 表单每次提交都带全量字段。
  // 不能把 null 送进 Number()：Number(null) === 0，会被区间校验当越界值拒掉，
  // 于是同一份表单在 mock 上改不了、在 server 上却成功（本地能存的数据 server 会拒）。
  const next = {
    id: modelId, kind: entry.kind,
    max_tokens: b.max_tokens === undefined ? null : b.max_tokens,
    thinking: b.thinking === undefined ? entry.thinking : b.thinking,
    thinking_levels: b.thinking_levels === undefined ? entry.thinking_levels : b.thinking_levels,
    context_window: b.context_window === undefined ? null : b.context_window,
  }
  const v = normalizedModels([next])
  if (v.error) return badRequest(v.error)
  const terr = checkThinkingAllowed(provider.api_type, v.entries)
  if (terr) return badRequest(terr)
  // 校验通过才落库：entry 是内存共享对象，先改名再校验失败会留下改了一半的条目。
  Object.assign(entry, v.entries[0])
  return ok(modelChains())
} })

routes.push({ method: 'DELETE', pattern: /^\/api\/v1\/admin\/models\/[^/]+$/, handler: (url) => {
  const rowId = decodeURIComponent(url.split('/').pop())
  for (const p of modelProviders) {
    const i = p.models.findIndex((m) => `${p.id}:${m.id}:${m.kind}` === rowId)
    if (i >= 0) {
      p.models.splice(i, 1)
      return noContent()
    }
  }
  return notFound('模型条目不存在')
} })

// 获取模型列表：真实实现为服务端持地址+秘钥调供应商 models 接口；mock 按协议返回预置列表。
// 已保存过秘钥的供应商可只传 provider_id（服务端用存储秘钥），否则必须带 api_key。
routes.push({ method: 'POST', pattern: /^\/api\/v1\/admin\/model-providers\/fetch-models$/, handler: (url, body) => {
  const b = JSON.parse(body || '{}')
  if (!API_PROTOCOLS[b.api_type]) return protocolInvalid()
  if (!b.base_url) return badRequest('base_url 必填')
  const viaSaved = b.provider_id && modelProviders.find((p) => p.id === Number(b.provider_id) && p.has_key)
  if (!viaSaved && !b.api_key) return badRequest('api_key 必填（或使用已保存秘钥的供应商）')
  return ok({ models: API_PROTOCOLS[b.api_type] })
} })

routes.push({ method: 'POST', pattern: /^\/api\/v1\/admin\/model-providers$/, handler: (url, body) => {
  const b = JSON.parse(body || '{}')
  if (!API_PROTOCOLS[b.api_type]) return protocolInvalid()
  if (!b.base_url) return badRequest('base_url 必填')
  if (!b.api_key) return badRequest('api_key 必填')
  const m = normalizedModels(b.models ?? [])
  if (m.error) return badRequest(m.error)
  const terr = checkThinkingAllowed(b.api_type, m.entries)
  if (terr) return badRequest(terr)
  if (embeddingOffProtocol(b.api_type, m.entries)) return badRequest('嵌入模型只能挂 openai-chat 供应商')
  const item = {
    id: ++seq.provider,
    api_type: b.api_type,
    name: (b.name || '').trim() || b.api_type,
    base_url: b.base_url,
    has_key: true,
    models: m.entries,
  }
  modelProviders.push(item)
  return created(item)
} })

// 编辑：api_key 不传 = 不修改（只写不读）
routes.push({ method: 'PUT', pattern: /^\/api\/v1\/admin\/model-providers\/\d+$/, handler: (url, body) => {
  const item = modelProviders.find((p) => p.id === idOf(url))
  if (!item) return notFound('供应商不存在')
  const b = JSON.parse(body || '{}')
  // 先校验再落库：item 是内存里的共享对象，就地改完才发现不合法会把状态改坏
  // （校验失败要原样保留，请求返回 400 后前端会重新拉列表）。
  if (b.api_type !== undefined && !API_PROTOCOLS[b.api_type]) return protocolInvalid()
  if (b.base_url !== undefined && !b.base_url) return badRequest('base_url 不能为空')
  const nextApiType = b.api_type !== undefined ? b.api_type : item.api_type
  let nextModels = item.models
  if (b.models !== undefined) {
    const m = normalizedModels(b.models)
    if (m.error) return badRequest(m.error)
    nextModels = m.entries
  }
  // 协议改离 openai-chat 时，名下已有嵌入条目要拦住（嵌入只允许 openai-chat）。
  // 与 server 同形：这里查的是「原样留下的旧条目」，随请求替换的条目在下面单独查。
  if (b.api_type !== undefined && nextApiType !== 'openai-chat' && embeddingOffProtocol(nextApiType, item.models)) {
    return badRequest('该供应商名下有嵌入模型，请先移出后再改协议（嵌入只允许 openai-chat）')
  }
  // 改协议且不带 models 时，名下旧条目按新协议复校：否则会把该协议
  // 根本不认的档位留在库里（admin 的供应商表单正是只提交 api_type/name/base_url）。
  const terr = checkThinkingAllowed(nextApiType, nextModels)
  if (terr) return badRequest(terr)
  if (embeddingOffProtocol(nextApiType, nextModels)) return badRequest('嵌入模型只能挂 openai-chat 供应商')

  if (b.api_type !== undefined) item.api_type = b.api_type
  if (b.name !== undefined) item.name = String(b.name).trim() || item.name
  if (b.base_url !== undefined) item.base_url = b.base_url
  if (b.models !== undefined) item.models = nextModels
  return ok(item)
} })

routes.push({ method: 'PUT', pattern: /^\/api\/v1\/admin\/model-providers\/\d+\/key$/, handler: (url, body) => {
  const item = modelProviders.find((p) => p.id === idOf(url))
  if (!item) return notFound('供应商不存在')
  const b = JSON.parse(body || '{}')
  if (!b.api_key) return badRequest('api_key 必填')
  item.has_key = true // 密钥加密存储、只写不读，mock 不落明文
  return ok(item)
} })

routes.push({ method: 'DELETE', pattern: /^\/api\/v1\/admin\/model-providers\/\d+$/, handler: (url) => {
  const i = modelProviders.findIndex((p) => p.id === idOf(url))
  if (i < 0) return notFound('供应商不存在')
  modelProviders.splice(i, 1)
  return noContent()
} })
routes.push({ method: 'GET', pattern: /^\/api\/v1\/admin\/system-prompt$/, handler: () => ok(systemPrompt) })
routes.push({ method: 'PUT', pattern: /^\/api\/v1\/admin\/system-prompt$/, handler: (url, body) => {
  const b = JSON.parse(body || '{}')
  if (typeof b.content !== 'string' || !b.content.trim()) return badRequest('content 必填')
  systemPrompt.content = b.content
  systemPrompt.updatedAt = nowText()
  return ok(systemPrompt)
} })
// 输出风格库：内置固化（不可删改），自定义后台管理；default = 全局默认（APP 未设置风格时使用）
// 用户端只读接口（APP 设置页风格选项；与 admin 接口同源数据）
routes.push({ method: 'GET', pattern: /^\/api\/v1\/styles$/, handler: () => ok({ items: styles, default_style_id: styleDefaults.id }) })
routes.push({ method: 'GET', pattern: /^\/api\/v1\/admin\/styles$/, handler: () => ok({ items: styles, default_style_id: styleDefaults.id }) })
routes.push({ method: 'POST', pattern: /^\/api\/v1\/admin\/styles$/, handler: (url, body) => {
  const b = JSON.parse(body || '{}')
  if (!b.name || !String(b.name).trim()) return badRequest('name 必填')
  if (!b.prompt || !String(b.prompt).trim()) return badRequest('prompt 必填')
  const name = String(b.name).trim()
  if (styles.some((s) => s.name === name)) return badRequest('风格名称已存在')
  const item = { id: ++seq.style, name, prompt: String(b.prompt), builtin: false }
  styles.push(item)
  return created(item)
} })
routes.push({ method: 'PUT', pattern: /^\/api\/v1\/admin\/styles\/default$/, handler: (url, body) => {
  const b = JSON.parse(body || '{}')
  const s = styles.find((x) => x.id === Number(b.id))
  if (!s) return notFound('风格不存在')
  styleDefaults.id = s.id
  return ok({ items: styles, default_style_id: styleDefaults.id })
} })
routes.push({ method: 'PUT', pattern: /^\/api\/v1\/admin\/styles\/\d+$/, handler: (url, body) => {
  const item = styles.find((x) => x.id === idOf(url))
  if (!item) return notFound('风格不存在')
  if (item.builtin) return badRequest('内置风格不可修改')
  const b = JSON.parse(body || '{}')
  if (b.name !== undefined) {
    if (!String(b.name).trim()) return badRequest('name 不能为空')
    item.name = String(b.name).trim()
  }
  if (b.prompt !== undefined) {
    if (!String(b.prompt).trim()) return badRequest('prompt 不能为空')
    item.prompt = String(b.prompt)
  }
  return ok(item)
} })
routes.push({ method: 'DELETE', pattern: /^\/api\/v1\/admin\/styles\/\d+$/, handler: (url) => {
  const i = styles.findIndex((x) => x.id === idOf(url))
  if (i < 0) return notFound('风格不存在')
  if (styles[i].builtin) return badRequest('内置风格不可删除')
  if (styles[i].id === styleDefaults.id) return badRequest('默认风格不可删除，请先将其他风格设为默认')
  styles.splice(i, 1)
  return noContent()
} })

// APP 端风格列表（内置 + 自定义并集）+ 全局默认标记；用户级选择走既有 /auth/me ai_style 与 PATCH /me
routes.push({ method: 'GET', pattern: /^\/api\/v1\/styles$/, handler: () => ok({ items: styles, default_style_id: styleDefaults.id }) })

// MCP 工具：列表 + 启停（参数 Schema 随列表下发，详情前端展开）
routes.push({ method: 'GET', pattern: /^\/api\/v1\/admin\/mcp-tools$/, handler: () => ok(tools) })
routes.push({ method: 'PATCH', pattern: /^\/api\/v1\/admin\/mcp-tools\/[\w]+$/, handler: (url, body) => {
  const name = decodeURIComponent(url.split('?')[0].split('/').pop())
  const tool = tools.find((t) => t.name === name)
  if (!tool) return notFound('工具不存在')
  const b = JSON.parse(body || '{}')
  if (typeof b.enabled !== 'boolean') return badRequest('enabled 必填（boolean）')
  tool.enabled = b.enabled
  return ok(tool)
} })

// 回收站：单条恢复 / 批量恢复统一走 restoreItems，均写回收站操作日志（恢复 + 系统超期永久删除，永久保留）
routes.push({ method: 'GET', pattern: /^\/api\/v1\/admin\/recycle$/, handler: () => ok(recycle) })
function restoreItems(ids) {
  const restored = []
  for (const id of ids) {
    const i = recycle.findIndex((r) => r.id === id)
    if (i < 0) return { error: '条目不存在：' + id }
    const [item] = recycle.splice(i, 1)
    restoreLog.unshift({ time: nowText(), operator: 'admin', summary: item.type + '"' + item.summary + '"' })
    restored.push(item)
  }
  return { restored }
}
routes.push({ method: 'POST', pattern: /^\/api\/v1\/admin\/recycle\/restore$/, handler: (url, body) => {
  const b = JSON.parse(body || '{}')
  if (!Array.isArray(b.ids) || b.ids.length === 0) return badRequest('ids 必填（非空数组）')
  const r = restoreItems(b.ids.map(Number))
  if (r.error) return notFound(r.error)
  return ok({ restored: r.restored.length })
} })
routes.push({ method: 'POST', pattern: /^\/api\/v1\/admin\/recycle\/\d+\/restore$/, handler: (url) => {
  const r = restoreItems([idOf(url)])
  if (r.error) return notFound(r.error)
  return ok({})
} })
routes.push({ method: 'GET', pattern: /^\/api\/v1\/admin\/recycle\/restore-log$/, handler: () => ok({ items: restoreLog }) })

routes.push({ method: 'GET', pattern: /^\/api\/v1\/admin\/security$/, handler: () => ok(security) })
routes.push({ method: 'PUT', pattern: /^\/api\/v1\/admin\/security$/, handler: (url, body) => {
  Object.assign(security, JSON.parse(body || '{}'))
  return ok(security)
} })

routes.push({ method: 'GET', pattern: /^\/api\/v1\/admin\/audit$/, handler: () => ok(audit) })

// 个人访问令牌（PAT）：生成/吊销；明文 token 仅在生成响应中出现一次
routes.push({ method: 'GET', pattern: /^\/api\/v1\/admin\/tokens$/, handler: () => ok(tokens) })
routes.push({ method: 'POST', pattern: /^\/api\/v1\/admin\/tokens$/, handler: (url, body) => {
  const b = JSON.parse(body || '{}')
  if (!b.name) return badRequest('name 必填')
  if (!['readonly', 'readwrite'].includes(b.tier)) return badRequest('tier 仅支持 readonly / readwrite')
  const bound = adminUsers.find((u) => u.nickname === b.user || u.username === b.user)
  if (!bound) return badRequest('绑定用户不存在')
 // PAT 仅绑定情侣用户（girl/boy）；admin 仅后台操作，不签发外部令牌
  if (bound.role === 'admin') return badRequest('PAT 仅可绑定情侣用户（girl/boy），admin 不签发令牌')
  const item = { id: ++seq.pat, name: b.name, user: b.user, tier: b.tier, createdAt: new Date().toISOString().slice(0, 10), status: 'active' }
  tokens.push(item)
  return created({ ...item, token: 'llp_' + randomBytes(24).toString('hex') })
} })
routes.push({ method: 'POST', pattern: /^\/api\/v1\/admin\/tokens\/\d+\/revoke$/, handler: (url) => {
  const item = tokens.find((t) => t.id === idOf(url))
  if (!item) return notFound('令牌不存在')
  item.status = 'revoked'
  return ok(item)
} })


// ---------- 上游模型服务模拟（联调） ----------
// 供应商 base_url 指向 http://localhost:8787/upstream/<proto>，服务端在 base 后拼协议路径
// （openai-chat: /chat/completions · openai-responses: /responses · anthropic: /v1/messages ·
//  gemini: /v1beta/models/{model}:generateContent）。启发式响应：消息含「记」→ propose_post
// tool_call；含「相册」→ propose_album；否则文本回复。开发态不校验 Key。

// 从已解析的各协议请求体提取最后一条真实用户输入
// （tool_result / functionResponse 载荷无文本，天然跳过）、工具名清单，以及是否已带回工具结果。
function parseUpstreamIntent(b, apiType) {
  let lastUser = ''
  let toolNames = []
  // 请求里已带回工具结果 = 「工具回灌后的第二次调用」，不能再触发同一个工具（会死循环）
  let hasToolResult = false
  if (apiType === 'openai-chat') {
    toolNames = (b.tools || []).map((t) => t.function && t.function.name).filter(Boolean)
    for (const m of (b.messages || [])) {
      if (m.role === 'tool') hasToolResult = true
      if (m.role === 'user' && typeof m.content === 'string') lastUser = m.content
    }
  } else if (apiType === 'openai-responses') {
    toolNames = (b.tools || []).map((t) => t.name).filter(Boolean)
    for (const item of (b.input || [])) {
      if (item.type === 'function_call_output') hasToolResult = true
      if (item.role === 'user' && typeof item.content === 'string') lastUser = item.content
    }
  } else if (apiType === 'anthropic') {
    toolNames = (b.tools || []).map((t) => t.name).filter(Boolean)
    for (const m of (b.messages || [])) {
      if (m.role !== 'user') continue
      if (typeof m.content === 'string') lastUser = m.content
      else if (Array.isArray(m.content)) {
        if (m.content.some((c) => c.type === 'tool_result')) hasToolResult = true
        const text = m.content.filter((c) => c.type === 'text').map((c) => c.text).join(' ')
        if (text) lastUser = text
      }
    }
  } else if (apiType === 'gemini') {
    toolNames = (b.tools || []).flatMap((t) => (t.functionDeclarations || []).map((d) => d.name)).filter(Boolean)
    for (const c of (b.contents || [])) {
      if (c.role !== 'user') continue
      const text = (c.parts || []).filter((p) => p.text).map((p) => p.text).join(' ')
      if (text) lastUser = text
    }
    if ((b.contents || []).some((c) => (c.parts || []).some((p) => p.functionResponse))) hasToolResult = true
  }
  return { lastUser, toolNames, hasToolResult }
}

// 启发式决定回复：写工具候选优先。
//
// 读工具（find_*）也要有触发词：写工具的提案是 pending 卡片，按设计不进模型上下文，
// 因此**同轮不会产生第二次上游调用**——想联调「工具结果回灌后的第二次请求」
// （如 gemini 的 thoughtSignature 往返）只能用读工具。
function decideUpstreamReply(lastUser, toolNames, hasToolResult) {
  const has = (n) => toolNames.includes(n)
  if (!hasToolResult) {
    if (lastUser.includes('相册') && has('propose_album')) {
      return { tool: 'propose_album', args: { name: '旅行' } }
    }
    if (lastUser.includes('记') && has('propose_post')) {
      return { tool: 'propose_post', args: { content: String(lastUser).slice(0, 50), mood: '开心' } }
    }
    if ((lastUser.includes('找') || lastUser.includes('查')) && has('find_posts')) {
      return { tool: 'find_posts', args: { query: String(lastUser).slice(0, 20) } }
    }
  }
  return { text: '（mock 上游）收到：' + String(lastUser || '').slice(0, 40) }
}

const upstreamToolCall = (call) => ({
  'openai-chat': () => ({ choices: [{ message: { content: '', tool_calls: [{ id: 'mock-1', type: 'function', function: { name: call.tool, arguments: JSON.stringify(call.args) } }] }, finish_reason: 'tool_calls' }] }),
  'openai-responses': () => ({ output: [{ type: 'function_call', call_id: 'call_mock1', name: call.tool, arguments: JSON.stringify(call.args) }] }),
  'anthropic': () => ({ content: [{ type: 'tool_use', id: 'toolu_mock1', name: call.tool, input: call.args }], stop_reason: 'tool_use' }),
  'gemini': () => ({ candidates: [{ content: { parts: [{ functionCall: { name: call.tool, args: call.args } }] }, finishReason: 'STOP' }] }),
})

const upstreamText = (text) => ({
  'openai-chat': () => ({ choices: [{ message: { content: text }, finish_reason: 'stop' }] }),
  'openai-responses': () => ({ output: [{ type: 'message', content: [{ type: 'output_text', text }] }] }),
  'anthropic': () => ({ content: [{ type: 'text', text }], stop_reason: 'end_turn' }),
  'gemini': () => ({ candidates: [{ content: { parts: [{ text }] }, finishReason: 'STOP' }] }),
})

// 上游 chat 端点工厂：按协议解析意图并按 native 形状回包。
//
// 请求体带 stream:true 时改写真 SSE（不再走 JSON 分支）——四协议各自的**原生**事件
// 形状都必须在这里被真实覆盖，否则服务端的原生流式实现只有单测、没有联调路径：
//   · 正文与思考都**逐字分片**（单帧多字会让「跨帧累积」的实现差异测不出来）
//   · 工具 arguments **跨多帧**到达（覆盖分片拼接）
//   · 按协议给各自的思考事件形状（anthropic thinking_delta / responses reasoning_summary /
//     gemini thought:true 的 part / openai-chat reasoning_content）
//   · gemini 侧覆盖 part 级 thoughtSignature（服务端必须原样回传，否则 400）
//   · 两种可触发故障：「首帧前失败」「首帧后失败」，用于验证换链与不换链
//
// 故障触发关键词写在用户消息里（联调时人工输入即可）：
//   「首帧前失败」→ 立刻回 500（无任何 SSE 帧）；「首帧后失败」→ 先发若干帧再中断连接。
const STREAM_FAIL_BEFORE = '首帧前失败'
const STREAM_FAIL_AFTER = '首帧后失败'

function upstreamChatHandler(apiType) {
  return (url, body) => {
    const b = JSON.parse(body || '{}')
    const { lastUser, toolNames, hasToolResult } = parseUpstreamIntent(b, apiType)
    const call = decideUpstreamReply(lastUser, toolNames, hasToolResult)
    // gemini 的流式由 **URL**（:streamGenerateContent）表达，请求体里没有 stream 字段；
    // 只看 b.stream 会把 gemini 的流式请求错当非流式，返回整块 JSON——
    // 服务端的原生流式就永远拿不到真实 SSE 帧（且非流式分支的行为与流式不一致）。
    const wantsStream = !!b.stream || String(url).includes(':streamGenerateContent')
    if (!wantsStream) {
      if (call.tool) return ok(upstreamToolCall(call)[apiType]())
      return ok(upstreamText(call.text)[apiType]())
    }
    return upstreamStream(apiType, lastUser, call)
  }
}

// 逐字切分（含多字节安全：JS 字符串按 UTF-16 码元切，中文一个字一个码元）
function charChunks(s) {
  return Array.from(String(s || ''))
}

// 各协议的 SSE 帧序列。返回 [{ event, data }]，data 为对象（undefined = 只发 event 行）。
function streamFrames(apiType, text, call, thinking) {
  const frames = []
  const push = (event, data) => frames.push({ event, data })
  const textChars = charChunks(text)
  const thinkChars = charChunks(thinking)
  // 工具参数跨帧：切成 3 段（不足 3 字符则逐字符）
  const argStr = call ? JSON.stringify(call.args) : ''
  const argParts = argStr.length >= 3
    ? [argStr.slice(0, 1), argStr.slice(1, -1), argStr.slice(-1)]
    : charChunks(argStr)

  if (apiType === 'openai-chat') {
    const delta = (d) => push('', { choices: [{ index: 0, delta: d }] })
    if (thinking) for (const ch of thinkChars) delta({ reasoning_content: ch })
    if (call) {
      delta({ tool_calls: [{ index: 0, id: 'mock-1', type: 'function', function: { name: call.tool, arguments: '' } }] })
      for (const p of argParts) delta({ tool_calls: [{ index: 0, function: { arguments: p } }] })
      push('', { choices: [{ index: 0, delta: {}, finish_reason: 'tool_calls' }] })
    } else {
      for (const ch of textChars) delta({ content: ch })
      push('', { choices: [{ index: 0, delta: {}, finish_reason: 'stop' }] })
    }
    // 兼容生态的 usage 末帧（choices 为空数组）
    push('', { choices: [], usage: { prompt_tokens: 12, completion_tokens: 34, total_tokens: 46 } })
    return frames
  }

  if (apiType === 'anthropic') {
    push('message_start', { type: 'message_start', message: { usage: { input_tokens: 12, output_tokens: 0 } } })
    let idx = 0
    if (thinking) {
      push('content_block_start', { type: 'content_block_start', index: idx, content_block: { type: 'thinking' } })
      for (const ch of thinkChars) push('content_block_delta', { type: 'content_block_delta', index: idx, delta: { type: 'thinking_delta', thinking: ch } })
      // signature_delta 是思考块完整性凭据，本仓不消费——这里真实发一帧，
      // 确保服务端「按规范忽略」而不是误当未知事件报错
      push('content_block_delta', { type: 'content_block_delta', index: idx, delta: { type: 'signature_delta', signature: 'sig-mock' } })
      push('content_block_stop', { type: 'content_block_stop', index: idx })
      idx++
    }
    if (call) {
      push('content_block_start', { type: 'content_block_start', index: idx, content_block: { type: 'tool_use', id: 'toolu_mock1', name: call.tool } })
      for (const p of argParts) push('content_block_delta', { type: 'content_block_delta', index: idx, delta: { type: 'input_json_delta', partial_json: p } })
      push('content_block_stop', { type: 'content_block_stop', index: idx })
      push('message_delta', { type: 'message_delta', delta: { stop_reason: 'tool_use' }, usage: { output_tokens: 34 } })
    } else {
      push('content_block_start', { type: 'content_block_start', index: idx, content_block: { type: 'text' } })
      for (const ch of textChars) push('content_block_delta', { type: 'content_block_delta', index: idx, delta: { type: 'text_delta', text: ch } })
      push('content_block_stop', { type: 'content_block_stop', index: idx })
      push('message_delta', { type: 'message_delta', delta: { stop_reason: 'end_turn' }, usage: { output_tokens: 34 } })
    }
    push('message_stop', { type: 'message_stop' })
    return frames
  }

  if (apiType === 'openai-responses') {
    let seq = 0
    const next = () => ++seq
    if (thinking) {
      push('response.reasoning_summary_part.added', { type: 'response.reasoning_summary_part.added', sequence_number: next() })
      for (const ch of thinkChars) push('response.reasoning_summary_text.delta', { type: 'response.reasoning_summary_text.delta', sequence_number: next(), delta: ch })
    }
    if (call) {
      push('response.output_item.added', { type: 'response.output_item.added', sequence_number: next(), item: { type: 'function_call', id: 'fc_mock1', call_id: 'call_mock1', name: call.tool, arguments: '' } })
      for (const p of argParts) push('response.function_call_arguments.delta', { type: 'response.function_call_arguments.delta', sequence_number: next(), item_id: 'fc_mock1', delta: p })
      push('response.function_call_arguments.done', { type: 'response.function_call_arguments.done', sequence_number: next(), item_id: 'fc_mock1', arguments: argStr })
      push('response.output_item.done', { type: 'response.output_item.done', sequence_number: next(), item: { type: 'function_call', id: 'fc_mock1', call_id: 'call_mock1', name: call.tool, arguments: argStr } })
    } else {
      push('response.output_item.added', { type: 'response.output_item.added', sequence_number: next(), item: { type: 'message', id: 'msg_mock1' } })
      for (const ch of textChars) push('response.output_text.delta', { type: 'response.output_text.delta', sequence_number: next(), item_id: 'msg_mock1', delta: ch })
      push('response.output_item.done', { type: 'response.output_item.done', sequence_number: next(), item: { type: 'message', id: 'msg_mock1' } })
    }
    // usage 只在 response.completed 出现
    push('response.completed', { type: 'response.completed', sequence_number: next(), response: { usage: { input_tokens: 12, output_tokens: 34, total_tokens: 46 } } })
    return frames
  }

  // gemini：alt=sse，每帧一个完整 GenerateContentResponse。
  // 注意 args 是 JSON **object**（不是字符串），故**无法**像其余协议那样表达「半截
  // 参数」——真实 Gemini API 也不分片（partialArgs 仅 Vertex 支持）。这里保持与真实
  // 端点一致：一帧给完整 args。
  const frame = (p) => push('', { candidates: [{ content: { parts: p }, finishReason: 'STOP' }] })
  if (thinking) {
    for (const ch of thinkChars) frame([{ text: ch, thought: true }])
  }
  if (call) {
    // part 级 thoughtSignature（与 functionCall 平级）——服务端必须原样回传，否则 400
    frame([{ functionCall: { name: call.tool, args: call.args }, thoughtSignature: 'sig-mock-gemini' }])
  } else {
    for (const ch of textChars) frame([{ text: ch }])
  }
  // usageMetadata 在末帧
  push('', { candidates: [{ content: { parts: [] }, finishReason: 'STOP' }], usageMetadata: { promptTokenCount: 12, candidatesTokenCount: 34, totalTokenCount: 46 } })
  return frames
}

// 写 SSE 响应（真流式：逐帧 write + flush 语义由 Node 自动处理）
// 帧间隔：真实上游不会把整段回复在同一个瞬间塞进一个 TCP 包。
// 不 pace 的话服务端与客户端都是一次性拿到全文，「逐字展开」这条联调路径等于没测
// （首帧时间与整段结束时间会几乎相同）。可用 MOCK_STREAM_DELAY_MS 调整或置 0 关闭。
const STREAM_FRAME_DELAY_MS = Number(process.env.MOCK_STREAM_DELAY_MS ?? 12)

const sleep = (ms) => (ms > 0 ? new Promise((r) => setTimeout(r, ms)) : Promise.resolve())

async function writeSSE(res, frames, { failAfterFirst = false } = {}) {
  res.statusCode = 200
  res.setHeader('Content-Type', 'text/event-stream;charset=utf-8')
  res.setHeader('Cache-Control', 'no-cache')
  res.setHeader('Connection', 'keep-alive')
  // 先把响应头刷出去：否则下面的 write 会留在 Node 的缓冲里，
  // res.destroy() 时连同数据一起丢弃，客户端一个字节都收不到——
  // 「首帧后失败」就退化成「首帧前失败」，测不出「已下发增量后不得换链」。
  res.flushHeaders()
  let sent = 0
  for (const f of frames) {
    // 「首帧后失败」：发够几帧就硬断连接（不写 res.end），验证服务端不换链。
    // 用 setImmediate 让已 write 的数据真正落到 socket 再销毁。
    if (failAfterFirst && sent >= 2) {
      setImmediate(() => res.destroy())
      return
    }
    let chunk = ''
    if (f.event) chunk += `event: ${f.event}\n`
    if (f.data !== undefined) chunk += `data: ${JSON.stringify(f.data)}\n`
    chunk += '\n'
    res.write(chunk)
    sent++
    await sleep(STREAM_FRAME_DELAY_MS)
  }
  res.end()
}

function upstreamStream(apiType, lastUser, call) {
  return (res) => {
    // 「首帧前失败」：一个 SSE 帧都不发就报错——此时换链是安全且必要的
    if (String(lastUser).includes(STREAM_FAIL_BEFORE)) {
      res.statusCode = 500
      res.setHeader('Content-Type', 'application/json')
      res.end(JSON.stringify({ error: { message: 'mock 上游：首帧前失败' } }))
      return
    }
    const text = call.tool ? '' : call.text
    // 思考链：消息含「思考」才下发（与 /api/v1/chat 的 mock 同口径）
    const thinking = /思考/.test(String(lastUser))
      ? `用户说「${String(lastUser).slice(0, 20)}」，我先梳理一下意图再作答。`
      : ''
    const frames = streamFrames(apiType, text, call.tool ? call : null, thinking)
    return writeSSE(res, frames, { failAfterFirst: String(lastUser).includes(STREAM_FAIL_AFTER) })
  }
}

// models 列表（fetch-models 的上游形状； 联调用）
const upstreamModels = (apiType) => {
  const list = API_PROTOCOLS[apiType] || []
  if (apiType === 'gemini') return ok({ models: list.map((m) => ({ name: 'models/' + m })) })
  return ok({ data: list.map((m) => ({ id: m })) })
}

routes.push({ method: 'GET', pattern: /^\/upstream\/openai-chat\/models$/, handler: () => upstreamModels('openai-chat') })
routes.push({ method: 'POST', pattern: /^\/upstream\/openai-chat\/chat\/completions$/, handler: upstreamChatHandler('openai-chat') })
routes.push({ method: 'GET', pattern: /^\/upstream\/openai-responses\/models$/, handler: () => upstreamModels('openai-responses') })
routes.push({ method: 'POST', pattern: /^\/upstream\/openai-responses\/responses$/, handler: upstreamChatHandler('openai-responses') })
routes.push({ method: 'GET', pattern: /^\/upstream\/anthropic\/v1\/models$/, handler: () => upstreamModels('anthropic') })
routes.push({ method: 'POST', pattern: /^\/upstream\/anthropic\/v1\/messages$/, handler: upstreamChatHandler('anthropic') })
routes.push({ method: 'GET', pattern: /^\/upstream\/gemini\/v1beta\/models$/, handler: () => upstreamModels('gemini') })
routes.push({ method: 'POST', pattern: /^\/upstream\/gemini\/v1beta\/models\/[^:]+:generateContent$/, handler: upstreamChatHandler('gemini') })
// 原生流式端点（streamGenerateContent?alt=sse）：同一 handler，按 **URL** 判定走 SSE
routes.push({ method: 'POST', pattern: /^\/upstream\/gemini\/v1beta\/models\/[^:]+:streamGenerateContent$/, handler: upstreamChatHandler('gemini') })


export { routes }
