<script setup lang="ts">
// 远程访问 · 备用地址：纯数据列表，服务端不代理。
// 反代、证书、DNS 由部署者自理——这里只登记「APP 可以直连的 HTTPS 地址」。
import { onMounted, ref } from 'vue'
import {
  createBackupAddress,
  deleteBackupAddress,
  listBackupAddresses,
  testBackupAddress,
  updateBackupAddress,
  type BackupAddress,
} from '../api/backupaddr'
import { errMsg } from '../lib/http'
import { toast } from '../lib/toast'
import { useConfirmAction } from '../lib/confirm'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import AppCard from '../components/AppCard.vue'
import AppButton from '../components/AppButton.vue'
import AppBadge from '../components/AppBadge.vue'
import AppAlert from '../components/AppAlert.vue'
import AppToggle from '../components/AppToggle.vue'

const items = ref<BackupAddress[]>([])
const loading = ref(true)

// 新增/编辑表单
const editingId = ref<string | null>(null)
const formName = ref('')
const formUrl = ref('')
const formEnabled = ref(true)
const saving = ref(false)
const testingId = ref<string | null>(null)

async function load() {
  loading.value = true
  try {
    items.value = (await listBackupAddresses()).items
  } catch (e) {
    toast.error(errMsg(e, '加载失败'))
  } finally {
    loading.value = false
  }
}

function resetForm() {
  editingId.value = null
  formName.value = ''
  formUrl.value = ''
  formEnabled.value = true
}

function startEdit(item: BackupAddress) {
  editingId.value = item.id
  formName.value = item.name
  formUrl.value = item.url
  formEnabled.value = item.enabled
}

async function submit() {
  saving.value = true
  try {
    const payload = {
      name: formName.value.trim(),
      url: formUrl.value.trim(),
      enabled: formEnabled.value,
    }
    if (editingId.value) {
      await updateBackupAddress(editingId.value, payload)
      toast.success('已更新')
    } else {
      await createBackupAddress(payload)
      toast.success('已添加')
    }
    resetForm()
    await load()
  } catch (e) {
    toast.error(errMsg(e, '保存失败'))
  } finally {
    saving.value = false
  }
}

const remove = useConfirmAction<BackupAddress>('删除失败')

async function runRemove(item: BackupAddress) {
  await deleteBackupAddress(item.id)
  toast.success('已删除')
  if (editingId.value === item.id) resetForm()
  await load()
}

async function toggleEnabled(item: BackupAddress) {
  try {
    await updateBackupAddress(item.id, {
      name: item.name,
      url: item.url,
      enabled: !item.enabled,
    })
    await load()
  } catch (e) {
    toast.error(errMsg(e, '操作失败'))
  }
}

async function test(item: BackupAddress) {
  testingId.value = item.id
  try {
    const res = await testBackupAddress(item.url)
    if (res.ok) toast.success('服务端可达')
    else toast.error(res.reason || '不可达')
  } catch (e) {
    toast.error(errMsg(e, '测试失败'))
  } finally {
    testingId.value = null
  }
}

onMounted(load)
</script>

<template>
  <div class="page">
    <AppAlert variant="info">
      这里登记的地址由 APP 直接连接，<strong>服务端不代理</strong>。
      反向代理、HTTPS 证书、DNS 解析需你自行配置；仅接受 HTTPS。
      保存后 APP 会<strong>并发探活所有启用地址，自动选延迟最低的一条</strong>；全部不可用则回落到内置地址。
    </AppAlert>

    <AppAlert variant="warning">
      证书须为<strong>系统信任的</strong>（如 Let's Encrypt）。<strong>自签名证书不支持</strong>——
      APP 只信任系统 CA，自签名会直接连接失败。内网自建可用「内网域名 + DNS 校验签发」或反代到已有公网域名。
    </AppAlert>

    <AppCard title="地址列表">
      <div v-if="loading" class="empty">加载中…</div>
      <div v-else-if="items.length === 0" class="empty">还没有备用地址</div>
      <div v-else class="list">
        <div v-for="item in items" :key="item.id" class="item">
          <div class="item-main">
            <div class="item-title">
              <span class="name">{{ item.name || '未命名' }}</span>
              <AppBadge :variant="item.enabled ? 'success' : 'neutral'">
                {{ item.enabled ? '启用' : '停用' }}
              </AppBadge>
            </div>
            <div class="item-url">{{ item.url }}</div>
          </div>
          <div class="item-actions">
            <AppButton size="sm" :disabled="testingId === item.id" @click="test(item)">
              {{ testingId === item.id ? '测试中…' : '测试连通' }}
            </AppButton>
            <AppButton size="sm" @click="toggleEnabled(item)">
              {{ item.enabled ? '停用' : '启用' }}
            </AppButton>
            <AppButton size="sm" @click="startEdit(item)">编辑</AppButton>
            <AppButton size="sm" variant="danger" @click="remove.target.value = item">删除</AppButton>
          </div>
        </div>
      </div>
    </AppCard>

    <AppCard :title="editingId ? '编辑地址' : '添加地址'">
      <div class="form">
        <label class="form-label">名称（可选）</label>
        <input v-model="formName" class="form-input" placeholder="如 我的域名" />

        <label class="form-label">HTTPS 地址</label>
        <input v-model="formUrl" class="form-input" placeholder="https://my.example.com" spellcheck="false" />

        <div class="row-toggle">
          <span class="form-label">启用</span>
          <AppToggle v-model="formEnabled" />
        </div>
      </div>
      <div class="actions">
        <AppButton :disabled="saving" @click="submit">
          {{ saving ? '保存中…' : editingId ? '保存修改' : '添加' }}
        </AppButton>
        <AppButton v-if="editingId" variant="secondary" @click="resetForm">取消</AppButton>
      </div>
    </AppCard>

    <ConfirmDialog
      v-if="remove.target.value"
      title="删除地址"
      :message="`删除「${remove.target.value.name || remove.target.value.url}」？删除后 APP 将不再尝试该地址。`"
      confirm-label="删除"
      :busy="remove.busy.value"
      @close="remove.target.value = null"
      @confirm="remove.confirm(runRemove)"
    />
  </div>
</template>

<style scoped>
.page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.empty {
  padding: 24px 0;
  text-align: center;
  font-size: 13px;
  color: var(--muted-ink);
}
.list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 12px;
  border: 1px solid var(--rule);
  border-radius: 10px;
}
.item-main {
  min-width: 0;
  flex: 1;
}
.item-title {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 4px;
}
.name {
  font-size: 14px;
  font-weight: 500;
  color: var(--ink);
}
.item-url {
  font-size: 13px;
  color: var(--muted-ink);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.item-actions {
  display: flex;
  gap: 6px;
  flex-shrink: 0;
}
.form {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.form-label {
  font-size: 13px;
  color: var(--muted-ink);
  margin-top: 8px;
}
.form-input {
  width: 100%;
  box-sizing: border-box;
  padding: 9px 12px;
  border: 1px solid var(--rule);
  border-radius: 10px;
  background: var(--bg);
  font-size: 14px;
  color: var(--ink);
}
.form-input:focus {
  outline: none;
  border-color: var(--primary);
}
.row-toggle {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: 12px;
}
.actions {
  display: flex;
  gap: 8px;
  margin-top: 16px;
}
</style>
