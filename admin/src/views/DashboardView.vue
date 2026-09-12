<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { dashboardApi } from '../api'
import type { DashboardStat, ActivityItem, AiUsagePoint, AlertItem } from '../api/dashboard'
import AppStatCard from '../components/AppStatCard.vue'
import AppCard from '../components/AppCard.vue'
import AppButton from '../components/AppButton.vue'
import AppBadge from '../components/AppBadge.vue'
import AppIcon from '../components/AppIcon.vue'
import { errMsg } from '../lib/http'
import { toast } from '../lib/toast'

const stats = ref<DashboardStat[]>([])
const activity = ref<ActivityItem[]>([])
const usage = ref<AiUsagePoint[]>([])
const alerts = ref<AlertItem[]>([])

const totalCalls = computed(() => usage.value.reduce((s, p) => s + p.calls, 0))
const totalTokens = computed(() => usage.value.reduce((s, p) => s + p.tokens, 0))

// 平均延迟：按调用量加权（仅统计有延迟数据的日期）
const avgLatency = computed(() => {
  const withLatency = usage.value.filter((p) => p.avgLatencyMs !== null && p.calls > 0)
  const calls = withLatency.reduce((s, p) => s + p.calls, 0)
  if (calls === 0) return '—'
  const ms = Math.round(withLatency.reduce((s, p) => s + p.calls * (p.avgLatencyMs as number), 0) / calls)
  return ms >= 1000 ? (ms / 1000).toFixed(1) + 's' : ms + 'ms'
})

// 纯 SVG 柱状图：按 calls 归一化高度，柱体 title 提示具体数值
const maxCalls = computed(() => Math.max(...usage.value.map((p) => p.calls), 1))

function barHeight(p: AiUsagePoint): number {
  return Math.round((p.calls / maxCalls.value) * 120)
}

async function load() {
  try {
    const [s, a, u, al] = await Promise.all([
      dashboardApi.getDashboardStats(),
      dashboardApi.getRecentActivity(),
      dashboardApi.getAiUsage(),
      dashboardApi.getAlerts(),
    ])
    stats.value = s
    activity.value = a
    usage.value = u.items
    alerts.value = al.items
  } catch (e) {
    toast.error(errMsg(e, '仪表盘加载失败'))
  }
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="stats-grid">
      <AppStatCard v-for="s in stats" :key="s.label" v-bind="s" />
    </div>

    <div class="page-scroll">
      <div class="grid-2">
        <AppCard title="AI 调用统计（近 14 天）">
          <div class="usage-summary">
            <span>调用 <b>{{ totalCalls }}</b> 次</span>
            <span>Token <b>{{ totalTokens.toLocaleString() }}</b></span>
            <span>平均延迟 <b>{{ avgLatency }}</b></span>
          </div>
          <div class="usage-chart">
            <div v-for="p in usage" :key="p.date" class="usage-col">
              <div
                class="usage-bar"
                :style="{ height: barHeight(p) + 'px' }"
                :title="`${p.date} · ${p.calls} 次 · ${p.tokens.toLocaleString()} tokens · 平均延迟 ${p.avgLatencyMs ?? '—'}`"
              />
              <span class="usage-date">{{ p.date }}</span>
            </div>
          </div>
        </AppCard>

        <AppCard title="最近活动">
          <template #actions>
            <AppButton variant="secondary" size="sm" icon="refresh-cw" @click="load">刷新</AppButton>
          </template>
          <ul class="activity-list">
            <li v-for="(a, i) in activity" :key="i" class="activity-item">
              <div class="activity-icon"><AppIcon :name="a.icon" :size="16" /></div>
              <div class="activity-content">
                <div class="activity-text">{{ a.text }}</div>
                <div class="activity-time">{{ a.time }}</div>
              </div>
            </li>
          </ul>
        </AppCard>
      </div>

      <AppCard title="异常告警" class="mt-6">
        <div v-if="alerts.length === 0" class="empty-state">
          <AppIcon name="check-circle" :size="40" />
          <div class="empty-state-title">暂无告警，一切正常</div>
        </div>
        <ul v-else class="alert-list">
          <li v-for="al in alerts" :key="al.id" class="alert-item">
            <AppBadge :variant="al.level === 'error' ? 'error' : 'warning'">
              <AppIcon :name="al.level === 'error' ? 'alert-circle' : 'alert-triangle'" :size="12" />
              {{ al.level === 'error' ? '错误' : '警告' }}
            </AppBadge>
            <span class="alert-message">{{ al.message }}</span>
            <span class="alert-time text-tertiary text-sm">{{ al.time }}</span>
          </li>
        </ul>
      </AppCard>
    </div>
  </div>
</template>

<style scoped>
.usage-summary {
  display: flex;
  gap: 24px;
  font-size: 13px;
  color: var(--color-text-secondary);
  margin-bottom: 12px;
}
.usage-summary b {
  color: var(--color-text-primary);
}
.usage-chart {
  display: flex;
  align-items: flex-end;
  gap: 8px;
  height: 150px;
  padding-top: 4px;
}
.usage-col {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: flex-end;
  gap: 6px;
  min-width: 0;
}
.usage-bar {
  width: 100%;
  max-width: 28px;
  min-height: 3px;
  border-radius: 4px 4px 2px 2px;
  background: var(--color-accent);
  transition: height var(--transition);
}
.usage-bar:hover {
  background: var(--color-accent-hover);
}
.usage-date {
  font-size: 10px;
  color: var(--color-text-tertiary);
  white-space: nowrap;
}
.alert-list {
  display: flex;
  flex-direction: column;
}
.alert-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 0;
  border-bottom: 1px solid var(--color-border);
}
.alert-item:last-child {
  border-bottom: none;
}
.alert-message {
  flex: 1;
  font-size: 13px;
  color: var(--color-text-primary);
}
.alert-time {
  white-space: nowrap;
}
</style>
