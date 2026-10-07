/// <reference types="vite/client" />

// 构建期注入的常量（vite.config.ts 的 define，取值 src-tauri/tauri.conf.json 的 version）。
// 声明在此，供设置页等直接使用；不要改成运行时读取——版本号必须在构建时固化。
declare const __APP_VERSION__: string
