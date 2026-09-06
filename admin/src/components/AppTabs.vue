<script setup lang="ts">
defineProps<{ items: string[]; modelValue: string }>()
const emit = defineEmits<{ (e: 'update:modelValue', value: string): void }>()
</script>

<template>
  <div class="tabs">
    <div
      v-for="item in items"
      :key="item"
      class="tab"
      :class="{ active: item === modelValue }"
      @click="emit('update:modelValue', item)"
    >
      {{ item }}
    </div>
  </div>
</template>
