<script setup lang="ts">
/**
 * 回收站：已删除内容列表 + 批量恢复；保留期限设置内联于顶部提示条
 * （参考应用更新页的代理开关：简单设置不单开页面）。
 * 恢复与清除流水在「日志 · 操作日志」。
 */
import { computed, h, onMounted, ref, watch } from 'vue'
import { NAlert, NButton, NCard, NCheckbox, NDataTable, NInputNumber, NPagination, NSelect, NTag, type DataTableColumns } from 'naive-ui'
import { RefreshCw } from 'lucide-vue-next'
import { recycleApi, securityApi } from '@/api'
import { RECYCLE_PAGE_SIZE, type RecycleItem, type RecycleType } from '@/api/recycle'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { errMsg } from '@/lib/http'
import { toast } from '@/lib/toast'
import { useConfirmAction } from '@/lib/confirm'

// 恢复目标：单条与批量共用（ids 恒为数组，summary 用于确认文案）
interface RestoreTarget {
  ids: number[]
  summary: string
}

const items = ref<RecycleItem[]>([])
const loading = ref(false)
const filterType = ref('全部')
const selected = ref<Set<number>>(new Set())

// 服务端分页（limit/offset）：固定 50 条/页，total 决定页数
const page = ref(1)
const total = ref(0)
const hasMore = ref(false)
const pageCount = computed(() => Math.max(1, Math.ceil(total.value / RECYCLE_PAGE_SIZE)))

// 保留期限设置内联于提示条（简单设置不单开页面）；null = 读不到服务端值，禁编辑
const retentionDays = ref<number | null>(30)
const savingRetention = ref(false)

async function loadRetention() {
  try {
    retentionDays.value = (await securityApi.getSecuritySettings()).recycleRetentionDays
  } catch {
    // 保留期加载失败不阻塞回收站主功能；置空并禁用输入，避免把默认值当真实值保存
    retentionDays.value = null
    toast.error('保留期限读取失败，可刷新重试')
  }
}

async function saveRetention() {
  const days = retentionDays.value
  // 保留期范围 7 ~ 365 天：超范围直接报错，不发请求
  if (days === null || !Number.isInteger(days) || days < 7 || days > 365) {
    toast.error('保留期限需在 7 ~ 365 天之间')
    return
  }
  savingRetention.value = true
  try {
    await securityApi.saveSecuritySettings({ recycleRetentionDays: days })
    toast.success('回收站保留期限已保存')
    await loadRetention()
  } catch (e) {
    toast.error(errMsg(e, '保存失败'))
  } finally {
    savingRetention.value = false
  }
}

const typeOptions = ['全部', '动态', '评论', '相册', '纪念日', '地图点位'].map((t) => ({ label: t, value: t }))

const typeTag: Record<RecycleType, 'info' | 'default' | 'warning' | 'error' | 'success'> = {
  动态: 'info',
  评论: 'default',
  相册: 'warning',
  纪念日: 'error',
  地图点位: 'success',
}

async function load() {
  loading.value = true
  try {
    let res = await recycleApi.listRecycle({ limit: RECYCLE_PAGE_SIZE, offset: (page.value - 1) * RECYCLE_PAGE_SIZE })
    // 恢复后当前页可能被清空：退到最后一页，不停在空页
    if (res.items.length === 0 && page.value > 1) {
      page.value = Math.max(1, Math.ceil(res.total / RECYCLE_PAGE_SIZE))
      res = await recycleApi.listRecycle({ limit: RECYCLE_PAGE_SIZE, offset: (page.value - 1) * RECYCLE_PAGE_SIZE })
    }
    // 按删除时间排序（倒序）；前端排序兜底，不依赖后端返回顺序
    items.value = [...res.items].sort((a, b) => b.deletedAt.localeCompare(a.deletedAt))
    total.value = res.total
    hasMore.value = res.has_more
    selected.value = new Set()
  } catch (e) {
    toast.error(errMsg(e, '回收站加载失败'))
  } finally {
    loading.value = false
  }
}

function goPage(target: number) {
  if (target < 1 || target === page.value) return
  // 服务端说没有下一页时不往后翻（total 与 has_more 不一致时以服务端标记为准）
  if (target > page.value && !hasMore.value) return
  page.value = target
  load()
}

onMounted(() => {
  load()
  loadRetention()
})

const filtered = computed(() => items.value.filter((i) => filterType.value === '全部' || i.type === filterType.value))

// 切换筛选后旧选中项可能已不可见，清空避免批量恢复作用到当前列表外的内容
watch(filterType, () => {
  selected.value = new Set()
})

// 恢复走确认弹窗（不用原生 confirm）；结果用全局 toast 反馈，操作记录「日志 · 操作日志」
const restoreAction = useConfirmAction<RestoreTarget>('恢复失败')

function restore(item: RecycleItem) {
  restoreAction.target.value = { ids: [item.id], summary: item.summary }
}

function restoreSelected() {
  const ids = [...selected.value]
  if (ids.length === 0) return
  restoreAction.target.value = { ids, summary: `选中的 ${ids.length} 条内容` }
}

async function runRestore(target: RestoreTarget) {
  if (target.ids.length === 1) {
    await recycleApi.restoreRecycle(target.ids[0])
    toast.success(`已恢复「${target.summary}」`)
  } else {
    await recycleApi.restoreMany(target.ids)
    toast.success(`已恢复 ${target.ids.length} 条内容`)
  }
  await load()
}

const columns: DataTableColumns<RecycleItem> = [
  {
    title: '',
    key: 'select',
    width: 40,
    render: (row) =>
      h(NCheckbox, {
        checked: selected.value.has(row.id),
        'onUpdate:checked': (v: boolean) => {
          const next = new Set(selected.value)
          if (v) next.add(row.id)
          else next.delete(row.id)
          selected.value = next
        }
      })
  },
  {
    title: '内容类型',
    key: 'type',
    width: 100,
    render: (row) =>
      h(NTag, { type: typeTag[row.type] ?? 'default', size: 'small', bordered: false }, { default: () => row.type })
  },
  { title: '内容摘要', key: 'summary', minWidth: 220, ellipsis: { tooltip: true } },
  { title: '删除时间', key: 'deletedAt', width: 150 },
  { title: '删除者', key: 'deletedBy', width: 100 },
  {
    title: '',
    key: 'actions',
    width: 80,
    align: 'right',
    render: (row) =>
      h(NButton, { size: 'small', type: 'primary', secondary: true, onClick: () => restore(row) }, { default: () => '恢复' })
  }
]
</script>

<template>
  <div class="view">
    <NAlert type="warning" :show-icon="false">
      <div class="retention-line">
        <span>回收站中的内容将保留</span>
        <NInputNumber
          v-model:value="retentionDays"
          size="small"
          :min="7"
          :max="365"
          :disabled="retentionDays === null"
          class="retention-input"
        />
        <span>天，超过期限将由定时任务自动永久删除，仅管理员可恢复。</span>
        <NButton
          size="small"
          type="primary"
          secondary
          :loading="savingRetention"
          :disabled="retentionDays === null"
          @click="saveRetention"
        >
          保存期限
        </NButton>
      </div>
    </NAlert>

    <div class="pane-toolbar">
      <NSelect v-model:value="filterType" :options="typeOptions" class="filter-select" />
      <span v-if="filterType !== '全部'" class="filter-hint">仅筛选当前页</span>
      <NButton type="primary" :disabled="selected.size === 0" @click="restoreSelected">
        <template #icon>
          <RefreshCw :size="14" :stroke-width="1.8" />
        </template>
        批量恢复{{ selected.size > 0 ? `（${selected.size}）` : '' }}
      </NButton>
      <NButton quaternary @click="load">
        <template #icon>
          <RefreshCw :size="14" :stroke-width="1.8" />
        </template>
        刷新
      </NButton>
    </div>

    <NCard size="small" class="table-card">
      <NDataTable
        flex-height
        style="flex: 1; min-height: 0"
        :columns="columns"
        :data="filtered"
        :loading="loading"
        :scroll-x="760"
        :bordered="false"
        :row-key="(row: RecycleItem) => row.id"
      />
    </NCard>

    <div class="pager">
      <span class="pager-total">共 {{ total }} 条</span>
      <NPagination v-if="pageCount > 1" :page="page" :page-count="pageCount" :disabled="loading" @update:page="goPage" />
    </div>

    <ConfirmDialog
      v-if="restoreAction.target.value"
      title="恢复回收站内容"
      :message="`确认恢复「${restoreAction.target.value.summary}」？该操作会记录到「日志 · 操作日志」。`"
      confirm-label="恢复"
      variant="primary"
      :busy="restoreAction.busy.value"
      @close="restoreAction.target.value = null"
      @confirm="restoreAction.confirm(runRestore)"
    />
  </div>
</template>

<style scoped>
.view {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  height: 100%;
}

.retention-line {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.retention-input {
  width: var(--control-sm);
}

.pane-toolbar {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

.filter-select {
  width: var(--control-sm);
}

.filter-hint {
  font-size: var(--text-sm);
  color: var(--color-ink-tertiary);
}

.pager {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: var(--space-3);
}

.pager-total {
  font-size: var(--text-sm);
  color: var(--color-ink-tertiary);
}

.table-card {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}
</style>
