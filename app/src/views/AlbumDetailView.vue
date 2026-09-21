<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Images, Music, Play, Plus } from 'lucide-vue-next'
import { api, errText, fullUrl } from '../api/client'
import AppToast from '../components/AppToast.vue'
import { mediaUrl } from '../utils/mediaUrls'
import { useViewer } from '../composables/useViewer'
import { groupByMonth } from '../utils/dates'
import ConfirmSheet from '../components/ConfirmSheet.vue'
import { useLongPress } from '../composables/useLongPress'
import { REMOVE_CONFIRM, REMOVE_TITLE, REMOVE_TOAST, removeMsg } from '../utils/deleteCopy'
import MediaViewer from '../components/MediaViewer.vue'
import PageLayout from '../components/PageLayout.vue'
import SkeletonState from '../components/SkeletonState.vue'
import EmptyState from '../components/EmptyState.vue'

const route = useRoute()
const router = useRouter()
const albumID = computed(() => String(route.params.id))
const albumIdNum = computed(() => Number(albumID.value))

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
    const res = await api<{ album: { name: string; type: string }; media: MediaItem[] }>(`/api/v1/albums/${albumID.value}`)
    album.value = res.album
    media.value = res.media
  } catch (e) {
    album.value = null
    media.value = []
    actionError.value = errText(e, '加载失败')
  } finally {
    loading.value = false
  }
}

// C3 移出照片 sheet
const showRemovePhoto = ref(false)
const removingPhoto = ref<MediaItem | null>(null)
// 全屏查看器（iOS）
const { viewer, openViewer, closeViewer } = useViewer()
// 长按 = 移出当前相册（与其它列表同一套手势：500ms、位移 >8px 取消、按住有反馈）
const { pressing, down: pressDown, move: pressMove, up: pressUp, click: pressClick } = useLongPress()
// 成功提示（移出相册用；错误仍走 actionError 常驻待点关闭）
const toast = ref('')
// B9 错误 toast
const actionError = ref('')

// —— 时间轴选择页（与相册时间轴 tab 一致） ——
const showPick = ref(false)
const pickLoading = ref(false)
const allTimeline = ref<TimelineItem[]>([])
const picked = ref<number[]>([])

// 月分组：与时间轴 tab 共用同一实现（utils/dates.groupByMonth）
const pickGroups = computed(() => groupByMonth(allTimeline.value, (m) => m.taken_at || ''))

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
    await api(`/api/v1/albums/${albumID.value}/photos`, {
      method: 'POST',
      body: JSON.stringify({ media_ids: picked.value }),
    })
    closePick()
    await load()
  } catch (e) {
    actionError.value = errText(e, '添加失败')
  }
}

// 长按 = 移出当前相册（与其它列表同一套手势：500ms、位移 >8px 取消、按住有反馈）
function askRemovePhoto(m: MediaItem) {
  // 只有手动相册能移出（月度相册按月份自动组织，DESIGN §12.2）
  if (album.value?.type !== 'manual') return
  removingPhoto.value = m
  showRemovePhoto.value = true
}
function onPhotoClick(index: number, e: MouseEvent) {
  pressClick(e)
  if (e.defaultPrevented) return // 长按已触发：不再开大图
  openViewer(index)
}
function onViewerRemove(id: string | number) {
  closeViewer()
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
    await api(`/api/v1/albums/${albumID.value}/photos`, {
      method: 'DELETE',
      body: JSON.stringify({ media_ids: [mid] }),
    })
    media.value = media.value.filter((m) => m.id !== mid)
    toast.value = REMOVE_TOAST
  } catch (e) {
    actionError.value = errText(e, '移出失败')
  } finally {
    removingPhoto.value = null
  }
}

// 卸载时清掉可能仍在计时的长按（否则 500ms 后回调会去改已销毁组件的状态）


// 路由参数变化（如相册间跳转）必须重载：此前只在 setup 时取一次，切换后仍是旧数据
watch(() => route.params.id, load)

onMounted(load)
</script>

<template>
  <PageLayout :title="album?.name ?? '…'" back @back="router.back()">
    <div class="body">
      <SkeletonState v-if="loading" variant="grid" :rows="6" />
      <div v-else class="grid">
        <EmptyState v-if="media.length === 0" class="ade-empty" :icon="Images" title="这个相册还空着" sub="添加几张照片，让它慢慢丰富起来。" :action="album?.type === 'manual' ? '添加照片' : undefined" @action="openPick()" />
        <template v-for="(m, index) in media" :key="m.id">
          <div class="thumb-cell pressable"
            :class="{ pressing: pressing === m.id }"
            @pointerdown="pressDown($event, m.id, () => askRemovePhoto(m))"
            @pointermove="pressMove"
            @pointerup="pressUp"
            @pointerleave="pressUp"
            @pointercancel="pressUp"
            @click="onPhotoClick(index, $event)"
          >
            <video v-if="m.kind === 'video'" class="thumb" controls playsinline :poster="fullUrl(mediaUrl(m).thumb)" preload="none" @pointerdown.stop>
              <source :src="fullUrl(mediaUrl(m).raw)" />
            </video>
            <div v-else-if="m.kind === 'audio'" class="thumb audio-thumb">
              <Music :size="28" stroke-width="1.6" />
              <audio controls preload="none" :src="fullUrl(mediaUrl(m).raw)" @pointerdown.stop />
            </div>
            <img v-else :src="fullUrl(mediaUrl(m).thumb)" class="thumb" loading="lazy" alt="" />
          </div>
        </template>
        <!-- 添加入口：做成最后一个图片图标（仅手动相册）；空相册时由 EmptyState 的 CTA 承担，避免重复入口 -->
        <button v-if="album?.type === 'manual' && media.length > 0" class="add-cell" aria-label="从时间轴添加照片" @click="openPick">
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
      @close="closeViewer"
      @remove="onViewerRemove"
    />

    <!-- C3 移出照片 sheet -->
    <ConfirmSheet
      v-if="removingPhoto"
      v-model:show="showRemovePhoto"
      :title="REMOVE_TITLE"
      :message="removeMsg(album?.name ?? '')"
      :confirm-text="REMOVE_CONFIRM"
      @confirm="confirmRemovePhoto"
    />
    <AppToast :text="actionError" :duration="0" @done="actionError = ''" />
    <AppToast :text="toast" @done="toast = ''" />

    <!-- 选择页：复用 PageLayout 外壳（navbar 布局/滚动/列宽与其余页面同源），
         外层 .pick-overlay 只负责全屏/桌面档卡片定位 -->
    <div v-if="showPick" class="pick-overlay">
      <PageLayout title="从时间轴添加" back @back="closePick">
        <template #nav-right>
          <button class="pick-save" :disabled="picked.length === 0" @click="confirmAdd">加入相册{{ picked.length ? `（${picked.length}）` : '' }}</button>
        </template>
        <p v-if="pickLoading" class="pick-hint">加载中…</p>
        <EmptyState v-else-if="allTimeline.length === 0" :icon="Images" title="先去动态里晒点照片吧" sub="发一条带照片的动态，回来就能挑。" action="去发动态" @action="router.push('/publish')" />
        <template v-else>
          <div v-for="g in pickGroups" :key="g.key" class="month-block">
            <div class="month-label">{{ g.label }}</div>
            <div class="photo-grid">
              <div
                v-for="m in g.items"
                :key="m.id"
                class="ph-wrap"
                :title="isLocked(m) ? (m.album_id === albumIdNum ? '已在本相册' : (m.album_name ? '已在「' + m.album_name + '」里' : '已在别的相册里')) : ''"
              >
                <img
                  :src="fullUrl(mediaUrl(m).thumb)"
                  class="ph"
                  :class="{ picked: picked.includes(m.id) }"
                  loading="lazy"
                  alt=""
                  @click="isLocked(m) ? undefined : togglePick(m.id)"
                />
                <span v-if="m.kind === 'video'" class="video-mark"><Play :size="9" fill="currentColor" /></span>
                <span v-else-if="m.kind === 'audio'" class="audio-mark"><Music :size="9" stroke-width="2.5" /></span>
                <span v-if="isLocked(m)" class="owned-tag pick-tag">
                  {{ m.album_id === albumIdNum ? '已在本相册' : (m.album_name || '已在别的相册') }}
                </span>
              </div>
            </div>
          </div>
        </template>
      </PageLayout>
    </div>
  </PageLayout>
</template>

<style scoped>
.body {
  padding: 8px 0 32px;
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

.pick-hint {
  text-align: center;
  color: var(--muted-ink);
  padding: 40px 0;
}
.ade-empty {
  grid-column: 1 / -1;
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
/* —— 选择页（与时间轴 tab 一致的全屏层） —— */
.pick-overlay {
  position: fixed;
  inset: 0;
  z-index: 200;
  background: var(--bg);
  display: flex;
  flex-direction: column;
}
/* 桌面档：选择页收进应用卡片（与外壳/弹窗共用框架令牌） */
@media (min-width: 1280px) {
  .pick-overlay {
    top: var(--frame-inset);
    bottom: var(--frame-inset);
    left: 50%;
    right: auto;
    width: min(var(--frame-max), calc(100% - 2 * var(--frame-inset)));
    transform: translateX(-50%);
    border-radius: var(--frame-radius);
    overflow: hidden;
  }
}
.pick-save {
  border: none;
  background: none;
  color: var(--tint);
  font-size: 15px;
  font-weight: 600;
  cursor: pointer;
  padding: 4px;
}
.pick-save:disabled {
  opacity: 0.4;
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
</style>
