<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ChevronLeft, MapPin, Send, ImagePlus, Music, Play, X, Search, Loader2 } from 'lucide-vue-next'
import { api, errText } from '../api/client'
import AppToast from '../components/AppToast.vue'
import Sheet from '../components/Sheet.vue'
import MiniMapPicker from '../components/MiniMapPicker.vue'

const router = useRouter()

// 文字 / 心情
const content = ref('')
const mood = ref('')
const moodPresets = ['温馨', '开心', '想念', '浪漫', '平静']
const customMood = ref('')

// 媒体（可选）：方格网格预览，最后一个格子是「添加 +」；阶段5.3：可追加选择，设有上限
const MAX_FILES = 9
const files = ref<File[]>([])
const previews = ref<{ url: string; kind: string }[]>([])
const uploading = ref(false)
const error = ref('')
const toast = ref('')
function showToast(msg: string) {
  toast.value = msg
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
  URL.revokeObjectURL(previews.value[i].url)
  previews.value.splice(i, 1)
  files.value.splice(i, 1)
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
  previews.value.forEach((p) => URL.revokeObjectURL(p.url))
})

const canSubmit = computed(
  () =>
    !uploading.value &&
    (content.value.trim() !== '' || files.value.length > 0 || !!loc.value || moodValue.value !== ''),
)

async function submit() {
  if (!canSubmit.value) return
  uploading.value = true
  error.value = ''
  try {
    const tokens: string[] = []
    for (const f of files.value) {
      const form = new FormData()
      form.append('file', f)
      const up = await api<{ media_token: string }>('/api/v1/media/upload', {
        method: 'POST',
        body: form,
      })
      tokens.push(up.media_token)
    }
    const body: Record<string, unknown> = {
      content: content.value,
      mood: moodValue.value,
      media_tokens: tokens,
    }
    if (loc.value) body.location = { name: loc.value.name, lat: loc.value.lat, lng: loc.value.lng }
    await api('/api/v1/posts', { method: 'POST', body: JSON.stringify(body) })
    router.replace('/posts')
  } catch (e) {
    error.value = errText(e, '发布失败')
  } finally {
    uploading.value = false
  }
}
</script>

<template>
  <section class="publish">
    <header class="navbar glass">
      <button class="nav-btn" aria-label="返回" @click="router.back()">
        <ChevronLeft :size="22" stroke-width="2.5" />
      </button>
      <span class="title">记动态</span>
      <div class="nav-right">
        <button class="primary" :disabled="!canSubmit" @click="submit">
          <Send :size="15" stroke-width="1.8" />{{ uploading ? '发布中…' : '发布' }}
        </button>
      </div>
    </header>

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
        <div v-for="(p, i) in previews" :key="i" class="pv">
          <img v-if="p.kind === 'image'" :src="p.url" alt="" />
          <div v-else-if="p.kind === 'video'" class="pv-v"><Play :size="24" fill="currentColor" /></div>
          <div v-else class="pv-v pv-audio"><Music :size="24" stroke-width="1.8" /></div>
          <button class="pv-x" aria-label="移除" @click="removeFile(i)">
            <X :size="13" stroke-width="2.5" />
          </button>
        </div>
        <label v-if="!atLimit" class="add-tile" :class="{ single: previews.length === 0 }">
          <input type="file" multiple :accept="acceptTypes" @change="pickFiles" />
          <ImagePlus :size="20" stroke-width="1.8" />
          <span>添加</span>
        </label>
      </div>

      <p v-if="previews.length" class="pick-hint">共 {{ previews.length }} 个（最多 {{ MAX_FILES }} 个）</p>

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
          <Loader2 v-if="searching" :size="15" class="spin" />
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
      <p v-else-if="!searching && locSearch.trim()" class="loc-empty">未匹配到，可直接在地图上点选</p>

      <p class="loc-label">或在地图上点选（候选都不满意时）</p>
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
  </section>
</template>

<style scoped>
.publish {
  height: 100%;
  display: flex;
  flex-direction: column;
  min-height: 0;
}
.navbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: calc(10px + env(safe-area-inset-top)) 12px 10px;
  flex-shrink: 0;
  z-index: 10;
}
.nav-right {
  display: flex;
  align-items: center;
  gap: 4px;
  margin-left: auto;
}
.nav-btn {
  border: none;
  background: none;
  color: var(--tint);
  cursor: pointer;
  padding: 4px;
  display: flex;
  align-items: center;
}
.body {
  flex: 1;
  overflow-y: auto;
  padding: 12px 16px 32px;
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
  padding: 6px 12px;
  font-size: 13px;
  color: var(--muted-ink);
  box-shadow: var(--shadow);
}
.chip.on {
  background: rgba(255, 143, 171, 0.14);
  color: var(--primary-deep);
  font-weight: 600;
}
:root[data-theme='dark'] .chip.on {
  background: rgba(255, 143, 171, 0.18);
  color: #ffb4c2;
}
.custom {
  border: none;
  border-radius: 999px;
  padding: 6px 12px;
  width: 100px;
  font-size: 13px;
  background: var(--surface);
  color: var(--ink);
  box-shadow: var(--shadow);
}
.custom::placeholder {
  color: var(--muted-ink);
}
.custom.on {
  background: rgba(255, 143, 171, 0.14);
  color: var(--primary-deep);
  font-weight: 600;
  box-shadow: none;
}
:root[data-theme='dark'] .custom.on {
  background: rgba(255, 143, 171, 0.18);
  color: #ffb4c2;
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
  padding: 4px;
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
.spin {
  animation: spin 1s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
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