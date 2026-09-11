// 共享领域类型与媒体地址 helper（客户端审查 P2-9：三视图 Post 切片统一 + mediaSrc 收拢）
import { fullUrl } from './api/client'
import { mediaUrl } from './utils/mediaUrls'

export interface MediaItem {
  id: number
  kind: string
  url: string
  thumb_url: string
  duration?: number
}

export interface Post {
  id: number
  author_id: number
  author: string
  content: string
  mood: string
  location_name: string
  created_at: string
  media: MediaItem[]
  like_count: number
  liked: boolean
  favorite_count: number
  favorited: boolean
  comment_count: number
}

// mediaSrc 取媒体可直接挂载的完整地址（签名重签逻辑仍在 mediaUrl 内部）
export function mediaSrc(m: { id?: number | string; url?: string; thumb_url?: string }, which: 'thumb' | 'raw'): string {
  const u = mediaUrl(m)
  return fullUrl(which === 'thumb' ? u.thumb : u.raw)
}
