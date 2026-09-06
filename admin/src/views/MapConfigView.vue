<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { mapApi } from '../api'
import type { MapSource, MapSourceType } from '../api/map'
import { toast } from '../lib/toast'
import AppCard from '../components/AppCard.vue'
import AppModal from '../components/AppModal.vue'
import AppButton from '../components/AppButton.vue'
import AppAlert from '../components/AppAlert.vue'
import AppBadge from '../components/AppBadge.vue'
import AppIcon from '../components/AppIcon.vue'

const sources = ref<MapSource[]>([])
const loaded = ref(false)
const loadError = ref('')

// 删除两步确认（避免误删；不用原生 confirm）
const confirmingDelete = ref<number | null>(null)

// 添加卡（新源默认不启用，启用前需配置完整；不支持编辑——配错删掉重加）
const adding = ref(false)
const newType = ref<MapSourceType>('public')
const newTemplate = ref('')
const newKey = ref('')
const newLayer = ref('vec_w')
const creating = ref(false)

async function load() {
  loadError.value = ''
  try {
    sources.value = (await mapApi.listMapSources()).items
    loaded.value = true
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : '加载失败'
  }
}

function typeLabel(t: MapSourceType): string {
  return t === 'public' ? '公开瓦片服务（XYZ）' : '天地图（密钥认证）'
}

async function enable(s: MapSource) {
  try {
    await mapApi.enableMapSource(s.id)
    toast.success(`已启用「${typeLabel(s.type)}」`)
    await load()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '启用失败')
  }
}

async function remove(s: MapSource) {
  // 两步确认：第一次点变「确认删除？」，3 秒未确认自动复位
  if (confirmingDelete.value !== s.id) {
    confirmingDelete.value = s.id
    setTimeout(() => {
      if (confirmingDelete.value === s.id) confirmingDelete.value = null
    }, 3000)
    return
  }
  confirmingDelete.value = null
  try {
    await mapApi.deleteMapSource(s.id)
    toast.success('瓦片源已删除')
    await load()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '删除失败')
  }
}

function openAdd() {
  adding.value = true
  newType.value = 'public'
  newTemplate.value = ''
  newKey.value = ''
  newLayer.value = 'vec_w'
}

async function create() {
  if (creating.value) return
  creating.value = true
  try {
    await mapApi.createMapSource({
      type: newType.value,
      url_template: newTemplate.value.trim(),
      tianditu_layer: newLayer.value,
      ...(newKey.value.trim() ? { tianditu_key: newKey.value.trim() } : {}),
    })
    toast.success('瓦片源已添加，配置完整后可在卡片上启用')
    adding.value = false
    await load()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '添加失败')
  } finally {
    creating.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="tab-panel" style="max-width: 960px">
    <div class="page-scroll">
      <AppAlert variant="info" icon="map-pin">
        服务端瓦片代理：APP 统一请求后端瓦片接口，密钥只存服务端永不下发。可添加多个瓦片源（类型限公开 XYZ 模板 / 天地图），任意时刻仅一个「使用中」；配置有误直接删除后重新添加。
      </AppAlert>

      <p v-if="!loaded" class="form-hint" style="margin-top: 16px">{{ loadError ? loadError : '加载中…' }}</p>

      <div v-else class="source-grid">
        <!-- 已保存的瓦片源卡片（等高：展示 + 底部操作行） -->
        <AppCard v-for="s in sources" :key="s.id" :title="typeLabel(s.type)">
          <template #actions>
            <AppBadge v-if="s.enabled" variant="success">使用中</AppBadge>
            <AppButton v-else size="sm" icon="check" @click="enable(s)">启用</AppButton>
          </template>

          <p v-if="s.type === 'public'" class="source-line">
            {{ s.url_template || '未配置地址模板' }}
          </p>
          <p v-else class="source-line flex gap-2 items-center" style="flex-wrap: wrap">
            <span>底图 {{ s.tianditu_layer }}</span>
            <AppBadge :variant="s.has_key ? 'success' : 'neutral'">
              {{ s.has_key ? '已配密钥' : '未配密钥' }}
            </AppBadge>
          </p>

          <div class="flex gap-2 actions-row">
            <AppButton
              size="sm"
              variant="danger"
              icon="trash-2"
              @click="remove(s)"
            >
              {{ confirmingDelete === s.id ? '确认删除？' : '删除' }}
            </AppButton>
          </div>
        </AppCard>

        <!-- 添加卡（固定最后）：虚线占位块仅作入口，表单走弹窗、不影响网格布局 -->
        <button class="add-tile" type="button" @click="openAdd">
          <AppIcon name="plus" :size="22" />
          <span>添加瓦片源</span>
        </button>
      </div>

      <AppModal v-if="adding" title="添加瓦片源" @close="adding = false">
        <form class="flex" style="flex-direction: column; gap: 12px" @submit.prevent="create">
          <div class="form-group" style="margin: 0">
            <label class="form-label">类型</label>
            <select v-model="newType" class="form-select">
              <option value="public">公开瓦片服务（XYZ 模板）</option>
              <option value="tianditu">天地图（密钥认证）</option>
            </select>
          </div>

          <div v-if="newType === 'public'" class="form-group" style="margin: 0">
            <label class="form-label">瓦片地址模板（含 {z}/{x}/{y} 占位）</label>
            <input v-model="newTemplate" class="form-input" required placeholder="https://map.example.com/{z}/{x}/{y}.png" />
            <p class="form-hint">示例：https://map.lw1314.site/{z}/{x}/{y}.png</p>
          </div>
          <template v-else>
            <div class="form-group" style="margin: 0">
              <label class="form-label">天地图密钥（tk，必填）</label>
              <input v-model="newKey" class="form-input" type="password" required placeholder="粘贴天地图 Key" />
            </div>
            <div class="form-group" style="margin: 0">
              <label class="form-label">底图类型</label>
              <select v-model="newLayer" class="form-select">
                <option value="vec_w">矢量底图（vec_w）</option>
                <option value="img_w">影像底图（img_w）</option>
                <option value="ter_w">地形晕渲（ter_w）</option>
              </select>
            </div>
          </template>
        </form>
        <template #footer>
          <span class="form-hint" style="margin-right: auto">新源默认不启用</span>
          <AppButton variant="secondary" icon="x" type="button" @click="adding = false">取消</AppButton>
          <AppButton variant="primary" icon="plus" type="button" :disabled="creating" @click="create">
            {{ creating ? '创建中…' : '创建' }}
          </AppButton>
        </template>
      </AppModal>
    </div>
  </div>
</template>

<style scoped>
.source-grid {
  margin-top: 16px;
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  grid-auto-rows: 1fr; /* 所有行等高：单列/多列下卡片大小一致 */
  gap: 16px;
}
/* 等高：网格默认 stretch 拉伸同行情（card 高度保持 auto），body 弹性列、操作行贴底 */
.source-grid :deep(.card) {
  display: flex;
  flex-direction: column;
}
.source-grid :deep(.card-body) {
  flex: 1;
  display: flex;
  flex-direction: column;
}
.source-grid .actions-row {
  margin-top: auto;
  padding-top: 12px;
}
.source-line {
  margin: 0;
  font-size: 13px;
  color: var(--color-text-secondary);
  word-break: break-all;
}
.add-tile {
  min-height: 140px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  border: 1.5px dashed var(--color-border);
  border-radius: var(--radius-lg);
  background: transparent;
  color: var(--color-text-tertiary);
  font-size: 14px;
  cursor: pointer;
}
.add-tile:hover {
  border-color: var(--color-accent);
  color: var(--color-accent);
}
</style>
