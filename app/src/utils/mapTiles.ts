// 地图瓦片共用工具（MapView / MiniMapPicker 单一实现）：WebMercator 投影 + 瓦片 URL。
// 约定：APP 统一走后端代理 /api/v1/map/tile，token 通过 query 传（img 标签无法带 header）。
import { getBase, getToken } from '../api/client'

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

/** 瓦片图片地址：base + token（打包后同样可用） */
export function tileUrl(z: number, x: number, y: number): string {
  const base = getBase()
  const tok = getToken() ?? ''
  const q = `z=${z}&x=${x}&y=${y}${tok ? `&token=${encodeURIComponent(tok)}` : ''}`
  return `${base}/api/v1/map/tile?${q}`
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