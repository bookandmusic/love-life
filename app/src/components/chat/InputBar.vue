<script setup lang="ts">
import { computed, ref } from 'vue'
import type { PickedMedia, QuoteRef } from '../../types/chat'
import { ImagePlus, Music, Plus, Send, Square, X } from 'lucide-vue-next'
import { isImage, isVideo } from '../../utils/mediaMime'


const props = defineProps<{
  input: string
  pickedMedia: PickedMedia[]
  quotes: QuoteRef[]
  busy: boolean
  streaming: boolean
  maxMedia: number
  // AI 不可用原因（后台未配置模型等）：非空时禁用输入/发送并展示提示
  unavailableReason?: string
}>()

// 忙或 AI 不可用时锁定输入区（发送按钮在 busy 时本就不渲染该分支，模板里单列）
const locked = computed(() => props.busy || !!props.unavailableReason)

const emit = defineEmits<{
  (e: 'update:input', v: string): void
  (e: 'pick', ev: Event): void
  (e: 'remove', index: number): void
  (e: 'unquote', index: number): void
  (e: 'send'): void
  (e: 'stop'): void
}>()

// ---------- 输入区动作 ----------
// 命令入口只有一个：输入区左侧「+」展开的动作面板（媒体与命令同屏可选），
// 选中即把模板填入输入框、不自动发送；不做 / 之类需要切符号键盘的入口（PRODUCT「输入与运行控制」）。
const COMMANDS: Array<{ label: string; tpl: string }> = [
  { label: '发动态', tpl: '帮我发一条动态：' },
  { label: '整理照片', tpl: '帮我把最近的照片整理到一个新相册，相册名：' },
  { label: '查回忆', tpl: '帮我查回忆：' },
  { label: '纪念日', tpl: '帮我看一下纪念日：' },
]
const panelMode = ref<'plus' | null>(null)
const cmdIndex = ref(0)
const cmdMatches = computed(() => COMMANDS)
function togglePlus() {
  panelMode.value = panelMode.value === 'plus' ? null : 'plus'
  cmdIndex.value = 0
  if (panelMode.value) inputRef.value?.focus()
}
function applyCommand(tpl: string) {
  emit('update:input', tpl)
  panelMode.value = null
  inputRef.value?.focus()
}
// 面板内键盘 Enter 选中命令后，紧随的 keyup.enter 不得把模板直接发出去（面板键盘操作的防误发护栏）
let suppressEnter = false
function onCmdKey(e: KeyboardEvent) {
  if (panelMode.value === null || cmdMatches.value.length === 0) return
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    cmdIndex.value = (cmdIndex.value + 1) % cmdMatches.value.length
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    cmdIndex.value = (cmdIndex.value + cmdMatches.value.length - 1) % cmdMatches.value.length
  } else if (e.key === 'Enter') {
    e.preventDefault()
    suppressEnter = true
    applyCommand(cmdMatches.value[cmdIndex.value]!.tpl)
  } else if (e.key === 'Escape') {
    panelMode.value = null
  }
}
function onEnter() {
  if (suppressEnter) {
    suppressEnter = false
    return
  }
  onSend()
}
function onSend() {
  panelMode.value = null
  emit('send')
}

const inputRef = ref<HTMLInputElement | null>(null)
const mediaInput = ref<HTMLInputElement | null>(null)

// 点选附件走模板内的直接调用（script setup 的函数在模板可用）
function openMediaPicker() {
  mediaInput.value?.click()
  panelMode.value = null
}

// 父级需要「聚焦输入框」，经 expose 提供，避免父级直接摸 DOM
defineExpose({
  focus: () => inputRef.value?.focus(),
})

function onInput(e: Event) {
  emit('update:input', (e.target as HTMLInputElement).value)
  // 打字即收起面板；面板只由「+」唤起
  if (panelMode.value) {
    panelMode.value = null
    cmdIndex.value = 0
  }
}
</script>

<template>
  <footer class="input-bar">
    <!-- 「+」动作面板：媒体与命令直接点选 -->
    <div v-if="panelMode" class="action-panel">
      <button
        v-if="panelMode === 'plus'"
        class="action-item"
        :disabled="pickedMedia.length >= maxMedia"
        @click="openMediaPicker()"
      >
        <ImagePlus :size="18" stroke-width="1.8" />
        <span>图片 / 视频 / 音频</span>
      </button>
      <p class="panel-label">命令</p>
      <button
        v-for="(c, i) in cmdMatches"
        :key="c.label"
        class="action-item"
        :class="{ hl: i === cmdIndex }"
        @click="applyCommand(c.tpl)"
      >
        <span class="cmd-label">{{ c.label }}</span>
      </button>
    </div>
    <div v-if="quotes.length" class="quote-strip">
      <span v-for="(q, i) in quotes" :key="i" class="quote-chip">
        {{ q.label || '引用' }}
        <button aria-label="移除引用" @click="emit('unquote', i)"><X :size="12" stroke-width="2" /></button>
      </span>
    </div>
    <div v-if="pickedMedia.length" class="media-strip">
      <div v-for="(m, i) in pickedMedia" :key="i" class="media-pick">
        <img v-if="isImage(m.type) && m.previewUrl" :src="m.previewUrl" alt="" />
        <video v-else-if="isVideo(m.type) && m.previewUrl" :src="m.previewUrl" muted />
        <div v-else class="media-audio"><Music :size="18" stroke-width="1.8" /><span class="audio-name">{{ m.name }}</span></div>
        <button class="rm" aria-label="移除附件" @click="emit('remove', i)"><X :size="12" stroke-width="2" /></button>
      </div>
    </div>
    <div class="compose">
      <button class="plus-btn" aria-label="添加内容或命令" :disabled="locked" @click="togglePlus()">
        <Plus :size="22" stroke-width="2" />
      </button>
      <input ref="mediaInput" class="file-input" type="file" accept="image/*,video/*,audio/*" multiple @change="emit('pick', $event)" />
      <input ref="inputRef" :value="input" placeholder="说点什么…" :disabled="locked" @input="onInput" @keydown="onCmdKey" @keyup.enter="onEnter" />
      <button v-if="streaming" class="send stop" aria-label="停止生成" @click="emit('stop')">
        <Square :size="14" stroke-width="2" fill="currentColor" />
      </button>
      <button v-else-if="busy" class="send running" aria-label="进行中" disabled>
        <span class="dot" />进行中
      </button>
      <button v-else class="send" :disabled="!!unavailableReason || (!input.trim() && pickedMedia.length === 0 && quotes.length === 0)" @click="onSend">
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

/* 「+」动作面板：浮在输入区上方，媒体动作 + 命令列表 */
.action-panel {
  display: flex;
  flex-direction: column;
  border-bottom: 0.5px solid var(--rule);
  max-height: 240px;
  overflow-y: auto;
  padding: 0.25rem 0;
}
.action-item {
  display: flex;
  align-items: center;
  gap: 10px;
  border: none;
  background: none;
  text-align: left;
  padding: 0.6rem 0.9rem;
  font-size: 0.875rem;
  cursor: pointer;
  color: var(--ink);
}
.action-item:disabled {
  opacity: 0.4;
}
.action-item.hl,
.action-item:active {
  background: var(--tint-wash);
}
.panel-label {
  margin: 0.25rem 0.9rem 0;
  font-size: 0.6875rem;
  color: var(--muted-ink);
  text-transform: uppercase;
  letter-spacing: 0.05em;
}
.cmd-label {
  color: var(--muted-ink);
  font-size: 0.8125rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.quote-strip {
  display: flex;
  flex-wrap: wrap;
  gap: 0.35rem;
  padding: 0.5rem 0.8rem 0;
}
.quote-chip {
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
  font-size: 0.75rem;
  color: var(--ink);
  background: var(--tint-wash);
  border-radius: 999px;
  padding: 0.15rem 0.35rem 0.15rem 0.55rem;
}
.quote-chip button {
  border: none;
  background: none;
  padding: 0;
  display: inline-flex;
  color: var(--muted-ink);
  cursor: pointer;
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

/* 「+」动作入口（iMessage 同位）：tint 圆形，锁定态置灰 */
.plus-btn {
  border: none;
  background: none;
  color: var(--tint);
  padding: 4px;
  display: flex;
  flex: 0 0 auto;
  border-radius: 50%;
  cursor: pointer;
}
.plus-btn:disabled {
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
.media-pick img,
.media-pick video {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.send {
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--primary);
  color: #fff;
  border: none;
  border-radius: 999px;
  padding: 0 1.2rem;
}
.send.stop {
  background: var(--danger);
  padding: 0 0.9rem;
}
.send.running {
  background: color-mix(in srgb, var(--primary) 55%, transparent);
  padding: 0 0.9rem;
  font-size: 0.75rem;
  gap: 0.3rem;
  cursor: default;
}
.send.running .dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
  animation: chat-dot 1.2s infinite;
}
@keyframes chat-dot {
  0%,
  100% {
    opacity: 0.3;
  }
  50% {
    opacity: 1;
  }
}
.send:disabled {
  opacity: 0.45;
}
</style>
