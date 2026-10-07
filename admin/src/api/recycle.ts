import { http } from '../lib/http'

/**
 * 回收站。数据源由 VITE_API_BASE_URL 指向的服务决定。
 * 恢复（单条/批量）均需二次确认并写回收站操作日志（恢复 + 系统超期永久删除，永久保留）。
 */

export type RecycleType = '动态' | '评论' | '相册' | '纪念日' | '地图点位'

export interface RecycleItem {
  id: number
  type: RecycleType
  summary: string
  deletedAt: string
  deletedBy: string
}

export interface RestoreLogEntry {
  time: string
  operator: string
  summary: string
}

export function listRecycle(): Promise<RecycleItem[]> {
  return http<RecycleItem[]>('/api/v1/admin/recycle')
}

export function restoreRecycle(id: number): Promise<void> {
  return http<void>(`/api/v1/admin/recycle/${id}/restore`, { method: 'POST' })
}

export function restoreMany(ids: number[]): Promise<{ restored: number }> {
  return http<{ restored: number }>('/api/v1/admin/recycle/restore', {
    method: 'POST',
    body: JSON.stringify({ ids }),
  })
}

export function listRestoreLog(): Promise<{ items: RestoreLogEntry[] }> {
  return http<{ items: RestoreLogEntry[] }>('/api/v1/admin/recycle/restore-log')
}
