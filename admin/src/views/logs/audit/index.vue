<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue'
import { NButton, NCard, NDataTable, NSelect, NTag, type DataTableColumns } from 'naive-ui'
import { auditApi } from '@/api'
import type { AuditEntry, AuditActorType } from '@/api/audit'
import SectionHeader from '@/components/SectionHeader.vue'
import { errMsg } from '@/lib/http'
import { toast } from '@/lib/toast'

const entries = ref<AuditEntry[]>([])
const filterType = ref<string>('全部')
// 分页状态：audit_logs 永久保留、随时间线性增长，全量拉取会越来越慢
const total = ref(0)
const page = ref(1)
const PAGE_SIZE = 20
const loadingMore = ref(false)
const hasMore = computed(() => entries.value.length < total.value)

const actorMeta: Record<AuditActorType, { label: string; tag: 'info' | 'warning' | 'default' }> = {
  user: { label: '用户', tag: 'info' },
  ai: { label: 'AI', tag: 'warning' },
  pat: { label: 'PAT', tag: 'default' },
}

const typeOptions = [
  { label: '全部类型', value: '全部' },
  { label: '用户操作', value: 'user' },
  { label: 'AI 操作', value: 'ai' },
  { label: 'PAT 操作', value: 'pat' },
]

async function loadPage(p: number): Promise<void> {
  const res = await auditApi.listAudit(p, PAGE_SIZE)
  entries.value = p === 1 ? res.items : [...entries.value, ...res.items]
  total.value = res.total
  page.value = res.page
}

onMounted(async () => {
  try {
    await loadPage(1)
  } catch (e) {
    toast.error(errMsg(e, '审计日志加载失败'))
  }
})

async function loadMore(): Promise<void> {
  if (loadingMore.value || !hasMore.value) return
  loadingMore.value = true
  try {
    await loadPage(page.value + 1)
  } catch (e) {
    toast.error(errMsg(e, '加载更多失败'))
  } finally {
    loadingMore.value = false
  }
}

const filtered = computed(() =>
  entries.value.filter((e) => filterType.value === '全部' || e.actorType === filterType.value),
)

// latencyMs 仅 ai/pat 调用有值（模型响应耗时）；user 直接写库为 null
function latency(e: AuditEntry): string {
  if (e.latencyMs === null) return '—'
  return e.latencyMs >= 1000 ? (e.latencyMs / 1000).toFixed(1) + 's' : e.latencyMs + 'ms'
}

// result 是四态而非布尔：出卡待确认与用户主动取消都不是失败，报成「失败」是误报。
// 失败值可带原因（存成 `failed: 原因`），用 title 提示展示。
const RESULT_META: Record<string, { label: string; tag: 'success' | 'error' | 'warning' | 'default' }> = {
  success: { label: '成功', tag: 'success' },
  failed: { label: '失败', tag: 'error' },
  pending: { label: '待确认', tag: 'warning' },
  cancelled: { label: '已取消', tag: 'default' },
}

const resultMeta = (raw: string) => {
  const key = raw.split(':')[0]!.trim()
  const meta = RESULT_META[key] ?? { label: raw || '—', tag: 'default' as const }
  const reason = key === 'failed' && raw.includes(':') ? raw.slice(raw.indexOf(':') + 1).trim() : ''
  return { ...meta, title: reason }
}

const columns: DataTableColumns<AuditEntry> = [
  { title: '时间', key: 'time', minWidth: 140 },
  {
    title: '操作者',
    key: 'actor',
    minWidth: 130,
    render: (row) =>
      h('span', { class: 'actor-cell' }, [
        h(NTag, { type: actorMeta[row.actorType].tag, size: 'small', bordered: false }, { default: () => actorMeta[row.actorType].label }),
        h('span', row.actor),
      ]),
  },
  { title: '操作', key: 'action', minWidth: 160, render: (row) => h('code', { class: 'cell-code' }, row.action) },
  { title: '参数摘要', key: 'params', minWidth: 200, ellipsis: { tooltip: true } },
  { title: '耗时', key: 'latency', width: 90, render: (row) => latency(row) },
  {
    title: '结果',
    key: 'result',
    width: 90,
    render: (row) => {
      const meta = resultMeta(row.result)
      return h(
        NTag,
        { type: meta.tag, size: 'small', bordered: false, title: meta.title },
        { default: () => meta.label },
      )
    },
  },
]
</script>

<template>
  <div class="view">
    <SectionHeader title="审计日志" desc="AI 与 PAT 的每次工具调用；用户操作直接写库，无耗时记录。">
      <NSelect v-model:value="filterType" :options="typeOptions" class="type-filter" />
    </SectionHeader>

    <NCard size="small" class="table-card">
      <NDataTable
        :columns="columns"
        :data="filtered"
        flex-height
        style="flex: 1; min-height: 0"
        :scroll-x="820"
        :bordered="false"
      />
      <!-- 加载更多：全量永久保留，只能分批取；同时给出「已载/总数」让管理员知道还有多少 -->
      <div v-if="entries.length > 0" class="load-more">
        <span class="load-more-hint">已载 {{ entries.length }} / {{ total }} 条</span>
        <NButton v-if="hasMore" size="small" quaternary :disabled="loadingMore" @click="loadMore">
          {{ loadingMore ? '加载中…' : '加载更多' }}
        </NButton>
      </div>
    </NCard>
  </div>
</template>

<style scoped>
.view {
  display: flex;
  flex-direction: column;
  height: 100%;
  gap: var(--space-5);
}

.table-card {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.type-filter {
  width: var(--control-sm);
}

.load-more {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-3);
  margin-top: var(--space-3);
}

.load-more-hint {
  font-size: var(--text-sm);
  color: var(--color-ink-secondary);
}

.actor-cell {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
}

.table-card :deep(.cell-code) {
  font-family: var(--font-mono);
  font-size: var(--text-sm);
}
</style>
