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
import {
  USERS, tokenStore, space, posts, comments, likes, favorites,
  albums, timelineMedia, anniversaries, notifications, mapSources, mapPoints,
  GEOCODE_PLACES,
  chatHistory, actions, seq, now,
  dashboardStats, recentActivity, aiUsage, alerts, modelProviders, embeddingConfig, systemPrompt, styles, styleDefaults,
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
const conflict = (msg) => ({ _s: 409, d: { error: msg } }) // 用户名冲突等「已存在」类（对齐服务端 409）

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
  // S5：单靠 next_anniversary 无法区分「没有纪念日」与「纪念日存在但日期无效」，
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
  // D4 音频时长（秒）；照片/视频不带 duration 字段
  if (m.kind === 'audio' && typeof m.duration === 'number') out.duration = m.duration
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
  return { ...mediaOut(m), post_id: m.post_id, taken_at: m.taken_at, author_id: m.author_id, album_id: m.album_id ?? null, album_name: mediaAlbumName(m) }
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
function addNotification(recipientId, eventType, targetType, targetId, snapshot, minutesAgo = 0) {
  notifications.push({
    id: ++seq.notification,
    recipient_id: recipientId,
    event_type: eventType,
    target_type: targetType,
    target_id: targetId,
    is_read: 0,
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

// 健康检查：客户端双地址探测（AP1，DESIGN §11.2）ping 的就是这里，无鉴权。
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

routes.push({ method: 'PUT', pattern: /^\/api\/v1\/space$/, handler: (url, body) => {
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
  if (b.chat_retention_days !== undefined) u.chat_retention_days = b.chat_retention_days
  return ok({ status: 'ok' })
}})

// --- Posts ---
routes.push({ method: 'GET', pattern: /^\/api\/v1\/posts$/, handler: (url, body, headers) => {
  const uid = userIdFromToken(headers)
  const q = queryParams(url)
  const page = parseInt(q.page) || 1
  const pageSize = parseInt(q.page_size) || 20
  let sorted = [...posts].sort((a, b) => new Date(b.created_at) - new Date(a.created_at))
  // D8 那年今日（REQUIREMENTS §4.1）：先按同月同日过滤，再分页——否则只能在最新一页里碰运气
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
  const total = sorted.length
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
  // F7 自删静默 + REQUIREMENTS §4.8：内容删除仅通知被删内容作者（非本人删除时），其余人静默
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
  // 响应维持 {liked}，计数由 APP 本地增减（A6）
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
  // F7 自删静默 + REQUIREMENTS §4.8：评论删除仅通知评论作者（非本人删除时），其余人静默
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

routes.push({ method: 'GET', pattern: /^\/api\/v1\/albums$/, handler: (url) => {
  const q = queryParams(url)
  const includeHidden = q.include_hidden === '1'
  const list = albums.filter((a) => includeHidden || !a.is_hidden).map((a) => ({
    ...a,
    // D4 月度虚拟口径：该月未归属手动相册的媒体数
    item_count: a.type === 'monthly'
      ? liveTimelineMedia().filter((m) => m.album_id == null && (m.taken_at || m.created_at || '').slice(0, 7) === a.month_key).length
      : liveTimelineMedia().filter((m) => m.album_id === a.id).length,
  }))
  return ok({ items: list })
}})

routes.push({ method: 'POST', pattern: /^\/api\/v1\/albums$/, handler: (url, body, headers) => {
  const uid = userIdFromToken(headers)
  if (!uid) return unauthorized()
  const b = JSON.parse(body || '{}')
  const id = ++seq.album
  const a = { id, type: 'manual', name: b.name || '新相册', description: b.description || '', is_hidden: false, cover_url: '', item_count: 0, created_by: uid }
  albums.push(a)
  // 通知另一方「相册上新」（按 REQUIREMENTS §4.8：对方创建相册时通知，自建静默）
  for (const r of partnerIds(uid)) addNotification(r, 'album_created', 'album', id, { operator: operatorName(uid), name: a.name })
  return created(a)
}})

routes.push({ method: 'GET', pattern: /^\/api\/v1\/albums\/\d+$/, handler: (url) => {
  const id = parseIntParam(url, /\/albums\/(\d+)/)
  if (!id) return notFound()
  const a = albums.find((x) => x.id === id)
  if (!a) return notFound()
  // 月度相册虚拟口径（D4）：该月未归属手动相册的媒体，item_count 同口径
  let media
  if (a.type === 'monthly') {
    media = liveTimelineMedia()
      .filter((m) => m.album_id == null && (m.taken_at || m.created_at || '').slice(0, 7) === a.month_key)
      .map(timelineItemOut)
  } else {
    media = liveTimelineMedia().filter((m) => m.album_id === id).map(timelineItemOut)
  }
  return ok({ album: { ...a, item_count: media.length }, media })
}})

routes.push({ method: 'DELETE', pattern: /^\/api\/v1\/albums\/\d+$/, handler: (url, body, headers) => {
  const id = parseIntParam(url, /\/albums\/(\d+)/)
  if (!id) return notFound()
  const idx = albums.findIndex((x) => x.id === id)
  if (idx === -1) return notFound()
  // 月度相册不可删除（只可隐藏，DESIGN §12.2）；此前漏判 + 漏删，删除后列表仍显示该相册
  if (albums[idx].type === 'monthly') return { _s: 400, d: { error: '月度相册不可删除，只可隐藏' } }
  // 删除对方创建的手动相册 → 通知创建者（自删静默；内容删除仅站内，REQUIREMENTS §4.8）
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

routes.push({ method: 'PATCH', pattern: /^\/api\/v1\/albums\/\d+\/hidden$/, handler: (url, body) => {
  const id = parseIntParam(url, /\/albums\/(\d+)\/hidden/)
  if (!id) return notFound()
  const a = albums.find((x) => x.id === id)
  if (!a) return notFound()
  const b = JSON.parse(body || '{}')
  a.is_hidden = b.hidden === true
  return ok({ status: 'ok' })
}})

// --- Media ---
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
  // F1/F2：列表项带 album_name；F2 一文件一相册由 album_id 决定
  const list = liveTimelineMedia().slice().sort((a, b) => {
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
  const token = 'mock-media-' + randomBytes(6).toString('hex')
  uploads.set(token, kind)
  return ok({ media_token: token, kind, filename })
}})

// F1 独立媒体 finalize 已停用：媒体仅随「动态」上传（post_id 必填），不再支持独立上传/传媒体。
routes.push({
  method: 'POST',
  pattern: /^\/api\/v1\/media$/,
  handler: () => ({ _s: 410, d: { error: 'INDEPENDENT_MEDIA_DISABLED', message: '媒体仅随动态发布，不再支持独立上传' } }),
})

// D1 reurl 批量重签：{ids ≤200} → {items:[{id,thumb_url,url}]}；无效/已删 id 跳过
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

// F6 独立媒体删除已停用（需求 §4.5/166：媒体仅随动态，删除媒体=删除整条动态；mock 契约 410）
routes.push({ method: 'DELETE', pattern: /^\/api\/v1\/media\/\d+$/, handler: (url, body, headers) => {
  const uid = userIdFromToken(headers)
  if (!uid) return unauthorized()
  return { _s: 410, d: { error: 'INDEPENDENT_MEDIA_DISABLED', message: '媒体仅随动态发布，不再支持独立删除（删除媒体=删除整条动态）' } }
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
    remind_days_before: b.remind_days_before ?? 1, // 默认 1 天（REQUIREMENTS §4.6）；显式 0=当天提醒，不得被 || 改写
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
  // 删除对方创建的纪念日 → 通知创建者（自删静默；内容删除仅站内，REQUIREMENTS §4.8）
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
  const count = notifications.filter((n) => n.recipient_id === uid && !n.is_read).length
  return ok({ count })
}})

routes.push({ method: 'POST', pattern: /^\/api\/v1\/notifications\/\d+\/read$/, handler: (url) => {
  const id = parseIntParam(url, /\/notifications\/(\d+)\/read/)
  if (!id) return notFound()
  const n = notifications.find((x) => x.id === id)
  if (!n) return notFound()
  n.is_read = 1
  return ok({})
}})

routes.push({ method: 'POST', pattern: /^\/api\/v1\/notifications\/read-all$/, handler: (url, body, headers) => {
  const uid = userIdFromToken(headers)
  notifications.forEach((n) => { if (n.recipient_id === uid) n.is_read = 1 })
  return ok({})
}})

// --- Chat ---
routes.push({ method: 'GET', pattern: /^\/api\/v1\/chat\/status$/, handler: () => ok({ available: true, reason: '' }) })

// A8 history 补字段：{id, role, content, card_id?, created_at}
// 阶段4/4.1：追加 card 快照 {action_id, tool, params, status, expires_in}，APP 重进对话可恢复确认卡片（未过期可继续确认，过期/重启置 expired）
routes.push({ method: 'GET', pattern: /^\/api\/v1\/chat\/history$/, handler: () => {
  const items = chatHistory.map((h) => {
    const base = { id: h.id, role: h.role, content: h.content, card_id: h.card_id ?? 0, created_at: h.created_at, media: h.media ?? [] }
    const cardId = h.card_id
    if (!cardId) return base
    const act = actions.get(cardId)
    let status = 'expired'
    let tool = ''
    let params = {}
    let expiresIn = 0
    if (act) {
      tool = act.tool
      params = act.params ?? {}
      if (act.resolved) {
        status = act.resolved === 'cancelled' ? 'cancelled' : (act.resolved === 'failed' ? 'failed' : 'executed')
      } else if (Date.now() > act.created_at + ACTION_TTL_MS) {
        status = 'expired'
      } else {
        status = 'pending'
        expiresIn = Math.max(1, Math.ceil((act.created_at + ACTION_TTL_MS - Date.now()) / 1000))
      }
    }
    return { ...base, card: { action_id: cardId, tool, params, status, expires_in: expiresIn } }
  })
  return ok({ items })
}})

// A1 SSE 流式响应 + A8 card 契约：{action_id(数值), tool, params, status:"pending", expires_in:900}
routes.push({ method: 'POST', pattern: /^\/api\/v1\/chat\/message$/, handler: (url, body, headers) => {
  const uid = userIdFromToken(headers)
  const b = JSON.parse(body || '{}')
  const msg = (b.content || '').trim()
  // 聊天多选媒体：仅随消息展示 + 卡片带「客户端引用」（media_refs，无服务端 id、不上传）。
  // 真正落库发生在「确认执行」：客户端先把本地文件 POST /media/upload 换 token，随 confirm 提交
  // （与「传统模式·发动态」一致——取消/过期不产生任何媒体资源，无垃圾数据），见 docs/TODO.md D8。
  const attach = Array.isArray(b.media) ? b.media.filter((m) => m && m.data_url) : []
  const mediaRefs = attach.map((m) => ({ name: m.name || '', type: m.type || '', size: m.size || 0 }))

  const userId = uid || 2
  const userMsgId = ++seq.chat
  chatHistory.push({ id: userMsgId, role: 'user', content: msg, media: attach, created_at: new Date().toISOString() })

  // 意图 → 卡片参数（按 DESIGN §6 工具参数；mock 先定契约）
  let replyText = null
  let card = null
  let searchResult = null // D2：搜索结果卡片（读工具，无确认卡，直接内联展示）

  if (attach.length && /相册/.test(msg)) {
    // 媒体只能随动态存在（D8 口径）：新照片不能直接落相册，只能发布成动态后从时间轴选入相册。
    // 「建相册」→ create_album（空相册，不含上传附件）；其他「加相册」→ 引导发带图动态。
    const actionId = ++seq.action
    if (/建相册|新建相册|创建相册/.test(msg)) {
      const params = { name: msg.match(/(?:建|新建|创建)相册[：:]?\s*(\S+)?/)?.[1] || '新相册', media_refs: [] }
      actions.set(actionId, { tool: 'create_album', params, user_id: userId, created_at: Date.now(), resolved: null })
      card = { action_id: actionId, tool: 'create_album', params, status: 'pending', expires_in: ACTION_TTL_SEC }
    } else {
      const params = { content: (msg.replace(/帮(我|你)(记|发|创建|把[^]*加(到)?相册)/, '').trim() || '记录一下今天'), mood: '', media_refs: mediaRefs }
      actions.set(actionId, { tool: 'create_post', params, user_id: userId, created_at: Date.now(), resolved: null })
      card = { action_id: actionId, tool: 'create_post', params, status: 'pending', expires_in: ACTION_TTL_SEC }
    }
  } else if (attach.length && /(动态|发|记)/.test(msg)) {
    // 带媒体发布动态：卡片携带 media_refs（元数据），确认时客户端上传+落库
    const actionId = ++seq.action
    const params = { content: msg.replace(/帮(我|你)(记|发|创建)/, '').trim() || '记录一下今天', mood: '', media_refs: mediaRefs }
    actions.set(actionId, { tool: 'create_post', params, user_id: userId, created_at: Date.now(), resolved: null })
    card = { action_id: actionId, tool: 'create_post', params, status: 'pending', expires_in: ACTION_TTL_SEC }
  } else if (attach.length) {
    replyText = `收到 ${attach.length} 张图片/视频${msg ? `（${msg}）` : ''}，你可以说「把它们加到相册」「建个相册」或「发条带图动态」。`
  } else if (/搜索|搜一下|搜搜|查找|查一下|找一下|找找/.test(msg)) {
    // D2 搜索结果卡片（REQUIREMENTS §5.1：卡片内联展示，不做跨模式跳转）
    const query = msg.replace(/^(帮我|帮忙)?\s*(搜索|搜一下|搜搜|查找|查一下|找一下|找找)(一下)?/, '').replace(/[「」"']/g, '').trim()
    const hits = posts
      .filter((p) => !query || (p.content || '').includes(query))
      .sort((a, b) => new Date(b.created_at) - new Date(a.created_at))
      .slice(0, 6)
    searchResult = {
      tool: 'search_posts',
      query,
      items: hits.map((p) => ({
        id: p.id,
        content: (p.content || '').slice(0, 60),
        thumb_url: p.media?.[0]?.thumb_url || '',
      })),
    }
    replyText = hits.length
      ? `找到 ${hits.length} 条${query ? `和「${query}」相关的` : '最近的'}回忆：`
      : `没找到${query ? `和「${query}」相关的` : ''}回忆。`
  } else if (msg.includes('纪念日') || msg.includes('周年')) {
    const nameMatch = msg.match(/纪念日[：:]\s*(.+?)(?:\s|$)/)
    const dateMatch = msg.match(/(\d{4}[-/]\d{1,2}[-/]\d{1,2})/)
    const actionId = ++seq.action // 数值 int64
    const params = { name: nameMatch ? nameMatch[1].trim() : '新纪念日', date: dateMatch ? dateMatch[1] : '' }
    // type 字段为可选（DESIGN §6 create_anniversary type?），mock 默认 anniversary
    actions.set(actionId, { tool: 'create_anniversary', params, user_id: userId, created_at: Date.now(), resolved: null })
    card = { action_id: actionId, tool: 'create_anniversary', params, status: 'pending', expires_in: ACTION_TTL_SEC }
  } else if (msg.includes('动态') || msg.includes('记') || msg.includes('发')) {
    const contentMatch = msg.replace(/帮(我|你)(记|发|创建)/, '').trim()
    const actionId = ++seq.action
    const params = { content: contentMatch || '今天过得怎么样？', mood: '' }
    actions.set(actionId, { tool: 'create_post', params, user_id: userId, created_at: Date.now(), resolved: null })
    card = { action_id: actionId, tool: 'create_post', params, status: 'pending', expires_in: ACTION_TTL_SEC }
  } else {
    replyText = '好的，我明白了！作为你的 AI 助手，我随时准备好帮你记录生活中的美好瞬间。你可以说「帮我记一条动态」或「创建一个纪念日」。'
  }

  // 返回 SSE 写入函数（server.mjs 接到 typeof result === 'function' 时调用 result(res)）
  return (res) => {
    res.statusCode = 200
    res.setHeader('Content-Type', 'text/event-stream;charset=utf-8')
    res.setHeader('Cache-Control', 'no-cache')
    res.setHeader('Connection', 'keep-alive')
    res.setHeader('X-Accel-Buffering', 'no')

    const send = (ev, data) => {
      res.write(`event: ${ev}\ndata: ${JSON.stringify(data)}\n\n`)
    }

    // D2：搜索结果先推（客户端渲染内联卡片网格），随后照常推文字；空结果由客户端渲染「没找到」
    if (searchResult) send('result', searchResult)

    if (card) {
      // 先推一段 text 提示，再推 card；推完结束流（否则 APP busy 永远不结束，右下角一直显示「进行中」）
      send('text', { content: '已为你准备好操作，请确认是否执行：' })
      send('card', card)
      const replyMsgId = ++seq.chat
      chatHistory.push({ id: replyMsgId, role: 'assistant', content: '已为你准备好操作，请确认是否执行：', card_id: card.action_id, created_at: new Date().toISOString() })
      send('done', null)
      res.end()
    } else if (replyText) {
      // 简易分片：每次 2-3 字
      let i = 0
      const chunkSize = 3
      const tick = () => {
        if (i >= replyText.length) {
          const replyMsgId = ++seq.chat
          chatHistory.push({ id: replyMsgId, role: 'assistant', content: replyText, created_at: new Date().toISOString() })
          send('done', null)
          res.end()
          return
        }
        const chunk = replyText.slice(i, i + chunkSize)
        i += chunkSize
        send('text', { content: chunk })
        setTimeout(tick, 30)
      }
      tick()
    } else {
      send('done', null)
      res.end()
    }
  }
}})

// A8 confirm：{status, result}；重复 409；不存在 404；action_id 数值
routes.push({ method: 'POST', pattern: /^\/api\/v1\/chat\/confirm$/, handler: (url, body) => {
  const b = JSON.parse(body || '{}')
  const actionId = parseInt(b.action_id) || 0
  const confirmed = b.confirmed === true
  const action = actions.get(actionId)
  if (!action) return { _s: 404, d: { error: '确认卡片不存在' } }

  if (action.resolved) return { _s: 409, d: { error: '卡片已处理过（单次有效）' } }
  // D4 确认卡片 15 分钟有效期（REQUIREMENTS §4.8/§5.1）：过期后确认 → 410「已过期」，
  // 与 /chat/history 的 expires_in 同口径；不改 resolved，保证重启/重进后仍显示 expired
  if (Date.now() > action.created_at + ACTION_TTL_MS) {
    return { _s: 410, d: { error: '确认卡片已过期，请重新发起' } }
  }

  if (!confirmed) {
    action.resolved = 'cancelled'
    return ok({ status: 'cancelled' })
  }

  action.resolved = 'executed'
  // 媒体随「确认执行」此刻才上传落库（token→湿库），与「传统模式·发动态」一致：取消/过期零垃圾数据
  const tokens = Array.isArray(b.media_tokens) ? b.media_tokens.map((t) => String(t)) : []

  if (action.tool === 'create_post') {
    const id = ++seq.post
    const media = []
    for (const t of tokens) {
      const kind = uploads.get(t) || 'photo'
      const m = { id: ++seq.media, post_id: id, kind, author_id: action.user_id, album_id: null, taken_at: new Date().toISOString().slice(0, 10), thumb_url: `/api/v1/media/file/${seq.media}?v=thumb`, url: `/api/v1/media/file/${seq.media}?v=raw` }
      timelineMedia.push(m)
      media.push(m)
    }
    const newPost = { id, author_id: action.user_id, content: action.params.content || '', mood: action.params.mood || '', point_id: null, location_name: '', media, created_at: new Date().toISOString() }
    posts.unshift(newPost)
    // 通知另一方 post_created（按 F7 任意操作均通知另一方；这里与 REQUIREMENTS 一致——新动态通知对方）
    for (const r of partnerIds(action.user_id)) addNotification(r, 'post_created', 'post', id, { operator: operatorName(action.user_id), content: (newPost.content || '').slice(0, 50) })
    return ok({ status: 'executed', result: `动态 #${id} 已发布${media.length ? `（含 ${media.length} 张媒体）` : ''}` })
  }

  if (action.tool === 'create_anniversary') {
    const id = ++seq.anniversary
    // type/is_leap 与表单契约对齐（DESIGN §6 create_anniversary）：lunar 时透传闰月标记
    const a = { id, name: action.params.name || '新纪念日', date: action.params.date || '2024-01-01', type: action.params.type === 'lunar' ? 'lunar' : 'solar', is_leap: action.params.type === 'lunar' && action.params.is_leap === true ? true : undefined, remind_days_before: action.params.remind_days_before ?? 1, created_by: action.user_id }
    anniversaries.push(a)
    return ok({ status: 'executed', result: `纪念日「${a.name}」已创建` })
  }

  if (action.tool === 'create_album') {
    const id = ++seq.album
    const al = { id, type: 'manual', name: action.params.name || '新相册', description: '', is_hidden: false, cover_url: '/api/v1/media/file/5', item_count: 0, created_by: action.user_id }
    albums.push(al)
    // AI 建相册同样通知另一方（对齐手动 POST /albums 与服务端口径：operator 真实发起人 + via 标记来源）
    for (const r of partnerIds(action.user_id)) addNotification(r, 'album_created', 'album', id, { operator: operatorName(action.user_id), name: al.name, via: 'ai' })
    return ok({ status: 'executed', result: `相册「${al.name}」已创建` })
  }

  if (action.tool === 'add_photos_to_album') {
    // D8 口径：媒体只能以动态为载体，相册只从媒体池（时间轴）选择 photo_ids（已有动态媒体 id），
    // 不接受随 confirm 上传的新附件 token（独立媒体已废除）。
    if (tokens.length) return badRequest('新附件不能直接加入相册：媒体只能随动态发布，请在时间轴选择照片后加入')
    const al = albums.find((a) => a.id === (action.params.album_id || 1))
    const ids = Array.isArray(action.params.media_ids) ? action.params.media_ids : []
    const picked = timelineMedia.filter((m) => ids.includes(m.id) && !m.deleted_at)
    if (!al) return badRequest('相册不存在')
    let added = 0
    let lastId = 0
    for (const m of picked) {
      m.album_id = al.id
      lastId = m.id
      added++
    }
    if (added) {
      al.item_count += added
      al.cover_url = `/api/v1/media/file/${lastId}`
    }
    return ok({ status: 'executed', result: `已加入相册「${al.name}」${added} 张` })
  }

  return ok({ status: 'failed', error: '未知操作类型' })
}})

// --- Map ---
// 瓦片短时签名：与真实服务端口径一致——只绑 exp 的空间级签名（一次覆盖全部坐标）
const TILE_GRANT_TTL_SEC = 1800
const TILE_SIG_SECRET = 'mock-tile-secret'
const tileSig = (exp) => createHmac('sha256', TILE_SIG_SECRET).update(`map:tile:${exp}`).digest('hex')

// APP 配置：下发「使用中」的源与瓦片短时签名（provider/tile_api/url_template? 不变，新增 exp/sig，无密钥）
routes.push({ method: 'GET', pattern: /^\/api\/v1\/map\/config$/, handler: () => {
  const s = mapSources.find((x) => x.enabled)
  const exp = Math.floor(Date.now() / 1000) + TILE_GRANT_TTL_SEC
  const out = { provider: s?.type ?? '', tile_api: '/api/v1/map/tile', exp, sig: tileSig(exp) }
  if (s?.type === 'public' && s.url_template) out.url_template = s.url_template
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
  // 删除对方创建的足迹 → 通知创建者（自删静默；内容删除仅站内，REQUIREMENTS §4.8）
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
routes.push({ method: 'POST', pattern: /^\/api\/v1\/admin\/users$/, handler: (url, body) => {
  const b = JSON.parse(body || '{}')
  if (!b.username || !b.password) return badRequest('username/password 必填')
  if (b.role !== 'girl' && b.role !== 'boy') return badRequest('role 仅允许 girl/boy')
  // 格式校验先于存在性判断（对齐服务端 createUser 契约）
  if (b.username.length < 2 || b.username.length > 32) return badRequest('用户名长度 2-32 个字符')
  if (b.password.length < 6) return badRequest('密码强度不足：至少 6 位')
  if (adminUsers.some((u) => u.username === b.username)) return conflict('用户名已存在')
  const user = { id: ++seq.user, username: b.username, role: b.role, nickname: b.nickname || '' }
  adminUsers.push(user)
  return created(user)
} })

// 情侣账户原子创建：双方信息一次提交，任一情侣角色已存在则拒绝（规则由服务端强制）
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

// GAP16 头像上传（mock 仅回执，不持久化文件）
routes.push({ method: 'POST', pattern: /^\/api\/v1\/admin\/users\/\d+\/avatar$/, handler: (url) => {
  const user = adminUsers.find((u) => u.id === idOf(url))
  if (!user) return notFound('用户不存在')
  user.avatar = `/api/v1/avatar/${user.id}?exp=0&sig=mock`
  return ok({ status: 'ok', avatar: user.avatar })
} })

// GAP16 生日（仅后台维护，空串清除）
routes.push({ method: 'PUT', pattern: /^\/api\/v1\/admin\/users\/\d+\/birthday$/, handler: (url, body) => {
  const user = adminUsers.find((u) => u.id === idOf(url))
  if (!user) return notFound('用户不存在')
  const b = JSON.parse(body || '{}')
  if (b.birthday && !/^\d{4}-\d{2}-\d{2}$/.test(b.birthday)) return badRequest('生日格式应为 YYYY-MM-DD')
  user.birthday = b.birthday || ''
  return ok({ status: 'ok' })
} })

// GAP16 头像公开读取（签名即凭证，mock 不校验签名）
routes.push({ method: 'GET', pattern: /^\/api\/v1\/avatar\/\d+$/, handler: () => {
  // 占位 1x1 PNG，前端真实展示由 SignedURL 驱动
  return {
    status: 200,
    headers: { 'content-type': 'image/png' },
    body: Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==', 'base64'),
  }
} })

// 通知渠道：CRUD（DESIGN 第 9 章：GET/POST/PUT/DELETE + test）
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

// 仪表盘：内容统计 / AI 调用量与 Token / 异常告警（DESIGN 第 9 章三项）
routes.push({ method: 'GET', pattern: /^\/api\/v1\/admin\/dashboard\/stats$/, handler: () => ok(dashboardStats) })
routes.push({ method: 'GET', pattern: /^\/api\/v1\/admin\/dashboard\/activity$/, handler: () => ok(recentActivity) })
routes.push({ method: 'GET', pattern: /^\/api\/v1\/admin\/dashboard\/ai-usage$/, handler: () => ok({ items: aiUsage }) })
routes.push({ method: 'GET', pattern: /^\/api\/v1\/admin\/dashboard\/alerts$/, handler: () => ok({ items: alerts }) })

// AI 供应商（协议化配置，DESIGN §9）：列表有序 = 故障切换链（从第一个开始、失败依次向下）
const API_PROTOCOLS = {
  'openai-chat': ['gpt-4o', 'gpt-4o-mini', 'gpt-4.1', 'o3-mini', 'deepseek-chat', 'deepseek-r1', 'kimi-k2', 'qwen3-max'],
  'openai-responses': ['gpt-5', 'gpt-5-mini', 'gpt-4.1', 'gpt-4o', 'o3', 'o4-mini'],
  'anthropic': ['claude-opus-4-1-20250805', 'claude-sonnet-4-20250514', 'claude-3-7-sonnet-20250219', 'claude-3-5-haiku-20241022'],
  'gemini': ['gemini-2.5-pro', 'gemini-2.5-flash', 'gemini-2.0-flash'],
}
const protocolInvalid = (t) => badRequest('api_type 不支持（openai-chat / openai-responses / anthropic / gemini）')

// models 条目校验：{ id 必填, temperature|null, max_tokens|null }——参数挂模型级，null = 调用不传
const normalizedModels = (raw) => {
  if (!Array.isArray(raw)) return { error: 'models 必填（数组）' }
  const entries = []
  for (const m of raw) {
    const id = String(m?.id ?? '').trim()
    if (!id) return { error: 'models 中存在模型名为空的条目' }
    // ''/undefined 一律先归一为 null（不设置），再做区间校验
    const t = m.temperature === '' || m.temperature === undefined ? null : m.temperature
    if (t !== null && !(Number(t) >= 0 && Number(t) <= 2)) return { error: `模型 ${id} 的 temperature 需在 0~2 之间，或不设置` }
    const mt = m.max_tokens === '' || m.max_tokens === undefined ? null : m.max_tokens
    if (mt !== null && (!Number.isInteger(Number(mt)) || Number(mt) < 256 || Number(mt) > 32768)) return { error: `模型 ${id} 的 max_tokens 需为 256~32768 整数，或不设置` }
    entries.push({ id, temperature: t === null ? null : Number(t), max_tokens: mt === null ? null : Number(mt) })
  }
  return { entries }
}

routes.push({ method: 'GET', pattern: /^\/api\/v1\/admin\/model-providers$/, handler: () => ok(modelProviders) })

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
  const item = {
    id: ++seq.provider,
    api_type: b.api_type,
    name: (b.name || '').trim() || b.api_type,
    base_url: b.base_url,
    has_key: true,
    models: m.entries,
    default_model: b.default_model || '',
  }
  modelProviders.push(item)
  return created(item)
} })

// 排序 = 故障切换链顺序；ids 必须恰好包含全部供应商（无未知、无重复）
routes.push({ method: 'PUT', pattern: /^\/api\/v1\/admin\/model-providers\/order$/, handler: (url, body) => {
  const b = JSON.parse(body || '{}')
  if (!Array.isArray(b.ids)) return badRequest('ids 必填（数组）')
  const ids = b.ids.map(Number)
  const okIds = ids.every((id) => modelProviders.some((p) => p.id === id))
  if (!okIds || new Set(ids).size !== modelProviders.length) return badRequest('ids 必须恰好包含全部供应商')
  const sorted = ids.map((id) => modelProviders.find((p) => p.id === id))
  modelProviders.splice(0, modelProviders.length, ...sorted)
  return ok(modelProviders)
} })

// 编辑：api_key 不传 = 不修改（只写不读）
routes.push({ method: 'PUT', pattern: /^\/api\/v1\/admin\/model-providers\/\d+$/, handler: (url, body) => {
  const item = modelProviders.find((p) => p.id === idOf(url))
  if (!item) return notFound('供应商不存在')
  const b = JSON.parse(body || '{}')
  if (b.api_type !== undefined) {
    if (!API_PROTOCOLS[b.api_type]) return protocolInvalid()
    item.api_type = b.api_type
  }
  if (b.name !== undefined) item.name = String(b.name).trim() || item.name
  if (b.base_url !== undefined) {
    if (!b.base_url) return badRequest('base_url 不能为空')
    item.base_url = b.base_url
  }
  if (b.models !== undefined) {
    const m = normalizedModels(b.models)
    if (m.error) return badRequest(m.error)
    item.models = m.entries
  }
  if (b.default_model !== undefined) item.default_model = b.default_model
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
// 语义检索（嵌入模型）：provider_id = 0 清空（关闭语义检索），否则必须给模型名且供应商为 OpenAI Chat 协议
routes.push({ method: 'GET', pattern: /^\/api\/v1\/admin\/embedding-config$/, handler: () => ok(embeddingConfig) })
routes.push({ method: 'PUT', pattern: /^\/api\/v1\/admin\/embedding-config$/, handler: (url, body) => {
  let b
  try {
    b = JSON.parse(body || '{}')
  } catch {
    return badRequest('请求格式错误')
  }
  const providerId = Number(b.provider_id || 0)
  let model = String(b.model ?? '').trim()
  if (providerId === 0) {
    model = ''
  } else {
    if (!model) return badRequest('model 必填')
    const p = modelProviders.find((x) => x.id === providerId)
    if (!p) return badRequest('供应商不存在')
    if (p.api_type !== 'openai-chat') return badRequest('嵌入模型仅支持 OpenAI Chat 协议的供应商')
  }
  embeddingConfig.provider_id = providerId
  embeddingConfig.model = model
  return ok({ ...embeddingConfig })
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
  // PAT 仅绑定情侣用户（girl/boy）；admin 仅后台操作，不签发外部令牌（DESIGN §8.2）
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


// ---------- 上游模型服务模拟（L2 联调） ----------
// 供应商 base_url 指向 http://localhost:8787/upstream/<proto>，服务端在 base 后拼协议路径
// （openai-chat: /chat/completions · openai-responses: /responses · anthropic: /v1/messages ·
//  gemini: /v1beta/models/{model}:generateContent）。启发式响应：消息含「记」→ create_post
// tool_call；含「相册」→ create_album；否则文本回复。开发态不校验 Key。

// 从各协议请求体提取最后一条真实用户输入（tool_result / functionResponse 载荷无文本，天然跳过）
// 与工具名清单。
function parseUpstreamIntent(body, apiType) {
  const b = JSON.parse(body || '{}')
  let lastUser = ''
  let toolNames = []
  if (apiType === 'openai-chat') {
    toolNames = (b.tools || []).map((t) => t.function && t.function.name).filter(Boolean)
    for (const m of (b.messages || [])) {
      if (m.role === 'user' && typeof m.content === 'string') lastUser = m.content
    }
  } else if (apiType === 'openai-responses') {
    toolNames = (b.tools || []).map((t) => t.name).filter(Boolean)
    for (const item of (b.input || [])) {
      if (item.role === 'user' && typeof item.content === 'string') lastUser = item.content
    }
  } else if (apiType === 'anthropic') {
    toolNames = (b.tools || []).map((t) => t.name).filter(Boolean)
    for (const m of (b.messages || [])) {
      if (m.role !== 'user') continue
      if (typeof m.content === 'string') lastUser = m.content
      else if (Array.isArray(m.content)) {
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
  }
  return { lastUser, toolNames }
}

// 启发式决定回复：写工具候选优先
function decideUpstreamReply(lastUser, toolNames) {
  const has = (n) => toolNames.includes(n)
  if (lastUser.includes('相册') && has('create_album')) {
    return { tool: 'create_album', args: { name: '旅行' } }
  }
  if (lastUser.includes('记') && has('create_post')) {
    return { tool: 'create_post', args: { content: String(lastUser).slice(0, 50), mood: '开心' } }
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

// 上游 chat 端点工厂：按协议解析意图并按 native 形状回包
function upstreamChatHandler(apiType) {
  return (url, body) => {
    const { lastUser, toolNames } = parseUpstreamIntent(body, apiType)
    const call = decideUpstreamReply(lastUser, toolNames)
    if (call.tool) return ok(upstreamToolCall(call)[apiType]())
    return ok(upstreamText(call.text)[apiType]())
  }
}

// models 列表（fetch-models 的上游形状；AD1 联调用）
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


export { routes }
