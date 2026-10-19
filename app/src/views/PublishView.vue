<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { useRouter } from 'vue-router'
import { MapPin, Send, ImagePlus, Music, Play, X, Search } from 'lucide-vue-next'
import { api, errText } from '../api/client'
import MEDIA_LIMITS from '../../../server/internal/limits/media.json' with { type: 'json' }
import { uploadMediaFile } from '../utils/uploadMedia'
import AppToast from '../components/AppToast.vue'
import PageLayout from '../components/PageLayout.vue'
import Sheet from '../components/Sheet.vue'
import MiniMapPicker from '../components/MiniMapPicker.vue'

const router = useRouter()

// 文字 / 心情
const content = ref('')
const mood = ref('')
const moodPresets = ['温馨', '开心', '想念', '浪漫', '平静']
const customMood = ref('')

// 媒体（可选）：方格网格预览，最后一个格子是「添加 +」；可追加选择，设有上限
// 上限与服务端同源（见 ARCHITECTURE「媒体上传管线」的数量上限行）
const MAX_FILES = MEDIA_LIMITS.maxMediaPerPost
const files = ref<File[]>([])
const previews = ref<{ url: string; kind: string }[]>([])
const error = ref('')
const toast = ref('')
function showToast(msg: string) {
  toast.value = msg
}

// —— 提交时才上传 + 整批把关（PRODUCT「发布动态」「提交时才上传」，与 AI 模式同一规则）——
// 选择阶段仅本机 blob 预览、零网络请求；点 [发布] 后逐个上传，全部就绪自动发创建请求。
type UpState = 'pending' | 'uploading' | 'ok' | 'failed'
const upStates = ref<UpState[]>([])
const tokens = ref<(string | null)[]>([])
const phase = ref<'idle' | 'uploading' | 'partial' | 'submitting'>('idle')
const uploading = computed(() => phase.value === 'uploading' || phase.value === 'submitting')
const failedCount = computed(() => upStates.value.filter((s) => s === 'failed').length)

function resetUploadState() {
  upStates.value = files.value.map(() => 'pending')
  tokens.value = files.value.map(() => null)
}

async function uploadOne(i: number): Promise<boolean> {
  upStates.value[i] = 'uploading'
  try {
    // 分片上传（含断点续传与重试策略）统一在 utils/uploadMedia：与 AI 模式同一实现
    const up = await uploadMediaFile(files.value[i]!)
    tokens.value[i] = up.media_token
    upStates.value[i] = 'ok'
    return true
  } catch {
    upStates.value[i] = 'failed'
    return false
  }
}

// 手动重传单个失败文件；全部就绪后自动续提交（无需再点发布）
async function retryOne(i: number) {
  if (phase.value !== 'partial') return
  phase.value = 'uploading'
  const okFlag = await uploadOne(i)
  if (!okFlag) {
    phase.value = 'partial'
    return
  }
  if (tokens.value.every((t) => t !== null)) await finalize()
  else phase.value = 'partial'
}

// 放弃 / 取消 / 关闭：已上传 token 立即撤销（fire-and-forget，失败静默——有 24h 兜底）
function discardUploaded() {
  const ts = tokens.value.filter((t): t is string => t !== null)
  if (ts.length === 0) return
  void api('/api/v1/media/discard', { method: 'POST', body: JSON.stringify({ tokens: ts }) }).catch(() => {})
}
const acceptTypes = 'image/jpeg,image/png,image/webp,video/mp4,video/quicktime,video/webm,audio/mpeg,audio/mp4,audio/wav,audio/x-wav'

// 地点（可选）：geocode 候选列表选择 + 地图点选兜底
const loc = ref<{ name: string; lat: number; lng: number } | null>(null)
const showLocSheet = ref(false)
const locSearch = ref('')
const searching = ref(false)
const geoList = ref<Array<{ name: string; lat: number; lng: number; display: string }>>([])

const moodValue = computed(() => customMood.value || mood.value)

// 阶段5.3：文件追加（重新选择追加而非替换）+ 上限提示
function pickFiles(e: Event) {
  const input = e.target as HTMLInputElement
  const arr = Array.from(input.files ?? [])
  for (const f of arr) {
    if (files.value.length >= MAX_FILES) {
      showToast(`最多选择 ${MAX_FILES} 个文件`)
      break
    }
    let kind = 'image'
    if (f.type.startsWith('video')) kind = 'video'
    else if (f.type.startsWith('audio')) kind = 'audio'
    files.value.push(f)
    previews.value.push({ url: URL.createObjectURL(f), kind })
  }
  input.value = ''
}

const atLimit = computed(() => files.value.length >= MAX_FILES)

function removeFile(i: number) {
  // 已上传成功的文件被移除：立即撤销其临时文件
  const tk = tokens.value[i]
  if (tk) void api('/api/v1/media/discard', { method: 'POST', body: JSON.stringify({ tokens: [tk] }) }).catch(() => {})
  URL.revokeObjectURL(previews.value[i].url)
  previews.value.splice(i, 1)
  files.value.splice(i, 1)
  if (upStates.value.length) {
    upStates.value.splice(i, 1)
    tokens.value.splice(i, 1)
  }
}

// 把关态：一键重传全部失败文件（就绪文件不重来）
async function retryAll() {
  if (phase.value !== 'partial') return
  phase.value = 'uploading'
  for (let i = 0; i < files.value.length; i++) {
    if (upStates.value[i] === 'failed') await uploadOne(i)
  }
  if (tokens.value.some((t) => t === null)) {
    phase.value = 'partial'
    return
  }
  await finalize()
}

async function searchLocation() {
  const q = locSearch.value.trim()
  if (!q) return
  searching.value = true
  try {
    const res = await api<{ candidates: Array<{ name: string; lat: number; lng: number; display: string }> }>(
      `/api/v1/geocode?q=${encodeURIComponent(q)}`,
    )
    geoList.value = res.candidates ?? []
  } catch {
    geoList.value = []
  } finally {
    searching.value = false
  }
}

function openLocSheet() {
  showLocSheet.value = true
  geoList.value = []
  locSearch.value = ''
  mapPicked.value = null
}

function onCandidatePick(c: { name: string; lat: number; lng: number }) {
  loc.value = { name: c.name, lat: c.lat, lng: c.lng }
  showLocSheet.value = false
}

// 地图选点兜底
const mapPicked = ref<{ lat: number; lng: number } | null>(null)
function onMapPick(pos: { lat: number; lng: number }) {
  mapPicked.value = pos
}
function applyMapPick() {
  if (!mapPicked.value) {
    // 未点选则给默认提示
    return
  }
  const name = locSearch.value.trim() || `地点(${mapPicked.value.lat.toFixed(3)},${mapPicked.value.lng.toFixed(3)})`
  loc.value = { name, lat: mapPicked.value.lat, lng: mapPicked.value.lng }
  showLocSheet.value = false
}

function clearLocation() {
  loc.value = null
}

onBeforeUnmount(() => {
  // 放弃发布：未随动态发出的已上传临时文件立即撤销，服务端零驻留
  discardUploaded()
  previews.value.forEach((p) => URL.revokeObjectURL(p.url))
})

const canSubmit = computed(
  () =>
    phase.value === 'idle' &&
    (content.value.trim() !== '' || files.value.length > 0 || !!loc.value || moodValue.value !== ''),
)

async function submit() {
  if (!canSubmit.value) return
  error.value = ''
  resetUploadState()
  phase.value = 'uploading'
  // 逐个上传（单文件独立重试，互不影响）；个别失败不半发——转入把关态等重传
  for (let i = 0; i < files.value.length; i++) {
    if (tokens.value[i] !== null) continue // 重传场景：已就绪的文件不重来
    await uploadOne(i)
  }
  if (tokens.value.some((t) => t === null)) {
    phase.value = 'partial'
    return
  }
  await finalize()
}

// 全部就绪：自动发出创建请求（无需二次点击）
async function finalize() {
  phase.value = 'submitting'
  try {
    const body: Record<string, unknown> = {
      content: content.value,
      mood: moodValue.value,
      media_tokens: tokens.value,
    }
    if (loc.value) body.location = { name: loc.value.name, lat: loc.value.lat, lng: loc.value.lng }
    await api('/api/v1/posts', { method: 'POST', body: JSON.stringify(body) })
    router.replace('/posts')
  } catch (e) {
    // 服务端整批把关失败（token 失效等）：整体重来，不产生缺图半条
    phase.value = 'idle'
    resetUploadState()
    error.value = errText(e, '发布失败，请重试')
  }
}
</script>

<template>
  <PageLayout title="记动态" back @back="router.back()">
    <template #nav-right>
      <button class="primary" :disabled="!canSubmit" @click="submit">
        <Send :size="15" stroke-width="1.8" />{{ phase === 'submitting' ? '发布中…' : uploading ? '上传中…' : '发布' }}
      </button>
    </template>
    <div class="body">
      <textarea v-model="content" placeholder="记录这一刻…（可留空，直接传照片也行）" rows="6" class="field area" />

      <!-- 心情 -->
      <div class="mood-row">
        <button
          v-for="m in moodPresets"
          :key="m"
          class="chip"
          :class="{ on: mood === m && !customMood }"
          @click="mood = mood === m ? '' : m"
        >
          {{ m }}
        </button>
        <input v-model="customMood" placeholder="自定义心情" class="custom" :class="{ on: !!customMood }" />
      </div>

      <!-- 媒体网格：已选为方格，最后一个格子是「添加 +」 -->
      <div class="previews">
        <div v-for="(p, i) in previews" :key="i" class="pv" :class="{ failed: upStates[i] === 'failed' }">
          <img v-if="p.kind === 'image'" :src="p.url" alt="" />
          <div v-else-if="p.kind === 'video'" class="pv-v"><Play :size="24" fill="currentColor" /></div>
          <div v-else class="pv-v pv-audio"><Music :size="24" stroke-width="1.8" /></div>
          <button class="pv-x" aria-label="移除" @click="removeFile(i)">
            <X :size="13" stroke-width="2.5" />
          </button>
          <span v-if="upStates[i] === 'uploading'" class="pv-state"><span class="glow" /></span>
          <span v-else-if="upStates[i] === 'failed'" class="pv-state fail-tag">失败</span>
          <button v-if="upStates[i] === 'failed'" class="pv-retry" @click="retryOne(i)">重试</button>
        </div>
        <label v-if="!atLimit" class="add-tile" :class="{ single: previews.length === 0 }">
          <input type="file" multiple :accept="acceptTypes" @change="pickFiles" />
          <ImagePlus :size="20" stroke-width="1.8" />
          <span>添加</span>
        </label>
      </div>

      <p v-if="previews.length" class="pick-hint">
        共 {{ previews.length }} 个（最多 {{ MAX_FILES }} 个）{{ uploading ? ` · 上传中 ${tokens.filter((t) => t !== null).length}/${files.length}` : '' }}
        <template v-if="phase === 'partial'">
          · {{ failedCount }} 个失败
          <button class="retry-all" @click="retryAll">全部重传</button>
        </template>
      </p>

      <!-- 地点 -->
      <div class="loc-row">
        <button v-if="!loc" class="loc-btn" @click="openLocSheet">
          <MapPin :size="16" stroke-width="1.8" />
          <span>选择地点（可选）</span>
        </button>
        <div v-else class="loc-set">
          <MapPin :size="16" stroke-width="1.8" />
          <span class="loc-name">{{ loc.name }}</span>
          <span class="loc-ll">{{ loc.lat.toFixed(4) }},{{ loc.lng.toFixed(4) }}</span>
          <button class="loc-x" aria-label="清除" @click="clearLocation"><X :size="14" stroke-width="2" /></button>
        </div>
      </div>

      <p v-if="error" class="error">{{ error }}</p>
    </div>

    <!-- 地点选择：候选列表 + 地图选点兜底 -->
    <Sheet v-model:show="showLocSheet" title="选择地点" confirm-text="完成" @confirm="applyMapPick">
      <p class="loc-label">搜索地名</p>
      <div class="loc-search">
        <input v-model="locSearch" class="loc-new" placeholder="如：杭州 / 厦门 · 鼓浪屿" @keyup.enter="searchLocation" />
        <button class="loc-go" :disabled="searching || !locSearch.trim()" @click="searchLocation">
          <span v-if="searching" class="glow" />
          <Search v-else :size="15" stroke-width="1.8" />
        </button>
      </div>
      <div v-if="geoList.length" class="loc-pick-list">
        <button v-for="c in geoList" :key="c.name" class="loc-pick-item" @click="onCandidatePick(c)">
          <MapPin :size="16" stroke-width="1.8" />
          <div class="loc-pick-txt">
            <span>{{ c.name }}</span>
            <small>{{ c.display }} · {{ c.lat.toFixed(3) }},{{ c.lng.toFixed(3) }}</small>
          </div>
        </button>
      </div>
      <p v-else-if="!searching && locSearch.trim()" class="loc-empty">没找到这个地名，在地图上点一下就行</p>

      <p class="loc-label">找不到的话，也可以在地图上点选</p>
      <MiniMapPicker
        :lat="mapPicked?.lat ?? null"
        :lng="mapPicked?.lng ?? null"
        :height="220"
        @pick="onMapPick"
      />
      <p v-if="mapPicked" class="loc-map-val">
        已选：{{ mapPicked.lat.toFixed(6) }}, {{ mapPicked.lng.toFixed(6) }}
      </p>
    </Sheet>

    <AppToast :text="toast" bottom="calc(30px + env(safe-area-inset-bottom))" @done="toast = ''" />
  </PageLayout>
</template>

<style scoped>
.body {
  padding: 12px 0 32px;
}
.field {
  width: 100%;
  box-sizing: border-box;
  border: none;
  border-radius: 12px;
  padding: 13px 14px;
  font-size: 15px;
  font-family: inherit;
  background: var(--surface);
  color: var(--ink);
  margin-bottom: 10px;
  box-shadow: var(--shadow);
}
.field:focus {
  outline: none;
}
.area {
  resize: none;
  min-height: 120px;
  line-height: 1.55;
}
.mood-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 14px;
}
.chip {
  border: none;
  background: var(--surface);
  border-radius: 999px;
  padding: 9px 12px; /* 触控目标 ≥32px */
  font-size: 13px;
  color: var(--muted-ink);
  box-shadow: var(--shadow);
}
.chip.on {
  background: color-mix(in srgb, var(--primary) 14%, transparent);
  color: var(--primary-deep);
  font-weight: 600;
}
:root[data-theme='dark'] .chip.on {
  background: color-mix(in srgb, var(--primary) 18%, transparent);
  color: var(--primary-deep);
}
.custom {
  border: none;
  border-radius: 999px;
  padding: 6px 12px;
  /* flex 而非固定 width：与标签同排时占剩余宽度，被挤到下一行时伸展占满整行
     （固定 100px 换行后右侧会空出 234px） */
  flex: 1 1 100px;
  min-width: 100px;
  font-size: 13px;
  background: var(--surface);
  color: var(--ink);
  box-shadow: var(--shadow);
}
.custom::placeholder {
  color: var(--muted-ink);
}
.custom.on {
  background: color-mix(in srgb, var(--primary) 14%, transparent);
  color: var(--primary-deep);
  font-weight: 600;
  box-shadow: none;
}
:root[data-theme='dark'] .custom.on {
  background: color-mix(in srgb, var(--primary) 18%, transparent);
  color: var(--primary-deep);
}

/* 媒体网格：每个方格 + 末尾「添加」格 */
.previews {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 8px;
  margin-bottom: 10px;
}
.pv,
.add-tile {
  position: relative;
  aspect-ratio: 1;
  border-radius: 10px;
  overflow: hidden;
  background: var(--surface);
  box-shadow: var(--shadow);
}
.add-tile {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 2px;
  color: var(--muted-ink);
  font-size: 12px;
  cursor: pointer;
  border: 1px dashed var(--rule);
}
.add-tile.single {
  width: 100%;
}
.add-tile input {
  display: none;
}
.pv img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}
.pv-v {
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--muted-ink);
  background: linear-gradient(135deg, #f5f5f7, #e8e8ed);
}
:root[data-theme='dark'] .pv-v {
  background: linear-gradient(135deg, #2c2c2e, #1c1c1e);
}
.pv.failed {
  outline: 2px solid var(--danger);
  outline-offset: -2px;
}
.pv-state {
  position: absolute;
  left: 5px;
  bottom: 5px;
  display: inline-flex;
  align-items: center;
  padding: 2px 6px;
  border-radius: 999px;
  background: rgba(0, 0, 0, 0.5);
  color: #fff;
  font-size: 10px;
}
.pv-state.fail-tag {
  background: var(--danger);
}
.pv-retry {
  position: absolute;
  right: 5px;
  bottom: 5px;
  border: none;
  border-radius: 999px;
  padding: 2px 8px;
  background: var(--danger);
  color: #fff;
  font-size: 10px;
  cursor: pointer;
}
.retry-all {
  border: none;
  background: none;
  color: var(--tint);
  font-size: 12px;
  cursor: pointer;
  padding: 0 2px;
}
.pv-x {
  position: absolute;
  top: 5px;
  right: 5px;
  width: 20px;
  height: 20px;
  border-radius: 50%;
  border: none;
  background: rgba(0, 0, 0, 0.5);
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
}
.pv-audio {
  font-size: 22px;
}
/* 导航栏动作 = tint 色文字按钮（iOS HIG） */
.primary {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  background: none;
  color: var(--tint);
  border: none;
  /* 主操作：视觉给足（触控目标 ≥36px），不靠伪元素扩命中区 */
  padding: 8px 12px;
  font-size: 17px;
}
.primary:disabled {
  opacity: 0.4;
}
.primary:active {
  transform: scale(0.97);
}
.error {
  color: var(--danger);
  font-size: 13px;
}
.pick-hint {
  margin-top: 4px;
  font-size: 12px;
  color: var(--muted-ink);
}
.loc-row {
  margin-bottom: 10px;
}
.loc-btn {
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  background: var(--surface);
  border-radius: 12px;
  padding: 12px;
  color: var(--muted-ink);
  font-size: 13px;
  box-shadow: var(--shadow);
  border: none;
  cursor: pointer;
}
.loc-set {
  display: flex;
  align-items: center;
  gap: 6px;
  background: var(--surface);
  border-radius: 12px;
  padding: 10px 12px;
  color: var(--primary-deep);
  font-size: 14px;
  box-shadow: var(--shadow);
}
.loc-name {
  flex: 1;
}
.loc-meta {
  font-size: 12px;
  color: var(--muted-ink);
}
.loc-x {
  border: none;
  background: none;
  color: var(--muted-ink);
  cursor: pointer;
  display: flex;
}
.loc-label {
  font-size: 13px;
  color: var(--muted-ink);
  margin: 4px 0 6px;
}
.loc-search {
  display: flex;
  gap: 6px;
}
.loc-new {
  flex: 1;
  border: 1px solid var(--rule);
  border-radius: 10px;
  padding: 9px 12px;
  font-size: 14px;
  background: var(--bg, #fff);
  color: var(--ink);
  outline: none;
}
.loc-go {
  width: 40px;
  border: none;
  border-radius: 10px;
  background: var(--primary);
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
}
.loc-go:disabled {
  opacity: 0.4;
}
/* 加载态：缓慢呼吸的光晕（PRODUCT「体验规范」明确排除旋转菊花）。
   节奏偏慢（1.8s）与「慢、柔、稳」的整体调性一致；尊重系统的减弱动效偏好。 */
.glow {
  display: inline-block;
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background: var(--tint);
  animation: breathe 1.8s ease-in-out infinite;
}
@keyframes breathe {
  0%,
  100% {
    opacity: 0.25;
    transform: scale(0.8);
  }
  50% {
    opacity: 1;
    transform: scale(1.15);
  }
}
@media (prefers-reduced-motion: reduce) {
  .glow {
    animation: none;
    opacity: 0.7;
  }
}
.loc-pick-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
  max-height: 200px;
  overflow-y: auto;
  margin-top: 8px;
}
.loc-pick-item {
  display: flex;
  align-items: center;
  gap: 8px;
  border: none;
  background: var(--bg, #f5f5f7);
  border-radius: 10px;
  padding: 10px 12px;
  font-size: 14px;
  color: var(--ink);
  cursor: pointer;
  text-align: left;
}
.loc-pick-txt {
  display: flex;
  flex-direction: column;
}
.loc-pick-txt small {
  font-size: 12px;
  color: var(--muted-ink);
}
.loc-empty {
  font-size: 13px;
  color: var(--muted-ink);
  text-align: center;
  margin: 8px 0;
}
.loc-map-val {
  font-size: 12px;
  color: var(--primary-deep);
  text-align: center;
  margin-top: 6px;
}
</style>