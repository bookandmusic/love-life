import { http } from '../lib/http'

/**
 * 审计与监控日志。数据源由 VITE_API_BASE_URL 指向的服务决定。
 */

export type AuditActorType = 'user' | 'ai' | 'pat'
/** 四态：成功 / 失败（可带原因，形如 `failed: 原因`）/ 待确认 / 已取消 */
export type AuditResult = 'success' | 'failed' | 'pending' | 'cancelled'

export interface AuditEntry {
  time: string
  actorType: AuditActorType
  actor: string
  action: string
  params: string
  result: AuditResult
  /** 模型响应耗时（毫秒）；user 直接写库为 null */
  latencyMs: number | null
}

export function listAudit(): Promise<AuditEntry[]> {
  return http<AuditEntry[]>('/api/v1/admin/audit')
}
