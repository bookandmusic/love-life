/** 媒体 MIME 类型语义判定（原 ChatView 内联 helper，多个聊天子组件共用后提取） */
export function isImage(type: string): boolean {
  return type.startsWith('image/')
}

export function isVideo(type: string): boolean {
  return type.startsWith('video/')
}
