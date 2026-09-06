import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  server: {
    host: true,
    port: 5173,
    proxy: {
      '/api': process.env.API_PROXY ?? 'http://localhost:8080',
    },
  },
  build: {
    outDir: 'dist',
  },
})
