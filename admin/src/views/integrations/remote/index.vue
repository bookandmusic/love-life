<script setup lang="ts">
/**
 * 远程访问：APP 内置地址之外的两类可达通道，统一在一个列表里管理。
 * - 绿联通道：服务端持凭据登录绿联换发中转凭据（内置单例，可编辑 / 启停 / 测试 / 删除，删除清除配置回到未配置态）；
 * - HTTPS 地址：直连地址列表（多条），服务端不代理，反代与证书由部署者自理。
 * APP 侧并发探活所有启用通道自动选优。
 */
import { computed, h, onMounted, ref } from 'vue'
import {
  NAlert,
  NButton,
  NCard,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NModal,
  NSelect,
  NSwitch,
  NTag,
  type DataTableColumns
} from 'naive-ui'
import { Plus } from 'lucide-vue-next'
import { saveUglinkConfig } from '@/api/uglink'
import { createBackupAddress, updateBackupAddress } from '@/api/backupaddr'
import SectionHeader from '@/components/SectionHeader.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { errMsg } from '@/lib/http'
import { toast } from '@/lib/toast'
import {
  BUILTIN_WEIGHT,
  DEFAULT_WEIGHT,
  useRemoteChannels,
  type AddrType,
  type RemoteRow
} from '@/hooks/business/remote-channels'

// ---------- 数据 ----------
// 列表加载、探活、启停、删除都在 useRemoteChannels 里；视图只保留表格列与添加/编辑表单
const {
  uglink,
  uglinkLoading,
  items,
  loading,
  rows,
  loadUglink,
  loadList,
  loadAll,
  remove,
  runRemove,
  testHttpsRow,
  testUglinkRow,
  toggleEnabled
} = useRemoteChannels()

onMounted(loadAll)

// ---------- 表格 ----------

const columns: DataTableColumns<RemoteRow> = [
  {
    title: '类型',
    key: 'kind',
    width: 110,
    render: (row) =>
      h(NTag, { type: row.kind === 'uglink' ? 'warning' : 'info', size: 'small', bordered: false }, { default: () => (row.kind === 'uglink' ? '绿联中转' : 'HTTPS 直连') })
  },
  { title: '名称 / 标识', key: 'name', minWidth: 180, ellipsis: { tooltip: true } },
  { title: '地址 / 标识', key: 'addr', minWidth: 200, ellipsis: { tooltip: true } },
  {
    title: '权重',
    key: 'weight',
    width: 80,
    render: (row) => String(row.weight)
  },
  {
    title: '状态',
    key: 'status',
    width: 170,
    render: (row) => {
      const tags = [
        h(NTag, { type: row.enabled ? 'success' : 'default', size: 'small', bordered: false }, { default: () => (row.enabled ? '启用' : '停用') })
      ]
      if (row.kind === 'uglink' && uglink.value.configured) {
        if (row.configError) {
          // 确定性配置错误（权限/凭据）：直说根因，tooltip 给完整原因
          tags.push(
            h(NTag, { type: 'error', size: 'small', bordered: false }, { default: () => '配置错误' })
          )
        } else {
          tags.push(h(NTag, { type: row.loggedIn ? 'success' : 'warning', size: 'small', bordered: false }, { default: () => (row.loggedIn ? '已登录' : '未登录') }))
        }
      }
      return h('span', { class: 'status-cell' }, tags)
    }
  },
  {
    title: '',
    key: 'actions',
    width: 240,
    align: 'right',
    render: (row) => {
      const ops = [
        h(
          NButton,
          row.kind === 'uglink'
            ? { size: 'small', quaternary: true, disabled: !uglink.value.configured, onClick: () => testUglinkRow() }
            : { size: 'small', quaternary: true, onClick: () => testHttpsRow(row.id as string) },
          { default: () => (row.kind === 'uglink' ? '测试连接' : '测试连通') }
        ),
        h(NButton, { size: 'small', quaternary: true, onClick: () => openEdit(row.id) }, { default: () => '编辑' })
      ]
      ops.push(
        h(NButton, { size: 'small', quaternary: true, onClick: () => toggleEnabled(row.id) }, { default: () => (row.enabled ? '停用' : '启用') })
      )
      ops.push(
        h(NButton, { size: 'small', type: 'error', secondary: true, onClick: () => (remove.target.value = row.id) }, { default: () => '删除' })
      )
      return h('div', { class: 'ops-cell' }, ops)
    }
  }
]

// ---------- 添加 / 编辑弹窗 ----------
const showForm = ref(false)
const editingKind = ref<AddrType>('https')
// 编辑中的行 id（https 行 = 备用地址 id；绿联 = 'uglink'）；null = 新增
const editingId = ref<string | null>(null)
const formEnabled = ref(true)
const formWeight = ref<number>(DEFAULT_WEIGHT)

/**
 * 已被占用的权重 → 占用者名。**唯一性在这里拦**（用户裁定：添加时就不该允许重复，
 * 而不是提交后由服务端报错）。正在编辑的那条要排除，否则改自己会被自己挡住。
 * 绿联未启用时不占用权重（它不参与选路）。
 */
const takenWeights = computed<Map<number, string>>(() => {
  const map = new Map<number, string>()
  const cfg = uglink.value
  if (cfg.configured && cfg.enabled && editingId.value !== 'uglink') {
    map.set(cfg.weight ?? DEFAULT_WEIGHT, '绿联通道')
  }
  for (const b of items.value) {
    if (!b.enabled || (editingId.value !== null && b.id === editingId.value)) continue
    map.set(b.weight ?? DEFAULT_WEIGHT, b.name || b.url)
  }
  return map
})

/** 表单里的权重问题（空串 = 可提交）。逐条给出可操作的说明，不做「保存失败」的模糊提示。 */
const weightError = computed<string>(() => {
  const w = formWeight.value
  if (w === null || w === undefined) return '请填写权重'
  if (!Number.isInteger(w)) return '权重需为整数'
  if (w < 1 || w > 999) return '权重需在 1–999 之间'
  if (w === BUILTIN_WEIGHT) return `${BUILTIN_WEIGHT} 是内置地址的权重，请换一个`
  const owner = takenWeights.value.get(w)
  if (owner) return `权重 ${w} 已被「${owner}」占用，请换一个`
  return ''
})

// https 字段
const formName = ref('')
const formUrl = ref('')
// 绿联字段（密码只写不读：留空 = 沿用）
const formUglinkId = ref('')
const formPort = ref<number | null>(8080)
const formUsername = ref('')
const formPassword = ref('')
const saving = ref(false)

const typeOptions = [
  { label: 'HTTPS 直连地址', value: 'https' },
  { label: '绿联中转通道', value: 'uglink' }
]

function openCreate(kind: AddrType) {
  editingId.value = null
  editingKind.value = kind
  formEnabled.value = true
  formWeight.value = DEFAULT_WEIGHT
  formName.value = ''
  formUrl.value = ''
  formUglinkId.value = ''
  formPort.value = 8080
  formUsername.value = ''
  formPassword.value = ''
  showForm.value = true
}

function openEdit(id: string) {
  editingId.value = id
  if (id === 'uglink') {
    const cfg = uglink.value
    editingKind.value = 'uglink'
    formUglinkId.value = cfg.uglink_id ?? ''
    formPort.value = cfg.port ?? 8080
    formUsername.value = cfg.username ?? ''
    formPassword.value = ''
    formEnabled.value = cfg.enabled ?? false
    formWeight.value = cfg.weight ?? DEFAULT_WEIGHT
    showForm.value = true
    return
  }
  const item = items.value.find((b) => b.id === id)
  if (!item) return
  editingKind.value = 'https'
  formName.value = item.name
  formUrl.value = item.url
  formEnabled.value = item.enabled
  formWeight.value = item.weight ?? DEFAULT_WEIGHT
  showForm.value = true
}

async function submit() {
  // 权重重复/越界在提交前就拦住（后端只做范围兜底，不负责去重）
  if (weightError.value) {
    toast.error(weightError.value)
    return
  }
  saving.value = true
  try {
    if (editingKind.value === 'uglink') {
      if (!formUglinkId.value.trim()) {
        toast.error('UGREENlink ID 必填')
        return
      }
      await saveUglinkConfig({
        uglink_id: formUglinkId.value.trim(),
        port: formPort.value ?? 8080,
        username: formUsername.value.trim(),
        password: formPassword.value || undefined,
        enabled: formEnabled.value,
        weight: formWeight.value
      })
      toast.success('绿联通道已保存')
      await loadUglink()
    } else {
      if (!formUrl.value.trim()) {
        toast.error('HTTPS 地址必填')
        return
      }
      const payload = {
        name: formName.value.trim(),
        url: formUrl.value.trim(),
        enabled: formEnabled.value,
        weight: formWeight.value
      }
      if (editingId.value) {
        await updateBackupAddress(editingId.value, payload)
        toast.success('已更新')
      } else {
        await createBackupAddress(payload)
        toast.success('已添加')
      }
      await loadList()
    }
    showForm.value = false
  } catch (e) {
    toast.error(errMsg(e, '保存失败'))
  } finally {
    saving.value = false
  }
}

const modalTitle = computed(() => (editingId.value ? '编辑地址' : '添加地址'))
</script>

<template>
  <div class="view">
    <SectionHeader title="远程访问" desc="APP 内置地址之外的两类可达通道：绿联中转（服务端持凭据换发）与 HTTPS 直连地址（服务端不代理）。APP 按权重降序逐个探活，取第一条通的（内置地址恒 100）。">
      <NButton type="primary" @click="openCreate('https')">
        <template #icon>
          <Plus :size="15" :stroke-width="1.8" />
        </template>
        添加地址
      </NButton>
    </SectionHeader>

    <NAlert type="warning" :show-icon="false">
      HTTPS 直连地址的证书须为<strong>系统信任的</strong>（如 Let's Encrypt），<strong>自签名证书不支持</strong>；反代、证书、DNS 由部署者自理。绿联通道由服务端完成握手，APP 不接触密码。
    </NAlert>

    <NCard size="small" class="table-card">
      <NDataTable
        :columns="columns"
        :data="rows"
        :loading="loading || uglinkLoading"
        flex-height
        style="flex: 1; min-height: 0"
        :scroll-x="780"
        :bordered="false"
        :row-key="(row: RemoteRow) => row.id"
      />
    </NCard>

    <NModal v-model:show="showForm" preset="card" :title="modalTitle" :style="{ width: '520px' }" :bordered="true">
      <NForm label-placement="top" size="medium">
        <NFormItem label="类型">
          <NSelect v-model:value="editingKind" :options="typeOptions" :disabled="!!editingId" />
        </NFormItem>

        <template v-if="editingKind === 'https'">
          <NFormItem label="名称（可选）">
            <NInput v-model:value="formName" placeholder="如 我的域名" />
          </NFormItem>
          <NFormItem label="HTTPS 地址" required>
            <NInput v-model:value="formUrl" placeholder="https://my.example.com" :input-props="{ spellcheck: false }" />
          </NFormItem>
        </template>

        <template v-else>
          <NFormItem label="UGREENlink ID" required>
            <NInput v-model:value="formUglinkId" placeholder="如 mynas123" :input-props="{ spellcheck: false }" />
          </NFormItem>
          <NFormItem label="服务端口">
            <NInputNumber v-model:value="formPort" placeholder="8080" class="port-input" />
          </NFormItem>
          <NFormItem label="绿联账号">
            <NInput v-model:value="formUsername" :input-props="{ autocomplete: 'off', spellcheck: false }" />
          </NFormItem>
          <NFormItem>
            <template #label>
              密码
              <NTag v-if="uglink.has_password" size="small" :bordered="false" class="pwd-note">已设置，留空表示不修改</NTag>
            </template>
            <NInput
              v-model:value="formPassword"
              type="password"
              show-password-on="click"
              :input-props="{ autocomplete: 'new-password' }"
              :placeholder="uglink.has_password ? '••••••••' : '首次启用必填'"
            />
          </NFormItem>
        </template>

        <NFormItem label="选路权重" :validation-status="weightError ? 'error' : undefined" :feedback="weightError || undefined">
          <NInputNumber v-model:value="formWeight" :min="1" :max="999" class="port-input" />
        </NFormItem>
        <p class="hint-block">
          <strong>数字越大越优先</strong>（1–999）。APP 按权重降序逐个探活，取第一条通的：填
          &gt;{{ BUILTIN_WEIGHT }} 优先于内置地址，填 &lt;{{ BUILTIN_WEIGHT }} 做兜底。
          {{ BUILTIN_WEIGHT }} 由内置地址占用，且各通道权重不可重复。
          <template v-if="takenWeights.size">
            已占用：{{ Array.from(takenWeights.entries()).map(([w, n]) => `${w}（${n}）`).join('、') }}、{{ BUILTIN_WEIGHT }}（内置）
          </template>
        </p>

        <div class="toggle-row">
          <span class="toggle-label">启用</span>
          <NSwitch v-model:value="formEnabled" />
        </div>
      </NForm>
      <template #footer>
        <div class="modal-actions">
          <NButton @click="showForm = false">取消</NButton>
          <NButton type="primary" :loading="saving" :disabled="!!weightError" @click="submit">{{ editingId ? '保存修改' : '添加' }}</NButton>
        </div>
      </template>
    </NModal>

    <ConfirmDialog
      v-if="remove.target.value"
      :title="remove.target.value === 'uglink' ? '删除绿联通道' : '删除地址'"
      :message="
        remove.target.value === 'uglink'
          ? '清除绿联通道的全部配置？清除后回到未配置态，APP 将不再使用绿联中转，直到重新添加。'
          : '删除该 HTTPS 直连地址？删除后 APP 将不再尝试它。'
      "
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
  gap: var(--space-4);
}

.table-card {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.port-input {
  width: var(--control-sm);
}

.pwd-note {
  margin-left: var(--space-2);
}

.toggle-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: var(--space-3);
}

.toggle-label {
  font-size: var(--text-base);
  color: var(--color-ink-secondary);
}

.table-card :deep(.status-cell) {
  display: inline-flex;
  gap: var(--space-2);
}

.table-card :deep(.ops-cell) {
  display: inline-flex;
  gap: var(--space-2);
}
</style>
