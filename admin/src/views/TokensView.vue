<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { tokensApi, usersApi } from '../api'
import type { PatToken, PatTier, CreatedPat } from '../api/tokens'
import type { UserItem } from '../api/users'
import AppCard from '../components/AppCard.vue'
import AppButton from '../components/AppButton.vue'
import AppBadge from '../components/AppBadge.vue'
import AppAlert from '../components/AppAlert.vue'
import AppModal from '../components/AppModal.vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import { errMsg } from '../lib/http'
import { toast } from '../lib/toast'
import { useConfirmAction } from '../lib/confirm'

const tokens = ref<PatToken[]>([])
const users = ref<UserItem[]>([])

// PAT 仅绑定情侣用户（girl/boy）；admin 仅后台操作，不签发外部令牌（DESIGN §8.2）
const bindableUsers = computed(() => users.value.filter((u) => u.role !== 'admin'))

// 生成表单弹窗
const showCreate = ref(false)
const createForm = ref({ name: '', user: '', tier: 'readonly' as PatTier })

// 生成结果弹窗：明文 token 仅此一次展示
const created = ref<CreatedPat | null>(null)
const copied = ref(false)

const tierMeta: Record<PatTier, { label: string; variant: 'info' | 'warning' }> = {
  readonly: { label: '只读', variant: 'info' },
  readwrite: { label: '读写', variant: 'warning' },
}

async function load() {
  try {
    ;[tokens.value, users.value] = await Promise.all([tokensApi.listTokens(), usersApi.listUsers()])
  } catch (e) {
    toast.error(errMsg(e, '令牌列表加载失败'))
  }
}

function openCreate() {
  createForm.value = { name: '', user: bindableUsers.value[0]?.nickname ?? '', tier: 'readonly' }
  showCreate.value = true
}

async function submitCreate() {
  try {
    created.value = await tokensApi.createToken({ ...createForm.value })
    copied.value = false
    showCreate.value = false
    toast.success('访问令牌已生成')
    await load()
  } catch (e) {
    toast.error(errMsg(e, '生成失败'))
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

// 吊销走确认弹窗（不用原生 confirm）；结果用全局 toast 反馈
const revoke = useConfirmAction<PatToken>('吊销失败')

async function runRevoke(t: PatToken) {
  // 吊销是幂等的状态迁移，不依赖响应体（服务端可能返回 204 空体）
  await tokensApi.revokeToken(t.id)
  t.status = 'revoked'
  toast.success(`已吊销令牌「${t.name}」`)
}

onMounted(load)
</script>

<template>
  <div class="tab-panel">
    <div class="page-section-header">
      <h2 class="page-section-title">个人访问令牌（PAT）</h2>
      <AppButton variant="primary" icon="plus" @click="openCreate">生成令牌</AppButton>
    </div>

    <AppCard class="card-fill">
      <div class="table-wrapper">
        <table>
          <thead>
            <tr><th>令牌名称</th><th>绑定用户</th><th>权限档位</th><th>创建时间</th><th>状态</th><th></th></tr>
          </thead>
          <tbody>
            <tr v-for="t in tokens" :key="t.id">
              <td>{{ t.name }}</td>
              <td>{{ t.user }}</td>
              <td><AppBadge :variant="tierMeta[t.tier].variant">{{ tierMeta[t.tier].label }}</AppBadge></td>
              <td class="text-secondary">{{ t.createdAt }}</td>
              <td>
                <AppBadge :variant="t.status === 'active' ? 'success' : 'error'">
                  {{ t.status === 'active' ? '有效' : '已吊销' }}
                </AppBadge>
              </td>
              <td class="text-right">
                <AppButton
                  v-if="t.status === 'active'"
                  variant="danger"
                  size="sm"
                  icon="trash"
                  @click="revoke.target.value = t"
                >
                  吊销
                </AppButton>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </AppCard>

    <AppAlert variant="info" icon="key">
      <strong>权限档位说明：</strong><br />
      <strong>只读</strong>：查询与推荐类工具，无副作用。<br />
      <strong>读写</strong>：可写入，不可删除。<br />
      删除类工具不对任何令牌开放，仅 APP 内部使用。
    </AppAlert>

    <AppModal v-if="showCreate" title="生成个人访问令牌" @close="showCreate = false">
      <form class="flex" style="flex-direction: column; gap: 14px" @submit.prevent="submitCreate">
        <div class="form-group" style="margin: 0">
          <label class="form-label">令牌名称</label>
          <input v-model="createForm.name" class="form-input" placeholder="如 Claude Desktop" required />
        </div>
        <div class="flex gap-3">
          <div class="form-group" style="flex: 1; margin: 0">
            <label class="form-label">绑定用户</label>
            <select v-model="createForm.user" class="form-select">
              <option v-for="u in bindableUsers" :key="u.id" :value="u.nickname || u.username">
                {{ u.nickname || u.username }}（{{ u.role }}）
              </option>
            </select>
          </div>
          <div class="form-group" style="flex: 1; margin: 0">
            <label class="form-label">权限档位</label>
            <select v-model="createForm.tier" class="form-select">
              <option value="readonly">只读</option>
              <option value="readwrite">读写</option>
            </select>
          </div>
        </div>
        <div class="flex gap-2" style="justify-content: flex-end">
          <AppButton variant="secondary" icon="x" type="button" @click="showCreate = false">取消</AppButton>
          <AppButton variant="primary" icon="key" type="submit">生成</AppButton>
        </div>
      </form>
    </AppModal>

    <AppModal v-if="created" title="令牌已生成" @close="created = null">
      <div class="flex" style="flex-direction: column; gap: 14px">
        <AppAlert variant="warning" icon="alert-triangle">
          明文令牌<strong>仅此一次</strong>展示，关闭后不可再查看，请立即复制保存。
        </AppAlert>
        <code class="token-plain">{{ created.token }}</code>
        <div class="flex gap-2" style="justify-content: flex-end">
          <AppButton variant="secondary" :icon="copied ? 'check' : 'copy'" @click="copyToken">
            {{ copied ? '已复制' : '复制令牌' }}
          </AppButton>
          <AppButton variant="primary" icon="check" @click="created = null">我已保存</AppButton>
        </div>
      </div>
    </AppModal>

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
.token-plain {
  display: block;
  padding: 12px 14px;
  background: var(--color-bg);
  border: 1px dashed var(--color-border);
  border-radius: var(--radius-md);
  font-family: var(--font-mono);
  font-size: 13px;
  word-break: break-all;
  user-select: all;
}
</style>
