// 相册域共享类型：AlbumsView 与 TimelineGrid / AlbumCollection / AlbumSheets 共用，
// 避免各组件各定义一份导致结构不兼容（与 types/chat.ts 同一做法）。
export interface AlbumItem {
  id: number
  type: string
  name: string
  is_hidden: boolean
  item_count: number
  cover_url?: string
  // 创建者：用于判断删除时是否提示「对方会收到站内通知」（权限不区分，双方都能删）
  created_by?: number
}

export interface TimelineItem {
  id: number
  post_id: number
  kind: string
  thumb_url: string
  url?: string
  taken_at: string
}

export interface MonthGroup {
  key: string
  label: string
  items: TimelineItem[]
}
