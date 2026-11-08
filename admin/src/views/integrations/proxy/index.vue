<script setup lang="ts">
/**
 * HTTP 代理：仅作用于应用更新的 GitHub 出站（版本信息与安装包下载）；
 * 模型 API / 地图瓦片 / 通知 Webhook 各走自己的通道，不经它。
 * 应用更新页的「使用 HTTP 代理」开关决定走不走这里配置的代理。
 */
import { onMounted, ref } from 'vue'
import { NButton, NCard, NForm, NFormItem, NInput, NTag } from 'naive-ui'
import { getAppUpdateConfig, saveAppUpdateConfig } from '@/api/appupdate'
import SectionHeader from '@/components/SectionHeader.vue'
import { errMsg } from '@/lib/http'
import { toast } from '@/lib/toast'

const proxyUrl = ref('')
const proxyUser = ref('')
const proxyPassword = ref('')
const hasPassword = ref(false)
// 开关本体在「应用更新」页，但 PUT 是整体替换（仅密码沿用）——本页保存必须带上它，否则会把开关重置回启用
const useProxy = ref(true)
const saving = ref(false)

async function load() {
  try {
    const cfg = await getAppUpdateConfig()
    proxyUrl.value = cfg.proxy_url
    proxyUser.value = cfg.proxy_user
    hasPassword.value = cfg.has_password
    useProxy.value = cfg.use_proxy ?? true
  } catch (e) {
    toast.error(errMsg(e, '加载失败'))
  }
}

async function save() {
  saving.value = true
  try {
    await saveAppUpdateConfig({
      proxy_url: proxyUrl.value.trim(),
      proxy_user: proxyUser.value.trim(),
      proxy_password: proxyPassword.value || undefined,
      use_proxy: useProxy.value
    })
    proxyPassword.value = ''
    toast.success('已保存')
    await load()
  } catch (e) {
    toast.error(errMsg(e, '保存失败'))
  } finally {
    saving.value = false
  }
}

async function clearPassword() {
  try {
    await saveAppUpdateConfig({
      proxy_url: proxyUrl.value.trim(),
      proxy_user: proxyUser.value.trim(),
      clear_password: true,
      use_proxy: useProxy.value
    })
    toast.success('代理密码已清除')
    await load()
  } catch (e) {
    toast.error(errMsg(e, '清除失败'))
  }
}

onMounted(load)
</script>

<template>
  <div class="view">
    <SectionHeader title="HTTP 代理" desc="标准 HTTP(S) 代理，供服务端出站访问 GitHub 使用；支持可选认证。是否启用由「应用更新」页的开关决定。" />
    <NCard size="small">
      <p class="hint">
        <strong>仅作用于应用更新的 GitHub 出站</strong>（版本信息与安装包下载）；模型 API、地图瓦片、通知 Webhook 不走它。留空则直连。
      </p>
      <NForm label-placement="top" size="medium">
        <NFormItem label="代理地址">
          <NInput v-model:value="proxyUrl" placeholder="http://192.168.1.10:7890" :input-props="{ spellcheck: false }" />
        </NFormItem>
        <NFormItem label="用户名（可选）">
          <NInput v-model:value="proxyUser" :input-props="{ autocomplete: 'off', spellcheck: false }" />
        </NFormItem>
        <NFormItem>
          <template #label>
            密码（可选）
            <NTag v-if="hasPassword" size="small" :bordered="false" class="pwd-note">已设置，留空表示不修改</NTag>
          </template>
          <NInput
            v-model:value="proxyPassword"
            type="password"
            show-password-on="click"
            :input-props="{ autocomplete: 'new-password' }"
            :placeholder="hasPassword ? '••••••••' : '代理无需认证时留空'"
          />
        </NFormItem>
      </NForm>
      <div class="actions">
        <NButton type="primary" :loading="saving" @click="save">保存</NButton>
        <NButton v-if="hasPassword" @click="clearPassword">清除密码</NButton>
      </div>
    </NCard>
  </div>
</template>

<style scoped>
.view {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.hint {
  margin: 0 0 var(--space-4);
  font-size: var(--text-base);
  line-height: var(--leading-relaxed);
  color: var(--color-ink-secondary);
}

.pwd-note {
  margin-left: var(--space-2);
}

.actions {
  display: flex;
  gap: var(--space-2);
}
</style>
