import { http } from '../lib/http'

export type UserRole = 'admin' | 'girl' | 'boy'

export interface UserItem {
  id: number
  username: string
  role: UserRole
  nickname: string
  /** 仅后台维护，不下发 APP */
  birthday?: string
  /** 头像签名 URL；空串表示无头像（独立通道） */
  avatar?: string
}

export function listUsers(): Promise<UserItem[]> {
  return http<UserItem[]>('/api/v1/admin/users')
}

export function resetPassword(id: number, password: string): Promise<void> {
  return http<void>(`/api/v1/admin/users/${id}/reset-password`, {
    method: 'POST',
    body: JSON.stringify({ password }),
  })
}

export interface CoupleSidePayload {
  username: string
  nickname: string
  password: string
}

// 情侣账户原子创建：双方一次提交，任一情侣角色已存在则被服务端拒绝
export function createCouple(payload: { girl: CoupleSidePayload; boy: CoupleSidePayload }): Promise<{ girl: UserItem; boy: UserItem }> {
  return http<{ girl: UserItem; boy: UserItem }>('/api/v1/admin/users/couple', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

// 更新昵称与生日（同端点；用户名/角色不可修改，admin 账号服务端拒绝）。
// birthday 省略 = 不改，空串 = 清除。
export function updateUser(id: number, nickname: string, birthday?: string): Promise<UserItem> {
  const body: { nickname: string; birthday?: string } = { nickname }
  if (birthday !== undefined) body.birthday = birthday
  return http<UserItem>(`/api/v1/admin/users/${id}`, {
    method: 'PUT',
    body: JSON.stringify(body),
  })
}

// 头像上传：multipart/form-data，字段名 avatar
export function uploadAvatar(id: number, file: File): Promise<{ status: string; avatar: string }> {
  const form = new FormData()
  form.append('avatar', file)
  return http<{ status: string; avatar: string }>(`/api/v1/admin/users/${id}/avatar`, {
    method: 'POST',
    body: form,
  })
}

// 生日：仅后台维护，空串清除；格式 YYYY-MM-DD。
// 走 updateUser 的同端点（服务端另有该独立端点，保留兼容，新代码不再调）。
export function updateBirthday(id: number, nickname: string, birthday: string): Promise<UserItem> {
  return updateUser(id, nickname, birthday)
}
