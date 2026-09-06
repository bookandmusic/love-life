import { createApp } from 'vue'
import { createPinia } from 'pinia'
// Inter 字体本地打包（@fontsource），出站依赖铁律禁止运行时外链 Google Fonts
import '@fontsource/inter/400.css'
import '@fontsource/inter/500.css'
import '@fontsource/inter/600.css'
import App from './App.vue'
import router from './router'
import './styles/tokens.css'
import './styles/base.css'
import './styles/components.css'

createApp(App).use(createPinia()).use(router).mount('#app')
