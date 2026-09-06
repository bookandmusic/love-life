<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { spaceApi, securityApi, usersApi } from '../api'
import type { SpaceInfo } from '../api/space'
import type { SecuritySettings } from '../api/security'
import type { UserItem, CoupleSidePayload } from '../api/users'
import AppCard from '../components/AppCard.vue'
import AppButton from '../components/AppButton.vue'
import AppBadge from '../components/AppBadge.vue'
import AppAlert from '../components/AppAlert.vue'
import AppModal from '../components/AppModal.vue'
import AppIcon from '../components/AppIcon.vue'
import { toast } from '../lib/toast'

// ---------- 空间信息（含在一起统计，同一张卡） ----------
const space = ref<SpaceInfo | null>(null)
const spaceForm = ref<SpaceInfo>({ name: '', signature: '', love_start_date: '' })
const editing = ref(false)

function daysTogether(): string {
  if (!space.value?.love_start_date) return '—'
  const start = new Date(space.value.love_start_date)
  const diff = Math.floor((Date.now() - start.getTime()) / 86400000)
  return diff > 0 ? String(diff) : '—'
}

async function saveSpace() {
  try {
    await spaceApi.saveSpace(spaceForm.value)
    space.value = { ...spaceForm.value }
    editing.value = false
    toast.success('空间信息已保存')
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '保存失败')
  }
}

// ---------- 情侣成员（创建态 / 成员态） ----------
const users = ref<UserItem[]>([])
const couple = computed(() => users.value.filter((u) => u.role === 'girl' || u.role === 'boy'))
const admins = computed(() => users.value.filter((u) => u.role === 'admin'))
const hasCouple = computed(() => couple.value.length > 0)

const roleMeta: Record<'girl' | 'boy', { label: string; variant: 'info' | 'warning' }> = {
  girl: { label: '她（girl）', variant: 'info' },
  boy: { label: '他（boy）', variant: 'warning' },
}

const emptySide = (): CoupleSidePayload => ({ username: '', nickname: '', password: '' })
const form = ref({ girl: emptySide(), boy: emptySide() })
const creating = ref(false)

async function loadAll() {
  try {
    ;[space.value, retention.value, users.value] = await Promise.all([
      spaceApi.getSpace(),
      securityApi.getSecuritySettings(),
      usersApi.listUsers(),
    ])
    if (space.value) spaceForm.value = { ...space.value }
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '空间管理加载失败')
  }
}

async function submitCouple() {
  creating.value = true
  try {
    await usersApi.createCouple({ girl: { ...form.value.girl }, boy: { ...form.value.boy } })
    form.value = { girl: emptySide(), boy: emptySide() }
    toast.success('情侣成员已创建')
    await loadAll()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '创建失败')
  } finally {
    creating.value = false
  }
}

const editTarget = ref<UserItem | null>(null)
const editNickname = ref('')

function openEdit(u: UserItem) {
  editTarget.value = u
  editNickname.value = u.nickname
}

async function submitEdit() {
  if (!editTarget.value) return
  if (!editNickname.value.trim()) {
    toast.error('昵称必填')
    return
  }
  try {
    await usersApi.updateUser(editTarget.value.id, editNickname.value.trim())
    editTarget.value = null
    toast.success('昵称已更新')
    await loadAll()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '保存失败')
  }
}

const pwTarget = ref<UserItem | null>(null)
const pw = ref('')

function openReset(u: UserItem) {
  pwTarget.value = u
  pw.value = ''
}

async function submitReset() {
  if (!pwTarget.value) return
  if (pw.value.length < 6) {
    toast.error('密码至少 6 位')
    return
  }
  try {
    await usersApi.resetPassword(pwTarget.value.id, pw.value)
    pwTarget.value = null
    toast.success('密码已重置')
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '重置失败')
  }
}

// ---------- 数据保留（两类空间级保留策略集中于此） ----------
const retention = ref<SecuritySettings>({ recycleRetentionDays: 30, chatRetention: '30' })

async function saveRetention() {
  try {
    await securityApi.saveSecuritySettings(retention.value)
    toast.success('保留策略已保存')
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '保存失败')
  }
}

onMounted(loadAll)
</script>

<template>
  <div class="page">
    <AppAlert variant="info" icon="users">
      单情侣应用：情侣账户需一次性同时创建（双方各一个账号）；建立后仅可修改昵称与重置密码，用户名与角色不可变更，不提供删除。
    </AppAlert>


    <div class="grid-2">
      <AppCard title="空间信息">
        <template #actions>
          <div class="flex gap-2">
            <AppButton v-if="!editing" variant="secondary" size="sm" icon="edit" @click="editing = true">
              编辑
            </AppButton>
            <template v-else>
              <AppButton variant="secondary" size="sm" icon="x" @click="editing = false">取消</AppButton>
              <AppButton variant="primary" size="sm" icon="save" @click="saveSpace">保存</AppButton>
            </template>
          </div>
        </template>
        <div v-if="space && !editing" class="space-info">
          <div class="space-row"><span class="text-tertiary text-sm">空间名称</span><b>{{ space.name }}</b></div>
          <div class="space-row"><span class="text-tertiary text-sm">个性签名</span><b>{{ space.signature || '—' }}</b></div>
          <div class="space-row"><span class="text-tertiary text-sm">在一起日期</span><b>{{ space.love_start_date }}</b></div>
          <div class="space-row">
            <span class="text-tertiary text-sm">在一起天数</span>
            <b class="accent-text">{{ daysTogether() }} 天</b>
          </div>
        </div>
        <div v-else-if="editing" class="flex" style="flex-direction: column; gap: 12px; margin-top: 12px">
          <div class="form-group">
            <label class="form-label">空间名称</label>
            <input v-model="spaceForm.name" class="form-input" />
          </div>
          <div class="form-group">
            <label class="form-label">个性签名</label>
            <input v-model="spaceForm.signature" class="form-input" />
          </div>
          <div class="form-group">
            <label class="form-label">在一起日期</label>
            <input v-model="spaceForm.love_start_date" class="form-input" type="date" />
          </div>
        </div>
        <p v-else class="text-tertiary text-sm">空间信息加载中…</p>
      </AppCard>

      <AppCard title="情侣成员">
        <template v-if="!hasCouple">
          <form class="flex" style="flex-direction: column; gap: 16px" @submit.prevent="submitCouple">
            <div class="grid-2">
              <div class="flex" style="flex-direction: column; gap: 12px">
                <div class="flex items-center gap-2">
                  <AppBadge variant="info">她 · girl</AppBadge>
                </div>
                <div class="form-group" style="margin: 0">
                  <label class="form-label">用户名</label>
                  <input v-model="form.girl.username" class="form-input" placeholder="2-32 字符" required />
                </div>
                <div class="form-group" style="margin: 0">
                  <label class="form-label">昵称</label>
                  <input v-model="form.girl.nickname" class="form-input" placeholder="昵称" />
                </div>
                <div class="form-group" style="margin: 0">
                  <label class="form-label">初始密码</label>
                  <input v-model="form.girl.password" class="form-input" type="password" placeholder="至少 6 位" required />
                </div>
              </div>

              <div class="flex" style="flex-direction: column; gap: 12px">
                <div class="flex items-center gap-2">
                  <AppBadge variant="warning">他 · boy</AppBadge>
                </div>
                <div class="form-group" style="margin: 0">
                  <label class="form-label">用户名</label>
                  <input v-model="form.boy.username" class="form-input" placeholder="2-32 字符" required />
                </div>
                <div class="form-group" style="margin: 0">
                  <label class="form-label">昵称</label>
                  <input v-model="form.boy.nickname" class="form-input" placeholder="昵称" />
                </div>
                <div class="form-group" style="margin: 0">
                  <label class="form-label">初始密码</label>
                  <input v-model="form.boy.password" class="form-input" type="password" placeholder="至少 6 位" required />
                </div>
              </div>
            </div>
            <div>
              <AppButton variant="primary" icon="user-plus" type="submit" :disabled="creating">
                {{ creating ? '创建中…' : '同时创建双方账户' }}
              </AppButton>
            </div>
          </form>
        </template>

        <template v-else>
          <div class="table-wrapper">
            <table>
              <thead>
                <tr><th>角色</th><th>用户名</th><th>昵称</th><th></th></tr>
              </thead>
              <tbody>
                <tr v-for="u in couple" :key="u.id">
                  <td><AppBadge :variant="roleMeta[u.role as 'girl' | 'boy'].variant">{{ roleMeta[u.role as 'girl' | 'boy'].label }}</AppBadge></td>
                  <td>{{ u.username }}</td>
                  <td>{{ u.nickname || '—' }}</td>
                  <td class="text-right">
                    <div class="flex gap-2" style="justify-content: flex-end">
                      <AppButton variant="secondary" size="sm" icon="edit" @click="openEdit(u)">改昵称</AppButton>
                      <AppButton variant="secondary" size="sm" icon="refresh-cw" @click="openReset(u)">重置密码</AppButton>
                    </div>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </template>

        <div class="table-wrapper" style="margin-top: 16px">
          <table>
            <thead>
              <tr><th>角色</th><th>用户名</th><th>昵称</th><th></th></tr>
            </thead>
            <tbody>
              <tr v-for="u in admins" :key="u.id">
                <td><AppBadge variant="neutral">管理员 · admin</AppBadge></td>
                <td>{{ u.username }}</td>
                <td>{{ u.nickname || '—' }}</td>
                <td class="text-right text-tertiary text-sm">
                  <span class="flex items-center gap-2" style="justify-content: flex-end">
                    <AppIcon name="lock" :size="14" />
                    仅后台登录使用，由部署环境管理
                  </span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </AppCard>
    </div>

    <div class="grid-2">
      <AppCard title="回收站保留期限">
        <div class="flex gap-2" style="align-items: flex-end">
          <div class="form-group" style="margin: 0; flex: 1">
            <label class="form-label">保留天数（7 ~ 365）</label>
            <input
              v-model.number="retention.recycleRetentionDays"
              class="form-input"
              type="number"
              min="7"
              max="365"
              style="max-width: 160px"
            />
          </div>
          <AppButton variant="secondary" size="sm" icon="save" @click="saveRetention()">保存</AppButton>
        </div>
        <p class="form-hint">超过此天数的已删除内容将被系统定时任务自动永久删除；回收站页顶部展示的期限提示读自此处。</p>
      </AppCard>

      <AppCard title="对话保留策略">
        <div class="flex gap-2" style="align-items: flex-end">
          <div class="form-group" style="margin: 0; flex: 1">
            <label class="form-label">AI 对话记录默认保留时长</label>
            <select v-model="retention.chatRetention" class="form-select" style="max-width: 160px">
              <option value="7">7 天</option>
              <option value="30">30 天</option>
              <option value="90">90 天</option>
              <option value="permanent">永久保留</option>
            </select>
          </div>
          <AppButton variant="secondary" size="sm" icon="save" @click="saveRetention()">保存</AppButton>
        </div>
        <p class="form-hint">空间级默认值，用户可在 APP 中自行调整。</p>
      </AppCard>
    </div>

    <AppModal v-if="editTarget" :title="`修改昵称 — ${editTarget.username}`" @close="editTarget = null">
      <form class="flex" style="flex-direction: column; gap: 14px" @submit.prevent="submitEdit">
        <div class="form-group" style="margin: 0">
          <label class="form-label">昵称</label>
          <input v-model="editNickname" class="form-input" placeholder="昵称" required />
        </div>
        <div class="flex gap-2" style="justify-content: flex-end">
          <AppButton variant="secondary" icon="x" type="button" @click="editTarget = null">取消</AppButton>
          <AppButton variant="primary" icon="save" type="submit">保存</AppButton>
        </div>
      </form>
    </AppModal>

    <AppModal v-if="pwTarget" :title="`重置密码 — ${pwTarget.username}`" @close="pwTarget = null">
      <form class="flex" style="flex-direction: column; gap: 14px" @submit.prevent="submitReset">
        <div class="form-group" style="margin: 0">
          <label class="form-label">新密码（至少 6 位）</label>
          <input v-model="pw" class="form-input" type="password" placeholder="至少 6 位" required />
        </div>
        <div class="flex gap-2" style="justify-content: flex-end">
          <AppButton variant="secondary" icon="x" type="button" @click="pwTarget = null">取消</AppButton>
          <AppButton variant="primary" icon="refresh-cw" type="submit">重置</AppButton>
        </div>
      </form>
    </AppModal>
  </div>
</template>

<style scoped>
.space-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 0;
  border-bottom: 1px solid var(--color-border);
}
.space-row:last-child {
  border-bottom: none;
}
.accent-text {
  color: var(--color-accent);
}
</style>
