<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
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

// 单次发送媒体上限（E7 保留）：防误传；与需求「连续追加」不冲突——未达上限前可继续追加选择
const MAX_MEDIA = 9 // 与 PublishView 对齐

import type { Bubble, CardItem, PickedMedia, SearchItem, PostDetailItem } from '../types/chat'

// 类型统一在 types/chat.ts：父级与 MessageList / CardActions / InputBar 共用一份定义


interface HistoryMsg {
  id: number
  role: string
  content: string
  card_id?: number
  created_at?: string
  media?: import('../types/chat').ChatMedia[]
  card?: { action_id: number; tool: string; params: Record<string, unknown>; status: CardItem['status']; expires_in: number; result?: string }
}

const auth = useAuth()
const bubbles = ref<Bubble[]>([])
const cards = ref<Record<number, CardItem>>({})
const input = ref('')
// busy = 本轮「进行中」总开关（流式 + 卡片待确认 + 确认执行中）；streaming = 仅 SSE 流式阶段（可中止）
const busy = ref(false)
const streaming = ref(false)
const pickedMedia = ref<PickedMedia[]>([])
// 确认时才上传：pending 卡片 → 其携带的本地文件（action_id → 文件列表）
const pendingCardFiles = new Map<number, PickedMedia[]>()
const toast = ref('')
const streamRef = ref<InstanceType<typeof MessageList> | null>(null)
const inputBarRef = ref<InstanceType<typeof InputBar> | null>(null)
const error = ref('')
const chips = ['帮我发动态', '整理最近的照片', '上次去海边是什么时候']
let abortCtrl: AbortController | null = null

function showToast(msg: string) {
  toast.value = msg
}

function scrollBottom() {
  nextTick(() => streamRef.value?.scrollBottom())
}

// 多选本地文件（不立即上传；确认执行时才经 /media/upload 换 token 落库，避免垃圾数据）
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
    const reader = new FileReader()
    reader.onload = () => {
      pickedMedia.value.push({ name: f.name, type: f.type, size: f.size, data_url: String(reader.result), file: f })
    }
    reader.readAsDataURL(f)
  }
  inputEl.value = ''
}

// 确认执行时才上传：本地文件 → /media/upload → media_token（与「传统模式·发动态」同一管线）
async function uploadPicked(files: PickedMedia[]): Promise<string[]> {
  const tokens: string[] = []
  for (const f of files) {
    const form = new FormData()
    form.append('file', f.file)
    const up = await api<{ media_token: string }>('/api/v1/media/upload', { method: 'POST', body: form })
    tokens.push(up.media_token)
  }
  return tokens
}

// 4.1①：loadHistory 恢复 card 快照（未确认且未过期 → pending 可继续确认；过期/已处理 → 只读态）
// AP2：先展示 indexedDB 本地缓存（断网可用），后台拉服务端权威数据覆盖并回写缓存。
function applyHistory(items: HistoryMsg[]) {
  const nextBubbles: Bubble[] = []
  const nextCards: Record<number, CardItem> = {}
  for (const m of items) {
    // 历史遗留的惰性 marker（2026-09-21 起服务端停写）：无可交互内容的纯文字，过滤不展示
    if (m.role === 'assistant' && !m.card_id && m.content === '已生成确认卡片，等待确认') continue
    if (m.card && m.card_id) {
      nextCards[m.card_id] = {
        action_id: m.card.action_id,
        tool: m.card.tool,
        params: m.card.params ?? {},
        status: m.card.status,
        expires_in: m.card.expires_in,
        result: m.card.result, // 执行结果已并入卡片快照（服务端），不再单独成泡
        received_at: Date.now(), // 服务端 expires_in 相对「本次响应」计算，收到即视为起点
      }
      // 卡片本体消息的内容是工具描述（「发布一条动态（需用户确认后生效）」），
      // 与卡片 describe 信息重复，回显时不再单独成泡
      nextBubbles.push({ role: m.role as Bubble['role'], content: '', cardID: m.card_id, media: m.media ?? [] })
    } else {
      nextBubbles.push({ role: m.role as Bubble['role'], content: m.content, media: m.media ?? [] })
    }
  }
  bubbles.value = nextBubbles
  cards.value = nextCards
  scrollBottom()
}

async function loadHistory() {
  const uid = auth.user?.id ?? 0
  // uid=0（用户未就绪的防御分支）跳过缓存，避免跨账号共享 history:0 键
  if (uid > 0) {
    const cached = (await loadCachedHistory(uid)) as HistoryMsg[] | null
    if (cached?.length) applyHistory(cached)
  }
  try {
    const res = await api<{ items: HistoryMsg[] }>('/api/v1/chat/history')
    applyHistory(res.items)
    if (uid > 0) void saveCachedHistory(uid, res.items)
  } catch {
    // 拉取失败：有缓存则继续展示（提示离线），无缓存维持原状静默
    if (uid > 0) {
      const cached = (await loadCachedHistory(uid)) as HistoryMsg[] | null
      if (cached?.length) {
        applyHistory(cached)
        showToast('网络不可用，已展示本地缓存')
      }
    }
  }
}

// 4.1②：chips 点击填入输入框（§5.1「点击填入输入框」，不直接发送）
function fillChip(c: string) {
  input.value = c + '：'
  inputBarRef.value?.focus()
}

function mediaCount(p: Record<string, unknown>): number {
  const refs = Array.isArray(p.media_refs) ? p.media_refs.length : 0
  const ids = Array.isArray(p.media_ids) ? p.media_ids.length : Array.isArray(p.photo_ids) ? p.photo_ids.length : 0
  return refs || ids
}

// 4.2：describe() 覆盖可确认/可展示的常见工具，兜底不再裸 JSON.stringify
function describe(card: CardItem): string {
  const p = card.params ?? {}
  switch (card.tool) {
    case 'create_post': {
      const mlen = mediaCount(p)
      return `发布动态：${String(p.content ?? '')}${p.mood ? `（${p.mood}）` : ''}${p.location ? ` · 地点：${p.location}` : ''}${mlen ? ` · ${mlen} 张媒体` : ''}`
    }
    case 'create_anniversary':
      return `创建纪念日：${String(p.name ?? '')}${p.date ? `（${p.date}）` : ''}${p.type === 'lunar' ? '农历' : '阳历'}`
    case 'toggle_like':
      return `点赞/取消点赞动态 #${p.post_id}`
    case 'toggle_favorite':
      return `收藏/取消收藏动态 #${p.post_id}`
    case 'search_posts': {
      const parts = [String(p.query ?? '')]
      if (p.date_range) parts.push(`时间：${JSON.stringify(p.date_range)}`)
      if (p.media_type) parts.push(`媒体：${p.media_type}`)
      return `搜索动态：${parts.filter(Boolean).join(' · ')}`
    }
    case 'get_space_summary':
      return '获取空间概览'
    case 'get_anniversaries':
      return '获取纪念日列表'
    case 'create_map_point':
      return `记录地点：${String(p.name ?? '')}（${p.latitude ?? ''},${p.longitude ?? ''}）`
    case 'create_album':
      return `新建相册：${String(p.name ?? '')}`
    case 'add_photos_to_album': {
      const cnt = mediaCount(p)
      return `向相册加入 ${cnt} 张照片${p.album_name ? `（${String(p.album_name)}）` : ''}`
    }
    case 'configure_reminder':
      return `设置纪念日提醒：提前 ${String(p.days_before ?? '')} 天`
    default: {
      const human = Object.entries(p)
        .filter(([_k, v]) => v !== '' && v !== null && v !== undefined)
        .map(([k, v]) => `${k}: ${typeof v === 'object' ? JSON.stringify(v) : String(v)}`)
        .join(' · ')
      return `${card.tool}${human ? `：${human}` : ''}`
    }
  }
}

function stopStream() {
  if (!streaming.value) return // 卡片待确认/确认执行中不可中止
  abortCtrl?.abort()
  void withdrawOrphanUserMessage()
}

// 停止即撤回（2026-09-21 定，REQ §5.1）：服务端删掉刚落库且尚无回复的用户消息，
// 撤回成功才把本轮气泡连同其后的流式残片一并移除；失败/已有回复则保留原状。
// 按气泡引用定位（而非文本匹配）：停止可发生在气泡 push 之前的任意时刻，重复文本/纯媒体空文本会误删旧气泡。
let sentBubble: Bubble | null = null
async function withdrawOrphanUserMessage() {
  try {
    const res = await api<{ withdrawn: boolean }>('/api/v1/chat/withdraw-last', { method: 'POST' })
    if (!res.withdrawn || !sentBubble) return
    const i = bubbles.value.indexOf(sentBubble)
    if (i >= 0) bubbles.value.splice(i) // 本轮用户气泡及其后的流式残片一起撤掉
  } catch {
    /* 撤回失败保留原状 */
  } finally {
    sentBubble = null
  }
}

// ---------- D4：确认卡片 15 分钟有效期（客户端侧） ----------
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
  for (const c of Object.values(cards.value)) {
    if (c.status === 'pending' && isCardExpired(c)) c.status = 'expired'
  }
})

// ---------- D2：搜索结果卡片（内联展开全文/大图，不跳转） ----------
const resultDetail = ref<PostDetailItem | null>(null)
const resultLoadingId = ref<number | null>(null)
async function toggleResult(it: SearchItem) {
  if (resultDetail.value?.id === it.id) {
    resultDetail.value = null // 再次点击收起
    return
  }
  resultLoadingId.value = it.id
  try {
    const p = await api<PostDetailItem>(`/api/v1/posts/${it.id}`)
    resultDetail.value = { id: p.id, content: p.content || '', media: p.media ?? [] }
  } catch (e) {
    showToast(errText(e, '打开失败'))
  } finally {
    resultLoadingId.value = null
  }
}

// 同一轮流式回复的文本累加目标气泡（多段 SSE text 合并在一个气泡，避免内容被拆散显示）
let streamBubble: Bubble | null = null

async function send(text?: string) {
  if (aiUnavailableReason.value) return
  const content = (text ?? input.value).trim()
  const media = pickedMedia.value.map((m) => ({ name: m.name, type: m.type, size: m.size, data_url: m.data_url }))
  if ((!content && media.length === 0) || busy.value) return
  // 本地文件暂存：确认执行时才上传换 token（上传发生在工具调用时，不提前产生服务端媒体）
  const sentFiles = pickedMedia.value.slice()
  input.value = ''
  pickedMedia.value = []
  busy.value = true
  streaming.value = true
  error.value = ''
  streamBubble = null

  // 4.3：支持中止请求
  abortCtrl = new AbortController()
  try {
    const res = await fetch(getBase() + '/api/v1/chat/message', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${getToken() ?? ''}`,
      },
      body: JSON.stringify({ content, media }),
      signal: abortCtrl.signal,
    })
    if (!res.ok || !res.body) {
      const msg = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
      throw new Error((msg as { error?: string }).error ?? '发送失败')
    }

    sentBubble = { role: 'user', content, media }
    bubbles.value.push(sentBubble)
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
        if (ev === 'text') {
          // 服务端把一条回复分多段 text 事件下发（如每 2-3 字一片），
          // 这里合并进同一气泡，避免「内容被拆分显示」。
          // 服务端以「放大镜」emoji（U+1F50E）前缀标记工具调用；仅在首片剥离，改为 Lucide Search 图标呈现
          const raw = typeof data.content === 'string' ? data.content : ''
          if (!raw) continue // 非 string / 空内容：跳过，避免 charCodeAt 抛错整轮失败
          const isToolStart = raw.charCodeAt(0) === 0xd83d && raw.charCodeAt(1) === 0xdd0e
          const chunk = isToolStart ? raw.replace(/^\uD83D\uDD0E\s*/, '') : raw
          if (!streamBubble) {
            streamBubble = { role: isToolStart ? 'tool' : 'assistant', content: chunk }
            bubbles.value.push(streamBubble)
          } else if (chunk) {
            streamBubble.content += chunk
          }
        } else if (ev === 'result') {
          // D2：搜索结果卡片先行落一个气泡，随后的 text 分片并入同一条（文字在上、卡片在下）
          streamBubble = { role: 'assistant', content: '', result: { tool: data.tool, query: data.query, items: data.items ?? [] } }
          bubbles.value.push(streamBubble)
        } else if (ev === 'card') {
          streamBubble = null // 卡片前的一段提示作为独立气泡已落定，后续不再并入
          cards.value[data.action_id] = { ...data, status: 'pending', received_at: Date.now() }
          if (sentFiles.length) pendingCardFiles.set(data.action_id, sentFiles)
          bubbles.value.push({ role: 'assistant', content: '' })
          bubbles.value[bubbles.value.length - 1].cardID = data.action_id
        } else if (ev === 'done') {
          streamEnded = true
        } else if (ev === 'error') {
          error.value = data.message
        }
        scrollBottom()
      }
    }
    streamBubble = null
  } catch (e) {
    if (e instanceof Error && e.name === 'AbortError') {
      // 用户主动停止：不算错误
    } else {
      error.value = errText(e, '发送失败')
    }
  } finally {
    streaming.value = false
    abortCtrl = null
    // 确认后才结束：若仍有待确认卡片，右下角保持「进行中」，直到确认/取消拿到后端返回
    busy.value = Object.values(cards.value).some((c) => c.status === 'pending')
  }
}
// 气泡 → 确认卡片（模板去重：一次访问卡 ID 与卡片对象）
function cardOf(b: Bubble): CardItem | undefined {
  return b.cardID == null ? undefined : cards.value[b.cardID]
}

async function confirmCard(id: number, confirmed: boolean) {
  const card = cards.value[id]
  if (!card || card.status !== 'pending') return
  busy.value = true // 确认请求期间右下角保持「进行中」
  streaming.value = false
  try {
    const files = pendingCardFiles.get(id) ?? []
    // 工具确认执行时才上传（本地文件 → media_token），与「传统模式·发动态」一致，取消/过期零媒体资源
    const mediaTokens = confirmed && files.length ? await uploadPicked(files) : []
    const res = await api<{ status: string; result?: string; error?: string }>('/api/v1/chat/confirm', {
      method: 'POST',
      body: JSON.stringify({ action_id: id, confirmed, media_tokens: mediaTokens }),
    })
    card.status = confirmed ? (res.status === 'executed' ? 'executed' : 'failed') : 'cancelled'
    card.result = res.result ?? res.error
    // 结果只呈现在卡片内（与刷新后回显一致），不再另推「已执行：…」气泡
    scrollBottom()
  } catch (e) {
    // 409=已处理 / 410=已过期（服务端判定，见 D4）；其余才是真失败，别一律标成「已过期」
    const status = e instanceof ApiError ? e.status : 0
    card.status = status === 409 || status === 410 ? 'expired' : 'failed'
    card.result = errText(e, '确认失败')
  } finally {
    pendingCardFiles.delete(id)
    // 拿到后端处理结果即「结束」
    busy.value = false
  }
}

// AI 可用性（后台未配置模型时禁用输入/发送并提示原因；与设置页模式置灰同源 /api/v1/chat/status）
// reason 的加工统一走 utils/aiStatus，保证与设置页文案一致
const aiUnavailableReason = ref('')
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

onMounted(() => {
  void loadHistory()
  void loadAIStatus()
  expiryTimer = window.setInterval(() => (now.value = Date.now()), 30000)
})

let expiryTimer: number | undefined
onBeforeUnmount(() => {
  if (expiryTimer) window.clearInterval(expiryTimer)
  if (aiPollTimer) window.clearInterval(aiPollTimer)
})
const router = useRouter()
</script>

<template>
  <PageLayout class="chat-page" title="我们的 AI 助手" back standalone :scroll="false" @back="router.push('/home')">
    <template #nav-right>
      <NavActions :show-bell="false" />
    </template>

    <MessageList
      ref="streamRef"
      :bubbles="bubbles"
      :error="error"
      :result-loading-id="resultLoadingId"
      :result-detail="resultDetail"
      :unavailable-reason="aiUnavailableReason"
      @toggle-result="toggleResult"
    >
      <template #card="{ bubble }">
        <CardActions
          v-if="cardOf(bubble)"
          :card="cardOf(bubble)!"
          :bubble="bubble"
          :describe="describe"
          :remaining-min="remainingMin"
          :is-card-expired="isCardExpired"
          @confirm="confirmCard"
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
      :busy="busy"
      :streaming="streaming"
      :max-media="MAX_MEDIA"
      :unavailable-reason="aiUnavailableReason"
      @update:input="input = $event"
      @pick="pickMedia"
      @remove="pickedMedia.splice($event, 1)"
      @send="send()"
      @stop="stopStream()"
    />
    </template>
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
/* D1 AI 专属头像标识（REQUIREMENTS:238）：助手气泡左侧固定标识，用户气泡不加 */
/* D2 搜索结果卡片（内联，无跳转） */

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
