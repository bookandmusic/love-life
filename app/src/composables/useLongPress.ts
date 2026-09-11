// 长按识别：低频操作（移入回收站 / 移出相册）的统一入口。
// 展示面不放常驻图标——长按 500ms 触发；手指移动超过 8px（在滚动）、抬起、指针取消都作废；
// 触发后的那次 click 必须吞掉，否则会顺带执行短按动作（打开大图、地图居中）。
// pressing 保存当前按住项的 id，供模板做「按住即高亮」的反馈（不用 :active，避免点按钮时整卡闪灰）。
import { onBeforeUnmount, ref } from 'vue'

export function useLongPress(ms = 500) {
  const pressing = ref<number | string | null>(null)
  let timer: number | null = null
  let fire: (() => void) | null = null
  let fired = false
  let sx = 0
  let sy = 0

  function up() {
    if (timer !== null) {
      clearTimeout(timer)
      timer = null
    }
    fire = null
    pressing.value = null
  }

  function down(e: PointerEvent, id: number | string, cb: () => void) {
    fired = false
    sx = e.clientX
    sy = e.clientY
    fire = cb
    pressing.value = id
    timer = window.setTimeout(() => {
      timer = null
      fire = null
      fired = true
      pressing.value = null
      cb()
    }, ms)
  }

  function move(e: PointerEvent) {
    if (timer !== null && fire && (Math.abs(e.clientX - sx) > 8 || Math.abs(e.clientY - sy) > 8)) up()
  }

  function click(e: MouseEvent) {
    if (!fired) return
    fired = false
    e.preventDefault()
    e.stopPropagation()
  }

  onBeforeUnmount(up)
  return { pressing, down, move, up, click }
}
