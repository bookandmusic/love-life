<script setup lang="ts">
import { computed, nextTick, onActivated, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Heart, Music, Play, Star, MessageCircle, MapPin, PenLine } from 'lucide-vue-next'
import { api, errText, fmtDateTime } from '../api/client'
import AppToast from '../components/AppToast.vue'
import ConfirmSheet from '../components/ConfirmSheet.vue'
import MediaViewer from '../components/MediaViewer.vue'
import { mediaSrc, type Post } from '../types'
import PageLayout from '../components/PageLayout.vue'
import SkeletonState from '../components/SkeletonState.vue'
import EmptyState from '../components/EmptyState.vue'
import ErrorState from '../components/ErrorState.vue'
import { useSpace } from '../stores/space'
import { useAuth } from '../stores/auth'
import { usePostInteractions } from '../composables/postInteractions'
import { useLongPress } from '../composables/useLongPress'
import { KEYS, onceHint } from '../utils/storage'
import { RECYCLE_CONFIRM, RECYCLE_TITLE, RECYCLE_TOAST, peerNotice, recycleMsg } from '../utils/deleteCopy'

defineOptions({ name: 'PostsView' })

const route = useRoute()
const router = useRouter()

// 那年今日：?memories=1 只列同月同日回忆（服务端过滤后再分页）
const memoriesOnly = computed(() => route.query.memories === '1')
function listPath(p: number): string {
  return `/api/v1/posts?page=${p}&page_size=${PAGE_SIZE}${memoriesOnly.value ? '&memories=1' : ''}`
}

interface CommentItem {
  id: number
  author_id: number
  author: string
  content: string
  created_at: string
}

type PostItem = Post

const spaceStore = useSpace()

const posts = ref<PostItem[]>([])
const error = ref('')
const loading = ref(false)
const toast = ref('')
const loadingMore = ref(false)
const total = ref(0)
const page = ref(1)
const expanded = ref<number | null>(null)
const comments = ref<Record<number, CommentItem[]>>({})
const commentText = ref('')

// 全屏媒体查看器：列表页直接看大图（原先只能跳详情页）
const viewer = ref<{ index: number; post: PostItem } | null>(null)
function openViewer(p: PostItem, index: number) {
  viewer.value = { index, post: p }
}
// ?focus=<id>：通知 / 首页回忆卡 / 相册跳过来时定位并短暂高亮该条（与地图 同做法）
const hlId = ref<number | null>(null)
const auth = useAuth()
// 长按 = 低频操作入口（移入回收站）：卡片与评论行共用一套手势
const { pressing, down: pressDown, move: pressMove, up: pressUp, click: pressClick } = useLongPress()
const deletingPost = ref<PostItem | null>(null)
const showDeletePost = ref(false)
const deletingComment = ref<CommentItem | null>(null)
const showDeleteComment = ref(false)

const noMore = computed(() => posts.value.length > 0 && posts.value.length >= total.value)
const PAGE_SIZE = 20
// ?focus=<id> 定位的翻页上限：目标不存在/已删时避免无限翻页（最多 10 页 / 500 条）
const FOCUS_MAX_PAGES = 10
const FOCUS_MAX_POSTS = 500

// 按 author_id 映射真实 role（girl/boy），替代昵称长度猜色
async function load() {
  // 静默刷新：仅冷启动（无数据）时亮骨架屏；已有数据时后台重取、失败不打扰旧内容
  const cold = posts.value.length === 0
  if (cold) {
    loading.value = true
    error.value = ''
  }
  try {
    const res = await api<{ items: PostItem[]; total: number }>(listPath(1))
    posts.value = res.items
    total.value = res.total
    page.value = 1
    await spaceStore.load()
  } catch (e) {
    if (cold) error.value = errText(e, '加载失败')
  } finally {
    loading.value = false
  }
}

// 滚动加载：接近底部时自动拉取下一页（服务端 /posts 支持 page/page_size）
async function loadMore() {
  if (loading.value || loadingMore.value || noMore.value) return
  loadingMore.value = true
  try {
    const next = page.value + 1
    const res = await api<{ items: PostItem[] }>(listPath(next))
    posts.value.push(...res.items)
    page.value = next
  } catch (e) {
    toast.value = errText(e, '加载失败')
  } finally {
    loadingMore.value = false
  }
}

function onScroll(e: Event) {
  const el = e.target as HTMLElement
  if (el.scrollTop + el.clientHeight >= el.scrollHeight - 120) loadMore()
}

// 点赞/收藏/评论：与详情页共用实现（composables/postInteractions.ts）
const { toggleLike, toggleFav, submitComment: postComment } = usePostInteractions({
  onError: (m) => (toast.value = m),
})

// 点赞/收藏的微粒子扩散（PRODUCT「动效规范」：微粒子扩散，非夸张）：
// 点击瞬间在图标中心撒 6 颗小点向外扩散淡出，图标同步轻微回弹；粒子的角度与距离带随机，
// 避免机械重复。系统开启「减弱动效」时整体不播放（可访问性优先）。
type BurstParticle = { dx: number; dy: number; d: number }
const bursts = ref<Record<string, BurstParticle[]>>({})

function reducedMotion(): boolean {
  return typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

function spawnBurst(key: string) {
  if (reducedMotion()) return
  bursts.value = {
    ...bursts.value,
    [key]: Array.from({ length: 6 }, (_, i) => {
      const angle = (Math.PI * 2 * i) / 6 + (Math.random() - 0.5) * 0.5
      const dist = 12 + Math.random() * 6
      return { dx: Math.cos(angle) * dist, dy: Math.sin(angle) * dist, d: Math.round(Math.random() * 60) }
    }),
  }
  window.setTimeout(() => {
    const next = { ...bursts.value }
    delete next[key]
    bursts.value = next
  }, 650)
}

/** 点赞：仅「点亮」时撒粒子（取消点赞不撒），请求前就播以保证手感跟手 */
function onLike(p: PostItem) {
  if (!p.liked) spawnBurst(`like:${p.id}`)
  void toggleLike(p)
}

/** 收藏：同上，仅「加上收藏」时撒粒子 */
function onFav(p: PostItem) {
  if (!p.favorited) spawnBurst(`fav:${p.id}`)
  void toggleFav(p)
}

async function toggleComments(p: PostItem) {
  if (expanded.value === p.id) {
    expanded.value = null
    return
  }
  expanded.value = p.id
  commentText.value = ''
  await loadComments(p.id)
}

async function loadComments(postID: number) {
  try {
    const res = await api<{ items: CommentItem[] }>(`/api/v1/posts/${postID}/comments`)
    comments.value[postID] = res.items
  } catch (e) {
    comments.value[postID] = []
    toast.value = errText(e, '评论加载失败')
  }
}

async function submitComment(p: PostItem) {
  if (!(await postComment(p, commentText.value))) return
  commentText.value = ''
  await loadComments(p.id)
}

// 移入回收站：文案统一走 utils/deleteCopy（标题/按钮「移入回收站」+ 后果 + 恢复路径）
function askDeletePost(p: PostItem) {
  deletingPost.value = p
  showDeletePost.value = true
}
function askDeleteComment(c: CommentItem) {
  deletingComment.value = c
  showDeleteComment.value = true
}
function postDeleteMsg(p: PostItem) {
  return recycleMsg('这条动态', '动态里的照片与评论会一并移入回收站。' + peerNotice(auth.user?.id, p.author_id))
}
function commentDeleteMsg(c: CommentItem) {
  return recycleMsg(`${c.author}：${c.content.slice(0, 20)}…`, peerNotice(auth.user?.id, c.author_id))
}

async function confirmDeletePost() {
  if (!deletingPost.value) return
  const id = deletingPost.value.id
  try {
    await api(`/api/v1/posts/${id}`, { method: 'DELETE' })
    // 原地移除，不整表重载（避免列表闪烁与滚动位置丢失）
    posts.value = posts.value.filter((p) => p.id !== id)
    total.value = Math.max(0, total.value - 1)
    toast.value = RECYCLE_TOAST
  } catch (e) {
    toast.value = errText(e, '删除失败')
  } finally {
    deletingPost.value = null
  }
}

async function confirmDeleteComment() {
  if (!deletingComment.value) return
  const c = deletingComment.value
  // 评论只可能来自当前展开的那条动态
  const pid = expanded.value
  try {
    await api(`/api/v1/comments/${c.id}`, { method: 'DELETE' })
    if (pid !== null) {
      comments.value[pid] = (comments.value[pid] ?? []).filter((x) => x.id !== c.id)
      const owner = posts.value.find((x) => x.id === pid)
      if (owner) owner.comment_count = Math.max(0, owner.comment_count - 1)
    }
    toast.value = RECYCLE_TOAST
  } catch (e) {
    toast.value = errText(e, '删除评论失败')
  } finally {
    deletingComment.value = null
  }
}

async function applyFocus() {
  const id = Number(route.query.focus)
  if (!id) return
  if (!posts.value.length) await load()
  await nextTick() // 先让首页 DOM 落地，避免第一页就能命中时白翻一页
  // 目标可能在第 2 页之后：循环翻页直到出现。带上限（10 页 / 500 条）——
  // 目标是已删动态或本就不存在时不能无限翻页（客户端设计「手机 APP 职责」跳转定位契约）。
  while (
    !document.getElementById(`post-${id}`) &&
    !noMore.value &&
    page.value < FOCUS_MAX_PAGES &&
    posts.value.length < FOCUS_MAX_POSTS
  ) {
    const before = posts.value.length
    await loadMore()
    if (posts.value.length === before) break // 拉不动了（请求失败或无更多）
  }
  await nextTick()
  const el = document.getElementById(`post-${id}`)
  if (!el) {
    // 静默失败会让用户以为「跳转没反应」：明确说一句
    toast.value = '未找到该条，可能已删除'
    return
  }
  el.scrollIntoView({ block: 'center' })
  hlId.value = id
  window.setTimeout(() => {
    hlId.value = null
  }, 2400)
}

onActivated(async () => {
  if (posts.value.length === 0) {
    await load()
    await applyFocus()
    // 长按是隐形手势：首次进入提示一次（本地记住后不再打扰）
    if (!toast.value && onceHint(KEYS.HINT_LONGPRESS)) toast.value = '长按动态或评论可移入回收站'
  } else if (page.value === 1) {
    // 停在顶部：静默重取第一页（发布新动态即时可），不亮骨架屏、不扰动已翻页的用户
    void load()
  }
})
// 同一页面内切换过滤条件（首页「那年今日」入口）需重载，否则仍是上一份列表
watch(memoriesOnly, () => void load())
watch(() => route.query.focus, () => {
  // KeepAlive 下失活组件 watcher 仍响应全局路由：限定本页路径，避免他 tab 的 ?focus 泄漏
  if (route.path === '/posts') void applyFocus()
})
</script>

<template>
  <PageLayout :title="memoriesOnly ? '那年今日' : '动态'" actions @scroll="onScroll">
      <SkeletonState v-if="loading" variant="list" :rows="3" />
      <ErrorState v-else-if="error" :error="error" :retry="load" />
      <EmptyState v-else-if="posts.length === 0" :icon="PenLine" :title="memoriesOnly ? '往年今日待记录' : '动态圈还空着'" :sub="memoriesOnly ? '从今天开始记录，明年今日就能在这里重逢。' : '发一条动态，记录此刻的心情和日常。'" :action="memoriesOnly ? '去发动态' : '发动态'" @action="router.push('/publish')" />

      <article
        v-for="p in posts"
        :id="`post-${p.id}`"
        :key="p.id"
        class="post pressable"
        :class="{ hl: hlId === p.id, pressing: pressing === p.id }"
        @pointerdown="pressDown($event, p.id, () => askDeletePost(p))"
        @pointermove="pressMove"
        @pointerup="pressUp"
        @pointerleave="pressUp"
        @pointercancel="pressUp"
        @click="pressClick"
      >
        <div class="head">
          <div class="mini" :class="spaceStore.roleClass(p.author_id)">{{ p.author.slice(0, 1) }}</div>
          <div class="who">
            <b>{{ p.author }}</b>
            <span>{{ fmtDateTime(p.created_at) }}</span>
          </div>
          <span v-if="p.mood" class="mood">{{ p.mood }}</span>
        </div>

        <p class="p-text">{{ p.content }}</p>

        <div
          v-if="p.media.length"
          class="media-grid"
          :class="{ single: p.media.length === 1, dual: p.media.length === 2 }"
        >
          <template v-for="(m, mi) in p.media" :key="m.id">
            <!-- 视频只放缩略图 + 播放角标（与时间轴 TimelineGrid 同一做法）：播放交给全屏查看器，
                 卡片内不再内嵌 <video>（内嵌会把点击吞掉，进不了查看器也就没有「保存」入口） -->
            <div v-if="m.kind === 'video'" class="ph video-ph" @pointerdown.stop @click="openViewer(p, mi)">
              <img :src="mediaSrc(m, 'thumb')" loading="lazy" alt="" />
              <span class="video-mark"><Play :size="9" fill="currentColor" /></span>
            </div>
            <div v-else-if="m.kind === 'audio'" class="ph audio-ph" @click="openViewer(p, mi)">
              <Music :size="22" stroke-width="1.6" />
              <audio controls preload="none" :src="mediaSrc(m, 'raw')" @pointerdown.stop @click.stop />
            </div>
            <img v-else class="ph" :src="mediaSrc(m, 'thumb')" loading="lazy" alt="" @click="openViewer(p, mi)" />
          </template>
        </div>

        <div v-if="p.location_name" class="loc">
          <MapPin :size="13" stroke-width="2" />{{ p.location_name }}
        </div>

        <div class="actions" @pointerdown.stop>
          <button class="act" :class="{ on: p.liked, bounce: bursts[`like:${p.id}`] }" @click="onLike(p)">
            <Heart :size="18" :fill="p.liked ? 'currentColor' : 'none'" stroke-width="1.8" />
            <b class="num">{{ p.like_count || '' }}</b>
            <span v-if="bursts[`like:${p.id}`]" class="burst" aria-hidden="true">
              <i
                v-for="(pt, bi) in bursts[`like:${p.id}`]"
                :key="bi"
                :style="{ '--dx': pt.dx + 'px', '--dy': pt.dy + 'px', '--d': pt.d + 'ms' }"
              />
            </span>
          </button>
          <button class="act" :class="{ on: p.favorited, bounce: bursts[`fav:${p.id}`] }" @click="onFav(p)">
            <Star :size="18" :fill="p.favorited ? 'currentColor' : 'none'" stroke-width="1.8" />
            <b class="num">{{ p.favorite_count || '' }}</b>
            <span v-if="bursts[`fav:${p.id}`]" class="burst" aria-hidden="true">
              <i
                v-for="(pt, bi) in bursts[`fav:${p.id}`]"
                :key="bi"
                :style="{ '--dx': pt.dx + 'px', '--dy': pt.dy + 'px', '--d': pt.d + 'ms' }"
              />
            </span>
          </button>
          <button class="act" @click="toggleComments(p)">
            <MessageCircle :size="18" stroke-width="1.8" />
            <b class="num">{{ p.comment_count || '' }}</b>
          </button>
        </div>

        <div v-if="expanded === p.id" class="comments">
          <div
            v-for="c in comments[p.id] ?? []"
            :key="c.id"
            class="comment pressable"
            :class="{ pressing: pressing === c.id }"
            @pointerdown.stop="pressDown($event, c.id, () => askDeleteComment(c))"
            @pointermove="pressMove"
            @pointerup="pressUp"
            @pointerleave="pressUp"
            @pointercancel="pressUp"
            @click="pressClick"
          >
            <b>{{ c.author }}</b>
            <span>{{ c.content }}</span>
          </div>
          <p v-if="(comments[p.id] ?? []).length === 0" class="no-comment">来说第一句吧。</p>
          <div class="comment-input" @pointerdown.stop>
            <input v-model="commentText" placeholder="说点什么…" @keyup.enter="submitComment(p)" />
            <button @click="submitComment(p)">发送</button>
          </div>
        </div>
      </article>

      <p v-if="loadingMore" class="load-more">加载中…</p>
      <p v-else-if="noMore && posts.length >= PAGE_SIZE" class="load-more">没有更多了</p>
    <!-- 移入回收站：卡片 / 评论行长按触发 -->
    <ConfirmSheet
      v-if="deletingPost"
      v-model:show="showDeletePost"
      :title="RECYCLE_TITLE"
      :message="postDeleteMsg(deletingPost)"
      :confirm-text="RECYCLE_CONFIRM"
      :confirm-danger="true"
      @confirm="confirmDeletePost"
    />
    <ConfirmSheet
      v-if="deletingComment"
      v-model:show="showDeleteComment"
      :title="RECYCLE_TITLE"
      :message="commentDeleteMsg(deletingComment)"
      :confirm-text="RECYCLE_CONFIRM"
      :confirm-danger="true"
      @confirm="confirmDeleteComment"
    />
    <!-- 全屏媒体查看器（列表页直接看大图） -->
    <MediaViewer
      v-if="viewer"
      :items="viewer.post.media"
      :start="viewer.index"
      mode="post"
      @close="viewer = null"
    />
    <AppToast :text="toast" @done="toast = ''" />
  </PageLayout>
</template>

<style scoped>
.load-more {
  text-align: center;
  color: var(--muted-ink);
  font-size: 13px;
  padding: 16px 0 4px;
}

/* iOS 动态卡片 */
.post {
  background: var(--surface);
  border-radius: 12px;
  margin-bottom: 14px;
  overflow: hidden;
  box-shadow: var(--shadow);
}
/* ?focus=<id> 定位高亮（与地图点位列表同一套语言） */
.post.hl {
  box-shadow: 0 0 0 2px var(--primary), var(--shadow);
}
/* 视频媒体格：缩略图 + 右下播放角标（与时间轴 TimelineGrid 同一套视觉语言）。
   角标是绝对定位浮层，故容器必须是定位上下文——TimelineGrid 的 .video-mark 规则
   是 scoped 的，不会作用到这里，必须在本文件自带一份。 */
.video-ph {
  position: relative;
  cursor: pointer;
}
.video-mark {
  position: absolute;
  right: 5px;
  bottom: 5px;
  width: 18px;
  height: 18px;
  border-radius: 50%;
  background: rgba(0, 0, 0, 0.55);
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
}
/* 音频媒体格（原详情页能力，列表页补齐） */
.audio-ph {
  display: grid;
  place-items: center;
  gap: 6px;
  color: var(--muted-ink);
  background: var(--bg);
}
.audio-ph audio {
  width: 100%;
}
.head {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 14px 0;
}
.mini {
  width: 36px;
  height: 36px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  font-size: 14px;
  font-weight: 600;
  flex-shrink: 0;
}
.mini.her {
  background: var(--primary);
}
.mini.him {
  background: var(--sem-blue);
}
.mini.other {
  background: #b0b0b5;
}
.who {
  flex: 1;
}
.who b {
  font-size: 14px;
  display: block;
}
.who span {
  font-size: 11px;
  color: var(--muted-ink);
}
.mood {
  font-size: 11px;
  padding: 4px 10px;
  border-radius: 999px;
  font-weight: 600;
  background: rgba(255, 143, 171, 0.14);
  color: var(--primary-deep);
}
:root[data-theme='dark'] .mood {
  background: rgba(255, 143, 171, 0.18);
  color: #ffb4c2;
}
.p-text {
  padding: 8px 14px 0;
  font-size: 14px;
  line-height: 1.55;
  white-space: pre-wrap;
}
.media-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 3px;
  margin: 10px 0 0;
}
.media-grid.single {
  grid-template-columns: 1fr;
}
.media-grid.dual {
  grid-template-columns: repeat(2, 1fr);
}
.ph {
  width: 100%;
  aspect-ratio: 1;
  object-fit: cover;
  display: block;
  background: var(--bg);
}
.media-grid.single .ph {
  aspect-ratio: 4/3;
}
.loc {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 8px 14px 0;
  font-size: 12px;
  color: var(--muted-ink);
}
.actions {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 14px 12px;
}
.act {
  position: relative; /* 粒子扩散的锚点 */
  display: flex;
  align-items: center;
  gap: 4px;
  border: none;
  background: none;
  font-size: 13px;
  color: var(--muted-ink);
  cursor: pointer;
  padding: 4px 6px;
}
.act.on {
  color: var(--primary-deep);
}
/* 微粒子扩散（PRODUCT「动效规范」）：6 颗小点自图标中心向外扩散淡出，约 450ms，非夸张 */
.burst {
  position: absolute;
  left: 9px; /* 图标 18px 的中心 */
  top: 50%;
  width: 0;
  height: 0;
  pointer-events: none;
}
.burst i {
  position: absolute;
  width: 4px;
  height: 4px;
  margin: -2px 0 0 -2px;
  border-radius: 50%;
  background: var(--primary-deep);
  opacity: 0;
  animation: burst-out 450ms ease-out forwards;
  animation-delay: var(--d, 0ms);
}
@keyframes burst-out {
  0% {
    opacity: 0.9;
    transform: translate(0, 0) scale(1);
  }
  100% {
    opacity: 0;
    transform: translate(var(--dx), var(--dy)) scale(0.4);
  }
}
/* 图标轻微回弹，与粒子同步 */
.act.bounce :deep(svg) {
  animation: act-bounce 260ms ease-out;
}
@keyframes act-bounce {
  0% {
    transform: scale(1);
  }
  40% {
    transform: scale(1.25);
  }
  100% {
    transform: scale(1);
  }
}
@media (prefers-reduced-motion: reduce) {
  .burst {
    display: none;
  }
  .act.bounce :deep(svg) {
    animation: none;
  }
}
.act b {
  font-size: 12px;
}
.comments {
  border-top: 0.5px solid var(--rule);
  padding: 10px 14px;
}
.comment {
  font-size: 13px;
  margin-bottom: 6px;
}
.comment b {
  color: var(--primary-deep);
  margin-right: 6px;
}
.no-comment {
  font-size: 13px;
  color: var(--muted-ink);
}
.comment-input {
  display: flex;
  gap: 8px;
  margin-top: 8px;
}
.comment-input input {
  flex: 1;
  border: 1px solid var(--rule);
  border-radius: 999px;
  padding: 8px 14px;
  font-size: 13px;
  background: var(--bg);
  color: var(--ink);
}
.comment-input button {
  border: none;
  background: var(--primary);
  color: #fff;
  border-radius: 999px;
  padding: 0 16px;
  font-weight: 600;
}
</style>
