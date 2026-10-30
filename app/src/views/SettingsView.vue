<script lang="ts">
import type { AppMode } from '../stores/mode'
// 模式暂存草稿：模块级，跨二级页往返保留（进代理/关于子页再返回不丢选择）；
// 离开设置页时由 onBeforeRouteLeave 消费（生效）或丢弃
let modeDraft: AppMode | null = null
</script>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { onBeforeRouteLeave, useRouter } from 'vue-router'
import { ArrowDownToLine, ChevronRight, CircleCheck, Globe, Info, Layers, LogOut, RefreshCw } from 'lucide-vue-next'
import { api, errText } from '../api/client'
import PageLayout from '../components/PageLayout.vue'
import Sheet from '../components/Sheet.vue'
import { useAuth } from '../stores/auth'
import { getProxies } from '../utils/appUpdate'
import { useMode } from '../stores/mode'
import { KEYS } from '../utils/storage'
import { ACCENTS, ACCENT_IDS } from '../utils/accents'
import { useAppUpdate } from '../composables/useAppUpdate'
import { aiReasonText, AI_STATUS_OFFLINE, type AIStatus } from '../utils/aiStatus'

// 版本号来自构建期注入（vite.config.ts 读 src-tauri/tauri.conf.json），不硬编码
const appVersion = __APP_VERSION__

const router = useRouter()
const auth = useAuth()
const modeStore = useMode()

const theme = ref<string>(localStorage.getItem(KEYS.THEME) || 'light')
const accent = ref<string>(localStorage.getItem(KEYS.ACCENT) || 'rose')

// 模式行 + 选择浮层：点选项只暂存，离开设置页（onBeforeRouteLeave）才生效——
// 高影响切换防误触；去二级页（/settings/*）往返与退出登录都不触发生效
const selected = ref<AppMode>(modeDraft ?? modeStore.mode)
const aiStatus = ref<AIStatus | null>(null)
const aiStatusLoading = ref(false)
const aiDisabled = computed(() => aiStatus.value !== null && !aiStatus.value.available)
const sheetShow = ref(false)
// 草稿未生效：摘要行内徽标显示生效时机（行高不变，避免页面纵向跳动）
const draftDirty = computed(() => selected.value !== modeStore.mode)
// AI 不可用原因（sheet 内 AI 卡展示；与 AI 对话页同一套文案口径）
const aiReason = computed(() =>
  aiStatusLoading.value ? '正在看 AI 能不能用…' : (aiStatus.value?.reason || AI_STATUS_OFFLINE)
)

async function loadAIStatus() {
  aiStatusLoading.value = true
  try {
    const s = await api<AIStatus>('/api/v1/chat/status')
    // reason 加工统一走 utils/aiStatus（与 AI 对话页同一套文案口径）
    aiStatus.value = { available: s.available, reason: aiReasonText(s) }
  } catch {
    aiStatus.value = { available: false, reason: AI_STATUS_OFFLINE }
  } finally {
    aiStatusLoading.value = false
  }
}

function pick(m: AppMode) {
  // AI 可用性未确认前禁选（宁可不选，不可误切）；不可用置灰
  if (m === 'ai' && (aiStatusLoading.value || aiDisabled.value)) return
  selected.value = m
  modeDraft = m
  // picker 手感：点选项即选定并收起浮层
  sheetShow.value = false
}

onBeforeRouteLeave((to) => {
  if (to.path.startsWith('/settings')) return true // 去二级页往返：草稿保留
  if (to.path === '/login') { modeDraft = null; return true } // 退出登录：草稿丢弃
  if (selected.value === modeStore.mode) { modeDraft = null; return true }
  // AI 竞态兜底：status 未确认可用时 AI 永不生效，暂存直接丢弃
  if (selected.value === 'ai' && (aiStatus.value === null || aiDisabled.value)) {
    modeDraft = null
    return true
  }
  modeStore.setMode(selected.value)
  modeDraft = null
  return selected.value === 'ai' ? '/chat' : '/home'
})

// AI 偏好（输出风格）：个人体验项，模式选择浮层选到 AI 时显示；点风格即保存（PATCH /api/v1/me）
// 风格选项从 /api/v1/styles 拉取（内置 + 自定义）；拉取失败用种子清单兜底
const aiStyle = ref('文艺')
const aiPrefToast = ref('')
let aiPrefTimer: number | undefined // 反馈文案的自动还原计时（卸载时清理）
const styleOpts = ref<string[]>(['文艺', '幽默', '简洁'])
const stylesLoaded = ref(false)

// 应用内更新：检查更新动作留设置页一级（右侧显示状态），代理配置收进二级页
const {
  state: updateState,
  progress: updateProgress,
  manualNotice,
  check: checkUpdate,
} = useAppUpdate()
const updateToast = ref('')
let updateToastTimer: number | undefined
watch(manualNotice, (m) => {
  if (!m) return
  updateToast.value = m
  if (updateToastTimer) clearTimeout(updateToastTimer)
  updateToastTimer = window.setTimeout(() => (updateToast.value = ''), 2600)
})
const updatePct = computed(() => {
  const { received, total } = updateProgress.value
  return total > 0 ? Math.min(100, Math.round((received / total) * 100)) : 0
})
const updateStateText = computed(() => {
  switch (updateState.value) {
    case 'checking':
      return '检查中…'
    case 'downloading':
      return `下载中 ${updatePct.value}%`
    case 'ready':
      return '待安装'
    case 'handoff':
      return '安装中'
    default:
      return ''
  }
})
function onCheckUpdate() {
  void checkUpdate(true)
}
// 代理数量只读展示（列表管理在二级页；从子页返回时本页重挂、数量即新值）
const ghProxyCount = getProxies().length

function syncAIPref() {
  const server = auth.user?.ai_style || defaultStyleName.value || '文艺'
  // 无未保存的本地改动才回填：styles 迟到不覆盖用户刚点选的风格
  if (aiStyle.value === aiStyleSaved.value) aiStyle.value = server
  aiStyleSaved.value = aiStyle.value // 服务端现状即「已保存」基准
}
const defaultStyleName = ref('')
async function loadStyles() {
  try {
    const res = await api<{ items: { id: number; name: string }[]; default_style_id: number }>('/api/v1/styles')
    if (res.items?.length) {
      styleOpts.value = res.items.map((s) => s.name)
      defaultStyleName.value = res.items.find((s) => s.id === res.default_style_id)?.name ?? ''
    }
    stylesLoaded.value = true
  } catch {
    /* 兜底清单已就位 */
  }
}
// 点风格即保存：反馈文案占用提示行（mode-hint 定高，不跳行）。
// aiStyleSaved = 服务端确认值：失败回滚到它；上次失败的同值允许重发（重试）
const aiStyleSaved = ref('')
let prefSeq = 0
function pickStyle(s: string) {
  if (aiStyle.value === s && aiStyleSaved.value === s) return
  aiStyle.value = s
  void saveAIPref()
}
async function saveAIPref() {
  const seq = ++prefSeq
  const target = aiStyle.value
  aiPrefToast.value = ''
  try {
    await api('/api/v1/me', {
      method: 'PATCH',
      body: JSON.stringify({ ai_style: target }),
    })
    if (seq !== prefSeq) return // 更新请求已发出：丢弃过期响应
    if (auth.user) auth.user.ai_style = target
    aiStyleSaved.value = target
    aiPrefToast.value = '已保存'
  } catch (e) {
    if (seq !== prefSeq) return
    aiPrefToast.value = errText(e, '保存失败')
    // 回滚本地高亮到服务端确认值，不让 UI 显示服务端没有的风格
    if (aiStyle.value === target) aiStyle.value = aiStyleSaved.value || '文艺'
  }
  if (seq === prefSeq) {
    if (aiPrefTimer) window.clearTimeout(aiPrefTimer)
    aiPrefTimer = window.setTimeout(() => (aiPrefToast.value = ''), 1800)
  }
}

onUnmounted(() => {
  if (aiPrefTimer) window.clearTimeout(aiPrefTimer)
  if (updateToastTimer) window.clearTimeout(updateToastTimer)
})

function applyTheme() {
  localStorage.setItem(KEYS.THEME, theme.value)
  const dark = theme.value === 'dark' || (theme.value === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches)
  document.documentElement.dataset.theme = dark ? 'dark' : 'light'
  localStorage.setItem(KEYS.ACCENT, accent.value)
  if (ACCENTS[accent.value]) document.documentElement.dataset.accent = accent.value
}

async function logout() {
  // 先吊销服务端 token 再跳转：logout() 内部保证本地凭证一定被清（失败也清）
  await auth.logout()
  await router.replace('/login')
}

// 风格清单按需加载：模式区选到 AI 且未加载过时补拉一次，避免空选项
watch(selected, (m) => {
  if (m === 'ai' && !stylesLoaded.value) void loadStyles().then(syncAIPref)
})

onMounted(() => {
  applyTheme()
  void loadAIStatus()
  if (selected.value === 'ai') void loadStyles().then(() => { stylesLoaded.value = true; syncAIPref() })
  syncAIPref()
})
</script>

<template>
  <PageLayout title="设置" back standalone @back="router.back()">
    <div class="content">
      <!-- 模式（页首摘要行）：点击升起底部选择浮层（带界面预览的选项卡）；点选只暂存，离开设置页才生效。
           草稿未生效时行内显示徽标（行高不变，避免页面纵向跳动） -->
      <div class="group">
        <button class="row row-link" @click="sheetShow = true">
          <span class="row-ic"><Layers :size="16" stroke-width="1.8" /></span>
          <span class="label">模式</span>
          <span v-if="draftDirty" class="pending-badge">离开设置时生效</span>
          <span class="value">{{ selected === 'ai' ? 'AI 模式' : '传统模式' }}</span>
          <ChevronRight :size="14" stroke-width="2" class="value-link" />
        </button>
      </div>

      <!-- 外观（通用项） -->
      <section class="grp">
        <div class="sec">外观</div>
        <div class="group">
          <div class="seg-wrap">
            <div class="seg">
              <button v-for="m in ['system', 'light', 'dark']" :key="m" :class="{ on: theme === m }" @click="theme = m; applyTheme()">
                {{ { system: '跟随系统', light: '浅色', dark: '深色' }[m] }}
              </button>
            </div>
          </div>
          <div class="accent-row">
            <button
              v-for="id in ACCENT_IDS"
              :key="id"
              class="accent-dot"
              :class="{ on: accent === id }"
              :aria-label="ACCENTS[id].label"
              :title="ACCENTS[id].label"
              :style="{ '--swatch': ACCENTS[id].light.primary }"
              @click="accent = id; applyTheme()"
            />
          </div>
        </div>
      </section>

      <!-- AI 偏好（模式选择浮层选到 AI 时显示；只列不藏——显示时机不同；点风格即保存） -->
      <section v-if="selected === 'ai'" class="grp">
        <div class="sec">AI 偏好</div>
        <div class="group">
          <div class="seg-wrap">
            <div class="seg">
              <button v-for="s in styleOpts" :key="s" :class="{ on: aiStyle === s }" @click="pickStyle(s)">{{ s }}</button>
            </div>
            <p class="mode-hint" aria-live="polite">{{ aiPrefToast || 'AI 输出风格偏好（仅对你发起的 AI 会话生效）' }}</p>
          </div>
        </div>
      </section>

      <!-- 通用：常用动作与二级入口（专业配置收二级：代理管理、版本/服务端/项目主页） -->
      <section class="grp">
        <div class="sec">通用</div>
        <div class="group">
          <button
            class="row row-link"
            :disabled="updateState === 'checking'"
            @click="onCheckUpdate"
          >
            <span class="row-ic"><ArrowDownToLine :size="16" stroke-width="1.8" /></span>
            <span class="label">检查更新</span>
            <!-- 状态与反馈都走行值（行高不变）：状态优先，无状态时显示检查结果提示 -->
            <span v-if="updateStateText || updateToast" class="value num value-trunc" aria-live="polite">{{ updateStateText || updateToast }}</span>
            <RefreshCw
              v-else
              :size="14"
              stroke-width="2"
              class="value-link"
            />
          </button>
          <button class="row row-link" @click="router.push('/settings/proxy')">
            <span class="row-ic"><Globe :size="16" stroke-width="1.8" /></span>
            <span class="label">更新代理</span>
            <span class="value num">{{ ghProxyCount }} 个</span>
            <ChevronRight :size="14" stroke-width="2" class="value-link" />
          </button>
          <button class="row row-link" @click="router.push('/settings/about')">
            <span class="row-ic"><Info :size="16" stroke-width="1.8" /></span>
            <span class="label">关于</span>
            <span class="value num">{{ appVersion }}</span>
            <ChevronRight :size="14" stroke-width="2" class="value-link" />
          </button>
        </div>
      </section>

      <!-- 退出登录（通用项，固定垫底） -->
      <div class="group">
        <button class="row logout-row" @click="logout">
          <span class="row-ic danger"><LogOut :size="16" stroke-width="1.8" /></span>
          <span class="label danger-text">退出登录</span>
        </button>
      </div>
    </div>

    <!-- 模式选择浮层（picker：点选项即选定并收起）：选项卡带 CSS 迷你界面预览，选前即见切换后的形态 -->
    <Sheet v-model:show="sheetShow" title="切换模式" hide-foot>
      <button class="opt" :class="{ on: selected === 'traditional' }" @click="pick('traditional')">
        <span class="mock" aria-hidden="true">
          <span class="m-bar m-title"></span>
          <span class="m-card"><span class="m-media"></span><span class="m-lines"><i></i><i></i></span></span>
          <span class="m-card"><span class="m-media"></span><span class="m-lines"><i></i><i></i></span></span>
          <span class="m-tabs"><i></i><i></i><i></i><i></i><i></i></span>
        </span>
        <span class="opt-text">
          <span class="opt-name">传统模式</span>
          <span class="opt-desc">手动记录与浏览，五个 tab 全在手里</span>
        </span>
        <CircleCheck v-if="selected === 'traditional'" :size="18" stroke-width="2" class="opt-check" />
      </button>
      <button class="opt" :class="{ on: selected === 'ai', off: aiDisabled || aiStatusLoading }" :disabled="aiDisabled || aiStatusLoading" :aria-disabled="aiDisabled || aiStatusLoading" @click="pick('ai')">
        <span class="mock" aria-hidden="true">
          <span class="m-bub l"></span>
          <span class="m-bub r"></span>
          <span class="m-bub l wide"></span>
          <span class="m-input"></span>
        </span>
        <span class="opt-text">
          <span class="opt-name">AI 模式</span>
          <span class="opt-desc">像聊天一样记录，说一句话就行</span>
          <span v-if="aiDisabled || aiStatusLoading" class="opt-reason">{{ aiReason }}</span>
        </span>
        <CircleCheck v-if="selected === 'ai'" :size="18" stroke-width="2" class="opt-check" />
      </button>
    </Sheet>
  </PageLayout>
</template>
<style scoped src="../styles/settings.css"></style>
<style scoped>
/* 草稿未生效徽标（行内展示：行高不变，页面零跳动）。
   文字承载独立信息，按 PRODUCT「色彩体系（iOS 风格）」次要文字 ≥4.5:1 口径用 muted-ink，淡底仅作衬 */
.pending-badge {
  flex-shrink: 0;
  font-size: 11px;
  line-height: 1;
  padding: 4px 8px;
  border-radius: 999px;
  background: color-mix(in srgb, var(--primary) 12%, transparent);
  color: var(--muted-ink);
}

/* 模式选择浮层选项卡：左迷你预览 + 中文案 + 右选中勾 */
.opt {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  box-sizing: border-box;
  border: 1.5px solid transparent;
  border-radius: 12px;
  background: rgba(120, 120, 128, 0.06);
  padding: 12px;
  text-align: left;
  color: var(--ink);
  cursor: pointer;
}
.opt.on {
  border-color: var(--primary);
  background: color-mix(in srgb, var(--primary) 6%, transparent);
}
/* 深底上 0.06 几乎不可见：覆写加深（先例 AnniversariesView .tg-solar） */
:root[data-theme='dark'] .opt {
  background: rgba(120, 120, 128, 0.14);
}
.opt.off {
  opacity: 0.5;
  cursor: not-allowed;
}
.opt-text {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}
.opt-name {
  font-size: 15px;
  font-weight: 600;
}
.opt-desc,
.opt-reason {
  font-size: 12px;
  line-height: 1.4;
  color: var(--muted-ink);
}
.opt-check {
  color: var(--primary);
  flex-shrink: 0;
}

/* 迷你界面预览骨架（纯 CSS，无图片资产）：外框 = 手机屏 */
.mock {
  width: 52px;
  height: 78px;
  flex-shrink: 0;
  box-sizing: border-box;
  border: 0.5px solid var(--rule);
  border-radius: 9px;
  background: var(--surface);
  padding: 7px 6px;
  display: flex;
  flex-direction: column;
  gap: 5px;
}
/* 传统：标题条 + 两张内容卡（媒体块+两行文字）+ 底部五 tab */
.m-bar {
  height: 5px;
  border-radius: 2.5px;
  background: rgba(120, 120, 128, 0.28);
}
.m-title {
  width: 55%;
  align-self: center;
}
.m-card {
  display: flex;
  gap: 4px;
  align-items: center;
}
.m-media {
  width: 14px;
  height: 14px;
  border-radius: 4px;
  background: rgba(120, 120, 128, 0.22);
  flex-shrink: 0;
}
.m-lines {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.m-lines i {
  height: 3px;
  border-radius: 1.5px;
  background: rgba(120, 120, 128, 0.22);
}
.m-lines i + i {
  width: 65%;
}
.m-tabs {
  margin-top: auto;
  display: flex;
  justify-content: space-between;
  padding: 0 3px;
}
.m-tabs i {
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: rgba(120, 120, 128, 0.28);
}
/* AI：左右聊天气泡 + 底部输入条（用户泡用主色淡底） */
.m-bub {
  height: 10px;
  border-radius: 5px;
  background: rgba(120, 120, 128, 0.2);
}
.m-bub.l {
  width: 64%;
}
.m-bub.l.wide {
  width: 78%;
}
.m-bub.r {
  width: 46%;
  margin-left: auto;
  background: color-mix(in srgb, var(--primary) 45%, transparent);
}
.m-input {
  margin-top: auto;
  height: 11px;
  border-radius: 5.5px;
  border: 0.5px solid rgba(120, 120, 128, 0.3);
}

/* 强调色选择行（暖色系预设） */
.accent-row {
  display: flex;
  gap: 12px;
  padding: 0 16px 14px;
}
.accent-dot {
  width: 30px;
  height: 30px;
  border-radius: 50%;
  border: none;
  background: var(--swatch, var(--primary));
  box-shadow: inset 0 0 0 1px rgba(0, 0, 0, 0.08);
  cursor: pointer;
  position: relative;
}
.accent-dot.on {
  box-shadow: 0 0 0 2px var(--surface), 0 0 0 4px var(--tint);
}
.accent-dot:active {
  transform: scale(0.94);
}

/* segmented */
.seg-wrap {
  padding: 8px 16px 12px;
}
.seg {
  display: flex;
  background: rgba(120, 120, 128, 0.16);
  border-radius: 9px;
  padding: 2px;
}
.seg button {
  flex: 1;
  border: none;
  background: none;
  padding: 10px 0;
  min-height: 36px; /* 触控目标 ≥36px */
  border-radius: 7px;
  font-size: 13px;
  color: var(--ink);
  cursor: pointer;
}
.seg button.on {
  background: var(--surface);
  box-shadow: var(--shadow);
  font-weight: 600;
}
.mode-hint {
  margin: 8px 0 0;
  font-size: 12px;
  color: var(--muted-ink);
  /* 兼保存反馈行：单行恒定高，文案长短不引起布局跳动 */
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.logout-row .danger-text {
  color: var(--danger);
}
</style>
