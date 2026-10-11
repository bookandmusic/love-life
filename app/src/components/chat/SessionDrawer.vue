<script setup lang="ts">
// 会话抽屉（/）：左上角菜单唤起——会话列表 / 新建 / 切换 / 删除（硬删）。
// 删除为破坏性操作：行内二次确认（再点一次「确认删除」才发请求），不做弹窗。
// 定位：由父页 overlay 插槽承载（PageLayout .page 内 absolute）——锚定会话窗口框架，
// 桌面档落在浮动卡片内并随卡片圆角裁切，不再是全浏览器视口弹窗。
import { ref } from 'vue'
import { Plus, Trash2 } from 'lucide-vue-next'
import type { ChatSession } from '../../types/chat'

defineProps<{
  open: boolean
  sessions: ChatSession[]
  currentId: number
  // busy = 流式/待确认中：禁切换与删除当前会话（SSE 与待传文件都锚定当前会话视图）
  busy: boolean
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'new'): void
  (e: 'select', id: number): void
  (e: 'remove', id: number): void
}>()

// 行内二次确认：记「待删除」会话 id，再点一次才真正删除；点别处/再点其他行取消
const confirmId = ref<number | null>(null)
function askRemove(id: number) {
  if (confirmId.value === id) {
    emit('remove', id)
    confirmId.value = null
  } else {
    confirmId.value = id
  }
}
</script>

<template>
  <Transition name="drawer">
    <div v-if="open" class="drawer-mask" @click.self="emit('close')">
      <aside class="drawer" aria-label="会话列表">
        <header class="drawer-head">
          <span class="drawer-title">会话</span>
          <button class="new-btn" :disabled="busy" aria-label="新建会话" @click="emit('new')">
            <Plus :size="18" stroke-width="2" />
            <span>新会话</span>
          </button>
        </header>
        <div class="drawer-body">
          <p v-if="sessions.length === 0" class="empty">还没有会话，发送第一条消息即自动创建。</p>
          <div
            v-for="s in sessions"
            :key="s.id"
            class="sess"
            :class="{ active: s.id === currentId }"
            role="button"
            tabindex="0"
            @click="confirmId = null; emit('select', s.id)"
            @keydown.enter="confirmId = null; emit('select', s.id)"
          >
            <span class="sess-title">{{ s.title }}</span>
            <span
              v-if="confirmId === s.id"
              class="sess-del confirming"
              role="button"
              aria-label="再次点击确认删除"
              @click.stop="askRemove(s.id)"
              >确认删除？</span>
            <span
              v-else
              class="sess-del"
              role="button"
              aria-label="删除会话"
              @click.stop="askRemove(s.id)"
              ><Trash2 :size="16" stroke-width="1.8" /></span>
          </div>
        </div>
      </aside>
    </div>
  </Transition>
</template>

<style scoped>
/* 遮罩只盖本页框架（overlay 插槽锚定 .page），桌面档不越过浮动卡片 */
.drawer-mask {
  position: absolute;
  inset: 0;
  background: rgba(0, 0, 0, 0.3);
  z-index: 40;
}
.drawer {
  position: absolute;
  top: 0;
  left: 0;
  bottom: 0;
  width: min(78%, 300px);
  background: linear-gradient(180deg, var(--glass-from), var(--glass-to));
  backdrop-filter: blur(20px);
  -webkit-backdrop-filter: blur(20px);
  display: flex;
  flex-direction: column;
  box-shadow: var(--shadow);
  padding-top: env(safe-area-inset-top);
}
@supports not (backdrop-filter: blur(1px)) {
  .drawer {
    background: var(--surface);
  }
}
.drawer-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0.7rem 0.75rem 0.7rem 1rem;
  border-bottom: 0.5px solid var(--rule);
  flex-shrink: 0;
}
.drawer-title {
  font-size: 17px;
  font-weight: 600;
}
/* iOS 导航按钮口径：tint 图标+文字，无底色药丸 */
.new-btn {
  display: flex;
  align-items: center;
  gap: 2px;
  border: none;
  background: none;
  color: var(--primary);
  padding: 6px 8px;
  font-size: 0.9375rem;
  cursor: pointer;
  border-radius: 10px;
}
.new-btn:disabled {
  opacity: 0.4;
}
.drawer-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 0.25rem 0.5rem 0.5rem;
}
.empty {
  margin: 2rem 1rem;
  font-size: 0.8125rem;
  color: var(--muted-ink);
  text-align: center;
}
/* iOS 分组列表行：细分割线、按下灰、当前会话 tint 淡底 + 对勾 */
.sess {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 0.8rem 0.5rem 0.8rem 0.6rem;
  border-radius: 10px;
  cursor: pointer;
  font-size: 0.9375rem;
  color: var(--ink);
}
.sess + .sess {
  border-top: 0.5px solid var(--rule);
}
.sess:active {
  background: var(--sk);
}
.sess.active {
  background: var(--primary-wash);
}
.sess-title {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.sess-del {
  flex-shrink: 0;
  color: var(--danger);
  display: inline-flex;
  padding: 5px;
  border-radius: 50%;
}
.sess-del:active {
  background: rgba(120, 120, 128, 0.12);
}
.sess-del.confirming {
  font-size: 0.75rem;
  font-weight: 600;
  background: var(--primary);
  color: #fff;
  border-radius: 999px;
  padding: 3px 9px;
}
.drawer-enter-active,
.drawer-leave-active {
  transition: opacity 0.22s ease;
}
.drawer-enter-active .drawer,
.drawer-leave-active .drawer {
  transition: transform 0.22s ease;
}
.drawer-enter-from,
.drawer-leave-to {
  opacity: 0;
}
.drawer-enter-from .drawer,
.drawer-leave-to .drawer {
  transform: translateX(-100%);
}
</style>
