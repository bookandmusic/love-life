<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { LocateFixed, MapPin, Minus, Plus } from 'lucide-vue-next'
import { ensureTileGrant, latLonToTile, tileInWorld, tileToLatLon, tileUrl, tileGrid, TILE } from '../utils/mapTiles'

// 迷你瓦片地图：用于「地点选择」——展示底图，支持点击选点 / 拖拽平移 / ±缩放 / 重置视角。
// 瓦片契约与 MapView 共用（/api/v1/map/tile，WebMercator，TILE=256，见 utils/mapTiles.ts）。
const MIN_ZOOM = 3
const MAX_ZOOM = 8
const DEFAULT_ZOOM = 6
const DEFAULT_LAT = 33
const DEFAULT_LNG = 106

const props = withDefaults(defineProps<{ lat?: number | null; lng?: number | null; height?: number }>(), {
  lat: null,
  lng: null,
  height: 240,
})
const emit = defineEmits<{ (e: 'pick', pos: { lat: number; lng: number }): void }>()

const container = ref<HTMLElement | null>(null)
const cw = ref(0)
const ch = ref(0)
const zoom = ref(DEFAULT_ZOOM)
const center = ref<{ x: number; y: number }>({ x: 0, y: 0 })
const picked = ref<{ lat: number; lng: number } | null>(null)
const dragging = ref(false)
const dragMoved = ref(false)
const dragLast = ref({ x: 0, y: 0 })
let ro: ResizeObserver | null = null

// 视口内需要渲染的瓦片（相对容器左上角定位；排布逻辑见 utils/mapTiles.tileGrid）
// 越出世界范围的格直接跳过（与 MapView 共用 tileInWorld 谓词），避免请求无效瓦片
const view = computed(() =>
  tileGrid(center.value.x, center.value.y, cw.value, ch.value).filter((c) => tileInWorld(zoom.value, c.x, c.y)),
)

function recenter(lat: number, lng: number) {
  center.value = latLonToTile(lat, lng, zoom.value)
}

const markerStyle = computed(() => {
  if (!picked.value) return null
  const t = latLonToTile(picked.value.lat, picked.value.lng, zoom.value)
  const ox = center.value.x * TILE - cw.value / 2
  const oy = center.value.y * TILE - ch.value / 2
  return { left: `${t.x * TILE - ox}px`, top: `${t.y * TILE - oy}px` }
})

// ============ 交互：点击选点 + 拖拽平移 ============
const canvas = ref<HTMLElement | null>(null)

function pickAtScreen(clientX: number, clientY: number) {
  const el = canvas.value
  if (!el) return
  const rect = el.getBoundingClientRect()
  const px = clientX - rect.left
  const py = clientY - rect.top
  const t = {
    x: (center.value.x * TILE - cw.value / 2 + px) / TILE,
    y: (center.value.y * TILE - ch.value / 2 + py) / TILE,
  }
  const geo = tileToLatLon(t.x, t.y, zoom.value)
  picked.value = { lat: Number(geo.lat.toFixed(6)), lng: Number(geo.lon.toFixed(6)) }
  emit('pick', { ...picked.value })
}

function onPointerDown(e: PointerEvent) {
  dragging.value = true
  dragMoved.value = false
  dragLast.value = { x: e.clientX, y: e.clientY }
  const el = e.currentTarget as HTMLElement
  try {
    el.setPointerCapture(e.pointerId)
  } catch {
    /* ignore */
  }
}

function onPointerMove(e: PointerEvent) {
  if (!dragging.value) return
  const dx = e.clientX - dragLast.value.x
  const dy = e.clientY - dragLast.value.y
  if (Math.abs(dx) + Math.abs(dy) > 4) dragMoved.value = true
  if (dragMoved.value) {
    center.value = { x: center.value.x - dx / TILE, y: center.value.y - dy / TILE }
    dragLast.value = { x: e.clientX, y: e.clientY }
  }
}

function onPointerUp(e: PointerEvent) {
  if (!dragging.value) return
  dragging.value = false
  if (!dragMoved.value) pickAtScreen(e.clientX, e.clientY)
}

function zoomBy(delta: number) {
  const newZoom = Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, zoom.value + delta))
  if (newZoom === zoom.value) return
  const anchorGeo = tileToLatLon(center.value.x, center.value.y, zoom.value)
  zoom.value = newZoom
  center.value = latLonToTile(anchorGeo.lat, anchorGeo.lon, newZoom)
}

// 重置：回到默认缩放 + 传入的经纬度（未传则默认视角）
function resetView() {
  zoom.value = DEFAULT_ZOOM
  recenter(props.lat ?? DEFAULT_LAT, props.lng ?? DEFAULT_LNG)
}

onMounted(() => {
  // 首个签名到位后 tileUrl 会经 version 依赖重渲染，补齐初始无签的瓦片
  void ensureTileGrant()
  if (!container.value) return
  const el = container.value
  const update = () => {
    cw.value = el.clientWidth || 300
    ch.value = el.clientHeight || props.height
  }
  update()
  ro = new ResizeObserver(update)
  ro.observe(el)
  const lat = props.lat ?? DEFAULT_LAT
  const lng = props.lng ?? DEFAULT_LNG
  if (props.lat != null && props.lng != null) picked.value = { lat: props.lat, lng: props.lng }
  recenter(lat, lng)
})

onBeforeUnmount(() => ro?.disconnect())

watch(
  () => [props.lat, props.lng] as const,
  ([nt, nl]) => {
    if (nt != null && nl != null) {
      picked.value = { lat: nt, lng: nl }
      recenter(nt, nl)
    }
  },
)
</script>

<template>
  <div class="mm-wrap" :style="{ height: `${height}px` }">
    <div class="mm-controls">
      <button class="mm-btn" aria-label="放大" @click="zoomBy(1)"><Plus :size="15" stroke-width="2" /></button>
      <button class="mm-btn" aria-label="缩小" @click="zoomBy(-1)"><Minus :size="15" stroke-width="2" /></button>
      <button class="mm-btn" aria-label="重置视角" title="重置" @click="resetView"><LocateFixed :size="15" stroke-width="2" /></button>
    </div>
    <div
      ref="canvas"
      class="mm-canvas"
      :class="{ dragging }"
      @pointerdown="onPointerDown"
      @pointermove="onPointerMove"
      @pointerup="onPointerUp"
      @pointercancel="onPointerUp"
    >
      <img
        v-for="c in view"
        :key="`${c.x}-${c.y}-${zoom}`"
        :src="tileUrl(zoom, c.x, c.y)"
        class="mm-tile"
        :style="{ left: `${c.left}px`, top: `${c.top}px` }"
        alt=""
        draggable="false"
      />
      <div v-if="markerStyle" class="mm-pin" :style="markerStyle"><MapPin :size="22" fill="currentColor" stroke-width="1.5" /></div>
      <div class="mm-hint">点击选点 · 拖动平移</div>
    </div>
  </div>
</template>

<style scoped>
.mm-wrap {
  position: relative;
  border-radius: 12px;
  overflow: hidden;
  box-shadow: var(--shadow);
  background: #e8ece5;
}
.mm-canvas {
  position: absolute;
  inset: 0;
  overflow: hidden;
  cursor: grab;
  touch-action: none;
}
.mm-canvas.dragging {
  cursor: grabbing;
}
.mm-tile {
  position: absolute;
  width: 256px;
  height: 256px;
  user-select: none;
  pointer-events: none;
}
.mm-pin {
  position: absolute;
  transform: translate(-50%, -100%);
  color: var(--danger, #ff3b30);
  pointer-events: none;
  filter: drop-shadow(0 1px 2px rgba(0, 0, 0, 0.35));
}
.mm-hint {
  position: absolute;
  left: 50%;
  bottom: 8px;
  transform: translateX(-50%);
  background: rgba(0, 0, 0, 0.55);
  color: #fff;
  font-size: 11px;
  padding: 3px 10px;
  border-radius: 999px;
  pointer-events: none;
  white-space: nowrap;
}
.mm-controls {
  position: absolute;
  right: 8px;
  top: 8px;
  z-index: 3;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.mm-btn {
  width: 28px;
  height: 28px;
  border: none;
  border-radius: 8px;
  background: rgba(255, 255, 255, 0.92);
  color: var(--ink);
  display: flex;
  align-items: center;
  justify-content: center;
  box-shadow: 0 1px 4px rgba(0, 0, 0, 0.2);
  cursor: pointer;
}
.mm-btn:active {
  transform: scale(0.94);
}
</style>