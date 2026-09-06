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