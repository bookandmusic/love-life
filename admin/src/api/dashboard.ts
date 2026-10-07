import { http } from '../lib/http'

/**
 * 仪表盘数据。数据源由 VITE_API_BASE_URL 指向的服务决定。
 * ARCHITECTURE 第 9 章：内容统计 / AI 调用量与 Token 消耗 / 异常告警。
 */

export interface DashboardStat {
  label: string
  value: string
  change: string
  trend: 'up' | 'neutral'
  icon: string
}

export interface ActivityItem {
  icon: string
  text: string
  time: string
}

export interface AiUsagePoint {
  date: string
  calls: number
  tokens: number
  avgLatencyMs: number | null
}

export interface AlertItem {
  id: number
  level: 'error' | 'warning'
  message: string
  time: string
}

export function getDashboardStats(): Promise<DashboardStat[]> {
  return http<DashboardStat[]>('/api/v1/admin/dashboard/stats')
}

export function getRecentActivity(): Promise<ActivityItem[]> {
  return http<ActivityItem[]>('/api/v1/admin/dashboard/activity')
}

export function getAiUsage(): Promise<{ items: AiUsagePoint[] }> {
  return http<{ items: AiUsagePoint[] }>('/api/v1/admin/dashboard/ai-usage')
}

export function getAlerts(): Promise<{ items: AlertItem[] }> {
  return http<{ items: AlertItem[] }>('/api/v1/admin/dashboard/alerts')
}
