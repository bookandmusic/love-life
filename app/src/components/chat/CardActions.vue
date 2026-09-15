<script setup lang="ts">
import { Check, CheckCircle2, Timer, XCircle } from 'lucide-vue-next'
import type { Bubble, CardItem } from '../../types/chat'



const props = defineProps<{
  card: CardItem
  bubble: Bubble
  describe: (card: CardItem) => string
  remainingMin: (c: CardItem) => number
  isCardExpired: (c: CardItem) => boolean
}>()

const emit = defineEmits<{
  (e: 'confirm', id: number, confirmed: boolean): void
}>()

// 模板里 b.cardID! 由父级保证（cardOf 命中即有 ID）；这里做一次显式收敛，避免非空断言散落
function confirm(confirmed: boolean) {
  const id = props.bubble.cardID
  if (id == null) return
  emit('confirm', id, confirmed)
}
</script>

<template>
  <div class="card" :class="card.status">
    <p class="desc">{{ describe(card) }}</p>
    <template v-if="card.status === 'pending'">
      <div class="btns">
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
  border-radius: 8px;
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

.desc {
  margin: 0 0 0.5rem;
  font-size: 0.875rem;
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
.hint {
  display: flex;
  gap: 0.5rem;
  padding: 0 1rem 0.5rem;
  flex-wrap: wrap;
  background: var(--ai-bg, var(--bg));
}
</style>
