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

export interface AuditPage {
  items: AuditEntry[]
  total: number
  page: number
  page_size: number
}

/**
 * 审计日志分页读取。audit_logs 永久保留、每次 AI/PAT 工具调用一条，
 * 不分页会随使用年限线性变慢（一年约 1.8 万条），故后端强制分页。
 */
export function listAudit(page = 1, pageSize = 20): Promise<AuditPage> {
  return http<AuditPage>(`/api/v1/admin/audit?page=${page}&page_size=${pageSize}`)
}
