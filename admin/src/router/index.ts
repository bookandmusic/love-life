import { createRouter, createWebHistory } from 'vue-router'
import { getToken, getRole } from '../lib/http'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/login',
      component: () => import('../views/LoginView.vue'),
      meta: { title: '登录' },
    },
    {
      path: '/',
      component: () => import('../views/LayoutView.vue'),
      children: [
        { path: '', redirect: '/dashboard' },
        { path: 'dashboard', component: () => import('../views/DashboardView.vue'), meta: { title: '仪表盘' } },
        { path: 'space', component: () => import('../views/SpaceManageView.vue'), meta: { title: '空间管理' } },
        {
          path: 'ai',
          component: () => import('../views/AiConfigView.vue'),
          redirect: '/ai/providers',
          meta: { title: 'AI 配置' },
          children: [
            { path: 'providers', component: () => import('../views/ProviderListView.vue'), meta: { title: 'AI 配置 · 供应商' } },
            { path: 'chat-chain', component: () => import('../views/ModelChainView.vue'), meta: { title: 'AI 配置 · 对话模型链', kind: 'chat' } },
            { path: 'embedding-chain', component: () => import('../views/ModelChainView.vue'), meta: { title: 'AI 配置 · 嵌入模型链', kind: 'embedding' } },
            { path: 'prompts', component: () => import('../views/PromptStyleView.vue'), meta: { title: 'AI 配置 · 提示词与风格' } },
            { path: 'tools', component: () => import('../views/McpToolsView.vue'), meta: { title: 'AI 配置 · MCP 工具' } },
            { path: 'tokens', component: () => import('../views/TokensView.vue'), meta: { title: 'AI 配置 · 访问令牌' } },
          ],
        },
        {
          path: 'integrations',
          component: () => import('../views/IntegrationsView.vue'),
          redirect: '/integrations/webhooks',
          meta: { title: '外部集成' },
          children: [
            { path: 'webhooks', component: () => import('../views/WebhooksView.vue'), meta: { title: '外部集成 · 通知渠道' } },
            { path: 'map', component: () => import('../views/MapConfigView.vue'), meta: { title: '外部集成 · 地图配置' } },
            { path: 'uglink', component: () => import('../views/UglinkConfigView.vue'), meta: { title: '外部集成 · 远程访问' } },
            { path: 'backup', component: () => import('../views/BackupAddrView.vue'), meta: { title: '外部集成 · 备用地址' } },
            { path: 'appupdate', component: () => import('../views/AppUpdateConfigView.vue'), meta: { title: '外部集成 · 应用更新' } },
          ],
        },
        { path: 'recycle', component: () => import('../views/RecycleBinView.vue'), meta: { title: '回收站' } },
        { path: 'audit', component: () => import('../views/AuditLogView.vue'), meta: { title: '审计日志' } },
        { path: 'logs', component: () => import('../views/ServiceLogView.vue'), meta: { title: '服务日志' } },
        // 旧路径兜底
        { path: 'settings', redirect: '/ai/providers' },
        { path: 'settings/models', redirect: '/ai/providers' },
        { path: 'settings/tools', redirect: '/ai/tools' },
        { path: 'settings/tokens', redirect: '/ai/tokens' },
        { path: 'settings/webhooks', redirect: '/integrations/webhooks' },
        { path: 'settings/map', redirect: '/integrations/map' },
        { path: 'settings/space', redirect: '/space' },
        { path: 'models', redirect: '/ai/chat-chain' },
        { path: 'tools', redirect: '/ai/tools' },
        { path: 'tokens', redirect: '/ai/tokens' },
        { path: 'webhooks', redirect: '/integrations/webhooks' },
        { path: 'map-config', redirect: '/integrations/map' },
        { path: 'users', redirect: '/space' },
        { path: 'members', redirect: '/space' },
        { path: 'security', redirect: '/space' },
        // 未知路径统一落到仪表盘：没有兜底时直开拼错/历史遗留路径是整页白屏
        // （ARCHITECTURE「后台管理页面实现」：旧路径全部 redirect 到新路径）
        { path: ':pathMatch(.*)*', redirect: '/dashboard' },
      ],
    },
  ],
})

router.beforeEach((to) => {
  const authed = !!getToken()
  const role = getRole()
  if (to.path !== '/login' && !authed) return '/login'
  if (to.path !== '/login' && role && role !== 'admin') return '/login'
  if (to.path === '/login' && authed && role === 'admin') return '/dashboard'
})

export default router
