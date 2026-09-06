<script setup lang="ts">
const model = defineModel<boolean>({ required: true })
</script>

<template>
  <div
    class="toggle"
    :class="{ active: model }"
    role="switch"
    :aria-checked="model"
    @click="model = !model"
  />
</template>
