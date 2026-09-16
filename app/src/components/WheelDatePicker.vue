<script setup lang="ts">
// iOS 风格三列日期滚轮（底部 sheet 内），阳历/农历共用：
//   - 阳历列：年 / 月 / 日（日按公历月长收窄，含闰年 2 月 29）
//   - 农历列：年 / 农历月（含「闰X月」选项）/ 农历日（按大小月收窄）
// 草稿式：打开时从 props 快照，取消丢弃；「完成」才 emit confirm。
import { computed, nextTick, ref, watch } from 'vue'
import Sheet from './Sheet.vue'
import { LUNAR_DAYS, leapDays, leapMonth, lunarMonthText, monthDays } from '../utils/lunar'

const ITEM_PX = 36 // 单列行高，与 CSS --item-h 同源（内联变量下发，避免两处漂移）
const VISIBLE = 5 // 可见项数（奇数，中间项对齐高亮带）
const BAND_TOP = Math.floor(VISIBLE / 2) // 高亮带上方留白 = (VISIBLE-1)/2
const PAD_PX = ITEM_PX * BAND_TOP // 首尾等长留白，使首尾项也能落到高亮带

const props = defineProps<{
  show: boolean
  mode: 'solar' | 'lunar'
  year: number
  month: number
  day: number
  leap: boolean
}>()

const emit = defineEmits<{
  'update:show': [v: boolean]
  confirm: [v: { year: number; month: number; day: number; leap: boolean }]
}>()

// —— 草稿 ——
const draft = ref({ y: props.year, m: props.month, d: props.day, leap: props.leap })

// —— 列数据 ——
const years = computed(() => {
  const cur = new Date().getFullYear()
  const list: number[] = []
  for (let y = cur; y >= 1900; y--) list.push(y) // 今年在顶，向下滑向更早年份
  return list
})

interface LunarMonthOpt {
  m: number
  leap: boolean
  label: string
}

const months = computed<LunarMonthOpt[]>(() => {
  if (props.mode === 'solar') {
    return Array.from({ length: 12 }, (_, i) => ({ m: i + 1, leap: false, label: `${i + 1} 月` }))
  }
  const lm = leapMonth(draft.value.y)
  const list: LunarMonthOpt[] = []
  for (let m = 1; m <= 12; m++) {
    list.push({ m, leap: false, label: lunarMonthText(m, false) })
    // 闰月紧跟在同名平月之后（农历书写习惯）
    if (lm === m) list.push({ m, leap: true, label: lunarMonthText(m, true) })
  }
  return list
})

const dayCount = computed(() =>
  props.mode === 'solar'
    ? new Date(draft.value.y, draft.value.m, 0).getDate() // 下一月第 0 天 = 本月天数（含闰年）
    : draft.value.leap
      ? leapDays(draft.value.y)
      : monthDays(draft.value.y, draft.value.m),
)
const days = computed(() => Array.from({ length: dayCount.value }, (_, i) => i + 1))

const yearItems = computed(() => years.value.map((y) => ({ text: `${y} 年`, active: y === draft.value.y })))
const monthItems = computed(() =>
  months.value.map((o) => ({ text: o.label, active: o.m === draft.value.m && o.leap === draft.value.leap })),
)
const dayItems = computed(() =>
  days.value.map((d) => ({ text: props.mode === 'solar' ? `${d} 日` : LUNAR_DAYS[d], active: d === draft.value.d })),
)
const cols = computed(() => [yearItems.value, monthItems.value, dayItems.value])

// —— 选中项下标 ——
const yearIndex = computed(() => Math.max(years.value.indexOf(draft.value.y), 0))
const monthIndex = computed(() => {
  const i = months.value.findIndex((o) => o.m === draft.value.m && o.leap === draft.value.leap)
  return i < 0 ? 0 : i
})
const dayIndex = computed(() => Math.max(draft.value.d - 1, 0))

// —— 滚动：结束时读回选中项并（由 scroll-snap）对齐；每列独立去抖计时 ——
const colRefs = ref<HTMLElement[]>([])
const settleTimers: number[] = []

function onScroll(col: number) {
  if (settleTimers[col]) window.clearTimeout(settleTimers[col])
  settleTimers[col] = window.setTimeout(() => settle(col), 120)
}
function settle(col: number) {
  const el = colRefs.value[col]
  if (!el) return
  const idx = Math.round(el.scrollTop / ITEM_PX)
  if (col === 0) {
    const y = years.value[idx]
    if (y != null && y !== draft.value.y) pickYear(y)
  } else if (col === 1) {
    const o = months.value[idx]
    if (o && (o.m !== draft.value.m || o.leap !== draft.value.leap)) {
      draft.value.m = o.m
      draft.value.leap = o.leap
      clampDay()
    }
  } else {
    const d = days.value[idx]
    if (d != null && d !== draft.value.d) draft.value.d = d
  }
}

function pickYear(y: number) {
  draft.value.y = y
  // 新年份可能没有当前选中的闰月 → 回退到同名平月（月份列随之重建）
  if (draft.value.leap && leapMonth(y) !== draft.value.m) draft.value.leap = false
  clampDay()
}

function clampDay() {
  if (draft.value.d > dayCount.value) draft.value.d = dayCount.value
}

function scrollTo(col: number, index: number, smooth = true) {
  colRefs.value[col]?.scrollTo({ top: index * ITEM_PX, behavior: smooth ? 'smooth' : 'auto' })
}

/** 点选某项：先写值（换算新下标）再滚动居中 */
function tapItem(col: number, index: number) {
  if (col === 0) pickYear(years.value[index])
  else if (col === 1) {
    const o = months.value[index]
    if (o) {
      draft.value.m = o.m
      draft.value.leap = o.leap
      clampDay()
    }
  } else {
    draft.value.d = days.value[index]
  }
  nextTick(() => scrollTo(col, col === 0 ? yearIndex.value : col === 1 ? monthIndex.value : dayIndex.value))
}

function syncAll(smooth = false) {
  nextTick(() => {
    scrollTo(0, yearIndex.value, smooth)
    scrollTo(1, monthIndex.value, smooth)
    scrollTo(2, dayIndex.value, smooth)
  })
}

watch(
  () => props.show,
  (v) => {
    if (!v) return
    draft.value = { y: props.year, m: props.month, d: props.day, leap: props.leap }
    clampDay()
    syncAll(false)
  },
)
// 年份变化重建月份列 → 重新对齐月列与日列（用户此刻在第 1 列，不会被拽走）
watch(() => draft.value.y, () => {
  nextTick(() => {
    scrollTo(1, monthIndex.value, false)
    scrollTo(2, dayIndex.value, false)
  })
})
// 月/闰月变化只影响日列长度 → 只对齐日列
watch([() => draft.value.m, () => draft.value.leap], () => {
  nextTick(() => scrollTo(2, dayIndex.value, false))
})

function onKey(col: number, e: KeyboardEvent) {
  if (e.key !== 'ArrowUp' && e.key !== 'ArrowDown') return
  e.preventDefault()
  const step = e.key === 'ArrowUp' ? -1 : 1
  const cur = col === 0 ? yearIndex.value : col === 1 ? monthIndex.value : dayIndex.value
  const max = cols.value[col].length - 1
  tapItem(col, Math.min(Math.max(cur + step, 0), max))
}

function onConfirm() {
  emit('confirm', { year: draft.value.y, month: draft.value.m, day: draft.value.d, leap: draft.value.leap })
}
</script>

<template>
  <Sheet
    :show="show"
    :title="mode === 'solar' ? '选择日期' : '选择农历日期'"
    confirm-text="完成"
    @update:show="emit('update:show', $event)"
    @confirm="onConfirm"
  >
    <div
      class="wheels"
      :style="{
        '--item-h': ITEM_PX + 'px',
        '--wheels-h': ITEM_PX * VISIBLE + 'px',
        '--band-top': ITEM_PX * BAND_TOP + 'px',
        '--pad-h': PAD_PX + 'px',
      }"
    >
      <div class="band" aria-hidden="true" />
      <div
        v-for="(col, ci) in cols"
        :key="ci"
        ref="colRefs"
        class="wheel num"
        role="listbox"
        :aria-label="['年', '月', '日'][ci]"
        tabindex="0"
        @scroll="onScroll(ci)"
        @keydown="onKey(ci, $event)"
      >
        <div class="pad" aria-hidden="true" />
        <button
          v-for="(it, ii) in col"
          :key="ii"
          class="item"
          type="button"
          role="option"
          tabindex="-1"
          :class="{ on: it.active }"
          :aria-selected="it.active"
          @click="tapItem(ci, ii)"
        >
          {{ it.text }}
        </button>
        <div class="pad" aria-hidden="true" />
      </div>
    </div>
  </Sheet>
</template>

<style scoped>
.wheels {
  position: relative;
  display: flex;
  gap: 2px;
}
.band {
  position: absolute;
  left: -2px;
  right: -2px;
  top: var(--band-top);
  height: var(--item-h);
  border-radius: 10px;
  background: rgba(120, 120, 128, 0.14);
  pointer-events: none;
}
:root[data-theme='dark'] .band {
  background: rgba(120, 120, 128, 0.3);
}
.wheel {
  flex: 1;
  min-width: 0;
  height: var(--wheels-h);
  overflow-y: auto;
  scroll-snap-type: y mandatory;
  -webkit-overflow-scrolling: touch;
  scrollbar-width: none;
  position: relative;
  z-index: 1;
  outline: none;
}
.wheel::-webkit-scrollbar { display: none; }
/* 上下等长留白：让首尾项也能落到中央高亮带（由 PAD_PX 下发） */
.pad {
  height: var(--pad-h);
}
.item {
  display: block;
  width: 100%;
  height: var(--item-h);
  border: none;
  background: none;
  padding: 0;
  font-size: 17px;
  font-weight: 500;
  color: var(--muted-ink);
  cursor: pointer;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  scroll-snap-align: center;
  transition: color 0.15s ease;
}
.item.on {
  color: var(--ink);
  font-weight: 600;
}
.wheel:focus-visible {
  border-radius: 10px;
  box-shadow: 0 0 0 2px var(--primary);
}
</style>