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

/** 服务端分页契约：items 为当前页，total 为全量条数，has_more = offset + items.length < total */
export interface RecyclePage {
  items: RecycleItem[]
  total: number
  has_more: boolean
}

export interface RestoreLogEntry {
  time: string
  operator: string
  summary: string
}

/** 服务端分页默认 50 条/页、单页上限 200 条 */
export const RECYCLE_PAGE_SIZE = 50
export const RECYCLE_MAX_PAGE_SIZE = 200

export function listRecycle(params: { limit?: number; offset?: number } = {}): Promise<RecyclePage> {
  const limit = Math.min(Math.max(params.limit ?? RECYCLE_PAGE_SIZE, 1), RECYCLE_MAX_PAGE_SIZE)
  const offset = Math.max(params.offset ?? 0, 0)
  return http<RecyclePage>(`/api/v1/admin/recycle?limit=${limit}&offset=${offset}`)
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
