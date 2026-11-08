<script setup lang="ts">
import { onMounted, h, ref } from 'vue'
import { NButton, NCard, NDataTable, NForm, NFormItem, NInput, NModal, NSelect, NTag, type DataTableColumns } from 'naive-ui'
import { Plus } from 'lucide-vue-next'
import { providersApi, stylesApi } from '@/api'
import type { Style, StylesData } from '@/api/styles'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { errMsg } from '@/lib/http'
import { toast } from '@/lib/toast'
import { useConfirmAction } from '@/lib/confirm'

// ---------- System Prompt（全局主干模板） ----------
const prompt = ref('')
const promptSaving = ref(false)

async function loadPrompt() {
  try {
    prompt.value = (await providersApi.getSystemPrompt()).content
  } catch (e) {
    toast.error(errMsg(e, 'System Prompt 加载失败'))
  }
}

async function savePrompt() {
  promptSaving.value = true
  try {
    await providersApi.saveSystemPrompt(prompt.value)
    toast.success('System Prompt 已保存')
  } catch (e) {
    toast.error(errMsg(e, '保存失败'))
  } finally {
    promptSaving.value = false
  }
}

// ---------- 输出风格库 ----------
const stylesData = ref<StylesData>({ items: [], default_style_id: 0 })
const defaultStyleId = ref(0)
const showStyleForm = ref(false)
const styleSaving = ref(false)
const styleEditingId = ref<number | null>(null)
const sName = ref('')
const sPrompt = ref('')

async function loadStyles() {
  try {
    stylesData.value = await stylesApi.listStyles()
    defaultStyleId.value = stylesData.value.default_style_id
  } catch (e) {
    toast.error(errMsg(e, '风格库加载失败'))
  }
}

async function onDefaultStyleChange() {
  try {
    stylesData.value = await stylesApi.setDefaultStyle(defaultStyleId.value)
    defaultStyleId.value = stylesData.value.default_style_id
  } catch (e) {
    toast.error(errMsg(e, '设置默认风格失败'))
    await loadStyles()
  }
}

function openCreateStyle() {
  styleEditingId.value = null
  sName.value = ''
  sPrompt.value = ''
  showStyleForm.value = true
}

function openEditStyle(s: Style) {
  if (s.builtin) return
  styleEditingId.value = s.id
  sName.value = s.name
  sPrompt.value = s.prompt
  showStyleForm.value = true
}

async function submitStyle() {
  if (styleSaving.value) return
  if (!sName.value.trim()) {
    toast.error('风格名称必填')
    return
  }
  if (!sPrompt.value.trim()) {
    toast.error('风格提示词必填')
    return
  }
  styleSaving.value = true
  try {
    if (styleEditingId.value === null) {
      await stylesApi.createStyle({ name: sName.value.trim(), prompt: sPrompt.value.trim() })
      toast.success('风格已创建')
    } else {
      await stylesApi.updateStyle(styleEditingId.value, { name: sName.value.trim(), prompt: sPrompt.value.trim() })
      toast.success('风格已保存')
    }
    showStyleForm.value = false
    await loadStyles()
  } catch (e) {
    toast.error(errMsg(e, '保存失败'))
  } finally {
    styleSaving.value = false
  }
}

// 删除走确认弹窗（不用原生 confirm）；结果用全局 toast 反馈
const remove = useConfirmAction<Style>('删除失败')

async function runRemove(s: Style) {
  await stylesApi.deleteStyle(s.id)
  toast.success(`已删除风格「${s.name}」`)
  await loadStyles()
}

onMounted(() => {
  loadPrompt()
  loadStyles()
})

const columns: DataTableColumns<Style> = [
  {
    title: '类型',
    key: 'builtin',
    width: 90,
    render: (row) =>
      h(NTag, { type: row.builtin ? 'default' : 'info', size: 'small', bordered: false }, { default: () => (row.builtin ? '内置' : '自定义') }),
  },
  { title: '名称', key: 'name', width: 110 },
  { title: '提示词', key: 'prompt', minWidth: 240, ellipsis: { tooltip: true } },
  {
    title: '',
    key: 'actions',
    width: 130,
    align: 'right',
    render: (row) =>
      row.builtin
        ? null
        : h('div', { class: 'ops-cell' }, [
            h(NButton, { size: 'small', quaternary: true, onClick: () => openEditStyle(row) }, { default: () => '编辑' }),
            h(NButton, { size: 'small', type: 'error', secondary: true, onClick: () => (remove.target.value = row) }, { default: () => '删除' }),
          ]),
  },
]
</script>

<template>
  <div class="view">
    <NCard title="全局 System Prompt 模板" size="small">
      <NInput
        v-model:value="prompt"
        type="textarea"
        :rows="8"
        placeholder="人设与行为边界主干；输出风格的语气片段会拼接在它之后。"
      />
      <div class="prompt-actions">
        <span class="hint-inline">保存后对新会话生效；「输出风格」的语气片段会拼在这段主干之后。</span>
        <NButton type="primary" :loading="promptSaving" @click="savePrompt">保存模板</NButton>
      </div>
    </NCard>

    <NCard size="small" class="table-card">
      <template #header>
        <div class="styles-header">
          <span>风格库（内置固化 + 自定义）</span>
          <div class="styles-controls">
            <label class="default-label">
              默认风格
              <NSelect
                v-model:value="defaultStyleId"
                :options="stylesData.items.map((s) => ({ label: s.name, value: s.id }))"
                class="default-select"
                size="small"
                @update:value="onDefaultStyleChange"
              />
            </label>
            <NButton size="small" type="primary" @click="openCreateStyle">
              <template #icon>
                <Plus :size="14" :stroke-width="1.8" />
              </template>
              添加自定义风格
            </NButton>
          </div>
        </div>
      </template>
      <NDataTable
        :columns="columns"
        :data="stylesData.items"
        flex-height
        style="flex: 1; min-height: 0"
        :scroll-x="640"
        :bordered="false"
        :row-key="(row: Style) => row.id"
      />
      <p class="hint-block">
        风格提示词拼接在 System Prompt 主干之后（只换语气、不动主干）；内置风格完全固化；全局默认风格在 APP 端未选择风格时生效。
      </p>
    </NCard>

    <NModal
      v-model:show="showStyleForm"
      preset="card"
      :title="styleEditingId === null ? '添加自定义风格' : '编辑自定义风格'"
      :style="{ width: '480px' }"
      :bordered="true"
    >
      <NForm label-placement="top" size="medium">
        <NFormItem label="风格名称" required>
          <NInput v-model:value="sName" placeholder="如 文艺" />
        </NFormItem>
        <NFormItem label="风格提示词（说话语气片段，拼接在主干之后）" required>
          <NInput
            v-model:value="sPrompt"
            type="textarea"
            :rows="5"
            placeholder="如：用文艺的笔触描述生活，善用比喻，语气温柔含蓄。"
          />
        </NFormItem>
      </NForm>
      <template #footer>
        <div class="modal-actions">
          <NButton @click="showStyleForm = false">取消</NButton>
          <NButton type="primary" :loading="styleSaving" @click="submitStyle">保存</NButton>
        </div>
      </template>
    </NModal>

    <ConfirmDialog
      v-if="remove.target.value"
      title="删除自定义风格"
      :message="`删除自定义风格「${remove.target.value.name}」？删除后不可恢复。`"
      confirm-label="删除"
      :busy="remove.busy.value"
      @close="remove.target.value = null"
      @confirm="remove.confirm(runRemove)"
    />
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

.prompt-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  margin-top: var(--space-3);
}

.hint-inline {
  font-size: var(--text-sm);
  color: var(--color-ink-tertiary);
}

.styles-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.styles-controls {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

.default-label {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--text-sm);
  color: var(--color-ink-secondary);
}

.default-select {
  width: var(--control-sm);
}

.hint-block {
  margin: var(--space-3) 0 0;
  font-size: var(--text-sm);
  color: var(--color-ink-tertiary);
}

.table-card :deep(.ops-cell) {
  display: inline-flex;
  gap: var(--space-2);
}
</style>
