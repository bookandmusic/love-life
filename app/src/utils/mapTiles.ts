// 地图瓦片共用工具（MapView / MiniMapPicker 单一实现）：WebMercator 投影 + 瓦片 URL。
// 鉴权：APP 统一走后端代理 /api/v1/map/tile，签名经 query 传（img 标签无法带 header）。
// 签名由 /api/v1/map/config 下发，只绑过期时间、一次覆盖全部瓦片坐标，故无需逐张签名。
import { computed, ref } from 'vue'
import { api, getBase } from '../api/client'

// 签名 TTL 30 分钟（服务端 tileGrantTTL），每 10 分钟换一次，留足失败重试窗口
const GRANT_REFRESH_INTERVAL = 10 * 60 * 1000

let grant: { exp: number; sig: string } | null = null
// version 只在「从无到有」时自增：让已渲染的（无签）img 补上新签名；
// 后续换签不必重渲染——旧签名在 exp 前仍有效，新瓦片请求直接读最新 grant。
const version = ref(0)

// provider：/map/config 下发的「使用中」瓦片源类型。null=未加载/未知（不误判为未配源），
// ''=已加载但无启用源，非空=源类型（public/tianditu）。地图组件据此显示「未配置」提示。
const provider = ref<string | null>(null)
// noTileSource：明确「无启用瓦片源」（已加载且 provider 为空）时 true，供组件 v-if 覆盖提示。
export const noTileSource = computed(() => provider.value === '')

// refreshTileGrant 换一份瓦片签名（失败静默：保留旧签名，下轮再试）
export async function refreshTileGrant(): Promise<void> {
  try {
    const res = await api<{ exp?: number; sig?: string; provider?: string }>('/api/v1/map/config')
    provider.value = res.provider ?? ''
    if (res.exp && res.sig) {
      const hadGrant = grant !== null
      grant = { exp: res.exp, sig: res.sig }
      if (!hadGrant) version.value++
    }
  } catch {
    // 未登录/离线/瞬断：签名未过期前无感；provider 保持旧值，不把网络抖动误判为「未配源」
  }
}

// ensureTileGrant 进入地图前确保手上有未过期签名（无/临期则等一次换签）。
// 「无启用源」（provider===''）时不走捷径：后端无源也照常下发签名，若只看签名有效期，
// admin 启用源后要等定时换签（最长 10 分钟）或重启 APP 才生效——进地图重拉一次即可热加载。
export async function ensureTileGrant(): Promise<void> {
  if (provider.value !== '' && grant && grant.exp * 1000 - Date.now() > 60 * 1000) return
  await refreshTileGrant()
}

let started = false

// startTileGrantRefresh 全局启动定时换签（App.vue 挂载时调用一次）。
export function startTileGrantRefresh(): void {
  if (started) return
  started = true
  void refreshTileGrant()
  window.setInterval(() => void refreshTileGrant(), GRANT_REFRESH_INTERVAL)
}

export const TILE = 256

/** 经纬度 → 缩略级别 z 上的瓦片坐标（WebMercator） */
export function latLonToTile(lat: number, lon: number, z: number) {
  const n = Math.pow(2, z)
  const x = ((lon + 180) / 360) * n
  const latRad = (lat * Math.PI) / 180
  const y = ((1 - Math.asinh(Math.tan(latRad)) / Math.PI) / 2) * n
  return { x, y }
}

/** 瓦片坐标 + 缩放级别 → 经纬度（latLonToTile 的逆变换） */
export function tileToLatLon(x: number, y: number, z: number) {
  const n = Math.pow(2, z)
  const lon = (x / n) * 360 - 180
  const lat = (Math.atan(Math.sinh(Math.PI * (1 - (2 * y) / n))) * 180) / Math.PI
  return { lat, lon }
}

/** 瓦片图片地址：base + 短时签名（打包后同样可用） */
export function tileUrl(z: number, x: number, y: number): string {
  void version.value // 响应式依赖：首个签名到达后触发引用处重渲染（C11：显式丢弃表达式值）
  const base = getBase()
  const sig = grant ? `&exp=${grant.exp}&sig=${encodeURIComponent(grant.sig)}` : ''
  return `${base}/api/v1/map/tile?z=${z}&x=${x}&y=${y}${sig}`
}

/** 瓦片是否落在世界范围内（z 级下 x/y ∈ [0, 2^z)）：越界格没有图可请求，MapView 与 MiniMapPicker 共用 */
export function tileInWorld(z: number, x: number, y: number): boolean {
  const n = Math.pow(2, z)
  return x >= 0 && y >= 0 && x < n && y < n
}

export interface TileCell {
  x: number
  y: number
  left: number
  top: number
}

// 视口瓦片排布（MapView / MiniMapPicker 共用）：给定中心瓦片坐标、容器尺寸与缩放，
// 返回覆盖视口的瓦片格（含各格相对容器左上角的 left/top）。
export function tileGrid(cx: number, cy: number, cw: number, ch: number): TileCell[] {
  const ox = cx * TILE - cw / 2
  const oy = cy * TILE - ch / 2
  const x0 = Math.floor(ox / TILE)
  const y0 = Math.floor(oy / TILE)
  const cols = Math.ceil(cw / TILE) + 2
  const rows = Math.ceil(ch / TILE) + 2
  const cells: TileCell[] = []
  for (let i = 0; i < cols; i++) {
    for (let j = 0; j < rows; j++) {
      cells.push({ x: x0 + i, y: y0 + j, left: (x0 + i) * TILE - ox, top: (y0 + j) * TILE - oy })
    }
  }
  return cells
}