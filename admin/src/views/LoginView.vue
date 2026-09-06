<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { login } from '../api/auth'
import AppIcon from '../components/AppIcon.vue'

const router = useRouter()
const username = ref('')
const password = ref('')
const error = ref('')
const loading = ref(false)

async function submit() {
  loading.value = true
  error.value = ''
  try {
    await login(username.value, password.value)
    router.push('/dashboard')
  } catch (e) {
    error.value = e instanceof Error ? e.message : '登录失败'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <main class="login">
    <div class="login-card card">
      <div class="login-brand">
        <div class="login-logo">
          <AppIcon name="heart" :size="22" class="icon-accent" />
        </div>
        <h1>情侣生活记录</h1>
        <p class="text-tertiary text-sm">后台管理系统</p>
      </div>
      <form class="login-form" @submit.prevent="submit">
        <div class="form-group">
          <label class="form-label">用户名</label>
          <input
            v-model="username"
            class="form-input"
            type="text"
            placeholder="admin"
            autocomplete="username"
          />
        </div>
        <div class="form-group">
          <label class="form-label">密码</label>
          <input
            v-model="password"
            class="form-input"
            type="password"
            placeholder="••••••••"
            autocomplete="current-password"
          />
        </div>
        <p v-if="error" class="form-error">{{ error }}</p>
        <button class="btn btn-primary full-width" type="submit" :disabled="loading">
          {{ loading ? '登录中…' : '登录' }}
        </button>
      </form>
    </div>
  </main>
</template>

<style scoped>
.login {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--color-bg);
}
.login-card {
  width: 360px;
  max-width: calc(100vw - 32px);
  padding: var(--space-6);
}
.login-brand {
  text-align: center;
  margin-bottom: var(--space-6);
}
.login-logo {
  width: 48px;
  height: 48px;
  border-radius: 50%;
  background: var(--color-accent-light);
  display: flex;
  align-items: center;
  justify-content: center;
  margin: 0 auto var(--space-3);
}
.login-logo .icon-accent {
  color: var(--color-accent);
}
.login-brand h1 {
  font-size: 18px;
}
.login-form {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}
.login-form .btn {
  margin-top: var(--space-2);
}
</style>
