<script setup lang="ts">
import { computed, onActivated, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Bell, ChevronLeft, ChevronRight, Cake, Gift, CalendarDays, Moon } from 'lucide-vue-next'
import { api, errText } from '../api/client'
import NavActions from '../components/NavActions.vue'
import SkeletonState from '../components/SkeletonState.vue'
import ErrorState from '../components/ErrorState.vue'
import ConfirmSheet from '../components/ConfirmSheet.vue'
import AppToast from '../components/AppToast.vue'
import Sheet from '../components/Sheet.vue'
import WheelDatePicker from '../components/WheelDatePicker.vue'
import { useLongPress } from '../composables/useLongPress'
import { KEYS, onceHint } from '../utils/storage'
import { RECYCLE_CONFIRM, RECYCLE_TOAST, RECYCLE_TITLE, recycleMsg, peerNotice } from '../utils/deleteCopy'
import { useAuth } from '../stores/auth'
import { nextLunarOccurrence, lunarDayText } from '../utils/lunar'
import { daysUntilDate, nextSolarDate, ymdOf } from '../utils/dates'

defineOptions({ name: 'AnniversariesView' })

interface Anniv {
  id: number
  name: string
  // 统一契约：date 一律 YYYY-MM-DD，type 决定语义（solar=公历；lunar=农历日期，每年按农历月/日重复）
  date: string
  type?: 'solar' | 'lunar'
  is_leap?: boolean
  remind_days_before: number
  days_until: number
  // 创建者：用于删除确认里的「对方会收到站内通知」提示
  created_by?: number
}

const route = useRoute()
const router = useRouter()
const auth = useAuth()
const items = ref<Anniv[]>([])
const error = ref('')
const loading = ref(false)
const saving = ref(false)
const remindOptions = [0, 1, 3, 7, 14, 30]

// ---- 完整表单页状态 ----
const newForm = ref(false)
const fName = ref('')
const fType = ref<'solar' | 'lunar'>('solar')
// 表单初值：阳历默认今天（与 resetForm 保持一致）
const _today = new Date()
const fSolarY = ref<number>(_today.getFullYear())
const fSolarM = ref<number>(_today.getMonth() + 1)
const fSolarD = ref<number>(_today.getDate())
const fLunarYear = ref<number>(_today.getFullYear())
const fLunarMonth = ref<number>(8)
const fLunarDay = ref<number>(15)
const fLunarLeap = ref(false)
const fRemind = ref(1)
// 日期选择：滚轮 sheet（iOS 风格），行内只显示当前值
const showPicker = ref(false)
const pickedDateLabel = computed(() =>
  fType.value === 'solar'
    ? `${fSolarY.value} 年 ${fSolarM.value} 月 ${fSolarD.value} 日`
    : lunarDayText(fLunarMonth.value, fLunarDay.value, fLunarLeap.value),
)
// 滚轮「完成」写回：闰月由所选月份携带（无独立开关）
function applyPickedDate(v: { year: number; month: number; day: number; leap: boolean }) {
  if (fType.value === 'solar') {
    fSolarY.value = v.year
    fSolarM.value = v.month
    fSolarD.value = v.day
  } else {
    fLunarYear.value = v.year
    fLunarMonth.value = v.month
    fLunarDay.value = v.day
    fLunarLeap.value = v.leap
  }
  showPicker.value = false
}
// 统一契约：阳历 date 同样落 YYYY-MM-DD（YYYY 为选中的原始年份）
const solarRefDate = computed(() => {
  if (fType.value !== 'solar') return ''
  const mm = String(fSolarM.value).padStart(2, '0')
  const dd = String(fSolarD.value).padStart(2, '0')
  return `${fSolarY.value}-${mm}-${dd}`
})

// B9 删除确认 sheet
const showDelete = ref(false)
const deleting = ref<Anniv | null>(null)
const toast = ref('')
// 移入回收站：长按卡片触发（卡片上没有常驻删除图标）
const { pressing, down: pressDown, move: pressMove, up: pressUp } = useLongPress()

// D6 提醒设置 sheet（REQUIREMENTS:182：每条纪念日的提醒天数可单独修改）
const showRemind = ref(false)
const reminding = ref<Anniv | null>(null)
const remindDays = ref(1)
const remindSaving = ref(false)
function askRemind(a: Anniv) {
  reminding.value = a
  remindDays.value = a.remind_days_before
  showRemind.value = true
}
async function saveRemind() {
  const a = reminding.value
  if (!a || remindSaving.value) return
  remindSaving.value = true
  try {
    await api(`/api/v1/anniversaries/${a.id}/reminder`, {
      method: 'PATCH',
      body: JSON.stringify({ remind_days_before: remindDays.value }),
    })
    a.remind_days_before = remindDays.value
    showRemind.value = false
  } catch (e) {
    error.value = errText(e, '提醒保存失败')
  } finally {
    remindSaving.value = false
  }
}

// 农历换算预览：农历 → 最近一个公历日期（含闰月语义）
const lunarPreview = computed(() =>
  fType.value === 'lunar' ? nextLunarOccurrence(fLunarMonth.value, fLunarDay.value, fLunarLeap.value) : null,
)
const lunarPreviewLabel = computed(() =>
  fType.value === 'lunar' ? lunarDayText(fLunarMonth.value, fLunarDay.value, fLunarLeap.value) : '',
)
// 农历新增统一结构：date = 真实农历年月日（YYYY-MM-DD，按农历语义，如 1996-05-15）
const lunarRefDate = computed(() => {
  if (fType.value !== 'lunar') return ''
  const mm = String(fLunarMonth.value).padStart(2, '0')
  const dd = String(fLunarDay.value).padStart(2, '0')
  return `${fLunarYear.value}-${mm}-${dd}`
})

async function load() {
  // 静默刷新：仅冷启动（无数据）时亮骨架屏；已有数据时后台重取、失败不打扰旧内容
  const cold = items.value.length === 0
  if (cold) {
    loading.value = true
    error.value = ''
  }
  try {
    const res = await api<{ items: Anniv[] }>('/api/v1/anniversaries')
    items.value = res.items
    error.value = ''
  } catch (e) {
    if (cold) error.value = errText(e, '加载失败')
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
    items.value = items.value.filter((x) => x.id !== a.id) // 原地移除，不再整表重载
    toast.value = RECYCLE_TOAST
  } catch (e) {
    error.value = errText(e, '删除失败')
  } finally {
    deleting.value = null
  }
}

function closeForm() {
  newForm.value = false
  router.replace('/anniversaries')
}

function resetForm() {
  const today = new Date()
  fName.value = ''
  fType.value = 'solar'
  // 阳历默认今天（记录当下的直觉），用户可在滚轮里改
  fSolarY.value = today.getFullYear()
  fSolarM.value = today.getMonth() + 1
  fSolarD.value = today.getDate()
  fLunarYear.value = today.getFullYear()
  fLunarMonth.value = 8
  fLunarDay.value = 15
  fLunarLeap.value = false
  fRemind.value = 1
  error.value = ''
}

const canSave = computed(() => {
  if (saving.value || !fName.value.trim()) return false
  if (fType.value === 'solar') return !!solarRefDate.value
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
        body: JSON.stringify({ name, type: 'solar', date: solarRefDate.value, remind_days_before: fRemind.value }),
      })
    } else {
      // 统一契约：date = 真实农历年月日 + is_leap 闰月标记；换算/倒计时由服务端推算
      await api('/api/v1/anniversaries', {
        method: 'POST',
        body: JSON.stringify({ name, type: 'lunar', date: lunarRefDate.value, is_leap: fLunarLeap.value, remind_days_before: fRemind.value }),
      })
    }
    closeForm()
    await load()
  } catch (e) {
    error.value = errText(e, '添加失败')
  } finally {
    saving.value = false
  }
}

// ?new=1 → 打开完整表单页（FAB「纪念日」入口；watch 支持停留本页时重复触发）
watch(
  () => route.query.new,
  (v) => {
    if (v === '1' && route.path === '/anniversaries') {
      resetForm()
      newForm.value = true
    }
  },
  { immediate: true },
)

// —— 统一计算「下一次即将到来」（阳历→今年正日；农历→按公历下次），并按最近排序 ——
interface AnnNext {
  days: number
  invalid: boolean // 服务端 -1（哨兵）/ 无日期 / 日期不可解析 → 排序置底并置灰
}

function unifiedNext(a: Anniv): AnnNext {
  // 服务端值优先（mock/后端已推算 days_until）；-1 是「无法推算」哨兵，不得当有效值用
  if (Number.isFinite(a.days_until) && a.days_until >= 0) return { days: a.days_until, invalid: false }
  if ((a.type || 'solar') === 'lunar') {
    // 农历：只能靠离线换算，换算不出（越出 1900-2100 表/无日期）即无效——绝不按公历解读农历 date
    if (a.date) {
      const { m, d } = ymdOf(a.date)
      const occ = nextLunarOccurrence(Number(m), Number(d), a.is_leap === true)
      if (occ) return { days: daysUntilDate(occ), invalid: false }
    }
    return { days: Number.MAX_SAFE_INTEGER, invalid: true }
  }
  // 阳历：按公历 date 每年重复；空日期/不可解析（NaN）→ 无效
  if (a.date) {
    const days = daysUntilDate(a.date)
    if (Number.isFinite(days)) return { days, invalid: false }
  }
  return { days: Number.MAX_SAFE_INTEGER, invalid: true }
}

const sortedItems = computed(() => [...items.value].sort((x, y) => unifiedNext(x).days - unifiedNext(y).days))

function rowMeta(a: Anniv): AnnivRowMeta {
  const n = unifiedNext(a)
  const hot = n.days <= 30
  const origin = a.date ? `原始日期 ${a.date}` : ''
  // 日期不可解析（含空）时不再展示「每年 X 月 X 日 / 下次 YYYY-MM-DD」这类假信息
  const md = a.date ? ymdOf(a.date) : null
  const ok = !!md && Number.isFinite(md.m) && Number.isFinite(md.d)
  if ((a.type || 'solar') === 'lunar') {
    const isLeap = a.is_leap === true
    const occ = ok ? nextLunarOccurrence(md!.m, md!.d, isLeap) : null
    return { kind: 'lunar', hot, anniv: ok ? `每年${lunarDayText(md!.m, md!.d, isLeap)}` : '', next: `下次 ${occ || '—'}`, origin, days: n.days, invalid: n.invalid }
  }
  return { kind: 'solar', hot, anniv: ok ? `每年 ${md!.m} 月 ${md!.d} 日` : '', next: `下次 ${ok ? nextSolarDate(a.date) : '—'}`, origin, days: n.days, invalid: n.invalid }
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
  invalid: boolean // 日期无效（服务端 -1 / 无日期 / 不可解析）→ 右侧显示「日期无效」并置灰
}


onActivated(async () => {
  await load()
  // 以 query 为真源回写表单开合（修复：经 TabBar 而非 closeForm 离开时，回来不应停在表单态）
  newForm.value = route.query.new === '1'
  // 长按是隐形手势：首次进入提示一次（本地记住）
  if (onceHint(KEYS.HINT_LONGPRESS)) toast.value = '长按纪念日可移入回收站'
})
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
        <div class="group form-rows">
          <label class="frow">
            <input v-model="fName" class="cell-input" placeholder="名字（如：中秋）" maxlength="30" />
          </label>
          <div class="frow">
            <span class="rlabel">类型</span>
            <div class="seg">
              <button :class="{ on: fType === 'solar' }" @click="fType = 'solar'"><CalendarDays :size="14" stroke-width="2" /> 阳历</button>
              <button :class="{ on: fType === 'lunar' }" @click="fType = 'lunar'"><Moon :size="14" stroke-width="2" /> 农历</button>
            </div>
          </div>
          <button class="frow frow-tap" @click="showPicker = true">
            <span class="rlabel">日期</span>
            <span class="rvalue">{{ pickedDateLabel }}</span>
            <ChevronRight class="rchev" :size="15" stroke-width="2" />
          </button>
          <div class="frow">
            <span class="rlabel">提前提醒</span>
            <select v-model.number="fRemind" class="row-select" aria-label="提前提醒">
              <option v-for="d in remindOptions" :key="d" :value="d">{{ d === 0 ? '当天提醒' : `提前 ${d} 天提醒` }}</option>
            </select>
          </div>
        </div>
        <p v-if="fType === 'lunar' && lunarPreviewLabel" class="foot-hint">
          你选的是 <b>{{ lunarPreviewLabel }}</b>
          <template v-if="lunarPreview">（本次 {{ lunarPreview }}）</template>
        </p>
        <p v-else-if="fType === 'lunar'" class="foot-hint warn">该农历日期在近几十年内不存在，请调整月份/日期</p>

        <p v-if="error" class="error">{{ error }}</p>

        <WheelDatePicker
          v-model:show="showPicker"
          :mode="fType"
          :year="fType === 'solar' ? fSolarY : fLunarYear"
          :month="fType === 'solar' ? fSolarM : fLunarMonth"
          :day="fType === 'solar' ? fSolarD : fLunarDay"
          :leap="fType === 'lunar' && fLunarLeap"
          @confirm="applyPickedDate"
        />
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

        <div
          v-for="a in rows"
          :key="a.id"
          class="row pressable"
          :class="{ pressing: pressing === a.id }"
          @pointerdown="pressDown($event, a.id, () => askRemove(a))"
          @pointermove="pressMove"
          @pointerup="pressUp"
          @pointerleave="pressUp"
          @pointercancel="pressUp"
        >
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
              <button class="meta-line remind" @pointerdown.stop @click="askRemind(a)">
                <Bell :size="12" stroke-width="2" />
                {{ a.remind_days_before === 0 ? '当天提醒' : `提前 ${a.remind_days_before} 天提醒` }}
              </button>
            </div>
          </div>
          <div class="count">
            <span v-if="a.meta.invalid" class="days-invalid">日期无效</span>
            <span v-else class="days-num num" :class="{ far: !a.meta.hot }"><b>{{ a.meta.days }}</b><small>天</small></span>
          </div>
        </div>
      </div>

      <!-- D6 提醒设置 sheet -->
      <Sheet
        v-if="reminding"
        v-model:show="showRemind"
        title="提醒设置"
        confirm-text="保存"
        :confirm-disabled="remindSaving"
        @confirm="saveRemind"
      >
        <p class="s-hint">纪念日「{{ reminding.name }}」</p>
        <select v-model.number="remindDays" class="s-input">
          <option v-for="d in remindOptions" :key="d" :value="d">{{ d === 0 ? '当天提醒' : `提前 ${d} 天提醒` }}</option>
        </select>
      </Sheet>

      <!-- B9 删除确认 sheet -->
      <ConfirmSheet
        v-if="deleting"
        v-model:show="showDelete"
        :title="RECYCLE_TITLE"
        :message="recycleMsg(deleting.name, peerNotice(auth.user?.id, deleting.created_by))"
        :confirm-text="RECYCLE_CONFIRM"
        :confirm-danger="true"
        @confirm="confirmRemove"
      />
      <AppToast :text="toast" @done="toast = ''" />
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
  color: var(--tint);
  cursor: pointer;
  padding: 4px;
  display: flex;
}
/* 导航栏动作 = tint 色文字按钮（iOS HIG），不用填充 pill */
.np-save {
  border: none;
  background: none;
  color: var(--tint);
  font-size: 17px;
  cursor: pointer;
  padding: 4px;
}
.np-save:disabled {
  opacity: 0.4;
}
.af-body {
  flex: 1;
  overflow-y: auto;
  padding: 12px 16px 32px;
}
/* 行结构样式（.form-rows/.frow/.rlabel/.rvalue/.cell-input/.row-select）已提升到
   App.vue 全局，与 .group .row 同族复用；此处只放本页专有项 */
.seg {
  display: flex;
  background: rgba(120, 120, 128, 0.16);
  border-radius: 9px;
  padding: 2px;
  margin: 0 0 0 auto;
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
/* 行结构样式（.frow/.frow-tap/.rlabel/.rvalue/.row-select/.cell-input）已提升到 App.vue 全局，
   与 .group .row 同族复用；此处只放本页专有项 */
.rchev {
  color: var(--muted-ink);
  opacity: 0.6;
  flex-shrink: 0;
}
.foot-hint {
  font-size: 13px;
  color: var(--muted-ink);
  margin: 10px 4px 0;
}
.foot-hint b {
  color: var(--primary-deep);
  font-weight: 600;
}
.foot-hint.warn {
  color: var(--danger);
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
/* D6 提醒入口：贴在「下次/每年」小字下方，点开改天数 */
.meta-line.remind {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  border: none;
  background: none;
  padding: 0;
  font-size: 11px;
  color: var(--tint);
  cursor: pointer;
  width: max-content;
}
.s-hint {
  margin: 0 0 8px;
  font-size: 13px;
  color: var(--muted-ink);
}
.s-input {
  width: 100%;
  padding: 10px 12px;
  border-radius: 10px;
  border: 1px solid var(--rule);
  background: var(--surface);
  color: var(--ink);
  font-size: 14px;
}
.count {
  display: flex;
  align-items: baseline;
  flex-shrink: 0;
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
.days-num.far {
  color: var(--muted-ink);
}
.days-invalid {
  font-size: 12px;
  color: var(--muted-ink);
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