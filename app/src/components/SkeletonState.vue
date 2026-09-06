<script setup lang="ts">
// 骨架屏（统一组件）：variant = list(点+行) / lines(纯行) / grid(格子) / hero(首页大卡)
withDefaults(
  defineProps<{
    variant?: 'list' | 'lines' | 'grid' | 'hero'
    rows?: number
  }>(),
  { variant: 'list', rows: 3 },
)
</script>

<template>
  <div class="sk-body" :class="`sk-${variant}`">
    <template v-if="variant === 'hero'">
      <div class="sk-hero" />
      <div class="sk-title" />
      <div class="sk-cards">
        <div v-for="i in rows" :key="i" class="sk-card" />
      </div>
    </template>
    <template v-else-if="variant === 'grid'">
      <div v-for="i in rows" :key="i" class="sk-cell" />
    </template>
    <template v-else-if="variant === 'lines'">
      <div v-for="i in rows" :key="i" class="sk-row ln">
        <span class="sk-line" />
      </div>
    </template>
    <template v-else>
      <div v-for="i in rows" :key="i" class="sk-row">
        <span class="sk-dot" />
        <span class="sk-line" />
      </div>
    </template>
  </div>
</template>

<style scoped>
.sk-body {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.sk-row,
.sk-hero,
.sk-title,
.sk-card,
.sk-cell,
.sk-line,
.sk-dot {
  background: linear-gradient(100deg, rgba(120, 120, 128, 0.09) 40%, rgba(120, 120, 128, 0.2) 50%, rgba(120, 120, 128, 0.09) 60%);
  background-size: 200% 100%;
  animation: skpulse 1.2s linear infinite;
}
.sk-row {
  display: flex;
  align-items: center;
  gap: 10px;
}
.sk-dot {
  width: 32px;
  height: 32px;
  min-width: 32px;
  border-radius: 50%;
}
.sk-line {
  height: 14px;
  flex: 1;
  border-radius: 6px;
}
.sk-row.ln .sk-line {
  flex: 1;
}
.sk-hero {
  height: 150px;
  border-radius: 16px;
}
.sk-title {
  width: 90px;
  height: 18px;
  border-radius: 6px;
}
.sk-cards {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 10px;
}
.sk-card {
  aspect-ratio: 1;
  border-radius: 12px;
}
.sk-grid.sk-body {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(104px, 1fr));
  gap: 10px;
}
.sk-cell {
  aspect-ratio: 1;
  border-radius: 10px;
}
@keyframes skpulse {
  to {
    background-position: -200% 0;
  }
}
</style>