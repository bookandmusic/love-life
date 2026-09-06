import { http } from '../lib/http'

/**
 * 个人访问令牌（PAT）。数据源由 VITE_API_BASE_URL 指向的服务决定。
 * 明文 token 仅在生成接口的响应中出现一次，列表不回读。
 */

export type PatTier = 'readonly' | 'readwrite'
export type PatStatus = 'active' | 'revoked'

export interface PatToken {
  id: number
  name: string
  user: string
  tier: PatTier
  createdAt: string
  status: PatStatus
}

export interface CreatedPat extends PatToken {
  token: string
}

export function listTokens(): Promise<PatToken[]> {
  return http<PatToken[]>('/api/v1/admin/tokens')
}

export function createToken(payload: { name: string; user: string; tier: PatTier }): Promise<CreatedPat> {
  return http<CreatedPat>('/api/v1/admin/tokens', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function revokeToken(id: number): Promise<PatToken> {
  return http<PatToken>(`/api/v1/admin/tokens/${id}/revoke`, { method: 'POST' })
}
