<script setup lang="ts">
import { FolderOpen, Images } from 'lucide-vue-next'
import { useRouter } from 'vue-router'
import { fullUrl } from '../../api/client'
import EmptyState from '../EmptyState.vue'
import type { AlbumItem } from '../../types/albums'

const router = useRouter()

defineProps<{
  albums: AlbumItem[]
  // 与 useLongPress 的 ref<string | number | null> 对齐（点位用 number，动态用 string）
  pressing: string | number | null
}>()

const emit = defineEmits<{
  (e: 'pointer-down', ev: PointerEvent, id: number, onLongPress: () => void): void
  (e: 'pointer-move', ev: PointerEvent): void
  (e: 'pointer-up'): void
  (e: 'open', album: AlbumItem, ev: MouseEvent): void
  (e: 'request-delete', album: AlbumItem): void
}>()

function longPressDelete(a: AlbumItem, ev: PointerEvent) {
  emit('pointer-down', ev, a.id, () => emit('request-delete', a))
}
</script>

<template>
  <div class="album-collection">
    <div v-if="albums.length === 0" class="ac-empty-wrap">
      <EmptyState :icon="FolderOpen" title="相册集合还空着" sub="按旅行、日常或家人建一个，把照片归归类。" action="新建相册" @action="router.push('/albums?new=1')" />
    </div>
    <div
      v-for="a in albums"
      :key="a.id"
      class="album-col pressable"
      :class="{ hidden: a.is_hidden, pressing: pressing === a.id }"
      @pointerdown="longPressDelete(a, $event)"
      @pointermove="emit('pointer-move', $event)"
      @pointerup="emit('pointer-up')"
      @pointerleave="emit('pointer-up')"
      @pointercancel="emit('pointer-up')"
      @click="emit('open', a, $event)"
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
</template>

<style scoped>
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
:root[data-theme='dark'] .ac-ph {
  background: rgba(255, 143, 171, 0.22);
}
.ac-empty-wrap {
  grid-column: 1 / -1;
}
</style>
