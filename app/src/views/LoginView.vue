<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuth } from '../stores/auth'
import { getLanBase, getWanBase, setDualBase } from '../api/client'
import { probeAndSelect, type ProbeStatus } from '../utils/netProbe'

const router = useRouter()
const auth = useAuth()

// 服务端地址（AP1 双通道，DESIGN §11.2）：内网（在家直连）/ 公网（在外 HTTPS），
// 各自可空——只填一个时直接用该地址；双地址探测优选内网。
type Schema = 'http' | 'https'
const lan = reactive({ schema: 'http' as Schema, addr: '' })
const wan = reactive({ schema: 'https' as Schema, addr: '' })
const username = ref('')
const password = ref('')
const error = ref('')
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

function fullUrl(row: { schema: Schema; addr: string }): string {
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
  setDualBase(fullUrl(lan), fullUrl(wan))
  probe.value = await probeAndSelect()
  return true
}

// 检测按钮：保存 + 探测 + 展示连通状态
async function probeNow() {
  probing.value = true
  try {
    await saveAndProbe()
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
    router.replace('/home')
  } catch (e) {
    error.value = e instanceof Error ? e.message : '登录失败'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <main class="login">
    <div class="hero">
      <h1>情侣生活记录</h1>
      <p>只属于我们的私密生活空间</p>
    </div>

    <form class="form" @submit.prevent="submit">
      <div class="group">
        <div class="field">
          <label for="addr-lan">内网地址（在家直连）</label>
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
        </div>
        <div class="field">
          <label for="addr-wan">公网地址（在外访问）</label>
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
        </div>
        <div class="probe-row">
          <button type="button" class="probe-btn" :disabled="probing" @click="probeNow">
            {{ probing ? '检测中…' : '检测连通性' }}
          </button>
          <span v-if="probe" class="probe-hint">
            {{ probeHint }}
          </span>
        </div>
        <div class="field">
          <label for="user">用户名</label>
          <input id="user" v-model="username" autocomplete="username" />
        </div>
        <div class="field">
          <label for="pass">密码</label>
          <input id="pass" v-model="password" type="password" autocomplete="current-password" />
        </div>
      </div>

      <p v-if="error" class="error">{{ error }}</p>
      <button class="primary" type="submit" :disabled="loading">
        {{ loading ? '正在进入…' : '进入空间' }}
      </button>
    </form>
  </main>
</template>

<style scoped>
.login {
  height: 100%;
  display: flex;
  flex-direction: column;
  justify-content: center;
  padding: 24px;
  box-sizing: border-box;
  max-width: var(--sheet-max);
  margin: 0 auto;
  background: var(--app-veil);
}

/* hero：克制标题 + 副标语，无图标 */
.hero {
  margin-bottom: 28px;
}
.hero h1 {
  font-size: 26px;
  font-weight: 700;
  margin: 0;
  letter-spacing: -0.02em;
}
.hero p {
  margin: 8px 0 0;
  font-size: 14px;
  color: var(--muted-ink);
}

/* 表单：iOS 分组卡 + 0.5px 分割线 */
.form {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.group {
  padding: 4px 16px;
}
.field {
  padding: 12px 0;
}
.field + .field {
  border-top: 0.5px solid var(--rule);
}
.field label {
  display: block;
  font-size: 12px;
  color: var(--muted-ink);
  margin-bottom: 6px;
}
.field input {
  width: 100%;
  border: none;
  background: none;
  padding: 0;
  font-size: 16px;
  color: var(--ink);
}
.field input:focus {
  outline: none;
}

/* 服务端地址：schema 切换 + 单行地址输入 */
.addr-row {
  display: flex;
  align-items: center;
  gap: 6px;
}
.schema {
  flex-shrink: 0;
  border: none;
  border-radius: 6px;
  padding: 4px 8px;
  font-size: 13px;
  font-weight: 600;
  background: rgba(120, 120, 128, 0.14);
  color: var(--primary-deep);
  cursor: pointer;
}
.schema:active {
  opacity: 0.7;
}
.sep {
  flex-shrink: 0;
  color: var(--muted-ink);
  font-size: 15px;
  user-select: none;
}
.addr-input {
  flex: 1;
  min-width: 0;
  font-variant-numeric: tabular-nums;
}

/* 探测状态点与检测行 */
.dot {
  flex-shrink: 0;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: rgba(120, 120, 128, 0.3);
}
.dot.ok {
  background: #34c759;
}
.dot.bad {
  background: var(--danger);
}
.probe-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 0 4px;
}
.probe-btn {
  border: none;
  border-radius: 8px;
  padding: 6px 12px;
  font-size: 13px;
  font-weight: 600;
  background: rgba(120, 120, 128, 0.14);
  color: var(--primary-deep);
  cursor: pointer;
}
.probe-btn:disabled {
  opacity: 0.5;
}
.probe-hint {
  font-size: 12px;
  color: var(--muted-ink);
}

.error {
  color: var(--danger);
  font-size: 13px;
  margin: 0;
  padding: 0 4px;
}
.primary {
  background: var(--primary);
  color: #fff;
  border: none;
  border-radius: 12px;
  padding: 14px;
  font-size: 16px;
  font-weight: 600;
  cursor: pointer;
}
.primary:disabled {
  opacity: 0.5;
}
.primary:active {
  transform: scale(0.98);
}
</style>
