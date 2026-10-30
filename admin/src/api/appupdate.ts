import { http } from '../lib/http'

// 应用内更新中转配置：服务端从 GitHub 取回并缓存，APP 只连本服务。
// 代理仅作用于 GitHub 出站（模型 API / 地图瓦片 / Webhook 不受影响）。
export interface AppUpdateCache {
  version: string
  size_bytes: number
  fetch_time: string
  file_name: string
}

export interface AppUpdateConfig {
  proxy_url: string
  proxy_user: string
  has_password: boolean
  cache: AppUpdateCache
  /** 后台下载任务进行中 */
  downloading: boolean
  download_of: string
  download_err: string
}

export interface AppUpdateConfigPayload {
  proxy_url: string
  proxy_user: string
  proxy_password?: string // 留空 = 沿用旧密码
  clear_password?: boolean
}

export interface CheckResult {
  ok: boolean
  reason?: string
  version_name?: string
  asset_size?: number
  ready?: boolean
}

export function getAppUpdateConfig(): Promise<AppUpdateConfig> {
  return http<AppUpdateConfig>('/api/v1/admin/appupdate/config')
}

export function saveAppUpdateConfig(
  payload: AppUpdateConfigPayload,
): Promise<{ ok: boolean }> {
  return http<{ ok: boolean }>('/api/v1/admin/appupdate/config', {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

/** 查 GitHub 最新版本（不下载） */
export function checkAppUpdate(): Promise<CheckResult> {
  return http<CheckResult>('/api/v1/admin/appupdate/check', { method: 'POST' })
}

/** 后台开始下载安装包（长任务，立即返回） */
export function startAppUpdateDownload(): Promise<{ ok: boolean; reason?: string }> {
  return http<{ ok: boolean; reason?: string }>('/api/v1/admin/appupdate/download', { method: 'POST' })
}

/** 删除已缓存的安装包 */
export function removeAppUpdate(): Promise<{ ok: boolean }> {
  return http<{ ok: boolean }>('/api/v1/admin/appupdate/remove', { method: 'POST' })
}