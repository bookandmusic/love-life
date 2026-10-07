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
      <div class="table-wrapper">
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
                <AppBadge :variant="e.result === 'success' ? 'success' : 'error'">
                  <AppIcon :name="e.result === 'success' ? 'check-circle' : 'alert-circle'" :size="12" />
                  {{ e.result === 'success' ? '成功' : '失败' }}
                </AppBadge>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </AppCard>
  </div>
</template>
