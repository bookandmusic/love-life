<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ChevronLeft, Heart, Music, Star, MessageCircle, MapPin, Trash2, ChevronDown, ChevronUp, X } from 'lucide-vue-next'
import { api, fullUrl, fmtDateTime } from '../api/client'
import { mediaUrl } from '../utils/mediaUrls'
import ConfirmSheet from '../components/ConfirmSheet.vue'
import MediaViewer from '../components/MediaViewer.vue'
import { useSpace } from '../stores/space'

const route = useRoute()
const router = useRouter()
const postID = route.params.id as string

interface MediaItem {
  id: number
  kind: string
  thumb_url: string
  url?: string
}

// 全屏媒体查看器（iOS）
const viewer = ref<{ index: number } | null>(null)
function openViewer(index: number) { viewer.value = { index } }

interface CommentItem {
  id: number
  author_id: number
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
const post = ref<PostItem | null>(null)
const comments = ref<CommentItem[]>([])
const commentText = ref('')
const showComments = ref(false)
const error = ref('')
const loading = ref(true)
// 点赞微粒子（§6.5：微粒子扩散，非夸张动画）
const burst = ref<number>(0)
// B9 sheet 状态
const showDeletePost = ref(false)
const showDeleteComment = ref(false)
const deletingComment = ref<CommentItem | null>(null)
// 3.9 媒体条目删除
const actionError = ref('')

async function load() {
  loading.value = true
  error.value = ''
  try {
    post.value = await api<PostItem>(`/api/v1/posts/${postID}`)
    await spaceStore.load()
    await loadComments()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
  } finally {
    loading.value = false
  }
}

async function loadComments() {
  try {
    const res = await api<{ items: CommentItem[] }>(`/api/v1/posts/${postID}/comments`)
    comments.value = res.items
  } catch {
    comments.value = []
  }
}

async function toggleLike() {
  if (!post.value) return
  try {
    const res = await api<{ liked: boolean }>(`/api/v1/posts/${postID}/like`, { method: 'POST' })
    post.value.liked = res.liked
    post.value.like_count += res.liked ? 1 : -1
    if (res.liked) burst.value++ // 触发微粒子
  } catch (e) {
    actionError.value = e instanceof Error ? e.message : '操作失败'
  }
}

async function toggleFav() {
  if (!post.value) return
  try {
    const res = await api<{ favorited: boolean }>(`/api/v1/posts/${postID}/favorite`, { method: 'POST' })
    post.value.favorited = res.favorited
    post.value.favorite_count += res.favorited ? 1 : -1
  } catch (e) {
    actionError.value = e instanceof Error ? e.message : '操作失败'
  }
}

async function submitComment() {
  const text = commentText.value.trim()
  if (!text) return
  try {
    await api(`/api/v1/posts/${postID}/comments`, {
      method: 'POST',
      body: JSON.stringify({ content: text }),
    })
    commentText.value = ''
    if (post.value) post.value.comment_count++
    await loadComments()
  } catch (e) {
    actionError.value = e instanceof Error ? e.message : '评论失败'
  }
}

async function remove() {
  if (!post.value) return
  showDeletePost.value = true
}

async function confirmDeletePost() {
  if (!post.value) return
  try {
    await api(`/api/v1/posts/${postID}`, { method: 'DELETE' })
    router.replace('/posts')
  } catch (e) {
    actionError.value = e instanceof Error ? e.message : '删除失败'
  }
}

// C1 评论删除：标记待删评论，弹确认
function askDeleteComment(c: CommentItem) {
  deletingComment.value = c
  showDeleteComment.value = true
}

async function confirmDeleteComment() {
  if (!deletingComment.value) return
  const cid = deletingComment.value.id
  try {
    await api(`/api/v1/comments/${cid}`, { method: 'DELETE' })
    comments.value = comments.value.filter((c) => c.id !== cid)
    if (post.value) post.value.comment_count = Math.max(0, post.value.comment_count - 1)
  } catch (e) {
    actionError.value = e instanceof Error ? e.message : '删除评论失败'
  } finally {
    deletingComment.value = null
  }
}

onMounted(load)
</script>

<template>
  <section class="detail">
    <header class="navbar glass">
      <button class="nav-btn" aria-label="返回" @click="router.back()" style="margin-right: auto"><ChevronLeft :size="22" stroke-width="2.5" /></button>
      <span class="title">动态详情</span>
      <div class="nav-right">
        <button v-if="post" class="nav-btn danger" aria-label="删除" @click="remove"><Trash2 :size="16" stroke-width="1.8" /></button>
      </div>
    </header>

    <p v-if="error" class="error">{{ error }}</p>
    <p v-else-if="loading" class="loading">加载中…</p>
    <article v-else-if="post" class="plate">
      <div class="head">
        <div class="mini" :class="spaceStore.roleClass(post.author_id)">{{ post.author.slice(0, 1) }}</div>
        <div class="who">
          <b>{{ post.author }}</b>
          <span class="num">{{ fmtDateTime(post.created_at) }}</span>
        </div>
      </div>
      <h2 v-if="post.title">{{ post.title }}</h2>
      <p class="content">{{ post.content }}</p>

      <div
        v-if="post.media.length"
        class="media"
        :class="{ single: post.media.length === 1, dual: post.media.length === 2 }"
      >
        <div v-for="(m, i) in post.media" :key="m.id" class="media-cell" @click="openViewer(i)">
          <video v-if="m.kind === 'video'" class="thumb video" controls playsinline>
            <source :src="fullUrl(mediaUrl(m).raw)" />
          </video>
          <div v-else-if="m.kind === 'audio'" class="thumb audio-thumb">
            <Music :size="28" stroke-width="1.6" />
            <audio controls preload="none" :src="fullUrl(mediaUrl(m).raw)" />
          </div>
          <img v-else class="thumb" :src="fullUrl(mediaUrl(m).thumb)" loading="lazy" />
        </div>
      </div>

      <footer v-if="post.mood || post.location_name" class="tags-row">
        <span v-if="post.mood" class="tag">{{ post.mood }}</span>
        <span v-if="post.location_name" class="tag"><MapPin :size="12" stroke-width="1.8" />{{ post.location_name }}</span>
      </footer>

      <div class="actions-row">
        <!-- 点赞微粒子：burst 变化时重新扩散（§6.5） -->
        <Transition name="burst">
          <span v-if="burst > 0" :key="burst" class="burst" aria-hidden="true">
            <i v-for="n in 6" :key="n" :style="{ '--i': n }" />
          </span>
        </Transition>
        <span class="acts">
          <button class="act" :class="{ on: post.liked }" @click="toggleLike">
            <Heart :size="20" :fill="post.liked ? 'currentColor' : 'none'" stroke-width="1.8" />
            <b class="num">{{ post.like_count || '' }}</b>
          </button>
          <button class="act" :class="{ on: post.favorited }" @click="toggleFav">
            <Star :size="20" :fill="post.favorited ? 'currentColor' : 'none'" stroke-width="1.8" />
            <b class="num">{{ post.favorite_count || '' }}</b>
          </button>
          <button class="act" @click="showComments = !showComments">
            <MessageCircle :size="20" stroke-width="1.8" />
            <b class="num">{{ post.comment_count || '' }}</b>
            <ChevronDown v-if="!showComments" :size="14" stroke-width="1.8" />
            <ChevronUp v-else :size="14" stroke-width="1.8" />
          </button>
        </span>
      </div>

      <div v-if="showComments" class="comments">        <div v-for="c in comments" :key="c.id" class="comment">
          <b>{{ c.author }}</b>
          <span>{{ c.content }}</span>
          <button class="comment-del" @click="askDeleteComment(c)" aria-label="删除评论"><X :size="13" stroke-width="2" /></button>
        </div>
        <p v-if="comments.length === 0" class="no-comment">还没有评论</p>
        <div class="comment-input">
          <input v-model="commentText" placeholder="说点什么…" @keyup.enter="submitComment" />
          <button @click="submitComment">发送</button>
        </div>
      </div>
    </article>
    <!-- B9 删除确认 sheets -->
    <ConfirmSheet
      v-model:show="showDeletePost"
      title="删除动态"
      message="删除后将进入回收站（软删除），恢复需联系管理员"
      confirm-text="删除"
      :confirm-danger="true"
      @confirm="confirmDeletePost"
    />
    <ConfirmSheet
      v-if="deletingComment"
      v-model:show="showDeleteComment"
      title="删除评论"
      :message="`确定删除「${deletingComment.author}：${deletingComment.content.slice(0, 20)}…」？`"
      confirm-text="删除"
      :confirm-danger="true"
      @confirm="confirmDeleteComment"
    />
    <!-- 全屏媒体查看器（iOS） -->
    <MediaViewer
      v-if="viewer && post"
      :items="post.media"
      :start="viewer.index"
      mode="post"
      @close="viewer = null"
    />
    <!-- 错误提示 toast -->
    <Transition name="fade">
      <div v-if="actionError" class="toast" @click="actionError = ''">{{ actionError }}</div>
    </Transition>
  </section>
</template>

<style scoped>
.detail {
  height: 100%;
  display: flex;
  flex-direction: column;
  min-height: 0;
}
.navbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: calc(10px + env(safe-area-inset-top)) 12px 10px;
  flex-shrink: 0;
  z-index: 10;
}
.title {
  font-size: 17px;
  font-weight: 600;
}
.nav-right {
  display: flex;
  align-items: center;
  gap: 4px;
  margin-left: auto;
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
.nav-btn.danger {
  color: var(--danger);
}
.loading,
.error {
  text-align: center;
  color: var(--muted-ink);
  margin-top: 3rem;
}
.loading {
  animation: breathe 1.8s ease-in-out infinite;
}
.error {
  color: var(--danger);
}
.plate {
  flex: 1;
  overflow-y: auto;
  padding: 12px 16px 32px;
}
.head {
  display: flex;
  align-items: center;
  gap: 10px;
  padding-bottom: 0.7rem;
  border-bottom: 0.5px solid var(--rule);
}
.mini {
  width: 40px;
  height: 40px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  font-size: 15px;
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
  font-size: 15px;
  display: block;
}
.who span {
  font-size: 11px;
  color: var(--muted-ink);
}
h2 {
  font-size: 17px;
  margin: 10px 0 2px;
}
.content {
  margin: 0.25rem 0;
  white-space: pre-wrap;
  line-height: 1.6;
  font-size: 0.9375rem;
}
.media {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 4px;
  margin-top: 0.5rem;
}
.media.single {
  grid-template-columns: minmax(0, 100%);
}
.media.dual {
  grid-template-columns: repeat(2, 1fr);
}
.thumb {
  width: 100%;
  aspect-ratio: 1;
  object-fit: cover;
  border-radius: 3px;
  background: var(--bg);
}
.media.single .thumb {
  aspect-ratio: auto;
}
.thumb.audio-thumb {
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(90, 167, 212, 0.14);
  color: var(--sem-blue);
  border-radius: 8px;
}
.tags-row {
  display: flex;
  gap: 0.4rem;
  margin-top: 0.55rem;
  flex-wrap: wrap;
}
.tag {
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
  border: 1px solid var(--rule);
  border-radius: 999px;
  padding: 0.14rem 0.6rem;
  font-size: 0.75rem;
  color: var(--muted-ink);
}
.actions-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-top: 0.7rem;
  padding-top: 0.55rem;
  border-top: 1px solid var(--rule);
  position: relative;
}
/* 点赞微粒子：6 个粒子按角度扩散 + 淡出（§6.5 微粒子扩散） */
.burst {
  position: absolute;
  left: 3.2rem;
  top: 0.1rem;
  pointer-events: none;
}
.burst i {
  position: absolute;
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: var(--primary);
  animation: particle 0.55s ease-out forwards;
}
.burst i:nth-child(1) { --a: 0deg; }
.burst i:nth-child(2) { --a: 60deg; }
.burst i:nth-child(3) { --a: 120deg; }
.burst i:nth-child(4) { --a: 180deg; }
.burst i:nth-child(5) { --a: 240deg; }
.burst i:nth-child(6) { --a: 300deg; }
@keyframes particle {
  0% {
    transform: rotate(var(--a)) translateX(0);
    opacity: 0.9;
  }
  100% {
    transform: rotate(var(--a)) translateX(16px);
    opacity: 0;
  }
}
.burst-leave-active {
  transition: opacity 0.2s ease;
}
.burst-leave-to {
  opacity: 0;
}
.acts {
  display: flex;
  gap: 0.4rem;
}
.act {
  display: inline-flex;
  align-items: center;
  gap: 0.3rem;
  border: none;
  background: none;
  font-size: 0.8125rem;
  color: var(--muted-ink);
  padding: 0.3rem 0.5rem;
}
.act b {
  font-size: 0.75rem;
}
.act.on {
  color: var(--primary-deep);
}
.comments {
  border-top: 1px solid var(--rule);
  margin-top: 0.6rem;
  padding-top: 0.6rem;
}
.comment {
  font-size: 0.875rem;
  margin-bottom: 0.4rem;
}
.comment b {
  color: var(--primary-deep);
  margin-right: 0.4rem;
}
.no-comment {
  color: var(--muted-ink);
  font-size: 0.8125rem;
}
.comment-input {
  display: flex;
  gap: 0.5rem;
  margin-top: 0.5rem;
}
.comment-input input {
  flex: 1;
  border: 1px solid var(--rule);
  border-radius: 999px;
  padding: 0.45rem 0.9rem;
  font-size: 0.875rem;
  background: var(--bg);
  color: var(--ink);
}
.comment-input button {
  border: none;
  background: var(--primary);
  color: #fff;
  border-radius: 999px;
  padding: 0 1rem;
  font-weight: 600;
}
/* C1 评论删除按钮 */
.comment {
  position: relative;
}
.comment-del {
  position: absolute;
  right: 0;
  top: 50%;
  transform: translateY(-50%);
  border: none;
  background: none;
  color: var(--muted-ink);
  opacity: 0.3;
  cursor: pointer;
  padding: 4px;
  border-radius: 50%;
  display: flex;
  align-items: center;
}
.comment-del:hover, .comment-del:focus { opacity: 1; color: var(--danger); }
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
