<script setup lang="ts">
defineProps<{ title?: string }>()
</script>

<template>
  <div class="card">
    <div v-if="title || $slots.actions" class="card-header">
      <span v-if="title" class="card-title">{{ title }}</span>
      <div class="flex gap-2 items-center">
        <slot name="actions" />
      </div>
    </div>
    <div class="card-body">
      <slot />
    </div>
  </div>
</template>
