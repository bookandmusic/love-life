<script setup lang="ts">
import { ref } from 'vue'
import type { Bubble, SearchItem, PostDetailItem } from '../../types/chat'
import { Sparkles, Search } from 'lucide-vue-next'
import { mediaSrc } from '../../types'
import { isImage, isVideo } from '../../utils/mediaMime'

defineProps<{
  bubbles: Bubble[]
  error: string
  resultLoadingId: number | null
  resultDetail: PostDetailItem | null
}>()

const emit = defineEmits<{
  (e: 'toggle-result', item: SearchItem): void
}>()

// 消息流容器归本组件持有：父级只需「滚到底」这一个能力，经 expose 提供
const streamEl = ref<HTMLElement | null>(null)

function scrollBottom() {
  const el = streamEl.value
  if (el) el.scrollTop = el.scrollHeight
}

defineExpose({ scrollBottom })
</script>

<template>
  <div ref="streamEl" class="stream">
    <p v-if="bubbles.length === 0" class="empty"><Sparkles :size="30" stroke-width="1.2" class="spark" /><br />和我聊聊你们的日常吧</p>
    <template v-for="(b, i) in bubbles" :key="i">
      <div v-if="b.content || b.result" class="row" :class="b.role">
        <span v-if="b.role === 'assistant'" class="ai-avatar" title="AI 助手">
          <Sparkles :size="14" stroke-width="2" />
        </span>
        <div class="bubble" :class="b.role">
        <span v-if="b.role === 'tool'" class="tool-icon"><Search :size="13" stroke-width="1.8" /></span>{{ b.content }}
        <div v-if="b.media?.length" class="bubble-media">
          <template v-for="m in b.media" :key="m.data_url">
            <img v-if="isImage(m.type)" :src="m.data_url" alt="附件" loading="lazy" />
            <video v-else-if="isVideo(m.type)" :src="m.data_url" controls />
            <audio v-else :src="m.data_url" controls class="bubble-audio" />
          </template>
        </div>
        <!-- D2 搜索结果卡片：内联展示，点击在流内展开全文/大图（不做跨模式跳转） -->
        <div v-if="b.result" class="result-list">
          <template v-if="b.result.items.length">
            <div v-for="it in b.result.items" :key="it.id" class="result-card" @click="emit('toggle-result', it)">
              <div class="rc-head">
                <img v-if="it.thumb_url" :src="mediaSrc({ thumb_url: it.thumb_url }, 'thumb')" alt="" loading="lazy" />
                <span class="rc-text">{{ it.content || '（无文字）' }}</span>
              </div>
              <p v-if="resultLoadingId === it.id" class="rf-loading">加载中…</p>
              <div v-if="resultDetail?.id === it.id" class="result-full">
                <p class="rf-text">{{ resultDetail.content || '（无文字）' }}</p>
                <div v-if="resultDetail.media.length" class="rf-media">
                  <img v-for="(m, mi) in resultDetail.media" :key="mi" :src="mediaSrc(m, 'raw')" alt="" loading="lazy" />
                </div>
              </div>
            </div>
          </template>
          <p v-else class="result-empty">没找到相关回忆</p>
        </div>
        </div>
      </div>
      <slot name="card" :bubble="b" />
    </template>
    <p v-if="error" class="error">{{ error }}</p>
  </div>
</template>

<style scoped>
.stream {
  flex: 1;
  overflow-y: auto;
  padding: 0.75rem 1rem;
  background: var(--ai-bg, var(--bg));
  min-height: 50vh;
}

.empty {
  text-align: center;
  color: var(--muted-ink);
  margin-top: 3rem;
}

.spark {
  color: var(--primary-deep);
  opacity: 0.6;
  margin-bottom: 0.6rem;
}

.row {
  display: flex;
  align-items: flex-end;
  gap: 0.4rem;
}

.row.user {
  justify-content: flex-end;
}

.ai-avatar {
  width: 26px;
  height: 26px;
  border-radius: 50%;
  background: linear-gradient(135deg, var(--primary), var(--sem-purple));
  color: #fff;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  margin-bottom: 0.6rem;
}

.result-list {
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
  margin-top: 0.5rem;
}

.result-card {
  border: 1px solid var(--rule);
  border-radius: 10px;
  padding: 0.4rem 0.5rem;
  cursor: pointer;
  background: var(--bg);
}

.rc-head {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.rc-head img {
  width: 42px;
  height: 42px;
  border-radius: 8px;
  object-fit: cover;
  flex-shrink: 0;
}

.rc-text {
  font-size: 0.8125rem;
  color: var(--ink);
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.result-full {
  margin-top: 0.5rem;
  border-top: 1px dashed var(--rule);
  padding-top: 0.5rem;
}

.rf-text {
  margin: 0;
  font-size: 0.8125rem;
  color: var(--ink);
  white-space: pre-wrap;
}

.rf-media {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 0.4rem;
}

.rf-media img {
  width: 108px;
  height: 108px;
  object-fit: cover;
  border-radius: 8px;
}

.result-empty {
  margin: 0.25rem 0 0;
  font-size: 0.75rem;
  color: var(--muted-ink);
}

.bubble {
  max-width: 80%;
  margin-bottom: 0.6rem;
  padding: 0.6rem 0.9rem;
  border-radius: 18px;
  font-size: 0.9375rem;
  white-space: pre-wrap;
  word-break: break-word;
}

.bubble-media {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 0.5rem;
}

.bubble-media img {
  width: 84px;
  height: 84px;
  object-fit: cover;
  border-radius: 8px;
  border: 1px solid rgba(255, 255, 255, 0.25);
}

.bubble.user {
  margin-left: auto;
  background: var(--primary);
  color: #fff;
  border-bottom-right-radius: 6px;
}

.bubble.assistant {
  background: var(--surface);
  color: var(--ink);
  border: 1px solid var(--rule);
  border-bottom-left-radius: 6px;
}

.bubble.tool {
  background: transparent;
  color: var(--muted-ink);
  font-size: 0.8125rem;
  display: flex;
  align-items: center;
  gap: 0.35rem;
}

.tool-icon {
  display: inline-flex;
  color: var(--primary-deep);
  flex-shrink: 0;
}

.bubble-audio {
  display: block;
  max-width: 220px;
  margin-top: 4px;
}

.rf-loading {
  margin: 0.25rem 0 0;
  font-size: 0.75rem;
  color: var(--muted-ink);
}
.error {
  color: var(--danger);
  font-size: 0.8125rem;
  text-align: center;
}
</style>
