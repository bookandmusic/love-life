import { http } from '../lib/http'

/**
 * AI 供应商与模型链（按类型分链）。数据源由 VITE_API_BASE_URL 指向的服务决定。
 * 契约：
 * - 供应商 = 协议类型 + 地址 + 秘钥（只写不读）
 * - 模型条目按 kind 分两条链（chat / embedding），链内顺序 = 故障切换顺序；
 *   用哪个模型、按什么顺序重试，全部由链序决定（供应商本身无默认模型）
 * - 模型参数（max_tokens / thinking / thinking_levels / context_window）可选，null = 调用时不传该参数
 * - 嵌入链只允许 openai-chat 供应商（服务端校验）
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

export type ModelKind = 'chat' | 'embedding'

/**
 * 思考档位（按模型，缺省 off）。
 * off = 该协议的最低档——现役模型关不掉思考，省略参数只会让它留在自己的默认档；
 * 开启会产生**思考 token 计费**，故由使用者按模型决定。
 * 各协议映射到原生参数（anthropic thinking / gemini thinkingConfig / responses reasoning）。
 */
export const THINKING_OPTIONS = [
  { value: 'off', label: '关' },
  { value: 'low', label: '低' },
  { value: 'medium', label: '中' },
  { value: 'high', label: '高' },
] as const

/** 取值即 THINKING_OPTIONS 的 value，避免档位在两个地方各写一遍（加档只改上面那一处）。 */
export type ThinkingLevel = (typeof THINKING_OPTIONS)[number]['value']

export interface ModelEntry {
  id: string
  kind: ModelKind
  /** null = 未设置，调用该模型时不传此参数（用供应商默认值） */
  max_tokens: number | null
  thinking: ThinkingLevel
  /** 该模型支持的思考档位集合（逗号分隔，服务端归一为固定顺序且恒含 off）；「思考」下拉的可选项来源 */
  thinking_levels: string
  /** 模型上下文窗口（token）；null = 未声明，用服务端兜底预算 */
  context_window: number | null
}

export interface AiProvider {
  id: number
  api_type: ApiProtocol
  name: string
  base_url: string
  /** 等待上游响应头（首包）的超时秒数；null = 默认 30s。 */
  header_timeout_sec: number | null
  has_key: boolean
  models: ModelEntry[]
}

export interface ProviderPayload {
  api_type: ApiProtocol
  name?: string
  base_url: string
  /** 秒；不传 = 默认 30s。 */
  header_timeout_sec?: number
  models?: ModelEntry[]
}

/** 模型链上的一行（链序 = 数组顺序）。 */
export interface ModelRow {
  id: number
  provider_id: number
  provider_name: string
  api_type: ApiProtocol
  kind: ModelKind
  model_id: string
  max_tokens: number | null
  thinking: ThinkingLevel
  thinking_levels: string
  context_window: number | null
}

export type ModelChains = { chat: ModelRow[]; embedding: ModelRow[] }

export function listModels(): Promise<ModelChains> {
  return http<ModelChains>('/api/v1/admin/models')
}

export function createModel(payload: {
  kind: ModelKind
  provider_id: number
  model_id: string
  max_tokens?: number | null
  thinking?: ThinkingLevel
  thinking_levels?: string
  context_window?: number | null
}): Promise<ModelChains> {
  return http<ModelChains>('/api/v1/admin/models', { method: 'POST', body: JSON.stringify(payload) })
}

export function updateModel(
  id: number,
  payload: {
    model_id?: string
    max_tokens?: number | null
    thinking?: ThinkingLevel
    thinking_levels?: string
    context_window?: number | null
  },
): Promise<ModelChains> {
  return http<ModelChains>(`/api/v1/admin/models/${id}`, { method: 'PUT', body: JSON.stringify(payload) })
}

export function deleteModel(id: number): Promise<void> {
  return http<void>(`/api/v1/admin/models/${id}`, { method: 'DELETE' })
}

/** 链内重排：ids 必须恰好覆盖该 kind 的全部条目 */
export function reorderModels(kind: ModelKind, ids: number[]): Promise<ModelChains> {
  return http<ModelChains>('/api/v1/admin/models/order', {
    method: 'PUT',
    body: JSON.stringify({ kind, ids }),
  })
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
