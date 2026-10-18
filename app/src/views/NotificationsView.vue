<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Bell, Heart, MessageCircle, Star, Trash2, Images, Cake, MapPin } from 'lucide-vue-next'
import { api, errText } from '../api/client'
import { fmtDateTime } from '../utils/time'
import AppToast from '../components/AppToast.vue'
import PageLayout from '../components/PageLayout.vue'
import EmptyState from '../components/EmptyState.vue'
import ErrorState from '../components/ErrorState.vue'
import SkeletonState from '../components/SkeletonState.vue'
import { useUnread } from '../stores/notifications'
import { useAuth } from '../stores/auth'

interface NotifItem {
  id: number
  event_type: string
  target_type: string
  target_id: number
  snapshot: string
  created_at: string
}

const router = useRouter()
const unreadStore = useUnread()
const auth = useAuth()
const items = ref<NotifItem[]>([])
const error = ref('')
const loading = ref(false)
const toast = ref('')

// 事件 → 图标与颜色
const eventIcon: Record<string, { icon: typeof Heart; color: string }> = {
  post_created: { icon: Bell, color: 'var(--primary-deep)' },
  post_liked: { icon: Heart, color: 'var(--primary)' },
  post_favorited: { icon: Star, color: 'var(--sem-gold)' },
  post_commented: { icon: MessageCircle, color: 'var(--sem-blue)' },
  post_deleted: { icon: Trash2, color: 'var(--muted-ink)' },
  comment_deleted: { icon: Trash2, color: 'var(--muted-ink)' },
  album_created: { icon: Images, color: 'var(--sem-blue)' },
  album_deleted: { icon: Trash2, color: 'var(--muted-ink)' },
  anniversary_deleted: { icon: Trash2, color: 'var(--muted-ink)' },
  map_point_deleted: { icon: Trash2, color: 'var(--muted-ink)' },
  anniversary_reminder: { icon: Cake, color: 'var(--sem-gold)' },
  map_point_created: { icon: MapPin, color: 'var(--sem-green)' },
}

function iconOf(t: string) {
  return eventIcon[t] ?? { icon: Bell, color: 'var(--muted-ink)' }
}

// 互动通知归属：目标动态作者是当前用户 → 「你的动态」；是操作者本人 → 「自己的动态」。
// author_id 缺失（升级前的历史通知）按旧口径「你的」渲染；当前身份未就绪时同样不猜归属，
// 一并回退「你的」——反猜成「自己的」会把对方的内容说成自己的。
function possessive(s: Record<string, unknown>): string {
  if (typeof s.author_id !== 'number') return '你的'
  const me = auth.user?.id
  if (me == null) return '你的'
  return s.author_id === me ? '你的' : '自己的'
}

const eventText: Record<string, (s: Record<string, unknown>) => string> = {
  post_created: (s) => `${s.operator} 发布了新动态：${s.content ?? ''}`,
  post_liked: (s) => `${s.operator} 赞了${possessive(s)}动态`,
  post_favorited: (s) => `${s.operator} 收藏了${possessive(s)}动态`,
  post_commented: (s) => `${s.operator} 评论了${possessive(s)}动态：${s.content ?? ''}`,
  post_deleted: (s) => `${s.deleted_by} 删除了你发布的动态：${s.content ?? ''}`,
  comment_deleted: (s) => `${s.deleted_by} 删除了你的评论：${s.content ?? ''}`,
  album_created: (s) => `${s.operator ?? ''} 创建了相册「${s.name}」`,
  anniversary_reminder: (s) => `纪念日「${s.name}」快到了`,
  album_deleted: (s) => `${s.deleted_by ?? '对方'} 删除了你的相册「${s.name ?? ''}」`,
  anniversary_deleted: (s) => `${s.deleted_by ?? '对方'} 删除了你的纪念日「${s.name ?? ''}」`,
  map_point_deleted: (s) => `${s.deleted_by ?? '对方'} 删除了你的足迹「${s.name ?? ''}」`,
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
  // 未知事件类型（如 mock/旧数据的 system）不直出英文类型名
  return fn ? fn(snap) : '系统通知'
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const res = await api<{ items: NotifItem[] }>('/api/v1/notifications')
    items.value = res.items
    unreadStore.refresh()
  } catch (e) {
    error.value = errText(e, '加载失败')
  } finally {
    loading.value = false
  }
}

async function clearAll() {
  try {
    await api('/api/v1/notifications', { method: 'DELETE' })
    items.value = []
    unreadStore.reset()
  } catch (e) {
    toast.value = errText(e, '操作失败')
  }
}

function targetRoute(n: NotifItem): string | null {
 // 内容删除通知无跳转：内容已不可见（「内容删除通知无跳转」）
  if (n.event_type.endsWith('_deleted')) return null
  if (!n.target_type || !n.target_id) return null
  switch (n.target_type) {
    case 'post':
      return `/posts?focus=${n.target_id}`
    case 'album':
      return `/albums/${n.target_id}`
    case 'anniversary':
      // 定位到具体纪念日卡片并短暂高亮（与动态 / 地图同款；PRODUCT「通知中心」交互）
      return `/anniversaries?focus=${n.target_id}`
    case 'map_point':
      return `/map?focus=${n.target_id}` // 定位到具体点位（列表页无锚点，只能到地图居中高亮）
    default:
      return null
  }
}

async function open(n: NotifItem) {
  // 点击即删（PRODUCT「通知中心」：通知是临时消费品，点击 = 知道）——无论有没有跳转目标都先消费；
  // 失败静默：仍继续跳转，条目下次加载仍在
  try {
    await api(`/api/v1/notifications/${n.id}`, { method: 'DELETE' })
    items.value = items.value.filter((x) => x.id !== n.id)
    unreadStore.decrement() // 删除成功后红点同步减 1（失败静默则不减）
  } catch {
    /* 删除失败静默 */
  }
  const to = targetRoute(n)
  if (to) {
    router.push(to)
    return
  }
  // 删除类通知无跳转：明说一句（内容已不可见）；其余无目标的通知（如系统公告）点了就是知道，静默消失
  if (n.event_type.endsWith('_deleted')) toast.value = '内容已删除'
}

onMounted(load)
</script>

<template>
  <PageLayout title="通知" back standalone @back="router.back()">
    <template #nav-right>
      <button class="nav-btn action" @click="clearAll"><Trash2 :size="16" stroke-width="2" />清空</button>
    </template>
      <SkeletonState v-if="loading" variant="list" :rows="5" />
      <ErrorState v-else-if="error" :error="error" :retry="load" />
      <EmptyState v-else-if="items.length === 0" :icon="Bell" title="暂时没有通知" sub="对方点赞、评论，或到纪念日时，会在这里告诉你。" />

      <div v-else class="group">
        <div
          v-for="n in items"
          :key="n.id"
          class="row"
          :class="{ tappable: !!targetRoute(n) }"
          @click="open(n)"
        >
          <span class="row-icon" :style="{ color: iconOf(n.event_type).color }">
            <component :is="iconOf(n.event_type).icon" :size="16" :fill="n.event_type === 'post_liked' ? 'currentColor' : 'none'" stroke-width="2" />
          </span>
          <div class="row-body">
            <span class="label">{{ text(n) }}</span>
            <span class="value num">{{ fmtDateTime(n.created_at) }}</span>
          </div>
        </div>
      </div>

    <AppToast :text="toast" @done="toast = ''" />
  </PageLayout>
</template>

<style scoped>
.nav-btn {
  border: none;
  background: none;
  color: var(--tint);
  cursor: pointer;
  padding: 4px;
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 14px;
}
.nav-btn.action {
  color: var(--tint);
  width: auto;
}


.row {
  display: flex;
  align-items: flex-start;
  gap: 10px;
}
.row-icon {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  height: 21px; /* 与 14px×1.5 的首行文字同高：图标光学居中于首行，长内容时不被拉高 */
}
.row-body {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.label {
  font-size: 14px;
  line-height: 1.5;
  display: -webkit-box;
  -webkit-line-clamp: 2; /* 长内容两行截断：通知是临时消费品，全文去内容页看 */
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.value {
  color: var(--muted-ink);
  font-size: 12px;
  white-space: nowrap;
}
/* 列表内均为未消费通知（临时消费品，PRODUCT「通知中心」）：无需区分已读态，
   红点恒显示、正文恒加粗。 */
.row .label {
  font-weight: 600;
}
.row::before {
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
</style>
