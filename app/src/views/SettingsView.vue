<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { LogOut, ExternalLink } from 'lucide-vue-next'
import { api, errText, getBase } from '../api/client'
import PageLayout from '../components/PageLayout.vue'
import { useAuth } from '../stores/auth'
import { useMode, type AppMode } from '../stores/mode'
import { KEYS } from '../utils/storage'
import { ACCENTS, ACCENT_IDS } from '../utils/accents'
import { aiReasonText, AI_STATUS_OFFLINE, type AIStatus } from '../utils/aiStatus'

const PROJECT_HOME = 'https://github.com/bookandmusic/love-life'
// 版本号来自构建期注入（vite.config.ts 读 src-tauri/tauri.conf.json），不硬编码
const appVersion = __APP_VERSION__

const router = useRouter()
const auth = useAuth()
const modeStore = useMode()

const theme = ref<string>(localStorage.getItem(KEYS.THEME) || 'light')
const accent = ref<string>(localStorage.getItem(KEYS.ACCENT) || 'rose')

// AI 偏好（输出风格）：个人体验项，仅 AI 模式显示；读 auth.user，改后 PATCH /api/v1/me
// 风格选项从 /api/v1/styles 拉取（内置 + 自定义）；拉取失败用种子清单兜底
const aiStyle = ref('文艺')
const aiPrefSaving = ref(false)
const aiPrefToast = ref('')
let aiPrefTimer: number | undefined // 「已保存」标签的自动隐藏计时（卸载时清理）
const styleOpts = ref<string[]>(['文艺', '幽默', '简洁'])
const stylesLoaded = ref(false)
function syncAIPref() {
  aiStyle.value = auth.user?.ai_style || defaultStyleName.value || '文艺'
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
async function saveAIPref() {
  aiPrefSaving.value = true
  aiPrefToast.value = ''
  try {
    await api('/api/v1/me', {
      method: 'PATCH',
      body: JSON.stringify({ ai_style: aiStyle.value }),
    })
    if (auth.user) auth.user.ai_style = aiStyle.value
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

// AI 可用性（后台未配置模型 Key 时 AI 模式置灰并提示原因）
const aiStatus = ref<AIStatus | null>(null)
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
    const s = await api<AIStatus>('/api/v1/chat/status')
    // reason 加工统一走 utils/aiStatus（与 AI 对话页同一套文案口径）
    aiStatus.value = { available: s.available, reason: aiReasonText(s) }
  } catch {
    aiStatus.value = { available: false, reason: AI_STATUS_OFFLINE }
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

// 风格清单按需加载：切到 AI 模式且未加载过时补拉一次，避免空选项
watch(() => modeStore.mode, (m) => {
  if (m === 'ai' && !stylesLoaded.value) void loadStyles().then(syncAIPref)
})

onMounted(() => {
  applyTheme()
  void loadAIStatus()
  if (modeStore.mode === 'ai') void loadStyles().then(() => { stylesLoaded.value = true; syncAIPref() })
  syncAIPref()
})
</script>

<template>
  <PageLayout title="设置" back standalone @back="router.back()">
    <div class="content">
      <!-- 模式切换（通用项，两模式都显示）：切换后立即生效；AI 未配置时置灰并提示原因 -->
      <div class="group">
        <div class="section-label">模式</div>
        <div class="seg-wrap">
          <div class="seg">
            <button :class="{ on: modeStore.mode === 'traditional' }" @click="applyMode('traditional')">传统模式</button>
            <button :class="{ on: modeStore.mode === 'ai', off: aiDisabled }" :disabled="aiDisabled" @click="applyMode('ai')">AI 模式</button>
          </div>
          <p class="mode-hint">
            {{
              aiStatusLoading
                ? '正在看 AI 能不能用…'
                : modeStore.mode === 'traditional'
                  ? (aiDisabled ? aiStatus?.reason : '切到 AI 模式可配置 AI 偏好')
                  : (aiStatus?.reason || '切换后立即生效')
            }}
          </p>
        </div>
      </div>

      <!-- 外观（通用项） -->
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
          <span class="value num">{{ appVersion }}</span>
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

      <!-- 退出登录（通用项） -->
      <div class="group">
        <button class="row logout-row" @click="logout">
          <span class="label danger-text">退出登录</span>
          <LogOut :size="16" stroke-width="2" class="danger-text" />
        </button>
      </div>

      <!-- AI 偏好（仅 AI 模式显示的个人体验项；只列不藏——传统模式下由切换器提示去哪配） -->
      <div v-if="modeStore.mode === 'ai'" class="group">
        <div class="section-label">AI 偏好</div>
        <div class="seg-wrap">
          <div class="seg">
            <button v-for="s in styleOpts" :key="s" :class="{ on: aiStyle === s }" @click="aiStyle = s">{{ s }}</button>
          </div>
          <p class="mode-hint">AI 输出风格偏好（仅对你发起的 AI 会话生效）</p>
        </div>
        <div class="save-row">
          <button class="save-btn" :disabled="aiPrefSaving" @click="saveAIPref">{{ aiPrefSaving ? '保存中…' : '保存' }}</button>
          <span v-if="aiPrefToast" class="save-toast">{{ aiPrefToast }}</span>
        </div>
      </div>
    </div>
  </PageLayout>
</template>
<style scoped>
.content {
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
  box-shadow: 0 0 0 2px var(--surface), 0 0 0 4px var(--tint);
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
