<script setup lang="ts">
import { nextTick, onMounted, ref } from 'vue'
import { Send, Search, CheckCircle2, XCircle, Timer, Check, Sparkles, Square, ImagePlus, X, Music } from 'lucide-vue-next'
import { api, getBase, getToken } from '../api/client'
import { useAuth } from '../stores/auth'
import { loadCachedHistory, saveCachedHistory } from '../utils/chatCache'
import NavActions from '../components/NavActions.vue'

const MAX_MEDIA = 9 // 与 PublishView 对齐

interface ChatMedia {
  name: string
  type: string
  size: number
  data_url: string
}

// 本地待发送/待上：文件先留在设备（确认执行时才上传换 token，不提前产生服务端媒体资源）
interface PickedMedia {
  name: string
  type: string
  size: number
  data_url: string
  file: File
}

interface Bubble {
  role: 'user' | 'assistant' | 'tool'
  content: string
  cardID?: number
  media?: ChatMedia[]
}

interface CardItem {
  action_id: number
  tool: string
  params: Record<string, unknown>
  status: 'pending' | 'executed' | 'cancelled' | 'expired' | 'failed'
  result?: string
  expires_in?: number
}

interface HistoryMsg {
  id: number
  role: string
  content: string
  card_id?: number
  created_at?: string
  media?: ChatMedia[]
  card?: { action_id: number; tool: string; params: Record<string, unknown>; status: CardItem['status']; expires_in: number }
}

const auth = useAuth()
const bubbles = ref<Bubble[]>([])
const cards = ref<Record<number, CardItem>>({})
const input = ref('')
const inputRef = ref<HTMLInputElement | null>(null)
// busy = 本轮「进行中」总开关（流式 + 卡片待确认 + 确认执行中）；streaming = 仅 SSE 流式阶段（可中止）
const busy = ref(false)
const streaming = ref(false)
const pickedMedia = ref<PickedMedia[]>([])
// 确认时才上传：pending 卡片 → 其携带的本地文件（action_id → 文件列表）
const pendingCardFiles = new Map<number, PickedMedia[]>()
const mediaInput = ref<HTMLInputElement | null>(null)
const toast = ref('')
let toastTimer: number | undefined
const container = ref<HTMLElement>()
const error = ref('')
const chips = ['帮我发动态', '整理最近的照片', '上次去海边是什么时候']
let abortCtrl: AbortController | null = null

function showToast(msg: string) {
  toast.value = msg
  if (toastTimer) window.clearTimeout(toastTimer)
  toastTimer = window.setTimeout(() => (toast.value = ''), 1800)
}

function scrollBottom() {
  nextTick(() => {
    if (container.value) container.value.scrollTop = container.value.scrollHeight
  })
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
    if (m.card && m.card_id) {
      nextCards[m.card_id] = {
        action_id: m.card.action_id,
        tool: m.card.tool,
        params: m.card.params ?? {},
        status: m.card.status,
        expires_in: m.card.expires_in,
      }
      nextBubbles.push({ role: m.role as Bubble['role'], content: m.content, cardID: m.card_id, media: m.media ?? [] })
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
  inputRef.value?.focus()
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
}

// 同一轮流式回复的文本累加目标气泡（多段 SSE text 合并在一个气泡，避免内容被拆散显示）
let streamBubble: Bubble | null = null

async function send(text?: string) {
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

    bubbles.value.push({ role: 'user', content, media })
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
        const data = JSON.parse(dataLine.slice(6))
        if (ev === 'text') {
          // 服务端把一条回复分多段 text 事件下发（如每 2-3 字一片），
          // 这里合并进同一气泡，避免「内容被拆分显示」。
          // 服务端以「放大镜」emoji（U+1F50E）前缀标记工具调用；仅在首片剥离，改为 Lucide Search 图标呈现
          const isToolStart = data.content.charCodeAt(0) === 0xd83d && data.content.charCodeAt(1) === 0xdd0e
          const chunk = isToolStart ? data.content.replace(/^\uD83D\uDD0E\s*/, '') : data.content
          if (!streamBubble) {
            streamBubble = { role: isToolStart ? 'tool' : 'assistant', content: chunk }
            bubbles.value.push(streamBubble)
          } else if (chunk) {
            streamBubble.content += chunk
          }
        } else if (ev === 'card') {
          streamBubble = null // 卡片前的一段提示作为独立气泡已落定，后续不再并入
          cards.value[data.action_id] = { ...data, status: 'pending' }
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
      error.value = e instanceof Error ? e.message : '发送失败'
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
    if (confirmed && card.status === 'executed') {
      bubbles.value.push({ role: 'tool', content: `已执行：${card.result}` })
    }
    scrollBottom()
  } catch (e) {
    card.status = 'expired'
    card.result = e instanceof Error ? e.message : '确认失败'
  } finally {
    pendingCardFiles.delete(id)
    // 拿到后端处理结果即「结束」
    busy.value = false
  }
}

onMounted(loadHistory)
</script>

<template>
  <section class="chat page-col">
    <header class="navbar glass">
      <span class="title">我们的 AI 助手</span>
      <NavActions :show-bell="false" />
    </header>

    <div ref="container" class="stream">
      <p v-if="bubbles.length === 0" class="empty"><Sparkles :size="30" stroke-width="1.2" class="spark" /><br />和我聊聊你们的日常吧</p>
      <template v-for="(b, i) in bubbles" :key="i">
        <div v-if="b.content" class="bubble" :class="b.role">
          <span v-if="b.role === 'tool'" class="tool-icon"><Search :size="13" stroke-width="1.8" /></span>{{ b.content }}
          <div v-if="b.media?.length" class="bubble-media">
            <template v-for="m in b.media" :key="m.data_url">
              <img v-if="m.type.startsWith('image/')" :src="m.data_url" alt="附件" loading="lazy" />
              <video v-else-if="m.type.startsWith('video/')" :src="m.data_url" controls />
              <audio v-else :src="m.data_url" controls class="bubble-audio" />
            </template>
          </div>
        </div>
        <template v-if="cardOf(b)">
          <div class="card" :class="cardOf(b)!.status">
            <p class="desc">{{ describe(cardOf(b)!) }}</p>
            <template v-if="cardOf(b)?.status === 'pending'">
              <div class="btns">
                <button class="ok" @click="confirmCard(b.cardID!, true)">
                  <Check :size="15" stroke-width="2" />确认执行
                </button>
                <button class="no" @click="confirmCard(b.cardID!, false)">取消</button>
              </div>
              <p class="hint">15 分钟内有效 · 单次有效</p>
            </template>
            <p v-else class="result">
              <template v-if="cardOf(b)?.status === 'executed'">
                <span class="res-ok"><CheckCircle2 :size="14" stroke-width="1.8" />已执行</span>
              </template>
              <span v-else-if="cardOf(b)?.status === 'cancelled'">已取消</span>
              <span v-else-if="cardOf(b)?.status === 'expired'" class="res-warn">
                <Timer :size="14" stroke-width="1.8" />已过期
              </span>
              <span v-else-if="cardOf(b)?.status === 'failed'" class="res-err">
                <XCircle :size="14" stroke-width="1.8" />失败
              </span>
              {{ cardOf(b)?.result }}
            </p>
          </div>
        </template>
      </template>
      <p v-if="error" class="error">{{ error }}</p>
    </div>

    <div v-if="bubbles.length === 0" class="chips">
      <button v-for="c in chips" :key="c" @click="fillChip(c)">{{ c }}</button>
    </div>

    <footer class="input-bar">
      <div v-if="pickedMedia.length" class="media-strip">
        <div v-for="(m, i) in pickedMedia" :key="i" class="media-pick">
          <img v-if="m.type.startsWith('image/')" :src="m.data_url" alt="" />
          <video v-else-if="m.type.startsWith('video/')" :src="m.data_url" muted />
          <div v-else class="media-audio"><Music :size="18" stroke-width="1.8" /><span class="audio-name">{{ m.name }}</span></div>
          <button class="rm" aria-label="移除附件" @click="pickedMedia.splice(i, 1)"><X :size="12" stroke-width="2" /></button>
        </div>
      </div>
      <div class="compose">
        <button class="media-btn" aria-label="选择图片/视频/音频" :disabled="busy || pickedMedia.length >= MAX_MEDIA" @click="mediaInput?.click()">
          <ImagePlus :size="20" stroke-width="1.8" />
        </button>
        <input ref="mediaInput" class="file-input" type="file" accept="image/*,video/*,audio/*" multiple @change="pickMedia" />
        <input ref="inputRef" v-model="input" placeholder="说点什么…" :disabled="busy" @keyup.enter="send()" />
        <button v-if="streaming" class="send stop" aria-label="停止生成" @click="stopStream()">
          <Square :size="14" stroke-width="2" fill="currentColor" />
        </button>
        <button v-else-if="busy" class="send running" aria-label="进行中" disabled>
          <span class="dot" />进行中
        </button>
        <button v-else class="send" :disabled="!input.trim() && pickedMedia.length === 0" @click="send()">
          <Send :size="16" stroke-width="1.8" />
        </button>
      </div>
    </footer>
    <div v-if="toast" class="toast" @click="toast = ''">{{ toast }}</div>
  </section>
</template>

<style scoped>
.chat {
  height: 100%;
  display: flex;
  flex-direction: column;
  background: var(--ai-bg, var(--bg));
}
.navbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: calc(10px + env(safe-area-inset-top)) 12px 10px;
  flex-shrink: 0;
  position: relative;
  z-index: 10;
}
.title {
  position: absolute;
  left: 50%;
  transform: translateX(-50%);
  font-size: 17px;
  font-weight: 600;
  white-space: nowrap;
}
.nav-btn {
  border: none;
  background: none;
  color: var(--ios-blue);
  cursor: pointer;
  padding: 4px;
  display: flex;
  align-items: center;
}
.stream {
  flex: 1;
  overflow-y: auto;
  padding: 0.75rem 1rem;
  background: var(--ai-bg, var(--bg));
  min-height: 50vh;
}
.empty {
  text-align: center;
  color: var(--muted-ink);
  margin-top: 3rem;
}
.spark {
  color: var(--primary-deep);
  opacity: 0.6;
  margin-bottom: 0.6rem;
}
.bubble {
  max-width: 80%;
  margin-bottom: 0.6rem;
  padding: 0.6rem 0.9rem;
  border-radius: 18px;
  font-size: 0.9375rem;
  white-space: pre-wrap;
  word-break: break-word;
}
.bubble-media {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 0.5rem;
}
.bubble-media img {
  width: 84px;
  height: 84px;
  object-fit: cover;
  border-radius: 8px;
  border: 1px solid rgba(255, 255, 255, 0.25);
}
.bubble.user {
  margin-left: auto;
  background: var(--primary);
  color: #fff;
  border-bottom-right-radius: 6px;
}
.bubble.assistant {
  background: var(--surface);
  color: var(--ink);
  border: 1px solid var(--rule);
  border-bottom-left-radius: 6px;
}
.bubble.tool {
  background: transparent;
  color: var(--muted-ink);
  font-size: 0.8125rem;
  display: flex;
  align-items: center;
  gap: 0.35rem;
}
.tool-icon {
  display: inline-flex;
  color: var(--primary-deep);
  flex-shrink: 0;
}
.card {
  background: var(--surface);
  border: 1px solid var(--rule);
  border-radius: 8px;
  padding: 0.75rem;
  margin-bottom: 0.8rem;
  max-width: 90%;
}
.card.executed {
  border-color: var(--primary);
}
.card.cancelled,
.card.expired {
  opacity: 0.6;
}
.desc {
  margin: 0 0 0.5rem;
  font-size: 0.875rem;
}
.btns {
  display: flex;
  gap: 0.5rem;
}
.ok {
  flex: 1;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 0.3rem;
  background: var(--primary);
  color: #fff;
  border: none;
  border-radius: 12px;
  padding: 0.45rem;
  font-weight: 600;
}
.no {
  flex: 1;
  background: none;
  border: 1px solid var(--rule);
  border-radius: 999px;
  padding: 0.45rem;
  color: var(--muted-ink);
}
.hint,
.result {
  margin: 0.4rem 0 0;
  font-size: 0.75rem;
  color: var(--muted-ink);
}
.result {
  display: flex;
  align-items: center;
  gap: 0.35rem;
  flex-wrap: wrap;
}
.res-ok {
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
  color: var(--primary-deep);
}
.res-warn {
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
  color: var(--muted-ink);
}
.res-err {
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
  color: var(--danger);
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
.input-bar {
  background: var(--surface);
  border-top: 1px solid var(--rule);
  padding-bottom: env(safe-area-inset-bottom);
}
.media-strip {
  display: flex;
  gap: 0.5rem;
  overflow-x: auto;
  padding: 0.6rem 0.8rem 0;
}
.media-pick {
  position: relative;
  flex: 0 0 56px;
  width: 56px;
  height: 56px;
  border-radius: 10px;
  overflow: hidden;
  border: 1px solid var(--rule);
}
.media-pick img,
.media-pick video {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.media-pick .rm {
  position: absolute;
  top: 2px;
  right: 2px;
  width: 18px;
  height: 18px;
  border-radius: 50%;
  border: none;
  background: rgba(0, 0, 0, 0.55);
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 0;
}
.media-pick .media-audio {
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--bg);
  color: var(--muted-ink);
}
.audio-name {
  font-size: 10px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 60px;
  margin-left: 4px;
}
.bubble-audio {
  display: block;
  max-width: 220px;
  margin-top: 4px;
}
.compose {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.6rem 0.8rem;
}
.media-btn {
  border: none;
  background: none;
  color: var(--muted-ink);
  padding: 4px;
  display: flex;
  flex: 0 0 auto;
}
.media-btn:disabled {
  opacity: 0.4;
}
.file-input {
  display: none;
}
.compose > input[type='text'],
.input-bar input:not(.file-input) {
  flex: 1;
  min-width: 0;
  border: 1px solid var(--rule);
  border-radius: 999px;
  padding: 0.55rem 1rem;
  font-size: 0.9375rem;
  background: var(--ai-bg, var(--bg));
  color: var(--ink);
}
.send {
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--primary);
  color: #fff;
  border: none;
  border-radius: 999px;
  padding: 0 1.2rem;
}
.send.stop {
  background: var(--danger);
  padding: 0 0.9rem;
}
.send.running {
  background: color-mix(in srgb, var(--primary) 55%, transparent);
  padding: 0 0.9rem;
  font-size: 0.75rem;
  gap: 0.3rem;
  cursor: default;
}
.send.running .dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
  animation: chat-dot 1.2s infinite;
}
@keyframes chat-dot {
  0%,
  100% {
    opacity: 0.3;
  }
  50% {
    opacity: 1;
  }
}
.send:disabled {
  opacity: 0.45;
}
.toast {
  position: fixed;
  left: 50%;
  bottom: 5rem;
  transform: translateX(-50%);
  background: rgba(30, 30, 30, 0.92);
  color: #fff;
  font-size: 0.8125rem;
  padding: 0.5rem 1rem;
  border-radius: 999px;
  z-index: 60;
  max-width: 80%;
}

.error {
  color: var(--danger);
  font-size: 0.8125rem;
  text-align: center;
}
.ghost {
  border: none;
  background: none;
  color: var(--muted-ink);
  padding: 0.25rem;
}
</style>
