// 分片上传的**循环推进逻辑**（纯函数，无浏览器依赖——故可被 node --test 直接覆盖）。
//
// 单独成模块的理由：uploadMedia.ts 顶部 import 了 api/client（模块加载即读 localStorage），
// 与 mediaSign.ts 同一个理由——把最容易写错、又最需要守住的那段口径抽出来单测。
//
// 守的是循环的**出口条件**（缺了会原地打转）：
//   ① offset 用服务端回的 received 推进，不用本地累加 → 「响应丢失但字节已落盘」能自动纠正；
//   ② 「字节已收齐却没拿到 token」（末片响应丢失、服务端已收尾）要再发一次末片头去取结果
//      —— 缺了它，409 把 offset 推到文件末尾后 slice 返回空片，循环既不退出也不前进。

/** 一次分片请求的响应：完成（带 media_token）或中间态（带 received） */
export interface ChunkResp {
  media_token?: string
  kind?: string
  size?: number
  width?: number
  height?: number
  duration?: number
  taken_at?: string
  upload_id?: string
  received?: number
}

/** 服务端回报 offset 不符：带实际已收长度，客户端据此重同步 */
export interface OffsetMismatch {
  received: number
}

/** 传输层抽象：真实现发 HTTP，测试注入假实现 */
export interface ChunkTransport {
  sendChunk(file: File, offset: number, uploadId: string | null, isFinal: boolean): Promise<ChunkResp>
}

export interface LoopResult {
  media_token: string
  kind?: string
  size?: number
  width?: number
  height?: number
  duration?: number
  taken_at?: string
}

/**
 * runChunkLoop 分片推进主循环：按服务端回报推进 offset，直到拿到 media_token。
 * 失败（4xx 或重试耗尽）由 transport 抛出，这里只负责推进与收敛。
 */
export async function runChunkLoop(
  file: File,
  transport: ChunkTransport,
  chunkBytes: number,
): Promise<LoopResult> {
  let offset = 0
  let uploadId: string | null = null
  // 防御性上限：正常情况请求数 = 片数 + 少量重同步；超出说明协议逻辑有环，直接失败而非死循环
  const maxRequests = Math.ceil(file.size / chunkBytes) + 10

  for (let req = 0; req < maxRequests; req++) {
    const isFinal = offset + chunkBytes >= file.size
    let resp: ChunkResp
    try {
      resp = await transport.sendChunk(file, offset, uploadId, isFinal)
    } catch (e) {
      // offset 不符：用服务端回的长度重同步，不重传已收部分
      const mismatch = e as OffsetMismatch & { status?: number }
      if (mismatch && mismatch.status === 409 && typeof mismatch.received === 'number') {
        offset = mismatch.received
        continue
      }
      throw e
    }

    if (resp.media_token) {
      // 完整性命中校验：服务端回的 size 必须等于文件大小。防「新 APP + 旧服务端」
      // ——旧服务端不认分片头，会把首片当完整文件收下并回 media_token，
      // 客户端若直接采信就会**静默截断**（20MB 视频只传了 8MB 还报成功）。
      if (typeof resp.size === 'number' && resp.size !== file.size) {
        throw new Error(`上传未完成：服务端收到 ${resp.size} 字节，应为 ${file.size} 字节（请升级服务端）`)
      }
      return {
        media_token: resp.media_token,
        kind: resp.kind,
        size: resp.size,
        width: resp.width,
        height: resp.height,
        duration: resp.duration,
        taken_at: resp.taken_at,
      }
    }
    if (resp.upload_id) uploadId = resp.upload_id

    const received = resp.received
    if (typeof received !== 'number') throw new Error('上传响应缺少 received')
    // 字节已收齐却没拿到 token（末片响应丢失、服务端已收尾）：再发一次末片头，
    // 命中服务端的 finalized 标记即可取回结果（见文件头注释②）
    offset = received >= file.size ? file.size : received
  }
  throw new Error('上传未收敛，请重试')
}
