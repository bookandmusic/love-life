// AI 可用性状态（`GET /api/v1/chat/status`）的唯一口径。
//
// APP 设置页与 AI 对话页都要展示「AI 为什么不可用」，两处必须同一套文案，
// 故 reason 的加工只在此处实现（服务端原文形如
// 「未配置模型服务——请管理员在后台「AI 配置」添加模型 Key」）。
//
// 加工规则：
// 1. 取「——」后的可执行片段——前半句与页面标题（「AI 助手还没准备好」）语义重复；
// 2. 去掉「管理员」称谓——自部署场景下使用者本人就是后台主人，不该被称作管理员；
// 3. 兜底文案同样面向使用者，不出现「管理员」「用户」等系统口吻。
export interface AIStatus {
  available: boolean
  reason: string
}

const FALLBACK = '到后台配上模型就能用了。'

/** 网络层失败（连不上服务端）的兜底文案，设置页与对话页共用同一字面量。 */
export const AI_STATUS_OFFLINE = '暂时连不上服务端，稍后再试试。'

/** 把服务端 reason 加工成面向使用者的一句可执行提示（不含标题语，可直接作副文案）。 */
export function aiReasonText(status: Pick<AIStatus, 'reason'> | null | undefined): string {
  const raw = status?.reason?.trim()
  if (!raw) return FALLBACK
  const tail = raw.split('——').pop()?.trim() || raw
  return tail.replace(/管理员/g, '').trim() || FALLBACK
}
