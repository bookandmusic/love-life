<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Menu } from 'lucide-vue-next'
import { ApiError, api, errText, getBase, getToken } from '../api/client'
import AppToast from '../components/AppToast.vue'
import PageLayout from '../components/PageLayout.vue'
import { useAuth } from '../stores/auth'
import { loadCachedHistory, saveCachedHistory } from '../utils/chatCache'
import { aiReasonText, AI_STATUS_OFFLINE, type AIStatus } from '../utils/aiStatus'
import NavActions from '../components/NavActions.vue'
import MessageList from '../components/chat/MessageList.vue'
import CardActions from '../components/chat/CardActions.vue'
import InputBar from '../components/chat/InputBar.vue'
import SessionDrawer from '../components/chat/SessionDrawer.vue'

// 单次发送媒体上限（E7 保留）：防误传；与需求「连续追加」不冲突——未达上限前可继续追加选择
const MAX_MEDIA = 9 // 与 PublishView 对齐

import type { AttachmentRef, Bubble, CardItem, ChatSession, MediaView, PickedMedia, ProposalPreview, QuoteRef, UIBlock, UIRecord } from '../types/chat'
import MediaViewer from '../components/MediaViewer.vue'
import type { ViewerItem } from '../components/MediaViewer.vue'

// 类型统一在 types/chat.ts：父级与 MessageList / CardActions / InputBar 共用一份定义


// 历史回显：服务端下发 parts（AI模式设计「消息 parts 契约」），客户端按 part.type 渲染
interface HistoryMsg {
  id: number
  role: string
  parts: Array<
    | { type: 'text'; text: string }
    | { type: 'thinking'; text: string; seconds?: number }
    | { type: 'tool'; call_id: string; label: string; status: string; text?: string; ui?: UIBlock }
    | { type: 'card'; action_id: number }
    | { type: 'attachment'; name: string; mime?: string; size?: number }
    | { type: 'quote'; refs: QuoteRef[] }
    | { type: 'interrupted' }
  >
  card_id?: number
  created_at?: string
  quoted?: QuoteRef[]
  interrupted?: boolean
  card?: {
    action_id: number
    intent: string
    summary: string
    preview: ProposalPreview
    warnings?: string[]
    attachments?: AttachmentRef[]
    status: CardItem['status']
    expires_in: number
    result?: string
  }
}

const auth = useAuth()
const bubbles = ref<Bubble[]>([])
const cards = ref<Record<number, CardItem>>({})
const input = ref('')
// busy = 流式或确认请求进行中（此时输入区锁定）；待确认卡片不算 busy：
// 提案独立过期，用户可以继续说话，但不能切走或删掉当前会话。
const hasPending = computed(() => Object.values(cards.value).some((c) => c.status === 'pending'))
const busy = ref(false)
const streaming = ref(false)
const pickedMedia = ref<PickedMedia[]>([])
// 确认时才上传：pending 卡片 → 其携带的本地文件（action_id → 文件列表）
const pendingCardFiles = new Map<number, PickedMedia[]>()
const toast = ref('')
const streamRef = ref<InstanceType<typeof MessageList> | null>(null)
const inputBarRef = ref<InstanceType<typeof InputBar> | null>(null)
const error = ref('')
// 本轮标识与整轮回滚：turn_id 由 SSE turn 事件回告；turnStartIdx / turnCardIds
// 记本轮在本地产生的气泡与卡片，服务端回滚成功后据此同步移除（不靠文本匹配定位）。
let currentTurnId = ''
let turnStartIdx = 0
let turnCardIds: number[] = []
const chips = ref<string[]>([])
let abortCtrl: AbortController | null = null

// ---------- 多会话 ----------
// currentSessionId=0 为「草稿态」：抽屉点新会话后、首条消息送达前，本地无会话；
// 首条消息由服务端懒创建会话并经 SSE session 事件回告（缓存键/抽屉列表都靠它）
const sessions = ref<ChatSession[]>([])
const currentSessionId = ref(0)
const drawerOpen = ref(false)

async function loadSessions() {
  try {
    const res = await api<{ items: ChatSession[] }>('/api/v1/chat/sessions')
    sessions.value = res.items
  } catch {
    /* 列表拉取失败不阻断对话（抽屉内可重试） */
  }
}

// 会话切换 = 清空本地视图 + 按会话拉历史（缓存先展示、服务端覆盖，同 AP2）
async function loadHistory() {
  const uid = auth.user?.id ?? 0
  const sid = currentSessionId.value
  if (sid <= 0) {
    bubbles.value = []
    cards.value = {}
    return
  }
  // uid=0（用户未就绪的防御分支）跳过缓存，避免跨账号共享 history 键
  if (uid > 0) {
    const cached = (await loadCachedHistory(uid, sid)) as HistoryMsg[] | null
    if (cached?.length) applyHistory(cached)
  }
  try {
    const res = await api<{ items: HistoryMsg[] }>(`/api/v1/chat/history?session_id=${sid}`)
    applyHistory(res.items)
    if (uid > 0) void saveCachedHistory(uid, sid, res.items)
  } catch {
    // 拉取失败：有缓存则继续展示（提示离线），无缓存维持原状静默
    if (uid > 0) {
      const cached = (await loadCachedHistory(uid, sid)) as HistoryMsg[] | null
      if (cached?.length) {
        applyHistory(cached)
        showToast('网络不可用，已展示本地缓存')
      }
    }
  }
}

function resetTurnView() {
  bubbles.value = []
  cards.value = {}
  quotes.value = []
  error.value = ''
  turnBubble = null
  toolBubbles.clear()
}

function openDrawer() {
  drawerOpen.value = true
  void loadSessions() // 打开即刷新（标题/排序可能被其它设备改过）
}

// 流式中或还有未裁决提案时，禁切换/新建/删当前会话：回复与待传附件都锚定当前会话
function guardSession() {
  if (busy.value || hasPending.value) {
    showToast(busy.value ? '请等待当前回复完成' : '还有未确认的操作，请先处理')
    return true
  }
  return false
}

function newSession() {
  if (guardSession()) return
  if (currentSessionId.value === 0) {
    drawerOpen.value = false
    return // 已是草稿态，无需再建
  }
  currentSessionId.value = 0
  resetTurnView()
  drawerOpen.value = false
  inputBarRef.value?.focus()
}

async function selectSession(id: number) {
  drawerOpen.value = false
  if (id === currentSessionId.value) return
  if (guardSession()) return
  currentSessionId.value = id
  resetTurnView()
  await loadHistory()
}

async function removeSession(id: number) {
  if ((busy.value || hasPending.value) && id === currentSessionId.value) {
    showToast(busy.value ? '请等待当前回复完成' : '还有未确认的操作，请先处理')
    return
  }
  try {
    await api(`/api/v1/chat/sessions/${id}`, { method: 'DELETE' })
    sessions.value = sessions.value.filter((s) => s.id !== id)
    if (id === currentSessionId.value) {
      // 删的是当前会话：落到最新一条或草稿态
      currentSessionId.value = sessions.value.length ? sessions.value[0]!.id : 0
      resetTurnView()
      await loadHistory()
    }
    showToast('会话已删除')
  } catch {
    showToast('删除失败')
  }
}

// SSE session 事件：把会话归属同步进抽屉列表（懒创建的会话就地入列，无需等重新拉取）
function upsertSession(id: number, title: string) {
  if (!id) return
  const i = sessions.value.findIndex((s) => s.id === id)
  const nowIso = new Date().toISOString()
  if (i >= 0) {
    if (title) sessions.value[i]!.title = title
    sessions.value[i]!.updated_at = nowIso
  } else {
    sessions.value.unshift({ id, title: title || '新的会话', created_at: nowIso, updated_at: nowIso })
  }
}

function showToast(msg: string) {
  toast.value = msg
}

// push 后回读数组内的响应式代理再持有：直接持有 push 前的原始对象，后续就地累加
// （bubble.content += chunk / thinkOpen 切换）会绕过响应式代理，表现为「数据已写入但界面不更新」——
// 实测：回答正文只在下次无关重渲染时才出现（思考文本能显示，是靠 thinking prop 变化顺带触发）。
function pushBubble(b: Bubble): Bubble {
  bubbles.value.push(b)
  return bubbles.value[bubbles.value.length - 1]!
}

function scrollBottom() {
  nextTick(() => streamRef.value?.scrollBottom())
}

// 多选本地文件（不立即上传；确认执行时才经 /media/upload 换 token 落库，避免垃圾数据）
// 预览用 blob URL（与发布页同模式）：只在本机渲染，永不进请求体——消息体只带元数据
// （AI模式设计「图片处理与存储」：服务端不存文件、不存 base64）。
function pickMedia(e: Event) {
  const inputEl = e.target as HTMLInputElement
  const files = inputEl.files
  if (!files) return
  for (const f of Array.from(files)) {
    if (pickedMedia.value.length >= MAX_MEDIA) {
      showToast(`最多选择 ${MAX_MEDIA} 个文件`)
      break
    }
    if (!/^image\//.test(f.type) && !/^video\//.test(f.type) && !/^audio\//.test(f.type)) continue
    pickedMedia.value.push({ name: f.name, type: f.type, size: f.size, previewUrl: URL.createObjectURL(f), file: f })
  }
  inputEl.value = ''
}

/** 释放本机预览的 blob URL（移除单个 / 清空 / 卸载时调用），避免内存泄漏 */
function releasePreview(url?: string) {
  if (url) URL.revokeObjectURL(url)
}

/** 移除一个待发布媒体：同时释放其预览 blob URL */
function removeOneMedia(i: number) {
  const m = pickedMedia.value[i]
  if (m) releasePreview(m.previewUrl)
  pickedMedia.value.splice(i, 1)
}

// 确认执行时才上传：本地文件 → /media/upload → media_token（与「传统模式·发动态」同一管线）
// 逐文件带状态 + 3 次尝试指数退避 + 手动重传 + 全部就绪自动续提交
// （AI模式设计「图片处理与存储」部分上传失败整批把关；镜像 PublishView 的同款实现）。
type UpState = 'idle' | 'uploading' | 'ok' | 'failed'
// 每张卡的逐文件状态与已得 token（null = 尚未成功）；全部就绪后自动续提交。
// 用 ref 而非裸 Map：状态变化要驱动卡片内的失败标记与重传入口重渲染。
const cardUpStates = ref<Record<number, UpState[]>>({})
const cardTokens = ref<Record<number, (string | null)[]>>({})

/** 单个文件上传：首传 + 自动重试 2 次（500 * 3^a 退避），失败返回 null 转手动重传。 */
async function uploadOne(f: PickedMedia): Promise<string | null> {
  const attempts = 3
  for (let a = 0; a < attempts; a++) {
    try {
      const form = new FormData()
      form.append('file', f.file)
      const up = await api<{ media_token: string }>('/api/v1/media/upload', { method: 'POST', body: form })
      return up.media_token
    } catch {
      if (a < attempts - 1) await new Promise((r) => setTimeout(r, 500 * 3 ** a))
    }
  }
  return null
}

/** 逐个上传并维护状态；返回是否全部成功。已成功的不重传。 */
async function uploadPicked(cardID: number, files: PickedMedia[]): Promise<boolean> {
  const states: UpState[] = cardUpStates.value[cardID] ?? files.map(() => 'idle' as UpState)
  const tokens: (string | null)[] = cardTokens.value[cardID] ?? files.map(() => null)
  cardUpStates.value = { ...cardUpStates.value, [cardID]: states }
  cardTokens.value = { ...cardTokens.value, [cardID]: tokens }
  for (let i = 0; i < files.length; i++) {
    if (tokens[i]) continue // 已成功 token 保留，不重传
    states[i] = 'uploading'
    cardUpStates.value = { ...cardUpStates.value, [cardID]: [...states] }
    const tk = await uploadOne(files[i]!)
    if (tk) {
      tokens[i] = tk
      states[i] = 'ok'
    } else {
      states[i] = 'failed'
    }
    cardUpStates.value = { ...cardUpStates.value, [cardID]: [...states] }
    cardTokens.value = { ...cardTokens.value, [cardID]: [...tokens] }
  }
  return tokens.every((t) => t !== null)
}

/** 只清理本机上传状态（token 已被提交消费，不得再撤销）。 */
function clearCardUploadState(cardID: number) {
  const nextStates = { ...cardUpStates.value }
  const nextTokens = { ...cardTokens.value }
  delete nextStates[cardID]
  delete nextTokens[cardID]
  cardUpStates.value = nextStates
  cardTokens.value = nextTokens
}

/** 撤销某张卡已上传但未提交的 token（放弃 / 取消 / 过期 / 卸载；失败静默，24h janitor 兜底）。 */
function discardCardTokens(cardID: number) {
  const ts = (cardTokens.value[cardID] ?? []).filter((t): t is string => t !== null)
  clearCardUploadState(cardID)
  if (ts.length === 0) return
  void api('/api/v1/media/discard', { method: 'POST', body: JSON.stringify({ tokens: ts }) }).catch(() => {})
}

/** 放弃全部未提交 token（组件卸载时调用）。 */
function discardAllCardTokens() {
  for (const id of Object.keys(cardTokens.value).map(Number)) discardCardTokens(id)
}

// 4.1①：loadHistory 恢复 card 快照（未确认且未过期 → pending 可继续确认；过期/已处理 → 只读态）
// AP2：先展示 indexedDB 本地缓存（断网可用），后台拉服务端权威数据覆盖并回写缓存。
function applyHistory(items: HistoryMsg[]) {
  const nextBubbles: Bubble[] = []
  const nextCards: Record<number, CardItem> = {}
  for (const m of items) {
    // 卡片快照先登记（渲染时经 cardID 挂在对应气泡上），但**不提前 continue**——
    // 一条带卡片的助手消息同样有自己的 parts（思考链、工具步骤、正文），
    // 漏掉它们会让刷新后看不到「已思考 N 秒」与「已完成 N 步」（PRODUCT「过程呈现」）。
    let cardID: number | undefined
    if (m.card && m.card_id) {
      nextCards[m.card_id] = {
        action_id: m.card.action_id,
        intent: m.card.intent,
        summary: m.card.summary,
        preview: m.card.preview,
        warnings: m.card.warnings,
        attachments: m.card.attachments,
        status: m.card.status,
        expires_in: m.card.expires_in,
        result: m.card.result,
        received_at: Date.now(),
      }
      cardID = m.card_id
    }
    // parts 驱动：一条消息按 parts 顺序渲染为一个气泡序列的最小单元
    let content = ''
    let reasoning = ''
    let thinkSeconds: number | undefined
    const media: import('../types/chat').ChatMedia[] = []
    // 历史回显的附件只有元数据（无本机文件），故用展示类型而非 PickedMedia
    const local: MediaView[] = []
    const tools: Bubble[] = []
    let interrupted = false
    for (const part of m.parts ?? []) {
      if (part.type === 'text') content += part.text
      else if (part.type === 'thinking') { reasoning = part.text; thinkSeconds = part.seconds }
      else if (part.type === 'tool') {
        // 步骤条文案 = 人话标签 + 结果（「搜索动态 → 找到 3 条」）：label 是注册表给的人话，
        // text 是本步结果；两者都来自 parts，刷新后与流式中看到的形态一致。
        tools.push({
          role: 'tool',
          content: [part.label, part.text].filter(Boolean).join(' → ') || '已完成',
          toolLabel: [part.label, part.text].filter(Boolean).join(' → ') || '已完成',
          toolStatus: part.status === 'failed' ? 'failed' : 'ok',
          ui: part.ui,
        })
      } else if (part.type === 'attachment') {
        // 刷新回显：只有 parts 里的元数据，本机文件与预览地址都已不存在（文档「刷新后仍能知道发过 N 个文件」）
        local.push({ name: part.name, type: part.mime || '', size: part.size || 0 })
      } else if (part.type === 'quote') {
        // 引用条回显（refresh 后仍可提示已引用条目）
      } else if (part.type === 'interrupted') {
        interrupted = true
      }
    }
    if (m.interrupted) interrupted = true
    const mainBubble: Bubble = {
      role: m.role as Bubble['role'], content,
      media: media.length ? media : undefined,
      localMedia: local.length ? local : undefined,
      reasoning, thinkOpen: false, thinkSeconds,
      quoted: (m.quoted ?? []).map(quoteLabel), interrupted,
    }
    // 卡片挂在本条消息的主气泡上（其 parts 的思考与工具步骤照常各自成行）
    if (cardID !== undefined) mainBubble.cardID = cardID
    nextBubbles.push(mainBubble)
    nextBubbles.push(...tools)
  }
  bubbles.value = nextBubbles
  cards.value = nextCards
  scrollBottom()
}

// 4.1②：chips 点击填入输入框（「点击填入输入框」，不直接发送）
function fillChip(c: string) {
  input.value = c + '：'
  inputBarRef.value?.focus()
}

const quotes = ref<QuoteRef[]>([])
const openKeys = ref<string[]>([])
const viewerItems = ref<ViewerItem[] | null>(null)
const viewerStart = ref(0)

const KIND_CN: Record<string, string> = { post: '动态', media: '照片', album: '相册', anniversary: '纪念日', place: '地点', comment: '评论' }

function quoteLabel(q: QuoteRef): QuoteRef {
  if (q.label) return q
  if (q.group) return { ...q, label: `引用了分组「${q.group}」` }
  if (q.ids?.length) return { ...q, label: `引用了 ${q.ids.length} 条` }
  if (q.kind && q.id) return { ...q, label: `引用了${KIND_CN[q.kind] || '内容'} #${q.id}` }
  return { ...q, label: q.result_id ? `引用了结果集 ${q.result_id}` : '引用了资源' }
}

function quoteKey(q: QuoteRef): string {
  if (q.group) return `${q.result_id}:group:${q.group}`
  if (q.kind && q.id) return `one:${q.kind}:${q.id}`
  return q.result_id || ''
}

const pickedKeys = computed(() => quotes.value.flatMap((q) => {
  if (q.group || !q.ids?.length) return [quoteKey(q)]
  return q.ids.map((id) => `${q.result_id}:${id}`)
}))

// loadMore 列表卡「再看几条」：把该结果集挂进引用条，再请 AI 按同一查询推进偏移量。
// 翻页由 AI 调 find_* 的 offset 完成（offset 是工具参数，ModelText 会回显区间）；
// 带上引用是为了让范围与上一卡一致——不然模型可能换查询条件。
function loadMore(ui: Bubble['ui']) {
  if (!ui?.result_id || busy.value) return
  if (!quotes.value.some((q) => q.result_id === ui.result_id)) {
    quotes.value.push({ result_id: ui.result_id, label: '引用上一次的查询结果' })
  }
  const noun = ui.records?.[0]?.kind
  const what = noun ? KIND_CN[noun] || '内容' : '内容'
  void send(`再看几条${what}`)
}

function togglePick(resultId: string, rec: UIRecord, single: boolean) {
  const noun = KIND_CN[rec.kind] || '内容'
  if (rec.group_key) {
    const key = `${resultId}:group:${rec.group_key}`
    const i = quotes.value.findIndex((q) => quoteKey(q) === key)
    if (i >= 0) quotes.value.splice(i, 1)
    else quotes.value.push({ result_id: resultId, group: rec.group_key, label: `引用了分组「${rec.text || rec.group_key}」` })
    return
  }
  // 同一结果集的多条勾选合成一条引用（服务端按 ids 取交集，拆成多条会变成多次「勾选的 1 条」）
  let q = quotes.value.find((x) => x.result_id === resultId && !x.group)
  if (single) {
    quotes.value = quotes.value.filter((x) => x.result_id !== resultId)
    q = undefined
  }
  if (!q) {
    q = { result_id: resultId, ids: [], label: '' }
    quotes.value.push(q)
  }
  const ids = q.ids ?? []
  const at = ids.indexOf(rec.id)
  if (at >= 0) ids.splice(at, 1)
  else ids.push(rec.id)
  q.ids = ids
  q.label = `引用了 ${ids.length} 条${noun}`
  if (ids.length === 0) quotes.value = quotes.value.filter((x) => x !== q)
}

function toggleOpen(key: string) {
  const i = openKeys.value.indexOf(key)
  if (i >= 0) openKeys.value.splice(i, 1)
  else openKeys.value.push(key)
}

function openMedia(rec: UIRecord) {
  if (!rec.thumb_url) return
  viewerItems.value = [{ id: rec.id, kind: rec.kind === 'media' ? 'photo' : rec.kind, thumb_url: rec.thumb_url, url: rec.thumb_url }]
  viewerStart.value = 0
}

function stopStream() {
  if (!streaming.value) return // 卡片待确认/确认执行中不可中止
  // 先抓本轮 id 与起点再中断：abort() 会让 send() 的 finally 立即清空这两个变量，
  // 若等 abortTurn() 自己读就都已归零（曾因此整轮回滚静默失效、DOM 残留幽灵气泡）。
  const turn = currentTurnId
  const startIdx = turnStartIdx
  abortCtrl?.abort()
  void abortTurn(turn, startIdx)
}

// 停止即整轮回滚（改版）：撤回是**用户意图**，由本接口显式触发——
// 旧版同时靠「服务端看到连接断开就删消息」，网络抖动/切后台会把用户刚发的消息静默吞掉。
// 服务端返回 aborted=false（本轮已答完或 id 不归属）时保留本地气泡，不谎报删成功。
async function abortTurn(turn: string, startIdx: number) {
  currentTurnId = ''
  if (!turn) return // 未收到 turn 事件（极早断线）：服务端未落任何本轮内容之外，不动本地
  try {
    const res = await api<{ aborted: boolean }>(`/api/v1/chat/turns/${turn}/abort`, { method: 'POST' })
    if (!res.aborted) return
    for (const id of turnCardIds) {
      delete cards.value[id]
      pendingCardFiles.delete(id)
      discardCardTokens(id) // 整轮回滚 → 本卡已上传但未提交的 token 一并撤销
    }
    if (startIdx > 0) bubbles.value.splice(startIdx) // 本轮用户气泡及其后所有残片一并撤掉
  } catch {
    /* 回滚失败保留原状：宁可多显示一行，不可谎称已撤回 */
  } finally {
    turnCardIds = []
    turnStartIdx = 0
  }
}

// ---------- ：确认卡片 15 分钟有效期（客户端侧） ----------
// 服务端 expires_in 是「相对收到时刻」的秒数；本地记 received_at 推算剩余分钟，
// 到 0 直接置 expired（置灰不可确认），真正的过期裁决仍在服务端（410）
const now = ref(Date.now())
function remainingMin(c: CardItem): number {
  if (!c.received_at) return Math.ceil((c.expires_in ?? 900) / 60)
  // 用实时时钟算：now 每 30s 才跳一次，若拿它当基准会把 15 分钟显示成 16 分钟（最多虚高 1 分钟）
  const left = c.received_at + (c.expires_in ?? 900) * 1000 - Date.now()
  return Math.max(0, Math.ceil(left / 60000))
}
function isCardExpired(c: CardItem): boolean {
  return remainingMin(c) <= 0
}
watch(now, () => {
  // 提交进行中不做过期清理：卡片在 resolve 返回前一直是 pending，此时撤销 token
  // 会把正在提交的这批 token 抽掉，让一次合法确认变成「token 失效整批失败」。
  if (busy.value) return
  for (const c of Object.values(cards.value)) {
    if (c.status === 'pending' && isCardExpired(c)) {
      c.status = 'expired'
      // 过期即放弃：已上传但未提交的 token 立即撤销（AI模式设计「图片处理与存储」：
      // 放弃 / 过期时对已上传 token 调 discard，来不及撤销的由 24h 孤儿清理兜底）。
      discardCardTokens(c.action_id)
    }
  }
})

// 本轮主气泡：思考与回答落在同一条；工具状态条按 call_id 单独成条
let turnBubble: Bubble | null = null
const toolBubbles = new Map<string, Bubble>()
// 等待指示：streaming 且本轮尚无内容落泡（reasoning/text/card 均未到）时为 true
const thinking = ref(false)

// 折叠思考区（正文/卡片到达即算思考完成），耗时按发送起点计
let turnStartedAt = 0
function collapseThink() {
  if (turnBubble && turnBubble.reasoning) {
    turnBubble.thinkOpen = false
    if (!turnBubble.thinkSeconds) {
      turnBubble.thinkSeconds = Math.max(1, Math.round((Date.now() - turnStartedAt) / 1000))
    }
  }
}

function ensureTurn(): Bubble {
  if (!turnBubble) turnBubble = pushBubble({ role: 'assistant', content: '' })
  return turnBubble
}

async function send(text?: string) {
  if (aiUnavailableReason.value) return
  const content = (text ?? input.value).trim()
  // 只提交元数据：文件在「确认执行」时才从本机上传（AI模式设计「图片处理与存储」）
  const media = pickedMedia.value.map((m) => ({ name: m.name, type: m.type, size: m.size }))
  const sentQuotes = quotes.value.map(({ label: _label, ...q }) => q)
  if ((!content && media.length === 0 && sentQuotes.length === 0) || busy.value) return
  const sentFiles = pickedMedia.value.slice()
  const echoQuotes = quotes.value.slice()
  // 用户气泡的本机预览：blob URL + 元数据（回显与「发送中」两处共用同一结构）
  const echoMedia = sentFiles.map((m) => ({ name: m.name, type: m.type, size: m.size, previewUrl: m.previewUrl }))
  input.value = ''
  // 清空选择：释放预览 blob URL（文件本身在 sentFiles 里继续用于确认执行时的上传）
  pickedMedia.value.forEach((m) => releasePreview(m.previewUrl))
  pickedMedia.value = []
  quotes.value = []
  busy.value = true
  streaming.value = true
  error.value = ''
  turnBubble = null
  toolBubbles.clear()
  thinking.value = true
  turnStartedAt = Date.now()
  currentTurnId = ''
  turnCardIds = []
  turnStartIdx = bubbles.value.length

  // 4.3：支持中止请求
  abortCtrl = new AbortController()
  try {
    const res = await fetch(getBase() + '/api/v1/chat/message', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${getToken() ?? ''}`,
      },
      body: JSON.stringify({ content, media, session_id: currentSessionId.value, quoted: sentQuotes }),
      signal: abortCtrl.signal,
    })
    if (!res.ok || !res.body) {
      const msg = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
      throw new Error((msg as { error?: string }).error ?? '发送失败')
    }

    pushBubble({ role: 'user', content, media: echoMedia, quoted: echoQuotes })
    scrollBottom()

    const reader = res.body.getReader()
    const decoder = new TextDecoder()
    let buf = ''
    // 收到服务端 done 事件即可结束本轮流式（即使连接未关闭，也避免「进行中」卡住）
    let streamEnded = false
    for (;;) {
      if (streamEnded) break
      const { done, value } = await reader.read()
      if (done) break
      buf += decoder.decode(value, { stream: true })
      const events = buf.split('\n\n')
      buf = events.pop() ?? ''
      for (const evt of events) {
        const lines = evt.split('\n')
        const ev = lines.find((l) => l.startsWith('event: '))?.slice(7)
        const dataLine = lines.find((l) => l.startsWith('data: '))
        if (!ev || !dataLine) continue
        let data
        try {
          data = JSON.parse(dataLine.slice(6))
        } catch {
          continue // C8：单条脏帧（截断/非 JSON）跳过，不中断整轮流式
        }
        if (ev === 'session') {
          currentSessionId.value = Number(data.session_id) || 0
          upsertSession(currentSessionId.value, String(data.title ?? ''))
        } else if (ev === 'turn.start') {
          currentTurnId = String(data.turn_id ?? '')
        } else if (ev === 'thinking.delta') {
          const b = ensureTurn()
          b.thinkOpen = true
          const chunk = typeof data.text === 'string' ? data.text : ''
          if (chunk) b.reasoning = b.reasoning ? `${b.reasoning}\n\n${chunk}` : chunk
          thinking.value = false
        } else if (ev === 'thinking.end') {
          const b = ensureTurn()
          if (typeof data.seconds === 'number') b.thinkSeconds = data.seconds
          collapseThink()
        } else if (ev === 'answer.delta') {
          const chunk = typeof data.text === 'string' ? data.text : ''
          if (chunk) ensureTurn().content += chunk
          thinking.value = false
          collapseThink()
        } else if (ev === 'tool.start') {
          const id = String(data.call_id ?? '')
          const b = pushBubble({ role: 'tool', content: String(data.label ?? '处理中'), toolLabel: String(data.label ?? '处理中'), toolStatus: 'running' })
          if (id) toolBubbles.set(id, b)
          thinking.value = false
        } else if (ev === 'tool.end') {
          const b = toolBubbles.get(String(data.call_id ?? ''))
          if (b) {
            const failed = data.status === 'failed'
            b.toolStatus = failed ? 'failed' : 'ok'
            // 步骤条保持人话标签（tool.start 给的动作名），结果追加在后
            // （「搜索动态 → 找到 3 条」）。失败时**不能**把 text 当标签——那是内部错误原文
            // （含句柄、规则细节），既不是人话也属审计信息（AI模式设计「时序与事件」）。
            const result = typeof data.text === 'string' ? data.text : ''
            const head = b.content || '处理中'
            if (failed) {
              // 失败原因由 AI 在回答里转述（它拿到了 errText）；步骤条只标「没成功」
              b.toolLabel = `${head}（没成功）`
              b.content = b.toolLabel
            } else if (result) {
              b.toolLabel = `${head} → ${result}`
              b.content = b.toolLabel
            }
            if (data.ui) b.ui = data.ui
          }
          thinking.value = false
          collapseThink()
        } else if (ev === 'proposal') {
          collapseThink()
          const p = data.proposal ?? {}
          const id = Number(p.id)
          if (!id) continue
          cards.value[id] = {
            action_id: id, intent: p.intent, summary: p.summary, preview: p.preview,
            warnings: p.warnings, attachments: p.attachments, status: 'pending',
            expires_in: p.expires_in, received_at: Date.now(),
          }
          turnCardIds.push(id)
          if (sentFiles.length) pendingCardFiles.set(id, sentFiles)
          const cardBubble = pushBubble({ role: 'assistant', content: '', localMedia: sentFiles.length ? sentFiles : undefined })
          cardBubble.cardID = id
          thinking.value = false
        } else if (ev === 'answer.end') {
          // 「回答结束」与「整轮结束」是两个语义：预算收尾时 answer.end 先于 turn.end 到达，
          // 之后不再有正文增量。此处只收掉本段回答的进行态（思考指示 + 思考区折叠），
          // **不**结束整轮、不置 streamEnded——连接仍开着，后续事件（如 turn.end）照常处理。
          thinking.value = false
          collapseThink()
        } else if (ev === 'turn.end') {
          streamEnded = true
        } else if (ev === 'error') {
          error.value = data.message
        }
        scrollBottom()
      }
    }
    collapseThink()
  } catch (e) {
    if (e instanceof Error && e.name === 'AbortError') {
      // 用户主动停止：不算错误
      collapseThink()
    } else {
      error.value = errText(e, '发送失败')
    }
  } finally {
    streaming.value = false
    thinking.value = false
    abortCtrl = null
    // 本轮正常答完（含以待确认收尾）后，它就不再是可中止的那一轮
    currentTurnId = ''
    turnStartIdx = 0
    busy.value = false
  }
}
// 气泡 → 确认卡片（模板去重：一次访问卡 ID 与卡片对象）
function cardOf(b: Bubble): CardItem | undefined {
  return b.cardID == null ? undefined : cards.value[b.cardID]
}

async function confirmCard(id: number, confirmed: boolean) {
  const card = cards.value[id]
  if (!card || card.status !== 'pending') return
  // 附件丢失守卫（2026-09-22 实测）：卡片带 media_refs（确认时上传），但本地文件已不在
  //（刷新/跨设备回显后 pendingCardFiles 为空）——继续确认会失败（纯媒体卡）或悄悄丢图
  //（有正文的卡会建出无媒体的动态），此时应拦下并提示重新发起。
  const needMedia = (card.attachments?.length ?? 0) > 0
  if (confirmed && needMedia && !(pendingCardFiles.get(id)?.length)) {
    showToast('附件已不在本机，请取消后重新发起')
    return
  }
  const files = pendingCardFiles.get(id) ?? []
  // 取消：本卡不提交，已上传的 token 立即撤销
  if (!confirmed) {
    discardCardTokens(id)
    pendingCardFiles.delete(id)
    busy.value = true
    streaming.value = false
    try {
      await resolveCard(id, 'cancel', [])
    } finally {
      busy.value = false
    }
    return
  }
  busy.value = true // 确认请求期间右下角保持「进行中」
  streaming.value = false
  try {
    // 部分上传失败：**不提交 resolve**，卡片保持待确认，失败文件可单独重传
    //（AI模式设计「图片处理与存储」：提案保持待确认、已成功 token 保留不重传）。
    if (files.length) {
      const allOk = await uploadPicked(id, files)
      if (!allOk) {
        card.result = '部分附件上传失败，可重传后自动提交'
        return
      }
    }
    const mediaTokens = (cardTokens.value[id] ?? []).filter((t): t is string => t !== null)
    await resolveCard(id, 'approve', mediaTokens)
    // 已成功提交：token 已被动态消费，只清本机状态，**不可**再撤销
    pendingCardFiles.delete(id)
    clearCardUploadState(id)
  } finally {
    // 拿到后端处理结果、或上传失败中止 → 「结束」
    busy.value = false
  }
}

/** 提交裁决并落卡片状态（确认与取消共用）。 */
async function resolveCard(id: number, decision: 'approve' | 'cancel', mediaTokens: string[]) {
  const card = cards.value[id]
  if (!card) return
  try {
    const res = await api<{ status: string; result?: string; error?: string }>(`/api/v1/chat/proposals/${id}/resolve`, {
      method: 'POST',
      body: JSON.stringify({ decision, media_tokens: mediaTokens }),
    })
    card.status = decision === 'approve' ? (res.status === 'executed' ? 'executed' : 'failed') : 'cancelled'
    card.result = res.result ?? res.error
    // 结果只呈现在卡片内（与刷新后回显一致），不再另推「已执行：…」气泡
    scrollBottom()
  } catch (e) {
    // 409=已处理 / 410=已过期（服务端判定）：其余才是真失败，别一律标成「已过期」
    const status = e instanceof ApiError ? e.status : 0
    card.status = status === 409 || status === 410 ? 'expired' : 'failed'
    card.result = errText(e, '确认失败')
    // 已过期 / 已处理：本机 token 不会再被这条提案消费，撤销掉
    discardCardTokens(id)
  }
}

/** 手动重传单个失败附件；全部就绪后自动续提交（无需再点确认）。 */
async function retryCardMedia(id: number) {
  const card = cards.value[id]
  const files = pendingCardFiles.get(id) ?? []
  if (!card || card.status !== 'pending' || !files.length) return
  busy.value = true
  streaming.value = false
  try {
    const allOk = await uploadPicked(id, files)
    if (!allOk) return
    const mediaTokens = (cardTokens.value[id] ?? []).filter((t): t is string => t !== null)
    await resolveCard(id, 'approve', mediaTokens)
    // 重传后自动提交成功：同上，token 已被消费
    pendingCardFiles.delete(id)
    clearCardUploadState(id)
  } finally {
    busy.value = false
  }
}

/** 该卡是否处于「部分上传失败」状态（卡片内提示 + 重传入口）。 */
function cardUploadFailed(id: number): boolean {
  return (cardUpStates.value[id] ?? []).some((s) => s === 'failed')
}

// AI 可用性（后台未配置模型时禁用输入/发送并提示原因；与设置页模式置灰同源）
// reason 的加工统一走 utils/aiStatus，保证与设置页文案一致
const aiUnavailableReason = ref('')
// 空会话欢迎语：服务端模板拼装空间事实，不调模型、不落库，仅本地渲染在空态副文案上
const welcomeText = ref('')

interface BootstrapPayload {
  available: boolean
  reason: string
  welcome?: string
  chips?: string[]
}

// loadBootstrap：首屏一次拿全（可用性 + 欢迎语 + chips），替代旧「拉 /status + 客户端硬编 chips」。
async function loadBootstrap() {
  try {
    const b = await api<BootstrapPayload>('/api/v1/chat/bootstrap')
    aiUnavailableReason.value = b.available ? '' : aiReasonText(b)
    welcomeText.value = b.welcome ?? ''
    if (b.chips?.length) chips.value = b.chips
  } catch {
    aiUnavailableReason.value = AI_STATUS_OFFLINE
  }
}

// loadAIStatus：锁定态轮询专用——它只回答“能不能用”，不重算欢迎语，15s 一次才廉价。
async function loadAIStatus() {
  try {
    const s = await api<AIStatus>('/api/v1/chat/status')
    aiUnavailableReason.value = s.available ? '' : aiReasonText(s)
  } catch {
    aiUnavailableReason.value = AI_STATUS_OFFLINE
  }
}

// 锁定时低频轮询状态：停留期间 admin 配好模型即自动解锁，无需退出重进；
// 解锁即停（发送按钮在锁定态是禁用的，无法靠「发送时重拉」触发，只能主动轮询）
let aiPollTimer: number | undefined
watch(aiUnavailableReason, (reason) => {
  if (reason && !aiPollTimer) {
    aiPollTimer = window.setInterval(() => void loadAIStatus(), 15 * 1000)
  } else if (!reason && aiPollTimer) {
    window.clearInterval(aiPollTimer)
    aiPollTimer = undefined
  }
})

onMounted(async () => {
  void loadBootstrap()
  expiryTimer = window.setInterval(() => (now.value = Date.now()), 30000)
  // 先取会话列表，落最新会话（无会话 = 草稿态），再按会话拉历史
  await loadSessions()
  currentSessionId.value = sessions.value.length ? sessions.value[0]!.id : 0
  await loadHistory()
})

let expiryTimer: number | undefined
onBeforeUnmount(() => {
  if (expiryTimer) window.clearInterval(expiryTimer)
  if (aiPollTimer) window.clearInterval(aiPollTimer)
  // 卸载即放弃未提交的上传 token（AI模式设计「图片处理与存储」：放弃 / 过期对已上传 token 调 discard）
  discardAllCardTokens()
})
</script>

<template>
  <PageLayout class="chat-page" title="我们的 AI 助手" standalone :scroll="false">
    <template #nav-left>
 <!-- 左上角菜单（替代返回键）唤起会话抽屉；回传统模式走右上设置页 -->
      <button class="menu-btn" aria-label="会话列表" @click="openDrawer">
        <Menu :size="22" stroke-width="2" />
      </button>
    </template>
    <template #nav-right>
      <NavActions :show-bell="false" />
    </template>

    <MessageList
      ref="streamRef"
      :bubbles="bubbles"
      :error="error"
      :picked-keys="pickedKeys"
      :open-keys="openKeys"
      :unavailable-reason="aiUnavailableReason"
      :welcome="welcomeText"
      :thinking="thinking"
      @toggle-pick="togglePick"
      @toggle-open="toggleOpen"
      @open-media="openMedia"
      @load-more="loadMore"
    >
      <template #card="{ bubble }">
        <CardActions
          v-if="cardOf(bubble)"
          :card="cardOf(bubble)!"
          :bubble="bubble"
          :remaining-min="remainingMin"
          :is-card-expired="isCardExpired"
          :upload-failed="cardOf(bubble)!.action_id ? cardUploadFailed(cardOf(bubble)!.action_id) : false"
          @confirm="confirmCard"
          @retry-media="retryCardMedia"
        />
      </template>
    </MessageList>
    <div v-if="bubbles.length === 0 && !aiUnavailableReason" class="chips">
      <button v-for="c in chips" :key="c" @click="fillChip(c)">{{ c }}</button>
    </div>
    <!-- 有历史对话时 MessageList 的空态不渲染，此处补一条提示，避免只看到禁用输入框却不知原因 -->
    <p v-if="bubbles.length > 0 && aiUnavailableReason" class="ai-offline">{{ aiUnavailableReason }}</p>

    <template #footer>
    <InputBar
      ref="inputBarRef"
      :input="input"
      :picked-media="pickedMedia"
      :quotes="quotes"
      :busy="busy"
      :streaming="streaming"
      :max-media="MAX_MEDIA"
      :unavailable-reason="aiUnavailableReason"
      @update:input="input = $event"
      @pick="pickMedia"
      @remove="removeOneMedia"
      @unquote="quotes.splice($event, 1)"
      @send="send()"
      @stop="stopStream()"
    />
    </template>
    <template #overlay>
      <!-- 页内浮层：锚定会话窗口框架（桌面档落在浮动卡片内），非浏览器视口弹窗 -->
      <SessionDrawer
        :open="drawerOpen"
        :sessions="sessions"
        :current-id="currentSessionId"
        :busy="busy"
        @close="drawerOpen = false"
        @new="newSession"
        @select="selectSession"
        @remove="removeSession"
      />
    </template>
    <MediaViewer v-if="viewerItems" :items="viewerItems" :start="viewerStart" mode="post" @close="viewerItems = null" />
    <AppToast :text="toast" bottom="5rem" pill @done="toast = ''" />
  </PageLayout>
</template>

<style scoped>
/* 注入 PageLayout 的 --page-bg 钩子：AI 对话页整页底色（含桌面档卡片），
   保证暗色档卡片与消息流同色，不与 --surface 撞色（重构前由 .chat 根承担）。
   注意：本组件模板必须保持单一根——根部上方的 HTML 注释在 dev 构建中会保留为注释节点，
   使组件变成多根片段，卡死 App.vue 顶层 out-in 过渡（离开本页即全站白屏）。 */
.chat-page {
  --page-bg: var(--ai-bg, var(--bg));
}
/* AI 专属头像标识：助手气泡左侧固定标识，用户气泡不加 */
/* 搜索结果卡片（内联，无跳转） */
/* 左上角菜单按钮：与 PageLayout 的 nav-btn 同视觉（scoped 下不复用其类，本地等价实现） */
.menu-btn {
  border: none;
  background: none;
  color: var(--tint);
  cursor: pointer;
  padding: 4px;
  display: flex;
  align-items: center;
}

.chips {
  display: flex;
  gap: 0.5rem;
  padding: 0 1rem 0.5rem;
  flex-wrap: wrap;
  background: var(--ai-bg, var(--bg));
}
.chips button {
  border: 1px solid var(--rule);
  background: var(--surface);
  border-radius: 999px;
  padding: 0.35rem 0.9rem;
  font-size: 0.8125rem;
  color: var(--primary-deep);
}

/* AI 不可用提示（有历史对话时补位，替代 MessageList 的空态）；底色由 --page-bg 钩子承担 */
.ai-offline {
  margin: 0;
  padding: 0.5rem 1rem 0.75rem;
  font-size: 0.8125rem;
  color: var(--muted-ink);
  text-align: center;
}

.error {
  color: var(--danger);
  font-size: 0.8125rem;
  text-align: center;
}
</style>
