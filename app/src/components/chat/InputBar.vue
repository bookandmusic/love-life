<script setup lang="ts">
import { ref } from 'vue'
import type { PickedMedia } from '../../types/chat'
import { ImagePlus, Music, Send, Square, X } from 'lucide-vue-next'
import { isImage, isVideo } from '../../utils/mediaMime'


defineProps<{
  input: string
  pickedMedia: PickedMedia[]
  busy: boolean
  streaming: boolean
  maxMedia: number
}>()

const emit = defineEmits<{
  (e: 'update:input', v: string): void
  (e: 'pick', ev: Event): void
  (e: 'remove', index: number): void
  (e: 'send'): void
  (e: 'stop'): void
}>()

const inputRef = ref<HTMLInputElement | null>(null)
const mediaInput = ref<HTMLInputElement | null>(null)

// 点选附件走模板内的直接调用（script setup 的函数在模板可用）
function openMediaPicker() {
  mediaInput.value?.click()
}

// 父级需要「聚焦输入框」，经 expose 提供，避免父级直接摸 DOM
defineExpose({
  focus: () => inputRef.value?.focus(),
})

function onInput(e: Event) {
  emit('update:input', (e.target as HTMLInputElement).value)
}
</script>

<template>
  <footer class="input-bar">
    <div v-if="pickedMedia.length" class="media-strip">
      <div v-for="(m, i) in pickedMedia" :key="i" class="media-pick">
        <img v-if="isImage(m.type)" :src="m.data_url" alt="" />
        <video v-else-if="isVideo(m.type)" :src="m.data_url" muted />
        <div v-else class="media-audio"><Music :size="18" stroke-width="1.8" /><span class="audio-name">{{ m.name }}</span></div>
        <button class="rm" aria-label="移除附件" @click="emit('remove', i)"><X :size="12" stroke-width="2" /></button>
      </div>
    </div>
    <div class="compose">
      <button class="media-btn" aria-label="选择图片/视频/音频" :disabled="busy || pickedMedia.length >= maxMedia" @click="openMediaPicker()">
        <ImagePlus :size="20" stroke-width="1.8" />
      </button>
      <input ref="mediaInput" class="file-input" type="file" accept="image/*,video/*,audio/*" multiple @change="emit('pick', $event)" />
      <input ref="inputRef" :value="input" placeholder="说点什么…" :disabled="busy" @input="onInput" @keyup.enter="emit('send')" />
      <button v-if="streaming" class="send stop" aria-label="停止生成" @click="emit('stop')">
        <Square :size="14" stroke-width="2" fill="currentColor" />
      </button>
      <button v-else-if="busy" class="send running" aria-label="进行中" disabled>
        <span class="dot" />进行中
      </button>
      <button v-else class="send" :disabled="!input.trim() && pickedMedia.length === 0" @click="emit('send')">
        <Send :size="16" stroke-width="1.8" />
      </button>
    </div>
  </footer>
</template>

<style scoped>
.input-bar {
  background: var(--surface);
  border-top: 1px solid var(--rule);
  padding-bottom: env(safe-area-inset-bottom);
}

.media-strip {
  display: flex;
  gap: 0.5rem;
  overflow-x: auto;
  padding: 0.6rem 0.8rem 0;
}

.media-pick {
  position: relative;
  flex: 0 0 56px;
  width: 56px;
  height: 56px;
  border-radius: 10px;
  overflow: hidden;
  border: 1px solid var(--rule);
}

.media-pick video {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.media-pick .rm {
  position: absolute;
  top: 2px;
  right: 2px;
  width: 18px;
  height: 18px;
  border-radius: 50%;
  border: none;
  background: rgba(0, 0, 0, 0.55);
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 0;
}

.media-pick .media-audio {
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--bg);
  color: var(--muted-ink);
}

.audio-name {
  font-size: 10px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 60px;
  margin-left: 4px;
}

.compose {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.6rem 0.8rem;
}

.media-btn {
  border: none;
  background: none;
  color: var(--muted-ink);
  padding: 4px;
  display: flex;
  flex: 0 0 auto;
}

.media-btn:disabled {
  opacity: 0.4;
}

.file-input {
  display: none;
}

.compose > input[type='text'],
.input-bar input:not(.file-input) {
  flex: 1;
  min-width: 0;
  border: 1px solid var(--rule);
  border-radius: 999px;
  padding: 0.55rem 1rem;
  font-size: 0.9375rem;
  background: var(--ai-bg, var(--bg));
  color: var(--ink);
}
.media-pick img {
  flex: 0 0 auto;
}
</style>
