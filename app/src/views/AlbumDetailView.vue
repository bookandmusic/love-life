<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ChevronLeft, Music, Play, Plus, Trash2 } from 'lucide-vue-next'
import { api, fullUrl } from '../api/client'
import ConfirmSheet from '../components/ConfirmSheet.vue'
import MediaViewer from '../components/MediaViewer.vue'
import SkeletonState from '../components/SkeletonState.vue'

const route = useRoute()
const router = useRouter()
const albumID = String(route.params.id)
const albumIdNum = Number(albumID)

interface MediaItem {
  id: number
  kind: string
  url: string
  thumb_url: string
}

interface TimelineItem {
  id: number
  kind: string
  thumb_url: string
  taken_at?: string
  album_id: number | null
  album_name?: string
}

const album = ref<{ name: string; type: string } | null>(null)
const media = ref<MediaItem[]>([])
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const res = await api<{ album: { name: string; type: string }; media: MediaItem[] }>(`/api/v1/albums/${albumID}`)
    album.value = res.album
    media.value = res.media
  } catch (e) {
    album.value = null
    media.value = []
    actionError.value = e instanceof Error ? e.message : '加载失败'
  } finally {
    loading.value = false
  }
}

// C3 移出照片 sheet
const showRemovePhoto = ref(false)
const removingPhoto = ref<MediaItem | null>(null)
// 全屏查看器（iOS）
const viewer = ref<{ index: number } | null>(null)
let longPressFired = false
let pressedIndex = 0
// B9 删除相册 sheet
const showDeleteAlbum = ref(false)
// B9 错误 toast
const actionError = ref('')

// —— 时间轴选择页（与相册时间轴 tab 一致） ——
const showPick = ref(false)
const pickLoading = ref(false)
const allTimeline = ref<TimelineItem[]>([])
const picked = ref<number[]>([])

function monthKey(m: TimelineItem): string {
  return (m.taken_at || '').slice(0, 7) || 'unknown'
}
function monthLabel(key: string): string {
  if (key === 'unknown') return '其他'
  const [y, mo] = key.split('-')
  return `${y}年${mo ? Number(mo) : ''}月`
}
const pickGroups = computed(() => {
  const groups = new Map<string, TimelineItem[]>()
  for (const m of allTimeline.value) {
    const k = monthKey(m)
    if (!groups.has(k)) groups.set(k, [])
    groups.get(k)!.push(m)
  }
  return [...groups.entries()]
    .sort((a, b) => (a[0] === 'unknown' ? 1 : b[0] === 'unknown' ? -1 : b[0].localeCompare(a[0])))
    .map(([key, items]) => ({ key, label: monthLabel(key), items }))
})

function isLocked(m: TimelineItem): boolean {
  return m.album_id != null
}

async function openPick() {
  showPick.value = true
  picked.value = []
  if (allTimeline.value.length === 0) {
    pickLoading.value = true
    try {
      const items: TimelineItem[] = []
      let page = 1
      // 选择场景数据量小：全量拉取保证跨月可见（mock size 上限 50）
      for (;;) {
        const res = await api<{ items: TimelineItem[]; has_more: boolean }>(`/api/v1/media/timeline?page=${page}&size=50`)
        items.push(...res.items)
        if (!res.has_more || res.items.length === 0) break
        page += 1
      }
      allTimeline.value = items
    } catch {
      allTimeline.value = []
    } finally {
      pickLoading.value = false
    }
  }
}

function closePick() {
  showPick.value = false
  picked.value = []
}

function togglePick(id: number) {
  const i = picked.value.indexOf(id)
  if (i >= 0) picked.value.splice(i, 1)
  else picked.value.push(id)
}

async function confirmAdd() {
  if (picked.value.length === 0) return
  try {
    await api(`/api/v1/albums/${albumID}/photos`, {
      method: 'POST',
      body: JSON.stringify({ media_ids: picked.value }),
    })
    closePick()
    await load()
  } catch (e) {
    actionError.value = e instanceof Error ? e.message : '添加失败'
  }
}

// C3 长按照片弹「移出相册」；短按（<500ms）打开全屏查看器
let longPressTimer: number | null = null
function onPhotoPressStart(m: MediaItem, index: number) {
  longPressFired = false
  pressedIndex = index
  if (album.value?.type !== 'manual') return
  longPressTimer = window.setTimeout(() => {
    longPressFired = true
    removingPhoto.value = m
    showRemovePhoto.value = true
  }, 500)
}
function onPhotoPressEnd() {
  if (longPressTimer !== null) {
    clearTimeout(longPressTimer)
    longPressTimer = null
  }
  if (!longPressFired) {
    viewer.value = { index: pressedIndex }
  }
}
function onViewerRemove(id: string | number) {
  viewer.value = null
  const m = media.value.find((x) => String(x.id) === String(id))
  if (m) {
    removingPhoto.value = m
    showRemovePhoto.value = true
  }
}

async function confirmRemovePhoto() {
  if (!removingPhoto.value) return
  const mid = removingPhoto.value.id
  try {
    await api(`/api/v1/albums/${albumID}/photos`, {
      method: 'DELETE',
      body: JSON.stringify({ media_ids: [mid] }),
    })
    media.value = media.value.filter((m) => m.id !== mid)
  } catch (e) {
    actionError.value = e instanceof Error ? e.message : '移出失败'
  } finally {
    removingPhoto.value = null
  }
}

function askDeleteAlbum() { showDeleteAlbum.value = true }
async function confirmDeleteAlbum() {
  try {
    await api(`/api/v1/albums/${albumID}`, { method: 'DELETE' })
    router.replace('/albums')
  } catch (e) {
    actionError.value = e instanceof Error ? e.message : '删除失败'
  }
}

onMounted(load)
</script>

<template>
  <section class="detail">
    <header class="navbar glass">
      <button class="nav-btn" aria-label="返回" @click="router.back()">
        <ChevronLeft :size="22" stroke-width="2.5" />
      </button>
      <span class="title">{{ album?.name ?? '…' }}</span>
      <div class="nav-right">
        <button v-if="album?.type === 'manual'" class="nav-btn danger" aria-label="删除" @click="askDeleteAlbum">
          <Trash2 :size="16" stroke-width="1.8" />
        </button>
      </div>
    </header>

    <div class="body">
      <SkeletonState v-if="loading" variant="grid" :rows="6" />
      <div v-else class="grid">
        <p v-if="media.length === 0" class="empty">相册还是空的</p>
        <template v-for="(m, index) in media" :key="m.id">
          <div class="thumb-cell"
            @pointerdown="onPhotoPressStart(m, index)"
            @pointerup="onPhotoPressEnd"
            @pointerleave="onPhotoPressEnd"
            @pointercancel="onPhotoPressEnd"
          >
            <video v-if="m.kind === 'video'" class="thumb" controls playsinline :poster="fullUrl(m.thumb_url)" preload="none">
              <source :src="fullUrl(m.url)" />
            </video>
            <div v-else-if="m.kind === 'audio'" class="thumb audio-thumb">
              <Music :size="28" stroke-width="1.6" />
              <audio controls preload="none" :src="fullUrl(m.url)" />
            </div>
            <img v-else :src="fullUrl(m.thumb_url)" class="thumb" loading="lazy" alt="" />
          </div>
        </template>
        <!-- 添加入口：做成最后一个图片图标（仅手动相册） -->
        <button v-if="album?.type === 'manual'" class="add-cell" aria-label="从时间轴添加照片" @click="openPick">
          <Plus :size="26" stroke-width="1.8" />
        </button>
      </div>
    </div>
    <!-- 全屏媒体查看器（iOS） -->
    <MediaViewer
      v-if="viewer"
      :items="media"
      :start="viewer.index"
      mode="album"
      :album-name="album?.name"
      @close="viewer = null"
      @remove="onViewerRemove"
    />

    <!-- C3 移出照片 sheet -->
    <ConfirmSheet
      v-if="removingPhoto"
      v-model:show="showRemovePhoto"
      title="移出当前相册"
      :message="`确定将这张照片从「${album?.name ?? ''}」中移出？照片本身不会删除`"
      confirm-text="移出"
      :confirm-danger="true"
      @confirm="confirmRemovePhoto"
    />
    <!-- B9 删除相册 sheet -->
    <ConfirmSheet
      v-model:show="showDeleteAlbum"
      title="删除相册"
      message="相册将删除，照片本身不受影响（回到未归属状态）"
      confirm-text="删除"
      :confirm-danger="true"
      @confirm="confirmDeleteAlbum"
    />
    <!-- B9 错误 toast -->
    <Transition name="fade">
      <div v-if="actionError" class="toast" @click="actionError = ''">{{ actionError }}</div>
    </Transition>

    <!-- 选择页：与相册时间轴 tab 视觉一致（按月分组） -->
    <div v-if="showPick" class="pick-overlay">
      <header class="pick-header">
        <button class="nav-btn" @click="closePick">取消</button>
        <span class="pick-title">从时间轴添加</span>
        <span class="pick-count">已选 {{ picked.length }}</span>
      </header>
      <div class="pick-scroll">
        <p v-if="pickLoading" class="empty">加载中…</p>
        <p v-else-if="allTimeline.length === 0" class="empty">时间轴里还没有媒体</p>
        <template v-else>
          <div v-for="g in pickGroups" :key="g.key" class="month-block">
            <div class="month-label">{{ g.label }}</div>
            <div class="photo-grid">
              <div
                v-for="m in g.items"
                :key="m.id"
                class="ph-wrap"
                :title="isLocked(m) ? (m.album_id === albumIdNum ? '已在本相册' : (m.album_name ? '已归属「' + m.album_name + '」' : '已归属其他相册')) : ''"
              >
                <img
                  :src="fullUrl(m.thumb_url)"
                  class="ph"
                  :class="{ picked: picked.includes(m.id) }"
                  loading="lazy"
                  alt=""
                  @click="isLocked(m) ? undefined : togglePick(m.id)"
                />
                <span v-if="m.kind === 'video'" class="video-mark"><Play :size="9" fill="currentColor" /></span>
                <span v-else-if="m.kind === 'audio'" class="audio-mark"><Music :size="9" stroke-width="2.5" /></span>
                <span v-if="isLocked(m)" class="owned-tag pick-tag">
                  {{ m.album_id === albumIdNum ? '已在本相册' : (m.album_name || '已入其他相册') }}
                </span>
              </div>
            </div>
          </div>
        </template>
      </div>
      <div class="pick-bar">
        <button class="btn-ghost" @click="closePick">取消</button>
        <button class="btn-go" :disabled="picked.length === 0" @click="confirmAdd">
          加入相册{{ picked.length ? `（${picked.length}）` : '' }}
        </button>
      </div>
    </div>
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
.body {
  flex: 1;
  overflow-y: auto;
  padding: 8px 16px 32px;
}
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(104px, 1fr));
  gap: 3px;
}
.thumb {
  width: 100%;
  aspect-ratio: 1;
  object-fit: cover;
  display: block;
  background: var(--surface);
}
.thumb.audio-thumb {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  background: rgba(90, 167, 212, 0.14);
  color: var(--sem-blue);
  aspect-ratio: auto;
  padding: 8px;
}
.thumb.audio-thumb audio {
  width: 100%;
  height: 32px;
}

/* F2 选图器已归属置灰 */
.ph-wrap {
  position: relative;
}
.ph,
.picked {
  width: 100%;
  aspect-ratio: 1;
  object-fit: cover;
  display: block;
  background: var(--surface);
}

/* 选图器已选高亮 */
.picked {
  outline: 3px solid var(--primary);
  outline-offset: -3px;
}
.owned-tag {
  position: absolute;
  bottom: 2px;
  left: 2px;
  right: 2px;
  background: rgba(0, 0, 0, 0.6);
  color: #fff;
  font-size: 10px;
  padding: 2px 4px;
  border-radius: 3px;
  text-align: center;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.pick-tag {
  left: auto;
  right: auto;
}

/* 添加入口：最后一个图片格 */
.add-cell {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
  aspect-ratio: 1;
  width: 100%;
  color: var(--primary-deep);
  background: var(--surface);
  border: 1px dashed rgba(240, 98, 146, 0.5);
  border-radius: 4px;
  cursor: pointer;
}
.add-cell:active {
  opacity: .8;
}
.video-mark,
.audio-mark {
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
.audio-mark {
  background: rgba(90, 167, 212, 0.75);
}

.empty {
  grid-column: 1 / -1;
  text-align: center;
  color: var(--muted-ink);
  padding: 40px 0;
}
/* C3 长按移出：仅手动相册有交互 */
.thumb-cell {
  position: relative;
  user-select: none;
  -webkit-user-select: none;
}
.thumb-cell:active::after {
  content: '长按可移出相册';
  position: absolute;
  inset: 0;
  background: rgba(0, 0, 0, 0.55);
  color: #fff;
  font-size: 11px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 3px;
  pointer-events: none;
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

/* —— 选择页（与时间轴 tab 一致的全屏层） —— */
.pick-overlay {
  position: fixed;
  inset: 0;
  z-index: 200;
  background: var(--bg);
  display: flex;
  flex-direction: column;
}
.pick-header {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: calc(10px + env(safe-area-inset-top)) 12px 10px;
  border-bottom: 1px solid var(--rule);
}
.pick-title {
  font-size: 16px;
  font-weight: 600;
}
.pick-count {
  color: var(--muted-ink);
  font-size: 13px;
  min-width: 56px;
  text-align: right;
}
.pick-scroll {
  flex: 1;
  overflow-y: auto;
  -webkit-overflow-scrolling: touch;
  padding: 8px 16px 16px;
}
.month-block + .month-block {
  margin-top: 6px;
}
.month-label {
  font-size: 13px;
  font-weight: 600;
  color: var(--muted-ink);
  margin: 14px 0 8px;
}
.month-block:first-child .month-label {
  margin-top: 4px;
}
.photo-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(110px, 1fr));
  gap: 3px;
}
.pick-bar {
  flex-shrink: 0;
  display: flex;
  gap: 10px;
  padding: 10px 16px calc(14px + env(safe-area-inset-bottom));
  border-top: 1px solid var(--rule);
  background: var(--bg);
}
.btn-ghost, .btn-go {
  border: none;
  border-radius: 12px;
  padding: 12px 0;
  font-weight: 600;
  cursor: pointer;
}
.btn-ghost {
  flex: 0 0 96px;
  background: var(--surface);
  color: var(--ios-blue);
}
.btn-go {
  flex: 1;
  background: var(--primary);
  color: var(--on-primary);
}
.btn-go:disabled {
  opacity: 0.45;
}
</style>