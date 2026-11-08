import { http } from '../lib/http'

// 绿联通道配置（密码只写不读：回读只有 has_password 标记）
export interface UglinkConfig {
  configured: boolean
  uglink_id?: string
  port?: number
  username?: string
  enabled?: boolean
  /** 选路权重（1–999，100 为内置地址保留） */
  weight?: number
  has_password?: boolean
  logged_in?: boolean
  expires_at?: string
  failure_count?: number
  backoff_until?: string
  /** 确定性配置错误（权限/凭据）：非空时状态列显示「配置错误」 */
  config_error?: string
}

export interface UglinkConfigPayload {
  uglink_id: string
  port: number
  username: string
  password?: string // 留空表示沿用已保存的密码
  enabled: boolean
  /** 选路权重（1–999，100 为内置地址保留）；省略则保留原值 */
  weight?: number
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

/** 清除绿联通道配置（回到未配置态） */
export function deleteUglinkConfig(): Promise<{ ok: boolean }> {
  return http<{ ok: boolean }>('/api/v1/admin/uglink/config', { method: 'DELETE' })
}

export function testUglink(): Promise<UglinkTestResult> {
  return http<UglinkTestResult>('/api/v1/admin/uglink/test', { method: 'POST' })
}
