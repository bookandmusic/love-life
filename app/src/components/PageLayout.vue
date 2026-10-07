<script setup lang="ts">
// 统一页面外壳：navbar（玻璃、标题居中、可选返回/右上操作）+ 内容区（统一列宽/内边距/滚动/安全区）。
// 三档响应式由 App.vue 的 --col-max / --side-pad token 自动生效（手机 480 居中 / 平板全宽 24 内边距 / 桌面浮窗内 720 居中）。
// 外层桌面浮窗（.main / .page-col）由 MainView / App 负责，本组件只管页面内层结构。
import { onActivated, ref } from 'vue'
import { ChevronLeft } from 'lucide-vue-next'
import NavActions from './NavActions.vue'

withDefaults(
  defineProps<{
    title: string
    back?: boolean
    actions?: boolean
    // 内容区是否可滚动：地图/对话等「填充不滚动」的页面传 false
    scroll?: boolean
    // 独立整页（通知/设置/对话）：桌面档套用 .page-col 浮动卡片框架
    standalone?: boolean
  }>(),
  { back: false, actions: false, scroll: true, standalone: false },
)

// 返回按钮交回页面决定动作（列表/详情 router.back()、表单 closeForm()）；
// 另转发内容区滚动（供无限滚动页），内部 passive 原生监听不阻主线程
defineEmits<{ (e: 'back'): void; (e: 'scroll', ev: Event): void }>()

// 滚动位置记忆：KeepAlive 重新激活时滚动容器 scrollTop 可能被重置（表现为切换“晃”），
// 记住离开前的 scrollTop 并在激活帧同步写回，避免跳变。地图等无滚动页不受影响。
const pageBody = ref<HTMLElement | null>(null)
let storedScroll = 0
function onBodyScroll(ev: Event) {
  storedScroll = (ev.target as HTMLElement).scrollTop
}
onActivated(() => {
  // 激活帧同步写回 scrollTop：同一帧内浏览器只会按最终值绘制一次，
  // 避免「先按 0 绘制、下一帧跳到恢复值」的两帧跳动。
  if (pageBody.value && pageBody.value.scrollTop !== storedScroll) {
    pageBody.value.scrollTop = storedScroll
  }
})
</script>

<template>
  <div class="page" :class="{ 'page-col': standalone }">
    <header class="navbar glass">
      <!-- 左侧默认渲染返回按钮；提供 nav-left 插槽的页面（如 AI 对话的会话抽屉）整侧自管 -->
      <slot name="nav-left">
        <button v-if="back" class="nav-btn" aria-label="返回" @click="$emit('back')">
          <ChevronLeft :size="22" stroke-width="2.5" />
        </button>
      </slot>
      <span class="title">{{ title }}</span>
      <span class="nav-right">
        <slot name="nav-right" />
        <NavActions v-if="actions" />
      </span>
    </header>
    <slot name="subheader" />
    <div ref="pageBody" class="page-body" :class="{ 'no-scroll': !scroll }" @scroll.passive="onBodyScroll($event); $emit('scroll', $event)">
      <slot />
    </div>
    <slot name="footer" />
    <!-- 页内浮层（会话抽屉等）：锚定本页框架而非浏览器视口——桌面档落在浮动卡片内、随卡片圆角裁切 -->
    <slot name="overlay" />
  </div>
</template>

<style scoped>
.page {
  height: 100%;
  display: flex;
  flex-direction: column;
  min-height: 0;
  /* overlay 插槽（absolute 浮层）的定位基准；relative 不影响布局与 fixed 子元素 */
  position: relative;
  /* 页面底色钩子：页面经 class 注入 --page-bg（如 AI 对话页注入 var(--ai-bg)）；
     未注入时 transparent，保持全局 body 底色不变 */
  background: var(--page-bg, transparent);
}
@media (min-width: 1280px) {
  /* 桌面档独立整页卡片底色同样吃 --page-bg；三类名追平全局 .page-col.page-col.page-col(0,3,0)，
     未注入时回落 var(--surface)，与全局规则一致 */
  .page.page-col.page-col {
    background: var(--page-bg, var(--surface));
  }
}
.navbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  /* 桌面档 --side-pad 为 0（内容内边距由浮窗外距承担），但导航栏图标贴卡片边缘会显挤，
     给导航栏保底 12px；手机/平板档 --side-pad(16/24px) 大于保底值，取原值不变 */
  padding: calc(10px + env(safe-area-inset-top)) max(12px, var(--side-pad)) 10px;
  flex-shrink: 0;
  z-index: 10;
}
.nav-btn {
  border: none;
  background: none;
  color: var(--tint);
  cursor: pointer;
  padding: 4px;
  display: flex;
  align-items: center;
}
.title {
  font-size: 17px;
  font-weight: 600;
}
.nav-right {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-left: auto;
}
.page-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  overscroll-behavior: contain;
  /* 必须 border-box：本组件同时声明 width:100% 与 padding-inline，
     全局未设 border-box 时 content-box 会让内容区比视口宽 2×side-pad（手机 32px、平板 48px），
 表现为整页内容右移、右侧被裁。横向 padding 只归本组件。 */
  box-sizing: border-box;
  width: 100%;
  max-width: var(--col-max);
  margin-inline: auto;
  padding-inline: var(--side-pad);
  padding-top: 12px;
  padding-bottom: calc(16px + env(safe-area-inset-bottom));
}
.page-body.no-scroll {
  overflow: hidden;
  display: flex;
  flex-direction: column;
}</style>
