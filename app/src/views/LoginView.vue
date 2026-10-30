<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Eye, EyeOff, Heart } from 'lucide-vue-next'
import { useAuth } from '../stores/auth'
import { errText } from '../api/client'

const router = useRouter()
const auth = useAuth()

const username = ref('')
const password = ref('')
const showPass = ref(false)
const error = ref('')
const loading = ref(false)
// 记住登录：勾选则把凭据写入系统安全存储，供 token 失效时静默重登
const remember = ref(true)

// 预填：读过安全存储的凭据填入表单（不自动提交——是否登录由用户点按钮决定）
onMounted(async () => {
  const saved = await auth.loadSavedCredentials()
  if (saved) {
    username.value = saved.username
    password.value = saved.password
  }
})

async function submit() {
  error.value = ''
  loading.value = true
  try {
    await auth.login(username.value, password.value)
    if (remember.value) {
      await auth.rememberCredentials(username.value, password.value)
    } else {
      // 取消勾选：清掉可能残留的旧凭据
      await auth.forgetCredentials()
    }
    // 统一进加载页：由 BootView 取通道并决定落地页（与冷启动同一套逻辑）
    router.replace('/boot')
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
                autofocus
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
            <label class="row remember" for="remember">
              <span class="label">记住登录</span>
              <input id="remember" v-model="remember" type="checkbox" class="checkbox" />
            </label>
          </div>

          <p v-if="error" class="error" role="alert">{{ error }}</p>

          <button class="primary" :class="{ busy: loading }" type="submit" :disabled="loading" :aria-busy="loading">
            <span class="glow" aria-hidden="true" />
            <span>{{ loading ? '正在进入…' : '进入空间' }}</span>
          </button>
        </form>
      </div>
    </div>
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
/* 加载态：缓慢呼吸的光晕（非旋转菊花） */
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

/* 记住登录：勾选框右对齐，与其它行的 value 位对齐 */
.remember {
  cursor: pointer;
}
.checkbox {
  width: 20px;
  height: 20px;
  accent-color: var(--primary);
  cursor: pointer;
}
</style>
