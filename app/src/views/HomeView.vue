<script setup lang="ts">
import { computed, onActivated, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Cake, ChevronRight, RefreshCw, CalendarDays, Heart, MapPin, Sparkles } from 'lucide-vue-next'
import { api, errText, fullUrl } from '../api/client'
import { mediaSrc, type Post } from '../types'
import PageLayout from '../components/PageLayout.vue'
import SkeletonState from '../components/SkeletonState.vue'
import ErrorState from '../components/ErrorState.vue'
import EmptyState from '../components/EmptyState.vue'
import { useSpace } from '../stores/space'

defineOptions({ name: 'HomeView' })

const router = useRouter()
const spaceStore = useSpace()

type PostItem = Pick<Post, 'id' | 'content' | 'mood' | 'location_name' | 'created_at' | 'media'>

// 那年今日：服务端按同月同日过滤后再分页，避免只在最新 50 条里找
const memories = ref<PostItem[]>([])
const loading = ref(false)
const error = ref('')
const space = computed(() => spaceStore.space)

const coupleNames = computed(() => {
  const names = spaceStore.space?.members.map((m) => m.nickname) ?? []
  return names.join(' & ') || '我们'
})

// 回忆标题：正文截断
function memoryTitle(m: PostItem): string {
  const t = (m.content || '').trim()
  return t.slice(0, 24) || '一条回忆'
}

// 回忆精选的两份分层池（服务端已按「有媒体 / 无媒体」随机抽好）；
// 抽到的这批用于挑选展示，刷新时由 load() 重新抽。
const mediaPosts = ref<PostItem[]>([])
const textPosts = ref<PostItem[]>([])
// 是否已成功取过数据：冷启动判定用它，不再拿「当前有没有数据」代替
// （抽样池可以为空，用长度判定会导致每次刷新都亮骨架屏）
const loaded = ref(false)
const shuffleTick = ref(0)
function shuffle<T>(arr: T[]): T[] {
  void shuffleTick.value // 依赖「换一批」的计数：值本身不用，但必须读，否则 computed 不再重算
  const a = [...arr]
  for (let i = a.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1))
    ;[a[i], a[j]] = [a[j], a[i]]
  }
  return a
}
const coverMemory = computed(() => {
  const withImg = mediaPosts.value.filter((p) => p.media.length > 0)
  return withImg.length ? shuffle(withImg)[0] : null
})
const textMemories = computed(() => shuffle(textPosts.value).slice(0, 3))
function refreshMemories() {
  shuffleTick.value++
}
const todayDD = computed(() => {
  const d = new Date()
  return `${String(d.getMonth() + 1).padStart(2, '0')}${String(d.getDate()).padStart(2, '0')}`
})
// 那年今日：同月同日的历史回忆（有数据才出现，入口卡）；服务端已按本地时区过滤，这里再校验一次。
// created_at 为 UTC（库内 datetime('now')），直接 slice 取的是 UTC 月日 → 必须解析后按本地时区取月日。
// 兜底：解析失败回退字符串 slice（老口径），不因格式异常而清空列表。
function localMMDD(s: string): string {
  const d = new Date(s.replace(' ', 'T') + (s.includes('Z') ? '' : 'Z'))
  if (Number.isNaN(d.getTime())) return `${(s || '').slice(5, 7)}${(s || '').slice(8, 10)}`
  return `${String(d.getMonth() + 1).padStart(2, '0')}${String(d.getDate()).padStart(2, '0')}`
}
// 「历史」年份：当年今天发的动态不算那年今日（「同月同日历史回忆」），
// 与服务端 ListPosts(memories) / mock 同口径。
function localYear(s: string): number {
  const d = new Date(s.replace(' ', 'T') + (s.includes('Z') ? '' : 'Z'))
  return Number.isNaN(d.getTime()) ? Number((s || '').slice(0, 4)) : d.getFullYear()
}
const thisYear = new Date().getFullYear()
const memoriesToday = computed(() =>
  memories.value.filter((x) => localMMDD(x.created_at) === todayDD.value && localYear(x.created_at) < thisYear),
)
const nextAnniv = computed(() => {
  const next = spaceStore.space?.next_anniversary
  return next && next.name ? next : null
})
// 「即将发生」：仅保留「那年今日」（纪念日已内联在头卡，避免重复）
const upcoming = computed(() => {
  if (!memoriesToday.value.length) return []
  return [
    {
      key: 'today',
      icon: 'cal' as const,
      title: '那年今日',
      sub: `${memoriesToday.value.length} 篇同月同日`,
      to: '/posts?memories=1',
    },
  ]
})

// 全新部署空态：无动态 + 无纪念日 + 无相册，三者皆空才算首启。
// 用计数而不是 next_anniversary.name：纪念日存在但日期全部无效时名称也是空，
// 旧判定会把有数据的空间误显示成空态。
// 不可放宽为「动态池为空」：否则「有相册/纪念日但没发过动态」的空间会被空态整页遮挡，
// 回忆精选（常驻兜底卡）与那年今日全不渲染，违反「首页永不空态」。
const totallyEmpty = computed(() => {
  const sp = spaceStore.space
  // 两份分层池一空即「没有动态可展示」：有图 / 无图两层都空，说明动态池为空
  const noPosts = mediaPosts.value.length === 0 && textPosts.value.length === 0
  return sp !== null && noPosts && sp.anniversary_count === 0 && sp.album_count === 0
})

async function load() {
  // 静默刷新：仅冷启动（无数据）时亮骨架屏；已有数据时后台重取、失败不打扰旧内容
  const cold = loaded.value === false
  loaded.value = true
  if (cold) {
    loading.value = true
    error.value = ''
  }
  try {
    // 回忆精选：服务端在**全量历史池**里随机取（只取最新 50 条会让更早的回忆抽不到），
    // 且**分层**取——封面卡要有图的、文字回忆要没图的，否则随机样本可能整批没照片、封面卡时有时无
    // （PRODUCT「首页」：回忆精选是常驻兜底卡）。
    const [, cover, text, m] = await Promise.all([
      spaceStore.load(true),
      api<{ items: PostItem[] }>('/api/v1/posts?page=1&page_size=20&sample=1&media=1'),
      api<{ items: PostItem[] }>('/api/v1/posts?page=1&page_size=20&sample=1&media=0'),
      api<{ items: PostItem[] }>('/api/v1/posts?memories=1&page=1&page_size=50'),
    ])
    mediaPosts.value = cover.items
    textPosts.value = text.items
    memories.value = m.items
  } catch (e) {
    if (cold) error.value = errText(e, '加载失败')
  } finally {
    loading.value = false
  }
}

// 每次进入首页：重抽一批回忆（满足 「每次进入可刷新」）+ 静默刷新数据
onActivated(() => {
  refreshMemories()
  void load()
})
</script>

<template>
  <PageLayout :title="space?.name || '我们的空间'" actions>
      <SkeletonState v-if="loading" variant="hero" />
      <ErrorState v-else-if="error" :error="error" :retry="load" />
      <template v-else>
        <!-- 1. 关系头卡：渐变底 + 叠头像 + 昵称 + 超大天数 + 下一纪念日 -->
        <div class="hero">
          <div class="hero-top">
            <div class="hero-couple">
              <div class="avatar him">
                <img v-if="space?.members?.[1]?.avatar" :src="fullUrl(space.members[1].avatar)" alt="" />
                <template v-else>{{ space?.members?.[1]?.nickname?.slice(0, 1) || '你' }}</template>
              </div>
              <div class="avatar her">
                <img v-if="space?.members?.[0]?.avatar" :src="fullUrl(space.members[0].avatar)" alt="" />
                <template v-else>{{ space?.members?.[0]?.nickname?.slice(0, 1) || '我' }}</template>
              </div>
            </div>
            <div class="hero-names">{{ coupleNames }}</div>
          </div>
          <!-- 天数为 null（未设开始日 / 日期不可解析 / 开始日在未来）时走下面的未设定文案，
               不渲染「null 天」——服务端给不出有意义的天数就不给数字 -->
          <template v-if="space?.love_start_date && space?.days_together != null">
            <div class="hero-days">
              <span class="num">{{ space.days_together }}</span>
              <span class="unit">天</span>
            </div>
            <div class="hero-sub">
              在一起 · {{ space.love_start_date.replace(/-/g, '.') }} 开始
            </div>
          </template>
 <!-- 未设恋爱日期（空间信息由后台维护，文案不指 APP 内入口） -->
          <div v-else class="hero-days-unset">在一起的日子，从定下的那天开始数</div>
          <div v-if="nextAnniv" class="hero-next" @click="router.push('/anniversaries')">
            <Cake :size="14" stroke-width="2" />
            <span>下一个纪念日：{{ nextAnniv.name }}</span>
            <b>{{ nextAnniv.days_until }} 天后</b>
          </div>
        </div>

        <!-- 2. 「即将发生」并排双卡 -->
        <div v-if="upcoming.length" class="upcoming-grid">
          <div
            v-for="u in upcoming"
            :key="u.key"
            class="upcoming-card"
            :class="u.icon"
            @click="router.push(u.to)"
          >
            <span class="u-ic">
              <CalendarDays :size="18" stroke-width="2" />
            </span>
            <span class="u-name">{{ u.title }}</span>
            <span class="u-sub">{{ u.sub }}</span>
          </div>
        </div>

        <!-- 空态：仅全新部署（无动态+无纪念日+无相册）显示；与其余页统一走 EmptyState -->
        <EmptyState v-if="totallyEmpty" :icon="Heart" title="回忆从今天开始" sub="写下第一条，往后每个今天都值得回看。" action="写第一条" @action="router.push('/publish')" />

        <template v-else>
          <!-- 3. 回忆精选：杂志主图 + 紧凑文字回忆 -->
          <div class="section-title">
            <h2>回忆精选</h2>
            <div class="row-actions">
              <button class="btn-refresh" aria-label="换一批" title="换一批" @click="refreshMemories">
                <RefreshCw :size="15" stroke-width="2" />
              </button>
              <a class="more" @click="router.push('/posts')">更多<ChevronRight :size="14" stroke-width="2" /></a>
            </div>
          </div>

          <!-- 杂志封面主图卡 -->
          <div v-if="coverMemory" class="cover-card" @click="router.push(`/posts?focus=${coverMemory.id}`)">
            <div class="cover-media">
              <img :src="mediaSrc(coverMemory.media[0], 'thumb')" loading="lazy" alt="" />
              <div class="cover-mask">
                <div class="cover-tags">
                  <span v-if="coverMemory.mood" class="cover-tag"><Sparkles :size="12" stroke-width="2" />{{ coverMemory.mood }}</span>
                  <span v-if="coverMemory.location_name" class="cover-tag"><MapPin :size="12" stroke-width="2" />{{ coverMemory.location_name }}</span>
                </div>
                <b class="cover-title">{{ memoryTitle(coverMemory) }}</b>
                <span class="cover-date">{{ coverMemory.created_at.slice(0, 10) }}</span>
              </div>
            </div>
          </div>

          <!-- 文字回忆卡 -->
          <div v-for="m in textMemories" :key="m.id" class="text-card" @click="router.push(`/posts?focus=${m.id}`)">
            <span class="tc-bar"></span>
            <div class="tc-body">
              <p>{{ m.content || '一条回忆' }}</p>
              <div class="tc-meta">
                <span>{{ m.created_at.slice(0, 10) }}</span>
                <span v-if="m.mood" class="tc-mood">· {{ m.mood }}</span>
                <span v-if="m.location_name" class="tc-loc">· {{ m.location_name }}</span>
              </div>
            </div>
          </div>
        </template>
      </template>
  </PageLayout>
</template>

<style scoped>
/* 1. 关系头卡：柔和渐变底 + 叠头像 + 超大天数 */
.hero {
  position: relative;
  overflow: hidden;
  border-radius: 20px;
  padding: 24px 22px 20px;
  margin-bottom: 16px;
  background: linear-gradient(150deg, rgba(255, 143, 171, 0.18), rgba(255, 143, 171, 0.04) 55%, transparent);
  box-shadow: var(--shadow);
  border: 1px solid var(--rule);
}
:root[data-theme='dark'] .hero {
  background: linear-gradient(150deg, rgba(255, 143, 171, 0.16), rgba(255, 143, 171, 0.03) 55%, transparent);
}
.hero-top {
  display: flex;
  align-items: center;
  gap: 16px;
}
.hero-couple {
  display: flex;
  flex-shrink: 0;
}
.avatar {
  width: 52px;
  height: 52px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  font-size: 19px;
  font-weight: 600;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.16);
  border: 2.5px solid rgba(255, 255, 255, 0.85);
  overflow: hidden;
}
.avatar img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
:root[data-theme='dark'] .avatar {
  border-color: rgba(28, 28, 30, 0.9);
}
.avatar.her {
  background: var(--primary);
}
.avatar.him {
  background: var(--sem-blue);
}
.hero-couple .avatar.him {
  margin-left: -14px;
  z-index: 1;
  position: relative;
}
.hero-couple .avatar.her {
  position: relative;
  z-index: 2;
}
.hero-names {
  font-size: 17px;
  font-weight: 600;
  color: var(--ink);
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.hero-days {
  display: flex;
  align-items: baseline;
  gap: 6px;
  margin-top: 18px;
}
.hero-days .num {
  font-size: 52px;
  font-weight: 800;
  letter-spacing: -0.03em;
  line-height: 1;
  color: var(--ink);
  font-variant-numeric: tabular-nums;
}
.hero-days .unit {
  font-size: 16px;
  color: var(--muted-ink);
  font-weight: 500;
}
.hero-sub {
  font-size: 13px;
  color: var(--muted-ink);
  margin-top: 6px;
}
.hero-days-unset {
  font-size: 14px;
  color: var(--muted-ink);
  margin-top: 10px;
}
.hero-next {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  margin-top: 16px;
  padding: 8px 14px;
  border-radius: 999px;
  background: var(--surface);
  box-shadow: var(--shadow);
  font-size: 13px;
  color: var(--muted-ink);
  cursor: pointer;
}
.hero-next svg {
  color: var(--primary-deep);
}
.hero-next b {
  color: var(--primary-deep);
  font-weight: 700;
}
.hero-next:active {
  opacity: 0.8;
}

/* 2. 「即将发生」单卡整宽（仅「那年今日」；纪念日已内联头卡） */
.upcoming-grid {
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-bottom: 16px;
}
.upcoming-card {
  display: flex;
  flex-direction: row;
  align-items: center;
  gap: 12px;
  padding: 14px 16px;
  border-radius: 16px;
  background: var(--surface);
  border: 1px solid var(--rule);
  box-shadow: var(--shadow);
  cursor: pointer;
}
.upcoming-card:active {
  opacity: 0.85;
}
.u-ic {
  width: 36px;
  height: 36px;
  min-width: 36px;
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
 /* 着色点（非交互）→ 暖粉，与 「暖粉只用于着色点/FAB/选中」一致；
     操作类文字/图标控件才用 --tint（蓝） */
  background: rgba(255, 143, 171, 0.18);
  color: var(--primary-deep);
}
:root[data-theme='dark'] .u-ic {
  background: rgba(255, 143, 171, 0.26);
}
.u-name {
  font-size: 15px;
  font-weight: 700;
  color: var(--ink);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 100%;
  flex: 1;
}
.u-sub {
  font-size: 12px;
  color: var(--muted-ink);
  flex-shrink: 0;
}

/* 区块标题 */
.section-title {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin: 4px 0 12px;
}
.section-title h2 {
  font-size: 17px;
  font-weight: 700;
}
.more {
  font-size: 13px;
  color: var(--muted-ink);
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 1px;
}


/* 3. 回忆精选：杂志主图卡 + 文字回忆卡 */
.row-actions {
  display: flex;
  align-items: center;
  gap: 10px;
}
.btn-refresh {
  border: none;
  background: transparent;
  color: var(--primary-deep);
  padding: 4px;
  border-radius: 50%;
  display: inline-flex;
  cursor: pointer;
}
.btn-refresh:active {
  transform: rotate(-90deg);
  transition: transform 0.15s;
}
.cover-card {
  border-radius: 20px;
  overflow: hidden;
  background: var(--surface);
  border: 1px solid var(--rule);
  box-shadow: var(--shadow);
  cursor: pointer;
  margin-bottom: 14px;
}
.cover-card:active {
  opacity: 0.92;
}
.cover-media {
  position: relative;
  width: 100%;
  aspect-ratio: 16 / 10;
}
.cover-media img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}
.cover-mask {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  justify-content: flex-end;
  gap: 4px;
  padding: 16px;
  background: linear-gradient(to top, rgba(0, 0, 0, 0.78), rgba(0, 0, 0, 0.15) 58%, transparent 82%);
}
.cover-tags {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 4px;
}
.cover-tag {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  font-size: 11px;
  color: #fff;
  padding: 3px 8px;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.22);
  backdrop-filter: blur(6px);
  -webkit-backdrop-filter: blur(6px);
}
.cover-title {
  color: #fff;
  font-size: 19px;
  font-weight: 700;
  letter-spacing: -0.01em;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  text-shadow: 0 1px 3px rgba(0, 0, 0, 0.5);
}
.cover-date {
  color: rgba(255, 255, 255, 0.88);
  font-size: 12px;
}
.text-card {
  display: flex;
  gap: 12px;
  padding: 14px 16px;
  border-radius: 16px;
  background: var(--surface);
  border: 1px solid var(--rule);
  box-shadow: var(--shadow);
  cursor: pointer;
  margin-bottom: 10px;
}
.text-card:active {
  opacity: 0.9;
}
.tc-bar {
  width: 3px;
  align-self: stretch;
  border-radius: 2px;
  background: var(--primary);
  flex-shrink: 0;
}
.tc-body {
  flex: 1;
  min-width: 0;
}
.tc-body p {
  margin: 0;
  font-size: 15px;
  line-height: 1.5;
  color: var(--ink);
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.tc-meta {
  display: flex;
  align-items: center;
  gap: 0;
  margin-top: 6px;
  font-size: 12px;
  color: var(--muted-ink);
  overflow: hidden;
  white-space: nowrap;
}
.tc-mood,
.tc-loc {
  opacity: 0.85;
}
</style>
