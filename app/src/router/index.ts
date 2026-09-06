import { createRouter, createWebHistory } from 'vue-router'
import { getToken } from '../api/client'
import { useAuth } from '../stores/auth'
import { useMode } from '../stores/mode'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', component: () => import('../views/LoginView.vue') },
    { path: '/notifications', component: () => import('../views/NotificationsView.vue') },
    { path: '/chat', component: () => import('../views/ChatView.vue') },
    { path: '/settings', component: () => import('../views/SettingsView.vue') },
    {
      path: '/',
      component: () => import('../views/MainView.vue'),
      children: [
        { path: '', redirect: '/home' },
        { path: 'home', component: () => import('../views/HomeView.vue') },
        { path: 'posts', component: () => import('../views/PostsView.vue') },
        { path: 'albums', component: () => import('../views/AlbumsView.vue') },
        { path: 'publish', component: () => import('../views/PublishView.vue') },
        { path: 'map', component: () => import('../views/MapView.vue') },
        { path: 'anniversaries', component: () => import('../views/AnniversariesView.vue') },
        { path: 'posts/:id', component: () => import('../views/PostDetailView.vue') },
        { path: 'albums/:id', component: () => import('../views/AlbumDetailView.vue') },
      ],
    },
  ],
})

router.beforeEach(async (to) => {
  if (to.path === '/login') return true
  if (!getToken()) return '/login'
  const auth = useAuth()
  if (!auth.user) {
    await auth.restore()
    if (!auth.user) return '/login'
  }
  if (to.path === '/' && auth.user) {
    return useMode().mode === 'ai' ? '/chat' : '/home'
  }
  return true
})

export default router
