import { http } from '../lib/http'

/**
 * 数据安全设置。数据源由 VITE_API_BASE_URL 指向的服务决定。
 */

export interface SecuritySettings {
  recycleRetentionDays: number
}

export function getSecuritySettings(): Promise<SecuritySettings> {
  return http<SecuritySettings>('/api/v1/admin/security')
}

/** 部分更新：只提交变更字段（当前仅回收站保留期限一项） */
export function saveSecuritySettings(settings: Partial<SecuritySettings>): Promise<void> {
  return http<void>('/api/v1/admin/security', {
    method: 'PUT',
    body: JSON.stringify(settings),
  })
}
