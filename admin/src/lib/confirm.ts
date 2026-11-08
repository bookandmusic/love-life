import { ref, type Ref } from 'vue'
import { errMsg } from './http'
import { toast } from './toast'

/**
 * 「确认弹窗 + 执行」通用流：五个视图的 confirmX/removing/doX 三件套只差
 * api 调用与 toast 文案，此处收敛公共骨架（null 守卫、busy 置位、成功后清 target）。
 * run 回调内自带成功 toast / 列表刷新；失败统一在此提示并保留弹窗可重试。
 */
export function useConfirmAction<T>(errorFallback: string) {
  const target: Ref<T | null> = ref(null)
  const busy = ref(false)

  async function confirm(run: (t: T) => Promise<void>): Promise<void> {
    const t = target.value
    if (!t || busy.value) return
    busy.value = true
    try {
      await run(t)
      target.value = null
    } catch (e) {
      toast.error(errMsg(e, errorFallback))
    } finally {
      busy.value = false
    }
  }

  return { target, busy, confirm }
}
