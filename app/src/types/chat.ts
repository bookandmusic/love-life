// 聊天域共享类型：ChatView 与其子组件共用一份定义。

/** 随消息提交给服务端的媒体元数据。依 AI模式设计「图片处理与存储」：
 *  消息体只带 {name, mime, size}，**不含文件内容、不含 base64**——文件在「确认执行」时
 *  才从本机上传（与发动态同一管线）。 */
export interface ChatMedia {
  name: string
  type: string
  size: number
}

/** 气泡内渲染的媒体：元数据 + 可选的本机预览地址。
 *  流式中来自本机选中项（有 previewUrl）；刷新回显来自 parts（无 previewUrl，只剩文件名与数量）。 */
export interface MediaView extends ChatMedia {
  previewUrl?: string
}

/** 本机待发布媒体：在元数据之上带本机文件与预览地址。
 *  previewUrl 是 blob URL（仅本机渲染用，永不进请求体）；刷新后历史回显无本机文件，
 *  此时 previewUrl 为空、只剩文件名与数量。 */
export interface PickedMedia extends ChatMedia {
  previewUrl: string
  file: File
}

// UI 块（tool.end.ui / 历史 tool_ui）：按 kind 渲染，可勾选引用。
export interface UIRecord {
  id: number
  kind: string
  text: string
  date?: string
  thumb_url?: string
  badge?: string
  group_key?: string
}

export interface UIStat {
  label: string
  value: string // 服务端拼好单位
  hint?: string
}

export interface UISeriesPoint {
  label: string
  count: number
}

export interface UISeries {
  title: string
  points: UISeriesPoint[]
}

export interface UIBlock {
  kind: string // record_list | media_grid | place_candidates | media_groups | record_detail | stats
  title: string
  records: UIRecord[]
  more?: number
  total?: number // 真实命中总数（列表卡计数与翻页用；语义检索路径不给）
  semantic?: boolean // 语义检索结果：无稳定总数，标注「按相关性截取，非全部命中」
  result_id?: string
  selectable?: boolean
  single_pick?: boolean
  stats?: UIStat[] // 统计概览卡：数字网格
  series?: UISeries[] // 统计概览卡：分布列表（按年 + 城市 Top 10）
  summaries?: { label: string; text: string }[] // 统计概览卡：分布摘要（一行一条，label + text）
}

/** 结果卡组件的事件契约：UiBlock 发出、MessageList 原样转发、ChatView 接收，三处共用一份。 */
export interface UiBlockEmits {
  (e: 'toggle-pick', resultId: string, rec: UIRecord, single: boolean): void
  (e: 'toggle-open', key: string): void
  (e: 'open-media', rec: UIRecord): void
  (e: 'load-more', ui: UIBlock | undefined): void
}

// 随消息提交的引用（仅当前会话）。结果集引用带 result_id；单条引用带 kind+id。
export interface QuoteRef {
  result_id?: string
  group?: string
  ids?: number[]
  kind?: string
  id?: number
  label?: string // 仅本地展示，不提交
}

export interface Bubble {
  role: 'user' | 'assistant' | 'tool'
  // 段落种类：think=思考段（reasoning 累积，可折叠）、text=正文段（content 累积）、
  // tool=工具步骤行（role:'tool' 补 kind 标记）、card=确认卡载体（cardID 挂在其上）。
  // undefined = 历史回显的整泡消息（兼容现行形态）。
  kind?: 'think' | 'text' | 'tool' | 'card'
  content: string
  cardID?: number
  // 气泡内渲染的媒体：本机选中的带 previewUrl（blob URL）；历史回显只有元数据（previewUrl 空）
  media?: MediaView[]
  ui?: UIBlock
  toolLabel?: string
  toolStatus?: 'running' | 'ok' | 'failed'
  reasoning?: string
  thinkOpen?: boolean
  thinkSeconds?: number
  // 卡片上的待上传附件预览：本机选中项（带 previewUrl）；历史回显无本机文件
  localMedia?: MediaView[]
  quoted?: QuoteRef[]
  interrupted?: boolean
  // 中断残局所属轮次（历史回显带出）：「编辑重发」据它调 DELETE /chat/turns/{id} 回收残局
  interruptedTurnId?: string
}

export interface AttachmentRef {
  name: string
  type: string
  size: number
}

export interface ProposalPreview {
  kind: string
  body: Record<string, unknown>
}

export interface CardItem {
  action_id: number
  intent: string
  summary: string
  preview: ProposalPreview
  warnings?: string[]
  attachments?: AttachmentRef[]
  status: 'pending' | 'executed' | 'cancelled' | 'expired' | 'failed'
  result?: string
  expires_in?: number
  received_at?: number
}

export interface ChatSession {
  id: number
  title: string
  created_at: string
  updated_at: string
}

