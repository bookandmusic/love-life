<script setup lang="ts">
import { computed, nextTick, onActivated, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  ChevronRight, List, Map as MapIcon, MapPin, Search,
} from 'lucide-vue-next'
import { api, errText } from '../api/client'
import AppToast from '../components/AppToast.vue'
import PageLayout from '../components/PageLayout.vue'
import ConfirmSheet from '../components/ConfirmSheet.vue'
import MiniMapPicker from '../components/MiniMapPicker.vue'
import MapCanvas from '../components/MapCanvas.vue'
import EmptyState from '../components/EmptyState.vue'
import { useLongPress } from '../composables/useLongPress'
import { KEYS, onceHint } from '../utils/storage'
import { RECYCLE_CONFIRM, RECYCLE_TOAST, RECYCLE_TITLE, recycleMsg, peerNotice } from '../utils/deleteCopy'
import { useAuth } from '../stores/auth'

defineOptions({ name: 'MapView' })

interface PointItem {
  id: number
  name: string
  lat: number
  lng: number
  visit_date: string | null
  description: string
  source: string
  created_by: number
  created_at: string
}

const route = useRoute()
const router = useRouter()
const auth = useAuth()

// 视图：地图 / 列表
const view = ref<'map' | 'list'>('map')

// 真实点位数据
const points = ref<PointItem[]>([])
const loadingPoints = ref(false)
const loadError = ref('')

// 地图画布（瓦片引擎/手势/投影统一在 MapCanvas 内；父级只管数据与视图切换）
const mapRef = ref<{ focus: (id: number) => void; attachMap: () => void; layout: () => void } | null>(null)
const hlPoint = ref<number | null>(null)

// 无真实位置的点位（lat/lng 均为 0）不落地图标记：会画在赤道正中（Null Island）污染地图
const mappablePoints = computed(() => points.value.filter((p) => p.lat !== 0 || p.lng !== 0))

const saving = ref(false)

// C3 删除点位 sheet
const showDeletePoint = ref(false)
const deletingPoint = ref<PointItem | null>(null)

// ============ FAB「添加地点」完整表单页（名称 + 经纬度 + geocode 候选 + 地图选点） ============
const newForm = ref(false)
const npName = ref('')
const npLat = ref<number | null>(null)
const npLng = ref<number | null>(null)
const npCandidates = ref<Array<{ name: string; lat: number; lng: number; display: string }>>([])
const npSearching = ref(false)
const npErr = ref('')

function closeNewForm() {
  newForm.value = false
  router.replace('/map')
}

async function searchGeoForm() {
  const q = npName.value.trim()
  if (!q) return
  npSearching.value = true
  try {
    const res = await api<{ candidates: Array<{ name: string; lat: number; lng: number; display: string }> }>(
      `/api/v1/geocode?q=${encodeURIComponent(q)}`,
    )
    npCandidates.value = res.candidates ?? []
  } catch (e) {
    npCandidates.value = []
    npErr.value = errText(e, '地点搜索失败，请检查网络后重试')
  } finally {
    npSearching.value = false
  }
}

function pickCandidateForm(c: { name: string; lat: number; lng: number }) {
  npName.value = c.name
  npLat.value = c.lat
  npLng.value = c.lng
  npCandidates.value = []
  npErr.value = ''
}

function onFormMapPick(pos: { lat: number; lng: number }) {
  npLat.value = pos.lat
  npLng.value = pos.lng
  npErr.value = ''
}

async function saveNewPointForm() {
  const name = npName.value.trim()
  if (!name || npLat.value == null || npLng.value == null || saving.value) return
  saving.value = true
  npErr.value = ''
  try {
    await api('/api/v1/map/points', {
      method: 'POST',
      body: JSON.stringify({ name, lat: npLat.value, lng: npLng.value, source: 'manual', visit_date: new Date().toISOString().slice(0, 10) }),
    })
    closeNewForm()
    await loadPoints()
    toast.value = '已添加'
  } catch (e) {
    npErr.value = errText(e, '添加失败')
  } finally {
    saving.value = false
  }
}

// 通用 toast
const toast = ref('')

// 视图切回地图时重挂画布（v-if 会销毁重建容器，MapCanvas 自带 teardown，重复调用安全）
watch(view, () => nextTick(() => mapRef.value?.attachMap()))

onActivated(async () => {
  await loadPoints()
  // FAB「添加地点」→ 打开完整表单页；以 query 为真源回写（修复：经 TabBar 而非 closeForm 离开时，回来不应停在表单态）
  newForm.value = route.query.new === '1'
  void applyFocus()
  // KeepAlive 重挂后重测容器尺寸，避免切走再切回地图空白/错位
  await nextTick()
  mapRef.value?.attachMap()
  // 长按是隐形手势：首次进入提示一次（本地记住）
  if (!toast.value && onceHint(KEYS.HINT_LONGPRESS)) toast.value = '长按足迹可移入回收站'
})

// D3：通知「足迹」→ /map?focus=<id>：切回地图视图并居中高亮该点位
async function applyFocus() {
  const id = Number(route.query.focus)
  if (!id) return
  newForm.value = false
  if (!points.value.length) await loadPoints()
  view.value = 'map'
  await nextTick()
  mapRef.value?.attachMap() // 关闭完整表单后容器会重建，先重挂再居中
  mapRef.value?.focus(id)
}

watch(() => route.query.focus, () => {
  // KeepAlive 下失活组件的 watcher 仍会响应全局路由变化：加路径守卫，避免他 tab 的 ?focus 泄漏到本页
  if (route.path === '/map') void applyFocus()
})

// ?new=1 → 打开完整表单页（FAB「地点」入口；watch 支持停留本页时重复触发）
watch(
  () => route.query.new,
  (v) => {
    if (v === '1' && route.path === '/map') {
      npName.value = ''
      npLat.value = null
      npLng.value = null
      npCandidates.value = []
      npErr.value = ''
      newForm.value = true
    }
  },
)

async function loadPoints() {
  // 静默刷新：仅冷启动（无数据）时置 loading；已有数据时后台重取、失败不打扰旧内容
  const cold = points.value.length === 0
  if (cold) {
    loadingPoints.value = true
    loadError.value = ''
  }
  try {
    const res = await api<{ items: PointItem[] }>('/api/v1/map/points')
    points.value = res.items
  } catch (e) {
    if (cold) loadError.value = errText(e, '加载失败')
  } finally {
    loadingPoints.value = false
  }
}

// 移入回收站：长按列表行触发（行内不再常驻删除图标）；短按仍是"居中该点位"
const { pressing, down: pressDown, move: pressMove, up: pressUp, click: pressClick } = useLongPress()
function onPointClick(p: PointItem, e: MouseEvent) {
  pressClick(e)
  if (e.defaultPrevented) return
  centerOnPoint(p.id)
}

// 居中并高亮某点位（列表点击 / ?focus=<id> 共用）：列表视图下先切回地图再居中，
// 否则地图被列表遮盖、focus 调用到的是隐藏的画布——用户感知「点了没反应」（5b9d798 回归）。
async function centerOnPoint(id: number) {
  hlPoint.value = id
  if (view.value !== 'map') {
    view.value = 'map'
    await nextTick()
    mapRef.value?.attachMap()
  }
  mapRef.value?.focus(id)
}

function askDeletePoint(p: PointItem) {
  deletingPoint.value = p
  showDeletePoint.value = true
}

async function confirmDeletePoint() {
  if (!deletingPoint.value) return
  const pid = deletingPoint.value.id
  try {
    await api(`/api/v1/map/points/${pid}`, { method: 'DELETE' })
    points.value = points.value.filter((x) => x.id !== pid)
    if (hlPoint.value === pid) hlPoint.value = null
    toast.value = RECYCLE_TOAST
  } catch (e) {
    toast.value = errText(e, '删除失败')
  } finally {
    deletingPoint.value = null
  }
}
</script>

<template>
  <!-- 完整表单：添加地点（名称+经纬度+geocode 候选+地图选点） -->
  <PageLayout v-if="newForm" title="添加地点" back @back="closeNewForm">
    <template #nav-right>
      <button class="np-save" :disabled="!npName.trim() || npLat == null || npLng == null || saving" @click="saveNewPointForm">{{ saving ? '保存中…' : '保存' }}</button>
    </template>
        <div class="group form-rows">
          <div class="frow np-search">
            <input v-model="npName" class="cell-input" placeholder="地点名称（必填，如：厦门·鼓浪屿）" maxlength="30" @keyup.enter="searchGeoForm" />
            <button class="np-go" :disabled="npSearching || !npName.trim()" aria-label="搜索" @click="searchGeoForm"><Search :size="16" stroke-width="2" /></button>
          </div>
          <div class="frow np-ll">
            <label>经度<input v-model.number="npLng" type="number" step="any" placeholder="经度" /></label>
            <label>纬度<input v-model.number="npLat" type="number" step="any" placeholder="纬度" /></label>
          </div>
        </div>
        <div v-if="npCandidates.length" class="np-cands">
          <button v-for="c in npCandidates" :key="c.name" class="np-cand" @click="pickCandidateForm(c)">
            <MapPin :size="16" stroke-width="1.8" />
            <div class="np-cand-txt"><span>{{ c.name }}</span><small>{{ c.display }} · {{ c.lat.toFixed(3) }},{{ c.lng.toFixed(3) }}</small></div>
          </button>
        </div>
        <p v-else-if="!npSearching && npName.trim()" class="np-empty">没找到这个地名，在地图上点一下就行</p>

        <p class="np-pick-hint">找不到的话，也可以在地图上点选</p>
        <MiniMapPicker :lat="npLat" :lng="npLng" :height="280" @pick="onFormMapPick" />
        <div v-if="npLat != null && npLng != null" class="np-picked">已选：{{ npLat.toFixed(5) }}, {{ npLng.toFixed(5) }}</div>
        <p v-if="npErr" class="s-err">{{ npErr }}</p>
  </PageLayout>

  <PageLayout v-else title="地图" actions :scroll="view === 'list'">
    <template #subheader>
      <!-- 视图切换 -->
      <div class="seg">
      <button :class="{ on: view === 'map' }" @click="view = 'map'">
        <MapIcon :size="14" stroke-width="2" /> 地图
      </button>
      <button :class="{ on: view === 'list' }" @click="view = 'list'">
        <List :size="14" stroke-width="2" /> 列表
      </button>
      </div>
    </template>

    <MapCanvas
      v-if="view === 'map'"
      ref="mapRef"
      v-model:highlight-id="hlPoint"
      :points="mappablePoints"
    />

    <!-- 列表视图 -->
    <div v-else class="list-view">
      <p v-if="loadError" class="error">{{ loadError }}</p>
      <EmptyState v-else-if="!loadingPoints && points.length === 0" :icon="MapPin" title="足迹还空着" sub="标记第一个地点，慢慢攒成你们的足迹。" action="标记地点" @action="router.push('/map?new=1')" />
      <div v-else>
        <div
          v-for="p in points"
          :key="p.id"
          class="point-row pressable"
          :class="{ hl: hlPoint === p.id, pressing: pressing === p.id }"
          @pointerdown="pressDown($event, p.id, () => askDeletePoint(p))"
          @pointermove="pressMove"
          @pointerup="pressUp"
          @pointerleave="pressUp"
          @pointercancel="pressUp"
          @click="onPointClick(p, $event)"
        >
          <div class="pr-icon"><MapPin :size="20" stroke-width="2" /></div>
          <div class="pr-info">
            <div class="pr-name">{{ p.name }}</div>
            <div v-if="p.visit_date" class="pr-time">{{ p.visit_date }}</div>
            <div v-if="p.description" class="pr-desc">{{ p.description }}</div>
          </div>
          <ChevronRight class="chevron" :size="18" stroke-width="1.8" />
        </div>
      </div>
    </div>

    <!-- 删除确认 -->
    <ConfirmSheet
      v-if="deletingPoint"
      v-model:show="showDeletePoint"
      :title="RECYCLE_TITLE"
      :message="recycleMsg(deletingPoint.name, '动态里的地点名仍保留为快照。' + peerNotice(auth.user?.id, deletingPoint.created_by))"
      :confirm-text="RECYCLE_CONFIRM"
      :confirm-danger="true"
      @confirm="confirmDeletePoint"
    />

    <AppToast :text="toast" @done="toast = ''" />
  </PageLayout>
</template>

<style scoped>
/* 视图切换 */
.seg {
  display: flex;
  background: rgba(120, 120, 128, 0.16);
  border-radius: 9px;
  padding: 2px;
  margin: 4px 16px 8px;
  flex-shrink: 0;
  align-self: stretch;
  width: max-content;
  margin-left: 16px;
}
.seg button {
  border: none;
  background: none;
  padding: 6px 14px;
  border-radius: 7px;
  font-size: 13px;
  font-weight: 500;
  color: var(--ink);
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 4px;
}
.seg button.on {
  background: var(--surface);
  box-shadow: var(--shadow);
  font-weight: 600;
}

/* 地图区 */

/* 添加地点完整表单：字段并入 iOS 分组卡 */
/* 行结构样式（.form-rows/.frow/.rlabel/.rvalue/.cell-input）已提升到 App.vue 全局，
   与 .group .row 同族复用；此处只放本页专有的 .np-* */
.np-search {
  gap: 6px;
}
.np-go {
  width: 34px;
  height: 34px;
  min-width: 34px;
  border: none;
  border-radius: 9px;
  background: var(--primary);
  color: var(--on-primary);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
}
.np-go:disabled { opacity: 0.4; }
.np-cands {
  display: flex;
  flex-direction: column;
  gap: 6px;
  max-height: 180px;
  overflow-y: auto;
  margin: 10px 0 8px;
}
.np-cand {
  display: flex;
  align-items: center;
  gap: 8px;
  border: none;
  background: var(--surface);
  border-radius: 10px;
  padding: 9px 12px;
  font-size: 14px;
  color: var(--ink);
  cursor: pointer;
  text-align: left;
  box-shadow: var(--shadow);
}
.np-cand-txt { display: flex; flex-direction: column; }
.np-cand-txt small { font-size: 12px; color: var(--muted-ink); }
.np-empty { font-size: 13px; color: var(--muted-ink); text-align: center; margin: 10px 0 6px; }
/* 经纬度两列：border-box + width:100%，栅格不再被 padding 撑溢出 */
.np-ll {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}
.np-ll label {
  display: flex;
  flex-direction: column;
  gap: 3px;
  font-size: 12px;
  color: var(--muted-ink);
  min-width: 0;
}
.np-ll input {
  box-sizing: border-box;
  width: 100%;
  border: none;
  padding: 2px 0 0;
  font-size: 15px;
  background: none;
  color: var(--ink);
  outline: none;
  font-family: inherit;
}
.np-pick-hint { font-size: 13px; color: var(--muted-ink); margin: 14px 0 6px; }
.np-picked {
  font-size: 12px;
  color: var(--primary-deep);
  text-align: center;
  margin-top: 6px;
}
/* 导航栏动作 = tint 色文字按钮（iOS HIG） */
.np-save {
  border: none;
  background: none;
  color: var(--tint);
  font-size: 17px;
  cursor: pointer;
  padding: 4px;
}
.np-save:disabled { opacity: 0.4; }

/* pin */

/* 缩放控件 */

/* 列表视图 */
.point-row {
  display: flex;
  align-items: center;
  gap: 12px;
  background: var(--surface);
  border-radius: 12px;
  padding: 12px;
  box-shadow: var(--shadow);
  margin-bottom: 10px;
  cursor: pointer;
  border: 1px solid transparent;
}
.point-row.hl {
  border-color: var(--primary);
  background: rgba(255, 143, 171, 0.08);
}
:root[data-theme='dark'] .point-row.hl {
  background: rgba(255, 143, 171, 0.12);
}
.pr-icon {
  width: 44px;
  height: 44px;
  border-radius: 10px;
  background: var(--sem-green-soft);
  color: var(--sem-green);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
:root[data-theme='dark'] .pr-icon {
  background: rgba(52, 199, 89, 0.16);
}
.pr-info { flex: 1; min-width: 0; }
.pr-name { font-size: 15px; font-weight: 600; }
.pr-time { font-size: 12px; color: var(--muted-ink); margin-top: 2px; }
.pr-desc {
  font-size: 13px;
  color: var(--ink);
  opacity: 0.75;
  margin-top: 3px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.chevron { color: #c7c7cc; flex-shrink: 0; }

/* 创建 sheet 输入 */
.s-input {
  width: 100%;
  border: 1px solid var(--rule);
  border-radius: 10px;
  padding: 10px 12px;
  font-size: 14px;
  background: var(--bg, #fff);
  color: var(--ink, #222);
  outline: none;
  font-family: inherit;
}
.s-input + .s-input { margin-top: 8px; }
.s-input:focus { border-color: var(--primary, #ff8fab); }
.s-textarea { resize: none; }
.s-err { font-size: 13px; color: var(--danger); margin: 0; }

/* 空态 */
.error {
  text-align: center;
  color: var(--danger);
  padding: 40px 0;
}
</style>