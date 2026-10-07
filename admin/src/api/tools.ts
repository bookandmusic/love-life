import { http } from '../lib/http'

/**
 * MCP 工具清单（定义）。数据源由 VITE_API_BASE_URL 指向的服务决定。
 * schema：参数名册（键=参数名，值="类型 — 说明"），详情展示用。
 */

export type ToolCategory = '动态' | '相册' | '地图' | '纪念日' | '互动' | '空间'

export interface McpTool {
  name: string
  description: string
  category: ToolCategory
  enabled: boolean
  schema: Record<string, string>
}

export function listTools(): Promise<McpTool[]> {
  return http<McpTool[]>('/api/v1/admin/mcp-tools')
}

export function setToolEnabled(name: string, enabled: boolean): Promise<McpTool> {
  return http<McpTool>(`/api/v1/admin/mcp-tools/${encodeURIComponent(name)}`, {
    method: 'PATCH',
    body: JSON.stringify({ enabled }),
  })
}
