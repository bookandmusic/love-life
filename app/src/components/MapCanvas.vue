<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { MapPin, Minus, Plus, RefreshCw } from 'lucide-vue-next'
import { ensureTileGrant, latLonToTile, tileInWorld, tileToLatLon, tileUrl, tileGrid, TILE } from '../utils/mapTiles'

// ============ 瓦片引擎（统一实现见 utils/mapTiles.ts：APP 统一请求后端 /api/v1/map/tile，密钥不下发） ============
const MAX_ZOOM = 12
const MIN_ZOOM = 3

export interface MapPoint {
  id: number
  name: string
  lat: number
  lng: number
}

const props = defineProps<{
  points: MapPoint[]
  highlightId: number | null
}>()

const emit = defineEmits<{
  (e: 'update:highlightId', id: number | null): void
}>()

// 地图状态（用 ref 保持响应式）
const mapZoom = ref(4)
const mapCenter = ref(latLonToTile(35, 104, 4))
const tiles = ref<Array<{ key: string; px: number; py: number; url: string | null }>>([])
const gridOrigin = ref({ x: 0, y: 0 })

// 容器/网格尺寸：用 ref 跟踪以避免 getElementById
const containerW = ref(0)
const containerH = ref(0)
const containerRef = ref<HTMLDivElement | null>(null)
const gridRef = ref<HTMLDivElement | null>(null)
let resizeObs: ResizeObserver | null = null
let detachInteractions: (() => void) | null = null

// 地图挂载：容器与网格随 v-if 切换销毁重建，所以「测尺寸 + 挂观察器 + 绑手势 + 布局」
// 必须整体重跑一次；否则重建后 containerW 恒为 0，layout() 直接早退（地图空白）且手势全丢。
function attachMap() {
  detachInteractions?.()
  detachInteractions = null
  resizeObs?.disconnect()
  resizeObs = null
  const grid = gridRef.value
  const container = containerRef.value
  if (!grid || !container) return
  containerW.value = container.clientWidth
  containerH.value = container.clientHeight
  resizeObs = new ResizeObserver(() => {
    if (!containerRef.value) return
    containerW.value = containerRef.value.clientWidth
    containerH.value = containerRef.value.clientHeight
    layout()
  })
  resizeObs.observe(container)
  detachInteractions = bindInteractions(grid)
  layout()
}

// 网格/容器被 v-if 重建后自动重挂（首帧也走这里；attachMap 自带 teardown，重复调用安全）
watch([gridRef, containerRef], () => nextTick(attachMap))

// 高亮（父组件持有，地图内点击与列表点击共用）
const hlPoint = computed({
  get: () => props.highlightId,
  set: (v: number | null) => emit('update:highlightId', v),
})

// 重新计算瓦片布局（排布逻辑见 utils/mapTiles.tileGrid；世界越界格 url 置空）
function layout() {
  if (containerW.value === 0 || containerH.value === 0) return
  const cw = containerW.value
  const ch = containerH.value
  const centerWorldX = mapCenter.value.x * TILE
  const centerWorldY = mapCenter.value.y * TILE
  const ox = centerWorldX - cw / 2
  const oy = centerWorldY - ch / 2
  gridOrigin.value = { x: ox, y: oy }
  tiles.value = tileGrid(mapCenter.value.x, mapCenter.value.y, cw, ch).map((c) => ({
    key: `${mapZoom.value}/${c.x}/${c.y}`,
    px: c.left,
    py: c.top,
    url: tileInWorld(mapZoom.value, c.x, c.y) ? tileUrl(mapZoom.value, c.x, c.y) : null,
  }))
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
  const p = props.points.find((x) => x.id === id)
  if (!p) return
  hlPoint.value = id
  if (p.lat === 0 && p.lng === 0) return // 位置未知：仅高亮列表项，不跳地图
  mapCenter.value = latLonToTile(p.lat, p.lng, mapZoom.value)
  layout()
}

function pointPos(p: MapPoint) {
  const t = latLonToTile(p.lat, p.lng, mapZoom.value)
  return { x: t.x * TILE - gridOrigin.value.x, y: t.y * TILE - gridOrigin.value.y }
}

function pointStyle(p: MapPoint) {
  const pos = pointPos(p)
  return { left: pos.x + 'px', top: pos.y + 'px' }
}

function pointVisible(p: MapPoint) {
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
  const g = gridRef.value
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
function bindDrag(grid: HTMLElement): () => void {
  const onDown = (e: PointerEvent) => {
    if (e.pointerType !== 'mouse' && e.pointerType !== 'pen') return
    const t = e.target as HTMLElement
    if (t.closest('.controls') || t.closest('.pin')) return
    hlPoint.value = null
    dragging = true
    lastX = e.clientX
    lastY = e.clientY
    try { grid.setPointerCapture(e.pointerId) } catch { /* ignore */ }
  }
  const onMove = (e: PointerEvent) => {
    if (!dragging) return
    if (e.pointerType !== 'mouse' && e.pointerType !== 'pen') { dragging = false; return }
    shiftX += e.clientX - lastX
    shiftY += e.clientY - lastY
    lastX = e.clientX
    lastY = e.clientY
    grid.style.transform = `translate(${shiftX}px, ${shiftY}px)`
  }
  const end = () => {
    if (!dragging) return
    dragging = false
    commitPan()
  }
  grid.addEventListener('pointerdown', onDown)
  grid.addEventListener('pointermove', onMove)
  grid.addEventListener('pointerup', end)
  grid.addEventListener('pointercancel', end)
  return () => {
    grid.removeEventListener('pointerdown', onDown)
    grid.removeEventListener('pointermove', onMove)
    grid.removeEventListener('pointerup', end)
    grid.removeEventListener('pointercancel', end)
  }
}

function touchDist(e: TouchEvent): number {
  const t0 = e.touches[0]
  const t1 = e.touches[1]
  const dx = t1.clientX - t0.clientX
  const dy = t1.clientY - t0.clientY
  return Math.sqrt(dx * dx + dy * dy)
}

// 触屏手势：单指平移 + 双指捏合（以双指中点为缩放锚点，二者互不冲突）
function bindTouch(grid: HTMLElement): () => void {
  const onStart = (e: TouchEvent) => {
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
  }
  const onMove = (e: TouchEvent) => {
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
      const g = gridRef.value
      if (g) g.style.transform = `translate(${shiftX}px, ${shiftY}px)`
    }
  }
  const onEnd = (e: TouchEvent) => {
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
  }
  const onCancel = () => {
    pinch = null
    touchPanning = false
    commitPan()
  }
  grid.addEventListener('touchstart', onStart, { passive: true })
  grid.addEventListener('touchmove', onMove, { passive: false })
  grid.addEventListener('touchend', onEnd, { passive: true })
  grid.addEventListener('touchcancel', onCancel, { passive: true })
  return () => {
    grid.removeEventListener('touchstart', onStart)
    grid.removeEventListener('touchmove', onMove)
    grid.removeEventListener('touchend', onEnd)
    grid.removeEventListener('touchcancel', onCancel)
  }
}

// 手势统一入口：两套绑定的解绑器合并（网格随 v-if 重建时先解绑再重绑）
function bindInteractions(grid: HTMLElement): () => void {
  const offs = [bindDrag(grid), bindTouch(grid)]
  return () => offs.forEach((off) => off())
}

onMounted(async () => {
  // 首次布局前拿到瓦片签名，避免首帧无签导致整屏瓦片 401
  await ensureTileGrant()
  nextTick(attachMap)
})

onBeforeUnmount(() => {
  resizeObs?.disconnect()
  resizeObs = null
  detachInteractions?.()
  detachInteractions = null
})

// 父组件可调用：点位高亮变化后若需要重新居中（?focus=<id>）
function focus(id: number) {
  centerOnPoint(id)
}

defineExpose({ focus, attachMap, layout })
</script>

<template>
  <div class="map-fixed">
    <div ref="containerRef" class="map-container">
      <div ref="gridRef" class="map-grid">
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
            v-for="p in points"
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

      </div>

      <!-- 缩放控件 -->
      <div class="controls">
        <button @click="zoomMap(1)" aria-label="放大"><Plus :size="17" stroke-width="2" /></button>
        <button @click="zoomMap(-1)" aria-label="缩小"><Minus :size="17" stroke-width="2" /></button>
        <button @click="resetMap" aria-label="重置"><RefreshCw :size="15" stroke-width="2" /></button>
      </div>
    </div>
  </div>
</template>

<style scoped>
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
.tile { position: absolute; width: 256px; height: 256px; pointer-events: none; }
.tile img { width: 256px; height: 256px; display: block; transition: opacity .25s ease; }

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
</style>
