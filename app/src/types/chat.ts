// 聊天域共享类型：ChatView 与其子组件（MessageList / CardActions / InputBar）共用，
// 避免各组件各定义一份导致结构不兼容。
export interface ChatMedia {
  name: string
  type: string
  size: number
  data_url: string
}

export interface SearchItem {
  id: number
  content: string
  thumb_url: string
}

export interface SearchResult {
  tool: string
  query?: string
  items: SearchItem[]
}

export interface Bubble {
  role: 'user' | 'assistant' | 'tool'
  content: string
  cardID?: number
  media?: ChatMedia[]
  result?: SearchResult
}

export interface CardItem {
  action_id: number
  tool: string
  params: Record<string, unknown>
  status: 'pending' | 'executed' | 'cancelled' | 'expired' | 'failed'
  result?: string
  expires_in?: number
  received_at?: number
}

export interface PickedMedia {
  name: string
  type: string
  size: number
  data_url: string
  file: File
}

export interface PostDetailMedia {
  id?: number | string
  kind?: string
  url?: string
  thumb_url?: string
}

export interface PostDetailItem {
  id: number
  content: string
  media: PostDetailMedia[]
}
