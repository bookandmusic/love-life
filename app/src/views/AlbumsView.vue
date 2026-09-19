<script setup lang="ts">
import { onActivated, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, errText } from '../api/client'
import AppToast from '../components/AppToast.vue'
import { useViewer } from '../composables/useViewer'
import { useLongPress } from '../composables/useLongPress'
import { KEYS, onceHint } from '../utils/storage'
import { RECYCLE_CONFIRM, RECYCLE_TOAST, RECYCLE_TITLE, recycleMsg, peerNotice } from '../utils/deleteCopy'
import { useAuth } from '../stores/auth'
import ConfirmSheet from '../components/ConfirmSheet.vue'
import MediaViewer from '../components/MediaViewer.vue'
import NavActions from '../components/NavActions.vue'
import ErrorState from '../components/ErrorState.vue'
import TimelineGrid from '../components/albums/TimelineGrid.vue'
import AlbumCollection from '../components/albums/AlbumCollection.vue'
import AlbumSheets from '../components/albums/AlbumSheets.vue'
import type { AlbumItem, TimelineItem } from '../types/albums'

defineOptions({ name: 'AlbumsView' })

const router = useRouter()
const route = useRoute()
const auth = useAuth()
const view = ref<'timeline' | 'collection'>('timeline')
// 全屏媒体查看器（iOS）
const { viewer, openViewer, closeViewer } = useViewer()
function onViewerPost(postId: number) {
  closeViewer()
  router.push(`/posts?focus=${postId}`)
}

const albums = ref<AlbumItem[]>([])
const timeline = ref<TimelineItem[]>([])
const error = ref('')
// C2 手动相册创建 sheet
const showCreate = ref(false)
const createName = ref('')
const createDesc = ref('')

// F2 时间轴多选 → 加入手动相册
const selectMode = ref(false)
const selectedIds = ref<number[]>([])
const showPickAlbum = ref(false)
const pickErr = ref('')
const addingAlbum = ref(false)
const pickNewName = ref('')

function toggleSelectMode() {
  selectMode.value = !selectMode.value
  selectedIds.value = []
}
function toggleSelect(id: number) {
  const i = selectedIds.value.indexOf(id)
  if (i >= 0) selectedIds.value.splice(i, 1)
  else selectedIds.value.push(id)
}
function openPickAlbum() {
  pickErr.value = ''
  pickNewName.value = ''
  showPickAlbum.value = true
}
async function addToAlbum(albumId: number) {
  if (addingAlbum.value || selectedIds.value.length === 0) return
  addingAlbum.value = true
  pickErr.value = ''
  try {
    await api(`/api/v1/albums/${albumId}/photos`, {
      method: 'POST',
      body: JSON.stringify({ media_ids: selectedIds.value }),
    })
    showPickAlbum.value = false
    const n = selectedIds.value.length
    selectMode.value = false
    selectedIds.value = []
    await loadAlbums()
    toast.value = `已加入 ${n} 个媒体`
  } catch (e) {
    pickErr.value = errText(e, '添加失败')
  } finally {
    addingAlbum.value = false
  }
}
async function createAndAdd() {
  const name = pickNewName.value.trim()
  if (!name || addingAlbum.value) return
  addingAlbum.value = true
  pickErr.value = ''
  try {
    const a = await api<{ id: number }>('/api/v1/albums', {
      method: 'POST',
      body: JSON.stringify({ name, description: '' }),
    })
    await addToAlbum(a.id)
  } catch (e) {
    pickErr.value = errText(e, '创建失败')
  } finally {
    addingAlbum.value = false
  }
}
const toast = ref('')

// 长按相册卡片 = 移入回收站；短按仍进详情页看照片
const { pressing, down: pressDown, move: pressMove, up: pressUp, click: pressClick } = useLongPress()
const deletingAlbum = ref<AlbumItem | null>(null)
const showDeleteAlbum = ref(false)

function askDeleteAlbum(a: AlbumItem) {
  deletingAlbum.value = a
  showDeleteAlbum.value = true
}
function onAlbumClick(a: AlbumItem, e: MouseEvent) {
  pressClick(e)
  if (e.defaultPrevented) return
  router.push(`/albums/${a.id}`)
}
function albumDeleteMsg(a: AlbumItem) {
  // 空相册没有内容可看，删除后果只有相册本身
  const extra = a.item_count > 0 ? '照片本身不受影响（回到未归属状态）。' : ''
  return recycleMsg(a.name, extra + peerNotice(auth.user?.id, a.created_by))
}
async function confirmDeleteAlbum() {
  if (!deletingAlbum.value) return
  const id = deletingAlbum.value.id
  try {
    await api(`/api/v1/albums/${id}`, { method: 'DELETE' })
    albums.value = albums.value.filter((x) => x.id !== id) // 原地移除，不整表重载
    toast.value = RECYCLE_TOAST
  } catch (e) {
    toast.value = errText(e, '删除失败')
  } finally {
    deletingAlbum.value = null
  }
}

// FAB「相册」带 ?new=1 时自动弹创建 sheet（onActivated 覆盖跨 tab 抵达；watch 覆盖同页重复触发）
onActivated(() => {
  void loadAlbums()
  // 时间轴：冷启动或停在第一页时刷新（新增即时可见）；已翻页则保留浏览位置
  if (timeline.value.length === 0 || tlPage.value === 1) void loadTimeline()
  // 以 query 为真源回写 sheet 开合（修复：经 TabBar 而非关闭离开时，回来不应停在 sheet 态）
  showCreate.value = route.query.new === '1'
  if (!showCreate.value && onceHint(KEYS.HINT_LONGPRESS)) toast.value = '长按相册可移入回收站'
})
watch(
  () => route.query.new,
  (v) => {
    if (v === '1' && route.path === '/albums') {
      createName.value = ''
      createDesc.value = ''
      showCreate.value = true
    }
  },
)

// KeepAlive 下 onMounted 不再每次触发；关闭创建 sheet 时清掉 ?new=1，
// 保证再次点 FAB（push /albums?new=1）能被上面的 watch 识别为「变化」而重新弹出。
watch(showCreate, (v) => {
  if (!v && route.query.new === '1') router.replace('/albums')
})

async function doCreate() {
  const name = createName.value.trim()
  if (!name) return
  try {
    const a = await api<{ id: number }>('/api/v1/albums', {
      method: 'POST',
      body: JSON.stringify({ name, description: createDesc.value.trim() }),
    })
    showCreate.value = false
    createName.value = ''
    createDesc.value = ''
    view.value = 'collection'
    await loadAlbums()
    router.push(`/albums/${a.id}`)
  } catch (e) {
    error.value = errText(e, '创建失败')
  }
}

// 时间轴按月份分组（taken_at 缺失时服务端已回退 created_at；分组规则与相册详情选择页共用）

async function loadAlbums() {
  try {
    const res = await api<{ items: AlbumItem[] }>('/api/v1/albums')
    albums.value = res.items
    error.value = ''
  } catch (e) {
    if (albums.value.length === 0) error.value = errText(e, '加载失败')
  }
}

// —— 时间轴分页加载（阶段5.5 B10：按 1.7⑥ 分页契约滚动加载）——
const tlPage = ref(1)
const tlTotal = ref(0)
const tlHasMore = ref(false)
const tlLoading = ref(false)
const tlLoadingMore = ref(false)
const TL_SIZE = 40

async function loadTimeline() {
  const cold = timeline.value.length === 0
  if (cold) {
    tlLoading.value = true
    error.value = ''
  }
  try {
    const res = await api<{ items: TimelineItem[]; total: number; has_more: boolean }>(
      `/api/v1/media/timeline?page=1&size=${TL_SIZE}`,
    )
    timeline.value = res.items
    tlTotal.value = res.total ?? res.items.length
    tlHasMore.value = res.has_more ?? false
    tlPage.value = 1
    error.value = ''
  } catch (e) {
    if (cold) error.value = errText(e, '加载失败')
  } finally {
    tlLoading.value = false
  }
}

async function loadMoreTimeline() {
  if (tlLoading.value || tlLoadingMore.value || !tlHasMore.value) return
  tlLoadingMore.value = true
  try {
    const next = tlPage.value + 1
    const res = await api<{ items: TimelineItem[]; total: number; has_more: boolean }>(
      `/api/v1/media/timeline?page=${next}&size=${TL_SIZE}`,
    )
    timeline.value.push(...res.items)
    tlTotal.value = res.total ?? timeline.value.length
    tlHasMore.value = res.has_more ?? false
    tlPage.value = next
  } catch (e) {
    // 不打断浏览（下次滚动自动重试），但要给出提示，别把「失败」显示成「没有更多」
    toast.value = errText(e, '加载更多失败')
  } finally {
    tlLoadingMore.value = false
  }
}

const doReload = () => { loadAlbums(); loadTimeline() }
function onTimelineScroll(e: Event) {
  const el = e.target as HTMLElement
  if (view.value !== 'timeline') return
  if (el.scrollTop + el.clientHeight >= el.scrollHeight - 180) void loadMoreTimeline()
}

</script>

<template>
  <div class="albums-page">
    <header class="navbar glass">
      <span class="title">相册</span>
      <NavActions />
    </header>

    <div class="seg">
      <button :class="{ on: view === 'timeline' }" @click="view = 'timeline'">时间轴</button>
      <button :class="{ on: view === 'collection' }" @click="view = 'collection'">集合</button>
    </div>

    <div class="scroll-area" @scroll.passive="onTimelineScroll">
      <!-- 阶段5.1：错误可重试 -->
      <ErrorState v-if="error" :error="error" :retry="doReload" />
      <TimelineGrid
        v-if="view === 'timeline' && !error"
        :items="timeline"
        :loading="tlLoading"
        :loading-more="tlLoadingMore"
        :has-more="tlHasMore"
        :total="tlTotal"
        :select-mode="selectMode"
        :selected-ids="selectedIds"
        @toggle-select-mode="toggleSelectMode"
        @toggle-select="toggleSelect"
        @open-pick-album="openPickAlbum"
        @open-viewer="openViewer"
      />
      <AlbumCollection
        v-else
        :albums="albums"
        :pressing="pressing"
        @pointer-down="pressDown"
        @pointer-move="pressMove"
        @pointer-up="pressUp"
        @open="onAlbumClick"
        @request-delete="askDeleteAlbum"
      />
    </div>
    <!-- 全屏媒体查看器（iOS） -->
    <MediaViewer
      v-if="viewer"
      :items="timeline"
      :start="viewer.index"
      mode="timeline"
      @close="closeViewer"
      @gopost="onViewerPost"
    />

    <AlbumSheets
      v-model:show-create="showCreate"
      v-model:create-name="createName"
      v-model:create-desc="createDesc"
      v-model:show-pick-album="showPickAlbum"
      v-model:pick-new-name="pickNewName"
      :albums="albums"
      :adding-album="addingAlbum"
      :pick-err="pickErr"
      @create="doCreate"
      @add-to-album="addToAlbum"
      @create-and-add="createAndAdd"
    />

    <ConfirmSheet
      v-if="deletingAlbum"
      v-model:show="showDeleteAlbum"
      :title="RECYCLE_TITLE"
      :message="albumDeleteMsg(deletingAlbum)"
      :confirm-text="RECYCLE_CONFIRM"
      :confirm-danger="true"
      @confirm="confirmDeleteAlbum"
    />
    <AppToast :text="toast" bottom="calc(120px + env(safe-area-inset-bottom))" pill @done="toast = ''" />
  </div>
</template>

<style scoped>
.albums-page {
  height: 100%;
  display: flex;
  flex-direction: column;
  min-height: 0;
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
.seg {
  display: flex;
  background: rgba(120, 120, 128, 0.16);
  border-radius: 9px;
  padding: 2px;
  margin: 8px 16px 10px;
  flex-shrink: 0;
}
.seg button {
  flex: 1;
  border: none;
  background: none;
  padding: 7px 0;
  border-radius: 7px;
  font-size: 14px;
  font-weight: 500;
  color: var(--ink);
  cursor: pointer;
}
.seg button.on {
  background: var(--surface);
  box-shadow: var(--shadow);
  font-weight: 600;
}
.scroll-area {
  flex: 1;
  overflow-y: auto;
  -webkit-overflow-scrolling: touch;
  padding: 0 16px 24px;
}
/* F2 时间轴多选工具栏 */
/* F2 加入相册 sheet */
</style>
