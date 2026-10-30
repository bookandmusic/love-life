<script setup lang="ts">
// 启动加载页：有 token 时进入本页（无 token 直接去登录页）。
// 三阶段并行：恢复会话 / 取通道 / 首屏预取；完成即跳落地页。
//
// 关键区分：
// - 凭据失效（refresh 也失败且无已存凭据）→ 回登录页并提示
// - 网络失败 → 停在本页可重试，**不退回登录页**（否则用户会误以为要重输密码）
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Heart } from 'lucide-vue-next'
import { useAuth } from '../stores/auth'
import { initChannel, startChannelWatcher } from '../utils/channel'
import { landingRoute } from '../router'
import { useAppUpdate } from '../composables/useAppUpdate'

const router = useRouter()
const route = useRoute()
const auth = useAuth()
// 应用内更新：通道就绪后检查（此时 base 已确定，更新请求会走当前通道）
const { check: checkAppUpdate } = useAppUpdate()

type Phase = 'restoring' | 'channel' | 'ready' | 'offline' | 'expired'
const phase = ref<Phase>('restoring')
const detail = ref('')

// 总超时：超过 15s 视为网络问题
const TOTAL_TIMEOUT_MS = 15_000

async function run() {
  phase.value = 'restoring'
  detail.value = ''

  // 会话恢复与通道获取并行：两者互不依赖（通道接口本身需要 token，
  // 但 token 在 localStorage 里已可用，不必等 /auth/me 返回）
  const timer = new Promise<'timeout'>((resolve) =>
    window.setTimeout(() => resolve('timeout'), TOTAL_TIMEOUT_MS)
  )
  const work = (async () => {
    const result = await auth.restoreWithRetry()
    if (!result.ok) return result
    await initChannel()
    return { ok: true as const }
  })()

  const settled = await Promise.race([work, timer])
  if (settled === 'timeout') {
    phase.value = 'offline'
    detail.value = '连接超时，请检查网络后重试'
    return
  }
  if (!settled.ok) {
    if (settled.reason === 'expired') {
      phase.value = 'expired'
      detail.value = '登录已过期，请重新登录'
    } else {
      phase.value = 'offline'
      detail.value = '网络不可用，请检查连接后重试'
    }
    return
  }

  // 通道就绪后开始监听网络变化（切网/回前台时重新探活择优）
  startChannelWatcher()
  // 更新检查也放在通道就绪之后：更新走当前 base，早于初始化会打到错误地址
  void checkAppUpdate()

  phase.value = 'ready'
  // 守卫带过来的原目标优先（刷新某页后回到该页）；没有则用落地规则
  const target = typeof route.query.to === 'string' && route.query.to.startsWith('/')
    ? route.query.to
    : landingRoute()
  await router.replace(target)
}

function retry() {
  void run()
}

function goLogin() {
  void router.replace('/login')
}

onMounted(run)
</script>

<template>
  <div class="boot">
    <div class="boot-inner">
      <span class="boot-tile" aria-hidden="true"><Heart :size="26" :stroke-width="1.8" /></span>
      <p class="boot-name">情侣生活记录</p>

      <!-- 进行中：呼吸光晕（PRODUCT「加载状态」口径：非旋转菊花） -->
      <template v-if="phase === 'restoring' || phase === 'channel'">
        <span class="boot-glow" aria-hidden="true" />
        <p class="boot-hint">
          {{ phase === 'restoring' ? '正在恢复登录…' : '正在连接服务…' }}
        </p>
      </template>

      <!-- 网络失败：可重试，不退回登录页 -->
      <template v-else-if="phase === 'offline'">
        <p class="boot-hint">{{ detail }}</p>
        <button class="boot-btn" @click="retry">重试</button>
      </template>

      <!-- 凭据失效：明确告知并引导重新登录 -->
      <template v-else-if="phase === 'expired'">
        <p class="boot-hint">{{ detail }}</p>
        <button class="boot-btn" @click="goLogin">去登录</button>
      </template>
    </div>
  </div>
</template>

<style scoped>
.boot {
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--bg);
}
.boot-inner {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  padding: 0 32px;
  text-align: center;
}
.boot-tile {
  width: 56px;
  height: 56px;
  border-radius: 16px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: color-mix(in srgb, var(--primary) 12%, transparent);
  color: var(--primary-deep);
}
.boot-name {
  margin: 0;
  font-size: 17px;
  font-weight: 600;
  color: var(--ink);
}
/* 呼吸光晕：缓慢明暗，替代旋转加载图标 */
.boot-glow {
  margin-top: 18px;
  width: 88px;
  height: 3px;
  border-radius: 2px;
  background: var(--primary);
  opacity: 0.3;
  animation: boot-breathe 1.6s ease-in-out infinite;
}
@keyframes boot-breathe {
  0%,
  100% {
    opacity: 0.18;
    transform: scaleX(0.7);
  }
  50% {
    opacity: 0.55;
    transform: scaleX(1);
  }
}
.boot-hint {
  margin: 8px 0 0;
  font-size: 13px;
  line-height: 1.6;
  color: var(--muted-ink);
}
.boot-btn {
  margin-top: 14px;
  border: none;
  border-radius: 999px;
  padding: 10px 28px;
  font-size: 15px;
  font-weight: 600;
  color: #fff;
  background: var(--primary);
  cursor: pointer;
}
.boot-btn:active {
  transform: scale(0.97);
}
</style>
