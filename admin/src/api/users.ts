import { http } from '../lib/http'

export type UserRole = 'admin' | 'girl' | 'boy'

export interface UserItem {
  id: number
  username: string
  role: UserRole
  nickname: string
}

export interface CreateUserPayload {
  username: string
  nickname: string
  role: Exclude<UserRole, 'admin'>
  password: string
}

export function listUsers(): Promise<UserItem[]> {
  return http<UserItem[]>('/api/v1/admin/users')
}

export function createUser(payload: CreateUserPayload): Promise<UserItem> {
  return http<UserItem>('/api/v1/admin/users', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
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

// 仅允许更新昵称；用户名/角色不可修改
export function updateUser(id: number, nickname: string): Promise<UserItem> {
  return http<UserItem>(`/api/v1/admin/users/${id}`, {
    method: 'PUT',
    body: JSON.stringify({ nickname }),
  })
}
