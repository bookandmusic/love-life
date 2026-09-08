<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { ChevronLeft, ChevronRight, Music, Trash2, X } from 'lucide-vue-next'
import { fullUrl } from '../api/client'
import { mediaUrl } from '../utils/mediaUrls'

export interface ViewerItem {
  id: number | string
  kind: string
  thumb_url: string
  url?: string
  post_id?: number | null
  taken_at?: string
}

const props = withDefaults(defineProps<{
  items: ViewerItem[]
  start: number
  mode?: 'timeline' | 'album' | 'post'
  albumName?: string
}>(), { mode: 'post' })

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'gopost', postId: number): void
  (e: 'remove', id: number | string): void
}>()

const idx = ref(Math.max(0, Math.min(props.start, props.items.length - 1)))
const total = computed(() => props.items.length)
const cur = computed(() => props.items[idx.value])

const scale = ref(1)
const tx = ref(0)
const ty = ref(0)
const animating = ref(true)
const isZoomed = computed(() => scale.value > 1.01)

function resetView() {
  scale.value = 1
  tx.value = 0
  ty.value = 0
}
function go(delta: number) {
  const n = idx.value + delta
  if (n < 0 || n >= total.value) return
  animating.value = true
  idx.value = n
  resetView()
  // iOS 滑动过渡：下一个位移过渡完后复位无动画
  requestAnimationFrame(resetView)
}

function togglleZoom() {
  animating.value = true
  if (scale.value > 1.01) resetView()
  else {
    scale.value = 2.5
    tx.value = 0
    ty.value = 0
  }
}
function clamp(v: number, min: number, max: number) { return Math.min(max, Math.max(min, v)) }

// —— 触摸手势（iOS 照片 App 风格） ——
const touches = new Map<number, { x: number; y: number }>()
let pinchDist = 0
let pinchStartScale = 1
let lastX = 0
let lastY = 0
let downX = 0
let downY = 0
let moved = false
let lastTap = 0

function onTouchStart(e: TouchEvent) {
  animating.value = false
  moved = false
  for (const t of Array.from(e.touches)) touches.set(t.identifier, { x: t.clientX, y: t.clientY })
  downX = e.touches[0].clientX
  downY = e.touches[0].clientY
  lastX = downX
  lastY = downY
  if (e.touches.length === 2) {
    pinchDist = Math.hypot(e.touches[0].clientX - e.touches[1].clientX, e.touches[0].clientY - e.touches[1].clientY)
    pinchStartScale = scale.value
  }
}
function onTouchMove(e: TouchEvent) {
  const arr = Array.from(e.touches)
  if (arr.length === 2) {
    const d = Math.hypot(arr[0].clientX - arr[1].clientX, arr[0].clientY - arr[1].clientY)
    if (pinchDist > 0) {
      scale.value = clamp(pinchStartScale * (d / pinchDist), 1, 6)
      if (e.cancelable) e.preventDefault()
    }
    return
  }
  if (arr.length === 1 && isZoomed.value) {
    const p = arr[0]
    const dx = p.clientX - lastX
    const dy = p.clientY - lastY
    tx.value += dx
    ty.value += dy
    lastX = p.clientX
    lastY = p.clientY
    if (Math.abs(dx) + Math.abs(dy) > 2) moved = true
    if (e.cancelable) e.preventDefault()
  }
}
function onTouchEnd(e: TouchEvent) {
  const arr = Array.from(e.changedTouches)
  const pointer = arr[0]
  if (pointer && !isZoomed.value) {
    const dx = pointer.clientX - downX
    const dy = pointer.clientY - downY
    if (Math.abs(dx) > 60 && Math.abs(dx) > Math.abs(dy)) {
      go(dx < 0 ? 1 : -1)
      moved = true
    }
  }
  for (const t of arr) touches.delete(t.identifier)
  if (touches.size === 0) {
    // 双击（两次 <300ms 的单击）
    const now = Date.now()
    if (!moved && now - lastTap < 300) {
      togglleZoom()
      lastTap = 0
    } else if (!moved) {
      lastTap = now
    } else {
      lastTap = 0
    }
  }
}

// 桌面：双击 / 滚轮 / 键盘
function onDblClick(e: MouseEvent) { e.preventDefault(); togglleZoom() }
function onWheel(e: WheelEvent) {
  e.preventDefault()
  scale.value = clamp(scale.value * (1 - e.deltaY * 0.002), 1, 4)
  if (scale.value <= 1.01) { tx.value = 0; ty.value = 0 }
}
function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') close()
  else if (e.key === 'ArrowLeft') go(-1)
  else if (e.key === 'ArrowRight') go(1)
}
function close() { emit('close') }

onMounted(() => {
  window.addEventListener('keydown', onKey)
})
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKey)
})

const transformStyle = computed(() => ({
  transform: `translate3d(${tx.value}px, ${ty.value}px, 0) scale(${scale.value})`,
  transition: animating.value ? 'transform .18s ease' : 'none',
}))
</script>

<template>
  <Teleport to="body">
    <div class="mv" @touchstart.passive="onTouchStart" @touchmove.passive="onTouchMove" @touchend.passive="onTouchEnd" @touchcancel.passive="onTouchEnd">
      <!-- 顶部（iOS：✕ 左 + 中央计数） -->
      <div class="mv-top">
        <button class="mv-close" aria-label="关闭" @click="close"><X :size="24" stroke-width="2.5" /></button>
        <span class="mv-counter">{{ idx + 1 }} / {{ total }}</span>
        <span class="mv-album" v-if="mode === 'album' && albumName">{{ albumName }}</span>
      </div>

      <!-- 媒体主体 -->
      <div class="mv-stage" @touchstart.passive="onTouchStart" @touchmove.passive="onTouchMove" @touchend.passive="onTouchEnd" @touchcancel.passive="onTouchEnd">
        <video
          v-if="cur.kind === 'video'"
          class="mv-media mv-video"
          :src="fullUrl(mediaUrl(cur).raw)"
          playsinline controls autoplay
        />
        <div v-else-if="cur.kind === 'audio'" class="mv-media mv-audio">
          <Music :size="56" stroke-width="1.4" />
          <audio :src="fullUrl(mediaUrl(cur).raw)" controls autoplay />
        </div>
        <img
          v-else
          class="mv-media mv-img"
          :src="fullUrl(mediaUrl(cur).raw)"
          :style="transformStyle"
          alt=""
          @dblclick.prevent="onDblClick"
          @wheel.prevent="onWheel"
        />
      </div>

      <!-- 左右切换（触屏可滑动；桌面可用半透明箭头） -->
      <button v-if="idx > 0" class="mv-nav left" aria-label="上一张" @click="go(-1)"><ChevronLeft :size="30" /></button>
      <button v-if="idx < total - 1" class="mv-nav right" aria-label="下一张" @click="go(1)"><ChevronRight :size="30" /></button>

      <!-- 底部操作（iOS 玻璃 pill） -->
      <div class="mv-bar">
        <button v-if="mode === 'timeline' && cur.post_id" class="pv-btn" @click="emit('gopost', cur.post_id)">
          查看动态
        </button>
        <button v-else-if="mode === 'album'" class="pv-btn danger" @click="emit('remove', cur.id)">
          <Trash2 :size="15" stroke-width="2" /> 移出当前相册
        </button>
        <span v-else class="mv-hint">单击媒体可放大 / 左右滑动切换</span>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
/* ===== iOS 沉浸式全屏查看器（纯黑） ===== */
.mv {
  position: fixed;
  inset: 0;
  z-index: 400;
  background: #000;
  color: #fff;
  touch-action: none;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}
.mv-top {
  position: absolute;
  inset-inline: 0;
  top: 0;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: calc(8px + env(safe-area-inset-top)) 14px 8px;
  z-index: 3;
}
.mv-close {
  border: none;
  background: none;
  color: #fff;
  padding: 6px;
  display: flex;
  align-items: center;
  cursor: pointer;
  opacity: .92;
}
.mv-counter {
  flex: 1;
  font-size: 14px;
  font-weight: 500;
  letter-spacing: .2px;
  opacity: .85;
}
.mv-album {
  flex-shrink: 1;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 13px;
  opacity: .65;
}
.mv-stage {
  flex: 1;
  min-height: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
}
.mv-media {
  max-width: 100%;
  max-height: 100%;
  object-fit: contain;
}
.mv-img {
  will-change: transform;
  user-select: none;
  -webkit-user-select: none;
}
.mv-video {
  width: 100%;
  height: 100%;
  object-fit: contain;
  background: #000;
}
.mv-audio {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 14px;
  color: rgba(255, 255, 255, 0.85);
}
.mv-audio audio {
  width: 78vw;
}
.mv-bottom {
  position: absolute;
  inset-inline: 0;
  bottom: 0;
  display: flex;
  justify-content: center;
  padding: 10px 0 calc(18px + env(safe-area-inset-bottom));
  z-index: 3;
}
.pv-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  min-width: 150px;
  border: none;
  border-radius: 999px;
  padding: 12px 20px;
  font-size: 15px;
  font-weight: 600;
  color: var(--ios-blue);
  background: rgba(255, 255, 255, 0.18);
  backdrop-filter: blur(20px);
  -webkit-backdrop-filter: blur(20px);
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.35);
  cursor: pointer;
}
.pv-btn.danger {
  color: #ff6b6b;
}
.mv-hint {
  font-size: 13px;
  color: rgba(255, 255, 255, 0.75);
  background: rgba(0, 0, 0, 0.4);
  border-radius: 999px;
  padding: 8px 16px;
}
.mv-nav {
  position: absolute;
  top: 50%;
  transform: translateY(-50%);
  border: none;
  background: rgba(0, 0, 0, 0.45);
  color: #fff;
  width: 40px;
  height: 40px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  z-index: 3;
  opacity: .85;
}
.mv-nav.left { left: 10px; }
.mv-nav.right { right: 10px; }
</style>
