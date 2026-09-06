<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuth } from '../stores/auth'

const router = useRouter()
const auth = useAuth()

// 服务端地址：单行完整地址（http/https + host + 可选端口），提供 schema 快捷切换。
const schema = ref<'http' | 'https'>('http')
const address = ref('')
const username = ref('')
const password = ref('')
const error = ref('')
const loading = ref(false)

// 初始值：已存 base 或 env，统一整理成「host[:port]」形态回填
function initAddress(value: string) {
  if (!value) return
  try {
    const u = new URL(value)
    if (u.protocol === 'http:' || u.protocol === 'https:') schema.value = u.protocol === 'https:' ? 'https' : 'http'
    address.value = u.port ? `${u.hostname}:${u.port}` : u.hostname
  } catch {
    address.value = value
  }
}
initAddress(localStorage.getItem('love-life-base') ?? (import.meta.env.VITE_SERVER_URL as string) ?? '')

function toggleSchema() {
  schema.value = schema.value === 'http' ? 'https' : 'http'
}

// 地址校验：host 必填；端口若存在需为 1-65535
function validateAddress(raw: string): string | null {
  const addr = raw.trim()
  if (!addr) return '请输入服务地址'
  // 去掉可能的协议前缀再判 host/port
  let rest = addr.replace(/^https?:\/\//i, '')
  let host = rest
  let port = ''
  const idx = rest.lastIndexOf(':')
  if (idx > 0 && !rest.slice(idx + 1).includes(']') && rest.lastIndexOf(']') < idx) {
    // 冒号可能是端口分隔（IPv6 形如 [::1]:8080 除外）
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

const serverUrl = computed(() => {
  const addr = address.value.trim()
  if (/^https?:\/\//i.test(addr)) return addr
  return `${schema.value}://${addr}`
})

async function submit() {
  error.value = ''
  const invalid = validateAddress(address.value)
  if (invalid) {
    error.value = invalid
    return
  }
  loading.value = true
  try {
    await auth.login(serverUrl.value, username.value, password.value)
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
          <label for="addr">服务端地址</label>
          <div class="addr-row">
            <button type="button" class="schema" :aria-label="`协议 ${schema}，点击切换`" @click="toggleSchema">
              {{ schema }}
            </button>
            <span class="sep">://</span>
            <input
              id="addr"
              v-model="address"
              class="addr-input"
              placeholder="192.168.1.100:8787"
              inputmode="url"
              autocomplete="off"
              autocapitalize="off"
              spellcheck="false"
            />
          </div>
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
