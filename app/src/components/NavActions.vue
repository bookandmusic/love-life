<script setup lang="ts">
import { onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { Bell, Settings } from 'lucide-vue-next'
import { useUnread } from '../stores/notifications'

// 页头右侧统一操作簇：通知（未读红点）+ 设置。
// showBell=false 时仅显示设置按钮（用于 AI 对话页等无通知入口的场景）。
const props = withDefaults(defineProps<{ showBell?: boolean }>(), { showBell: true })
const router = useRouter()
const unreadStore = useUnread()

onMounted(() => {
  if (props.showBell) void unreadStore.refresh()
})
</script>

<template>
  <div class="nav-actions">
    <button v-if="showBell" class="nav-btn" aria-label="通知" @click="router.push('/notifications')">
      <Bell :size="21" stroke-width="1.8" />
      <span v-if="unreadStore.count > 0" class="badge">{{ unreadStore.count > 99 ? '99+' : unreadStore.count }}</span>
    </button>
    <button class="nav-btn" aria-label="设置" @click="router.push('/settings')">
      <Settings :size="21" stroke-width="1.8" />
    </button>
  </div>
</template>

<style scoped>
.nav-actions {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
}
.nav-btn {
  width: 34px;
  height: 34px;
  border: none;
  background: none;
  color: var(--ios-blue);
  cursor: pointer;
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
}
.nav-btn:active {
  background: rgba(0, 122, 255, 0.1);
}
.badge {
  position: absolute;
  top: 2px;
  right: 2px;
  min-width: 16px;
  height: 16px;
  border-radius: 8px;
  background: var(--danger);
  color: #fff;
  font-size: 10px;
  font-weight: 600;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 0 3px;
}
</style>