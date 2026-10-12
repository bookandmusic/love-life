// 签名媒体地址的解析口径（纯函数，无浏览器依赖——故可被 node --test 直接覆盖）。
//
// 单独成模块的理由：mediaUrls.ts 顶部 import 了 api/client（模块加载即读 localStorage），
// 无法在 node 里直接跑；而「重签该用哪个 id」「这个地址要不要立刻换新」正是最容易写错、
// 又最需要守住的两条口径，故抽到这里由 mediaSign.test.mjs 守。
//
// 为什么 id 必须从 URL 路径解析：结果集记录（UIRecord）的 id 是**记录自己的 id**
// （动态记录是动态 id、相册记录是相册 id），只有 thumb_url 才指向媒体；
// 拿 rec.id 去重签会换错对象（客户端设计「签名与重签」）。

const MEDIA_FILE_RE = /\/api\/v1\/media\/file\/(\d+)/

/** mediaIdFromUrl 从签名 URL 解析媒体 id；非媒体地址返回 undefined。 */
export function mediaIdFromUrl(u: string | undefined): number | undefined {
  if (!u) return undefined
  const m = MEDIA_FILE_RE.exec(u)
  return m ? Number(m[1]) : undefined
}

/** signedExpMs 取签名 URL 的过期时刻（毫秒）；无 exp 返回 undefined。 */
export function signedExpMs(u: string | undefined): number | undefined {
  if (!u) return undefined
  const m = /[?&]exp=(\d+)/.exec(u)
  return m ? Number(m[1]) * 1000 : undefined
}

/**
 * mediaIdOf 取「该按哪个 id 处理这条媒体」——优先从签名 URL 路径解析（那才是签名主体），
 * 解析不出才退回记录自身的 id。结果集记录（UIRecord）的 id 是记录自己的 id
 * （动态记录给的是动态 id），拿它去重签会换错对象，故调用方一律走这里、不要各写一遍。
 */
export function mediaIdOf(m: { id?: number | string; url?: string; thumb_url?: string }): number | string | undefined {
  const fromUrl = mediaIdFromUrl(m.thumb_url) ?? mediaIdFromUrl(m.url)
  if (fromUrl !== undefined) return fromUrl
  return m.id === null ? undefined : m.id
}

/**
 * rawOrThumb 取可直接挂载的 raw 地址；缺 raw 时**回落 thumb**，绝不把 `v=thumb`
 * 改写成 `v=raw`——签名覆盖 variant，服务端按 raw 重算必然不匹配（401），
 * 那个地址永远打不开。先用能打开的 thumb 顶上，重签拿到真实 raw 后自动换新。
 */
export function rawOrThumb(m: { url?: string; thumb_url?: string }): string {
  return m.url || m.thumb_url || ''
}

/**
 * needsResign 注册到该媒体时是否要立刻换新签名（不等下一个 5 分钟周期，
 * 否则期间就是黑屏 / 裂图）。两种情形都要换：
 *   ① 地址已过期——历史回显读的是落库快照，15 分钟 TTL 早已过去；
 *   ② 只有 thumb、没有 raw——派生出的 raw 是 `v=thumb` 改成 `v=raw` 的产物，
 *      而签名覆盖 variant，改一个字母就验不过（必然 401）。
 */
export function needsResign(m: { url?: string; thumb_url?: string }, now: number): boolean {
  if (!m.url) return true
  const exp = signedExpMs(m.thumb_url || m.url)
  return exp !== undefined && exp <= now
}
