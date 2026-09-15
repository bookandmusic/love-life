/**
 * 统一开发 mock 后端 —— 内存 fixture 与状态（零依赖，仅 Node 内置模块 + 原生 fetch）。
 *
 * admin 与 app 共用此服务（app 通过登录页 base URL 或 VITE_SERVER_URL 指向 8787 即可切换），
 * 通过 \`VITE_API_BASE_URL\` 指向即可切换。
 * 认证：三组固定账号（admin/girl/boy），登录颁发 token（内存态，重启失效）。
 * 状态：进程内内存 state，写操作就地变更，重启回到 fixture。
 */

import { randomBytes } from 'node:crypto'

// ---------- 认证与用户 ----------
export const USERS = [
  { id: 1, username: 'admin', password: 'admin123456', role: 'admin', nickname: '管理员', avatar: '', birthday: '', ai_style: '简洁', chat_retention_days: 30 },
  { id: 2, username: 'girl', password: 'girl123456', role: 'girl', nickname: '她', avatar: '', birthday: '1998-06-15', ai_style: '文艺', chat_retention_days: 30 },
  { id: 3, username: 'boy', password: 'boy123456', role: 'boy', nickname: '他', avatar: '', birthday: '1996-03-22', ai_style: '幽默', chat_retention_days: -1 },
]

export const tokenStore = new Map()
export function newToken(userId) {
  const access = `acc_${userId}_${randomBytes(12).toString('hex')}`
  const refresh = `ref_${userId}_${randomBytes(12).toString('hex')}`
  tokenStore.set(access, userId)
  tokenStore.set(refresh, userId)
  return { access, refresh }
}
export function userIdFromToken(headers) {
  const auth = headers['authorization']
  if (!auth) return null
  const m = /^Bearer\s+(.+)$/.exec(auth)
  if (!m) return null
  return tokenStore.get(m[1]) ?? null
}
export function userById(id) {
  const u = USERS.find((x) => x.id === id)
  if (!u) return null
  const { password: _p, ...pub } = u
  return pub
}

// ---------- 空间 ----------
export const space = {
  name: '我们的小窝', signature: '记录每一天的小美好', love_start_date: '2023-05-20',
}

// ---------- 动态 ----------
export const now = Date.now()
export const day = 86400000

/** 去年今天的本地正午时间戳（ISO 串）：供「那年今日」fixture 使用。 */
function lastYearToday() {
  const d = new Date(now)
  return new Date(d.getFullYear() - 1, d.getMonth(), d.getDate(), 12, 0, 0).toISOString()
}

export const posts = [
  { id: 1, author_id: 2, content: '厦门的海真的好蓝，一起在沙滩走了好久，风很大但很舒服。', mood: '开心', point_id: 1, location_name: '厦门 · 鼓浪屿', media: [{ id: 1, kind: 'photo', thumb_url: '/api/v1/media/file/1' }], created_at: new Date(now - 1 * day).toISOString() },
  { id: 2, author_id: 3, content: '照着教程试了一下，虽然卖相一般，但她吃得很开心。', mood: '幸福', point_id: null, location_name: '', media: [{ id: 2, kind: 'photo', thumb_url: '/api/v1/media/file/2' }], created_at: new Date(now - 2 * day).toISOString() },
  { id: 3, author_id: 2, content: '杭州的春天真舒服，柳树都发芽了，沿着湖边走了两小时。', mood: '平静', point_id: 2, location_name: '杭州 · 西湖', media: [], created_at: new Date(now - 3 * day).toISOString() },
  { id: 4, author_id: 3, content: '今天加班到很晚，回家看到她留了热汤，心里暖暖的。', mood: '感动', point_id: null, location_name: '', media: [], created_at: new Date(now - 5 * day).toISOString() },
  { id: 5, author_id: 2, content: '上海的夜景真的太美了，灯光洒在黄浦江上，拍了好多照片。', mood: '兴奋', point_id: 3, location_name: '上海 · 外滩', media: [{ id: 5, kind: 'photo', thumb_url: '/api/v1/media/file/5' }, { id: 6, kind: 'photo', thumb_url: '/api/v1/media/file/6' }], created_at: new Date(now - 8 * day).toISOString() },
  { id: 6, author_id: 3, content: '还记得去年在苏州平江路，一起在小桥上看了很久的落日。', mood: '怀念', point_id: 4, location_name: '苏州 · 平江路', media: [], created_at: new Date(now - 12 * day).toISOString() },
  // 「去年的今天」按运行日动态计算，保证 D8 那年今日永远有命中数据（固定日期会过期导致断言假红）；
  // 用本地年月日构造，与服务端/APP 的「本地时区月日」口径一致。
  { id: 7, author_id: 2, content: '去年的今天，我们刚认识——她穿了一件浅蓝的裙子，笑起来眼睛弯弯的。', mood: '心动', point_id: null, location_name: '大理 · 古城', media: [], created_at: lastYearToday() },
]

export const comments = [
  { id: 1, post_id: 1, author_id: 3, content: '照片拍得真好！下次带我去', created_at: new Date(now - 20 * 3600000).toISOString() },
  { id: 2, post_id: 1, author_id: 2, content: '哈哈下次必须的', created_at: new Date(now - 18 * 3600000).toISOString() },
  { id: 3, post_id: 2, author_id: 2, content: '好吃！虽然卖相确实一般', created_at: new Date(now - 40 * 3600000).toISOString() },
  { id: 4, post_id: 4, author_id: 2, content: '你辛苦啦，记得多喝热水', created_at: new Date(now - 100 * 3600000).toISOString() },
]

export const likes = new Set(['1:2', '1:3', '2:2', '3:2', '5:3'])
export const favorites = new Set(['1:2', '5:3'])

// ---------- 相册 ----------
export const albums = [
  { id: 1, type: 'manual', name: '海边合集', description: '所有看海的时光', is_hidden: false, cover_url: '/api/v1/media/file/5', item_count: 2, created_by: 2 },
  { id: 2, type: 'manual', name: '苏州回忆', description: '平江路与落日', is_hidden: false, cover_url: '/api/v1/media/file/6', item_count: 1, created_by: 2 },
]

/**
 * 媒体（时间轴）。字段对齐真实契约：kind(photo/video/audio)、thumb_url、url、taken_at。
 * post_id 可空（F1 独立媒体）；album_id 表示归属的手动相册（null=未归属，F2 一文件一相册）；author_id 媒体作者（F6 删除通知用）。
 */
function mediaRow(id, { post_id = null, kind = 'photo', taken_at, author_id = 2, album_id = null, duration } = {}) {
  // ext 与 fixtures/ 里真实样本的字节一致（照片 JPEG / 视频 WebM / 录音 WAV，见 routes.mjs MOCK_RAW）
  const ext = kind === 'video' ? 'webm' : kind === 'audio' ? 'wav' : 'jpg'
  const row = {
    id, post_id, kind, author_id, album_id, taken_at,
    thumb_url: `/api/v1/media/file/${id}?v=thumb`,
    url: `/api/v1/media/file/${id}?v=raw&kind=${kind}&ext=${ext}`,
  }
  // D4：音频必带时长（秒，DESIGN 12）；默认给样例值，可显式覆盖
  if (kind === 'audio') row.duration = duration ?? 42.5
  return row
}
export const timelineMedia = [
  mediaRow(1, { post_id: 1, taken_at: '2026-08-20' }),
  mediaRow(2, { post_id: 2, taken_at: '2026-08-19' }),
  mediaRow(5, { post_id: 5, taken_at: '2026-08-15', album_id: 1 }),
  mediaRow(6, { post_id: 5, taken_at: '2026-08-15', album_id: 1 }),
  mediaRow(7, { post_id: 6, taken_at: '2026-07-28' }),
  mediaRow(8, { kind: 'audio', taken_at: '2026-08-14', author_id: 3, album_id: 2, duration: 187 }), // 独立音频，已归入手动相册2
  mediaRow(9, { kind: 'audio', taken_at: '2026-08-12', author_id: 2, duration: 95 }), // 独立音频，未归属
  mediaRow(10, { kind: 'video', taken_at: '2026-08-10', author_id: 3 }), // 独立视频，未归属
  mediaRow(11, { post_id: 5, kind: 'video', taken_at: '2026-08-15' }),
  // 阶段5.5：扩充时间轴样本至 50+，验证 /media/timeline 分页滚动加载（默认一页 20 条、上限 50）
  ...Array.from({ length: 52 }, (_, k) => {
    const id = 200 + k
    const kind = k % 6 === 0 ? 'video' : k % 6 === 5 ? 'audio' : 'photo'
    return mediaRow(id, {
      kind,
      taken_at: new Date(now - (300 + k) * 3600000).toISOString().slice(0, 10),
      author_id: (k % 2) + 2,
    })
  }),
]

// ---------- 纪念日 ----------
export const anniversaries = [
  { id: 1, name: '在一起', date: '2023-05-20', remind_days_before: 7, created_by: 1, type: 'solar' },
  { id: 2, name: '相识纪念日', date: '2022-10-01', remind_days_before: 7, created_by: 1, type: 'solar' },
  // 统一契约：date 统一为 YYYY-MM-DD，type 决定语义——solar=公历；lunar=农历（如 1996-05-15 = 农历1996年五月十五）。
  // 农历的倒计时/排序/公历换算由前端（及 mock 所用同源 lunar.ts）处理，不再存 lunar_month/lunar_day/lunar_leap 等冗余字段。
  { id: 3, name: '她的生日', date: '1996-05-15', remind_days_before: 3, created_by: 1, type: 'lunar' },
  { id: 4, name: '他的生日', date: '1996-03-22', remind_days_before: 3, created_by: 1, type: 'solar' },
  { id: 5, name: '中秋节', date: '2026-08-15', remind_days_before: 3, created_by: 1, type: 'lunar' },
]

// ---------- 通知（站内 8+1 类事件；字段对齐真实：snapshot 为 JSON 字符串，快照键 operator 等） ----------
// 8 业务事件：post_liked / post_commented / post_created / post_favorited / album_created / anniversary_reminder / post_deleted / comment_deleted，加 media_deleted → 8+1
export const notifications = [
  { id: 1, recipient_id: 2, event_type: 'post_liked', target_type: 'post', target_id: 1, is_read: 0, snapshot: '{"operator":"他"}', created_at: new Date(now - 1 * 3600000).toISOString() },
  { id: 2, recipient_id: 2, event_type: 'post_commented', target_type: 'post', target_id: 1, is_read: 0, snapshot: '{"operator":"他","content":"照片拍得真好！下次带我去"}', created_at: new Date(now - 2 * 3600000).toISOString() },
  { id: 3, recipient_id: 3, event_type: 'post_created', target_type: 'post', target_id: 5, is_read: 0, snapshot: '{"operator":"她","content":"外滩夜景"}', created_at: new Date(now - 3 * 3600000).toISOString() },
  { id: 4, recipient_id: 2, event_type: 'anniversary_reminder', target_type: 'anniversary', target_id: 1, is_read: 1, snapshot: '{"name":"在一起"}', created_at: new Date(now - 26 * 3600000).toISOString() },
  { id: 5, recipient_id: 2, event_type: 'post_favorited', target_type: 'post', target_id: 5, is_read: 1, snapshot: '{"operator":"他"}', created_at: new Date(now - 30 * 3600000).toISOString() },
  { id: 6, recipient_id: 3, event_type: 'album_created', target_type: 'album', target_id: 2, is_read: 1, snapshot: '{"operator":"她","name":"苏州回忆"}', created_at: new Date(now - 50 * 3600000).toISOString() },
  { id: 7, recipient_id: 2, event_type: 'post_deleted', target_type: 'post', target_id: null, is_read: 1, snapshot: '{"deleted_by":"他","content":"被我删除的动态内容"}', created_at: new Date(now - 55 * 3600000).toISOString() },
  { id: 8, recipient_id: 2, event_type: 'comment_deleted', target_type: 'comment', target_id: null, is_read: 1, snapshot: '{"deleted_by":"他","content":"被你删除的评论"}', created_at: new Date(now - 60 * 3600000).toISOString() },
  { id: 9, recipient_id: 3, event_type: 'media_deleted', target_type: 'media', target_id: null, is_read: 1, snapshot: '{"deleted_by":"她"}', created_at: new Date(now - 65 * 3600000).toISOString() },
  { id: 10, recipient_id: 2, event_type: 'system', target_type: null, target_id: null, is_read: 1, snapshot: '{"message":"系统维护公告"}', created_at: new Date(now - 72 * 3600000).toISOString() },
]

// ---------- 地图点位（A7：无 merged_into；同名即同一点位，坐标以先创建者为准） ----------
export const mapPoints = [
  { id: 1, name: '厦门 · 鼓浪屿', lat: 24.4438, lng: 118.0683, visit_date: '2026-08-19', description: '', source: 'post', created_at: new Date(now - 1 * day).toISOString(), deleted_at: null, created_by: 2 },
  { id: 2, name: '杭州 · 西湖', lat: 30.2592, lng: 120.1481, visit_date: '2026-08-17', description: '', source: 'post', created_at: new Date(now - 3 * day).toISOString(), deleted_at: null, created_by: 2 },
  { id: 3, name: '上海 · 外滩', lat: 31.2397, lng: 121.4903, visit_date: '2026-08-12', description: '夜景超美', source: 'post', created_at: new Date(now - 8 * day).toISOString(), deleted_at: null, created_by: 3 },
  { id: 4, name: '苏州 · 平江路', lat: 31.3097, lng: 120.6309, visit_date: '2026-08-08', description: '', source: 'post', created_at: new Date(now - 12 * day).toISOString(), deleted_at: null, created_by: 3 },
  { id: 5, name: '厦门 · 曾厝垵', lat: 24.4305, lng: 118.1121, visit_date: '2026-08-05', description: '', source: 'manual', created_at: new Date(now - 15 * day).toISOString(), deleted_at: null, created_by: 2 },
]

// ---------- 地理编码内置表（geocode：名称 → 经纬度候选，离线无第三方依赖） ----------
export const GEOCODE_PLACES = [
  { name: '杭州', city: '杭州', lat: 30.2741, lng: 120.1551, display: '浙江·杭州' },
  { name: '杭州·西湖', city: '杭州', lat: 30.2467, lng: 120.4599, display: '浙江·杭州·西湖' },
  { name: '上海', city: '上海', lat: 31.2304, lng: 121.4737, display: '上海·上海' },
  { name: '上海·外滩', city: '上海', lat: 31.2397, lng: 121.4903, display: '上海·外滩' },
  { name: '北京', city: '北京', lat: 39.9042, lng: 116.4074, display: '北京·北京' },
  { name: '广州', city: '广州', lat: 23.1291, lng: 113.2644, display: '广东·广州' },
  { name: '深圳', city: '深圳', lat: 22.5431, lng: 114.0579, display: '广东·深圳' },
  { name: '成都', city: '成都', lat: 30.5728, lng: 104.0668, display: '四川·成都' },
  { name: '武汉', city: '武汉', lat: 30.5928, lng: 114.3055, display: '湖北·武汉' },
  { name: '西安', city: '西安', lat: 34.3416, lng: 108.9398, display: '陕西·西安' },
  { name: '重庆', city: '重庆', lat: 29.563, lng: 106.5516, display: '重庆·重庆' },
  { name: '南京', city: '南京', lat: 32.0603, lng: 118.7969, display: '江苏·南京' },
  { name: '苏州', city: '苏州', lat: 31.2989, lng: 120.5853, display: '江苏·苏州' },
  { name: '苏州·平江路', city: '苏州', lat: 31.3097, lng: 120.6309, display: '江苏·苏州·平江路' },
  { name: '厦门', city: '厦门', lat: 24.4798, lng: 118.0894, display: '福建·厦门' },
  { name: '厦门·鼓浪屿', city: '厦门', lat: 24.4438, lng: 118.0683, display: '福建·厦门·鼓浪屿' },
  { name: '厦门·曾厝垵', city: '厦门', lat: 24.4305, lng: 118.1121, display: '福建·厦门·曾厝垵' },
  { name: '大理', city: '大理', lat: 25.6065, lng: 100.2672, display: '云南·大理' },
  { name: '丽江', city: '丽江', lat: 26.8721, lng: 100.2299, display: '云南·丽江' },
  { name: '青岛', city: '青岛', lat: 36.0671, lng: 120.3826, display: '山东·青岛' },
]

// ---------- 地图瓦片源（多卡片：类型限 public/tianditu，enabled 至多一个=使用中） ----------
export const mapSources = [
  { id: 1, type: 'public', url_template: 'https://tile.openstreetmap.org/{z}/{x}/{y}.png', tianditu_key: '', tianditu_layer: 'vec_w', enabled: true, created_at: new Date(now - 30 * day).toISOString() },
  { id: 2, type: 'tianditu', url_template: '', tianditu_key: '', tianditu_layer: 'vec_w', enabled: false, created_at: new Date(now - 20 * day).toISOString() },
]

// ---------- AI 对话 ----------
export const chatHistory = [
  { id: 1, role: 'assistant', content: '你好呀！我是你的 AI 助手，可以帮你记录动态、纪念日、管理相册。今天有什么想记下的吗？', created_at: new Date(now - 2 * 3600000).toISOString() },
]
export const actions = new Map()

// 计数器（所有写操作共享）
export const seq = { post: 100, chat: 100, action: 100, comment: 100, album: 100, anniversary: 100, media: 100, mapPoint: 100, mapSource: 100, notification: 100, pat: 10, provider: 10, style: 10, webhook: 10, user: 10 }
// --- Admin（保留原有全文） ---
export const dashboardStats = [
  { label: '动态总数', value: '128', change: '+12 本月', trend: 'up', icon: 'message-square' },
  { label: '照片数量', value: '486', change: '+45 本月', trend: 'up', icon: 'image' },
  { label: '足迹城市', value: '12', change: '+2 本月', trend: 'up', icon: 'map-pin' },
  { label: '纪念日', value: '5', change: '下一个: 30天后', trend: 'neutral', icon: 'calendar' },
]
export const recentActivity = [
  { icon: 'message-square', text: '发布了一条新动态', time: '2 小时前' },
  { icon: 'image', text: '上传了 3 张照片', time: '5 小时前' },
  { icon: 'heart', text: '点赞了一条动态', time: '昨天' },
  { icon: 'message-circle', text: '发表了一条评论', time: '昨天' },
]
// AI 供应商（协议化配置，DESIGN §9）：列表有序，调用从第一个开始、失败依次向下（排序即故障切换链）。
// has_key：秘钥是否已配置（明文只写不回读）。
// models：每条 = { id, temperature|null, max_tokens|null }——参数挂模型级，null = 调用不传（用供应商默认）。
// default_model：models 中某项的 id。
export const modelProviders = [
  { id: 1, api_type: 'openai-chat', name: 'OpenAI', base_url: 'https://api.openai.com/v1', has_key: true, models: [
    { id: 'gpt-4o', temperature: null, max_tokens: null },
    { id: 'gpt-4o-mini', temperature: null, max_tokens: null },
  ], default_model: 'gpt-4o' },
  { id: 2, api_type: 'anthropic', name: 'Claude', base_url: 'https://api.anthropic.com', has_key: true, models: [
    { id: 'claude-sonnet-4-20250514', temperature: 0.7, max_tokens: null },
    { id: 'claude-opus-4-1-20250805', temperature: null, max_tokens: null },
  ], default_model: 'claude-sonnet-4-20250514' },
]
// 语义检索（嵌入模型）配置：复用某个 OpenAI Chat 协议供应商的地址与 Key 调 /embeddings；
// provider_id = 0 表示关闭语义检索（检索回落关键词）。语义与 server/internal/api/embedding.go 一致。
export const embeddingConfig = { provider_id: 0, model: '' }
export const systemPrompt = {
  content: '你们是这对情侣的生活助手。说话温暖简短，主动回顾共同记忆；写操作先出确认卡片，经发起人确认后才执行。',
  updatedAt: '2026-08-20 21:00',
}
// 输出风格库：内置风格完全固化（不可删改）；自定义风格由后台管理。
// prompt = 「说话语气」片段，拼在全局 System Prompt 主干之后；default_id = 全局默认（APP 未设置风格时使用）。
export const styles = [
  { id: 1, name: '文艺', prompt: '语气文艺有文采，善用比喻和意象，像写给彼此的信一样温柔。', builtin: true },
  { id: 2, name: '幽默', prompt: '语气幽默活泼，可以适当开玩笑和自嘲，营造轻松氛围。', builtin: true },
  { id: 3, name: '简洁', prompt: '回答尽量简短，直给要点，不啰嗦。', builtin: true },
]
export const styleDefaults = { id: 1 }
// schema：参数名册（键=参数名，值="类型 — 说明"）；空对象 = 无参数。与 DESIGN §6 工具清单对应
export const tools = [
  { name: 'create_post', description: '创建一条新动态', category: '动态', enabled: true, schema: { content: 'string — 动态正文', media_tokens: 'string[] — 已上传媒体令牌（可选）', location_name: 'string — 地点名（可选）' } },
  { name: 'delete_post', description: '删除动态（进入回收站）', category: '动态', enabled: true, schema: { post_id: 'number — 动态 ID' } },
  { name: 'search_posts', description: '语义搜索历史动态', category: '动态', enabled: true, schema: { query: 'string — 语义检索词' } },
  { name: 'get_today_in_history', description: '获取那年今日内容', category: '动态', enabled: true, schema: {} },
  { name: 'create_album', description: '创建专门相册', category: '相册', enabled: true, schema: { name: 'string — 相册名', description: 'string — 描述（可选）' } },
  { name: 'add_photos_to_album', description: '将照片加入相册', category: '相册', enabled: true, schema: { album_id: 'number — 相册 ID', media_ids: 'number[] — 待加入媒体 ID（来自动态媒体池）' } },
  { name: 'remove_photos_from_album', description: '从相册移除照片（不影响照片本身）', category: '相册', enabled: true, schema: { album_id: 'number — 相册 ID', media_ids: 'number[] — 待移除媒体' } },
  { name: 'suggest_photo_grouping', description: 'AI 推荐照片分组', category: '相册', enabled: true, schema: { media_ids: 'number[] — 待分组照片' } },
  { name: 'list_albums', description: '列出所有相册（时间轴/集合视图）', category: '相册', enabled: true, schema: { view_type: 'string — timeline（全部）或 collection（手动相册）', page: 'number — 页码（从 1 开始，可选）', page_size: 'number — 每页数量（可选）' } },
  { name: 'delete_album', description: '删除手动相册（进入回收站）', category: '相册', enabled: true, schema: { album_id: 'number — 相册 ID' } },
  { name: 'create_map_point', description: '创建地图点位（足迹）', category: '地图', enabled: true, schema: { name: 'string — 地点名', latitude: 'number — 纬度', longitude: 'number — 经度', visit_date: 'string — 到访日期 YYYY-MM-DD（可选）', description: 'string — 描述（可选）' } },
  { name: 'delete_map_point', description: '删除地图点位（进入回收站）', category: '地图', enabled: true, schema: { point_id: 'number — 点位 ID' } },
  { name: 'search_locations', description: '按地点名称搜索足迹', category: '地图', enabled: true, schema: { query: 'string — 地点关键词' } },
  { name: 'get_location_posts', description: '获取某地点下所有动态', category: '地图', enabled: true, schema: { location_id: 'number — 点位 ID' } },
  { name: 'create_anniversary', description: '创建纪念日', category: '纪念日', enabled: true, schema: { name: 'string — 名称', date: 'string — YYYY-MM-DD', type: 'solar | lunar', remind_days_before: 'number — 提前提醒天数' } },
  { name: 'delete_anniversary', description: '删除纪念日（进入回收站）', category: '纪念日', enabled: true, schema: { anniversary_id: 'number — 纪念日 ID' } },
  { name: 'get_next_anniversary', description: '获取下一个纪念日信息', category: '纪念日', enabled: true, schema: {} },
  { name: 'configure_reminder', description: '配置纪念日提醒', category: '纪念日', enabled: true, schema: { anniversary_id: 'number — 纪念日 ID', days_before: 'number — 提前提醒天数（0-90）' } },
  { name: 'get_space_summary', description: '空间概览（天数/动态/相册/足迹/纪念日聚合）', category: '空间', enabled: true, schema: {} },
  { name: 'get_timeline_summary', description: '时间线统计（按月/年分布）', category: '空间', enabled: true, schema: { granularity: 'month | year — 粒度' } },
  { name: 'toggle_like', description: '点赞/取消点赞动态', category: '互动', enabled: true, schema: { post_id: 'number — 动态 ID' } },
  { name: 'toggle_favorite', description: '收藏/取消收藏动态', category: '互动', enabled: true, schema: { post_id: 'number — 动态 ID' } },
  { name: 'list_favorites', description: '列出我收藏的动态', category: '互动', enabled: true, schema: { page: 'number — 页码' } },
  { name: 'create_comment', description: '发表评论', category: '互动', enabled: true, schema: { post_id: 'number — 动态 ID', content: 'string — 评论内容' } },
  { name: 'delete_comment', description: '删除评论（软删除，进入回收站）', category: '互动', enabled: true, schema: { comment_id: 'number — 评论 ID' } },
  { name: 'list_comments', description: '列出某条动态下的评论', category: '互动', enabled: true, schema: { post_id: 'number — 动态 ID' } },
]
export const recycle = [
  { id: 1, type: '动态', summary: '今天天气真好，适合出去走走...', deletedAt: '2026-08-20 14:30', deletedBy: 'AI 助手' },
  { id: 2, type: '评论', summary: '好漂亮的照片！', deletedAt: '2026-08-19 10:15', deletedBy: 'Boy' },
  { id: 3, type: '照片', summary: 'IMG_20260815.jpg', deletedAt: '2026-08-18 20:45', deletedBy: 'Girl' },
]
export const security = { recycleRetentionDays: 30, chatRetention: '30' }
// 回收站操作日志：操作人/时间/内容摘要（恢复 + 系统超期永久删除）；不可删除，永久保留（DESIGN §5/第9章）
export const restoreLog = [
  { time: '2026-08-21 09:12:44', operator: 'admin', summary: '动态"上次去海边的那组照片"' },
  { time: '2026-08-15 22:03:10', operator: 'admin', summary: '评论"下次还要一起去！"' },
]
// AI 调用量 / Token 消耗 / 平均延迟：近 14 天时序（仪表盘图表数据源）
export const aiUsage = [
  { date: '08-15', calls: 12, tokens: 15800, avgLatencyMs: 1150 }, { date: '08-16', calls: 8, tokens: 9600, avgLatencyMs: 980 },
  { date: '08-17', calls: 15, tokens: 21200, avgLatencyMs: 1320 }, { date: '08-18', calls: 6, tokens: 7400, avgLatencyMs: 870 },
  { date: '08-19', calls: 18, tokens: 26100, avgLatencyMs: 1490 }, { date: '08-20', calls: 22, tokens: 31500, avgLatencyMs: 1680 },
  { date: '08-21', calls: 9, tokens: 11800, avgLatencyMs: 1060 }, { date: '08-22', calls: 14, tokens: 18900, avgLatencyMs: 1240 },
  { date: '08-23', calls: 17, tokens: 24700, avgLatencyMs: 2130 }, { date: '08-24', calls: 21, tokens: 30200, avgLatencyMs: 1710 },
  { date: '08-25', calls: 11, tokens: 14600, avgLatencyMs: 1120 }, { date: '08-26', calls: 16, tokens: 22800, avgLatencyMs: 1380 },
  { date: '08-27', calls: 19, tokens: 27100, avgLatencyMs: 1560 }, { date: '08-28', calls: 7, tokens: 8900, avgLatencyMs: 940 },
]
// 异常告警（仪表盘）
export const alerts = [
  { id: 1, level: 'error', message: '08-23 一次 create_anniversary 调用超时（12.4s）', time: '2026-08-23 14:20' },
  { id: 2, level: 'warning', message: 'Bark 渠道近 7 天未收到任何订阅事件', time: '2026-08-22 08:00' },
]
// latencyMs：模型响应耗时，仅 ai/pat 调用有值；user 直接写库为 null
export const audit = [
  { time: '2026-08-24 10:30:15', actorType: 'user', actor: 'Girl', action: 'create_post', params: 'content: "周末出游"', result: 'success', latencyMs: null },
  { time: '2026-08-24 09:15:42', actorType: 'ai', actor: 'GPT-4o', action: 'search_posts', params: 'query: "海边"', result: 'success', latencyMs: 1320 },
  { time: '2026-08-23 22:05:18', actorType: 'pat', actor: 'Claude Desktop', action: 'list_favorites', params: 'page: 1', result: 'success', latencyMs: 861 },
  { time: '2026-08-23 18:30:05', actorType: 'user', actor: 'Boy', action: 'delete_comment', params: 'comment_id: 42', result: 'success', latencyMs: null },
  { time: '2026-08-23 14:20:33', actorType: 'ai', actor: 'GPT-4o', action: 'create_anniversary', params: 'name: "相识纪念日", date: "2023-05-20"', result: 'failed', latencyMs: 12400 },
]
export const tokens = [
  { id: 1, name: 'Claude Desktop', user: 'Girl', tier: 'readonly', createdAt: '2026-08-20', status: 'active' },
  { id: 2, name: '自定义 Agent', user: 'Boy', tier: 'readwrite', createdAt: '2026-08-15', status: 'active' },
  { id: 3, name: '测试令牌', user: 'Boy', tier: 'readonly', createdAt: '2026-08-10', status: 'revoked' },
]
export const webhooks = [
  { id: 1, name: 'Bark 推送', method: 'POST', url: 'https://api.day.app/xxx', headers: {}, body_template: '', events: ['anniversary', 'new_post'] },
  { id: 2, name: 'Server酱', method: 'POST', url: 'https://sctapi.ftqq.com/xxx.send', headers: {}, body_template: '', events: ['new_post', 'comment', 'like', 'favorite', 'footprint'] },
]

export const adminUsers = [
  { id: 1, username: 'admin', role: 'admin', nickname: '管理员', birthday: '', avatar: '' },
  { id: 2, username: 'girl', role: 'girl', nickname: '她', birthday: '1998-06-15', avatar: '' },
  { id: 3, username: 'boy', role: 'boy', nickname: '他', birthday: '1996-03-22', avatar: '' },
]
