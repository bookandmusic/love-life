<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  ChevronLeft, ChevronRight, List, Map as MapIcon, MapPin, Minus, Pin, Plus, RefreshCw, Search, Trash2,
} from 'lucide-vue-next'
import { api } from '../api/client'
import NavActions from '../components/NavActions.vue'
import ConfirmSheet from '../components/ConfirmSheet.vue'
import Sheet from '../components/Sheet.vue'
import MiniMapPicker from '../components/MiniMapPicker.vue'
import { latLonToTile, tileToLatLon, tileUrl, TILE } from '../utils/mapTiles'

// ============ 瓦片引擎（统一实现见 utils/mapTiles.ts：APP 统一请求后端 /api/v1/map/tile，密钥不下发） ============
const MAX_ZOOM = 12
const MIN_ZOOM = 3

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

// 视图：地图 / 列表
const view = ref<'map' | 'list'>('map')

// 真实点位数据
const points = ref<PointItem[]>([])
const loadingPoints = ref(false)
const loadError = ref('')

// 地图状态（用 ref 保持响应式）
const mapZoom = ref(4)
const mapCenter = ref(latLonToTile(35, 104, 4))
const tiles = ref<Array<{ key: string; px: number; py: number; url: string | null }>>([])
const gridOrigin = ref({ x: 0, y: 0 })

// 容器尺寸：用 ref 跟踪以避免 getElementById
const containerW = ref(0)
const containerH = ref(0)
const containerRef = ref<HTMLDivElement | null>(null)
let resizeObs: ResizeObserver | null = null

// 高亮
const hlPoint = ref<number | null>(null)

// FAB「添加地点」入口：?new=1 → 建点模式（点选 → 命名 sheet → 提交）
const creating = ref(false)
const tapToPick = ref(false) // 点选模式标志
const pickedCoord = ref<{ lat: number; lng: number } | null>(null)
const newName = ref('')
const newDesc = ref('')
const newVisitDate = ref('')
const saving = ref(false)
const createError = ref('')

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
  } catch {
    npCandidates.value = []
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
    layout()
    toast.value = '已添加'
    setTimeout(() => { if (toast.value === '已添加') toast.value = '' }, 1500)
  } catch (e) {
    npErr.value = e instanceof Error ? e.message : '添加失败'
  } finally {
    saving.value = false
  }
}

// 通用 toast
const toast = ref('')

// 视图切换时重渲染
watch(view, () => { nextTick(() => layout()) })

onMounted(async () => {
  await loadPoints()
  // FAB「添加地点」→ 打开完整表单页
  if (route.query.new === '1') {
    newForm.value = true
  }
  nextTick(() => {
    layout()
    // 跟踪容器尺寸
    if (containerRef.value) {
      containerW.value = containerRef.value.clientWidth
      containerH.value = containerRef.value.clientHeight
      resizeObs = new ResizeObserver(() => {
        if (!containerRef.value) return
        containerW.value = containerRef.value.clientWidth
        containerH.value = containerRef.value.clientHeight
        layout()
      })
      resizeObs.observe(containerRef.value)
    }
    bindDrag()
    bindTouch()
  })
})

// ?new=1 → 打开完整表单页（FAB「地点」入口；watch 支持停留本页时重复触发）
watch(
  () => route.query.new,
  (v) => {
    if (v === '1') {
      npName.value = ''
      npLat.value = null
      npLng.value = null
      npCandidates.value = []
      npErr.value = ''
      newForm.value = true
    }
  },
)

onBeforeUnmount(() => {
  resizeObs?.disconnect()
  resizeObs = null
})

async function loadPoints() {
  loadingPoints.value = true
  loadError.value = ''
  try {
    const res = await api<{ items: PointItem[] }>('/api/v1/map/points')
    points.value = res.items
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : '加载失败'
  } finally {
    loadingPoints.value = false
  }
}

// 位置未知的点位（纯文字地点、离线表未命中）不落地图标记：lat/lng 均为 0 无真实位置，
// 落标记会画在赤道正中（Null Island）污染地图；列表视图照常展示
const mappablePoints = computed(() => points.value.filter((p) => p.lat !== 0 || p.lng !== 0))

// 重新计算瓦片布局
function layout() {
  if (containerW.value === 0 || containerH.value === 0) return
  const cw = containerW.value
  const ch = containerH.value
  const centerWorldX = mapCenter.value.x * TILE
  const centerWorldY = mapCenter.value.y * TILE
  const ox = centerWorldX - cw / 2
  const oy = centerWorldY - ch / 2
  gridOrigin.value = { x: ox, y: oy }
  const startX = Math.floor(ox / TILE)
  const startY = Math.floor(oy / TILE)
  const endX = Math.ceil((ox + cw) / TILE)
  const endY = Math.ceil((oy + ch) / TILE)
  const list: Array<{ key: string; px: number; py: number; url: string | null }> = []
  for (let tx = startX; tx <= endX; tx++) {
    for (let ty = startY; ty <= endY; ty++) {
      const inWorld = tx >= 0 && ty >= 0 && tx < Math.pow(2, mapZoom.value) && ty < Math.pow(2, mapZoom.value)
      list.push({
        key: `${mapZoom.value}/${tx}/${ty}`,
        px: tx * TILE - ox,
        py: ty * TILE - oy,
        url: inWorld ? tileUrl(mapZoom.value, tx, ty) : null,
      })
    }
  }
  tiles.value = list
}

function zoomMap(delta: number) {
  const newZoom = Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, mapZoom.value + delta))
  if (newZoom === mapZoom.value) return
  if (containerW.value === 0) return
  const cw = containerW.value
  const ch = containerH.value
  const centerWorldX = gridOrigin.value.x + cw / 2
  const centerWorldY = gridOrigin.value.y + ch / 2
  const oldTileX = centerWorldX / TILE
  const oldTileY = centerWorldY / TILE
  const anchor = tileToLatLon(oldTileX, oldTileY, mapZoom.value)
  mapZoom.value = newZoom
  mapCenter.value = latLonToTile(anchor.lat, anchor.lon, newZoom)
  layout()
}

function resetMap() {
  mapZoom.value = 4
  mapCenter.value = latLonToTile(35, 104, 4)
  hlPoint.value = null
  layout()
}

function centerOnPoint(id: number) {
  const p = points.value.find((x) => x.id === id)
  if (!p) return
  hlPoint.value = id
  if (p.lat === 0 && p.lng === 0) return // 位置未知：仅高亮列表项，不跳地图
  mapCenter.value = latLonToTile(p.lat, p.lng, mapZoom.value)
  layout()
  view.value = 'map'
}

function pointPos(p: PointItem) {
  const t = latLonToTile(p.lat, p.lng, mapZoom.value)
  return { x: t.x * TILE - gridOrigin.value.x, y: t.y * TILE - gridOrigin.value.y }
}

function pointStyle(p: PointItem) {
  const pos = pointPos(p)
  return { left: pos.x + 'px', top: pos.y + 'px' }
}

function pointVisible(p: PointItem) {
  if (containerW.value === 0) return false
  const pos = pointPos(p)
  return pos.x > -60 && pos.x < containerW.value + 60 && pos.y > -60 && pos.y < containerH.value + 60
}

// ---------- 手势：单指平移（鼠标/手写笔 Pointer + 触屏 Touch）+ 双指捏合缩放 ----------
let dragging = false       // 鼠标/手写笔拖拽中
let touchPanning = false   // 触屏单指平移中
let lastX = 0
let lastY = 0
let panLastX = 0
let panLastY = 0
let shiftX = 0
let shiftY = 0
let pinch: { active: boolean; startDist: number; startZoom: number; anchorLat: number; anchorLon: number; offsetX: number; offsetY: number } | null = null

// 提交未落地的平移：把 CSS transform 位移合入 mapCenter，并重新布局
function commitPan() {
  const g = document.getElementById('map-grid')
  if (g) g.style.transform = ''
  if (shiftX || shiftY) {
    if (containerW.value) {
      const cw = containerW.value
      const ch = containerH.value
      const centerWorldX = gridOrigin.value.x + cw / 2 - shiftX
      const centerWorldY = gridOrigin.value.y + ch / 2 - shiftY
      mapCenter.value = { x: centerWorldX / TILE, y: centerWorldY / TILE }
    }
    shiftX = 0
    shiftY = 0
    layout()
  }
}

// 鼠标 / 手写笔单指拖拽（触屏手势统一由 bindTouch 处理，避免双指捏合混入拖拽）
function bindDrag() {
  const grid = document.getElementById('map-grid')
  if (!grid) return
  grid.addEventListener('pointerdown', (e) => {
    if (e.pointerType !== 'mouse' && e.pointerType !== 'pen') return
    const t = e.target as HTMLElement
    if (t.closest('.controls') || t.closest('.pin')) return
    if (tapToPick.value) return // 点选模式下由点击处理
    hlPoint.value = null
    dragging = true
    lastX = e.clientX
    lastY = e.clientY
    try { grid.setPointerCapture(e.pointerId) } catch { /* ignore */ }
  })
  grid.addEventListener('pointermove', (e) => {
    if (!dragging) return
    if (e.pointerType !== 'mouse' && e.pointerType !== 'pen') { dragging = false; return }
    const dx = e.clientX - lastX
    const dy = e.clientY - lastY
    lastX = e.clientX
    lastY = e.clientY
    shiftX += dx
    shiftY += dy
    grid.style.transform = `translate(${shiftX}px, ${shiftY}px)`
  })
  const end = () => {
    if (!dragging) return
    dragging = false
    commitPan()
  }
  grid.addEventListener('pointerup', end)
  grid.addEventListener('pointercancel', end)
}

function touchDist(e: TouchEvent): number {
  const t0 = e.touches[0]
  const t1 = e.touches[1]
  const dx = t1.clientX - t0.clientX
  const dy = t1.clientY - t0.clientY
  return Math.sqrt(dx * dx + dy * dy)
}

// 触屏手势：单指平移 + 双指捏合（以双指中点为缩放锚点，二者互不冲突）
function bindTouch() {
  const grid = document.getElementById('map-grid')
  if (!grid) return

  grid.addEventListener('touchstart', (e) => {
    if (tapToPick.value) return // 点选模式：手势不启用，由点击处理
    if (e.touches.length === 2) {
      // 若此前有未落地的单指平移，先提交，避免与捏合互相污染
      if (touchPanning) {
        touchPanning = false
        commitPan()
      }
      const rect = grid.getBoundingClientRect()
      const t0 = e.touches[0]
      const t1 = e.touches[1]
      // 双指中点对应的屏幕坐标 → 当前缩放级别下的瓦片坐标 → 地理锚点
      const mx = (t0.clientX + t1.clientX) / 2 - rect.left
      const my = (t0.clientY + t1.clientY) / 2 - rect.top
      const za = mapZoom.value
      const tx = (gridOrigin.value.x + mx) / TILE
      const ty = (gridOrigin.value.y + my) / TILE
      const anchor = tileToLatLon(tx, ty, za)
      pinch = {
        active: true,
        startDist: touchDist(e),
        startZoom: za,
        anchorLat: anchor.lat,
        anchorLon: anchor.lon,
        // 锚点（双指中点）相对视口中心的像素偏移：缩放时保持该屏幕位置不变
        offsetX: mx - containerW.value / 2,
        offsetY: my - containerH.value / 2,
      }
    } else if (e.touches.length === 1) {
      if (pinch?.active) pinch = null
      touchPanning = true
      panLastX = e.touches[0].clientX
      panLastY = e.touches[0].clientY
    }
  }, { passive: true })

  grid.addEventListener('touchmove', (e) => {
    if (tapToPick.value) return
    if (e.touches.length === 2 && pinch?.active) {
      e.preventDefault()
      const d = touchDist(e)
      const ratio = d / pinch.startDist
      const newZoom = Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, Math.round(pinch.startZoom + Math.log2(ratio))))
      if (newZoom !== mapZoom.value) {
        mapZoom.value = newZoom
        // 核心修复：将捏合锚点换算到新缩放级别的瓦片坐标，避免中心随 2^(Δz) 漂移；
        // 并保持锚点（双指中点）在屏幕上的相对位置不随时间偏移
        const baseTile = latLonToTile(pinch.anchorLat, pinch.anchorLon, newZoom)
        mapCenter.value = {
          x: baseTile.x - pinch.offsetX / TILE,
          y: baseTile.y - pinch.offsetY / TILE,
        }
        layout()
      }
      return
    }
    if (e.touches.length === 1 && touchPanning) {
      e.preventDefault()
      const t = e.touches[0]
      const dx = t.clientX - panLastX
      const dy = t.clientY - panLastY
      panLastX = t.clientX
      panLastY = t.clientY
      shiftX += dx
      shiftY += dy
      const g2 = document.getElementById('map-grid')
      if (g2) g2.style.transform = `translate(${shiftX}px, ${shiftY}px)`
    }
  }, { passive: false })

  grid.addEventListener('touchend', (e) => {
    if (tapToPick.value) return
    if (pinch?.active) {
      if (e.touches.length >= 2) return // 仍处于捏合中
      pinch = null
      // 剩一指：无缝转回单指平移（从当前手指位置续）
      if (e.touches.length === 1) {
        touchPanning = true
        panLastX = e.touches[0].clientX
        panLastY = e.touches[0].clientY
      }
      return
    }
    if (touchPanning && e.touches.length === 0) {
      touchPanning = false
      commitPan()
    }
  }, { passive: true })

  grid.addEventListener('touchcancel', () => {
    pinch = null
    touchPanning = false
    commitPan()
  }, { passive: true })
}


// 点选模式：点击地图空白 → 记录该点经纬度 → 弹命名 sheet
function onGridClick(e: MouseEvent) {
  if (!tapToPick.value) return
  const t = e.target as HTMLElement
  if (t.closest('.pin') || t.closest('.controls')) return
  const grid = e.currentTarget as HTMLElement
  const rect = grid.getBoundingClientRect()
  const x = e.clientX - rect.left - shiftX
  const y = e.clientY - rect.top - shiftY
  const worldX = gridOrigin.value.x + x
  const worldY = gridOrigin.value.y + y
  const tileX = worldX / TILE
  const tileY = worldY / TILE
  const n = Math.pow(2, mapZoom.value)
  const lon = (tileX / n) * 360 - 180
  const lat = (Math.atan(Math.sinh(Math.PI * (1 - (2 * tileY) / n))) * 180) / Math.PI
  pickedCoord.value = { lat, lng: lon }
  newName.value = ''
  newDesc.value = ''
  newVisitDate.value = new Date().toISOString().slice(0, 10)
  creating.value = true
  tapToPick.value = false
}

// 取消点选模式
function cancelPick() {
  tapToPick.value = false
  pickedCoord.value = null
}

async function submitNewPoint() {
  if (!pickedCoord.value || !newName.value.trim() || saving.value) return
  saving.value = true
  createError.value = ''
  try {
    const body: Record<string, unknown> = {
      name: newName.value.trim(),
      lat: pickedCoord.value.lat,
      lng: pickedCoord.value.lng,
      source: 'manual',
    }
    if (newDesc.value.trim()) body.description = newDesc.value.trim()
    if (newVisitDate.value) body.visit_date = newVisitDate.value
    await api('/api/v1/map/points', {
      method: 'POST',
      body: JSON.stringify(body),
    })
    creating.value = false
    pickedCoord.value = null
    await loadPoints()
    layout()
    toast.value = '已添加'
    setTimeout(() => { if (toast.value === '已添加') toast.value = '' }, 1500)
  } catch (e) {
    createError.value = e instanceof Error ? e.message : '添加失败'
  } finally {
    saving.value = false
  }
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
  } catch (e) {
    toast.value = e instanceof Error ? e.message : '删除失败'
  } finally {
    deletingPoint.value = null
  }
}
</script>

<template>
  <div class="map-page">
    <!-- 完整表单：添加地点（名称+经纬度+geocode 候选+地图选点） -->
    <template v-if="newForm">
      <header class="navbar glass">
        <button class="nav-btn" aria-label="返回" @click="closeNewForm"><ChevronLeft :size="22" stroke-width="2.5" /></button>
        <span class="title">添加地点</span>
        <button class="np-save" :disabled="!npName.trim() || npLat == null || npLng == null || saving" @click="saveNewPointForm">{{ saving ? '保存中…' : '保存' }}</button>
      </header>
      <div class="np-body">
        <div class="np-search">
          <input v-model="npName" class="s-input np-name" placeholder="地点名称（必填，如：厦门·鼓浪屿）" maxlength="30" @keyup.enter="searchGeoForm" />
          <button class="np-go" :disabled="npSearching || !npName.trim()" aria-label="搜索" @click="searchGeoForm"><Search :size="16" stroke-width="2" /></button>
        </div>
        <div v-if="npCandidates.length" class="np-cands">
          <button v-for="c in npCandidates" :key="c.name" class="np-cand" @click="pickCandidateForm(c)">
            <MapPin :size="16" stroke-width="1.8" />
            <div class="np-cand-txt"><span>{{ c.name }}</span><small>{{ c.display }} · {{ c.lat.toFixed(3) }},{{ c.lng.toFixed(3) }}</small></div>
          </button>
        </div>
        <p v-else-if="!npSearching && npName.trim()" class="np-empty">未匹配到，可直接在地图上点选</p>

        <div class="np-ll">
          <label>经度<input v-model.number="npLng" type="number" step="any" placeholder="经度" /></label>
          <label>纬度<input v-model.number="npLat" type="number" step="any" placeholder="纬度" /></label>
        </div>

        <p class="np-pick-hint">或在地图上点选（候选不满意时）</p>
        <MiniMapPicker :lat="npLat" :lng="npLng" :height="280" @pick="onFormMapPick" />
        <div v-if="npLat != null && npLng != null" class="np-picked">已选：{{ npLat.toFixed(5) }}, {{ npLng.toFixed(5) }}</div>
        <p v-if="createError || npErr" class="s-err">{{ npErr || createError }}</p>
      </div>
    </template>

    <template v-else>
    <header class="navbar glass">
      <span class="title">地图</span>
      <NavActions />
    </header>

    <!-- 视图切换 -->
    <div class="seg">
      <button :class="{ on: view === 'map' }" @click="view = 'map'">
        <MapIcon :size="14" stroke-width="2" /> 地图
      </button>
      <button :class="{ on: view === 'list' }" @click="view = 'list'">
        <List :size="14" stroke-width="2" /> 列表
      </button>
    </div>

    <div v-if="view === 'map'" class="map-fixed">
      <div ref="containerRef" class="map-container">
        <!-- 点选模式提示 -->
        <Transition name="fade">
          <div v-if="tapToPick" class="pick-banner" @click="cancelPick">
            <Pin :size="14" stroke-width="2" />
            点选位置（再次点击此处取消）
          </div>
        </Transition>
        <div id="map-grid" class="map-grid" :class="{ picking: tapToPick }" @click="onGridClick">
          <div
            v-for="t in tiles"
            :key="t.key"
            class="tile"
            :style="{ left: t.px + 'px', top: t.py + 'px' }"
          >
            <img v-if="t.url" :src="t.url" alt="" loading="lazy" @error="(e: Event) => ((e.target as HTMLImageElement).style.opacity = '0.3')" />
          </div>

          <TransitionGroup name="pin">
            <div
              v-for="p in mappablePoints"
              :key="p.id"
              v-show="pointVisible(p)"
              class="pin"
              :class="{ hl: hlPoint === p.id }"
              :style="pointStyle(p)"
              @click.stop="centerOnPoint(p.id)"
            >
              <MapPin :size="14" stroke-width="2" />
              <span class="pin-name">{{ p.name.split('·')[0].trim() }}</span>
            </div>
          </TransitionGroup>

          <!-- 地图十字准星（点选模式可见） -->
          <div v-if="tapToPick" class="crosshair"><Pin :size="20" stroke-width="2" /></div>
        </div>

        <!-- 缩放控件 -->
        <div class="controls">
          <button v-if="!tapToPick" @click="zoomMap(1)" aria-label="放大"><Plus :size="17" stroke-width="2" /></button>
          <button v-if="!tapToPick" @click="zoomMap(-1)" aria-label="缩小"><Minus :size="17" stroke-width="2" /></button>
          <button v-if="!tapToPick" @click="resetMap" aria-label="重置"><RefreshCw :size="15" stroke-width="2" /></button>
        </div>
      </div>
    </div>

    <!-- 列表视图 -->
    <div v-else class="list-view">
      <p v-if="loadError" class="error">{{ loadError }}</p>
      <p v-else-if="!loadingPoints && points.length === 0" class="empty">
        还没有足迹，从右下角 + 开始记录吧
      </p>
      <div v-else>
        <div
          v-for="p in points"
          :key="p.id"
          class="point-row"
          :class="{ hl: hlPoint === p.id }"
          @click="centerOnPoint(p.id)"
        >
          <div class="pr-icon"><MapPin :size="20" stroke-width="2" /></div>
          <div class="pr-info">
            <div class="pr-name">{{ p.name }}</div>
            <div v-if="p.visit_date" class="pr-time">{{ p.visit_date }}</div>
            <div v-if="p.description" class="pr-desc">{{ p.description }}</div>
          </div>
          <button class="pr-del" aria-label="删除" @click.stop="askDeletePoint(p)">
            <Trash2 :size="16" stroke-width="1.8" />
          </button>
          <ChevronRight class="chevron" :size="18" stroke-width="1.8" />
        </div>
      </div>
    </div>

    <!-- 命名 sheet（点选完成后弹） -->
    <Sheet
      v-if="creating"
      v-model:show="creating"
      title="命名这个地点"
      confirm-text="添加"
      :confirm-disabled="!newName.trim() || saving"
      @confirm="submitNewPoint"
    >
      <input v-model="newName" class="s-input" placeholder="地点名（如：厦门·鼓浪屿）" maxlength="30" />
      <input v-model="newVisitDate" type="date" class="s-input" />
      <textarea v-model="newDesc" class="s-input s-textarea" placeholder="备注（选填）" maxlength="100" rows="2" />
      <p v-if="createError" class="s-err">{{ createError }}</p>
    </Sheet>

    <!-- 删除确认 -->
    <ConfirmSheet
      v-if="deletingPoint"
      v-model:show="showDeletePoint"
      title="删除足迹"
      :message="`确定删除「${deletingPoint.name}」？该动态中的地点名仍保留为快照`"
      confirm-text="删除"
      :confirm-danger="true"
      @confirm="confirmDeletePoint"
    />

    <!-- toast -->
    <Transition name="fade">
      <div v-if="toast" class="toast" @click="toast = ''">{{ toast }}</div>
    </Transition>
    </template>
  </div>
</template>

<style scoped>
.map-page {
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
.title { font-size: 17px; font-weight: 600; }

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
.map-fixed {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-height: 0;
  padding: 4px 16px 12px;
}
.map-container {
  flex: 1;
  min-height: 280px;
  border-radius: 12px;
  overflow: hidden;
  position: relative;
  border: 0.5px solid var(--rule);
  background: #eef3e8;
}
:root[data-theme='dark'] .map-container { background: #1c1c1e; }
.map-grid {
  position: absolute;
  inset: 0;
  overflow: hidden;
  cursor: grab;
  touch-action: none;
}
.map-grid.picking { cursor: crosshair; }
.tile { position: absolute; width: 256px; height: 256px; pointer-events: none; }
.tile img { width: 256px; height: 256px; display: block; transition: opacity .25s ease; }

/* 添加地点完整表单 */
.np-body {
  flex: 1;
  overflow-y: auto;
  padding: 12px 16px 32px;
}
.np-search {
  display: flex;
  gap: 6px;
  margin-bottom: 8px;
}
.np-name { flex: 1; }
.np-go {
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
.np-go:disabled { opacity: 0.4; }
.np-cands {
  display: flex;
  flex-direction: column;
  gap: 6px;
  max-height: 180px;
  overflow-y: auto;
  margin-bottom: 8px;
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
.np-empty { font-size: 13px; color: var(--muted-ink); text-align: center; margin: 6px 0; }
.np-ll {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
  margin: 8px 0;
}
.np-ll label {
  display: flex;
  flex-direction: column;
  gap: 3px;
  font-size: 12px;
  color: var(--muted-ink);
}
.np-ll input {
  border: 1px solid var(--rule);
  border-radius: 10px;
  padding: 9px 10px;
  font-size: 14px;
  background: var(--surface);
  color: var(--ink);
  outline: none;
}
.np-pick-hint { font-size: 13px; color: var(--muted-ink); margin: 8px 0 6px; }
.np-picked {
  font-size: 12px;
  color: var(--primary-deep);
  text-align: center;
  margin-top: 6px;
}
.np-save {
  border: none;
  background: var(--primary);
  color: #fff;
  border-radius: 999px;
  padding: 7px 14px;
  font-size: 14px;
  font-weight: 600;
}
.np-save:disabled { opacity: 0.45; }

/* 点选模式横条 */
.pick-banner {
  position: absolute;
  top: 10px;
  left: 50%;
  transform: translateX(-50%);
  z-index: 5;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  background: rgba(0, 0, 0, 0.7);
  color: #fff;
  padding: 6px 12px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
}

/* 十字准星 */
.crosshair {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  z-index: 4;
  color: var(--danger);
  pointer-events: none;
  opacity: 0.7;
}

/* pin */
.pin {
  position: absolute;
  transform: translate(-50%, -100%);
  display: flex;
  flex-direction: column;
  align-items: center;
  z-index: 3;
  cursor: pointer;
  color: var(--sem-green);
  filter: drop-shadow(0 1px 3px rgba(0, 0, 0, 0.3));
}
.pin.hl {
  color: #e5484d;
  transform: translate(-50%, -100%) scale(1.3);
  z-index: 4;
}
.pin-name {
  background: var(--surface);
  border-radius: 6px;
  font-size: 11px;
  font-weight: 600;
  padding: 2px 7px;
  margin-top: 2px;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.15);
  white-space: nowrap;
  color: var(--ink);
}
.pin-enter-active, .pin-leave-active { transition: opacity .2s ease, transform .2s ease; }
.pin-enter-from, .pin-leave-to { opacity: 0; }

/* 缩放控件 */
.controls {
  position: absolute;
  right: 12px;
  top: 12px;
  z-index: 5;
  display: flex;
  flex-direction: column;
  border-radius: 12px;
  overflow: hidden;
  background: linear-gradient(180deg, var(--glass-from), var(--glass-to));
  backdrop-filter: saturate(200%) blur(28px) brightness(1.05);
  -webkit-backdrop-filter: saturate(200%) blur(28px) brightness(1.05);
  border: 0.5px solid var(--glass-edge);
  box-shadow: 0 1px 12px rgba(0, 0, 0, 0.08), inset 0 1px 0 var(--glass-inset);
}
.controls button {
  width: 38px;
  height: 34px;
  border: none;
  background: none;
  color: var(--ios-blue);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
}
.controls button + button { border-top: 0.5px solid var(--rule); }
.controls button:active { background: rgba(0, 122, 255, 0.1); }

/* 列表视图 */
.list-view {
  flex: 1;
  overflow-y: auto;
  -webkit-overflow-scrolling: touch;
  padding: 0 16px 24px;
}
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
.pr-del {
  border: none;
  background: none;
  color: var(--danger);
  cursor: pointer;
  padding: 6px;
  border-radius: 50%;
  display: flex;
  align-items: center;
}
.pr-del:active { background: rgba(224, 73, 75, 0.1); }
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

/* 错误 toast */
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

/* 空态 */
.empty {
  text-align: center;
  color: var(--muted-ink);
  padding: 56px 16px;
  font-size: 14px;
}
.error {
  text-align: center;
  color: var(--danger);
  padding: 40px 0;
}
</style>