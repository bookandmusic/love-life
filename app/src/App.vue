<script setup lang="ts">
import { onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { KEYS, getItem } from './utils/storage'
import { applyWindowState } from './utils/windowState'
import { ACCENTS } from './utils/accents'

const router = useRouter()

// 主题 + 强调色初始化（设置页可改；深浅跟随系统仅作可选项，不随环境暗色自动变深）
function applyStoredAppearance() {
  const theme = getItem(KEYS.THEME) || 'light'
  const dark = theme === 'dark' || (theme === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches)
  document.documentElement.dataset.theme = dark ? 'dark' : 'light'
  const accent = getItem(KEYS.ACCENT) || 'rose'
  if (ACCENTS[accent]) document.documentElement.dataset.accent = accent
}

onMounted(() => {
  applyStoredAppearance()
  window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
    if ((localStorage.getItem(KEYS.THEME) || 'light') === 'system') applyStoredAppearance()
  })
  router.afterEach(() => applyStoredAppearance())
  void applyWindowState()
})
</script>

<template>
  <!--
    THESIS: 一对情侣的日常记录——用 iOS 原生语言承载：毛玻璃导航栏、系统 tab bar、
    列表式内容、克制的动效。回归「正常、温馨、好用」的情侣记录 app。
    OWN-WORLD: 系统浅灰底（#f2f2f7）+ 白色卡片/列表 + 暖粉主色（#ff8fab 情感数据/强调）
    + iOS 语义蓝（操作）+ 深红（破坏）；-apple-system 字体栈；液态玻璃
    （backdrop-filter: blur + saturate + 彩色光斑）+ 0.5px 分割线；圆角 10-16。
    STORY: 使用者觉得「这就是一个认真做记录的情侣 app」——不奇怪、不花哨、每件事都顺手。
    FIRST VIEWPORT: 毛玻璃导航栏（标题+右侧通知/设置按钮）、内容区（在一起天数卡+纪念日+
    回忆精选）、底部 iOS tab bar（首页/动态/相册/纪念日/地图）、右下角 FAB。
    FORM: iOS Human Interface Guidelines（用户已定：iOS 风格、5 tab + 右下角 FAB、
    页头通知/设置按钮、地图瓦片+联动+缩放）。
    FINISH: unreviewed and undocumented is unfinished; this build ends
    with the finish review, the verdict, and DESIGN.md
  -->
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
  --col-max: 480px;
  --sheet-max: 480px;
  --bg: #f2f2f7;
  --surface: #ffffff;
  --ink: #1c1c1e;
  --muted-ink: #8e8e93;
  --primary: #ff8fab;
  --primary-deep: #f06292;
  --danger: #ff3b30;
  --ios-blue: #007aff;
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
  --danger: #ff453a;
  --ios-blue: #0a84ff;
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

/* 强调色（暖色系预设色板）——阶段4.4；覆盖主粉/深档粉，深浅各一份 */
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
    --col-max: 1023px; /* 平板：不限制列宽（侧内边距由视图自管） */
    --side-pad: 24px;
  }
}
@media (min-width: 1280px) {
  :root:root {
    --col-max: 720px;  /* 桌面：内容列上限，浮窗由 MainView 承载 */
    --side-pad: 0px;   /* 桌面：侧内边距由浮窗外距承担 */
  }
}

/* 独立整页（登录/通知/AI 对话/设置）：与主壳同宽策略，桌面档切浮动面板 */
.page-col {
  width: 100%;
  max-width: var(--col-max);
  margin-inline: auto;
}
@media (min-width: 1280px) {
  .page-col {
    max-width: 720px;
    height: calc(100% - 32px);
    margin: 16px auto;
    background: var(--surface);
    border-radius: 18px;
    box-shadow: 0 20px 60px rgba(0, 0, 0, 0.12), 0 2px 8px rgba(0, 0, 0, 0.06);
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
  box-shadow: 0 1px 12px rgba(0, 0, 0, 0.05), inset 0 1px 0 var(--glass-inset);
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

button,
input,
textarea {
  font-family: inherit;
}
</style>
