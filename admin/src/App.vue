<script setup lang="ts">
import AppToast from './components/AppToast.vue'
</script>

<template>
  <router-view />
  <!-- 全局轻提示宿主：挂在应用根，登录页与 LayoutView 内页面共用同一实例 -->
  <AppToast />
</template>
