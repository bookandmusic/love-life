<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Bubble, UIRecord } from '../../types/chat'
import { Sparkles, Search, ChevronDown } from 'lucide-vue-next'
import { mediaSrc } from '../../types'
import { isImage, isVideo } from '../../utils/mediaMime'
import { renderMarkdown } from '../../utils/markdown'
import EmptyState from '../EmptyState.vue'

const props = defineProps<{
  bubbles: Bubble[]
  error: string
  // 当前已勾选的键（result_id:id / result_id:group:key），用于勾选框回显
  pickedKeys: string[]
  // 已展开全文的记录键
  openKeys: string[]
  unavailableReason?: string
  thinking?: boolean
  welcome?: string
}>()

// 渲染行：连续的工具步骤合并为一条「已完成 N 步」可展开摘要（PRODUCT「最终形态」：
// 界面最终只剩结果卡 + 回答正文 + 思考与步骤两条折叠条目）。单步不聚合——一行更直接。
type RenderRow =
  | { kind: 'bubble'; bubble: Bubble }
  | { kind: 'steps'; steps: Bubble[] }

const rows = computed<RenderRow[]>(() => {
  const list = props.bubbles
  const out: RenderRow[] = []
  let i = 0
  while (i < list.length) {
    if (list[i].role !== 'tool') {
      out.push({ kind: 'bubble', bubble: list[i] })
      i++
      continue
    }
    const run: Bubble[] = []
    while (i < list.length && list[i].role === 'tool') {
      run.push(list[i])
      i++
    }
    if (run.length === 1) out.push({ kind: 'bubble', bubble: run[0] })
    else out.push({ kind: 'steps', steps: run })
  }
  return out
})

// 折叠状态按首个步骤的序号记（同一摘要展开/收起状态在重渲染间保持）
const stepsOpen = ref<Record<number, boolean>>({})

function stepSummary(steps: Bubble[]): string {
  return `已完成 ${steps.length} 步`
}

const emit = defineEmits<{
  (e: 'toggle-pick', resultId: string, rec: UIRecord, single: boolean): void
  (e: 'toggle-open', key: string): void
  (e: 'open-media', rec: UIRecord): void
  (e: 'load-more', ui: Bubble['ui']): void
}>()

function recKey(resultId: string, rec: UIRecord): string {
  if (rec.group_key) return `${resultId}:group:${rec.group_key}`
  return `${resultId}:${rec.id}`
}

function onRec(ui: Bubble['ui'], rec: UIRecord) {
  if (!ui) return
  if (rec.thumb_url && (ui.kind === 'media_grid' || rec.kind === 'media')) {
    emit('open-media', rec)
    return
  }
  emit('toggle-open', recKey(ui.result_id || '', rec))
}

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
    <EmptyState v-if="bubbles.length === 0 && unavailableReason" :icon="Sparkles" title="AI 助手还没准备好" :sub="unavailableReason" />
    <EmptyState v-else-if="bubbles.length === 0" :icon="Sparkles" title="想聊点什么" :sub="welcome || '试试问：帮我写纪念日祝福、安排周末或记录日常。'" />
    <template v-for="(row, i) in rows" :key="i">
      <!-- 多次工具调用聚合为一条可展开摘要（展开见每步人话状态，不是原始参数） -->
      <div v-if="row.kind === 'steps'" class="row assistant">
        <span class="ai-avatar" title="AI 助手"><Sparkles :size="14" stroke-width="2" /></span>
        <div class="bubble assistant steps-bubble">
          <button class="think-head" aria-label="展开/收起步骤" @click="stepsOpen[i] = !stepsOpen[i]">
            <span>{{ stepSummary(row.steps) }}</span>
            <ChevronDown :size="13" stroke-width="2" class="think-chevron" />
          </button>
          <div v-show="stepsOpen[i]" class="steps-body">
            <p v-for="(s, si) in row.steps" :key="si" class="step-line">
              <Search :size="12" stroke-width="1.8" class="tool-icon" />
              <span>{{ s.toolLabel || s.content }}</span>
            </p>
          </div>
        </div>
      </div>
      <template v-else>
      <!-- 正文/结果卡片/思考/附件/中断标记任一成立即需渲染：纯媒体用户消息正文为空，
           漏掉 media 会让它整条不可见（发图不说字的常见路径） -->
      <div v-if="row.bubble.content || row.bubble.ui || row.bubble.reasoning || row.bubble.media?.length || row.bubble.quoted?.length || row.bubble.toolLabel || row.bubble.interrupted || row.bubble.cardID !== undefined" class="row" :class="row.bubble.role">
        <span v-if="row.bubble.role === 'assistant'" class="ai-avatar" title="AI 助手">
          <Sparkles :size="14" stroke-width="2" />
        </span>
        <div class="bubble" :class="row.bubble.role">
        <div v-if="row.bubble.reasoning" class="think" :class="{ open: row.bubble.thinkOpen }">
          <button class="think-head" aria-label="展开/收起思考过程" @click="row.bubble.thinkOpen = !row.bubble.thinkOpen">
            <span>已思考 {{ row.bubble.thinkSeconds ? `${row.bubble.thinkSeconds} 秒` : '' }}</span>
            <ChevronDown :size="13" stroke-width="2" class="think-chevron" />
          </button>
          <p v-show="row.bubble.thinkOpen" class="think-body">{{ row.bubble.reasoning }}</p>
        </div>
        <!-- eslint-disable-next-line vue/no-v-html -- renderMarkdown: markdown-it html:false + DOMPurify 白名单消毒后才入 v-html -->
        <div v-if="row.bubble.role === 'assistant' && row.bubble.content" class="md" v-html="renderMarkdown(row.bubble.content)" />
        <!-- 工具状态条：一行人话，不可展开（审计归后台） -->
        <p v-else-if="row.bubble.role === 'tool'" class="tool-line">
          <Search :size="13" stroke-width="1.8" class="tool-icon" />
          <span>{{ row.bubble.toolLabel || row.bubble.content }}</span>
        </p>
        <template v-else>{{ row.bubble.content }}</template>
        <p v-if="row.bubble.quoted?.length" class="quote-echo">
          <span v-for="(q, qi) in row.bubble.quoted" :key="qi">{{ q.label || '引用了资源' }}</span>
        </p>
        <div v-if="row.bubble.media?.length" class="bubble-media">
          <!-- 本机选中的有 previewUrl（blob URL）可渲染；刷新回显只有元数据 → 显示文件名占位 -->
          <template v-for="(m, mi) in row.bubble.media" :key="mi">
            <img v-if="isImage(m.type) && m.previewUrl" :src="m.previewUrl" alt="附件" loading="lazy" />
            <video v-else-if="isVideo(m.type) && m.previewUrl" :src="m.previewUrl" controls />
            <audio v-else-if="m.previewUrl" :src="m.previewUrl" controls class="bubble-audio" />
            <span v-else class="media-file">{{ m.name }}</span>
          </template>
        </div>
        <div v-if="row.bubble.ui" class="ui-block" :class="row.bubble.ui.kind">
          <p class="ui-title">{{ row.bubble.ui.title }}</p>
          <!-- 统计概览卡：数字网格 + 两组分布列表；不做图表、不可勾选（PRODUCT「卡片体系」统计概览卡） -->
          <template v-if="row.bubble.ui.kind === 'stats'">
            <div v-if="row.bubble.ui.stats?.length" class="ui-stats">
              <div v-for="(st, si) in row.bubble.ui.stats" :key="si" class="ui-stat">
                <span class="ui-stat-label">{{ st.label }}</span>
                <b class="ui-stat-value">{{ st.value }}</b>
                <span v-if="st.hint" class="ui-stat-hint">{{ st.hint }}</span>
              </div>
            </div>
            <div v-for="(se, si) in row.bubble.ui.series ?? []" :key="si" class="ui-series">
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
          <template v-else-if="(row.bubble.ui.records ?? []).length">
            <div
              v-for="(rec, ri) in (row.bubble.ui.records ?? [])"
              :key="ri"
              class="ui-rec"
              @click="onRec(row.bubble.ui, rec)"
            >
              <input
                v-if="row.bubble.ui.selectable && row.bubble.ui.result_id"
                class="pick"
                type="checkbox"
                :checked="pickedKeys.includes(recKey(row.bubble.ui.result_id, rec))"
                @click.stop="emit('toggle-pick', row.bubble.ui.result_id, rec, !!row.bubble.ui.single_pick)"
              />
              <img
                v-if="rec.thumb_url"
                class="thumb"
                :src="mediaSrc({ thumb_url: rec.thumb_url }, 'thumb')"
                alt=""
                loading="lazy"
                @click.stop="emit('open-media', rec)"
              />
              <span class="ui-text" :class="{ open: openKeys.includes(recKey(row.bubble.ui.result_id || '', rec)) }">{{ rec.text || '（无文字）' }}</span>
              <span v-if="rec.date" class="ui-meta">{{ rec.date }}</span>
              <span v-if="rec.badge" class="ui-badge">{{ rec.badge }}</span>
            </div>
          </template>
          <p v-else class="ui-empty">没有内容</p>
          <!-- 列表卡计数与翻页：真实总数 + 真实余量（语义检索无稳定总数时不显示假数） -->
          <p v-if="row.bubble.ui.more" class="ui-more">
            <template v-if="row.bubble.ui.total">共 {{ row.bubble.ui.total }} 条 · 还有 {{ row.bubble.ui.more }} 条</template>
            <template v-else>还有 {{ row.bubble.ui.more }} 条</template>
          </p>
          <!-- 语义检索无稳定总数：标注口径而非给假数（PRODUCT「卡片体系」） -->
          <p v-if="row.bubble.ui.semantic" class="ui-more">按相关性截取，非全部命中</p>
          <button
            v-if="row.bubble.ui.more && row.bubble.ui.result_id && row.bubble.ui.selectable"
            class="ui-more-btn"
            type="button"
            @click="emit('load-more', row.bubble.ui)"
          >再看几条</button>
        </div>
        </div>
      </div>
      <!-- 意外断线的残局不删用户消息，但得说明这里不会再有下文（否则界面看着像在等回复） -->
      <p v-if="row.bubble.interrupted" class="interrupted">回复已中断，可重新发送这一句。</p>
      <slot name="card" :bubble="row.bubble" />
      </template>
    </template>
    <!-- 等待指示：流式进行且本轮还没有任何内容落泡 -->
    <div v-if="thinking" class="row assistant">
      <span class="ai-avatar" title="AI 助手"><Sparkles :size="14" stroke-width="2" /></span>
      <div class="bubble assistant thinking-bubble"><span class="think-dot" /><span class="think-dot" /><span class="think-dot" /> 思考中…</div>
    </div>
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

.quote-echo {
  margin: 0.35rem 0 0;
  display: flex;
  flex-wrap: wrap;
  gap: 0.3rem;
}
.quote-echo span {
  font-size: 0.75rem;
  background: rgba(255, 255, 255, 0.18);
  border-radius: 999px;
  padding: 0.1rem 0.45rem;
}

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
/* 无本机预览时的附件占位（刷新回显：只剩文件名） */
.media-file {
  display: inline-block;
  padding: 0.15rem 0.5rem;
  border-radius: 6px;
  background: var(--sk);
  color: var(--muted-ink);
  font-size: 0.75rem;
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
/* 步骤聚合摘要（多步调用折叠为一条，展开见每步人话） */
.steps-bubble {
  padding: 0.4rem 0.7rem;
}
.steps-body {
  margin-top: 0.3rem;
}
.step-line {
  display: flex;
  align-items: center;
  gap: 0.35rem;
  margin: 0.15rem 0;
  font-size: 0.8125rem;
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
  display: block;
  padding: 0.15rem 0;
  max-width: 100%;
}

.tool-line {
  margin: 0;
  display: inline-flex;
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

/* 思考区（助手气泡顶部）：默认收起，点标题展开 */
.think {
  border: 1px solid var(--rule);
  border-radius: 10px;
  background: var(--ai-bg, var(--bg));
  margin-bottom: 0.4rem;
  overflow: hidden;
}
.think-head {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 4px;
  border: none;
  background: none;
  font-size: 0.75rem;
  color: var(--muted-ink);
  padding: 0.35rem 0.6rem;
  cursor: pointer;
}
.think-chevron {
  transition: transform 0.2s ease;
}
.think.open .think-chevron {
  transform: rotate(180deg);
}
.think-body {
  margin: 0;
  padding: 0.1rem 0.6rem 0.55rem;
  font-size: 0.75rem;
  color: var(--muted-ink);
  white-space: pre-wrap;
  word-break: break-word;
  max-height: 180px;
  overflow-y: auto;
}

/* markdown 气泡内样式：接管排版（关闭 .bubble 的 pre-wrap），元素紧凑贴近聊天 */
.bubble .md {
  white-space: normal;
  font-size: 0.9375rem;
  line-height: 1.55;
}
.bubble .md :deep(p) {
  margin: 0.2em 0;
}
.bubble .md :deep(p:first-child) {
  margin-top: 0;
}
.bubble .md :deep(p:last-child) {
  margin-bottom: 0;
}
.bubble .md :deep(ul),
.bubble .md :deep(ol) {
  margin: 0.3em 0;
  padding-left: 1.4em;
}
.bubble .md :deep(code) {
  background: var(--sk);
  border-radius: 4px;
  padding: 0.1em 0.35em;
  font-size: 0.85em;
}
.bubble .md :deep(pre) {
  background: var(--sk);
  border-radius: 8px;
  padding: 0.6em 0.8em;
  overflow-x: auto;
}
.bubble .md :deep(pre code) {
  background: none;
  padding: 0;
}
.bubble .md :deep(blockquote) {
  margin: 0.4em 0;
  padding: 0.1em 0.8em;
  border-left: 3px solid var(--primary);
  color: var(--muted-ink);
}
.bubble .md :deep(h1),
.bubble .md :deep(h2),
.bubble .md :deep(h3),
.bubble .md :deep(h4),
.bubble .md :deep(h5),
.bubble .md :deep(h6) {
  font-size: 0.9375rem;
  margin: 0.5em 0 0.2em;
  font-weight: 600;
}
.bubble .md :deep(a) {
  color: var(--tint);
  word-break: break-all;
}
.bubble .md :deep(table) {
  border-collapse: collapse;
  font-size: 0.8125rem;
  margin: 0.4em 0;
}
.bubble .md :deep(th),
.bubble .md :deep(td) {
  border: 1px solid var(--rule);
  padding: 0.25em 0.5em;
}
.bubble .md :deep(hr) {
  border: none;
  border-top: 1px solid var(--rule);
  margin: 0.5em 0;
}

/* 等待指示「思考中…」 */
.thinking-bubble {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  color: var(--muted-ink);
  font-size: 0.8125rem;
}
.think-dot {
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: currentColor;
  animation: think-bounce 1.2s infinite;
}
.think-dot:nth-child(2) {
  animation-delay: 0.2s;
}
.think-dot:nth-child(3) {
  animation-delay: 0.4s;
}
@keyframes think-bounce {
  0%,
  100% {
    opacity: 0.3;
  }
  50% {
    opacity: 1;
  }
}

.error {
  color: var(--danger);
  font-size: 0.8125rem;
  text-align: center;
}

/* 中断提示：弱化置灰，不用红色——这不是错误，只是没答完 */
.interrupted {
  margin: -0.35rem 0 0.6rem;
  font-size: 0.75rem;
  color: var(--muted-ink);
  text-align: right;
}
</style>
