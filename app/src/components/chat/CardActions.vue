<script setup lang="ts">
import { computed } from 'vue'
import { Check, CheckCircle2, RefreshCw, Timer, XCircle } from 'lucide-vue-next'
import type { Bubble, CardItem, UIRecord } from '../../types/chat'
import { mediaSrc } from '../../types'

const props = defineProps<{
  card: CardItem
  bubble: Bubble
  remainingMin: (c: CardItem) => number
  isCardExpired: (c: CardItem) => boolean
  /** 本卡有附件上传失败：卡片保持待确认，失败文件可单独重传（AI模式设计「图片处理与存储」） */
  uploadFailed?: boolean
}>()

const body = computed(() => props.card.preview?.body ?? {})
function recs(v: unknown): UIRecord[] {
  return Array.isArray(v) ? (v as UIRecord[]) : []
}
function flag(v: unknown): string {
  return v ? '是' : '否'
}

const emit = defineEmits<{
  (e: 'confirm', id: number, confirmed: boolean): void
  (e: 'retry-media', id: number): void
}>()

// 模板里 b.cardID! 由父级保证（cardOf 命中即有 ID）；这里做一次显式收敛，避免非空断言散落
function confirm(confirmed: boolean) {
  const id = props.bubble.cardID
  if (id == null) return
  emit('confirm', id, confirmed)
}

function retryMedia() {
  const id = props.bubble.cardID
  if (id == null) return
  emit('retry-media', id)
}
</script>

<template>
  <div class="card" :class="card.status">
    <p class="title">{{ card.summary }}</p>
    <div class="items">
      <template v-if="card.preview?.kind === 'post'">
        <p v-if="body.content" class="value">{{ body.content }}</p>
        <p v-if="body.mood" class="meta">心情：{{ body.mood }}</p>
        <p v-if="body.place" class="meta">地点：{{ body.place }}<template v-if="body.place_city"> · {{ body.place_city }}</template></p>
      </template>
      <template v-else-if="card.preview?.kind === 'comment'">
        <p class="value">{{ (body.post as UIRecord | undefined)?.text }}</p>
        <p class="meta">评论：{{ body.content }}</p>
      </template>
      <template v-else-if="card.preview?.kind === 'album'">
        <p class="value">{{ body.name }}<template v-if="body.count"> · {{ body.count }} 张</template></p>
        <p v-if="body.description" class="meta">{{ body.description }}</p>
        <div v-if="recs(body.media).length" class="thumbs">
          <img v-for="m in recs(body.media)" :key="m.id" class="thumb" :src="mediaSrc(m, 'thumb')" alt="" loading="lazy" />
        </div>
      </template>
      <template v-else-if="card.preview?.kind === 'album_members'">
        <p class="value">{{ (body.album as UIRecord | undefined)?.text }}</p>
        <p class="meta">加入 {{ body.add_count ?? 0 }} · 移出 {{ body.remove_count ?? 0 }}</p>
      </template>
      <template v-else-if="card.preview?.kind === 'anniversary'">
        <p class="value">{{ body.name }} · {{ body.date }}</p>
        <p class="meta">{{ body.calendar === 'lunar' ? '农历' : '阳历' }}<template v-if="body.is_leap"> · 闰月</template><template v-if="body.next_solar_date"> · 下次 {{ body.next_solar_date }}</template></p>
        <p v-if="body.remind_days_before != null" class="meta">提前 {{ body.remind_days_before }} 天提醒</p>
      </template>
      <template v-else-if="card.preview?.kind === 'reminder'">
        <p class="value">{{ (body.anniversary as UIRecord | undefined)?.text }}</p>
        <p class="meta">提前 {{ body.from }} 天 → 提前 {{ body.to }} 天</p>
      </template>
      <template v-else-if="card.preview?.kind === 'place'">
        <p class="value">{{ body.name }}</p>
        <p class="meta">{{ body.lat }}, {{ body.lng }}<template v-if="body.visit_date"> · {{ body.visit_date }}</template></p>
        <p v-if="body.description" class="meta">{{ body.description }}</p>
      </template>
      <template v-else-if="card.preview?.kind === 'reaction'">
        <p class="value">{{ (body.post as UIRecord | undefined)?.text }}</p>
        <p v-if="body.like" class="meta">点赞 {{ flag((body.like as { from: boolean }).from) }} → {{ flag((body.like as { to: boolean }).to) }}</p>
        <p v-if="body.favorite" class="meta">收藏 {{ flag((body.favorite as { from: boolean }).from) }} → {{ flag((body.favorite as { to: boolean }).to) }}</p>
      </template>
      <template v-else-if="card.preview?.kind === 'delete'">
        <p class="value">{{ body.text }}</p>
        <p v-if="body.date" class="meta">{{ body.date }}</p>
      </template>
    </div>
    <ul v-if="card.warnings?.length" class="warns">
      <li v-for="(w, i) in card.warnings" :key="i">{{ w }}</li>
    </ul>
    <!-- 本次消息携带的本地待上传附件预览：确认前最后一眼核对（仅展示，确认时才上传落库） -->
    <div v-if="bubble.localMedia?.length" class="local-media">
      <div v-for="(m, i) in bubble.localMedia" :key="i" class="lm">
        <img v-if="m.type.startsWith('image/') && m.previewUrl" :src="m.previewUrl" alt="" loading="lazy" />
        <video v-else-if="m.type.startsWith('video/') && m.previewUrl" :src="m.previewUrl" muted />
        <span v-else class="lm-file">{{ m.name }}</span>
      </div>
    </div>
    <template v-if="card.status === 'pending'">
      <!-- 部分附件上传失败：卡内标出并可单独重传；已成功的不用重传，全部就绪后自动续提交 -->
      <div v-if="uploadFailed" class="retry-row">
        <span class="retry-hint">部分附件上传失败</span>
        <button class="retry" @click="retryMedia">
          <RefreshCw :size="13" stroke-width="2" />重传
        </button>
      </div>
      <!-- 过期后可见不可确认（REQ:419）：只读提示，不再渲染可点按钮 -->
      <div v-if="!isCardExpired(card)" class="btns">
        <button class="ok" @click="confirm(true)">
          <Check :size="15" stroke-width="2" />确认执行
        </button>
        <button class="no" @click="confirm(false)">取消</button>
      </div>
      <p class="hint">
        <template v-if="isCardExpired(card)">已过期，请重新发起</template>
        <template v-else>剩余 {{ remainingMin(card) }} 分钟 · 单次有效</template>
      </p>
    </template>
    <p v-else class="result">
      <template v-if="card.status === 'executed'">
        <span class="res-ok"><CheckCircle2 :size="14" stroke-width="1.8" />已执行</span>
      </template>
      <span v-else-if="card.status === 'cancelled'">已取消</span>
      <span v-else-if="card.status === 'expired'" class="res-warn">
        <Timer :size="14" stroke-width="1.8" />已过期
      </span>
      <span v-else-if="card.status === 'failed'" class="res-err">
        <XCircle :size="14" stroke-width="1.8" />失败
      </span>
      {{ card.result }}
    </p>
  </div>
</template>

<style scoped>
.card {
  background: var(--surface);
  border: 1px solid var(--rule);
  border-radius: 10px; /* 卡片圆角下限（PRODUCT「字体与排版」；与全站卡片主流的 10px 一致） */
  padding: 0.75rem;
  margin-bottom: 0.8rem;
  max-width: 90%;
}

.card.executed {
  border-color: var(--primary);
}

.card.expired {
  opacity: 0.6;
}

.title {
  margin: 0;
  font-size: 0.875rem;
  font-weight: 600;
}
.items {
  display: flex;
  flex-direction: column;
  gap: 2px;
  margin-top: 0.4rem;
}
.value {
  margin: 0;
  font-size: 0.8125rem;
  color: var(--ink);
  white-space: pre-wrap;
  word-break: break-word;
}
.meta {
  margin: 0;
  font-size: 0.75rem;
  color: var(--muted-ink);
}
.thumbs {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-top: 0.3rem;
}
.warns {
  margin: 0.4rem 0 0;
  padding-left: 1.1em;
  font-size: 0.75rem;
  color: var(--danger);
}
.thumb {
  width: 32px;
  height: 32px;
  border-radius: 6px;
  object-fit: cover;
  flex-shrink: 0;
  border: 0.5px solid var(--rule);
}

/* 本地待上传附件预览 */
.local-media {
  display: flex;
  gap: 6px;
  margin-top: 0.5rem;
  flex-wrap: wrap;
}
.local-media .lm {
  width: 44px;
  height: 44px;
  border-radius: 8px;
  overflow: hidden;
  border: 0.5px solid var(--rule);
}
.local-media .lm img,
.local-media .lm video {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.local-media .lm-file {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 100%;
  height: 100%;
  font-size: 9px;
  color: var(--muted-ink);
  word-break: break-all;
  padding: 2px;
  box-sizing: border-box;
}

.btns {
  display: flex;
  gap: 0.5rem;
}

.ok {
  flex: 1;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 0.3rem;
  background: var(--primary);
  color: #fff;
  border: none;
  border-radius: 12px;
  padding: 0.45rem;
  font-weight: 600;
}

.no {
  flex: 1;
  background: none;
  border: 1px solid var(--rule);
  border-radius: 999px;
  padding: 0.45rem;
  color: var(--muted-ink);
}

.result {
  margin: 0.4rem 0 0;
  font-size: 0.75rem;
  color: var(--muted-ink);
}

.result {
  display: flex;
  align-items: center;
  gap: 0.35rem;
  flex-wrap: wrap;
}

.res-ok {
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
  color: var(--primary-deep);
}

.res-warn {
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
  color: var(--muted-ink);
}

.res-err {
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
  color: var(--danger);
}

.card.cancelled,
.card.expired {
  opacity: 0.6;
}
.hint {
  margin: 0.4rem 0 0;
  font-size: 0.75rem;
  color: var(--muted-ink);
}

/* 部分上传失败：重传入口（与确认/取消并排一行之上，避免误点「确认执行」） */
.retry-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  margin-top: 0.5rem;
}
.retry-hint {
  font-size: 0.75rem;
  color: var(--danger);
}
.retry {
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
  background: none;
  border: 1px solid var(--rule);
  border-radius: 999px;
  padding: 0.3rem 0.7rem;
  font-size: 0.75rem;
  color: var(--ink);
}
</style>
