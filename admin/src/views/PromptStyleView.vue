<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { providersApi, stylesApi } from '../api'
import type { Style, StylesData } from '../api/styles'
import AppCard from '../components/AppCard.vue'
import AppButton from '../components/AppButton.vue'
import AppBadge from '../components/AppBadge.vue'
import AppModal from '../components/AppModal.vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import { errMsg } from '../lib/http'
import { toast } from '../lib/toast'
import { useConfirmAction } from '../lib/confirm'


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
  if (!sName.value.trim()) {
    toast.error('风格名称必填')
    return
  }
  if (!sPrompt.value.trim()) {
    toast.error('风格提示词必填')
    return
  }
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
</script>

<template>
  <div class="tab-panel">
    <AppCard title="全局 System Prompt 模板">
      <textarea v-model="prompt" class="form-textarea" rows="8" placeholder="人设与行为边界主干；输出风格的语气片段会拼接在它之后。" />
      <div class="flex gap-2 items-center" style="margin-top: 12px; justify-content: space-between">
        <span class="text-tertiary text-sm">保存后对新会话生效；「输出风格」的语气片段会拼在这段主干之后。</span>
        <AppButton variant="primary" icon="save" :disabled="promptSaving" @click="savePrompt">保存模板</AppButton>
      </div>
    </AppCard>

    <AppCard title="风格库（内置固化 + 自定义）" class="card-fill">
      <template #actions>
        <div class="flex gap-3 items-center">
          <label class="flex gap-2 items-center text-sm">
            默认风格
            <select v-model="defaultStyleId" class="form-select" style="width: auto" @change="onDefaultStyleChange">
              <option v-for="s in stylesData.items" :key="s.id" :value="s.id">{{ s.name }}</option>
            </select>
          </label>
          <AppButton variant="primary" size="sm" icon="plus" @click="openCreateStyle">添加自定义风格</AppButton>
        </div>
      </template>
      <div class="table-wrapper">
        <table>
          <thead>
            <tr><th style="width: 90px">类型</th><th style="width: 100px">名称</th><th>提示词</th><th></th></tr>
          </thead>
          <tbody>
            <tr v-for="s in stylesData.items" :key="s.id">
              <td>
                <AppBadge :variant="s.builtin ? 'neutral' : 'info'">{{ s.builtin ? '内置' : '自定义' }}</AppBadge>
              </td>
              <td>{{ s.name }}</td>
              <td class="text-secondary style-prompt">{{ s.prompt }}</td>
              <td class="text-right">
                <div v-if="!s.builtin" class="flex gap-2" style="justify-content: flex-end">
                  <AppButton variant="secondary" size="sm" icon="edit" @click="openEditStyle(s)">编辑</AppButton>
                  <AppButton variant="danger" size="sm" icon="trash" @click="remove.target.value = s">删除</AppButton>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <p class="form-hint" style="margin-top: 8px">
        风格提示词拼接在 System Prompt 主干之后（只换语气、不动主干）；内置风格完全固化；全局默认风格在 APP 端未选择风格时生效。
      </p>
    </AppCard>

    <AppModal
      v-if="showStyleForm"
      :title="styleEditingId === null ? '添加自定义风格' : '编辑自定义风格'"
      @close="showStyleForm = false"
    >
      <form class="flex" style="flex-direction: column; gap: 14px" @submit.prevent="submitStyle">
        <div class="form-group" style="margin: 0">
          <label class="form-label">风格名称</label>
          <input v-model="sName" class="form-input" placeholder="如 文艺" required />
        </div>
        <div class="form-group" style="margin: 0">
          <label class="form-label">风格提示词（说话语气片段，拼接在主干之后）</label>
          <textarea v-model="sPrompt" class="form-textarea" rows="5" placeholder="如：用文艺的笔触描述生活，善用比喻，语气温柔含蓄。" required />
        </div>
        <div class="flex gap-2" style="justify-content: flex-end">
          <AppButton variant="secondary" icon="x" type="button" @click="showStyleForm = false">取消</AppButton>
          <AppButton variant="primary" icon="save" type="submit">保存</AppButton>
        </div>
      </form>
    </AppModal>

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
.style-prompt {
  max-width: 480px;
  word-break: break-all;
}
</style>
