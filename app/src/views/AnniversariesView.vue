<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ChevronLeft, Trash2, Cake, Gift, CalendarDays, Moon } from 'lucide-vue-next'
import { api } from '../api/client'
import NavActions from '../components/NavActions.vue'
import SkeletonState from '../components/SkeletonState.vue'
import ErrorState from '../components/ErrorState.vue'
import ConfirmSheet from '../components/ConfirmSheet.vue'
import { nextLunarOccurrence, lunarDayText, lunarMonthText, LUNAR_DAYS } from '../utils/lunar'

interface Anniv {
  id: number
  name: string
  // 统一契约：date 一律 YYYY-MM-DD，type 决定语义（solar=公历；lunar=农历日期，每年按农历月/日重复）
  date: string
  type?: 'solar' | 'lunar'
  remind_days_before: number
  days_until: number
}

const route = useRoute()
const router = useRouter()
const items = ref<Anniv[]>([])
const error = ref('')
const loading = ref(false)
const saving = ref(false)
const remindOptions = [0, 1, 3, 7, 14, 30]

// ---- 完整表单页状态 ----
const newForm = ref(false)
const fName = ref('')
const fType = ref<'solar' | 'lunar'>('solar')
const fSolarDate = ref('')
const fLunarMonth = ref<number>(8)
const fLunarDay = ref<number>(15)
const fRemind = ref(1)

// B9 删除确认 sheet
const showDelete = ref(false)
const deleting = ref<Anniv | null>(null)

// 农历换算预览：农历 → 最近一个公历日期
const lunarPreview = computed(() =>
  fType.value === 'lunar' ? nextLunarOccurrence(fLunarMonth.value, fLunarDay.value) : null,
)
const lunarPreviewLabel = computed(() =>
  fType.value === 'lunar' ? lunarDayText(fLunarMonth.value, fLunarDay.value) : '',
)
// 农历新增统一结构：date = 当前年份 + 农历月/日（YYYY-MM-DD，按农历语义；仅展示参考，重复规则=月/日）
const lunarRefDate = computed(() => {
  if (fType.value !== 'lunar') return ''
  const y = new Date().getFullYear()
  const mm = String(fLunarMonth.value).padStart(2, '0')
  const dd = String(fLunarDay.value).padStart(2, '0')
  return `${y}-${mm}-${dd}`
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    const res = await api<{ items: Anniv[] }>('/api/v1/anniversaries')
    items.value = res.items
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
  } finally {
    loading.value = false
  }
}

function askRemove(a: Anniv) {
  deleting.value = a
  showDelete.value = true
}

async function confirmRemove() {
  if (!deleting.value) return
  const a = deleting.value
  try {
    await api(`/api/v1/anniversaries/${a.id}`, { method: 'DELETE' })
    await load()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '删除失败'
  } finally {
    deleting.value = null
  }
}

function closeForm() {
  newForm.value = false
  router.replace('/anniversaries')
}

function resetForm() {
  fName.value = ''
  fType.value = 'solar'
  fSolarDate.value = ''
  fLunarMonth.value = 8
  fLunarDay.value = 15
  fRemind.value = 1
  error.value = ''
}

const canSave = computed(() => {
  if (saving.value || !fName.value.trim()) return false
  if (fType.value === 'solar') return !!fSolarDate.value
  return !!lunarRefDate.value
})

async function save() {
  if (!canSave.value) return
  saving.value = true
  error.value = ''
  try {
    const name = fName.value.trim()
    if (fType.value === 'solar') {
      await api('/api/v1/anniversaries', {
        method: 'POST',
        body: JSON.stringify({ name, type: 'solar', date: fSolarDate.value, remind_days_before: fRemind.value }),
      })
    } else {
      // 统一契约：date = YYYY-MM-DD（农历语义参考日期），只在客户端换算；不存「本次公历」
      await api('/api/v1/anniversaries', {
        method: 'POST',
        body: JSON.stringify({ name, type: 'lunar', date: lunarRefDate.value, remind_days_before: fRemind.value }),
      })
    }
    closeForm()
    await load()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '添加失败'
  } finally {
    saving.value = false
  }
}

// ?new=1 → 打开完整表单页（FAB「纪念日」入口；watch 支持停留本页时重复触发）
watch(
  () => route.query.new,
  (v) => {
    if (v === '1') {
      resetForm()
      newForm.value = true
    }
  },
  { immediate: true },
)

// 统一契约：从 YYYY-MM-DD 取 m/d（农历时按农历语义；私人 type 决定）
function ymdOf(date: string): { y: number; m: number; d: number } {
  const [y, m, d] = date.split('-').map(Number)
  return { y, m, d }
}



// —— 统一计算「下一次即将到来」（阳历→今年正日；农历→按公历下次），并按最近排序 ——
function daysUntilDate(dateStr: string): number {
  const [y, m, d] = dateStr.split('-').map(Number)
  const today = new Date()
  today.setHours(0, 0, 0, 0)
  let target = new Date(y, m - 1, d)
  target.setHours(0, 0, 0, 0)
  while (target.getTime() < today.getTime()) target.setFullYear(target.getFullYear() + 1)
  return Math.round((target.getTime() - today.getTime()) / 86400000)
}

interface AnnNext {
  date: string
  days: number
}

function unifiedNext(a: Anniv): AnnNext {
  if (a.type === 'lunar' && a.date) {
    const { m, d } = ymdOf(a.date)
    const occ = nextLunarOccurrence(Number(m), Number(d))
    if (occ) return { date: occ, days: daysUntilDate(occ) }
  }
  // 兜底：solar 用公历 date；无有效日期排在最后
  return { date: a.date || '', days: a.date ? daysUntilDate(a.date) : Number.MAX_SAFE_INTEGER }
}

const sortedItems = computed(() => [...items.value].sort((x, y) => unifiedNext(x).days - unifiedNext(y).days))

/** 阳历「月/日」在未来的下一次出现日期（今天起） */
function nextSolarDate(dateStr: string): string {
  const [y, m, d] = dateStr.split('-').map(Number)
  const today = new Date()
  today.setHours(0, 0, 0, 0)
  const target = new Date(y, m - 1, d)
  target.setHours(0, 0, 0, 0)
  while (target.getTime() < today.getTime()) target.setFullYear(target.getFullYear() + 1)
  const Y = target.getFullYear()
  const M = String(target.getMonth() + 1).padStart(2, '0')
  const D = String(target.getDate()).padStart(2, '0')
  return `${Y}-${M}-${D}`
}

function rowMeta(a: Anniv): AnnivRowMeta {
  const n = unifiedNext(a)
  const hot = n.days <= 30
  if (a.type === 'lunar' && a.date) {
    const { m, d } = ymdOf(a.date)
    const occ = nextLunarOccurrence(m, d)
    return { kind: 'lunar', hot, anniv: `每年${lunarDayText(m, d)}`, next: `下次 ${occ || '—'}`, origin: a.date ? `原始日期 ${a.date}` : '', days: n.days }
  }
  const { m, d } = a.date ? ymdOf(a.date) : { m: 0, d: 0 }
  const solarRule = m ? `每年 ${m} 月 ${d} 日` : ''
  return { kind: 'solar', hot, anniv: solarRule, next: `下次 ${a.date ? nextSolarDate(a.date) : '—'}`, origin: a.date ? `原始日期 ${a.date}` : '', days: n.days }
}

const rows = computed(() => sortedItems.value.map((a) => ({ ...a, meta: rowMeta(a) })))


/** 行级展示元数据（iOS 风格，阳历/农历完全对称：副行1=每年重复规则，副行2=下次日期） */
interface AnnivRowMeta {
  kind: 'solar' | 'lunar'
  hot: boolean
  anniv: string // 副1：每年规则（如「每年农历八月十五」/「每年 10 月 1 日」）
  next: string // 副2：下次具体日期（如「下次 2026-09-25」）
  origin: string // 副3：原始日期（不丢主体信息，仅作小字陪衬）
  days: number
}


onMounted(load)
</script>

<template>
  <div class="anniv-page">
    <!-- 完整表单：添加纪念日 -->
    <template v-if="newForm">
      <header class="navbar glass">
        <button class="nav-btn" aria-label="返回" @click="closeForm"><ChevronLeft :size="22" stroke-width="2.5" /></button>
        <span class="title">添加纪念日</span>
        <button class="np-save" :disabled="!canSave" @click="save">{{ saving ? '保存中…' : '保存' }}</button>
      </header>
      <div class="af-body">
        <input v-model="fName" class="s-field" placeholder="名字（如：中秋）" maxlength="30" />

        <div class="seg">
          <button :class="{ on: fType === 'solar' }" @click="fType = 'solar'"><CalendarDays :size="14" stroke-width="2" /> 阳历</button>
          <button :class="{ on: fType === 'lunar' }" @click="fType = 'lunar'"><Moon :size="14" stroke-width="2" /> 农历</button>
        </div>

        <template v-if="fType === 'solar'">
          <label class="s-label">日期（每年按阳历重复）</label>
          <input v-model="fSolarDate" type="date" class="s-field" />
        </template>
        <template v-else>
          <div class="lunar-row">
            <select v-model.number="fLunarMonth" class="s-select">
              <option v-for="m in 12" :key="m" :value="m">{{ lunarMonthText(m) }}</option>
            </select>
            <select v-model.number="fLunarDay" class="s-select">
              <option v-for="d in 30" :key="d" :value="d">{{ LUNAR_DAYS[d] }}</option>
            </select>
          </div>
          <p v-if="lunarPreviewLabel" class="lunar-pv">你选的是 <b>{{ lunarPreviewLabel }}</b>
            <template v-if="lunarPreview">（本次 {{ lunarPreview }}）</template>
          </p>
          <p v-else class="lunar-nopv">该农历日期在近几十年内不存在，请调整月份/日期</p>
        </template>

        <div class="s-row">
          <span class="s-label">提前提醒</span>
          <select v-model.number="fRemind" class="s-select">
            <option v-for="d in remindOptions" :key="d" :value="d">{{ d === 0 ? '当天提醒' : `提前 ${d} 天提醒` }}</option>
          </select>
        </div>

        <p v-if="error" class="error">{{ error }}</p>
      </div>
    </template>

    <!-- 正常列表页 -->
    <template v-else>
      <header class="navbar glass">
        <span class="title">纪念日</span>
        <NavActions />
      </header>

      <div class="scroll-area">
        <SkeletonState v-if="loading" variant="list" :rows="3" />
        <ErrorState v-else-if="error" :error="error" :retry="load" />
        <div v-else-if="items.length === 0" class="empty">
          <Cake :size="34" stroke-width="1.2" class="empty-ic" />
          <p>还没有纪念日，点右下角 + 添加第一个吧</p>
        </div>

        <div v-for="a in rows" :key="a.id" class="row">
          <span class="icon" :class="a.meta.hot ? '' : 'far'">
            <Cake v-if="a.meta.hot" :size="18" stroke-width="2" />
            <Gift v-else :size="18" stroke-width="2" />
          </span>
          <div class="mid">
            <div class="title-row">
              <span class="name">{{ a.name }}</span>
              <em class="type-tag" :class="a.meta.kind === 'lunar' ? 'tg-lunar' : 'tg-solar'">{{ a.meta.kind === 'lunar' ? '农历' : '阳历' }}</em>
            </div>
            <div class="meta">
              <span class="meta-line next">{{ a.meta.next }}</span>
              <span class="meta-line anniv">{{ a.meta.anniv }}</span>
            </div>
          </div>
          <div class="count">
            <span class="days-num num" :class="{ done: !a.meta.hot }">
              <b>{{ a.meta.days }}</b><small>天</small>
            </span>
            <span v-if="a.meta.hot" class="count-tip">后</span>
          </div>
          <button class="del" aria-label="删除纪念日" @click="askRemove(a)"><Trash2 :size="17" stroke-width="1.8" /></button>
        </div>
      </div>

      <!-- B9 删除确认 sheet -->
      <ConfirmSheet
        v-if="deleting"
        v-model:show="showDelete"
        title="删除纪念日"
        :message="`确定删除「${deleting.name}」？将进入回收站`"
        confirm-text="删除"
        :confirm-danger="true"
        @confirm="confirmRemove"
      />
    </template>
  </div>
</template>

<style scoped>
.anniv-page {
  height: 100%;
  display: flex;
  flex-direction: column;
}
.navbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: calc(10px + env(safe-area-inset-top)) 16px 10px;
  flex-shrink: 0;
  z-index: 10;
}
.title {
  font-size: 17px;
  font-weight: 600;
}
.nav-btn {
  border: none;
  background: none;
  color: var(--ios-blue);
  cursor: pointer;
  padding: 4px;
  display: flex;
}
.np-save {
  border: none;
  background: var(--primary);
  color: #fff;
  border-radius: 999px;
  padding: 7px 14px;
  font-size: 14px;
  font-weight: 600;
}
.np-save:disabled {
  opacity: 0.45;
}
.af-body {
  flex: 1;
  overflow-y: auto;
  padding: 12px 16px 32px;
}
.s-field {
  width: 100%;
  box-sizing: border-box;
  border: none;
  border-radius: 12px;
  padding: 13px 14px;
  font-size: 15px;
  font-family: inherit;
  background: var(--surface);
  color: var(--ink);
  margin-bottom: 12px;
  box-shadow: var(--shadow);
}
.s-field:focus {
  outline: none;
}
.s-label {
  display: block;
  font-size: 13px;
  color: var(--muted-ink);
  margin: 4px 0 6px;
}
.seg {
  display: flex;
  background: rgba(120, 120, 128, 0.16);
  border-radius: 9px;
  padding: 2px;
  margin: 4px 0 14px;
  width: max-content;
}
.seg button {
  border: none;
  background: none;
  padding: 7px 16px;
  border-radius: 7px;
  font-size: 13px;
  font-weight: 500;
  color: var(--ink);
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 5px;
}
.seg button.on {
  background: var(--surface);
  box-shadow: var(--shadow);
  font-weight: 600;
}
.s-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin: 4px 0 14px;
}
.s-select {
  border: none;
  border-radius: 10px;
  padding: 9px 12px;
  font-size: 14px;
  font-family: inherit;
  background: var(--surface);
  color: var(--ink);
  box-shadow: var(--shadow);
}
.lunar-pv {
  font-size: 14px;
  color: var(--ink);
  background: var(--surface);
  border-radius: 10px;
  padding: 10px 12px;
  box-shadow: var(--shadow);
  margin: 0 0 12px;
}
.lunar-pv b {
  color: var(--primary-deep);
}
.lunar-nopv {
  font-size: 13px;
  color: var(--muted-ink);
  margin-bottom: 12px;
}
.lunar-row {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-bottom: 12px;
}
.scroll-area {
  flex: 1;
  overflow-y: auto;
  -webkit-overflow-scrolling: touch;
  padding: 12px 16px 24px;
}
.row {
  display: flex;
  align-items: center;
  gap: 14px;
  background: var(--surface);
  border-radius: 18px;
  border: 1px solid var(--rule);
  box-shadow: var(--shadow);
  padding: 16px;
  margin-bottom: 12px;
}
.row:active {
  opacity: 0.9;
}
.icon.far {
  opacity: 0.55;
}
.icon {
  width: 40px;
  height: 40px;
  min-width: 40px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--sem-gold-soft);
  color: var(--sem-gold);
  flex-shrink: 0;
}
:root[data-theme='dark'] .icon {
  color: #ffd166;
}
.mid {
  flex: 1;
  min-width: 0;
}
.title-row {
  display: flex;
  align-items: center;
  gap: 8px;
}
.name {
  font-size: 17px;
  font-weight: 700;
  color: var(--ink);
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.type-tag {
  font-style: normal;
  font-size: 10px;
  line-height: 1;
  padding: 3px 6px;
  border-radius: 6px;
  font-weight: 500;
  flex-shrink: 0;
  opacity: 0.85;
}
.tg-solar {
  color: rgba(60, 60, 67, 0.55);
  background: rgba(120, 120, 128, 0.14);
}
.tg-lunar {
  color: #b7791e;
  background: var(--sem-gold-soft);
}
:root[data-theme='dark'] .tg-solar {
  color: rgba(242, 242, 247, 0.55);
  background: rgba(120, 120, 128, 0.24);
}
:root[data-theme='dark'] .tg-lunar {
  color: #ffd166;
  background: rgba(255, 209, 102, 0.16);
}
.meta {
  display: flex;
  flex-direction: column;
  gap: 2px;
  margin-top: 5px;
}
.meta-line {
  font-size: 13px;
  line-height: 1.4;
  color: var(--muted-ink);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.meta-line.next {
  color: var(--ink);
  font-weight: 500;
}
.meta-line.anniv {
  font-size: 12px;
  opacity: 0.85;
}
.meta-line.origin {
  font-size: 11px;
  color: var(--muted-ink);
  opacity: 0.65;
  font-variant-numeric: tabular-nums;
}
.count {
  display: flex;
  flex-direction: column;
  align-items: center;
  flex-shrink: 0;
  min-width: 44px;
}
.days-num {
  font-size: 22px;
  font-weight: 700;
  color: var(--sem-gold);
  font-variant-numeric: tabular-nums;
  display: inline-flex;
  align-items: baseline;
  gap: 1px;
}
.days-num b {
  font-weight: 700;
  letter-spacing: -0.01em;
}
.days-num small {
  font-size: 11px;
  font-weight: 500;
  color: var(--muted-ink);
}
:root[data-theme='dark'] .days-num {
  color: #ffd166;
}
.count-tip {
  font-size: 11px;
  color: var(--muted-ink);
}
.days-num.done {
  color: var(--muted-ink);
  font-size: 15px;
}
.days-num.done small {
  font-size: 10px;
}
.del {
  border: none;
  background: none;
  color: var(--muted-ink);
  cursor: pointer;
  padding: 8px;
  border-radius: 10px;
  flex-shrink: 0;
}
.del:active {
  background: rgba(255, 69, 58, 0.12);
  color: var(--danger);
}
.empty {
  text-align: center;
  padding: 56px 0;
}
.empty-ic {
  color: var(--primary);
  opacity: 0.55;
  margin-bottom: 12px;
}
.empty p {
  font-size: 15px;
  color: var(--ink);
}
.error {
  text-align: center;
  color: var(--danger);
  padding: 40px 0;
}

</style>