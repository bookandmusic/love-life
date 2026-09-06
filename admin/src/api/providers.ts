import { http } from '../lib/http'

/**
 * AI 供应商配置（协议化）。数据源由 VITE_API_BASE_URL 指向的服务决定。
 * 契约（DESIGN §4.3/§9）：
 * - 供应商 = 协议类型 + 地址 + 秘钥（只写不读）+ 模型列表 + 默认模型
 * - 列表有序：调用从第一个开始，失败依次向下（排序即故障切换链）
 * - 模型参数（temperature/max_tokens）可选，null = 调用时不传该参数
 */

export type ApiProtocol = 'openai-chat' | 'openai-responses' | 'anthropic' | 'gemini'

export const API_PROTOCOLS: Array<{ value: ApiProtocol; label: string; hint: string }> = [
  { value: 'openai-chat', label: 'OpenAI Chat Completions', hint: '兼容生态最广：OpenAI / DeepSeek / Kimi / 通义千问兼容模式 / GLM / vLLM / Ollama 等' },
  { value: 'openai-responses', label: 'OpenAI Responses', hint: 'OpenAI 新版接口' },
  { value: 'anthropic', label: 'Anthropic', hint: 'Claude 原生 Messages API' },
  { value: 'gemini', label: 'Gemini', hint: 'Google 原生 API' },
]

export function protocolLabel(type: ApiProtocol): string {
  return API_PROTOCOLS.find((p) => p.value === type)?.label ?? type
}

export interface ModelEntry {
  id: string
  /** null = 未设置，调用该模型时不传此参数（用供应商默认值） */
  temperature: number | null
  max_tokens: number | null
}

export interface AiProvider {
  id: number
  api_type: ApiProtocol
  name: string
  base_url: string
  has_key: boolean
  models: ModelEntry[]
  /** models 中某项的 id */
  default_model: string
}

export interface ProviderPayload {
  api_type: ApiProtocol
  name?: string
  base_url: string
  models?: ModelEntry[]
  default_model?: string
}

export function listProviders(): Promise<AiProvider[]> {
  return http<AiProvider[]>('/api/v1/admin/model-providers')
}

export function createProvider(payload: ProviderPayload & { api_key: string }): Promise<AiProvider> {
  return http<AiProvider>('/api/v1/admin/model-providers', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

/** 编辑：api_key 不传 = 不修改（只写不读） */
export function updateProvider(id: number, payload: Partial<ProviderPayload>): Promise<AiProvider> {
  return http<AiProvider>(`/api/v1/admin/model-providers/${id}`, {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

export function deleteProvider(id: number): Promise<void> {
  return http<void>(`/api/v1/admin/model-providers/${id}`, { method: 'DELETE' })
}

export function setProviderKey(id: number, apiKey: string): Promise<AiProvider> {
  return http<AiProvider>(`/api/v1/admin/model-providers/${id}/key`, {
    method: 'PUT',
    body: JSON.stringify({ api_key: apiKey }),
  })
}

/** 排序 = 故障切换链顺序；ids 必须恰好包含全部供应商 */
export function reorderProviders(ids: number[]): Promise<AiProvider[]> {
  return http<AiProvider[]>('/api/v1/admin/model-providers/order', {
    method: 'PUT',
    body: JSON.stringify({ ids }),
  })
}

/** 获取模型列表：服务端持地址+秘钥调供应商 models 接口；已保存秘钥的供应商可只传 provider_id */
export function fetchModels(payload: {
  api_type: ApiProtocol
  base_url: string
  api_key?: string
  provider_id?: number
}): Promise<{ models: string[] }> {
  return http<{ models: string[] }>('/api/v1/admin/model-providers/fetch-models', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export interface SystemPromptData {
  content: string
  updatedAt: string
}

export function getSystemPrompt(): Promise<SystemPromptData> {
  return http<SystemPromptData>('/api/v1/admin/system-prompt')
}

export function saveSystemPrompt(content: string): Promise<SystemPromptData> {
  return http<SystemPromptData>('/api/v1/admin/system-prompt', {
    method: 'PUT',
    body: JSON.stringify({ content }),
  })
}
