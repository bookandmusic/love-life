<script setup lang="ts">
/**
 * 种子页：访问令牌（PAT）。其余 18 页照本页的骨架与写法生成。
 * 覆盖：数据表（吸顶滚动）、生成表单弹窗、一次性明文展示弹窗、危险操作确认、
 * 空态、说明性 Alert——形态最全的一页。
 */
import { computed, h, onMounted, ref } from 'vue'
import {
  NAlert,
  NButton,
  NCard,
  NDataTable,
  NForm,
  NFormItem,
  NInput,
  NModal,
  NSelect,
  NTag,
  type DataTableColumns,
} from 'naive-ui'
import { KeyRound } from 'lucide-vue-next'
import { tokensApi, usersApi } from '@/api'
import type { PatToken, PatTier, CreatedPat } from '@/api/tokens'
import type { UserItem } from '@/api/users'
import SectionHeader from '@/components/SectionHeader.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { errMsg } from '@/lib/http'
import { toast } from '@/lib/toast'
import { useConfirmAction } from '@/lib/confirm'

const tokens = ref<PatToken[]>([])
const users = ref<UserItem[]>([])
const loading = ref(false)

// PAT 仅绑定情侣用户（girl/boy）；admin 仅后台操作，不签发外部令牌
const bindableUsers = computed(() => users.value.filter((u) => u.role !== 'admin'))

// 生成表单弹窗
const saving = ref(false)
const showCreate = ref(false)
const createForm = ref({ name: '', user: '', tier: 'readonly' as PatTier })

// 生成结果弹窗：明文 token 仅此一次展示
const created = ref<CreatedPat | null>(null)
const copied = ref(false)

// 弹窗开关由 created 派生：非空即显示，关闭即清空
const showCreated = computed({
  get: () => created.value !== null,
  set: (v) => {
    if (!v) created.value = null
  },
})

const tierMeta: Record<PatTier, { label: string; tag: 'info' | 'warning' }> = {
  readonly: { label: '只读', tag: 'info' },
  readwrite: { label: '读写', tag: 'warning' },
}

// 吊销走确认弹窗（不用原生 confirm）；结果用全局 toast 反馈
const revoke = useConfirmAction<PatToken>('吊销失败')

const columns: DataTableColumns<PatToken> = [
  { title: '令牌名称', key: 'name', minWidth: 160 },
  { title: '绑定用户', key: 'user', minWidth: 100 },
  {
    title: '权限档位',
    key: 'tier',
    width: 90,
    render: (row) => h(NTag, { type: tierMeta[row.tier].tag, size: 'small', bordered: false }, { default: () => tierMeta[row.tier].label }),
  },
  { title: '创建时间', key: 'createdAt', minWidth: 150 },
  {
    title: '状态',
    key: 'status',
    width: 90,
    render: (row) =>
      h(
        NTag,
        { type: row.status === 'active' ? 'success' : 'error', size: 'small', bordered: false },
        { default: () => (row.status === 'active' ? '有效' : '已吊销') },
      ),
  },
  {
    title: '',
    key: 'actions',
    width: 80,
    align: 'right',
    render: (row) =>
      row.status === 'active'
        ? h(NButton, { size: 'small', type: 'error', secondary: true, onClick: () => (revoke.target.value = row) }, { default: () => '吊销' })
        : null,
  },
]

async function load() {
  loading.value = true
  try {
    ;[tokens.value, users.value] = await Promise.all([tokensApi.listTokens(), usersApi.listUsers()])
  } catch (e) {
    toast.error(errMsg(e, '令牌列表加载失败'))
  } finally {
    loading.value = false
  }
}

function openCreate() {
  createForm.value = { name: '', user: bindableUsers.value[0]?.nickname ?? '', tier: 'readonly' }
  showCreate.value = true
}

const userOptions = computed(() =>
  bindableUsers.value.map((u) => ({ label: `${u.nickname || u.username}（${u.role}）`, value: u.nickname || u.username })),
)

const tierOptions = [
  { label: '只读', value: 'readonly' },
  { label: '读写', value: 'readwrite' },
]

async function submitCreate() {
  saving.value = true
  try {
    created.value = await tokensApi.createToken({ ...createForm.value })
    copied.value = false
    showCreate.value = false
    toast.success('访问令牌已生成')
    await load()
  } catch (e) {
    toast.error(errMsg(e, '生成失败'))
  } finally {
    saving.value = false
  }
}

async function copyToken() {
  if (!created.value) return
  try {
    await navigator.clipboard.writeText(created.value.token)
    copied.value = true
  } catch {
    toast.error('自动复制失败，请手动选择令牌文本复制')
  }
}

async function runRevoke(t: PatToken) {
  // 吊销是幂等的状态迁移，不依赖响应体（服务端可能返回 204 空体）
  await tokensApi.revokeToken(t.id)
  t.status = 'revoked'
  toast.success(`已吊销令牌「${t.name}」`)
}

onMounted(load)
</script>

<template>
  <div class="view">
    <SectionHeader title="个人访问令牌（PAT）" desc="供 MCP 客户端等外部 Agent 接入；明文仅生成时展示一次。">
      <NButton type="primary" @click="openCreate">
        <template #icon>
          <KeyRound :size="15" :stroke-width="1.8" />
        </template>
        生成令牌
      </NButton>
    </SectionHeader>

    <NCard :bordered="true" size="small" class="table-card">
      <NDataTable
        :columns="columns"
        :data="tokens"
        :loading="loading"
        flex-height
        style="flex: 1; min-height: 0"
        :scroll-x="720"
        :bordered="false"
        :row-key="(row: PatToken) => row.id"
      />
    </NCard>

    <NAlert type="info" :show-icon="false">
      <strong>权限档位说明：</strong>
      <span>只读：查询与推荐类工具，无副作用；读写：可写入，不可删除。</span>
      <span>删除类工具不对任何令牌开放，仅 APP 内部使用。</span>
    </NAlert>

    <NModal v-model:show="showCreate" preset="card" title="生成个人访问令牌" :style="{ width: '480px' }" :bordered="true">
      <NForm label-placement="top" size="medium">
        <NFormItem label="令牌名称" required>
          <NInput v-model:value="createForm.name" placeholder="如 Claude Desktop" />
        </NFormItem>
        <NFormItem label="绑定用户" required>
          <NSelect v-model:value="createForm.user" :options="userOptions" placeholder="选择用户" />
        </NFormItem>
        <NFormItem label="权限档位" required>
          <NSelect v-model:value="createForm.tier" :options="tierOptions" />
        </NFormItem>
      </NForm>
      <template #footer>
        <div class="modal-actions">
          <NButton @click="showCreate = false">取消</NButton>
          <NButton type="primary" :loading="saving" @click="submitCreate">生成</NButton>
        </div>
      </template>
    </NModal>

    <NModal v-model:show="showCreated" preset="card" title="令牌已生成" :style="{ width: '480px' }" :bordered="true">
      <div class="created-body">
        <NAlert type="warning" :show-icon="false">
          明文令牌<strong>仅此一次</strong>展示，关闭后不可再查看，请立即复制保存。
        </NAlert>
        <code class="token-plain">{{ created?.token }}</code>
      </div>
      <template #footer>
        <div class="modal-actions">
          <NButton @click="copyToken">{{ copied ? '已复制' : '复制令牌' }}</NButton>
          <NButton type="primary" @click="created = null">我已保存</NButton>
        </div>
      </template>
    </NModal>

    <ConfirmDialog
      v-if="revoke.target.value"
      title="吊销访问令牌"
      :message="`吊销令牌「${revoke.target.value.name}」？吊销后该 Agent 将立即失去访问权限。`"
      confirm-label="吊销"
      :busy="revoke.busy.value"
      @close="revoke.target.value = null"
      @confirm="revoke.confirm(runRevoke)"
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

.created-body {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.token-plain {
  display: block;
  padding: var(--space-3) var(--space-4);
  background: var(--color-subtle);
  border: 1px dashed var(--color-border);
  border-radius: var(--radius-md);
  font-family: var(--font-mono);
  font-size: var(--text-sm);
  word-break: break-all;
  user-select: all;
  color: var(--color-ink);
}
</style>
