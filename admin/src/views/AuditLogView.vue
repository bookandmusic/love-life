<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { auditApi } from '../api'
import type { AuditEntry, AuditActorType } from '../api/audit'
import AppCard from '../components/AppCard.vue'
import AppBadge from '../components/AppBadge.vue'
import AppIcon from '../components/AppIcon.vue'
import { errMsg } from '../lib/http'
import { toast } from '../lib/toast'

const entries = ref<AuditEntry[]>([])
const filterType = ref<'全部' | AuditActorType>('全部')

const filters: Array<'全部' | AuditActorType> = ['全部', 'user', 'ai', 'pat']
const actorMeta: Record<AuditActorType, { label: string; variant: 'info' | 'warning' | 'neutral' }> = {
  user: { label: '用户', variant: 'info' },
  ai: { label: 'AI', variant: 'warning' },
  pat: { label: 'PAT', variant: 'neutral' },
}

onMounted(async () => {
  try {
    entries.value = await auditApi.listAudit()
  } catch (e) {
    toast.error(errMsg(e, '审计日志加载失败'))
  }
})

const filtered = computed(() =>
  entries.value.filter((e) => filterType.value === '全部' || e.actorType === filterType.value),
)

// latencyMs 仅 ai/pat 调用有值（模型响应耗时）；user 直接写库为 null
function latency(e: AuditEntry): string {
  if (e.latencyMs === null) return '—'
  return e.latencyMs >= 1000 ? (e.latencyMs / 1000).toFixed(1) + 's' : e.latencyMs + 'ms'
}

// result 是四态而非布尔：出卡待确认与用户主动取消都不是失败，报成「失败」是误报。
// 失败值可带原因（存成 `failed: 原因`），用 title 展示。
const RESULT_META: Record<string, { label: string; variant: 'success' | 'error' | 'warning' | 'neutral'; icon: string }> = {
  success: { label: '成功', variant: 'success', icon: 'check-circle' },
  failed: { label: '失败', variant: 'error', icon: 'alert-circle' },
  pending: { label: '待确认', variant: 'warning', icon: 'alert-triangle' },
  cancelled: { label: '已取消', variant: 'neutral', icon: 'x' },
}

const resultMeta = (raw: string) => {
  const key = raw.split(':')[0]!.trim()
  const meta = RESULT_META[key] ?? { label: raw || '—', variant: 'neutral' as const, icon: 'info' }
  const reason = key === 'failed' && raw.includes(':') ? raw.slice(raw.indexOf(':') + 1).trim() : ''
  return { ...meta, title: reason }
}
</script>

<template>
  <div class="page">
    <div class="page-section-header">
      <h2 class="page-section-title">审计日志</h2>
      <div class="flex gap-3 items-center">
        <select v-model="filterType" class="form-select" style="width: auto">
          <option v-for="f in filters" :key="f" :value="f">
            {{ f === '全部' ? '全部类型' : actorMeta[f].label + '操作' }}
          </option>
        </select>
      </div>
    </div>


    <AppCard class="card-fill">
      <div v-if="filtered.length === 0" class="empty-state">
        <AppIcon name="file-text" :size="48" />
        <div class="empty-state-title">暂无日志</div>
      </div>
      <div v-else class="table-wrapper">
        <table>
          <thead>
            <tr><th>时间</th><th>操作者</th><th>操作</th><th>参数摘要</th><th>耗时</th><th>结果</th></tr>
          </thead>
          <tbody>
            <tr v-for="(e, i) in filtered" :key="i">
              <td class="text-secondary">{{ e.time }}</td>
              <td>
                <AppBadge :variant="actorMeta[e.actorType].variant">{{ actorMeta[e.actorType].label }}</AppBadge>
                {{ e.actor }}
              </td>
              <td><code>{{ e.action }}</code></td>
              <td class="text-secondary">{{ e.params }}</td>
              <td class="text-secondary">{{ latency(e) }}</td>
              <td>
                <!-- 失败原因（`failed: 原因`）经 attrs 透传到 badge 根节点作 title；icon 走组件自带属性 -->
                <AppBadge :variant="resultMeta(e.result).variant" :icon="resultMeta(e.result).icon" :title="resultMeta(e.result).title">
                  {{ resultMeta(e.result).label }}
                </AppBadge>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </AppCard>
  </div>
</template>
