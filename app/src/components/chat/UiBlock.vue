<script setup lang="ts">
import type { UIBlock as UIBlockT, UIRecord, UiBlockEmits } from '../../types/chat'
import { mediaSrc } from '../../types'

// 结果卡（AI模式设计「返回值三分离」的 UI 通道）：气泡分支与步骤摘要行共用一份渲染。
// 勾选/展开状态留在父级（ChatView 持有），本组件只透传事件。
defineProps<{
  ui?: UIBlockT
  pickedKeys: string[]
  openKeys: string[]
}>()

const emit = defineEmits<UiBlockEmits>()

function recKey(resultId: string, rec: UIRecord): string {
  if (rec.group_key) return `${resultId}:group:${rec.group_key}`
  return `${resultId}:${rec.id}`
}

function onRec(ui: UIBlockT | undefined, rec: UIRecord) {
  if (!ui) return
  if (rec.thumb_url && (ui.kind === 'media_grid' || rec.kind === 'media')) {
    emit('open-media', rec)
    return
  }
  emit('toggle-open', recKey(ui.result_id || '', rec))
}
</script>

<template>
  <div v-if="ui" class="ui-block" :class="ui.kind">
    <p class="ui-title">{{ ui.title }}</p>
    <!-- 统计概览卡：数字网格 + 两组分布列表；不做图表、不可勾选（PRODUCT「卡片体系」统计概览卡） -->
    <template v-if="ui.kind === 'stats'">
      <div v-if="ui.stats?.length" class="ui-stats">
        <div v-for="(st, si) in ui.stats" :key="si" class="ui-stat">
          <span class="ui-stat-label">{{ st.label }}</span>
          <b class="ui-stat-value">{{ st.value }}</b>
          <span v-if="st.hint" class="ui-stat-hint">{{ st.hint }}</span>
        </div>
      </div>
      <div v-for="(se, si) in ui.series ?? []" :key="si" class="ui-series">
        <p class="ui-series-title">{{ se.title }}</p>
        <p v-if="!se.points.length" class="ui-empty">没有内容</p>
        <ul v-else class="ui-series-list">
          <li v-for="(pt, pi) in se.points" :key="pi" class="ui-series-row">
            <span class="ui-series-label">{{ pt.label }}</span>
            <span class="ui-series-count">{{ pt.count }}</span>
          </li>
        </ul>
      </div>
    </template>
    <!-- records 可能是服务端下发的 null（统计卡 Records 为 nil → "records":null），取值统一走 ?? []，
         否则对 null 取 .length 直接抛 TypeError 让整条消息流渲染失败 -->
    <template v-else-if="(ui.records ?? []).length">
      <div v-for="(rec, ri) in (ui.records ?? [])" :key="ri" class="ui-rec" @click="onRec(ui, rec)">
        <input
          v-if="ui.selectable && ui.result_id"
          class="pick"
          type="checkbox"
          :checked="pickedKeys.includes(recKey(ui.result_id, rec))"
          @click.stop="emit('toggle-pick', ui.result_id, rec, !!ui.single_pick)"
        />
        <img
          v-if="rec.thumb_url"
          class="thumb"
          :src="mediaSrc({ thumb_url: rec.thumb_url }, 'thumb')"
          alt=""
          loading="lazy"
          @click.stop="emit('open-media', rec)"
        />
        <span class="ui-text" :class="{ open: openKeys.includes(recKey(ui.result_id || '', rec)) }">{{ rec.text || '（无文字）' }}</span>
        <span v-if="rec.date" class="ui-meta">{{ rec.date }}</span>
        <span v-if="rec.badge" class="ui-badge">{{ rec.badge }}</span>
      </div>
    </template>
    <p v-else class="ui-empty">没有内容</p>
    <!-- 列表卡计数与翻页：真实总数 + 真实余量（语义检索无稳定总数时不显示假数） -->
    <p v-if="ui.more" class="ui-more">
      <template v-if="ui.total">共 {{ ui.total }} 条 · 还有 {{ ui.more }} 条</template>
      <template v-else>还有 {{ ui.more }} 条</template>
    </p>
    <!-- 语义检索无稳定总数：标注口径而非给假数（PRODUCT「卡片体系」） -->
    <p v-if="ui.semantic" class="ui-more">按相关性截取，非全部命中</p>
    <button
      v-if="ui.more && ui.result_id && ui.selectable"
      class="ui-more-btn"
      type="button"
      @click="emit('load-more', ui)"
    >再看几条</button>
  </div>
</template>

<style scoped>
.ui-block {
  margin-top: 0.45rem;
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
}
.ui-title {
  margin: 0;
  font-size: 0.75rem;
  color: var(--muted-ink);
}
.ui-rec {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  border: 1px solid var(--rule);
  border-radius: 10px;
  padding: 0.35rem 0.45rem;
  background: var(--bg);
  cursor: pointer;
  min-width: 0;
}
/* 统计概览卡：数字网格 + 分布列表，沿用聊天区既有卡片风格（--surface / --rule / 圆角 10px） */
.ui-stats {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.35rem;
}
.ui-stat {
  display: flex;
  flex-direction: column;
  gap: 0.1rem;
  min-width: 0;
  border: 1px solid var(--rule);
  border-radius: 10px;
  padding: 0.35rem 0.45rem;
  background: var(--bg);
}
.ui-stat-label {
  font-size: 0.75rem;
  color: var(--muted-ink);
}
.ui-stat-value {
  font-size: 0.9375rem;
  color: var(--ink);
}
.ui-stat-hint {
  font-size: 0.6875rem;
  color: var(--muted-ink);
}
.ui-series {
  display: flex;
  flex-direction: column;
  gap: 0.2rem;
}
.ui-series-title {
  margin: 0;
  font-size: 0.75rem;
  color: var(--muted-ink);
}
.ui-series-list {
  margin: 0;
  padding: 0;
  list-style: none;
}
.ui-series-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  padding: 0.2rem 0;
  border-bottom: 1px solid var(--rule);
  font-size: 0.8125rem;
}
.ui-series-row:last-child {
  border-bottom: none;
}
.ui-series-label {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--ink);
}
.ui-series-count {
  flex-shrink: 0;
  color: var(--muted-ink);
}
.ui-block.media_grid {
  flex-direction: row;
  flex-wrap: wrap;
}
.ui-block.media_grid .ui-rec {
  width: 108px;
  flex-direction: column;
  align-items: stretch;
  position: relative;
  padding: 0;
  overflow: hidden;
}
.pick {
  flex-shrink: 0;
}
.ui-block.media_grid .pick {
  position: absolute;
  top: 4px;
  right: 4px;
  z-index: 1;
}
.thumb {
  width: 42px;
  height: 42px;
  border-radius: 8px;
  object-fit: cover;
  flex-shrink: 0;
}
.ui-block.media_grid .thumb {
  width: 108px;
  height: 108px;
  border-radius: 0;
}
.ui-text {
  font-size: 0.8125rem;
  color: var(--ink);
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  min-width: 0;
}
.ui-text.open {
  display: block;
  -webkit-line-clamp: unset;
  white-space: pre-wrap;
}
.ui-meta,
.ui-badge,
.ui-more,
.ui-empty {
  font-size: 0.75rem;
  color: var(--muted-ink);
  flex-shrink: 0;
}
.ui-more,
.ui-empty {
  margin: 0;
}
.ui-more-btn {
  align-self: flex-start;
  margin-top: 0.35rem;
  padding: 0.25rem 0.7rem;
  border: 1px solid var(--line, rgba(0, 0, 0, 0.12));
  border-radius: 999px;
  background: transparent;
  color: var(--muted-ink);
  font-size: 0.75rem;
  cursor: pointer;
}
.ui-more-btn:hover {
  color: var(--ink);
  border-color: currentColor;
}
</style>
