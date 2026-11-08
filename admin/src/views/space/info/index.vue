<script setup lang="ts">
/** 空间管理 · 空间信息：单空间名称 / 签名 / 在一起日期（含在一起天数统计）。 */
import { onMounted, ref } from 'vue'
import { NButton, NCard, NForm, NFormItem, NInput } from 'naive-ui'
import { spaceApi } from '@/api'
import { errMsg } from '@/lib/http'
import type { SpaceInfo } from '@/api/space'
import { toast } from '@/lib/toast'

const space = ref<SpaceInfo | null>(null)
const spaceForm = ref<SpaceInfo>({ name: '', signature: '', love_start_date: '' })
const editing = ref(false)
const loading = ref(false)
const saving = ref(false)

function daysTogether(): string {
  if (!space.value?.love_start_date) return '—'
  const start = new Date(space.value.love_start_date)
  const diff = Math.floor((Date.now() - start.getTime()) / 86400000)
  return diff >= 0 ? String(diff) : '—'
}

async function load() {
  loading.value = true
  try {
    space.value = await spaceApi.getSpace()
    if (space.value) spaceForm.value = { ...space.value }
  } catch (e) {
    toast.error(errMsg(e, '空间信息加载失败'))
  } finally {
    loading.value = false
  }
}

async function saveSpace() {
  saving.value = true
  try {
    await spaceApi.saveSpace(spaceForm.value)
    space.value = { ...spaceForm.value }
    editing.value = false
    toast.success('空间信息已保存')
  } catch (e) {
    toast.error(errMsg(e, '保存失败'))
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="view">
    <NCard size="small">
      <template #header>
        <div class="pane-header">
          <span>空间信息</span>
          <div class="pane-actions">
            <NButton v-if="!editing" size="small" quaternary @click="editing = true">编辑</NButton>
            <template v-else>
              <NButton size="small" @click="editing = false">取消</NButton>
              <NButton size="small" type="primary" :loading="saving" @click="saveSpace">保存</NButton>
            </template>
          </div>
        </div>
      </template>
      <div v-if="space && !editing" class="space-info">
        <div class="space-row"><span class="space-label">空间名称</span><b>{{ space.name }}</b></div>
        <div class="space-row"><span class="space-label">个性签名</span><b>{{ space.signature || '—' }}</b></div>
        <div class="space-row"><span class="space-label">在一起日期</span><b>{{ space.love_start_date }}</b></div>
        <div class="space-row"><span class="space-label">在一起天数</span><b>{{ daysTogether() }} 天</b></div>
      </div>
      <NForm v-else-if="editing" label-placement="top" size="medium" class="space-form">
        <NFormItem label="空间名称"><NInput v-model:value="spaceForm.name" /></NFormItem>
        <NFormItem label="个性签名"><NInput v-model:value="spaceForm.signature" /></NFormItem>
        <NFormItem label="在一起日期"><NInput v-model:value="spaceForm.love_start_date" type="text" placeholder="YYYY-MM-DD" /></NFormItem>
      </NForm>
      <p v-else class="empty">空间信息加载中…</p>
    </NCard>
  </div>
</template>

<style scoped>
.view {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.pane-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
}

.pane-actions {
  display: flex;
  gap: var(--space-2);
}

.space-info {
  display: flex;
  flex-direction: column;
}

.space-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: var(--space-3) 0;
  border-bottom: 1px solid var(--color-border);
}

.space-row:last-child {
  border-bottom: none;
}

.space-label {
  font-size: var(--text-sm);
  color: var(--color-ink-tertiary);
}

.space-form {
  margin-top: var(--space-3);
}

.empty {
  padding: var(--space-5) 0;
  font-size: var(--text-base);
  color: var(--color-ink-tertiary);
}
</style>
