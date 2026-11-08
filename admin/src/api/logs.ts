import { http } from '../lib/http'

/**
 * 服务日志（后台实时查看服务端运行日志）。数据源由 VITE_API_BASE_URL 指向的服务决定。
 *
 * 契约：
 * - 数据源是服务端**进程内环形缓冲**，不是数据库：重启后历史清空（重启前的日志在 stdout）
 * - 服务端写入缓冲前已脱敏（api_key / token / password 等替换为 ••••）
 * - 用 seq 做增量拉取：带上上次拿到的最大 seq，只取新条目（不重复、不丢）
 */

export interface LogEntry {
  /** 单调递增序号，用作增量游标 */
  seq: number
  time: string
  /** slog 级别名：DEBUG / INFO / WARN / ERROR */
  level: string
  msg: string
  /** 结构化字段（已脱敏）；无字段时省略 */
  attrs?: Record<string, string>
}

export interface LogPage {
  items: LogEntry[]
  latestSeq: number
}

/** since 为上次拿到的最大 seq；0 = 全量（首次加载） */
export function listLogs(since = 0): Promise<LogPage> {
  return http<LogPage>(`/api/v1/admin/logs?since=${since}`)
}
