import { http } from '../lib/http'

/**
 * 输出风格库。数据源由 VITE_API_BASE_URL 指向的服务决定。
 * 契约：
 * - 风格 = 名称 + 提示词片段（拼在全局 System Prompt 主干之后，切风格不动主干）
 * - 内置风格完全固化（不可删改）；自定义风格由后台管理
 * - default_style_id = 全局默认风格，APP 端未设置风格时使用
 */

export interface Style {
  id: number
  name: string
  prompt: string
  builtin: boolean
}

export interface StylesData {
  items: Style[]
  default_style_id: number
}

export function listStyles(): Promise<StylesData> {
  return http<StylesData>('/api/v1/admin/styles')
}

export function createStyle(payload: { name: string; prompt: string }): Promise<Style> {
  return http<Style>('/api/v1/admin/styles', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

/** 仅自定义风格可编辑（内置风格服务端拒绝） */
export function updateStyle(id: number, payload: { name?: string; prompt?: string }): Promise<Style> {
  return http<Style>(`/api/v1/admin/styles/${id}`, {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

/** 仅自定义风格可删除；默认风格需先切换默认 */
export function deleteStyle(id: number): Promise<void> {
  return http<void>(`/api/v1/admin/styles/${id}`, { method: 'DELETE' })
}

export function setDefaultStyle(id: number): Promise<StylesData> {
  return http<StylesData>('/api/v1/admin/styles/default', {
    method: 'PUT',
    body: JSON.stringify({ id }),
  })
}
