<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Bubble, UIRecord, UiBlockEmits } from '../../types/chat'
import { Sparkles, ChevronDown, LoaderCircle, Check, XCircle } from 'lucide-vue-next'
import { isImage, isVideo } from '../../utils/mediaMime'
import { renderMarkdown } from '../../utils/markdown'
import EmptyState from '../EmptyState.vue'
import UiBlock from './UiBlock.vue'

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
  // 本轮流式起点（bubbles 下标）：≥ 该下标的工具气泡不聚合（进行中每步一行，PRODUCT「过程呈现」）
  liveFrom?: number
}>()

// 渲染行：连续的工具步骤合并为一条「已完成 N 步」可展开摘要（PRODUCT「最终形态」：
// 界面最终只剩结果卡 + 回答正文 + 思考与步骤两条折叠条目）。单步不聚合——一行更直接。
type RenderRow =
  | { kind: 'bubble'; bubble: Bubble }
  | { kind: 'steps'; steps: Bubble[] }

const rows = computed<RenderRow[]>(() => {
  const list = props.bubbles
  const live = props.liveFrom ?? -1
  const out: RenderRow[] = []
  let i = 0
  while (i < list.length) {
    if (list[i].role !== 'tool') {
      out.push({ kind: 'bubble', bubble: list[i] })
      i++
      continue
    }
    const start = i
    const run: Bubble[] = []
    while (i < list.length && list[i].role === 'tool') {
      run.push(list[i])
      i++
    }
    // 流式进行中的本轮不聚合：每步一行、结果卡立即可见（PRODUCT「过程呈现」进行中口径）
    if (run.length === 1 || (live >= 0 && start >= live)) {
      for (const b of run) out.push({ kind: 'bubble', bubble: b })
    } else out.push({ kind: 'steps', steps: run })
  }
  return out
})

// 折叠状态按首个步骤的序号记（同一摘要展开/收起状态在重渲染间保持）
const stepsOpen = ref<Record<number, boolean>>({})

function stepSummary(steps: Bubble[]): string {
  return `已完成 ${steps.length} 步`
}

// 工具步骤图标按状态分档：运行中转圈、完成打勾、失败打叉——不再一律放大镜
// （tool.start 只下发 call_id + 人话 label，不含工具名，故按状态而非类型分档）。
function toolIcon(b: Bubble) {
  if (b.toolStatus === 'running') return LoaderCircle
  if (b.toolStatus === 'failed') return XCircle
  return Check
}

// 渲染行守卫：正文/结果卡/思考/附件/引用/工具行/中断任一成立即渲染（纯媒体用户消息正文为空）
function rowGuard(b: Bubble): boolean {
  return !!(b.content || b.ui || b.reasoning || b.media?.length || b.quoted?.length || b.toolLabel || b.interrupted || b.cardID !== undefined)
}

// 气泡壳：纯卡片载体（只有 cardID、无正文/思考/结果卡）不渲染空壳，卡片接管该行
function showShell(b: Bubble): boolean {
  if (b.role === 'tool') return true
  return !!(b.content || b.reasoning || b.ui || b.media?.length || b.quoted?.length)
}

// AI 标识一轮至多一次：仅带气泡壳的助手行显示；其余助手行用等宽占位保持左对齐
function showAvatar(b: Bubble): boolean {
  return b.role === 'assistant' && showShell(b)
}

// 与 UiBlock 共用同一事件契约（types/chat.ts「UiBlockEmits」），逐个转发给 ChatView
const emit = defineEmits<UiBlockEmits>()

// UiBlock 的事件签名与本级 emit 相同，逐个转发给 ChatView
function fwdPick(resultId: string, rec: UIRecord, single: boolean) {
  emit('toggle-pick', resultId, rec, single)
}
function fwdOpen(key: string) {
  emit('toggle-open', key)
}
function fwdMedia(rec: UIRecord) {
  emit('open-media', rec)
}
function fwdMore(ui: Bubble['ui']) {
  emit('load-more', ui)
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
        <span class="avatar-slot" aria-hidden="true" />
        <div class="content-col">
        <div class="bubble assistant steps-bubble">
          <button class="think-head" aria-label="展开/收起步骤" @click="stepsOpen[i] = !stepsOpen[i]">
            <span>{{ stepSummary(row.steps) }}</span>
            <ChevronDown :size="13" stroke-width="2" class="think-chevron" />
          </button>
          <div v-show="stepsOpen[i]" class="steps-body">
            <p v-for="(s, si) in row.steps" :key="si" class="step-line">
              <component :is="toolIcon(s)" :size="12" stroke-width="1.8" class="tool-icon" />
              <span>{{ s.toolLabel || s.content }}</span>
            </p>
          </div>
          <!-- 步骤摘要行常显结果卡（PRODUCT「过程呈现」最终形态：卡片不随聚合消失） -->
          <UiBlock
            v-for="(s, si) in row.steps"
            :key="`u${si}`"
            :ui="s.ui"
            :picked-keys="pickedKeys"
            :open-keys="openKeys"
            @toggle-pick="fwdPick"
            @toggle-open="fwdOpen"
            @open-media="fwdMedia"
            @load-more="fwdMore"
          />
        </div>
        </div>
      </div>
      <template v-else>
      <!-- 正文/结果卡片/思考/附件/引用/工具行/中断任一成立即需渲染：纯媒体用户消息正文为空，
           漏掉 media 会让它整条不可见（发图不说字的常见路径） -->
      <div v-if="rowGuard(row.bubble)" class="row" :class="row.bubble.role">
        <!-- AI 标识一轮至多一次：仅带气泡壳的助手行显示，其余助手行用等宽占位保持左对齐 -->
        <span v-if="showAvatar(row.bubble)" class="ai-avatar" title="AI 助手">
          <Sparkles :size="13" stroke-width="2" />
        </span>
        <span v-else-if="row.bubble.role !== 'user'" class="avatar-slot" aria-hidden="true" />
        <!-- 内容列：气泡、工具行、结果卡/确认卡同列堆叠，共享同一左边界与宽度上限 -->
        <div class="content-col">
        <div v-if="showShell(row.bubble)" class="bubble" :class="[row.bubble.role, { 'has-ui': !!row.bubble.ui }]">
        <div v-if="row.bubble.reasoning" class="think" :class="{ open: row.bubble.thinkOpen }">
          <button class="think-head" aria-label="展开/收起思考过程" @click="row.bubble.thinkOpen = !row.bubble.thinkOpen">
            <span>{{ row.bubble.thinkSeconds ? `已思考 ${row.bubble.thinkSeconds} 秒` : '思考中…' }}</span>
            <ChevronDown :size="13" stroke-width="2" class="think-chevron" />
          </button>
          <p v-show="row.bubble.thinkOpen" class="think-body">{{ row.bubble.reasoning }}</p>
        </div>
        <!-- eslint-disable-next-line vue/no-v-html -- renderMarkdown: markdown-it html:false + DOMPurify 白名单消毒后才入 v-html -->
        <div v-if="row.bubble.role === 'assistant' && row.bubble.content" class="md" v-html="renderMarkdown(row.bubble.content)" />
        <!-- 工具状态条：一行人话，不可展开（审计归后台）；进行中「正在…」（PRODUCT「过程呈现」） -->
        <p v-else-if="row.bubble.role === 'tool'" class="tool-line" :class="{ running: row.bubble.toolStatus === 'running' }">
          <component :is="toolIcon(row.bubble)" :size="13" stroke-width="1.8" class="tool-icon" />
          <span>{{ row.bubble.toolStatus === 'running' ? `正在${row.bubble.toolLabel || row.bubble.content}…` : row.bubble.toolLabel || row.bubble.content }}</span>
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
        <UiBlock
          :ui="row.bubble.ui"
          :picked-keys="pickedKeys"
          :open-keys="openKeys"
          @toggle-pick="fwdPick"
          @toggle-open="fwdOpen"
          @open-media="fwdMedia"
          @load-more="fwdMore"
        />
        </div>
        <slot name="card" :bubble="row.bubble" />
        </div>
      </div>
      <!-- 意外断线的残局不删用户消息，但得说明这里不会再有下文（否则界面看着像在等回复） -->
      <p v-if="row.bubble.interrupted" class="interrupted">回复已中断，可重新发送这一句。</p>
      </template>
    </template>
    <!-- 等待指示：流式进行且本轮还没有任何内容落泡 -->
    <div v-if="thinking" class="row assistant">
      <span class="avatar-slot" aria-hidden="true" />
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
  /* 助手/用户内容列宽度上限：卡片行铺满此宽，文字气泡随内容收缩 */
  --chat-col-max: 520px;
}

.row {
  display: flex;
  align-items: flex-start;
  gap: 0.4rem;
}

.row.user {
  justify-content: flex-end;
}

/* 间距分级：回合内紧凑、回合间（自用户消息起）明显留白，让眼睛把一段回复圈成一组 */
.row + .row {
  margin-top: 0.375rem;
}
.row.user {
  margin-top: 1.25rem;
}
/* AI 标识：柔和单色（不再粉紫强渐变），一轮至多一次，克制不抢视觉焦点 */
.ai-avatar {
  width: 26px;
  height: 26px;
  border-radius: 50%;
  background: color-mix(in srgb, var(--primary) 16%, transparent);
  color: var(--primary-deep);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  /* 顶对齐：与回合首行光学对齐，不再贴气泡下沿 */
  margin-top: 0.25rem;
}

/* 无标识的助手行占位：与头像等宽，保证各助手行左边界对齐 */
.avatar-slot {
  width: 26px;
  flex-shrink: 0;
}

/* 内容列：气泡、工具行、结果卡/确认卡同列堆叠，共享同一左边界与宽度上限；
   列本身设上限，卡片行铺满列宽（见下 .bubble.has-ui / .bubble.tool / .steps-bubble），
   文字气泡仍随内容收缩，不再出现「左齐右乱、卡片窄成一栏」 */
.content-col {
  flex: 1;
  min-width: 0;
  max-width: min(100%, var(--chat-col-max, 520px));
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 0.4rem;
}
/* 用户行：气泡靠右，与 AI 侧镜像 */
.row.user .content-col {
  align-items: flex-end;
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

/* 无本机预览时的附件占位（刷新回显：只剩文件名） */
.media-file {
  display: inline-block;
  padding: 0.15rem 0.5rem;
  border-radius: 6px;
  background: var(--sk);
  color: var(--muted-ink);
  font-size: 0.75rem;
}

/* 步骤聚合摘要（多步调用折叠为一条，展开见每步人话）；结果卡渲染已抽至 UiBlock.vue */
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
  /* 宽度由内容决定、上限为内容列宽（卡片行另铺满，见下） */
  max-width: 100%;
  padding: 0.6rem 0.9rem;
  border-radius: 18px;
  font-size: 0.9375rem;
  white-space: pre-wrap;
  word-break: break-word;
}

/* 卡片行铺满内容列：结果卡/统计卡/确认卡与工具行随列宽对齐，右边缘不再参差。
   border-box：无全局 border-box 时 width:100% 会把 padding/border 溢出列宽 */
.bubble.has-ui,
.bubble.tool,
.steps-bubble {
  width: 100%;
  box-sizing: border-box;
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

/* 进行中步骤：与运行态图标同步呼吸（PRODUCT「过程呈现」正在…） */
.tool-line.running .tool-icon {
  animation: think-bounce 1.2s infinite;
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

/* 思考区（助手气泡顶部）：去框——安静一行灰字 + 箭头，不再框套框 */
.think {
  margin-bottom: 0.35rem;
}
.think-head {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  border: none;
  background: none;
  font-size: 0.75rem;
  color: var(--muted-ink);
  padding: 0;
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
  padding: 0.1rem 0 0.35rem;
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
