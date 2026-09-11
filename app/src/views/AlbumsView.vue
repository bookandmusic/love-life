<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Images, Music, Play, Check, FolderPlus } from 'lucide-vue-next'
import { api, errText, fullUrl } from '../api/client'
import AppToast from '../components/AppToast.vue'
import { mediaUrl } from '../utils/mediaUrls'
import { groupByMonth } from '../utils/dates'
import { useViewer } from '../composables/useViewer'
import { useLongPress } from '../composables/useLongPress'
import { KEYS, onceHint } from '../utils/storage'
import { RECYCLE_CONFIRM, RECYCLE_TOAST, RECYCLE_TITLE, recycleMsg } from '../utils/deleteCopy'
import ConfirmSheet from '../components/ConfirmSheet.vue'
import MediaViewer from '../components/MediaViewer.vue'
import NavActions from '../components/NavActions.vue'
import SkeletonState from '../components/SkeletonState.vue'
import ErrorState from '../components/ErrorState.vue'
import Sheet from '../components/Sheet.vue'

interface AlbumItem {
  id: number
  type: string
  name: string
  is_hidden: boolean
  item_count: number
  cover_url?: string
}

interface TimelineItem {
  id: number
  post_id: number
  kind: string
  thumb_url: string
  url?: string
  taken_at: string
}

const router = useRouter()
const route = useRoute()
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
function isSelected(id: number) {
  return selectedIds.value.includes(id)
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
  return recycleMsg(a.name, extra)
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

// FAB「相册」带 ?new=1 时自动弹创建 sheet（watch 支持停留本页时重复触发）
onMounted(() => {
  loadAlbums()
  loadTimeline()
  if (route.query.new === '1') showCreate.value = true
  // 长按是隐形手势：首次进入提示一次（本地记住）
  else if (onceHint(KEYS.HINT_LONGPRESS)) toast.value = '长按相册可移入回收站'
})
watch(
  () => route.query.new,
  (v) => {
    if (v === '1') {
      createName.value = ''
      createDesc.value = ''
      showCreate.value = true
    }
  },
)

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
const timelineGroups = computed(() => groupByMonth(timeline.value, (m) => m.taken_at))

async function loadAlbums() {
  try {
    const res = await api<{ items: AlbumItem[] }>('/api/v1/albums')
    albums.value = res.items
  } catch (e) {
    error.value = errText(e, '加载失败')
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
  tlLoading.value = true
  error.value = ''
  try {
    const res = await api<{ items: TimelineItem[]; total: number; has_more: boolean }>(
      `/api/v1/media/timeline?page=1&size=${TL_SIZE}`,
    )
    timeline.value = res.items
    tlTotal.value = res.total ?? res.items.length
    tlHasMore.value = res.has_more ?? false
    tlPage.value = 1
  } catch (e) {
    error.value = errText(e, '加载失败')
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
      <div v-if="view === 'timeline' && !error" class="timeline-wrap">
        <SkeletonState v-if="tlLoading" variant="lines" :rows="4" />
        <p v-else-if="timeline.length === 0" class="empty">
          <Images :size="32" stroke-width="1.2" class="empty-ic" />
          <span>还没有照片</span>
        </p>
        <template v-else>
          <div v-if="!selectMode" class="tl-toolbar">
            <span class="tl-count">共 {{ tlTotal || timeline.length }} 个媒体</span>
            <button class="tl-sel" @click="toggleSelectMode">选择</button>
          </div>
          <div v-else class="tl-toolbar sel">
            <span class="tl-count">已选 {{ selectedIds.length }} 个</span>
            <button class="tl-sel ghost" @click="toggleSelectMode">取消</button>
            <button class="tl-sel go" :disabled="selectedIds.length === 0" @click="openPickAlbum">加入相册</button>
          </div>
          <div v-for="g in timelineGroups" :key="g.key" class="month-block">
            <div class="month-label">{{ g.label }}</div>
            <div class="photo-grid">
              <div
                v-for="(m, i) in g.items"
                :key="m.id"
                class="ph-wrap"
                :class="{ selected: isSelected(m.id) }"
                @click="selectMode ? toggleSelect(m.id) : openViewer(i)"
              >
                <img :src="fullUrl(mediaUrl(m).thumb)" loading="lazy" class="ph" alt="" />
                <span v-if="selectMode" class="pick-mark" :class="{ on: isSelected(m.id) }">
                  <Check v-if="isSelected(m.id)" :size="13" stroke-width="3" />
                </span>
                <span v-if="m.kind === 'video'" class="video-mark">
                  <Play :size="9" fill="currentColor" />
                </span>
                <span v-if="m.kind === 'audio'" class="audio-mark">
                  <Music :size="9" stroke-width="2.5" />
                </span>
              </div>
            </div>
          </div>
          <p v-if="tlLoadingMore" class="tl-foot">加载中…</p>
          <p v-else-if="!tlHasMore" class="tl-foot">没有更多了</p>
        </template>
      </div>

      <div v-else class="album-collection">
        <p v-if="albums.length === 0" class="empty">还没有相册</p>
        <div
          v-for="a in albums"
          :key="a.id"
          class="album-col pressable"
          :class="{ hidden: a.is_hidden, pressing: pressing === a.id }"
          @pointerdown="pressDown($event, a.id, () => askDeleteAlbum(a))"
          @pointermove="pressMove"
          @pointerup="pressUp"
          @pointerleave="pressUp"
          @pointercancel="pressUp"
          @click="onAlbumClick(a, $event)"
        >
          <div class="ac-cover-wrap">
            <img v-if="a.cover_url" :src="fullUrl(a.cover_url)" class="ac-cover" alt="" />
            <div v-else class="ac-cover ac-ph"><Images :size="22" stroke-width="1.6" /></div>
            <span class="cnt">{{ a.item_count }}</span>
          </div>
          <div class="ac-meta">
            <b>{{ a.name }}</b>
            <!-- E8：相册类型按契约渲染（type=monthly→月度相册 / manual→手动相册），不再硬编码 -->
            <span>{{ a.type === 'monthly' ? '月度相册' : '手动相册' }}</span>
          </div>
        </div>
      </div>
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

    <!-- C2 手动相册创建 sheet -->
    <Sheet
      v-model:show="showCreate"
      title="创建手动相册"
      confirm-text="创建"
      :confirm-disabled="!createName.trim()"
      @confirm="doCreate"
    >
      <input
        v-model="createName"
        class="create-input"
        placeholder="相册名称"
        maxlength="20"
        @keyup.enter="doCreate"
      />
      <input
        v-model="createDesc"
        class="create-input"
        placeholder="描述（选填）"
        maxlength="50"
      />
    </Sheet>

    <!-- F2 加入相册（时间轴多选） -->
    <Sheet v-model:show="showPickAlbum" title="加入哪个相册？" confirm-text="关闭">
      <div v-if="albums.filter((a) => !a.is_hidden).length" class="pk-list">
        <button
          v-for="a in albums.filter((x) => !x.is_hidden)"
          :key="a.id"
          class="pk-row"
          :disabled="addingAlbum"
          @click="addToAlbum(a.id)"
        >
          <span class="pk-ic"><Images :size="16" stroke-width="1.8" /></span>
          <span class="pk-name">{{ a.name }}</span>
          <b class="pk-add">加入</b>
        </button>
      </div>
      <p v-else class="pk-empty">还没有手动相册，先新建一个</p>

      <div class="pk-new">
        <input v-model="pickNewName" class="pk-new-input" placeholder="或新建相册并加入" maxlength="20" @keyup.enter="createAndAdd" />
        <button class="pk-new-go" :disabled="addingAlbum || !pickNewName.trim()" @click="createAndAdd">
          <FolderPlus :size="16" stroke-width="2" />
        </button>
      </div>
      <p v-if="pickErr" class="pk-err">{{ pickErr }}</p>
    </Sheet>

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

.photo-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(110px, 1fr));
  gap: 3px;
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
  margin-top: 0;
}
.ph-wrap {
  position: relative;
  cursor: pointer;
}
.ph {
  width: 100%;
  aspect-ratio: 1;
  object-fit: cover;
  display: block;
  background: var(--surface);
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

.album-collection {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: 12px;
}
.album-col {
  background: var(--surface);
  border-radius: 12px;
  overflow: hidden;
  box-shadow: var(--shadow);
  position: relative;
}
.album-col.hidden {
  opacity: 0.55;
}
.ac-cover-wrap {
  position: relative;
}
.ac-cover,
.ac-ph {
  width: 100%;
  aspect-ratio: 4/3;
  object-fit: cover;
  display: block;
}
.ac-ph {
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(255, 143, 171, 0.14);
  color: var(--primary-deep);
}
:root[data-theme='dark'] .ac-ph {
  background: rgba(255, 143, 171, 0.22);
}
.cnt {
  position: absolute;
  right: 6px;
  bottom: 6px;
  background: rgba(0, 0, 0, 0.5);
  color: #fff;
  font-size: 11px;
  border-radius: 6px;
  padding: 1px 6px;
}
.ac-meta {
  padding: 10px 12px;
}
.ac-meta b {
  font-size: 14px;
  display: block;
}
.ac-meta span {
  font-size: 11px;
  color: var(--muted-ink);
}
.empty {
  text-align: center;
  color: var(--muted-ink);
  padding: 40px 0;
  grid-column: 1 / -1;
}
.empty {
  display: flex;
  flex-direction: column;
  align-items: center;
}
.empty-ic {
  opacity: 0.6;
  margin-bottom: 10px;
}
.create-input {
  width: 100%;
  border: 1px solid var(--rule);
  border-radius: 10px;
  padding: 10px 12px;
  font-size: 14px;
  background: var(--bg, #fff);
  color: var(--ink, #222);
  outline: none;
}
.create-input + .create-input { margin-top: 8px; }
.create-input:focus { border-color: var(--primary, #ff8fab); }

/* F2 时间轴多选工具栏 */
.tl-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin: 4px 0 10px;
}
.tl-toolbar.sel { justify-content: flex-end; }
.tl-count { font-size: 13px; color: var(--muted-ink); margin-right: auto; }
.tl-sel {
  border: none;
  border-radius: 999px;
  padding: 6px 14px;
  font-size: 13px;
  font-weight: 600;
  background: var(--primary);
  color: #fff;
  cursor: pointer;
}
.tl-sel.ghost {
  background: none;
  color: var(--primary-deep);
  border: 1px solid var(--rule);
}
.tl-sel.go { background: var(--primary); }
.tl-sel.go:disabled { opacity: 0.4; }
.ph-wrap.selected .ph { outline: 2px solid var(--primary); }
.pick-mark {
  position: absolute;
  top: 6px;
  right: 6px;
  width: 22px;
  height: 22px;
  border-radius: 50%;
  border: 2px solid #fff;
  background: rgba(0, 0, 0, 0.35);
  display: flex;
  align-items: center;
  justify-content: center;
  color: transparent;
  z-index: 1;
}
.pick-mark.on { background: var(--primary); color: #fff; }

/* F2 加入相册 sheet */
.pk-list { display: flex; flex-direction: column; gap: 6px; max-height: 40vh; overflow-y: auto; }
.pk-row {
  display: flex;
  align-items: center;
  gap: 10px;
  border: none;
  background: var(--bg, #f5f5f7);
  border-radius: 10px;
  padding: 10px 12px;
  font-size: 14px;
  color: var(--ink);
  cursor: pointer;
  text-align: left;
}
.pk-row:disabled { opacity: 0.5; }
.pk-ic { color: var(--primary-deep); display: flex; }
.pk-name { flex: 1; }
.pk-add { color: var(--primary-deep); font-weight: 600; font-size: 13px; }
.pk-empty { font-size: 13px; color: var(--muted-ink); text-align: center; padding: 8px 0; }
.pk-new { display: flex; gap: 6px; margin-top: 4px; }
.pk-new-input {
  flex: 1;
  border: 1px solid var(--rule);
  border-radius: 10px;
  padding: 9px 12px;
  font-size: 14px;
  background: var(--bg, #fff);
  color: var(--ink);
  outline: none;
}
.pk-new-go {
  width: 42px;
  border: none;
  border-radius: 10px;
  background: var(--primary);
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
}
.pk-new-go:disabled { opacity: 0.4; }
.pk-err { color: var(--danger); font-size: 13px; }
.tl-foot {
  text-align: center;
  color: var(--muted-ink);
  font-size: 13px;
  padding: 16px 0 4px;
}
</style>
