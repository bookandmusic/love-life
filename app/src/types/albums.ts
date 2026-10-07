// 相册域共享类型：AlbumsView 与 TimelineGrid / AlbumCollection / AlbumSheets 共用，
// 避免各组件各定义一份导致结构不兼容（与 types/chat.ts 同一做法）。
export interface AlbumItem {
  id: number
  name: string
  item_count: number
  cover_url?: string
  // 创建者：用于判断删除时是否提示「对方会收到站内通知」（权限不区分，双方都能删）
  created_by?: number
}

// 月度分组（PRODUCT「相册（独立底部 tab）」：展示形态非实体，无隐藏开关、不可删）
export interface MonthRow {
  month_key: string
  item_count: number
  cover_url?: string
}

export interface TimelineItem {
  id: number
  post_id: number
  kind: string
  thumb_url: string
  url?: string
  taken_at: string
  /** 时长（秒）：视频角标与音频查看器用；解析不出时服务端不下发 */
  duration?: number
}

export interface MonthGroup {
  key: string
  label: string
  items: TimelineItem[]
}
