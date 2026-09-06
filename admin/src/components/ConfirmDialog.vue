<script setup lang="ts">
import AppModal from './AppModal.vue'
import AppButton from './AppButton.vue'

defineProps<{
  title: string
  message: string
  confirmLabel?: string
  busy?: boolean
}>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'confirm'): void }>()
</script>

<template>
  <AppModal :title="title" :width="420" @close="emit('close')">
    <p class="confirm-message">{{ message }}</p>
    <template #footer>
      <AppButton size="sm" @click="emit('close')">取消</AppButton>
      <AppButton size="sm" variant="danger" :disabled="busy" @click="emit('confirm')">
        {{ confirmLabel ?? '确认' }}
      </AppButton>
    </template>
  </AppModal>
</template>

<style scoped>
.confirm-message {
  margin: 0;
  font-size: 14px;
  line-height: 1.7;
  color: var(--color-text-secondary);
}
</style>
