<script setup lang="ts">
// 更新代理（专业配置，从设置页「更新代理」行进入）：
// 内置 + 自定义列表，检查更新时并发竞速取最快（见客户端设计「应用内更新」）；内置项不可删
import { onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Globe } from 'lucide-vue-next'
import PageLayout from '../components/PageLayout.vue'
import { addProxy, buildApiUrl, getProxies, isBuiltinProxy, removeProxy } from '../utils/appUpdate'

const router = useRouter()
const ghProxies = ref(getProxies())
const ghProxyDraft = ref('')
const proxyTesting = ref(false)
// 反馈分锚点：测试结果进「测试连通」行值，列表增删进底部说明行——都是常驻位换文案
const testToast = ref('')
const listToast = ref('')
let testTimer: number | undefined
let listTimer: number | undefined
function flashTest(text: string) {
  testToast.value = text
  if (testTimer) clearTimeout(testTimer)
  testTimer = window.setTimeout(() => (testToast.value = ''), 2600)
}
function flashList(text: string) {
  listToast.value = text
  if (listTimer) clearTimeout(listTimer)
  listTimer = window.setTimeout(() => (listToast.value = ''), 2600)
}
onUnmounted(() => {
  if (testTimer) clearTimeout(testTimer)
  if (listTimer) clearTimeout(listTimer)
})
function addGhProxy() {
  if (!ghProxyDraft.value.trim()) return
  const before = getProxies()
  addProxy(ghProxyDraft.value)
  const after = getProxies()
  ghProxyDraft.value = ''
  ghProxies.value = after
  // addProxy 对重复/与内置同值静默忽略，提示要跟实际结果一致
  flashList(after.length > before.length ? '代理已添加' : '该代理已在列表中')
}
function removeGhProxy(p: string) {
  removeProxy(p)
  ghProxies.value = getProxies()
  flashList('代理已移除')
}
async function testProxy() {
  proxyTesting.value = true
  const proxies = ghProxies.value
  try {
    const results = await Promise.allSettled(
      proxies.map((p) =>
        fetch(buildApiUrl(p), { signal: AbortSignal.timeout(8_000) }).then((res) => {
          if (!res.ok) throw new Error(`HTTP ${res.status}`)
        })
      )
    )
    const ok = results.filter((r) => r.status === 'fulfilled').length
    flashTest(ok ? `连通 ${ok}/${proxies.length}` : '全部不可连通')
  } finally {
    proxyTesting.value = false
  }
}
</script>

<template>
  <PageLayout title="更新代理" back standalone @back="router.back()">
    <div class="content">
      <section class="grp">
        <div class="sec">代理列表</div>
        <div class="group">
          <div v-for="p in ghProxies" :key="p" class="row">
            <span class="label proxy-url">{{ p }}</span>
            <span v-if="isBuiltinProxy(p)" class="value num">内置</span>
            <button v-else class="proxy-del" @click="removeGhProxy(p)">删除</button>
          </div>
          <div class="row">
            <input
              v-model="ghProxyDraft"
              class="value proxy-input"
              type="text"
              inputmode="url"
              placeholder="添加自定义代理，如 https://gh-proxy.net"
              spellcheck="false"
              @change="addGhProxy"
            />
          </div>
        </div>
      </section>

      <section class="grp">
        <div class="sec">连通性</div>
        <div class="group">
          <button class="row row-link" :disabled="proxyTesting" @click="testProxy">
            <span class="row-ic"><Globe :size="16" stroke-width="1.8" /></span>
            <span class="label">测试连通</span>
            <span class="value value-trunc" aria-live="polite">{{ proxyTesting ? '测试中…' : (testToast || '全部逐个探测') }}</span>
          </button>
        </div>
      </section>

      <p class="page-hint" aria-live="polite">{{ listToast || '内置代理不可删；检查更新时竞速取最快。' }}</p>
    </div>
  </PageLayout>
</template>
<style scoped src="../styles/settings.css"></style>
<style scoped>
/* 代理 URL 行与添加输入（右侧对齐占满 value 位） */
.proxy-input {
  border: none;
  outline: none;
  background: transparent;
  text-align: right;
  font-size: 13px;
  color: var(--ink);
  min-width: 0;
  flex: 1;
  padding: 0;
}
.proxy-input::placeholder { color: var(--muted-ink); }
.proxy-url {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.proxy-del {
  position: relative; /* ::before 外扩命中区 */
  border: none;
  background: transparent;
  color: var(--danger, #e0494b);
  font-size: 13px;
  padding: 0;
  cursor: pointer;
  flex-shrink: 0;
}
/* 触控目标 ≥44px（客户端设计「触控目标口径」）：视觉小字不变，命中区外扩 */
.proxy-del::before {
  content: '';
  position: absolute;
  inset: -14px -13px;
}
.page-hint {
  margin: 2px 0 0;
  font-size: 12px;
  line-height: 1.5;
  color: var(--muted-ink);
  text-align: center;
  /* 兼反馈行：单行恒定高，文案长短不引起布局跳动 */
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
</style>
