// 删除 / 移出的统一文案（唯一措辞来源，避免各个面各写一套）
// 依据：REQUIREMENTS §6.4「删除动作写『移入回收站』」、§8 通知模型（删除对方内容发站内通知，自删静默）。

export const RECYCLE_TITLE = '移入回收站'
export const RECYCLE_CONFIRM = '移入回收站'
export const RECYCLE_TOAST = '已移入回收站'

/** extra：按类型的后果说明（媒体评论一并、地点名保留快照…），空则省略 */
export function recycleMsg(name: string, extra?: string): string {
  return `把「${name}」移入回收站？${extra ?? ''}想恢复的话，到后台回收站里找回。`
}

/** 删除对方发布的内容会通知对方；删自己的静默 */
export function peerNotice(selfId?: number, ownerId?: number): string {
  return selfId && ownerId && selfId !== ownerId ? '对方会收到一条通知。' : ''
}

// 「移出相册」是非破坏性动作：不进回收站，也不该用危险确认样式
export const REMOVE_TITLE = '移出当前相册'
export const REMOVE_CONFIRM = '移出'
export const REMOVE_TOAST = '已移出相册'

export function removeMsg(albumName: string): string {
  return `把这张照片移出「${albumName}」？只是移出相册，照片本身不会删除（仍在时间轴）。`
}
