<script setup lang="ts">
import { FolderPlus, Images } from 'lucide-vue-next'
import Sheet from '../Sheet.vue'
import EmptyState from '../EmptyState.vue'
import type { AlbumItem } from '../../types/albums'

defineProps<{
  showCreate: boolean
  createName: string
  createDesc: string
  showPickAlbum: boolean
  albums: AlbumItem[]
  addingAlbum: boolean
  pickNewName: string
  pickErr: string
}>()

const emit = defineEmits<{
  (e: 'update:showCreate', v: boolean): void
  (e: 'update:createName', v: string): void
  (e: 'update:createDesc', v: string): void
  (e: 'update:showPickAlbum', v: boolean): void
  (e: 'update:pickNewName', v: string): void
  (e: 'create'): void
  (e: 'add-to-album', id: number): void
  (e: 'create-and-add'): void
}>()
</script>

<template>
  <!-- C2 手动相册创建 sheet -->
  <Sheet
    :show="showCreate"
    title="创建手动相册"
    confirm-text="创建"
    :confirm-disabled="!createName.trim()"
    @update:show="emit('update:showCreate', $event)"
    @confirm="emit('create')"
  >
    <input
      :value="createName"
      class="create-input"
      placeholder="相册名称"
      maxlength="20"
      @input="emit('update:createName', ($event.target as HTMLInputElement).value)"
      @keyup.enter="emit('create')"
    />
    <input
      :value="createDesc"
      class="create-input"
      placeholder="描述（选填）"
      maxlength="50"
      @input="emit('update:createDesc', ($event.target as HTMLInputElement).value)"
    />
  </Sheet>

  <!-- F2 加入相册（时间轴多选） -->
  <Sheet
    :show="showPickAlbum"
    title="加入哪个相册？"
    confirm-text="关闭"
    @update:show="emit('update:showPickAlbum', $event)"
  >
    <div v-if="albums.filter((a) => !a.is_hidden).length" class="pk-list">
      <button
        v-for="a in albums.filter((x) => !x.is_hidden)"
        :key="a.id"
        class="pk-row"
        :disabled="addingAlbum"
        @click="emit('add-to-album', a.id)"
      >
        <span class="pk-ic"><Images :size="16" stroke-width="1.8" /></span>
        <span class="pk-name">{{ a.name }}</span>
        <b class="pk-add">加入</b>
      </button>
    </div>
    <EmptyState v-else compact :icon="Images" title="先建一个相册吧" sub="建好就能把这批照片装进去。" />

    <div class="pk-new">
      <input
        :value="pickNewName"
        class="pk-new-input"
        placeholder="或新建相册并加入"
        maxlength="20"
        @input="emit('update:pickNewName', ($event.target as HTMLInputElement).value)"
        @keyup.enter="emit('create-and-add')"
      />
      <button class="pk-new-go" :disabled="addingAlbum || !pickNewName.trim()" @click="emit('create-and-add')">
        <FolderPlus :size="16" stroke-width="2" />
      </button>
    </div>
    <p v-if="pickErr" class="pk-err">{{ pickErr }}</p>
  </Sheet>
</template>

<style scoped>
.create-input {
  width: 100%;
  border: 1px solid var(--rule);
  border-radius: 10px;
  padding: 10px 12px;
  font-size: 14px;
  background: var(--bg, #fff);
  color: var(--ink, #222);
  outline: none;
}

.pk-row {
  display: flex;
  align-items: center;
  gap: 10px;
  border: none;
  background: var(--bg, #f5f5f7);
  border-radius: 10px;
  padding: 10px 12px;
  font-size: 14px;
  color: var(--ink);
  cursor: pointer;
  text-align: left;
}

.pk-new-input {
  flex: 1;
  border: 1px solid var(--rule);
  border-radius: 10px;
  padding: 9px 12px;
  font-size: 14px;
  background: var(--bg, #fff);
  color: var(--ink);
  outline: none;
}

.pk-new-go {
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
.create-input + .create-input { margin-top: 8px; }
.create-input:focus { border-color: var(--primary, #ff8fab); }
.pk-list { display: flex; flex-direction: column; gap: 6px; max-height: 40vh; overflow-y: auto; }
.pk-row:disabled { opacity: 0.5; }
.pk-ic { color: var(--primary-deep); display: flex; }
.pk-name { flex: 1; }
.pk-add { color: var(--primary-deep); font-weight: 600; font-size: 13px; }
.pk-new { display: flex; gap: 6px; margin-top: 4px; }
.pk-new-go:disabled { opacity: 0.4; }
.pk-err { color: var(--danger); font-size: 13px; }
</style>
