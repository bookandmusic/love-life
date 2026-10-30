import { createRouter, createWebHistory } from 'vue-router'
import { getToken } from '../api/client'
import { useAuth } from '../stores/auth'
import { KEYS } from '../utils/storage'

// 一级 tab（只记这五个；详情页/发布页不记，避免冷启动落到需要参数或不适合停留的页面）
const TABS = ['/home', '/posts', '/albums', '/anniversaries', '/map']

/** 落地页（'/' redirect 与登录成功后共用，2026-09-21：此前登录页硬编码 /home，与 AI 模式存储打架）：
 AI 模式→对话页；传统模式→上次一级 tab，无记录则首页 */
export function landingRoute(): string {
  // 直读 localStorage 而不经 Pinia：record redirect 在导航最早期执行，避免依赖 store 安装顺序
  if (localStorage.getItem(KEYS.MODE) === 'ai') return '/chat'
  const last = localStorage.getItem(KEYS.LAST_ROUTE) || ''
  return TABS.includes(last) ? last : '/home'
}

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', component: () => import('../views/LoginView.vue') },
    { path: '/boot', component: () => import('../views/BootView.vue') },
    { path: '/notifications', component: () => import('../views/NotificationsView.vue') },
    { path: '/chat', component: () => import('../views/ChatView.vue') },
    { path: '/settings', component: () => import('../views/SettingsView.vue') },
    { path: '/settings/about', component: () => import('../views/AboutView.vue') },
    {
      path: '/',
      component: () => import('../views/MainView.vue'),
      children: [
        { path: '', redirect: landingRoute },
        { path: 'home', component: () => import('../views/HomeView.vue') },
        { path: 'posts', component: () => import('../views/PostsView.vue') },
        { path: 'albums', component: () => import('../views/AlbumsView.vue') },
        { path: 'publish', component: () => import('../views/PublishView.vue') },
        { path: 'map', component: () => import('../views/MapView.vue') },
        { path: 'anniversaries', component: () => import('../views/AnniversariesView.vue') },
        { path: 'albums/month/:key', name: 'album-month', component: () => import('../views/AlbumDetailView.vue') },
        { path: 'albums/:id', component: () => import('../views/AlbumDetailView.vue') },
      ],
    },
  ],
})

router.beforeEach(async (to) => {
  // 登录页与启动页自行决定去留：/boot 内部处理恢复与跳转，避免与守卫互相踢
  if (to.path === '/login' || to.path === '/boot') return true
  if (!getToken()) return '/login'
  const auth = useAuth()
  // 有 token 但用户信息未恢复：交给 BootView 统一处理（区分失效与网络失败）。
  // 带上原目标，BootView 完成后跳回这里，而不是一律回 landingRoute——
  // 否则刷新设置页会被送回首页。
  if (!auth.user) return { path: '/boot', query: { to: to.fullPath } }
  return true
})

// 记住上次停留的一级 tab（只记 TABS，详情页等不覆盖）
router.afterEach((to) => {
  if (TABS.includes(to.path)) localStorage.setItem(KEYS.LAST_ROUTE, to.path)
})

export default router
