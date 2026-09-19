<script setup lang="ts">
// 地图「未配置瓦片源」覆盖提示层：MapCanvas 与 MiniMapPicker 共用一份文案与样式。
// 由父容器提供 position: relative，本组件绝对铺满并居中提示卡。
import { Map as MapGlyph } from 'lucide-vue-next'
</script>

<template>
  <div class="map-nosource">
    <div class="ns-card">
      <MapGlyph :size="26" stroke-width="1.6" />
      <p class="ns-title">未配置地图服务</p>
      <p class="ns-sub">管理员请到 后台 → 外部集成 → 地图配置 添加并启用瓦片源</p>
    </div>
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
.ns-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  text-align: center;
  color: var(--muted-ink);
}
.ns-title {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
  color: var(--ink);
}
.ns-sub {
  margin: 0;
  font-size: 12px;
  line-height: 1.5;
  max-width: 240px;
}
</style>
