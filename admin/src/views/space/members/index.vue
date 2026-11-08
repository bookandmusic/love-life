<script setup lang="ts">
/** 空间管理 · 情侣成员：双账户一次性创建（创建态）与成员维护（成员态）+ 后台管理员列表。 */
import { computed, h, onMounted, ref, type Ref } from 'vue'
import {
  NAlert,
  NButton,
  NCard,
  NDataTable,
  NForm,
  NFormItem,
  NInput,
  NModal,
  NTag,
  type DataTableColumns
} from 'naive-ui'
import { Lock } from 'lucide-vue-next'
import { usersApi } from '@/api'
import type { UserItem, CoupleSidePayload } from '@/api/users'
import { errMsg } from '@/lib/http'
import { toast } from '@/lib/toast'

const users = ref<UserItem[]>([])
const loading = ref(false)
const couple = computed(() => users.value.filter((u) => u.role === 'girl' || u.role === 'boy'))
const admins = computed(() => users.value.filter((u) => u.role === 'admin'))
const hasCouple = computed(() => couple.value.length > 0)

const roleMeta: Record<'girl' | 'boy', { label: string; tag: 'info' | 'warning' }> = {
  girl: { label: '她（girl）', tag: 'info' },
  boy: { label: '他（boy）', tag: 'warning' }
}

const emptySide = (): CoupleSidePayload => ({ username: '', nickname: '', password: '' })
const form = ref({ girl: emptySide(), boy: emptySide() })
const creating = ref(false)

async function load() {
  loading.value = true
  try {
    users.value = await usersApi.listUsers()
  } catch (e) {
    toast.error(errMsg(e, '成员加载失败'))
  } finally {
    loading.value = false
  }
}

async function submitCouple() {
  creating.value = true
  try {
    await usersApi.createCouple({ girl: { ...form.value.girl }, boy: { ...form.value.boy } })
    form.value = { girl: emptySide(), boy: emptySide() }
    toast.success('情侣成员已创建')
    await load()
  } catch (e) {
    toast.error(errMsg(e, '创建失败'))
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

const savingEdit = ref(false)

async function submitEdit() {
  if (!editTarget.value) return
  if (!editNickname.value.trim()) {
    toast.error('昵称必填')
    return
  }
  savingEdit.value = true
  try {
    await usersApi.updateUser(editTarget.value.id, editNickname.value.trim())
    editTarget.value = null
    toast.success('昵称已更新')
    await load()
  } catch (e) {
    toast.error(errMsg(e, '保存失败'))
  } finally {
    savingEdit.value = false
  }
}

// ---------- 头像与生日（仅后台维护；头像独立通道，生日不下发 APP） ----------
const avatarTarget = ref<UserItem | null>(null)
const avatarUploading = ref(false)

function openAvatar(u: UserItem) {
  avatarTarget.value = u
}

async function onPickAvatar(e: Event) {
  if (!avatarTarget.value) return
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  avatarUploading.value = true
  try {
    await usersApi.uploadAvatar(avatarTarget.value.id, file)
    toast.success('头像已更新')
    avatarTarget.value = null
    await load()
  } catch (err) {
    toast.error(errMsg(err, '头像上传失败'))
  } finally {
    avatarUploading.value = false
    // 重置 input 以便同一文件可重复选择
    input.value = ''
  }
}

const birthdayTarget = ref<UserItem | null>(null)
const birthdayValue = ref('')

function openBirthday(u: UserItem) {
  birthdayTarget.value = u
  birthdayValue.value = u.birthday ?? ''
}

const savingBirthday = ref(false)

async function submitBirthday() {
  if (!birthdayTarget.value) return
  if (birthdayValue.value && !/^\d{4}-\d{2}-\d{2}$/.test(birthdayValue.value)) {
    toast.error('生日格式应为 YYYY-MM-DD')
    return
  }
  savingBirthday.value = true
  try {
    // 专用端点提交：只需生日，不牵动昵称
    await usersApi.updateBirthday(birthdayTarget.value.id, birthdayValue.value)
    toast.success('生日已更新')
    birthdayTarget.value = null
    await load()
  } catch (e) {
    toast.error(errMsg(e, '保存失败'))
  } finally {
    savingBirthday.value = false
  }
}

const pwTarget = ref<UserItem | null>(null)
const pw = ref('')

function openReset(u: UserItem) {
  pwTarget.value = u
  pw.value = ''
}

const savingPw = ref(false)

async function submitReset() {
  if (!pwTarget.value) return
  if (pw.value.length < 6) {
    toast.error('密码至少 6 位')
    return
  }
  savingPw.value = true
  try {
    await usersApi.resetPassword(pwTarget.value.id, pw.value)
    pwTarget.value = null
    toast.success('密码已重置')
  } catch (e) {
    toast.error(errMsg(e, '重置失败'))
  } finally {
    savingPw.value = false
  }
}

onMounted(load)

// 弹窗开关由目标对象派生：非空即显示，关闭即清空
function proxyOf(target: Ref<UserItem | null>) {
  return computed({
    get: () => target.value !== null,
    set: (v) => {
      if (!v) target.value = null
    }
  })
}
const editTargetProxy = proxyOf(editTarget)
const pwTargetProxy = proxyOf(pwTarget)
const avatarTargetProxy = proxyOf(avatarTarget)
const birthdayTargetProxy = proxyOf(birthdayTarget)

const coupleColumns: DataTableColumns<UserItem> = [
  {
    title: '角色',
    key: 'role',
    width: 100,
    render: (row) => {
      const meta = roleMeta[row.role as 'girl' | 'boy']
      return h(NTag, { type: meta.tag, size: 'small', bordered: false }, { default: () => meta.label })
    }
  },
  {
    title: '头像',
    key: 'avatar',
    width: 60,
    render: (row) =>
      row.avatar
        ? h('img', { src: row.avatar, alt: row.nickname || row.username, class: 'avatar-thumb' })
        : h('span', { class: 'avatar-empty' }, '无')
  },
  { title: '用户名', key: 'username', width: 120 },
  { title: '昵称', key: 'nickname', width: 120, render: (row) => row.nickname || '—' },
  { title: '生日', key: 'birthday', width: 110, render: (row) => row.birthday || '—' },
  {
    title: '',
    key: 'actions',
    width: 280,
    align: 'right',
    render: (row) =>
      h('div', { class: 'ops-cell' }, [
        h(NButton, { size: 'small', quaternary: true, onClick: () => openEdit(row) }, { default: () => '改昵称' }),
        h(NButton, { size: 'small', quaternary: true, onClick: () => openAvatar(row) }, { default: () => '头像' }),
        h(NButton, { size: 'small', quaternary: true, onClick: () => openBirthday(row) }, { default: () => '生日' }),
        h(NButton, { size: 'small', quaternary: true, onClick: () => openReset(row) }, { default: () => '重置密码' })
      ])
  }
]

const adminColumns: DataTableColumns<UserItem> = [
  { title: '角色', key: 'role', width: 140, render: () => h(NTag, { size: 'small', bordered: false }, { default: () => '管理员 · admin' }) },
  { title: '用户名', key: 'username', width: 120 },
  { title: '昵称', key: 'nickname', width: 120, render: (row) => row.nickname || '—' },
  {
    title: '',
    key: 'note',
    render: () =>
      h('span', { class: 'admin-note' }, [h(Lock, { size: 14, 'stroke-width': 1.8 }), '仅后台登录使用，由部署环境管理'])
  }
]
</script>

<template>
  <div class="view">
    <NAlert type="info" :show-icon="false">
      单情侣应用：情侣账户需一次性同时创建（双方各一个账号）；建立后仅可修改昵称与重置密码，用户名与角色不可变更，不提供删除。
    </NAlert>

    <NCard size="small" :loading="loading">
      <template v-if="!hasCouple">
        <NForm label-placement="top" size="medium" @submit.prevent="submitCouple">
          <div class="grid-2">
            <div class="side-form">
              <NTag type="info" size="small" :bordered="false" class="side-tag">她 · girl</NTag>
              <NFormItem label="用户名" required><NInput v-model:value="form.girl.username" placeholder="2-32 字符" /></NFormItem>
              <NFormItem label="昵称"><NInput v-model:value="form.girl.nickname" placeholder="昵称" /></NFormItem>
              <NFormItem label="初始密码" required><NInput v-model:value="form.girl.password" type="password" show-password-on="click" placeholder="至少 6 位" /></NFormItem>
            </div>
            <div class="side-form">
              <NTag type="warning" size="small" :bordered="false" class="side-tag">他 · boy</NTag>
              <NFormItem label="用户名" required><NInput v-model:value="form.boy.username" placeholder="2-32 字符" /></NFormItem>
              <NFormItem label="昵称"><NInput v-model:value="form.boy.nickname" placeholder="昵称" /></NFormItem>
              <NFormItem label="初始密码" required><NInput v-model:value="form.boy.password" type="password" show-password-on="click" placeholder="至少 6 位" /></NFormItem>
            </div>
          </div>
          <NButton attr-type="submit" type="primary" :loading="creating">同时创建双方账户</NButton>
        </NForm>
      </template>

      <template v-else>
        <NDataTable
          :columns="coupleColumns"
          :data="couple"
          :bordered="false"
          size="small"
          :row-key="(row: UserItem) => row.id"
        />
      </template>

      <div class="admins-block">
        <NDataTable :columns="adminColumns" :data="admins" :bordered="false" size="small" :row-key="(row: UserItem) => row.id" />
      </div>
    </NCard>

    <NModal v-model:show="editTargetProxy" preset="card" :title="`修改昵称 — ${editTarget?.username}`" :style="{ width: '480px' }" :bordered="true">
      <NForm label-placement="top" size="medium" @submit.prevent="submitEdit">
        <NFormItem label="昵称" required><NInput v-model:value="editNickname" placeholder="昵称" /></NFormItem>
      </NForm>
      <template #footer>
        <div class="modal-actions">
          <NButton @click="editTarget = null">取消</NButton>
          <NButton type="primary" :loading="savingEdit" @click="submitEdit">保存</NButton>
        </div>
      </template>
    </NModal>

    <NModal v-model:show="pwTargetProxy" preset="card" :title="`重置密码 — ${pwTarget?.username}`" :style="{ width: '480px' }" :bordered="true">
      <NForm label-placement="top" size="medium" @submit.prevent="submitReset">
        <NFormItem label="新密码（至少 6 位）" required>
          <NInput v-model:value="pw" type="password" show-password-on="click" placeholder="至少 6 位" />
        </NFormItem>
      </NForm>
      <template #footer>
        <div class="modal-actions">
          <NButton @click="pwTarget = null">取消</NButton>
          <NButton type="primary" :loading="savingPw" @click="submitReset">重置</NButton>
        </div>
      </template>
    </NModal>

    <NModal v-model:show="avatarTargetProxy" preset="card" :title="`上传头像 — ${avatarTarget?.username}`" :style="{ width: '480px' }" :bordered="true">
      <div class="avatar-body">
        <p class="hint-block">支持 jpg/png/webp，建议正方形。此入口仅后台使用，头像独立存储，不占用动态媒体配额。</p>
        <div class="avatar-preview-row">
          <img v-if="avatarTarget?.avatar" :src="avatarTarget.avatar" :alt="avatarTarget.username" class="avatar-thumb large" />
          <span v-else class="avatar-empty">当前无头像</span>
        </div>
        <label class="upload-row">
          <input type="file" accept="image/jpeg,image/png,image/webp" :disabled="avatarUploading" @change="onPickAvatar" />
          <NButton size="small" :loading="avatarUploading" tag="span">选择文件上传</NButton>
        </label>
        <div class="modal-actions">
          <NButton :disabled="avatarUploading" @click="avatarTarget = null">关闭</NButton>
        </div>
      </div>
    </NModal>

    <NModal v-model:show="birthdayTargetProxy" preset="card" :title="`设置生日 — ${birthdayTarget?.username}`" :style="{ width: '480px' }" :bordered="true">
      <NForm label-placement="top" size="medium" @submit.prevent="submitBirthday">
        <p class="hint-block">仅后台维护，不会下发到 APP。</p>
        <NFormItem label="生日">
          <NInput v-model:value="birthdayValue" placeholder="YYYY-MM-DD（清空 = 清除生日）" />
        </NFormItem>
      </NForm>
      <template #footer>
        <div class="modal-actions">
          <NButton @click="birthdayTarget = null">取消</NButton>
          <NButton type="primary" :loading="savingBirthday" @click="submitBirthday">保存</NButton>
        </div>
      </template>
    </NModal>
  </div>
</template>

<style scoped>
.view {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.grid-2 {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: var(--space-5);
}

.side-form {
  display: flex;
  flex-direction: column;
}

.side-tag {
  align-self: flex-start;
  margin-bottom: var(--space-3);
}

.admins-block {
  margin-top: var(--space-4);
  padding-top: var(--space-4);
  border-top: 1px solid var(--color-border);
}

.admin-note {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--text-sm);
  color: var(--color-ink-tertiary);
}

.avatar-body {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.avatar-preview-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

.avatar-thumb {
  width: 40px;
  height: 40px;
  border-radius: var(--radius-xl);
  object-fit: cover;
  display: block;
}

.avatar-thumb.large {
  width: 64px;
  height: 64px;
}

.avatar-empty {
  display: inline-block;
  font-size: var(--text-sm);
  color: var(--color-ink-tertiary);
}

.upload-row {
  display: inline-flex;
  align-items: center;
}

.upload-row input[type='file'] {
  position: absolute;
  width: 1px;
  height: 1px;
  opacity: 0;
}

.hint-block {
  margin: 0;
  font-size: var(--text-sm);
  color: var(--color-ink-tertiary);
}
</style>
