<script setup lang="ts">
// 关于（专业信息，从设置页「关于」行进入）：版本 / 服务端地址 / 项目主页
import { useRouter } from 'vue-router'
import { ExternalLink, Github, Info, Server } from 'lucide-vue-next'
import { getBase } from '../api/client'
import PageLayout from '../components/PageLayout.vue'

const PROJECT_HOME = 'https://github.com/bookandmusic/love-life'
// 版本号来自构建期注入（vite.config.ts 读 src-tauri/tauri.conf.json），不硬编码
const appVersion = __APP_VERSION__

const router = useRouter()
</script>

<template>
  <PageLayout title="关于" back standalone @back="router.back()">
    <div class="content">
      <section class="grp">
        <div class="sec">应用</div>
        <div class="group">
          <div class="row">
            <span class="row-ic"><Info :size="16" stroke-width="1.8" /></span>
            <span class="label">版本</span>
            <span class="value num">{{ appVersion }}</span>
          </div>
          <div class="row">
            <span class="row-ic"><Server :size="16" stroke-width="1.8" /></span>
            <span class="label">服务端</span>
            <span class="value server-url">{{ getBase() || '（未配置）' }}</span>
          </div>
        </div>
      </section>

      <section class="grp">
        <div class="sec">项目</div>
        <div class="group">
          <a class="row row-link" :href="PROJECT_HOME" target="_blank" rel="noopener">
            <span class="row-ic"><Github :size="16" stroke-width="1.8" /></span>
            <span class="label">项目主页</span>
            <ExternalLink :size="14" stroke-width="2" class="value-link" />
          </a>
        </div>
      </section>
    </div>
  </PageLayout>
</template>
<style scoped src="../styles/settings.css"></style>
<style scoped>
.server-url {
  max-width: 180px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
