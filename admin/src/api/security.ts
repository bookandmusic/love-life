import { http } from '../lib/http'

/**
 * 数据安全设置。数据源由 VITE_API_BASE_URL 指向的服务决定。
 */

export type ChatRetention = '7' | '30' | '90' | 'permanent'

export interface SecuritySettings {
  recycleRetentionDays: number
  chatRetention: ChatRetention
}

export function getSecuritySettings(): Promise<SecuritySettings> {
  return http<SecuritySettings>('/api/v1/admin/security')
}

export function saveSecuritySettings(settings: SecuritySettings): Promise<void> {
  return http<void>('/api/v1/admin/security', {
    method: 'PUT',
    body: JSON.stringify(settings),
  })
}
