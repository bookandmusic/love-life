// 媒体类型的单一来源：中文词与兜底扩展名。服务端、客户端、mock 都从这里取口径，
// 避免三处各抄一份（历史上抄出过「mock 给 webm、客户端兜底说 mp4」的契约漂移）。
export type MediaKind = 'photo' | 'video' | 'audio'

export const MEDIA_KIND_LABEL: Record<MediaKind, string> = { photo: '照片', video: '视频', audio: '录音' }

// 生产口径（服务端下发 jpg / mp4 / m4a）。开发 mock 的样本是 jpg / webm / wav，
// 那边走媒体 URL 上的 ?ext= 参数对齐真实字节，见 mediaExtFromUrl。
export const MEDIA_KIND_EXT: Record<MediaKind, string> = { photo: 'jpg', video: 'mp4', audio: 'm4a' }

export function mediaKindLabel(kind: string): string {
  return MEDIA_KIND_LABEL[kind as MediaKind] ?? '媒体'
}

export function mediaKindExt(kind: string): string {
  return MEDIA_KIND_EXT[kind as MediaKind] ?? 'bin'
}

// 从媒体 URL 的 ?ext= 取真实扩展名（服务端与 mock 都会带；没有才退回上表）
export function mediaExtFromUrl(url: string): string | undefined {
  const m = /[?&]ext=([A-Za-z0-9]+)/.exec(url)
  return m ? m[1].toLowerCase() : undefined
}
