import { http } from '../lib/http'

export interface SpaceInfo {
  name: string
  signature: string
  love_start_date: string
}

export function getSpace(): Promise<SpaceInfo> {
  return http<SpaceInfo>('/api/v1/space')
}

export function saveSpace(payload: Partial<SpaceInfo>): Promise<void> {
  return http<void>('/api/v1/space', {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}
