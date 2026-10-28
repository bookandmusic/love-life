<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { authApi } from '../api'
import AppIcon from '../components/AppIcon.vue'

const route = useRoute()
const router = useRouter()

const nav = [
  { to: '/dashboard', label: '仪表盘', icon: 'layout-dashboard' },
  { to: '/space', label: '空间管理', icon: 'heart' },
  { to: '/ai', label: 'AI 配置', icon: 'brain' },
  { to: '/integrations', label: '外部集成', icon: 'send' },
  { to: '/recycle', label: '回收站', icon: 'trash-2' },
  { to: '/audit', label: '审计日志', icon: 'file-text' },
  { to: '/logs', label: '服务日志', icon: 'scroll-text' },
]

const pageTitle = computed(() => (route.meta.title as string) ?? '后台管理')

async function logout() {
  // 先吊销服务端 token 再跳转：logout() 内部保证本地凭证一定被清（失败也清）
  await authApi.logout()
  await router.push('/login')
}
</script>

<template>
  <div class="layout">
    <aside class="sidebar">
      <div class="sidebar-header">
        <RouterLink to="/dashboard" class="sidebar-logo">
          <AppIcon name="heart" :size="20" class="icon-accent" />
          <span>情侣生活记录</span>
        </RouterLink>
      </div>
      <nav class="sidebar-nav">
        <RouterLink
          v-for="item in nav"
          :key="item.to"
          :to="item.to"
          class="nav-item"
          active-class="active"
        >
          <AppIcon :name="item.icon" :size="18" />
          <span>{{ item.label }}</span>
        </RouterLink>
      </nav>
      <div class="sidebar-footer">
        <div class="user-info">
          <div class="user-avatar">A</div>
          <div class="user-details">
            <div class="user-name">Admin</div>
            <div class="user-role">管理员</div>
          </div>
          <button class="btn btn-icon btn-secondary" title="退出登录" @click="logout">
            <AppIcon name="log-out" :size="16" />
          </button>
        </div>
      </div>
    </aside>

    <main class="main">
      <header class="topbar">
        <h1 class="topbar-title">{{ pageTitle }}</h1>
      </header>
      <div class="content">
        <RouterView />
      </div>
    </main>
  </div>
</template>
