<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { toolsApi } from '../api'
import type { McpTool, ToolCategory } from '../api/tools'
import AppCard from '../components/AppCard.vue'
import AppButton from '../components/AppButton.vue'
import AppBadge from '../components/AppBadge.vue'
import AppToggle from '../components/AppToggle.vue'
import AppTabs from '../components/AppTabs.vue'
import AppAlert from '../components/AppAlert.vue'
import AppModal from '../components/AppModal.vue'
import AppIcon from '../components/AppIcon.vue'
import { errMsg } from '../lib/http'
import { toast } from '../lib/toast'

const tools = ref<McpTool[]>([])
const activeTab = ref('全部')
const search = ref('')
const detail = ref<McpTool | null>(null)

const tabs = ['全部', '动态', '相册', '地图', '纪念日', '互动', '空间']
const categoryVariant: Record<ToolCategory, 'info' | 'warning' | 'success' | 'error' | 'neutral'> = {
  动态: 'info',
  相册: 'warning',
  地图: 'success',
  纪念日: 'error',
  互动: 'neutral',
  空间: 'info',
}

onMounted(async () => {
  try {
    tools.value = await toolsApi.listTools()
  } catch (e) {
    toast.error(errMsg(e, '工具列表加载失败'))
  }
})

const filtered = computed(() =>
  tools.value.filter((t) => {
    const byCat = activeTab.value === '全部' || t.category === activeTab.value
    const bySearch =
      !search.value ||
      t.name.includes(search.value) ||
      t.description.includes(search.value)
    return byCat && bySearch
  }),
)

// 启停先落 mock，再以服务端响应回填；失败回滚开关
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
</script>

<template>
  <div class="tab-panel">
    <AppTabs v-model="activeTab" :items="tabs" />


    <div class="page-section-header">
      <h2 class="page-section-title">MCP 工具列表（{{ tools.length }} 个）</h2>
      <div class="search-input" style="width: 220px">
        <AppIcon name="search" :size="16" />
        <input v-model="search" class="form-input" type="text" placeholder="搜索工具…" />
      </div>
    </div>

    <AppCard class="card-fill">
      <div class="table-wrapper">
        <table>
          <thead>
            <tr><th>工具名称</th><th>描述</th><th>类别</th><th>状态</th><th></th></tr>
          </thead>
          <tbody>
            <tr v-for="t in filtered" :key="t.name">
              <td><code>{{ t.name }}</code></td>
              <td class="text-secondary">{{ t.description }}</td>
              <td><AppBadge :variant="categoryVariant[t.category]">{{ t.category }}</AppBadge></td>
              <td>
                <AppToggle :model-value="t.enabled" @update:model-value="(v) => toggleTool(t, v)" />
              </td>
              <td class="text-right">
                <AppButton variant="secondary" size="sm" icon="eye" @click="detail = t">详情</AppButton>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </AppCard>

    <AppAlert variant="warning" icon="alert-triangle">
      删除类工具（delete_*）仅在 APP 内部使用，不对个人访问令牌（PAT）开放。
    </AppAlert>

    <AppModal v-if="detail" :title="`工具详情 — ${detail.name}`" @close="detail = null">
      <div class="flex" style="flex-direction: column; gap: 12px">
        <p class="text-secondary">{{ detail.description }}</p>
        <div class="flex gap-2">
          <AppBadge :variant="categoryVariant[detail.category]">{{ detail.category }}</AppBadge>
          <AppBadge :variant="detail.enabled ? 'success' : 'neutral'">{{ detail.enabled ? '已启用' : '已停用' }}</AppBadge>
        </div>
        <div v-if="schemaEntries.length === 0" class="text-tertiary text-sm">该工具无参数。</div>
        <table v-else class="schema-table">
          <thead>
            <tr><th>参数</th><th>类型与说明</th></tr>
          </thead>
          <tbody>
            <tr v-for="[name, desc] in schemaEntries" :key="name">
              <td><code>{{ name }}</code></td>
              <td class="text-secondary">{{ desc }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </AppModal>
  </div>
</template>

<style scoped>
.schema-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}
.schema-table th,
.schema-table td {
  text-align: left;
  padding: 8px 10px;
  border-bottom: 1px solid var(--color-border);
}
.schema-table th {
  color: var(--color-text-tertiary);
  font-weight: 500;
}
</style>
