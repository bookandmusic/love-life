<script setup lang="ts">
import AppIcon from './AppIcon.vue'

defineProps<{ icon?: string; title: string; desc?: string }>()
</script>

<template>
  <div class="empty-state">
    <AppIcon v-if="icon" :name="icon" :size="48" />
    <div class="empty-state-title">{{ title }}</div>
    <div v-if="desc" class="empty-state-desc">{{ desc }}</div>
  </div>
</template>
