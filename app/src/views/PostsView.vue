<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Heart, Star, MessageCircle, MapPin, PenLine } from 'lucide-vue-next'
import { api, fullUrl, fmtDateTime } from '../api/client'
import NavActions from '../components/NavActions.vue'
import SkeletonState from '../components/SkeletonState.vue'
import ErrorState from '../components/ErrorState.vue'
import { useSpace } from '../stores/space'

const router = useRouter()
function goDetail(id: number) {
  router.push(`/posts/${id}`)
}

interface MediaItem {
  id: number
  kind: string
  thumb_url: string
  url?: string
}

interface CommentItem {
  id: number
  author: string
  content: string
  created_at: string
}

interface PostItem {
  id: number
  author_id: number
  author: string
  title: string
  content: string
  mood: string
  location_name: string
  created_at: string
  media: MediaItem[]
  like_count: number
  liked: boolean
  favorite_count: number
  favorited: boolean
  comment_count: number
}

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

const noMore = computed(() => posts.value.length > 0 && posts.value.length >= total.value)
const PAGE_SIZE = 20

// 按 author_id 映射真实 role（girl/boy），替代昵称长度猜色
async function load() {
  loading.value = true
  error.value = ''
  try {
    const res = await api<{ items: PostItem[]; total: number }>(`/api/v1/posts?page=1&page_size=${PAGE_SIZE}`)
    posts.value = res.items
    total.value = res.total
    page.value = 1
    await spaceStore.load()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
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
    const res = await api<{ items: PostItem[] }>(`/api/v1/posts?page=${next}&page_size=${PAGE_SIZE}`)
    posts.value.push(...res.items)
    page.value = next
  } catch (e) {
    toast.value = e instanceof Error ? e.message : '加载失败'
    setTimeout(() => (toast.value = ''), 1800)
  } finally {
    loadingMore.value = false
  }
}

function onScroll(e: Event) {
  const el = e.target as HTMLElement
  if (el.scrollTop + el.clientHeight >= el.scrollHeight - 120) loadMore()
}

async function toggleLike(p: PostItem) {
  try {
    const res = await api<{ liked: boolean }>(`/api/v1/posts/${p.id}/like`, { method: 'POST' })
    p.liked = res.liked
    p.like_count += res.liked ? 1 : -1
  } catch (e) {
    toast.value = e instanceof Error ? e.message : '操作失败'
  }
}

async function toggleFav(p: PostItem) {
  try {
    const res = await api<{ favorited: boolean }>(`/api/v1/posts/${p.id}/favorite`, { method: 'POST' })
    p.favorited = res.favorited
    p.favorite_count += res.favorited ? 1 : -1
  } catch (e) {
    toast.value = e instanceof Error ? e.message : '操作失败'
  }
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
  } catch {
    comments.value[postID] = []
  }
}

async function submitComment(p: PostItem) {
  const text = commentText.value.trim()
  if (!text) return
  try {
    await api(`/api/v1/posts/${p.id}/comments`, {
      method: 'POST',
      body: JSON.stringify({ content: text }),
    })
    commentText.value = ''
    p.comment_count++
    await loadComments(p.id)
  } catch (e) {
    toast.value = e instanceof Error ? e.message : '评论失败'
  }
}

onMounted(load)
</script>

<template>
  <div class="posts-page">
    <!-- 毛玻璃导航栏 -->
    <header class="navbar glass">
      <span class="title">动态</span>
      <NavActions />
    </header>

    <div class="scroll-area" @scroll.passive="onScroll">
      <SkeletonState v-if="loading" variant="list" :rows="3" />
      <ErrorState v-else-if="error" :error="error" :retry="load" />
      <div v-else-if="posts.length === 0" class="empty">
        <PenLine :size="32" stroke-width="1.2" class="empty-ic" />
        <p>还没有动态，点右下角 + 记录第一条吧</p>
      </div>

      <article v-for="p in posts" :key="p.id" class="post">
        <div class="head">
          <div class="mini" :class="spaceStore.roleClass(p.author_id)">{{ p.author.slice(0, 1) }}</div>
          <div class="who">
            <b>{{ p.author }}</b>
            <span>{{ fmtDateTime(p.created_at) }}</span>
          </div>
          <span v-if="p.mood" class="mood">{{ p.mood }}</span>
        </div>

        <h3 v-if="p.title" class="p-title" @click="goDetail(p.id)">{{ p.title }}</h3>
        <p class="p-text" @click="goDetail(p.id)">{{ p.content }}</p>

        <div
          v-if="p.media.length"
          class="media-grid"
          :class="{ single: p.media.length === 1, dual: p.media.length === 2 }"
          @click="goDetail(p.id)"
        >
          <template v-for="m in p.media" :key="m.id">
            <video v-if="m.kind === 'video'" class="ph" controls playsinline :poster="fullUrl(m.thumb_url)" preload="none" @click.stop>
              <source :src="fullUrl(m.url ?? m.thumb_url.replace('v=thumb', 'v=raw'))" />
            </video>
            <img v-else class="ph" :src="fullUrl(m.thumb_url)" loading="lazy" alt="" />
          </template>
        </div>

        <div v-if="p.location_name" class="loc">
          <MapPin :size="13" stroke-width="2" />{{ p.location_name }}
        </div>

        <div class="actions">
          <button class="act" :class="{ on: p.liked }" @click="toggleLike(p)">
            <Heart :size="18" :fill="p.liked ? 'currentColor' : 'none'" stroke-width="1.8" />
            <b class="num">{{ p.like_count || '' }}</b>
          </button>
          <button class="act" :class="{ on: p.favorited }" @click="toggleFav(p)">
            <Star :size="18" :fill="p.favorited ? 'currentColor' : 'none'" stroke-width="1.8" />
            <b class="num">{{ p.favorite_count || '' }}</b>
          </button>
          <button class="act" @click="toggleComments(p)">
            <MessageCircle :size="18" stroke-width="1.8" />
            <b class="num">{{ p.comment_count || '' }}</b>
          </button>
        </div>

        <div v-if="expanded === p.id" class="comments">
          <div v-for="c in comments[p.id] ?? []" :key="c.id" class="comment">
            <b>{{ c.author }}</b>
            <span>{{ c.content }}</span>
          </div>
          <p v-if="(comments[p.id] ?? []).length === 0" class="no-comment">还没有评论</p>
          <div class="comment-input">
            <input v-model="commentText" placeholder="说点什么…" @keyup.enter="submitComment(p)" />
            <button @click="submitComment(p)">发送</button>
          </div>
        </div>
      </article>

      <p v-if="loadingMore" class="load-more">加载中…</p>
      <p v-else-if="noMore && posts.length >= PAGE_SIZE" class="load-more">没有更多了</p>
    </div>
    <!-- B9 错误 toast -->
    <Transition name="fade">
      <div v-if="toast" class="toast" @click="toast = ''">{{ toast }}</div>
    </Transition>
  </div>
</template>

<style scoped>
.posts-page {
  height: 100%;
  display: flex;
  flex-direction: column;
}
.navbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: calc(10px + env(safe-area-inset-top)) 16px 10px;
  flex-shrink: 0;
  z-index: 10;
}
.title {
  font-size: 17px;
  font-weight: 600;
}
.scroll-area {
  flex: 1;
  overflow-y: auto;
  -webkit-overflow-scrolling: touch;
  padding: 12px 16px 24px;
}

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
.p-title {
  font-size: 16px;
  margin: 10px 14px 0;
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
.empty {
  text-align: center;
  color: var(--muted-ink);
  padding: 56px 0;
  display: flex;
  flex-direction: column;
  align-items: center;
}
.empty p {
  font-size: 14px;
  color: var(--ink);
}
.empty-ic {
  color: var(--primary);
  opacity: 0.55;
  margin-bottom: 10px;
}
.error {
  text-align: center;
  color: var(--danger);
  padding: 40px 0;
}
/* B9 错误 toast */
.toast {
  position: fixed;
  bottom: calc(16px + env(safe-area-inset-bottom));
  left: 50%;
  transform: translateX(-50%);
  background: rgba(0,0,0,0.78);
  color: #fff;
  font-size: 14px;
  padding: 10px 20px;
  border-radius: 12px;
  z-index: 1100;
  max-width: 90vw;
  text-align: center;
}
.fade-enter-active, .fade-leave-active { transition: opacity .22s ease; }
.fade-enter-from, .fade-leave-to { opacity: 0; }
</style>
