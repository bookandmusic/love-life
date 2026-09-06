import { http } from '../lib/http'

export type MapSourceType = 'public' | 'tianditu'

// 瓦片源（多卡片；密钥只写不读：回读仅含 has_key 标记——REQUIREMENTS §8.3）
export interface MapSource {
  id: number
  type: MapSourceType
  url_template: string
  tianditu_layer: string
  has_key: boolean
  enabled: boolean
}

export interface MapSourcePayload {
  type: MapSourceType
  url_template: string
  tianditu_layer: string
  tianditu_key?: string
}

export function listMapSources(): Promise<{ items: MapSource[] }> {
  return http<{ items: MapSource[] }>('/api/v1/admin/map-config')
}

export function createMapSource(payload: MapSourcePayload): Promise<MapSource> {
  return http<MapSource>('/api/v1/admin/map-config', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function deleteMapSource(id: number): Promise<void> {
  return http<void>(`/api/v1/admin/map-config/${id}`, { method: 'DELETE' })
}

export function enableMapSource(id: number): Promise<MapSource> {
  return http<MapSource>(`/api/v1/admin/map-config/${id}/enable`, { method: 'PUT' })
}
