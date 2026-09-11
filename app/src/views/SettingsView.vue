<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ChevronLeft, LogOut, ExternalLink } from 'lucide-vue-next'
import { api, errText, getBase } from '../api/client'
import { useAuth } from '../stores/auth'
import { useMode, type AppMode } from '../stores/mode'
import { KEYS } from '../utils/storage'
import { ACCENTS, ACCENT_IDS } from '../utils/accents'

const PROJECT_HOME = 'https://github.com/bookandmusic/love-life'

const router = useRouter()
const auth = useAuth()
const modeStore = useMode()

const theme = ref<string>(localStorage.getItem(KEYS.THEME) || 'light')
const accent = ref<string>(localStorage.getItem(KEYS.ACCENT) || 'rose')

// AI 偏好（输出风格 + 对话保留时长）：读 auth.user，改后 PATCH /api/v1/me
// 风格选项从 /api/v1/styles 拉取（内置 + 自定义）；拉取失败用种子清单兜底
const aiStyle = ref('文艺')
const retentionDays = ref(30)
const aiPrefSaving = ref(false)
const aiPrefToast = ref('')
let aiPrefTimer: number | undefined // 「已保存」标签的自动隐藏计时（卸载时清理）
const styleOpts = ref<string[]>(['文艺', '幽默', '简洁'])
const RETENTION_OPTS = [
  { v: 7, label: '7 天' },
  { v: 30, label: '30 天' },
  { v: 90, label: '90 天' },
  { v: -1, label: '永久' },
]
function syncAIPref() {
  aiStyle.value = auth.user?.ai_style || defaultStyleName.value || '文艺'
  retentionDays.value = auth.user?.chat_retention_days ?? 30
}
const defaultStyleName = ref('')
async function loadStyles() {
  try {
    const res = await api<{ items: { id: number; name: string }[]; default_style_id: number }>('/api/v1/styles')
    if (res.items?.length) {
      styleOpts.value = res.items.map((s) => s.name)
      defaultStyleName.value = res.items.find((s) => s.id === res.default_style_id)?.name ?? ''
    }
  } catch {
    /* 兜底清单已就位 */
  }
}
async function saveAIPref() {
  aiPrefSaving.value = true
  aiPrefToast.value = ''
  try {
    await api('/api/v1/me', {
      method: 'PATCH',
      body: JSON.stringify({ ai_style: aiStyle.value, chat_retention_days: retentionDays.value }),
    })
    if (auth.user) {
      auth.user.ai_style = aiStyle.value
      auth.user.chat_retention_days = retentionDays.value
    }
    aiPrefToast.value = '已保存'
  } catch (e) {
    aiPrefToast.value = errText(e, '保存失败')
  } finally {
    aiPrefSaving.value = false
    if (aiPrefTimer) window.clearTimeout(aiPrefTimer)
    aiPrefTimer = window.setTimeout(() => (aiPrefToast.value = ''), 1800)
  }
}

onUnmounted(() => {
  if (aiPrefTimer) window.clearTimeout(aiPrefTimer)
})

// AI 可用性（阶段4.4 B6-②：后台未配置模型 Key 时 AI 模式置灰并提示原因）
const aiStatus = ref<{ available: boolean; reason: string } | null>(null)
const aiStatusLoading = ref(false)

function applyTheme() {
  localStorage.setItem(KEYS.THEME, theme.value)
  const dark = theme.value === 'dark' || (theme.value === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches)
  document.documentElement.dataset.theme = dark ? 'dark' : 'light'
  localStorage.setItem(KEYS.ACCENT, accent.value)
  if (ACCENTS[accent.value]) document.documentElement.dataset.accent = accent.value
}

const aiDisabled = computed(() => aiStatus.value !== null && !aiStatus.value.available)

async function loadAIStatus() {
  aiStatusLoading.value = true
  try {
    aiStatus.value = await api<{ available: boolean; reason: string }>('/api/v1/chat/status')
  } catch {
    aiStatus.value = { available: false, reason: '无法连接服务端，AI 模式暂不可用' }
  } finally {
    aiStatusLoading.value = false
  }
}

function applyMode(m: AppMode) {
  if (m === 'ai' && aiDisabled.value) return
  if (modeStore.mode === m) return
  modeStore.setMode(m)
  router.replace(m === 'ai' ? '/chat' : '/home')
}

function logout() {
  auth.logout()
  router.replace('/login')
}

onMounted(() => {
  applyTheme()
  void loadAIStatus()
  void loadStyles().then(syncAIPref)
  syncAIPref()
})
</script>

<template>
  <div class="settings-page page-col">
    <!-- 毛玻璃导航栏（demo subpage 风格） -->
    <header class="navbar glass">
      <button class="nav-btn" aria-label="返回" @click="router.back()" style="margin-right: auto">
        <ChevronLeft :size="22" stroke-width="2.5" />
     </button>
      <span class="title">设置</span>
   </header>

    <div class="content">
      <!-- APP 模式（传统 / AI）：切换后立即生效；AI 未配置时置灰并提示原因 -->
      <div class="group">
        <div class="section-label">APP 模式</div>
        <div class="seg-wrap">
          <div class="seg">
            <button :class="{ on: modeStore.mode === 'traditional' }" @click="applyMode('traditional')">传统模式</button>
            <button :class="{ on: modeStore.mode === 'ai', off: aiDisabled }" :disabled="aiDisabled" @click="applyMode('ai')">AI 模式</button>
          </div>
          <p class="mode-hint">
            {{ aiStatusLoading ? '正在检查 AI 可用性…' : aiStatus?.reason || '切换后立即生效' }}
          </p>
        </div>
      </div>

      <!-- AI 偏好：输出风格 + 对话保留时长（个人体验项，PATCH /api/v1/me） -->
      <div class="group">
        <div class="section-label">AI 偏好</div>
        <div class="seg-wrap">
          <div class="seg">
            <button v-for="s in styleOpts" :key="s" :class="{ on: aiStyle === s }" @click="aiStyle = s">{{ s }}</button>
          </div>
          <p class="mode-hint">AI 输出风格偏好</p>
        </div>
        <div class="seg-wrap">
          <div class="seg">
            <button v-for="r in RETENTION_OPTS" :key="r.v" :class="{ on: retentionDays === r.v }" @click="retentionDays = r.v">{{ r.label }}</button>
          </div>
          <p class="mode-hint">对话历史保留时长</p>
        </div>
        <div class="save-row">
          <button class="save-btn" :disabled="aiPrefSaving" @click="saveAIPref">{{ aiPrefSaving ? '保存中…' : '保存' }}</button>
          <span v-if="aiPrefToast" class="save-toast">{{ aiPrefToast }}</span>
        </div>
      </div>

      <!-- 外观 -->
      <div class="group">
        <div class="section-label">外观</div>
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

      <!-- 关于 -->
      <div class="group">
        <div class="row">
          <span class="label">版本</span>
          <span class="value num">0.1.0</span>
        </div>
        <div class="row">
          <span class="label">服务端</span>
          <span class="value server-url">{{ getBase() || '（未配置）' }}</span>
        </div>
        <a class="row row-link" :href="PROJECT_HOME" target="_blank" rel="noopener">
          <span class="label">项目主页</span>
          <ExternalLink :size="14" stroke-width="1.8" class="value-link" />
        </a>
      </div>

      <!-- 退出登录 -->
      <div class="group">
        <button class="row logout-row" @click="logout">
          <span class="label danger-text">退出登录</span>
          <LogOut :size="16" stroke-width="2" class="danger-text" />
        </button>
      </div>
    </div>
 </div>
</template>
<style scoped>
.settings-page {
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
  align-items: center;
}
.content {
  flex: 1;
  overflow-y: auto;
  -webkit-overflow-scrolling: touch;
  padding: 12px 16px 24px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}

/* 分组（背景/圆角/阴影继承全局 .group 玻璃卡片，这里只补布局） */
.group {
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
}
.section-label {
  font-size: 13px;
  font-weight: 600;
  color: var(--muted-ink);
  padding: 12px 16px 4px;
}

/* 行 */
.row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 13px 16px;
  width: 100%;
  border: none;
  background: none;
  font-size: 15px;
  color: var(--ink);
  text-align: left;
  cursor: pointer;
}
.row + .row {
  border-top: 0.5px solid var(--rule);
}
.ic {
  width: 32px;
  height: 32px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
.label {
  flex: 1;
}
.ai-hint {
  margin: 0;
  padding: 0 16px 12px;
  font-size: 12px;
  color: var(--muted-ink);
}
.logout-row .danger-text {
  color: var(--danger);
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
  box-shadow: 0 0 0 2px var(--surface), 0 0 0 4px var(--ios-blue);
}
.accent-dot:active {
  transform: scale(0.94);
}
.seg button.off {
  opacity: 0.4;
  cursor: not-allowed;
}
.mode-hint {
  margin: 8px 0 0;
  font-size: 12px;
  color: var(--muted-ink);
}
.save-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 2px 16px 14px;
}
.save-btn {
  border: none;
  background: var(--primary);
  color: #fff;
  border-radius: 999px;
  padding: 8px 20px;
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
}
.save-btn:disabled {
  opacity: 0.45;
}
.save-toast {
  font-size: 13px;
  color: var(--muted-ink);
}
.row-link {
  text-decoration: none;
  color: var(--ink);
}
.value-link {
  color: var(--muted-ink);
  flex-shrink: 0;
}

/* segmented */
.seg-wrap {
  padding: 4px 16px 12px;
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
  padding: 7px 0;
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

/* 关于 */
.value {
  color: var(--muted-ink);
  font-size: 14px;
}
.server-url {
  max-width: 180px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
