<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Eye, EyeOff, Heart, PenLine } from 'lucide-vue-next'
import Sheet from '../components/Sheet.vue'
import { useAuth } from '../stores/auth'
import { errText, getLanBase, getWanBase, setDualBase } from '../api/client'
import { landingRoute } from '../router'
import { probeAndSelect, type ProbeStatus } from '../utils/netProbe'

const router = useRouter()
const auth = useAuth()

// 服务端地址（AP1 双通道，DESIGN §11.2）：内网（在家直连）/ 公网（在外 HTTPS），
// 各自可空——只填一个时直接用该地址；双地址探测优选内网。
// 主界面只留一行摘要，编辑收进 Sheet：每天动线是「进空间」，改地址是低频配置。
type Schema = 'http' | 'https'
const lan = reactive({ schema: 'http' as Schema, addr: '' })
const wan = reactive({ schema: 'https' as Schema, addr: '' })
const username = ref('')
const password = ref('')
const showPass = ref(false)
const showAddr = ref(false)
const error = ref('')
const addrError = ref('')
const loading = ref(false)
const probing = ref(false)
const probe = ref<ProbeStatus | null>(null)

// 初始回填：已存双地址（旧单地址迁移后在公网槽）
function initAddr(row: { schema: Schema; addr: string }, value: string) {
  if (!value) return
  try {
    const u = new URL(value)
    if (u.protocol === 'http:' || u.protocol === 'https:') row.schema = u.protocol === 'https:' ? 'https' : 'http'
    row.addr = u.port ? `${u.hostname}:${u.port}` : u.hostname
  } catch {
    row.addr = value
  }
}
initAddr(lan, getLanBase() || (import.meta.env.VITE_SERVER_URL as string) || '')
initAddr(wan, getWanBase())

function toggleSchema(row: { schema: Schema }) {
  row.schema = row.schema === 'http' ? 'https' : 'http'
}

// 地址校验：host 必填；端口若存在需为 1-65535；空串合法（双地址可任选其一填写）
function validateAddress(raw: string): string | null {
  const addr = raw.trim()
  if (!addr) return null
  let rest = addr.replace(/^https?:\/\//i, '')
  let host = rest
  let port = ''
  const idx = rest.lastIndexOf(':')
  if (idx > 0 && !rest.slice(idx + 1).includes(']') && rest.lastIndexOf(']') < idx) {
    const maybePort = rest.slice(idx + 1)
    if (/^\d+$/.test(maybePort)) {
      host = rest.slice(0, idx)
      port = maybePort
    }
  }
  if (!host) return '请输入服务地址'
  if (port && !(/^\d{1,5}$/.test(port) && +port >= 1 && +port <= 65535)) {
    return '端口需为 1-65535 的数字'
  }
  return null
}

// 与 client 的 fullUrl（相对路径转绝对地址）同名异义，改名避免误用
function resolveUrl(row: { schema: Schema; addr: string }): string {
  const addr = row.addr.trim()
  if (!addr) return ''
  if (/^https?:\/\//i.test(addr)) return addr
  return `${row.schema}://${addr}`
}

// 校验并保存双地址 → 探测连通性 → 优选生效地址；失败返回 false（error 已置）
async function saveAndProbe(): Promise<boolean> {
  error.value = ''
  for (const row of [lan, wan]) {
    const invalid = validateAddress(row.addr)
    if (invalid) {
      error.value = invalid
      return false
    }
  }
  if (!lan.addr.trim() && !wan.addr.trim()) {
    error.value = '请至少填写一个服务地址'
    return false
  }
  setDualBase(resolveUrl(lan), resolveUrl(wan))
  probe.value = await probeAndSelect()
  return true
}

// 主界面摘要：优先内网（在家场景），未配置时给出去编辑的入口文案
const addrLabel = computed(() => lan.addr.trim() || wan.addr.trim() || '未配置')
const addrConfigured = computed(() => !!(lan.addr.trim() || wan.addr.trim()))
// 行内状态点：只在本次探测有结果时显示（App.vue 挂载时已做过一次优选探测，不重复请求）
const addrState = computed(() => {
  const p = probe.value
  if (!p) return null
  if ((p.lanConfigured && p.lan) || (p.wanConfigured && p.wan)) return true
  if (p.lanConfigured || p.wanConfigured) return false
  return null
})

// 打开编辑：清掉上一次的探测结果，避免展示陈旧状态
function openAddr() {
  addrError.value = ''
  probe.value = null
  showAddr.value = true
}

// 保存并检测：校验失败留在 Sheet 内提示；成功保持打开以展示两路探测结果
async function saveAddr() {
  addrError.value = ''
  probing.value = true
  try {
    if (!(await saveAndProbe())) {
      addrError.value = error.value || '地址无效'
      error.value = ''
    }
  } finally {
    probing.value = false
  }
}

// 探测状态点：绿=通、红=不通、灰=未配置或未测
function probeDot(state: boolean | null | undefined): string {
  if (state === true) return 'ok'
  if (state === false) return 'bad'
  return 'idle'
}

const probeHint = computed(() => {
  if (!probe.value) return ''
  const parts: string[] = []
  if (probe.value.lanConfigured) parts.push(probe.value.lan ? '内网可用' : '内网不可达')
  if (probe.value.wanConfigured) parts.push(probe.value.wan ? '公网可用' : '公网不可达')
  return parts.join(' · ')
})

async function submit() {
  if (!(await saveAndProbe())) return
  loading.value = true
  try {
    await auth.login(username.value, password.value)
    // 与冷启动同一套落地规则：AI 模式→对话页，传统模式→上次一级 tab（此前硬编码 /home 与设置页显示打架）
    router.replace(landingRoute())
  } catch (e) {
    error.value = errText(e, '登录失败')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="login-page page-col">
    <div class="content">
      <div class="form-wrap">
        <div class="brand">
          <span class="brand-tile" aria-hidden="true"><Heart :size="20" :stroke-width="1.8" /></span>
          <p class="brand-name">情侣生活记录</p>
          <p class="brand-line">只属于我们的私密生活空间</p>
        </div>

        <form class="form" @submit.prevent="submit">
          <div class="group">
            <label class="row" for="user">
              <span class="label">用户名</span>
              <input
                id="user"
                v-model="username"
                autocomplete="username"
                autocapitalize="off"
                spellcheck="false"
                :autofocus="addrConfigured"
              />
            </label>
            <label class="row" for="pass">
              <span class="label">密码</span>
              <span class="pass-row">
                <input
                  id="pass"
                  v-model="password"
                  :type="showPass ? 'text' : 'password'"
                  autocomplete="current-password"
                  enterkeyhint="go"
                />
                <button
                  type="button"
                  class="eye"
                  :aria-label="showPass ? '隐藏密码' : '显示密码'"
                  :aria-pressed="showPass"
                  @click="showPass = !showPass"
                >
                  <EyeOff v-if="showPass" :size="18" :stroke-width="1.8" />
                  <Eye v-else :size="18" :stroke-width="1.8" />
                </button>
              </span>
            </label>
            <button type="button" class="row addr" @click="openAddr">
              <span class="label">服务地址</span>
              <span class="value">{{ addrLabel }}</span>
              <span v-if="addrState !== null" class="dot" :class="probeDot(addrState)" aria-hidden="true" />
              <PenLine :size="14" :stroke-width="1.8" class="edit" aria-hidden="true" />
            </button>
          </div>

          <p v-if="error" class="error" role="alert">{{ error }}</p>

          <button class="primary" :class="{ busy: loading }" type="submit" :disabled="loading" :aria-busy="loading">
            <span class="glow" aria-hidden="true" />
            <span>{{ loading ? '正在进入…' : '进入空间' }}</span>
          </button>
        </form>
      </div>
    </div>

    <Sheet
      v-model:show="showAddr"
      title="服务地址"
      confirm-text="保存并检测"
      cancel-text="关闭"
      :confirm-disabled="probing"
      @confirm="saveAddr"
    >
      <p class="s-hint">在家填内网地址、在外填公网地址；两个都填时自动优选可用的一个。</p>

      <label class="s-label" for="addr-lan">内网（在家直连）</label>
      <div class="addr-row">
        <button type="button" class="schema" :aria-label="`协议 ${lan.schema}，点击切换`" @click="toggleSchema(lan)">
          {{ lan.schema }}
        </button>
        <span class="sep">://</span>
        <input
          id="addr-lan"
          v-model="lan.addr"
          class="addr-input"
          placeholder="192.168.1.100:8080"
          inputmode="url"
          autocomplete="off"
          autocapitalize="off"
          spellcheck="false"
        />
        <span class="dot" :class="probeDot(probe?.lanConfigured ? probe.lan : null)" aria-hidden="true" />
      </div>

      <label class="s-label" for="addr-wan">公网（在外访问）</label>
      <div class="addr-row">
        <button type="button" class="schema" :aria-label="`协议 ${wan.schema}，点击切换`" @click="toggleSchema(wan)">
          {{ wan.schema }}
        </button>
        <span class="sep">://</span>
        <input
          id="addr-wan"
          v-model="wan.addr"
          class="addr-input"
          placeholder="example.com"
          inputmode="url"
          autocomplete="off"
          autocapitalize="off"
          spellcheck="false"
        />
        <span class="dot" :class="probeDot(probe?.wanConfigured ? probe.wan : null)" aria-hidden="true" />
      </div>

      <p v-if="probeHint" class="probe-hint">{{ probeHint }}</p>
      <p v-if="addrError" class="s-err" role="alert">{{ addrError }}</p>
    </Sheet>
  </div>
</template>

<style scoped>
/* 独立整页：与通知/设置/AI 对话同壳（.page-col 桌面档切浮窗）。登录页无导航栏，全屏 brand + 表单 */
.login-page {
  height: 100%;
  display: flex;
  flex-direction: column;
}

.content {
  flex: 1;
  overflow-y: auto;
  -webkit-overflow-scrolling: touch;
  display: flex;
  flex-direction: column;
  /* 横向 padding 走 --side-pad 令牌，与 PageLayout 的 .page-body 同源（手机 16 / 平板 24 / 桌面 0）。
     本页是 PageLayout 的唯一例外（无 navbar + 内容需可滚动地垂直居中），
     但横向内距不该自行硬编码——否则令牌调整时本页会掉队。
     桌面档令牌为 0：.form-wrap 自带 max-width 420 + margin auto，仍居中不贴边。 */
  padding: calc(16px + env(safe-area-inset-top)) var(--side-pad) calc(28px + env(safe-area-inset-bottom));
}

/* margin:auto 而非 justify-content:center：内容高于视口时（小屏 + 软键盘）不裁顶 */
.form-wrap {
  width: 100%;
  max-width: 420px;
  margin: auto;
  display: flex;
  flex-direction: column;
  gap: 22px;
}

.brand {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  text-align: center;
}
.brand-name {
  margin: 4px 0 0;
  font-size: 19px;
  font-weight: 700;
  color: var(--ink);
}
.brand-tile {
  width: 44px;
  height: 44px;
  border-radius: 14px;
  display: grid;
  place-items: center;
  color: var(--primary-deep);
  background: color-mix(in srgb, var(--primary) 16%, transparent);
}
.brand-line {
  margin: 0;
  font-size: 13px;
  color: var(--muted-ink);
}

.form {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

/* 字段行：沿用全局 .group .row 的 iOS 分组行（13px 16px + 0.5px 分割线） */
/* 行高下限 48dp（Android 触控规范）；border-box 才能让 48 含 13px 上下内边距 */
.row {
  position: relative;
  box-sizing: border-box;
  min-height: 48px;
}
.row .label {
  flex-shrink: 0;
  font-size: 15px;
  color: var(--ink);
}
.row input {
  flex: 1;
  min-width: 0;
  border: none;
  background: none;
  padding: 0;
  font-size: 16px;
  color: var(--ink);
}
.row input:focus {
  outline: none;
}
/* 焦点态 = 整行等长的强调色下划线（不随输入框宽度变化）+ 标签变色。
   之前画在输入框自身：黑色 2px 过重，且密码行被眼睛按钮挤短 37px */
.row::after {
  content: '';
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  height: 2px;
  background: var(--primary-deep);
  opacity: 0;
  transition: opacity 0.18s ease;
}
.row:focus-within::after {
  opacity: 1;
}
.row:focus-within .label {
  color: var(--primary-deep);
}
@media (prefers-reduced-motion: reduce) {
  .row::after {
    transition: none;
  }
}

.pass-row {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 8px;
}
.eye {
  flex-shrink: 0;
  /* 触控目标 ≥48dp（Android 规范）；负外边距让命中区不外扩成高行 */
  width: 48px;
  height: 48px;
  margin: -12px -10px -12px 0;
  display: grid;
  place-items: center;
  border: none;
  border-radius: 10px;
  background: none;
  color: var(--muted-ink);
  cursor: pointer;
}
.eye:active {
  background: var(--sk);
}

/* 服务地址行：低频配置入口（主界面只显示当前生效地址）。
   按钮需显式清掉浏览器默认样式（buttonface 底 + 1px 边 + 13.33px 居中字） */
.addr {
  width: 100%;
  min-height: 48px;
  margin: 0;
  border: none;
  background: none;
  font: inherit;
  font-size: 15px;
  color: var(--ink);
  text-align: left;
  cursor: pointer;
}
.addr .value {
  margin-left: auto;
  max-width: 56%;
  font-size: 15px;
  color: var(--muted-ink);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.addr .edit {
  flex-shrink: 0;
  color: var(--muted-ink);
}

.dot {
  flex-shrink: 0;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--sk);
}
.dot.ok {
  background: var(--sem-green);
}
.dot.bad {
  background: var(--danger);
}

.error {
  margin: 0;
  padding: 0 4px;
  font-size: 13px;
  color: var(--danger);
}

.primary {
  position: relative;
  overflow: hidden;
  display: flex;
  align-items: center;
  justify-content: center;
  border: none;
  border-radius: 12px;
  padding: 14px;
  font-size: 16px;
  font-weight: 600;
  background: var(--primary);
  color: var(--on-primary);
  cursor: pointer;
  transition: transform 0.18s ease, opacity 0.18s ease;
}
.primary:disabled {
  opacity: 0.58;
  cursor: default;
}
.primary:active:not(:disabled) {
  transform: scale(0.985);
}
.primary:focus-visible {
  outline: 2px solid var(--ink);
  outline-offset: 2px;
}
/* 加载态：缓慢呼吸的光晕（REQUIREMENTS §6.5，非旋转菊花） */
.glow {
  position: absolute;
  inset: 0;
  opacity: 0;
  pointer-events: none;
  background: radial-gradient(120% 140% at 50% 0%, color-mix(in srgb, var(--on-primary) 55%, transparent), transparent 70%);
}
.primary.busy .glow {
  opacity: 1;
  animation: breathe 1.8s ease-in-out infinite;
}
@media (prefers-reduced-motion: reduce) {
  .primary {
    transition: none;
  }
  .primary.busy .glow {
    animation: none;
    opacity: 0.5;
  }
}

/* Sheet 内：地址编辑（沿用 .s-input 那套「描边 + 底色」字段语言） */
.s-hint {
  margin: 0 0 10px;
  font-size: 13px;
  color: var(--muted-ink);
}
.s-label {
  display: block;
  margin: 6px 0;
  font-size: 13px;
  color: var(--muted-ink);
}
.addr-row {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 9px 12px;
  border: 1px solid var(--rule);
  border-radius: 10px;
  background: var(--bg);
  margin-bottom: 10px;
}
.addr-row:focus-within {
  border-color: var(--primary);
}
.schema {
  flex-shrink: 0;
  border: none;
  border-radius: 6px;
  padding: 4px 8px;
  font-size: 13px;
  font-weight: 600;
  background: var(--sk);
  color: var(--ink); /* 该按钮是功能信息，--primary-deep 在白/浅灰底仅 3.06:1（< AA） */
  cursor: pointer;
}
.schema:active {
  opacity: 0.7;
}
.sep {
  flex-shrink: 0;
  font-size: 15px;
  color: var(--muted-ink);
  user-select: none;
}
.addr-input {
  flex: 1;
  min-width: 0;
  border: none;
  background: none;
  padding: 0;
  font-size: 16px;
  color: var(--ink);
}
.addr-input:focus {
  outline: none;
}
.probe-hint {
  margin: 8px 0 0;
  font-size: 12px;
  color: var(--muted-ink);
}
.s-err {
  margin: 8px 0 0;
  font-size: 13px;
  color: var(--danger);
}
</style>
