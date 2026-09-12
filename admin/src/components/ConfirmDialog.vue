<script setup lang="ts">
import AppModal from './AppModal.vue'
import AppButton from './AppButton.vue'

withDefaults(
  defineProps<{
    title: string
    message: string
    confirmLabel?: string
    busy?: boolean
    variant?: 'primary' | 'secondary' | 'danger'
  }>(),
  // 默认危险色：现有调用点多为删除等破坏性操作，非破坏性操作（如恢复）显式传 primary
  { variant: 'danger' },
)
const emit = defineEmits<{ (e: 'close'): void; (e: 'confirm'): void }>()
</script>

<template>
  <AppModal :title="title" :width="420" @close="emit('close')">
    <p class="confirm-message">{{ message }}</p>
    <template #footer>
      <AppButton size="sm" @click="emit('close')">取消</AppButton>
      <AppButton size="sm" :variant="variant" :disabled="busy" @click="emit('confirm')">
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
