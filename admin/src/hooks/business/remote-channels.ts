import { computed, ref } from 'vue'
import {
  deleteUglinkConfig,
  getUglinkConfig,
  saveUglinkConfig,
  testUglink,
  type UglinkConfig
} from '@/api/uglink'
import {
  deleteBackupAddress,
  listBackupAddresses,
  testBackupAddress,
  updateBackupAddress,
  type BackupAddress
} from '@/api/backupaddr'
import { errMsg } from '@/lib/http'
import { toast } from '@/lib/toast'
import { useConfirmAction } from '@/lib/confirm'

/** 通道权重：与服务端 defaultChannelWeight / channelWeightBuiltin 同口径 */
export const DEFAULT_WEIGHT = 50
export const BUILTIN_WEIGHT = 100

export type AddrType = 'uglink' | 'https'

export interface RemoteRow {
  kind: AddrType
  id: string
  name: string
  addr: string
  enabled: boolean
  /** 选路优先级（越大越优先）；内置地址恒 100 */
  weight: number
  loggedIn?: boolean
  /** 确定性配置错误（权限/凭据）：显示「配置错误」替代「未登录」 */
  configError?: string
}

/**
 * 远程访问通道的数据与操作：绿联单例配置 + HTTPS 地址列表。
 * 列表加载、探活、启停、删除的请求与提示在这里收敛；视图只保留表格列与添加/编辑表单。
 */
export function useRemoteChannels() {
  const uglink = ref<UglinkConfig>({ configured: false })
  const uglinkLoading = ref(true)
  const items = ref<BackupAddress[]>([])
  const loading = ref(true)

  const uglinkRow = computed<RemoteRow>(() => {
    const cfg = uglink.value
    return {
      kind: 'uglink',
      id: 'uglink',
      name: cfg.configured ? `绿联通道（${cfg.uglink_id ?? ''}）` : '绿联通道（未配置）',
      addr: cfg.configured ? `${cfg.uglink_id ?? ''} · 端口 ${cfg.port ?? 8080}` : '尚未配置',
      enabled: cfg.enabled ?? false,
      weight: cfg.weight ?? DEFAULT_WEIGHT,
      loggedIn: cfg.logged_in,
      configError: cfg.config_error
    }
  })

  const rows = computed<RemoteRow[]>(() => [
    uglinkRow.value,
    ...items.value.map((b) => ({
      kind: 'https' as const,
      id: b.id,
      name: b.name || '未命名',
      addr: b.url,
      enabled: b.enabled,
      weight: b.weight ?? DEFAULT_WEIGHT
    }))
  ])

  async function loadUglink() {
    uglinkLoading.value = true
    try {
      uglink.value = await getUglinkConfig()
    } catch (e) {
      toast.error(errMsg(e, '绿联通道配置加载失败'))
    } finally {
      uglinkLoading.value = false
    }
  }

  async function loadList() {
    loading.value = true
    try {
      items.value = (await listBackupAddresses()).items
    } catch (e) {
      toast.error(errMsg(e, '地址列表加载失败'))
    } finally {
      loading.value = false
    }
  }

  function loadAll() {
    loadUglink()
    loadList()
  }

  // 删除走确认弹窗（不用原生 confirm）；结果用全局 toast 反馈
  const remove = useConfirmAction<string>('删除失败')

  async function runRemove(id: string) {
    if (id === 'uglink') {
      await deleteUglinkConfig()
      toast.success('绿联通道配置已清除')
      await loadUglink()
      return
    }
    await deleteBackupAddress(id)
    toast.success('已删除')
    await loadList()
  }

  async function testHttpsRow(id: string) {
    const item = items.value.find((b) => b.id === id)
    if (!item) return
    try {
      const res = await testBackupAddress(item.url)
      if (res.ok) toast.success('服务端可达')
      else toast.error(res.reason || '不可达')
    } catch (e) {
      toast.error(errMsg(e, '测试失败'))
    }
  }

  async function testUglinkRow() {
    try {
      const res = await testUglink()
      // 服务端测试失败也回 HTTP 200 + {ok:false, reason}：不写 else 的话界面毫无反应
      if (res.ok) toast.success('连接成功')
      else toast.error(res.reason || '连接失败')
      // 测试会真实登录并更新失败计数/退避状态，回读刷新「已登录/未登录」标签
      await loadUglink()
    } catch (e) {
      toast.error(errMsg(e, '测试失败'))
    }
  }

  async function toggleEnabled(id: string) {
    if (id === 'uglink') {
      const cfg = uglink.value
      try {
        await saveUglinkConfig({
          uglink_id: cfg.uglink_id ?? '',
          port: cfg.port ?? 8080,
          username: cfg.username ?? '',
          enabled: !(cfg.enabled ?? false),
          // 权重必须显式回传：后端缺省沿用已保存的权重，这里带上可省一次依赖（已保存值即当前值）
          weight: cfg.weight ?? DEFAULT_WEIGHT
        })
        toast.success('已保存')
        await loadUglink()
      } catch (e) {
        toast.error(errMsg(e, '操作失败'))
      }
      return
    }
    const item = items.value.find((b) => b.id === id)
    if (!item) return
    const nextWeight = item.weight ?? DEFAULT_WEIGHT
    // 启用即参与选路：开关走同一套唯一性检查（表单提交时拦的是编辑路径，这里补开关路径），
    // 否则「停 A → 建同权重 B → 开 A」能造出两条同权重的启用通道，优先级意图失效
    if (!item.enabled) {
      const taken = new Map<number, string>()
      const cfg = uglink.value
      if (cfg.configured && cfg.enabled) taken.set(cfg.weight ?? DEFAULT_WEIGHT, '绿联通道')
      for (const b of items.value) {
        if (b.id === item.id || !b.enabled) continue
        taken.set(b.weight ?? DEFAULT_WEIGHT, b.name || b.url)
      }
      if (taken.has(nextWeight)) {
        toast.error(`权重 ${nextWeight} 已被「${taken.get(nextWeight)}」占用，无法启用；请先编辑权重`)
        return
      }
    }
    try {
      await updateBackupAddress(id, { name: item.name, url: item.url, enabled: !item.enabled, weight: nextWeight })
      await loadList()
    } catch (e) {
      toast.error(errMsg(e, '操作失败'))
    }
  }

  return {
    uglink,
    uglinkLoading,
    items,
    loading,
    rows,
    loadUglink,
    loadList,
    loadAll,
    remove,
    runRemove,
    testHttpsRow,
    testUglinkRow,
    toggleEnabled
  }
}
