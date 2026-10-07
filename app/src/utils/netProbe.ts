// 双服务地址探测与优选（AP1）：
// 在家走局域网直连、在外走公网域名；并行 ping /api/v1/health（无鉴权、轻量），
// 内网通优先选内网，否则公网；全不通保持现状（不猜测）。
import { getLanBase, getWanBase, setBase } from '../api/client'

const HEALTH_PATH = '/api/v1/health'
const PROBE_TIMEOUT = 3000

async function ping(base: string): Promise<boolean> {
  if (!base) return false
  const ctrl = new AbortController()
  const timer = setTimeout(() => ctrl.abort(), PROBE_TIMEOUT)
  try {
    const res = await fetch(base + HEALTH_PATH, { signal: ctrl.signal })
    return res.ok
  } catch {
    return false
  } finally {
    clearTimeout(timer)
  }
}

export interface ProbeStatus {
  lan: boolean
  wan: boolean
  lanConfigured: boolean
  wanConfigured: boolean
}

// probeAndSelect 探测并更新当前生效地址：内网通 → 内网；否则公网通 → 公网；
// 全不通 → 保持原值（可能为最后一次可达的地址）。只填一个地址时直接用该地址。
// 返回连通状态（登录页展示用，避免二次探测）。
export async function probeAndSelect(): Promise<ProbeStatus> {
  const lan = getLanBase()
  const wan = getWanBase()
  const [lanOk, wanOk] = await Promise.all([ping(lan), ping(wan)])
  if (lan && !wan) setBase(lan)
  else if (!lan && wan) setBase(wan)
  else if (lan && wan) {
    if (lanOk) setBase(lan)
    else if (wanOk) setBase(wan)
  }
  return { lan: lanOk, wan: wanOk, lanConfigured: !!lan, wanConfigured: !!wan }
}
