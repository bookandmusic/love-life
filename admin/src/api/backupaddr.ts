import { http } from '../lib/http'

// 备用服务地址：服务端不代理，只存取与探活（反代 / 证书由部署者自理）
export interface BackupAddress {
  id: string
  name: string
  url: string
  enabled: boolean
  /** 选路权重（1–999，越大越优先，100 为内置地址保留） */
  weight: number
}

export interface BackupAddressPayload {
  name: string
  url: string
  enabled: boolean
  /** 省略时服务端按默认 50 存 */
  weight?: number
}

export function listBackupAddresses(): Promise<{ items: BackupAddress[] }> {
  return http<{ items: BackupAddress[] }>('/api/v1/admin/backup-addresses')
}

export function createBackupAddress(payload: BackupAddressPayload): Promise<{ ok: boolean }> {
  return http<{ ok: boolean }>('/api/v1/admin/backup-addresses', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function updateBackupAddress(
  id: string,
  payload: BackupAddressPayload,
): Promise<{ ok: boolean }> {
  return http<{ ok: boolean }>(`/api/v1/admin/backup-addresses/${id}`, {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

export function deleteBackupAddress(id: string): Promise<{ ok: boolean }> {
  return http<{ ok: boolean }>(`/api/v1/admin/backup-addresses/${id}`, { method: 'DELETE' })
}

export function testBackupAddress(url: string): Promise<{ ok: boolean; reason?: string }> {
  return http<{ ok: boolean; reason?: string }>('/api/v1/admin/backup-addresses/test', {
    method: 'POST',
    body: JSON.stringify({ url }),
  })
}
