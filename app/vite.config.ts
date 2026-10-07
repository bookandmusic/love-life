import { readFileSync } from 'node:fs'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// 版本号单一来源：src-tauri/tauri.conf.json 的 version（CI 发版会校验 tag 与它一致，
// 见 DEVELOPMENT「发布（CI，tag 触发）」）。构建期注入，避免设置页硬编码随发版失真。
const { version } = JSON.parse(readFileSync(new URL('./src-tauri/tauri.conf.json', import.meta.url), 'utf8'))

export default defineConfig({
  define: { __APP_VERSION__: JSON.stringify(version) },
  // comments:false：dev 也剥掉模板注释（默认仅 prod 剥）。根部注释节点会让页面组件变成
  // 多根片段，卡死 App.vue 顶层 out-in 过渡（离开该页即全站白屏）——dev 与 prod 渲染保持一致，根除该类地雷。
  plugins: [vue({ template: { compilerOptions: { comments: false } } })],
  // Tauri 固定使用 1420 端口（tauri.conf.json devUrl 指向此处）
  clearScreen: false,
  server: {
    host: true,
    port: 1420,
    strictPort: true,
    proxy: {
      // mock 先行（2026-07-18 定）：开发阶段 /api 默认代理到 mock（:8787），不连真实后端；
      // 需要联调真实后端时显式以 API_PROXY=http://<ip>:8080 启动。
      '/api': process.env.API_PROXY ?? 'http://localhost:8787',
    },
  },
  build: {
    outDir: 'dist',
    target: process.env.TAURI_ENV_PLATFORM == 'android' ? 'es2021' : 'es2022',
  },
})
