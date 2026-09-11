<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Home, Newspaper, Images, Calendar, Map, Plus, PenLine, MapPin, Cake, FolderPlus } from 'lucide-vue-next'

const router = useRouter()
const route = useRoute()
const fabOpen = ref(false)

const chromeHidden = computed(() =>
  route.path.startsWith('/publish') || /\/posts\/\d+/.test(route.path) || /\/albums\/\d+/.test(route.path),
)

const addItems = [
  { label: '动态', icon: PenLine, color: 'var(--primary)', action: () => router.push('/publish') },
  { label: '地点', icon: MapPin, color: 'var(--sem-green)', action: () => router.push('/map?new=1') },
  { label: '纪念日', icon: Cake, color: 'var(--sem-gold)', action: () => router.push('/anniversaries?new=1') },
  { label: '相册', icon: FolderPlus, color: 'var(--sem-purple)', action: () => router.push('/albums?new=1') },
]

function toggleFab() {
  fabOpen.value = !fabOpen.value
}
function addClick(fn: () => void) {
  fabOpen.value = false
  fn()
}
</script>

<template>
  <div class="main">
    <router-view />

    <template v-if="!chromeHidden">
      <div v-if="fabOpen" class="fab-backdrop" @click="fabOpen = false" />
      <div class="fab-menu" :class="{ show: fabOpen }">
        <button v-for="(it, i) in addItems" :key="it.label" class="fab-item" :style="{ animationDelay: `${0.02 + i * 0.03}s` }" @click="addClick(it.action)">
          <span class="fab-ic" :style="{ background: it.color }">
            <component :is="it.icon" :size="14" stroke-width="2" />
          </span>
          {{ it.label }}
        </button>
      </div>
      <button class="fab" :class="{ open: fabOpen }" aria-label="添加" @click="toggleFab">
        <Plus :size="26" stroke-width="2" />
      </button>

      <nav class="tabbar glass">
        <RouterLink to="/home" class="tab">
          <Home :size="22" stroke-width="1.8" />
          <span>首页</span>
        </RouterLink>
        <RouterLink to="/posts" class="tab">
          <Newspaper :size="22" stroke-width="1.8" />
          <span>动态</span>
        </RouterLink>
        <RouterLink to="/albums" class="tab">
          <Images :size="22" stroke-width="1.8" />
          <span>相册</span>
        </RouterLink>
        <RouterLink to="/anniversaries" class="tab">
          <Calendar :size="22" stroke-width="1.8" />
          <span>纪念日</span>
        </RouterLink>
        <RouterLink to="/map" class="tab">
          <Map :size="22" stroke-width="1.8" />
          <span>地图</span>
        </RouterLink>
      </nav>
    </template>
  </div>
</template>

<style scoped>
.main {
  height: 100%;
  display: flex;
  flex-direction: column;
  position: relative;
  max-width: var(--frame-max);
  margin: 0 auto;
  background: var(--app-veil);
}

.main > :deep(*) {
  flex: 1;
  min-height: 0;
}

.tabbar {
  display: flex;
  padding: 4px 0 calc(4px + env(safe-area-inset-bottom));
  z-index: 10;
  flex: 0 0 auto;
  border-bottom: none;
  border-top: 0.5px solid var(--glass-edge);
}
.tab {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 2px;
  font-size: 10px;
  text-decoration: none;
  color: var(--muted-ink);
  padding: 3px 0;
  -webkit-tap-highlight-color: transparent;
}
.tab.router-link-active {
  color: var(--primary-deep);
  font-weight: 600;
}
.tab.router-link-active svg {
  color: var(--primary-deep);
}

.fab {
  position: absolute;
  right: 18px;
  bottom: calc(86px + env(safe-area-inset-bottom));
  width: 52px;
  height: 52px;
  border-radius: 50%;
  background: var(--primary);
  color: var(--on-primary);
  border: none;
  cursor: pointer;
  z-index: 20;
  box-shadow: 0 6px 16px rgba(240, 98, 146, 0.35);
  display: flex;
  align-items: center;
  justify-content: center;
  transition: transform 0.2s;
}
.fab.open {
  transform: rotate(45deg);
}
.fab:active {
  transform: scale(0.92);
}

.fab-menu {
  position: absolute;
  right: 18px;
  bottom: calc(146px + env(safe-area-inset-bottom));
  display: none;
  flex-direction: column;
  gap: 10px;
  z-index: 20;
  align-items: flex-end;
}
.fab-menu.show {
  display: flex;
}
.fab-item {
  display: flex;
  align-items: center;
  gap: 10px;
  background: var(--surface);
  border-radius: 999px;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.12);
  border: 1px solid var(--rule);
  padding: 10px 16px 10px 10px;
  cursor: pointer;
  font-size: 14px;
  font-weight: 600;
  color: var(--ink);
  animation: fadeIn 0.18s ease backwards;
}
@keyframes fadeIn { from { opacity: 0; } to { opacity: 1; } }
:root[data-theme='dark'] .fab-item {
  background: #2c2c2e;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.4);
}
.fab-ic {
  width: 30px;
  height: 30px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  flex-shrink: 0;
}
.fab-backdrop {
  position: absolute;
  inset: 0;
  background: rgba(0, 0, 0, 0.25);
  z-index: 19;
}

/* 桌面档：浮动圆角面板（iOS Stage Manager 风格）—— 居中 + 圆角 + 大阴影 + 白底，
   内部切左导航栏 + 720 内容列；body 渐变透出画布感。断点 1280 含 iPad Pro 不被切到桌面。 */
@media (min-width: 1280px) {
  .main {
    flex-direction: row;
    width: calc(100% - 2 * var(--frame-inset));
    height: calc(100% - 2 * var(--frame-inset));
    margin: var(--frame-inset) auto;
    background: var(--surface);
    border-radius: var(--frame-radius);
    box-shadow: var(--frame-shadow);
    overflow: hidden;
  }
  .tabbar {
    flex: 0 0 92px;
    order: -1;
    flex-direction: column;
    gap: 14px;
    padding: 16px 0 calc(16px + env(safe-area-inset-bottom));
    border-top: none;
    border-right: 0.5px solid var(--glass-edge);
    border-radius: var(--frame-radius) 0 0 var(--frame-radius);
  }
  .tab {
    flex: 0 0 auto;
    padding: 6px 0;
  }
  .fab {
    right: 24px;
    bottom: 28px;
  }
  .fab-menu {
    right: 24px;
    bottom: 92px;
  }
}
</style>
