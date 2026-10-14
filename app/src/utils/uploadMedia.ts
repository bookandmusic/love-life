// 媒体上传的 HTTP 传输层（分片 + 断点续传）——发动态与 AI 模式共用同一实现。
//
// 分工：本文件只管「怎么发一片」（multipart、协议头、错误分类、片内重试）；
// 推进与重同步在 uploadChunkLoop.ts（纯函数、有单测），调用方见 uploadMediaFile。
//
// 协议（ARCHITECTURE「媒体上传管线」）：
//   - 首片（无 id）        → {upload_id, received}
//   - 首片即末片（+final） → {media_token, ...}（文件 ≤ 一片时一次请求走完）
//   - 中间片（id + offset）→ {upload_id, received}
//   - 末片（id + offset + final）→ {media_token, ...}
//   - offset 不符 → 409 + {received}（据此重同步，不重传已收部分）
import { ApiError, api } from '../api/client'
import MEDIA_LIMITS from '../../../server/internal/limits/media.json' with { type: 'json' }
import { runChunkLoop, type ChunkResp, type LoopResult } from './uploadChunkLoop'

/** 片大小（与「单条最多几个媒体」同一份共享 JSON，两端同源） */
export const CHUNK_BYTES = MEDIA_LIMITS.uploadChunkBytes

/** 单片最多尝试次数（首传 + 重试）；仅网络异常与 5xx 重试，4xx 不重试 */
const CHUNK_ATTEMPTS = 3

/** 可重试：网络层异常（非 ApiError）与 5xx；4xx 是确定性失败，重试无意义 */
function isRetriable(e: unknown): boolean {
  if (e instanceof ApiError) return e.status >= 500
  return true
}

function sleep(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms))
}

/** 发一片（含重试）。4xx 不重试，交由调用方处理。 */
async function sendChunk(
  file: File,
  offset: number,
  uploadId: string | null,
  isFinal: boolean,
): Promise<ChunkResp> {
  const end = Math.min(offset + CHUNK_BYTES, file.size)
  const form = new FormData()
  form.append('file', file.slice(offset, end), file.name)

  const headers: Record<string, string> = { 'X-Upload-Offset': String(offset) }
  if (uploadId) headers['X-Upload-Id'] = uploadId
  if (isFinal) headers['X-Upload-Final'] = '1'

  let lastErr: unknown
  for (let a = 0; a < CHUNK_ATTEMPTS; a++) {
    try {
      return await api<ChunkResp>('/api/v1/media/upload', { method: 'POST', body: form, headers })
    } catch (e) {
      // 409（offset 不符）：把服务端的 received 附到错误上，交给循环重同步（不当作失败）
      if (e instanceof ApiError && e.status === 409) {
        const received = e.details?.received
        if (typeof received === 'number' && received >= 0) {
          throw Object.assign(new Error(e.message), { status: 409, received })
        }
      }
      lastErr = e
      // 4xx（含 413）：确定性失败，交给调用方，重试只是白传
      if (!isRetriable(e)) throw e
      if (a < CHUNK_ATTEMPTS - 1) await sleep(500 * 3 ** a)
    }
  }
  throw lastErr
}

/**
 * uploadMediaFile 上传单个文件（自动分片），返回 media_token。
 *
 * 推进逻辑在 uploadChunkLoop.ts（纯函数、有单测）；这里只做 HTTP 传输与错误分类。
 * 失败语义：4xx（格式不支持 / 超限 / 上传已过期）与重试耗尽的网络错误都抛出；
 * 调用方按既有约定转「手动重传」（PRODUCT「发布动态」：个别失败不半发）。
 */
export async function uploadMediaFile(file: File): Promise<LoopResult> {
  return runChunkLoop(file, { sendChunk }, CHUNK_BYTES)
}
