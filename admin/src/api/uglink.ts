import { http } from '../lib/http'

// 绿联通道配置（密码只写不读：回读只有 has_password 标记）
export interface UglinkConfig {
  configured: boolean
  uglink_id?: string
  port?: number
  username?: string
  enabled?: boolean
  has_password?: boolean
  logged_in?: boolean
  expires_at?: string
  failure_count?: number
  backoff_until?: string
}

export interface UglinkConfigPayload {
  uglink_id: string
  port: number
  username: string
  password?: string // 留空表示沿用旧密码
  enabled: boolean
}

export interface UglinkTestResult {
  ok: boolean
  reason?: string
  base_url?: string
  expires_at?: string
}

export function getUglinkConfig(): Promise<UglinkConfig> {
  return http<UglinkConfig>('/api/v1/admin/uglink/config')
}

export function saveUglinkConfig(payload: UglinkConfigPayload): Promise<{ ok: boolean }> {
  return http<{ ok: boolean }>('/api/v1/admin/uglink/config', {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

export function testUglink(): Promise<UglinkTestResult> {
  return http<UglinkTestResult>('/api/v1/admin/uglink/test', { method: 'POST' })
}
