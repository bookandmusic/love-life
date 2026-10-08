<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { recycleApi, securityApi } from '../api'
import type { RecycleItem, RecycleType, RestoreLogEntry } from '../api/recycle'
import AppCard from '../components/AppCard.vue'
import AppButton from '../components/AppButton.vue'
import AppBadge from '../components/AppBadge.vue'
import AppAlert from '../components/AppAlert.vue'
import AppIcon from '../components/AppIcon.vue'
import AppTabs from '../components/AppTabs.vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import { errMsg } from '../lib/http'
import { toast } from '../lib/toast'
import { useConfirmAction } from '../lib/confirm'

// 恢复目标：单条与批量共用（ids 恒为数组，summary 用于确认文案）
interface RestoreTarget {
  ids: number[]
  summary: string
}

// 两个数据集各占一屏，不上下堆叠（PRODUCT「后台管理页面功能范围（admin）」布局约定）
const tabs = ['已删除内容', '操作日志']
const activeTab = ref('已删除内容')

const items = ref<RecycleItem[]>([])
const restoreLog = ref<RestoreLogEntry[]>([])
const filterType = ref('全部')
const selected = ref<Set<number>>(new Set())

// 保留期限仅用于提示展示，设置入口在「空间管理 · 数据保留」
const retentionDays = ref(30)

async function loadRetention() {
  try {
    retentionDays.value = (await securityApi.getSecuritySettings()).recycleRetentionDays
  } catch {
    // 保留期加载失败不阻塞回收站主功能
  }
}

const types: Array<'全部' | RecycleType> = ['全部', '动态', '评论', '相册', '纪念日', '地图点位']
const typeVariant: Record<RecycleType, 'info' | 'neutral' | 'warning' | 'success' | 'error'> = {
  动态: 'info',
  评论: 'neutral',
  相册: 'warning',
  纪念日: 'error',
  地图点位: 'success',
}

async function load() {
  try {
    const [list, log] = await Promise.all([recycleApi.listRecycle(), recycleApi.listRestoreLog()])
    // 按删除时间排序（倒序）；前端排序兜底，不依赖后端返回顺序
    items.value = [...list].sort((a, b) => b.deletedAt.localeCompare(a.deletedAt))
    restoreLog.value = log.items
    selected.value = new Set()
  } catch (e) {
    toast.error(errMsg(e, '回收站加载失败'))
  }
}

onMounted(() => {
  load()
  loadRetention()
})

const filtered = computed(() =>
  items.value.filter((i) => filterType.value === '全部' || i.type === filterType.value),
)

const allChecked = computed(
  () => filtered.value.length > 0 && filtered.value.every((i) => selected.value.has(i.id)),
)

function toggleAll() {
  if (allChecked.value) {
    selected.value = new Set()
  } else {
    selected.value = new Set(filtered.value.map((i) => i.id))
  }
}

function toggleOne(id: number) {
  const next = new Set(selected.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  selected.value = next
}

// 恢复走确认弹窗（不用原生 confirm）；结果用全局 toast 反馈，操作记录审计日志
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
</script>

<template>
  <div class="page">
    <AppTabs v-model="activeTab" :items="tabs" />

    <AppAlert v-if="activeTab === '已删除内容'" variant="warning" icon="alert-triangle">
      回收站中的内容将保留 {{ retentionDays }} 天，超过期限将被系统自动永久删除，仅管理员可恢复；保留期限可在「空间管理 · 数据保留」中调整。
    </AppAlert>

    <div v-if="activeTab === '已删除内容'" class="page-section-header">
      <h2 class="page-section-title">已删除内容</h2>
      <div class="flex gap-3 items-center">
        <select v-model="filterType" class="form-select" style="width: auto">
          <option v-for="t in types" :key="t" :value="t">{{ t }}</option>
        </select>
        <AppButton variant="primary" icon="refresh-cw" :disabled="selected.size === 0" @click="restoreSelected">
          批量恢复{{ selected.size > 0 ? `（${selected.size}）` : '' }}
        </AppButton>
        <AppButton variant="secondary" icon="refresh-cw" @click="load">刷新</AppButton>
      </div>
    </div>

    <AppCard v-if="activeTab === '已删除内容'" class="card-fill">
      <div v-if="filtered.length === 0" class="empty-state">
        <AppIcon name="trash-2" :size="48" />
        <div class="empty-state-title">回收站是空的</div>
      </div>
      <div v-else class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th style="width: 36px">
                <input type="checkbox" class="form-checkbox" :checked="allChecked" @change="toggleAll" />
              </th>
              <th>内容类型</th><th>内容摘要</th><th>删除时间</th><th>删除者</th><th></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in filtered" :key="item.id">
              <td>
                <input type="checkbox" class="form-checkbox" :checked="selected.has(item.id)" @change="toggleOne(item.id)" />
              </td>
              <td><AppBadge :variant="typeVariant[item.type]">{{ item.type }}</AppBadge></td>
              <td class="text-secondary">{{ item.summary }}</td>
              <td class="text-secondary">{{ item.deletedAt }}</td>
              <td>{{ item.deletedBy }}</td>
              <td class="text-right">
                <AppButton variant="primary" size="sm" icon="refresh-cw" @click="restore(item)">
                  恢复
                </AppButton>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </AppCard>

    <div v-if="activeTab === '操作日志'" class="page-section-header">
      <h2 class="page-section-title">回收站操作日志</h2>
    </div>

    <AppCard v-if="activeTab === '操作日志'" class="card-fill">
      <div v-if="restoreLog.length === 0" class="empty-state">
        <AppIcon name="file-text" :size="40" />
        <div class="empty-state-title">暂无操作记录</div>
      </div>
      <!-- 包装层必须是 <table> 外的一层：<table> 自身不能作滚动容器（表头 sticky 无从参照） -->
      <div v-else class="table-wrapper">
        <table>
          <thead>
            <tr><th>时间</th><th>操作人</th><th>内容摘要</th></tr>
          </thead>
          <tbody>
            <tr v-for="(l, i) in restoreLog" :key="i">
              <td class="text-secondary">{{ l.time }}</td>
              <td>{{ l.operator }}</td>
              <td class="text-secondary">{{ l.summary }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </AppCard>

    <ConfirmDialog
      v-if="restoreAction.target.value"
      title="恢复回收站内容"
      :message="`确认恢复「${restoreAction.target.value.summary}」？该操作会被记录审计日志。`"
      confirm-label="恢复"
      variant="primary"
      :busy="restoreAction.busy.value"
      @close="restoreAction.target.value = null"
      @confirm="restoreAction.confirm(runRestore)"
    />
  </div>
</template>

