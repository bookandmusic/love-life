<script setup lang="ts">
// 地图「未配置瓦片源」覆盖提示层：保留绝对铺满定位，内容复用统一的 EmptyState。
import { Map as MapGlyph } from 'lucide-vue-next'
import EmptyState from './EmptyState.vue'
</script>

<template>
  <div class="map-nosource">
    <EmptyState compact :icon="MapGlyph" title="地图还差一块底图" sub="到 后台 → 外部集成 → 地图配置 启用瓦片源" />
  </div>
</template>

<style scoped>
.map-nosource {
  position: absolute;
  inset: 0;
  z-index: 6;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 16px;
  background: rgba(238, 243, 232, 0.92);
}
:root[data-theme='dark'] .map-nosource {
  background: rgba(28, 28, 30, 0.92);
}
</style>
