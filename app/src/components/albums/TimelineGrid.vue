<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { Check, Images, Music, Play } from 'lucide-vue-next'
import { fullUrl } from '../../api/client'
import { mediaUrl } from '../../utils/mediaUrls'
import { mediaDurationText } from '../../utils/mediaKind'
import { groupByMonth } from '../../utils/dates'
import SkeletonState from '../SkeletonState.vue'
import EmptyState from '../EmptyState.vue'
import type { MonthGroup, TimelineItem } from '../../types/albums'

const router = useRouter()

const props = defineProps<{
  items: TimelineItem[]
  loading: boolean
  loadingMore: boolean
  hasMore: boolean
  total: number
  selectMode: boolean
  selectedIds: number[]
}>()

const emit = defineEmits<{
  (e: 'toggle-select-mode'): void
  (e: 'toggle-select', id: number): void
  (e: 'open-pick-album'): void
  (e: 'open-viewer', index: number): void
}>()

const groups = computed<MonthGroup[]>(() => groupByMonth(props.items, (m) => m.taken_at))

// monthOffsets: 每个月分组的首条在全局 items 数组中的索引（MediaViewer 的 start 期望全局索引，
// 而模板里 v-for 只拿到月内索引——跨月点击会错位）。按 groupByMonth 的输出顺序累加。
const monthOffsets = computed<Record<string, number>>(() => {
  const offsets: Record<string, number> = {}
  let acc = 0
  for (const g of groups.value) {
    offsets[g.key] = acc
    acc += g.items.length
  }
  return offsets
})

function isSelected(id: number) {
  return props.selectedIds.includes(id)
}
</script>

<template>
  <div class="timeline-wrap">
    <SkeletonState v-if="loading" variant="lines" :rows="4" />
    <div v-else-if="items.length === 0" class="tl-empty-wrap">
      <EmptyState :icon="Images" title="相册还空着" sub="发动态时带上照片，就会自动收进这里。" action="去发动态" @action="router.push('/publish')" />
    </div>
    <template v-else>
      <div v-if="!selectMode" class="tl-toolbar">
        <span class="tl-count">共 {{ total || items.length }} 个媒体</span>
        <button class="tl-sel" @click="emit('toggle-select-mode')">选择</button>
      </div>
      <div v-else class="tl-toolbar sel">
        <span class="tl-count">已选 {{ selectedIds.length }} 个</span>
        <button class="tl-sel ghost" @click="emit('toggle-select-mode')">取消</button>
        <button class="tl-sel go" :disabled="selectedIds.length === 0" @click="emit('open-pick-album')">加入相册</button>
      </div>
      <div v-for="g in groups" :key="g.key" class="month-block">
        <div class="month-label">{{ g.label }}</div>
        <div class="photo-grid">
          <div
            v-for="(m, i) in g.items"
            :key="m.id"
            class="ph-wrap"
            :class="{ selected: isSelected(m.id) }"
            @click="selectMode ? emit('toggle-select', m.id) : emit('open-viewer', monthOffsets[g.key] + i)"
          >
            <img :src="fullUrl(mediaUrl(m).thumb)" loading="lazy" class="ph" alt="" />
            <span v-if="selectMode" class="pick-mark" :class="{ on: isSelected(m.id) }">
              <Check v-if="isSelected(m.id)" :size="13" stroke-width="3" />
            </span>
            <span v-if="m.kind === 'video'" class="video-mark">
              <Play :size="9" fill="currentColor" />
              <!-- 时长角标：解析不出时长时只留播放图标（ARCHITECTURE「媒体上传管线」缩略图行） -->
              <template v-if="mediaDurationText(m.duration)">{{ mediaDurationText(m.duration) }}</template>
            </span>
            <span v-if="m.kind === 'audio'" class="audio-mark">
              <Music :size="9" stroke-width="2.5" />
            </span>
          </div>
        </div>
      </div>
      <p v-if="loadingMore" class="tl-foot">加载中…</p>
      <p v-else-if="!hasMore" class="tl-foot">没有更多了</p>
    </template>
  </div>
</template>

<style scoped>
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

/* 视频角标：播放图标 + 时长（无时长时只剩图标，故用 min-width 撑住圆点） */
.video-mark {
  position: absolute;
  right: 5px;
  bottom: 5px;
  min-width: 18px;
  height: 18px;
  padding: 0 5px;
  box-sizing: border-box;
  border-radius: 9px;
  background: rgba(0, 0, 0, 0.55);
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 3px;
  font-size: 10px;
  line-height: 1;
  font-variant-numeric: tabular-nums;
}

.audio-mark {
  background: rgba(90, 167, 212, 0.75);
}

.tl-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin: 4px 0 10px;
}

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

.tl-foot {
  text-align: center;
  color: var(--muted-ink);
  font-size: 13px;
  padding: 16px 0 4px;
}
.tl-toolbar.sel { justify-content: flex-end; }
.tl-count { font-size: 13px; color: var(--muted-ink); margin-right: auto; }
.tl-sel.go { background: var(--primary); }
.tl-sel.go:disabled { opacity: 0.4; }
.ph-wrap.selected .ph { outline: 2px solid var(--primary); }
.pick-mark.on { background: var(--primary); color: #fff; }
.tl-empty-wrap {
  grid-column: 1 / -1;
}
</style>
