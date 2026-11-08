<script setup lang="ts">
/**
 * 确认弹窗：危险操作统一入口（Naive UI dialog 预设自带 Esc 关闭、焦点圈定、滚动锁）。
 * 现有调用点多为删除等破坏性操作，非破坏性操作（如恢复）显式传 primary。
 */
import { computed } from 'vue'
import { NModal } from 'naive-ui'

const props = withDefaults(
  defineProps<{
    title: string
    message: string
    confirmLabel?: string
    busy?: boolean
    variant?: 'primary' | 'secondary' | 'danger'
  }>(),
  { confirmLabel: '确认', variant: 'danger' },
)

const emit = defineEmits<{ (e: 'close'): void; (e: 'confirm'): void }>()

const buttonType = computed(() => (props.variant === 'danger' ? 'error' : props.variant === 'primary' ? 'primary' : 'default'))
</script>

<template>
  <NModal
    preset="dialog"
    :show="true"
    :title="title"
    :positive-text="confirmLabel"
    negative-text="取消"
    :positive-button-props="{ type: buttonType, disabled: busy, loading: busy }"
    :closable="true"
    :mask-closable="true"
    :auto-focus="false"
    :style="{ width: '420px', maxWidth: 'calc(100vw - 40px)' }"
    @positive-click="emit('confirm')"
    @negative-click="emit('close')"
    @close="emit('close')"
    @esc="emit('close')"
  >
    {{ message }}
  </NModal>
</template>
