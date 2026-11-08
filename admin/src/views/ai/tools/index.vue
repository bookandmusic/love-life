<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue'
import { NAlert, NButton, NCard, NDataTable, NModal, NSelect, NSwitch, NTag, type DataTableColumns } from 'naive-ui'
import { Search, Wrench } from 'lucide-vue-next'
import { toolsApi } from '@/api'
import type { McpTool, ToolCategory } from '@/api/tools'
import SectionHeader from '@/components/SectionHeader.vue'
import { errMsg } from '@/lib/http'
import { toast } from '@/lib/toast'

const tools = ref<McpTool[]>([])
const loading = ref(false)
// 类别是筛选维度，不是页面导航：用下拉（与回收站、审计日志同口径），避免与页内 tab 条撞脸
const category = ref('全部')
const search = ref('')
const detail = ref<McpTool | null>(null)
const showDetail = computed({
  get: () => detail.value !== null,
  set: (v) => {
    if (!v) detail.value = null
  },
})

const categories = ['全部', '动态', '相册', '地图', '纪念日', '互动', '空间']
const categoryOptions = categories.map((c) => ({ label: c === '全部' ? '全部类别' : c, value: c }))

const categoryTag: Record<ToolCategory, 'info' | 'warning' | 'success' | 'error' | 'default'> = {
  动态: 'info',
  相册: 'warning',
  地图: 'success',
  纪念日: 'error',
  互动: 'default',
  空间: 'info',
}

onMounted(async () => {
  loading.value = true
  try {
    tools.value = await toolsApi.listTools()
  } catch (e) {
    toast.error(errMsg(e, '工具列表加载失败'))
  } finally {
    loading.value = false
  }
})

const filtered = computed(() =>
  tools.value.filter((t) => {
    const byCat = category.value === '全部' || t.category === category.value
    const bySearch = !search.value || t.name.includes(search.value) || t.description.includes(search.value)
    return byCat && bySearch
  }),
)

// 启停先落服务端，再以响应回填；失败回滚开关
async function toggleTool(t: McpTool, enabled: boolean) {
  const prev = t.enabled
  t.enabled = enabled
  try {
    const updated = await toolsApi.setToolEnabled(t.name, enabled)
    Object.assign(t, updated)
    toast.success(`已${enabled ? '启用' : '停用'}「${t.name}」`)
  } catch (e) {
    t.enabled = prev
    toast.error(errMsg(e, '操作失败'))
  }
}

const schemaEntries = computed(() => Object.entries(detail.value?.schema ?? {}))

const columns: DataTableColumns<McpTool> = [
  { title: '工具名称', key: 'name', minWidth: 200, render: (row) => h('code', { class: 'cell-code' }, row.name) },
  { title: '描述', key: 'description', minWidth: 240, ellipsis: { tooltip: true } },
  {
    title: '类别',
    key: 'category',
    width: 90,
    render: (row) =>
      h(NTag, { type: categoryTag[row.category] ?? 'default', size: 'small', bordered: false }, { default: () => row.category }),
  },
  {
    title: '状态',
    key: 'enabled',
    width: 80,
    render: (row) =>
      h(NSwitch, {
        size: 'small',
        value: row.enabled,
        'onUpdate:value': (v: boolean) => toggleTool(row, v),
      }),
  },
  {
    title: '',
    key: 'actions',
    width: 90,
    align: 'right',
    render: (row) =>
      h(NButton, { size: 'small', quaternary: true, onClick: () => (detail.value = row) }, { default: () => '详情' }),
  },
]
</script>

<template>
  <div class="view">
    <SectionHeader :title="`MCP 工具列表（${tools.length} 个）`" desc="AI 通过 MCP 调用的能力全集；启停即时生效。">
      <NSelect v-model:value="category" :options="categoryOptions" class="filter-select" />
      <NInput v-model:value="search" placeholder="搜索工具…" class="search-input" clearable>
        <template #prefix>
          <Search :size="14" :stroke-width="1.8" />
        </template>
      </NInput>
    </SectionHeader>

    <NCard size="small" class="table-card">
      <NDataTable
        :columns="columns"
        :data="filtered"
        :loading="loading"
        flex-height
        style="flex: 1; min-height: 0"
        :scroll-x="720"
        :bordered="false"
        :row-key="(row: McpTool) => row.name"
      />
    </NCard>

    <NAlert type="warning" :show-icon="false">
      删除类工具（delete_*）仅在 APP 内部使用，不对个人访问令牌（PAT）开放。
    </NAlert>

    <NModal v-model:show="showDetail" preset="card" :title="`工具详情 — ${detail?.name}`" :style="{ width: '480px' }" :bordered="true">
      <div class="detail-body">
        <p class="detail-desc">{{ detail?.description }}</p>
        <div class="detail-tags">
          <NTag :type="detail ? (categoryTag[detail.category] ?? 'default') : 'default'" size="small" :bordered="false">
            {{ detail?.category }}
          </NTag>
          <NTag :type="detail?.enabled ? 'success' : 'default'" size="small" :bordered="false">
            {{ detail?.enabled ? '已启用' : '已停用' }}
          </NTag>
        </div>
        <p v-if="schemaEntries.length === 0" class="detail-empty">该工具无参数。</p>
        <table v-else class="schema-table">
          <thead>
            <tr>
              <th>
                <Wrench :size="12" :stroke-width="1.8" />
                参数
              </th>
              <th>类型与说明</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="[name, desc] in schemaEntries" :key="name">
              <td><code>{{ name }}</code></td>
              <td>{{ desc }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </NModal>
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

.filter-select {
  width: var(--control-sm);
}

.search-input {
  width: var(--control-md);
}

.detail-body {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.detail-desc {
  font-size: var(--text-base);
  color: var(--color-ink-secondary);
}

.detail-tags {
  display: flex;
  gap: var(--space-2);
}

.detail-empty {
  font-size: var(--text-sm);
  color: var(--color-ink-tertiary);
}

.schema-table {
  width: 100%;
  border-collapse: collapse;
}

.schema-table th,
.schema-table td {
  text-align: left;
  padding: var(--space-2) var(--space-3);
  border-bottom: 1px solid var(--color-border);
}

.schema-table th {
  font-size: var(--text-sm);
  color: var(--color-ink-tertiary);
  font-weight: 500;
}

.schema-table td {
  font-size: var(--text-base);
  color: var(--color-ink);
}

.table-card :deep(.cell-code),
.schema-table code {
  font-family: var(--font-mono);
  font-size: var(--text-sm);
}
</style>
