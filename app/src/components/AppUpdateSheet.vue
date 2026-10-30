<script setup lang="ts">
// 应用内更新弹窗：按 useAppUpdate 状态机渲染 发现新版/下载进度/就绪安装/失败/交接 各态。
// 常驻单例（App.vue 挂载一次），强更态无「稍后」且蒙层不可关。
import { computed, ref, watch } from 'vue'
import Sheet from './Sheet.vue'
import { humanBytes } from '../utils/appUpdate'
import { useAppUpdate } from '../composables/useAppUpdate'

const {
  state,
  release,
  progress,
  errorMsg,
  needsPermission,
  isForce,
  dismiss,
  startDownload,
  retryDownload,
  cancelDownload,
  install,
} = useAppUpdate()

const SHOW_STATES = ['prompt', 'downloading', 'ready', 'failed', 'handoff']
/** 下载中可收起弹窗（后台继续）；状态切换时复位 */
const collapsed = ref(false)
watch(state, () => {
  collapsed.value = false
})

const show = computed(
  () => SHOW_STATES.includes(state.value) && !(state.value === 'downloading' && collapsed.value)
)
const force = computed(() => isForce())

const title = computed(() => {
  switch (state.value) {
    case 'prompt':
      return `发现新版本 v${release.value?.versionName ?? ''}`
    case 'downloading':
      return '正在下载更新'
    case 'ready':
      return '更新已就绪'
    case 'failed':
      return '更新失败'
    case 'handoff':
      return '正在打开安装器'
    default:
      return ''
  }
})
const confirmText = computed(() => {
  switch (state.value) {
    case 'prompt':
      return '立即更新'
    case 'downloading':
      return '取消下载'
    case 'ready':
      return '立即安装'
    case 'failed':
      return '重试'
    case 'handoff':
      return '好'
    default:
      return ''
  }
})
const cancelText = computed(() => (state.value === 'downloading' ? '后台下载' : '稍后'))
const hideCancel = computed(() => force.value && state.value !== 'downloading')
const pct = computed(() => {
  const { received, total } = progress.value
  return total > 0 ? Math.min(100, Math.round((received / total) * 100)) : 0
})
const sizeHint = computed(() => humanBytes(progress.value.total ?? release.value?.sizeBytes ?? 0))

function onConfirm() {
  switch (state.value) {
    case 'prompt':
      void startDownload()
      break
    case 'downloading':
      void cancelDownload()
      break
    case 'ready':
      void install()
      break
    case 'failed':
      void retryDownload() // 重新取一次清单（版本信息与下载地址都可能已变）再续传
      break
    case 'handoff':
      state.value = 'idle'
      break
  }
}
function onCancel() {
  switch (state.value) {
    case 'downloading':
      collapsed.value = true // 后台下载：只收起弹窗，状态机不动
      return
    case 'failed':
    case 'handoff':
      state.value = 'idle' // 失败/交接后的「稍后」不写记忆，下次启动仍会提示
      return
    default:
      dismiss() // prompt 的「稍后」：记忆 versionCode，同版本不再弹
  }
}
</script>

<template>
  <Sheet
    :show="show"
    :title="title"
    :confirm-text="confirmText"
    :cancel-text="cancelText"
    :hide-cancel="hideCancel"
    :confirm-danger="state === 'downloading'"
    @update:show="(v) => { if (!v) onCancel() }"
    @cancel="onCancel"
    @confirm="onConfirm"
  >
    <!-- 发现新版：更新说明 + 下载体积（外网下载前知情） -->
    <template v-if="state === 'prompt'">
      <ul v-if="release?.notes?.length" class="notes">
        <li v-for="(n, i) in release.notes" :key="i">{{ n }}</li>
      </ul>
      <p v-if="sizeHint" class="msg muted">安装包大小 {{ sizeHint }}</p>
    </template>

    <!-- 下载中：进度条 + 后台提示 -->
    <template v-else-if="state === 'downloading'">
      <div class="bar" role="progressbar" :aria-valuenow="pct" aria-valuemin="0" aria-valuemax="100">
        <div class="fill" :style="{ width: pct + '%' }" />
      </div>
      <p class="msg num">{{ pct }}%</p>
      <p class="msg muted">可关闭此弹窗，下载在后台继续，完成后会通知提醒</p>
    </template>

    <!-- 就绪安装 -->
    <template v-else-if="state === 'ready'">
      <p class="msg">下载完成并通过校验，点击交给系统安装器安装。</p>
      <p v-if="needsPermission" class="msg muted">需要「安装未知应用」授权，请允许后返回。</p>
    </template>

    <!-- 失败：错误信息 + 可重试 -->
    <template v-else-if="state === 'failed'">
      <p class="msg">{{ errorMsg }}</p>
      <p class="msg muted">中断的下载会自动续传。</p>
    </template>

    <!-- 已交给系统安装器 -->
    <template v-else-if="state === 'handoff'">
      <p class="msg">请在系统安装器中确认安装，完成后应用会重启为新版本。</p>
    </template>
  </Sheet>
</template>

<style scoped>
.notes {
  margin: 0;
  padding-left: 18px;
  font-size: 14px;
  line-height: 1.6;
  color: var(--ink);
}
.msg { font-size: 14px; line-height: 1.5; color: var(--ink); text-align: center; margin: 0; }
.msg.muted { color: var(--ink-3, var(--ink)); opacity: 0.72; }
.msg.num { font-variant-numeric: tabular-nums; }
.bar {
  height: 8px;
  border-radius: 999px;
  background: var(--sk, rgba(127, 127, 127, 0.24));
  overflow: hidden;
}
.fill { height: 100%; border-radius: 999px; background: var(--primary); transition: width 0.2s ease; }
</style>
