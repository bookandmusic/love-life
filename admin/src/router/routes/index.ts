import type { ElegantConstRoute, RouteKey } from '@elegant-router/types';
import { layouts, views } from '../elegant/imports';
import { generatedRoutes } from '../elegant/routes';
import { transformElegantRoutesToVueRoutes } from '../elegant/transform';

/**
 * 分组导航：elegant-router 按目录把 space / ai / integrations / logs 生成嵌套路由
 * （父级 layout.base + 子级 view.*）。父级 = 侧栏可展开分组（图标 / 顺序在 GROUPS，
 * redirect 到组内第一个子页），子路由保持可见、作为侧栏二级菜单项直达子页。
 */
const GROUPS: Record<string, { icon: string; order: number; entry: RouteKey }> = {
  space: { icon: 'mdi:heart-outline', order: 2, entry: 'space_info' },
  ai: { icon: 'mdi:brain', order: 3, entry: 'ai_providers' },
  integrations: { icon: 'mdi:link-variant', order: 4, entry: 'integrations_webhooks' },
  logs: { icon: 'mdi:text-box-outline', order: 6, entry: 'logs_audit' }
};

/** 顶级单页（仪表盘 / 回收站）的菜单图标与顺序；分组的在 GROUPS */
const MENU_ICONS: Partial<Record<RouteKey, string>> = {
  dashboard: 'mdi:view-dashboard-outline',
  recycle: 'mdi:trash-can-outline'
};

/** 分组内子页的顺序（与各组页签展示顺序一致） */
const CHILD_ORDER: Partial<Record<RouteKey, number>> = {
  space_info: 1,
  space_members: 2,
  ai_providers: 1,
  'ai_chat-chain': 2,
  'ai_embedding-chain': 3,
  ai_prompts: 4,
  ai_tools: 5,
  ai_tokens: 6,
  integrations_webhooks: 1,
  integrations_map: 2,
  integrations_remote: 3,
  integrations_proxy: 4,
  integrations_appupdate: 5,
  logs_audit: 1,
  logs_operations: 2,
  logs_service: 3
};

const MENU_ORDER: Partial<Record<RouteKey, number>> = {
  dashboard: 1,
  space: 2,
  ai: 3,
  integrations: 4,
  recycle: 5,
  logs: 6
};

function finalizeGroupMenus(routes: ElegantConstRoute[]): ElegantConstRoute[] {
  for (const route of routes) {
    const group = GROUPS[route.name as string];
    if (group) {
      route.meta = { ...(route.meta ?? {}) } as NonNullable<ElegantConstRoute['meta']>;
      route.meta.icon = group.icon;
      route.meta.order = group.order;
      // 分组入口落到组内第一个页面（transform 默认取第一个子路由，顺序由目录而定，这里显式指定）
      route.redirect = '/' + group.entry.replace(/_/g, '/');
      for (const child of route.children ?? []) {
        // 子路由直接作为侧栏二级菜单项（模板菜单生成器对含可见 children 的父级渲染为可展开分组）
        const meta = { ...(child.meta ?? {}) } as NonNullable<ElegantConstRoute['meta']>;
        // 模型链共用一个视图，链别由路由 meta.kind 区分（chat / embedding）
        if (route.name === 'ai') meta.kind = child.name === 'ai_chat-chain' ? 'chat' : 'embedding';
        const childOrder = CHILD_ORDER[child.name as RouteKey];
        if (childOrder) meta.order = childOrder;
        child.meta = meta;
      }
      continue;
    }
    const icon = MENU_ICONS[route.name as RouteKey];
    const order = MENU_ORDER[route.name as RouteKey];
    if ((icon || order) && route.meta) {
      route.meta.icon = icon;
      route.meta.order = order;
    }
  }
  return routes;
}

/** create routes when the auth route mode is static */
export function createStaticRoutes() {
  const constantRoutes: ElegantConstRoute[] = [];

  const authRoutes: ElegantConstRoute[] = [];

  finalizeGroupMenus(generatedRoutes).forEach(item => {
    if (item.meta?.constant) {
      constantRoutes.push(item);
    } else {
      authRoutes.push(item);
    }
  });

  return {
    constantRoutes,
    authRoutes
  };
}

/**
 * Get auth vue routes
 *
 * @param routes Elegant routes
 */
export function getAuthVueRoutes(routes: ElegantConstRoute[]) {
  return transformElegantRoutesToVueRoutes(routes, layouts, views);
}
