<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ChevronLeft, CheckCheck, Bell, Heart, MessageCircle, Star, Trash2, Images, Cake, MapPin } from 'lucide-vue-next'
import { api, fmtDateTime } from '../api/client'
import { useUnread } from '../stores/notifications'

interface NotifItem {
  id: number
  event_type: string
  target_type: string
  target_id: number
  snapshot: string
  is_read: boolean
  created_at: string
}

const router = useRouter()
const unreadStore = useUnread()
const items = ref<NotifItem[]>([])
const error = ref('')
const loading = ref(false)
const toast = ref('')

// 事件 → 图标与颜色
const eventIcon: Record<string, { icon: typeof Heart; color: string }> = {
  post_created: { icon: Bell, color: 'var(--primary-deep)' },
  post_liked: { icon: Heart, color: 'var(--primary)' },
  post_favorited: { icon: Star, color: 'var(--sem-gold)' },
  post_commented: { icon: MessageCircle, color: 'var(--ios-blue)' },
  post_deleted: { icon: Trash2, color: 'var(--muted-ink)' },
  comment_deleted: { icon: Trash2, color: 'var(--muted-ink)' },
  album_created: { icon: Images, color: 'var(--sem-blue)' },
  anniversary_reminder: { icon: Cake, color: 'var(--sem-gold)' },
  media_deleted: { icon: Trash2, color: 'var(--muted-ink)' },
  map_point_created: { icon: MapPin, color: 'var(--sem-green)' },
}

function iconOf(t: string) {
  return eventIcon[t] ?? { icon: Bell, color: 'var(--muted-ink)' }
}

const eventText: Record<string, (s: Record<string, unknown>) => string> = {
  post_created: (s) => `${s.operator} 发布了新动态：${s.content ?? ''}`,
  post_liked: (s) => `${s.operator} 赞了你的动态`,
  post_favorited: (s) => `${s.operator} 收藏了你的动态`,
  post_commented: (s) => `${s.operator} 评论了你的动态：${s.content ?? ''}`,
  post_deleted: (s) => `${s.deleted_by} 删除了你发布的动态：${s.content ?? ''}`,
  comment_deleted: (s) => `${s.deleted_by} 删除了你的评论：${s.content ?? ''}`,
  album_created: (s) => (s.monthly ? `月度相册「${s.name}」已生成` : `${s.operator} 创建了相册「${s.name}」`),
  anniversary_reminder: (s) => `纪念日「${s.name}」快到了`,
  media_deleted: (s) => `${s.deleted_by ?? '对方'} 删除了媒体`,
  map_point_created: (s) => `${s.operator ?? '对方'} 添加了足迹「${s.name ?? ''}」`,
}

function text(n: NotifItem): string {
  let snap: Record<string, unknown> = {}
  try {
    snap = JSON.parse(n.snapshot || '{}')
  } catch {
    /* 快照缺失时退回事件类型 */
  }
  const fn = eventText[n.event_type]
  return fn ? fn(snap) : n.event_type
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const res = await api<{ items: NotifItem[] }>('/api/v1/notifications')
    items.value = res.items
    unreadStore.refresh()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
  } finally {
    loading.value = false
  }
}

async function readAll() {
  try {
    await api('/api/v1/notifications/read-all', { method: 'POST' })
    unreadStore.reset()
    await load()
  } catch (e) {
    toast.value = e instanceof Error ? e.message : '操作失败'
    setTimeout(() => (toast.value = ''), 1800)
  }
}

function targetRoute(n: NotifItem): string | null {
  if (!n.target_type || !n.target_id) return null
  switch (n.target_type) {
    case 'post':
      return `/posts/${n.target_id}`
    case 'album':
      return `/albums/${n.target_id}`
    case 'anniversary':
      return '/anniversaries'
    case 'map_point':
      return '/map'
    default:
      return null
  }
}

async function open(n: NotifItem) {
  const to = targetRoute(n)
  if (!to) return
  if (!n.is_read) {
    try {
      await api(`/api/v1/notifications/${n.id}/read`, { method: 'POST' })
      n.is_read = true
    } catch {
      /* 已读失败静默 */
    }
  }
  router.push(to)
}

onMounted(load)
</script>

<template>
  <div class="notif-page page-col">
    <!-- 毛玻璃导航栏（demo subpage 风格） -->
    <header class="navbar glass">
      <button class="nav-btn" aria-label="返回" @click="router.back()" style="margin-right: auto">
        <ChevronLeft :size="22" stroke-width="2.5" />
      </button>
      <span class="title">通知</span>
      <div class="nav-right">
        <button class="nav-btn action" @click="readAll"><CheckCheck :size="18" stroke-width="2" />全部已读</button>
      </div>
    </header>

    <div class="content">
      <SkeletonState v-if="loading" variant="list" :rows="5" />
      <ErrorState v-else-if="error" :error="error" :retry="load" />
      <p v-else-if="items.length === 0" class="empty">
        <Bell :size="34" stroke-width="1.2" class="bell" /><br />暂无通知
      </p>

      <div v-else class="group">
        <div
          v-for="n in items"
          :key="n.id"
          class="row"
          :class="{ unread: !n.is_read, tappable: !!targetRoute(n) }"
          @click="open(n)"
        >
          <span class="row-icon" :style="{ color: iconOf(n.event_type).color }">
            <component :is="iconOf(n.event_type).icon" :size="16" :fill="n.event_type === 'post_liked' ? 'currentColor' : 'none'" stroke-width="2" />
          </span>
          <span class="label">{{ text(n) }}</span>
          <span class="value num">{{ fmtDateTime(n.created_at) }}</span>
        </div>
      </div>
    </div>

    <Transition name="fade">
      <div v-if="toast" class="toast" @click="toast = ''">{{ toast }}</div>
    </Transition>
  </div>
</template>

<style scoped>
.notif-page {
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
  gap: 4px;
  font-size: 14px;
}
.nav-btn.action {
  color: var(--ios-blue);
  width: auto;
}

.content {
  flex: 1;
  overflow-y: auto;
  -webkit-overflow-scrolling: touch;
  padding: 12px 16px 24px;
}

.row {
  display: flex;
  align-items: center;
  gap: 10px;
}
.row-icon {
  flex-shrink: 0;
  display: flex;
  align-items: center;
}
.label {
  flex: 1;
  font-size: 14px;
  min-width: 0;
}
.value {
  color: var(--muted-ink);
  font-size: 12px;
  white-space: nowrap;
}
.row.unread .label {
  font-weight: 600;
}
.row.unread::before {
  content: '';
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--danger);
  position: absolute;
  margin-left: -12px;
}
.row {
  position: relative;
  padding-left: 4px;
}
.row.tappable {
  cursor: pointer;
}
.row.tappable:active {
  opacity: 0.6;
}

.empty,
.error {
  text-align: center;
  color: var(--muted-ink);
  padding: 60px 0;
}
.bell {
  color: var(--primary-deep);
  opacity: 0.5;
  margin-bottom: 8px;
}
.error {
  color: var(--danger);
}

.toast {
  position: fixed;
  bottom: calc(16px + env(safe-area-inset-bottom));
  left: 50%;
  transform: translateX(-50%);
  background: rgba(0, 0, 0, 0.78);
  color: #fff;
  font-size: 14px;
  padding: 10px 20px;
  border-radius: 12px;
  z-index: 1100;
  max-width: 90vw;
  text-align: center;
}
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.22s ease;
}
.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
</style>
