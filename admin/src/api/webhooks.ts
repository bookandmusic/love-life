import { http } from '../lib/http'

export type WebhookMethod = 'GET' | 'POST'

export interface WebhookChannel {
  id: number
  name: string
  method: WebhookMethod
  url: string
  headers: Record<string, string>
  body_template: string
  events: string[]
}

/** 订阅事件（7 类，以 为准） */
export const WEBHOOK_EVENTS: Array<[string, string]> = [
  ['new_post', '新动态'],
  ['like', '点赞'],
  ['comment', '评论'],
  ['favorite', '收藏'],
  ['anniversary', '纪念日'],
  ['footprint', '足迹'],
  ['album', '相册上新'],
]

/** 预设模板（Bark/Server酱/Qmsg/企业微信） */
export const WEBHOOK_PRESETS: Record<string, string> = {
  Bark: 'https://api.day.app/你的key',
  'Server酱': 'https://sctapi.ftqq.com/你的key.send',
  Qmsg: 'https://qmsg.zendee.cn/send/你的key',
  '企业微信': 'https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=你的key',
}

export function listWebhooks(): Promise<{ items: WebhookChannel[] }> {
  return http<{ items: WebhookChannel[] }>('/api/v1/admin/webhooks')
}

export function createWebhook(
  payload: Omit<WebhookChannel, 'id'>,
): Promise<WebhookChannel> {
  return http<WebhookChannel>('/api/v1/admin/webhooks', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function updateWebhook(
  id: number,
  payload: Omit<WebhookChannel, 'id'>,
): Promise<WebhookChannel> {
  return http<WebhookChannel>(`/api/v1/admin/webhooks/${id}`, {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

export function deleteWebhook(id: number): Promise<void> {
  return http<void>(`/api/v1/admin/webhooks/${id}`, { method: 'DELETE' })
}

export function testWebhook(id: number): Promise<void> {
  return http<void>(`/api/v1/admin/webhooks/${id}/test`, { method: 'POST' })
}
