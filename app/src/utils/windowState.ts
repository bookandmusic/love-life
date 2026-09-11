// Tauri 窗口大小/位置记忆（阶段5.4 B20）：仅 Tauri 环境启用，浏览器内置静默退化
import { PhysicalPosition, PhysicalSize } from '@tauri-apps/api/dpi'
import { KEYS } from './storage'

interface Rect {
  x: number
  y: number
  width: number
  height: number
}

let saveTimer: number | undefined

async function capture(win: { innerPosition(): Promise<{ x: number; y: number }>; innerSize(): Promise<{ width: number; height: number }> }) {
  try {
    const pos = await win.innerPosition()
    const size = await win.innerSize()
    const rect: Rect = { x: pos.x, y: pos.y, width: size.width, height: size.height }
    if (saveTimer !== undefined) clearTimeout(saveTimer)
    saveTimer = window.setTimeout(() => localStorage.setItem(KEYS.WINDOW_STATE, JSON.stringify(rect)), 400)
  } catch {
    /* 保存失败静默 */
  }
}

export async function applyWindowState() {
  if (!('__TAURI_INTERNALS__' in window)) return
  try {
    const { getCurrentWindow } = await import('@tauri-apps/api/window')
    const win = getCurrentWindow()
    const raw = localStorage.getItem(KEYS.WINDOW_STATE)
    if (raw) {
      const r = JSON.parse(raw) as Rect
      if (Number.isFinite(r.x) && Number.isFinite(r.y) && Number.isFinite(r.width) && Number.isFinite(r.height)) {
        await win.setPosition(new PhysicalPosition(r.x, r.y))
        await win.setSize(new PhysicalSize(r.width, r.height))
      }
    }
    win.onResized(() => {
      void capture(win)
    })
    win.onMoved(() => {
      void capture(win)
    })
  } catch {
    /* 浏览器环境静默 */
  }
}