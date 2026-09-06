import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
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
