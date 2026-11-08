<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { NButton, NCard, NTag } from 'naive-ui'
import { RefreshCw } from 'lucide-vue-next'
import { dashboardApi } from '@/api'
import type { DashboardStat, ActivityItem, AiUsagePoint, AlertItem } from '@/api/dashboard'
import StatCard from '@/components/StatCard.vue'
import { icon } from '@/lib/lucide-map'

const stats = ref<DashboardStat[]>([])
const activity = ref<ActivityItem[]>([])
const usage = ref<AiUsagePoint[]>([])
const alerts = ref<AlertItem[]>([])
const loading = ref(false)
// 四块数据各自可能失败：分别记录，失败只影响所在区块的提示，不把整屏打成空白
const failed = ref({ stats: false, activity: false, usage: false, alerts: false })

const totalCalls = computed(() => usage.value.reduce((s, p) => s + p.calls, 0))
const totalTokens = computed(() => usage.value.reduce((s, p) => s + p.tokens, 0))

// 平均延迟：按调用量加权（仅统计有延迟数据的日期）
const avgLatency = computed(() => {
  const withLatency = usage.value.filter((p) => p.avgLatencyMs !== null && p.calls > 0)
  const calls = withLatency.reduce((s, p) => s + p.calls, 0)
  if (calls === 0) return '—'
  const ms = Math.round(withLatency.reduce((s, p) => s + p.calls * (p.avgLatencyMs ?? 0), 0) / calls)
  return ms >= 1000 ? (ms / 1000).toFixed(1) + 's' : ms + 'ms'
})

// 柱状图：按 calls 归一化高度，title 提示具体数值（几何高度是计算值，走 :style）
const maxCalls = computed(() => Math.max(...usage.value.map((p) => p.calls), 1))

function barHeight(p: AiUsagePoint): number {
  return Math.round((p.calls / maxCalls.value) * 120)
}

async function load() {
  loading.value = true
  // 四块数据互不依赖：任一失败只清空自己那块并提示，其余区块照常渲染
  const [s, a, u, al] = await Promise.allSettled([
    dashboardApi.getDashboardStats(),
    dashboardApi.getRecentActivity(),
    dashboardApi.getAiUsage(),
    dashboardApi.getAlerts(),
  ])
  stats.value = s.status === 'fulfilled' ? s.value : []
  activity.value = a.status === 'fulfilled' ? a.value : []
  usage.value = u.status === 'fulfilled' ? u.value.items : []
  alerts.value = al.status === 'fulfilled' ? al.value.items : []
  failed.value = {
    stats: s.status === 'rejected',
    activity: a.status === 'rejected',
    usage: u.status === 'rejected',
    alerts: al.status === 'rejected',
  }
  loading.value = false
}

onMounted(load)
</script>

<template>
  <div class="view">
    <p v-if="failed.stats" class="empty">统计数据加载失败，可刷新重试</p>
    <div v-else class="stats-grid">
      <StatCard v-for="s in stats" :key="s.label" :icon="icon(s.icon)" :label="s.label" :value="s.value">
        {{ s.change }}
      </StatCard>
    </div>

    <div class="grid-2">
      <NCard title="AI 调用统计（近 14 天）" size="small">
        <p v-if="failed.usage" class="empty">AI 调用统计加载失败，可刷新重试</p>
        <template v-else>
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
        </template>
      </NCard>

      <NCard title="最近活动" size="small">
        <template #header-extra>
          <NButton size="small" quaternary @click="load">
            <template #icon>
              <RefreshCw :size="14" :stroke-width="1.8" />
            </template>
            刷新
          </NButton>
        </template>
        <p v-if="failed.activity" class="empty">活动记录加载失败，可刷新重试</p>
        <p v-else-if="activity.length === 0" class="empty">{{ loading ? '加载中…' : '暂无活动记录' }}</p>
        <ul v-else class="activity-list">
          <li v-for="(a, i) in activity" :key="i" class="activity-item">
            <div class="activity-icon">
              <component :is="icon(a.icon)" :size="16" :stroke-width="1.8" />
            </div>
            <div class="activity-content">
              <div class="activity-text">{{ a.text }}</div>
              <div class="activity-time">{{ a.time }}</div>
            </div>
          </li>
        </ul>
      </NCard>
    </div>

    <NCard title="异常告警" size="small">
      <p v-if="failed.alerts" class="empty">告警加载失败，可刷新重试</p>
      <p v-else-if="alerts.length === 0" class="empty">{{ loading ? '加载中…' : '暂无告警，一切正常' }}</p>
      <ul v-else class="alert-list">
        <li v-for="al in alerts" :key="`${al.level}-${al.id}`" class="alert-item">
          <NTag :type="al.level === 'error' ? 'error' : 'warning'" size="small" :bordered="false">
            {{ al.level === 'error' ? '错误' : '警告' }}
          </NTag>
          <span class="alert-message">{{ al.message }}</span>
          <span class="alert-time">{{ al.time }}</span>
        </li>
      </ul>
    </NCard>
  </div>
</template>

<style scoped>
.view {
  display: flex;
  flex-direction: column;
  gap: var(--space-5);
}

.stats-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: var(--space-4);
}

.grid-2 {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: var(--space-5);
}

.empty {
  padding: var(--space-6) 0;
  text-align: center;
  font-size: var(--text-base);
  color: var(--color-ink-tertiary);
}

.usage-summary {
  display: flex;
  gap: var(--space-6);
  font-size: var(--text-base);
  color: var(--color-ink-secondary);
  margin-bottom: var(--space-3);
}

.usage-summary b {
  color: var(--color-ink);
  font-weight: 600;
}

.usage-chart {
  display: flex;
  align-items: flex-end;
  gap: var(--space-2);
  height: 150px;
}

.usage-col {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: flex-end;
  gap: var(--space-1);
  min-width: 0;
}

.usage-bar {
  width: 100%;
  max-width: var(--space-7);
  min-height: var(--space-1);
  border-radius: var(--radius-xs);
  background: var(--color-ink);
  transition: height var(--transition);
}

.usage-bar:hover {
  background: var(--color-primary-hover);
}

.usage-date {
  font-size: var(--text-xs);
  color: var(--color-ink-tertiary);
  white-space: nowrap;
}

.activity-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
}

.activity-item {
  display: flex;
  align-items: flex-start;
  gap: var(--space-3);
  padding: var(--space-3) 0;
  border-bottom: 1px solid var(--color-border);
}

.activity-item:last-child {
  border-bottom: none;
}

.activity-icon {
  width: var(--space-6);
  height: var(--space-6);
  border-radius: var(--radius-xl);
  background: var(--color-subtle);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  color: var(--color-ink-secondary);
}

.activity-text {
  font-size: var(--text-base);
  color: var(--color-ink);
}

.activity-time {
  margin-top: var(--space-1);
  font-size: var(--text-sm);
  color: var(--color-ink-tertiary);
}

.alert-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
}

.alert-item {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-3) 0;
  border-bottom: 1px solid var(--color-border);
}

.alert-item:last-child {
  border-bottom: none;
}

.alert-message {
  flex: 1;
  font-size: var(--text-base);
  color: var(--color-ink);
}

.alert-time {
  font-size: var(--text-sm);
  color: var(--color-ink-tertiary);
  white-space: nowrap;
}

@media (max-width: 1024px) {
  .stats-grid {
    grid-template-columns: repeat(2, 1fr);
  }

  .grid-2 {
    grid-template-columns: 1fr;
  }
}
</style>
