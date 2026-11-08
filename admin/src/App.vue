<script setup lang="ts">
import { computed } from 'vue';
import { NConfigProvider, darkTheme } from 'naive-ui';
import type { WatermarkProps } from 'naive-ui';
import { naiveDateLocales, naiveLocales } from './locales/naive';
import { watchEffect } from 'vue';
import { useAppStore } from './store/modules/app';
import { injectCssVars } from './tokens';
import { useThemeStore } from './store/modules/theme';

defineOptions({
  name: 'App'
});

const appStore = useAppStore();
const themeStore = useThemeStore();

// 嫁接视图的 CSS 变量令牌跟随明暗主题（模板主题抽屉切换时重注入）
watchEffect(() => {
  injectCssVars(themeStore.darkMode);
});

const naiveDarkTheme = computed(() => (themeStore.darkMode ? darkTheme : undefined));

const naiveLocale = computed(() => {
  return naiveLocales[appStore.locale];
});

const naiveDateLocale = computed(() => {
  return naiveDateLocales[appStore.locale];
});

const watermarkProps = computed<WatermarkProps>(() => {
  return {
    content: themeStore.watermarkContent,
    cross: true,
    fullscreen: true,
    fontSize: 16,
    lineHeight: 16,
    width: 384,
    height: 384,
    xOffset: 12,
    yOffset: 60,
    rotate: -15,
    zIndex: 9999
  };
});
</script>

<template>
  <NConfigProvider
    :theme="naiveDarkTheme"
    :theme-overrides="themeStore.naiveTheme"
    :locale="naiveLocale"
    :date-locale="naiveDateLocale"
    class="h-full"
  >
    <AppProvider>
      <RouterView class="bg-layout" />
      <NWatermark v-if="themeStore.watermark.visible" v-bind="watermarkProps" />
    </AppProvider>
  </NConfigProvider>
</template>

<style scoped></style>
