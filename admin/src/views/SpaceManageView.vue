<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { spaceApi, securityApi, usersApi } from '../api'
import { errMsg } from '../lib/http'
import type { SpaceInfo } from '../api/space'
import type { UserItem, CoupleSidePayload } from '../api/users'
import AppCard from '../components/AppCard.vue'
import AppButton from '../components/AppButton.vue'
import AppBadge from '../components/AppBadge.vue'
import AppAlert from '../components/AppAlert.vue'
import AppModal from '../components/AppModal.vue'
import AppIcon from '../components/AppIcon.vue'
import AppTabs from '../components/AppTabs.vue'
import { toast } from '../lib/toast'

// 三个数据集各占一屏，不上下堆叠（PRODUCT「后台管理页面功能范围（admin）」布局约定）
const tabs = ['空间信息', '情侣成员', '数据保留']
const activeTab = ref('空间信息')

// ---------- 空间信息（含在一起统计，同一张卡） ----------
const space = ref<SpaceInfo | null>(null)
const spaceForm = ref<SpaceInfo>({ name: '', signature: '', love_start_date: '' })
const editing = ref(false)

function daysTogether(): string {
  if (!space.value?.love_start_date) return '—'
  const start = new Date(space.value.love_start_date)
  const diff = Math.floor((Date.now() - start.getTime()) / 86400000)
  return diff >= 0 ? String(diff) : '—'
}

async function saveSpace() {
  try {
    await spaceApi.saveSpace(spaceForm.value)
    space.value = { ...spaceForm.value }
    editing.value = false
    toast.success('空间信息已保存')
  } catch (e) {
    toast.error(errMsg(e, '保存失败'))
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
    const [sp, sec, us] = await Promise.all([
      spaceApi.getSpace(),
      securityApi.getSecuritySettings(),
      usersApi.listUsers(),
    ])
    space.value = sp
    users.value = us
    recycleDays.value = sec.recycleRetentionDays
    if (space.value) spaceForm.value = { ...space.value }
  } catch (e) {
    toast.error(errMsg(e, '空间管理加载失败'))
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
    toast.error(errMsg(e, '保存失败'))
  }
}

// ---------- ：头像与生日（仅后台维护；头像独立通道，生日不下发 APP） ----------
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
    await loadAll()
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

async function submitBirthday() {
  if (!birthdayTarget.value) return
  if (birthdayValue.value) {
    const ok = /^\d{4}-\d{2}-\d{2}$/.test(birthdayValue.value)
    if (!ok) {
      toast.error('生日格式应为 YYYY-MM-DD')
      return
    }
  }
  try {
    // 同端点提交（昵称一并带上，服务端要求 nickname 必填）
    await usersApi.updateBirthday(birthdayTarget.value.id, birthdayTarget.value.nickname, birthdayValue.value)
    toast.success('生日已更新')
    birthdayTarget.value = null
    await loadAll()
  } catch (e) {
    toast.error(errMsg(e, '保存失败'))
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
    toast.error(errMsg(e, '重置失败'))
  }
}

// ---------- 数据保留（空间级保留策略：仅回收站期限；对话历史不设保留时长） ----------
const recycleDays = ref(30)

async function saveRecycle() {
  const days = recycleDays.value
  // 范围有文档依据（PRODUCT「后台管理页面功能范围（admin）」数据保留），超范围行为无规定：不静默改写，直接报错不发请求
  if (!Number.isInteger(days) || days < 7 || days > 365) {
    toast.error('保留期限需在 7 ~ 365 天之间')
    return
  }
  try {
    await securityApi.saveSecuritySettings({ recycleRetentionDays: days })
    recycleDays.value = days
    toast.success('回收站保留期限已保存')
  } catch (e) {
    toast.error(errMsg(e, '保存失败'))
  }
}

onMounted(loadAll)
</script>

<template>
  <div class="page">
    <AppTabs v-model="activeTab" :items="tabs" />

    <!-- 页面本身不滚动，滚动只发生在内容区（PRODUCT「后台管理页面功能范围（admin）」布局约定）。
         一屏一段是刻意的：同屏堆多段会让表格只分到窄列而被挤到换行。 -->
    <div class="page-scroll">
      <AppAlert
        v-if="activeTab === '情侣成员'"
        variant="info"
        icon="users"
        style="margin-bottom: var(--space-5)"
      >
        单情侣应用：情侣账户需一次性同时创建（双方各一个账号）；建立后仅可修改昵称与重置密码，用户名与角色不可变更，不提供删除。
      </AppAlert>

      <AppCard v-if="activeTab === '空间信息'" title="空间信息">
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

      <AppCard v-if="activeTab === '情侣成员'" title="情侣成员">
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
                <tr><th>角色</th><th>头像</th><th>用户名</th><th>昵称</th><th>生日</th><th></th></tr>
              </thead>
              <tbody>
                <tr v-for="u in couple" :key="u.id">
                  <td><AppBadge :variant="roleMeta[u.role as 'girl' | 'boy'].variant">{{ roleMeta[u.role as 'girl' | 'boy'].label }}</AppBadge></td>
                  <td>
                    <img v-if="u.avatar" :src="u.avatar" :alt="u.nickname || u.username" class="avatar-thumb" />
                    <span v-else class="avatar-empty">无</span>
                  </td>
                  <td>{{ u.username }}</td>
                  <td>{{ u.nickname || '—' }}</td>
                  <td>{{ u.birthday || '—' }}</td>
                  <td class="text-right">
                    <div class="flex gap-2" style="justify-content: flex-end">
                      <AppButton variant="secondary" size="sm" icon="edit" @click="openEdit(u)">改昵称</AppButton>
                      <AppButton variant="secondary" size="sm" icon="image" @click="openAvatar(u)">头像</AppButton>
                      <AppButton variant="secondary" size="sm" icon="calendar" @click="openBirthday(u)">生日</AppButton>
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

      <AppCard v-if="activeTab === '数据保留'" title="回收站保留期限">
        <div class="flex gap-2" style="align-items: flex-end">
          <div class="form-group" style="margin: 0; flex: 1">
            <label class="form-label">保留天数（7 ~ 365）</label>
            <input
              v-model.number="recycleDays"
              class="form-input"
              type="number"
              min="7"
              max="365"
              style="max-width: 160px"
            />
          </div>
          <AppButton variant="secondary" size="sm" icon="save" @click="saveRecycle()">保存</AppButton>
        </div>
        <p class="form-hint">超过此天数的已删除内容将被系统定时任务自动永久删除；回收站页顶部展示的期限提示读自此处。</p>
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

    <AppModal v-if="avatarTarget" :title="`上传头像 — ${avatarTarget.username}`" @close="avatarTarget = null">
      <div class="flex" style="flex-direction: column; gap: 14px">
        <p class="form-hint">支持 jpg/png/webp，建议正方形。此入口仅后台使用，头像独立存储，不占用动态媒体配额。</p>
        <div class="avatar-preview-row">
          <img v-if="avatarTarget.avatar" :src="avatarTarget.avatar" :alt="avatarTarget.username" class="avatar-thumb" />
          <span v-else class="avatar-empty">当前无头像</span>
        </div>
        <label class="form-label">
          <input type="file" accept="image/jpeg,image/png,image/webp" :disabled="avatarUploading" @change="onPickAvatar" />
        </label>
        <div class="flex gap-2" style="justify-content: flex-end">
          <AppButton variant="secondary" icon="x" type="button" :disabled="avatarUploading" @click="avatarTarget = null">关闭</AppButton>
        </div>
      </div>
    </AppModal>

    <AppModal v-if="birthdayTarget" :title="`设置生日 — ${birthdayTarget.username}`" @close="birthdayTarget = null">
      <form class="flex" style="flex-direction: column; gap: 14px" @submit.prevent="submitBirthday">
        <p class="form-hint">仅后台维护，不会下发到 APP。</p>
        <div class="form-group" style="margin: 0">
          <label class="form-label">生日</label>
          <input v-model="birthdayValue" class="form-input" type="date" />
          <span class="form-hint">清空日期 = 清除生日。</span>
        </div>
        <div class="flex gap-2" style="justify-content: flex-end">
          <AppButton variant="secondary" icon="x" type="button" @click="birthdayTarget = null">取消</AppButton>
          <AppButton variant="primary" icon="save" type="submit">保存</AppButton>
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
.avatar-thumb {
  width: 40px;
  height: 40px;
  border-radius: 50%;
  object-fit: cover;
  display: block;
}
.avatar-empty {
  display: inline-block;
  font-size: 0.75rem;
  color: var(--color-text-tertiary);
}
.avatar-preview-row {
  display: flex;
  align-items: center;
  gap: 12px;
}
.avatar-preview-row .avatar-thumb {
  width: 64px;
  height: 64px;
}
</style>
