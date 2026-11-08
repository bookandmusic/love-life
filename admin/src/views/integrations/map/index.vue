<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { NAlert, NButton, NCard, NForm, NFormItem, NInput, NModal, NSelect, NTag } from 'naive-ui'
import { Check, Plus, Trash2 } from 'lucide-vue-next'
import { mapApi } from '@/api'
import type { MapSource, MapSourceType } from '@/api/map'
import { errMsg } from '@/lib/http'
import { toast } from '@/lib/toast'
import { useConfirmAction } from '@/lib/confirm'
import ConfirmDialog from '@/components/ConfirmDialog.vue'

const sources = ref<MapSource[]>([])
const loaded = ref(false)
const loadFailed = ref(false)

// 添加卡（新源默认不启用，启用前需配置完整；不支持编辑——配错删掉重加）
const adding = ref(false)
const newType = ref<MapSourceType>('public')
const newTemplate = ref('')
const newKey = ref('')
const newLayer = ref('vec_w')
const newMaxZoom = ref(17)
const creating = ref(false)
// 探测门禁：probed = null 未探测 / 0 失败 / N 成功层级——创建必须 probed > 0
const probing = ref(false)
const probed = ref<number | null>(null)
const probeMsg = ref('')

const typeOptions = [
  { label: '公开瓦片服务（XYZ 模板）', value: 'public' },
  { label: '天地图（密钥认证）', value: 'tianditu' },
]

const layerOptions = [
  { label: '矢量底图（vec_w）', value: 'vec_w' },
  { label: '影像底图（img_w）', value: 'img_w' },
  { label: '地形晕渲（ter_w）', value: 'ter_w' },
]

// 三种底图的场景说明（卡片与表单共用一处定义）
const layerMeta: Record<string, { label: string; desc: string }> = {
  vec_w: { label: '矢量底图', desc: '街道详图（路名/地名标注，日常默认推荐）' },
  img_w: { label: '影像底图', desc: '卫星照片（真实地貌，标注叠加显示）' },
  ter_w: { label: '地形晕渲', desc: '山地起伏着色（无路网细节，户外/旅行用）' },
}

async function load() {
  try {
    sources.value = (await mapApi.listMapSources()).items
    loaded.value = true
    loadFailed.value = false
  } catch (e) {
    // 操作反馈统一走全局 toast；占位段只表达加载状态，不渲染原始错误串
    loadFailed.value = true
    toast.error(errMsg(e, '加载失败'))
  }
}

function typeLabel(t: MapSourceType): string {
  return t === 'public' ? '公开瓦片服务（XYZ）' : '天地图（密钥认证）'
}

// 卡片标题：天地图带中文图层名一眼可辨；public 保持原文案
function titleFor(s: MapSource): string {
  if (s.type === 'tianditu') {
    const meta = layerMeta[s.tianditu_layer]
    return meta ? `天地图 · ${meta.label}` : '天地图（密钥认证）'
  }
  return typeLabel(s.type)
}

async function enable(s: MapSource) {
  try {
    await mapApi.enableMapSource(s.id)
    toast.success(`已启用「${typeLabel(s.type)}」`)
    await load()
  } catch (e) {
    toast.error(errMsg(e, '启用失败'))
  }
}

// 删除走确认弹窗（不用原生 confirm）；结果用全局 toast 反馈
const remove = useConfirmAction<MapSource>('删除失败')

async function runRemove(s: MapSource) {
  await mapApi.deleteMapSource(s.id)
  toast.success('瓦片源已删除')
  await load()
}

function openAdd() {
  adding.value = true
  newType.value = 'public'
  newTemplate.value = ''
  newKey.value = ''
  newLayer.value = 'vec_w'
  newMaxZoom.value = 17
  probed.value = null
  probeMsg.value = ''
}

// 表单内探测（不落库）：服务端试拉真实瓦片返回数据上限；成功后回显并解锁创建
async function runProbe() {
  if (probing.value) return
  // 参数快照：响应回来时表单可能已改，这份结果就不属于当前配置，直接丢弃
  const snapshot = () => `${newType.value}|${newLayer.value}|${newTemplate.value.trim()}|${newKey.value.trim()}`
  const requested = snapshot()
  probing.value = true
  probeMsg.value = ''
  try {
    const res = await mapApi.probeMapSource({
      type: newType.value,
      url_template: newTemplate.value.trim(),
      tianditu_layer: newLayer.value,
      ...(newKey.value.trim() ? { tianditu_key: newKey.value.trim() } : {}),
    })
    if (snapshot() !== requested) return
    probed.value = res.max_zoom
    newMaxZoom.value = res.max_zoom
    probeMsg.value = `探测成功：最高到 z${res.max_zoom}`
  } catch (e) {
    if (snapshot() !== requested) return
    probed.value = 0
    probeMsg.value = errMsg(e, '探测失败')
  } finally {
    probing.value = false
  }
}

const canCreate = computed(() => (probed.value ?? 0) > 0 && !creating.value && !probing.value)

// 表单关键字段变更 → 探测值失效（探测针对的是当时的配置；改了配置必须重新探测）
watch([newType, newTemplate, newKey, newLayer], () => {
  if (probed.value !== null) {
    probed.value = null
    probeMsg.value = '配置已变更，请重新探测'
  }
})

async function create() {
  if (creating.value) return
  creating.value = true
  try {
    await mapApi.createMapSource({
      type: newType.value,
      url_template: newTemplate.value.trim(),
      tianditu_layer: newLayer.value,
      max_zoom: newMaxZoom.value, // 探测值（服务端强校验：未探测 400）
      ...(newKey.value.trim() ? { tianditu_key: newKey.value.trim() } : {}),
    })
    toast.success('瓦片源已添加，配置完整后可在卡片上启用')
    adding.value = false
    await load()
  } catch (e) {
    toast.error(errMsg(e, '添加失败'))
  } finally {
    creating.value = false
  }
}

onMounted(load)

const showAdd = computed({
  get: () => adding.value,
  set: (v) => (adding.value = v),
})
</script>

<template>
  <div class="view">
    <NAlert type="info" :show-icon="false">
      服务端瓦片代理：APP 统一请求后端瓦片接口，密钥只存服务端永不下发。可添加多个瓦片源（类型限公开 XYZ 模板 / 天地图），任意时刻仅一个「使用中」；配置有误直接删除后重新添加。
    </NAlert>

    <p v-if="!loaded" class="hint-block">
      {{ loadFailed ? '加载失败，请刷新页面重试' : '加载中…' }}
    </p>

    <div v-else class="source-grid">
      <!-- 已保存的瓦片源卡片 -->
      <NCard v-for="s in sources" :key="s.id" size="small" :title="titleFor(s)" class="source-card">
        <template #header-extra>
          <NTag v-if="s.enabled" type="success" size="small" :bordered="false">使用中</NTag>
          <NButton v-else size="small" quaternary @click="enable(s)">
            <template #icon>
              <Check :size="14" :stroke-width="1.8" />
            </template>
            启用
          </NButton>
        </template>

        <p v-if="s.type === 'public'" class="source-line">
          {{ s.url_template || '未配置地址模板' }}
        </p>
        <template v-else>
          <p class="source-line">{{ layerMeta[s.tianditu_layer]?.desc || '' }}</p>
          <p class="source-line source-line-row">
            <span>底图 {{ s.tianditu_layer }}</span>
            <NTag :type="s.has_key ? 'success' : 'default'" size="small" :bordered="false">
              {{ s.has_key ? '已配密钥' : '未配密钥' }}
            </NTag>
          </p>
        </template>
        <p class="source-line">
          <span>数据上限 z{{ s.max_zoom || (s.type === 'tianditu' ? 14 : 17) }} · APP 显示到 z{{ Math.max((s.max_zoom || (s.type === 'tianditu' ? 14 : 17)) - 1, 3) }}</span>
        </p>

        <div class="source-actions">
          <NButton size="small" type="error" secondary @click="remove.target.value = s">
            <template #icon>
              <Trash2 :size="14" :stroke-width="1.8" />
            </template>
            删除
          </NButton>
        </div>
      </NCard>

      <!-- 添加卡（固定最后）：虚线占位块仅作入口，表单走弹窗、不影响网格布局 -->
      <button class="add-tile" type="button" @click="openAdd">
        <Plus :size="22" :stroke-width="1.8" />
        <span>添加瓦片源</span>
      </button>
    </div>

    <NModal v-model:show="showAdd" preset="card" title="添加瓦片源" :style="{ width: '480px' }" :bordered="true">
      <NForm label-placement="top" size="medium">
        <NFormItem label="类型">
          <NSelect v-model:value="newType" :options="typeOptions" />
        </NFormItem>
        <NFormItem v-if="newType === 'public'" label="瓦片地址模板（含 {z}/{x}/{y} 占位）" required>
          <NInput v-model:value="newTemplate" placeholder="https://map.example.com/{z}/{x}/{y}.png" />
        </NFormItem>
        <p v-if="newType === 'public'" class="hint-block">示例：https://map.example.com/{z}/{x}/{y}.png</p>
        <template v-else>
          <NFormItem label="天地图密钥（tk，必填）" required>
            <NInput v-model:value="newKey" type="password" show-password-on="click" placeholder="粘贴天地图 Key" />
          </NFormItem>
          <NFormItem label="底图类型">
            <NSelect v-model:value="newLayer" :options="layerOptions" />
          </NFormItem>
        </template>
        <NFormItem label="数据上限（自动探测）">
          <div class="probe-row">
            <NButton size="small" :loading="probing" @click="runProbe">探测最大层级</NButton>
            <span v-if="probeMsg" class="probe-msg" :class="{ ok: (probed ?? 0) > 0 }">{{ probeMsg }}</span>
          </div>
        </NFormItem>
      </NForm>
      <template #footer>
        <div class="modal-actions">
          <span class="hint-inline">{{ canCreate ? '新源默认不启用' : '先探测成功才能创建' }}</span>
          <NButton @click="adding = false">取消</NButton>
          <NButton type="primary" :disabled="!canCreate" :loading="creating" @click="create">创建</NButton>
        </div>
      </template>
    </NModal>

    <ConfirmDialog
      v-if="remove.target.value"
      title="删除瓦片源"
      :message="`删除${typeLabel(remove.target.value.type)}？删除后需重新添加；使用中的源不可删除。`"
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
  gap: var(--space-5);
}

.source-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  grid-auto-rows: 1fr; /* 所有行等高：单列/多列下卡片大小一致 */
  gap: var(--space-4);
}

.source-card {
  display: flex;
  flex-direction: column;
}

.source-card :deep(.n-card-content) {
  flex: 1;
  display: flex;
  flex-direction: column;
}

.source-line {
  margin: 0;
  font-size: var(--text-base);
  color: var(--color-ink-secondary);
  word-break: break-all;
}

.source-line-row {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.source-actions {
  display: flex;
  gap: var(--space-2);
  margin-top: auto;
  padding-top: var(--space-3);
}

.add-tile {
  min-height: 140px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  border: 1px dashed var(--color-border);
  border-radius: var(--radius-lg);
  background: transparent;
  color: var(--color-ink-tertiary);
  font-size: var(--text-md);
  font-family: inherit;
  cursor: pointer;
  transition: border-color var(--transition), color var(--transition);
}

.add-tile:hover {
  border-color: var(--color-ink);
  color: var(--color-ink);
}

.hint-block {
  margin: 0 0 var(--space-3);
  font-size: var(--text-sm);
  color: var(--color-ink-tertiary);
}

.hint-inline {
  margin-right: auto;
  font-size: var(--text-sm);
  color: var(--color-ink-tertiary);
}

.probe-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  width: 100%;
}

.probe-msg {
  font-size: var(--text-xs);
  color: var(--color-ink-secondary);
}

.probe-msg.ok {
  color: var(--color-success);
}

.n-form-item .hint-inline {
  margin-left: var(--space-3);
  flex-shrink: 0;
}
</style>
