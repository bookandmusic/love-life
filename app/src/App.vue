<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { KEYS } from './utils/storage'
import { applyWindowState } from './utils/windowState'
import { ACCENTS } from './utils/accents'
import { probeAndSelect } from './utils/netProbe'
import { startMediaRefresh } from './utils/mediaUrls'
import { startTileGrantRefresh } from './utils/mapTiles'

const router = useRouter()

// 主题 + 强调色初始化（设置页可改；深浅跟随系统仅作可选项，不随环境暗色自动变深）
function applyStoredAppearance() {
  const theme = localStorage.getItem(KEYS.THEME) || 'light'
  const dark = theme === 'dark' || (theme === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches)
  document.documentElement.dataset.theme = dark ? 'dark' : 'light'
  const accent = localStorage.getItem(KEYS.ACCENT) || 'rose'
  if (ACCENTS[accent]) document.documentElement.dataset.accent = accent
}

let probeTimer: number | undefined
// 具名引用才能解绑：此前匿名回调无法移除，卸载后仍会监听系统主题变化
const media = window.matchMedia('(prefers-color-scheme: dark)')
function onSystemThemeChange() {
  if ((localStorage.getItem(KEYS.THEME) || 'light') === 'system') applyStoredAppearance()
}

onMounted(() => {
  applyStoredAppearance()
  media.addEventListener('change', onSystemThemeChange)
  router.afterEach(() => applyStoredAppearance())
  void applyWindowState()
  // 双地址定时重探测（AP1）：在家/在外网络切换后自动优选内网
  void probeAndSelect()
  probeTimer = window.setInterval(() => void probeAndSelect(), 5 * 60 * 1000)
  // 媒体签名 URL 定时重签（AP3）：15 分钟 TTL 临期前静默换新
  startMediaRefresh()
  // 地图瓦片短时签名定时换新（DB3）：30 分钟 TTL，每 10 分钟静默重取
  startTileGrantRefresh()
})

onUnmounted(() => {
  if (probeTimer !== undefined) clearInterval(probeTimer)
  media.removeEventListener('change', onSystemThemeChange)
})
</script>

<template>
  <router-view v-slot="{ Component }">
    <Transition name="page-fade" mode="out-in">
      <component :is="Component" />
    </Transition>
  </router-view>
</template>

<style>
/* 设计令牌——iOS 风格（浅色=日常，深色=夜间；暖粉主色 + 系统浅灰底） */
:root,
:root[data-theme='light'] {
  /* 应用框架（外壳 / 独立整页 / 底部弹窗共用一套尺寸）：手机=整屏，桌面=浮动卡片 */
  --frame-max: 480px;
  --frame-inset: 0px;
  --frame-radius: 0px;
  --frame-shadow: none;
  --col-max: 480px;
  --bg: #f2f2f7;
  --surface: #ffffff;
  --ink: #1c1c1e;
  --muted-ink: #6c6c70; /* iOS secondaryLabel 等效：白底 5.23:1（AA），原 #8e8e93 仅 3.26:1 */
  --primary: #ff8fab;
  --primary-deep: #f06292;
  /* 操作色（REQUIREMENTS:285/293）：文字/图标型可点控件的 tint（导航栏按钮、保存/发布、文字动作）。
     与 --primary 分工——暖粉只管着色点/FAB/选中（填充型按钮），操作类用系统蓝保证白底对比度 4.0:1 */
  --tint: #007aff;
  --tint-wash: rgba(0, 122, 255, 0.1);
  --danger: #ff3b30;
  --rule: rgba(60, 60, 67, 0.18);
  --on-primary: #ffffff;
  --ai-bg: #f2f2f7;
  --app-veil: #f2f2f7;
  --glass-from: rgba(249, 249, 252, 0.94);
  --glass-to: rgba(246, 246, 250, 0.88);
  --glass-edge: rgba(60, 60, 67, 0.14);
  --glass-inset: rgba(255, 255, 255, 0.5);
  --shadow: 0 1px 3px rgba(0, 0, 0, 0.06);
  --sk: rgba(120, 120, 128, 0.12);
  /* 语义色（iOS 语义 + 情侣暖色，深浅各一份） */
  --sem-blue: #5aa7d4;
  --sem-green: #34c759;
  --sem-gold: #e8a800;
  --sem-purple: #af52de;
  --sem-gold-soft: rgba(255, 209, 102, 0.22);
  --sem-green-soft: rgba(52, 199, 89, 0.14);
}

:root[data-theme='dark'] {
  --bg: #000000;
  --surface: #1c1c1e;
  --ink: #f2f2f7;
  --muted-ink: #98989f;
  --primary: #ff8fab;
  --primary-deep: #ffb4c2;
  --tint: #0a84ff;
  --tint-wash: rgba(10, 132, 255, 0.16);
  --danger: #ff453a;
  --rule: rgba(84, 84, 88, 0.65);
  --on-primary: #ffffff;
  --ai-bg: #000000;
  --app-veil: #000000;
  --glass-from: rgba(36, 36, 40, 0.94);
  --glass-to: rgba(26, 26, 28, 0.88);
  --glass-edge: rgba(255, 255, 255, 0.12);
  --glass-inset: rgba(255, 255, 255, 0.07);
  --shadow: 0 1px 3px rgba(0, 0, 0, 0.35);
  --sk: rgba(120, 120, 128, 0.2);
  /* 语义色（深色） */
  --sem-blue: #6db9e8;
  --sem-green: #30d158;
  --sem-gold: #ffd166;
  --sem-purple: #bf5af2;
  --sem-gold-soft: rgba(255, 209, 102, 0.16);
  --sem-green-soft: rgba(48, 209, 88, 0.16);
}

/* 强调色（暖色系预设色板）——阶段4.4；覆盖主粉/深档粉，深浅各一份。
   单一事实源：app/src/utils/accents.ts（Settings 色板 label/swatch 取自该处） */
:root[data-accent='rose']          { --primary: #ff8fab; --primary-deep: #f06292; }
:root[data-accent='peach']         { --primary: #ff9f7a; --primary-deep: #e35f3d; }
:root[data-accent='gold']          { --primary: #f2b441; --primary-deep: #cf8a1c; }
:root[data-accent='dousha']        { --primary: #e0786e; --primary-deep: #b94d4d; }
:root[data-accent='rose'][data-theme='dark']   { --primary: #ff8fab; --primary-deep: #ffb4c2; }
:root[data-accent='peach'][data-theme='dark']  { --primary: #ff9d82; --primary-deep: #ffc4a6; }
:root[data-accent='gold'][data-theme='dark']   { --primary: #f2b441; --primary-deep: #ffd98b; }
:root[data-accent='dousha'][data-theme='dark'] { --primary: #e0877f; --primary-deep: #f0b0a9; }

/* 多设备响应式（REQUIREMENTS §6.7）：平板档 100% 全宽带内边距；桌面档切浮动面板。
   :root:root 提升优先级以压过 :root[data-theme] 主题块 */
:root:root {
  --side-pad: 16px;
}
@media (min-width: 768px) {
  :root:root {
    --col-max: 100%;    /* 平板：内容列不限制（侧内边距由视图自管） */
    --frame-max: 100%;  /* 平板：外壳/独立页全宽，与 APP 同宽 */
    --side-pad: 24px;
  }
}
@media (min-width: 1280px) {
  :root:root {
    --col-max: 720px;   /* 桌面：内容列上限 */
    --frame-max: 832px; /* 桌面：浮动卡片上限（MainView 外壳与独立整页同宽） */
    --frame-inset: 16px; /* 桌面：卡片与视口的外距（上下同值） */
    --frame-radius: 18px;
    --frame-shadow: 0 20px 60px rgba(0, 0, 0, 0.12), 0 2px 8px rgba(0, 0, 0, 0.06);
    --side-pad: 0px;    /* 桌面：侧内边距由浮窗外距承担 */
  }
}

/* 独立整页（登录/通知/AI 对话/设置）：与主壳同一套框架令牌，桌面档切浮动卡片
   （宽度/外距/圆角/阴影全部取 --frame-*，保证登录卡片与登录后的 APP 卡片逐像素一致） */
.page-col {
  width: 100%;
  max-width: var(--frame-max);
  margin-inline: auto;
}
@media (min-width: 1280px) {
  /* 三类名提权：各页 scoped 的 height:100%（.settings-page[data-v-x] = 0,2,0）会压过
     .page-col（0,1,0），桌面档卡片因此被撑到贴住视口下边缘（只剩上外距）。0,3,0 稳赢。 */
  .page-col.page-col.page-col {
    height: calc(100% - 2 * var(--frame-inset));
    margin: var(--frame-inset) auto;
    background: var(--surface);
    border-radius: var(--frame-radius);
    box-shadow: var(--frame-shadow);
    overflow: hidden;
  }
}

html,
body,
#app {
  height: 100%;
  margin: 0;
  color: var(--ink);
  font-family: -apple-system, BlinkMacSystemFont, 'SF Pro SC', 'PingFang SC', 'Helvetica Neue', system-ui, sans-serif;
  -webkit-font-smoothing: antialiased;
}

/* 导航栏标题居中（iOS HIG 内联标题） */
.navbar .title {
  position: absolute;
  left: 50%;
  transform: translateX(-50%);
  white-space: nowrap;
}
.navbar .nav-actions {
  margin-left: auto;
}

/* 长按目标（低频操作入口）：按住即有反馈，并禁掉系统文本选择/呼出菜单。
   反馈用 .pressing 类（由 useLongPress 驱动），不用 :active——否则点卡片里的按钮时整卡会闪灰 */
.pressable {
  user-select: none;
  -webkit-user-select: none;
  -webkit-touch-callout: none;
}
.pressable.pressing {
  background: var(--sk);
}

/* 全局锁定 chrome 滚动/拖动：仅各视图内容区可滚动，body/主壳/侧栏等永不出滚动条或拖动回弹 */
html,
body,
#app {
  overflow: hidden;
  overscroll-behavior: none;
}

/* iOS 手感：全局隐藏滚动条（保留滚动能力） */
* {
  scrollbar-width: none;
}
*::-webkit-scrollbar {
  display: none;
}

body {
  word-break: break-word;
  background: var(--bg);
}

:root[data-theme='dark'] body {
  background: var(--bg);
}

#app {
  background: transparent;
}

.page-fade-enter-active,
.page-fade-leave-active {
  transition: opacity 0.28s ease;
}
.page-fade-enter-from,
.page-fade-leave-to {
  opacity: 0;
}

@keyframes breathe {
  0%,
  100% {
    opacity: 0.45;
  }
  50% {
    opacity: 1;
  }
}

.num {
  font-variant-numeric: tabular-nums;
  letter-spacing: -0.02em;
}

/* iOS 导航玻璃：近乎不透明的毛玻璃，高光克制（去掉 brightness/彩色光感） */
.glass {
  position: relative;
  background: linear-gradient(180deg, var(--glass-from), var(--glass-to));
  backdrop-filter: saturate(180%) blur(20px);
  -webkit-backdrop-filter: saturate(180%) blur(20px);
  border-bottom: 0.5px solid var(--glass-edge);
  /* 不设外投阴影：该阴影恰好画在下方滚动容器顶部，跨合成层混合，
     KeepAlive 切换重建滚动层时闪烁（「内容区顶部黑影晃动」）。
     仅保留 inset 高光与边线，静态视觉几乎无差。 */
  box-shadow: inset 0 1px 0 var(--glass-inset);
  overflow: hidden;
}
.glass::after {
  content: '';
  position: absolute;
  inset: 0;
  pointer-events: none;
  background: linear-gradient(115deg, rgba(255, 255, 255, 0.28) 0%, rgba(255, 255, 255, 0.08) 30%, transparent 55%);
  opacity: 0.35;
}
:root[data-theme='dark'] .glass::after {
  opacity: 0.18;
}

/* iOS 分组卡片：实心白色表面（非玻璃），只保留细分割线与软阴影 */
.group {
  background: var(--surface);
  border-radius: 12px;
  overflow: hidden;
  box-shadow: var(--shadow), inset 0 1px 0 rgba(255, 255, 255, 0.8);
}
:root[data-theme='dark'] .group {
  background: #1c1c1e;
  box-shadow: var(--shadow), inset 0 1px 0 rgba(255, 255, 255, 0.06);
}
.group .row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 13px 16px;
}
.group .row + .row {
  border-top: 0.5px solid var(--rule);
}

/* 表单页 inset grouped 行（纪念日 / 添加地点共用）：与 .group .row 同族，
   行内为「标签 + 值/控件」，行间 0.5px 分割线；行高下限 48px（触控目标） */
.form-rows {
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
}
.frow {
  display: flex;
  align-items: center;
  gap: 12px;
  min-height: 48px;
  padding: 10px 14px;
  box-sizing: border-box;
}
.frow + .frow {
  border-top: 0.5px solid var(--rule);
}
/* 可点整行（点开 sheet/选择器） */
.frow-tap {
  border: none;
  background: none;
  font-family: inherit;
  text-align: left;
  cursor: pointer;
  width: 100%;
}
.frow-tap:active {
  background: rgba(120, 120, 128, 0.08);
}
.rlabel {
  font-size: 15px;
  color: var(--ink);
  flex-shrink: 0;
}
/* 行值：iOS 惯例用中性色（属性值不着色），动作/可点控件才用 --tint */
.rvalue {
  margin-left: auto;
  font-size: 15px;
  color: var(--muted-ink);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.row-select {
  border: none;
  background: none;
  padding: 4px 0;
  font-size: 15px;
  font-family: inherit;
  color: var(--muted-ink);
  text-align: right;
  cursor: pointer;
  margin-left: auto;
  max-width: 170px;
}
/* 行内文本输入（无边框，占满余量） */
.cell-input {
  flex: 1;
  min-width: 0;
  border: none;
  background: none;
  padding: 0;
  font-size: 15px;
  color: var(--ink);
  font-family: inherit;
}
.cell-input:focus {
  outline: none;
}
.cell-input::placeholder {
  color: var(--muted-ink);
}

button,
input,
textarea {
  font-family: inherit;
}
</style>
