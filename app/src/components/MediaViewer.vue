<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { AlertTriangle, ChevronLeft, ChevronRight, Download, Image as ImageIcon, Music, Pause, Play, Trash2, Video, X } from 'lucide-vue-next'
import { fullUrl } from '../api/client'
import { mediaUrl, refreshMediaUrl } from '../utils/mediaUrls'
import { mediaKindLabel } from '../utils/mediaKind'
import { saveMedia } from '../utils/saveMedia'

export interface ViewerItem {
  id: number | string
  kind: string
  thumb_url: string
  url?: string
  post_id?: number | null
  taken_at?: string
  duration?: number
  /** 动态内序号：只有调用方确定时才有值（服务端给不出文件名时用于兜底命名） */
  seq?: number
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

// —— 另存到本机（手机优先走系统「另存为」，见 utils/saveMedia.ts）——
const saveState = ref<'idle' | 'saving' | 'saved' | 'failed'>('idle')
const saveMsg = ref('')
let saveTimer: number | null = null

async function onSave() {
  if (saveState.value === 'saving') return
  const item = cur.value
  if (!item) return
  saveState.value = 'saving'
  saveMsg.value = ''
  const out = await saveMedia({
    id: item.id,
    kind: item.kind,
    url: mediaUrl(item).raw,
    taken_at: item.taken_at,
    // 只有 post 模式（items 就是该动态的媒体）下标才等于「动态内序号」；
    // 时间轴/相册模式下标是查看器自己的位置，不能拿来命名，留空 → 兜底用媒体 id
    seq: props.mode === 'post' ? idx.value + 1 : undefined,
    onProgress: (loaded, total) => {
      if (total > 0) saveMsg.value = '保存中 ' + Math.round((loaded / total) * 100) + '%'
    },
    refresh: () => refreshMediaUrl(item.id),
  })
  if (out.status === 'saved') {
    saveState.value = 'saved'
    saveMsg.value = '已保存'
  } else if (out.status === 'cancelled') {
    saveState.value = 'idle'
    saveMsg.value = ''
  } else {
    saveState.value = 'failed'
    saveMsg.value = out.message ?? '保存失败'
  }
  if (saveTimer !== null) window.clearTimeout(saveTimer)
  saveTimer = window.setTimeout(() => {
    saveState.value = 'idle'
    saveMsg.value = ''
  }, 2600)
}

// —— 三类媒体：照片（缩放）/ 视频（点击播放）/ 录音（音频卡片）——
// 播放器元素：v-if 分支里同时只存在一个，共用同一个 ref
const playerEl = ref<HTMLVideoElement | HTMLAudioElement | null>(null)
const playing = ref(false)
const failed = ref(false)
const mediaDuration = ref(0)

const kindLabel = computed(() => mediaKindLabel(cur.value?.kind ?? ''))
const hintText = computed(() => {
  if (failed.value) return '无法播放，可先「保存」到本机'
  return cur.value?.kind === 'photo' ? '单击媒体可放大 / 左右滑动切换' : '点击播放 / 左右滑动切换'
})
const durationText = computed(() => {
  const d = mediaDuration.value || cur.value?.duration || 0
  if (!d || !isFinite(d)) return ''
  const m = Math.floor(d / 60)
  const s = Math.round(d % 60)
  return m + ':' + String(s).padStart(2, '0')
})

// 点了才播：浏览器会拦掉带声音的自动播放，点击是用户手势，可以直接出声
function toggleMedia() {
  const el = playerEl.value
  if (!el) return
  if (!el.paused) { el.pause(); return }
  const p = el.play()
  if (p && typeof p.catch === 'function') p.catch(() => { playing.value = false; failed.value = true })
}
// 切条目 / 关闭时停掉上一条，避免换图后背景还在播
function stopMedia() {
  const el = playerEl.value
  if (el && !el.paused) el.pause()
  playing.value = false
  failed.value = false
  mediaDuration.value = 0
}
function onMeta(e: Event) {
  const el = e.target as HTMLMediaElement | null
  mediaDuration.value = el && isFinite(el.duration) ? el.duration : 0
}
function onMediaError() {
  playing.value = false
  failed.value = true
}

function resetView() {
  scale.value = 1
  tx.value = 0
  ty.value = 0
}
function go(delta: number) {
  const n = idx.value + delta
  if (n < 0 || n >= total.value) return
  stopMedia()
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
function close() { stopMedia(); emit('close') }

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
      <!-- 顶部（iOS：✕ 左 + 中央计数 + 类型标识） -->
      <div class="mv-top">
        <button class="mv-close" aria-label="关闭" @click="close"><X :size="24" stroke-width="2.5" /></button>
        <span class="mv-counter">{{ idx + 1 }} / {{ total }}</span>
        <span class="mv-kind">
          <ImageIcon v-if="cur.kind === 'photo'" :size="12" stroke-width="2.2" />
          <Video v-else-if="cur.kind === 'video'" :size="12" stroke-width="2.2" />
          <Music v-else :size="12" stroke-width="2.2" />
          {{ kindLabel }}
        </span>
        <span class="mv-album" v-if="mode === 'album' && albumName">{{ albumName }}</span>
      </div>

      <!-- 媒体主体：视频（点击播放）/ 录音（音频卡片）/ 照片（可缩放） -->
      <div class="mv-stage" @touchstart.passive="onTouchStart" @touchmove.passive="onTouchMove" @touchend.passive="onTouchEnd" @touchcancel.passive="onTouchEnd">
        <template v-if="cur.kind === 'video'">
          <video
            ref="playerEl"
            class="mv-media mv-video"
            :src="fullUrl(mediaUrl(cur).raw)"
            playsinline
            controls
            @play="playing = true"
            @pause="playing = false"
            @ended="playing = false"
            @loadedmetadata="onMeta"
            @error="onMediaError"
          />
          <button v-if="!playing && !failed" class="mv-play" aria-label="播放视频" @click="toggleMedia">
            <Play :size="30" fill="currentColor" stroke-width="0" />
          </button>
          <div v-if="failed" class="mv-fail">
            <AlertTriangle :size="26" stroke-width="1.8" />
            <p>无法播放这段视频</p>
            <span>文件可能已损坏或格式不受支持；可以先「保存」到本机再打开。</span>
          </div>
        </template>

        <template v-else-if="cur.kind === 'audio'">
          <div class="mv-audio">
            <div class="mv-audio-cover"><Music :size="40" stroke-width="1.3" /></div>
            <div class="mv-audio-title">录音</div>
            <div class="mv-audio-meta">
              <span v-if="cur.taken_at">{{ cur.taken_at }}</span>
              <span v-if="durationText">{{ durationText }}</span>
            </div>
            <button class="mv-play audio" :aria-label="playing ? '暂停录音' : '播放录音'" @click="toggleMedia">
              <Pause v-if="playing" :size="26" fill="currentColor" stroke-width="0" />
              <Play v-else :size="26" fill="currentColor" stroke-width="0" />
            </button>
            <audio
              ref="playerEl"
              class="mv-audio-el"
              :src="fullUrl(mediaUrl(cur).raw)"
              controls
              @play="playing = true"
              @pause="playing = false"
              @ended="playing = false"
              @loadedmetadata="onMeta"
              @error="onMediaError"
            />
            <div v-if="failed" class="mv-fail audio">
              <AlertTriangle :size="24" stroke-width="1.8" />
              <p>无法播放这段录音</p>
              <span>文件可能已损坏或格式不受支持；可以先「保存」到本机再打开。</span>
            </div>
          </div>
        </template>

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

      <!-- 底部操作（iOS 玻璃 pill）：按钮一行，提示 / 保存结果一行 -->
      <div class="mv-bar">
        <div class="mv-actions">
          <button class="pv-btn" :disabled="saveState === 'saving'" @click="onSave">
            <Download :size="15" stroke-width="2" /> {{ saveState === 'saving' ? '保存中…' : '保存' }}
          </button>
          <!-- 时间轴与相册都提供「查看所属动态」（PRODUCT「动态」：三处统一定位入口含相册） -->
          <button v-if="(mode === 'timeline' || mode === 'album') && cur.post_id" class="pv-btn" @click="emit('gopost', cur.post_id)">
            查看动态
          </button>
          <!-- 相册模式：查看所属动态与移出当前相册可并存（两者不是互斥选择） -->
          <button v-if="mode === 'album'" class="pv-btn danger" @click="emit('remove', cur.id)">
            <Trash2 :size="15" stroke-width="2" /> 移出当前相册
          </button>
        </div>
        <p class="mv-note" :class="{ saved: saveState === 'saved' }">
          {{ saveMsg || hintText }}
        </p>
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
/* 桌面档：全屏查看器不逃出应用卡片（与外壳/弹窗共用框架令牌），手机/平板仍整屏 */
@media (min-width: 1280px) {
  .mv {
    top: var(--frame-inset);
    bottom: var(--frame-inset);
    left: 50%;
    right: auto;
    width: min(var(--frame-max), calc(100% - 2 * var(--frame-inset)));
    transform: translateX(-50%);
    border-radius: var(--frame-radius);
  }
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
  position: relative;
  flex: 1;
  min-height: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
}
/* 类型标识：照片 / 视频 / 录音 */
.mv-kind {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  flex: 0 0 auto;
  margin-right: 8px;
  padding: 3px 9px;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.18);
  color: #fff;
  font-size: 12px;
  font-weight: 500;
}
/* 居中大播放按钮（视频）/ 音频卡片里的同款按钮 */
.mv-play {
  position: absolute;
  left: 50%;
  top: 50%;
  transform: translate(-50%, -50%);
  width: 72px;
  height: 72px;
  border: none;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.45);
  color: #fff;
  cursor: pointer;
  backdrop-filter: blur(6px);
  -webkit-backdrop-filter: blur(6px);
}
.mv-play:active { transform: translate(-50%, -50%) scale(0.94); }
.mv-play.audio {
  position: static;
  transform: none;
  width: 62px;
  height: 62px;
  background: rgba(255, 255, 255, 0.16);
}
.mv-play.audio:active { transform: scale(0.94); }
/* 播不了时的兜底（原来是黑屏静默） */
.mv-fail {
  position: absolute;
  left: 50%;
  top: 50%;
  transform: translate(-50%, -50%);
  width: min(320px, 84vw);
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  padding: 18px 16px;
  border-radius: 14px;
  background: rgba(0, 0, 0, 0.5);
  color: rgba(255, 255, 255, 0.86);
  text-align: center;
}
.mv-fail p { margin: 0; font-size: 15px; font-weight: 600; color: #fff; }
.mv-fail span { font-size: 12.5px; line-height: 1.5; opacity: 0.8; }
.mv-fail.audio { position: static; transform: none; width: min(320px, 84vw); }
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
/* 录音：封面 + 标题 + 日期/时长 + 播放按钮 + 原生控件（进度、音量、倍速都白拿） */
.mv-audio {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  color: rgba(255, 255, 255, 0.85);
}
.mv-audio-cover {
  width: 130px;
  height: 130px;
  border-radius: 22px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(135deg, rgba(255, 143, 171, 0.55), rgba(120, 140, 255, 0.5));
  color: #fff;
}
.mv-audio-title { font-size: 16px; font-weight: 600; color: #fff; }
.mv-audio-meta { display: flex; gap: 10px; font-size: 12.5px; opacity: 0.7; }
.mv-audio-el { width: min(360px, 78vw); }
/* 底部操作：按钮一行 + 提示/状态一行。原来按钮与提示并排且会被压扁，
   文字挤成窄条看着错乱；提示常驻一行，保存结果临时顶替它（不再同时出现两个 pill）。 */
.mv-bar {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  box-sizing: border-box;
  width: 100%;
  max-width: 100%;
  padding: 0 12px calc(12px + env(safe-area-inset-bottom));
  flex-shrink: 0; /* 不被查看器纵向 flex 压扁 */
}
.mv-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 10px;
}
.mv-note {
  margin: 0;
  max-width: 100%;
  font-size: 13px;
  color: rgba(255, 255, 255, 0.75);
  background: rgba(0, 0, 0, 0.4);
  border-radius: 999px;
  padding: 8px 16px;
  text-align: center;
}
.mv-note.saved {
  color: #fff;
  font-weight: 600;
  background: rgba(0, 0, 0, 0.55);
}
.pv-btn:disabled {
  opacity: 0.6;
  cursor: default;
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
  color: var(--tint);
  background: rgba(255, 255, 255, 0.18);
  backdrop-filter: blur(20px);
  -webkit-backdrop-filter: blur(20px);
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.35);
  cursor: pointer;
}
.pv-btn.danger {
  color: #ff6b6b;
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
