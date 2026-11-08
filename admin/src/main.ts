import { setupVueRootValidator } from 'vite-plugin-vue-transition-root-validator/client';
import { createApp } from 'vue';
import {
  setupAppErrorHandle,
  setupAppVersionNotification,
  setupDayjs,
  setupIconifyOffline,
  setupLoading,
  setupNProgress
} from './plugins';
import './plugins/assets';
import { getLocale, setupI18n } from './locales';
import { setupStore } from './store';
import { setupRouter } from './router';
import App from './App.vue';

async function setupApp() {
  setupLoading();

  setupNProgress();

  setupIconifyOffline();

  setupDayjs();

  const app = createApp(App);

  setupAppErrorHandle(app);

  setupStore(app);

  await setupRouter(app);

  setupI18n(app);

  setupAppVersionNotification();

  setupVueRootValidator(app, {
    lang: getLocale() === 'zh-CN' ? 'zh' : 'en'
  });

  app.mount('#app');
}

setupApp();
