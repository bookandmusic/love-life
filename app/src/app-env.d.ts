/// <reference types="vite/client" />

// 构建期注入的常量（vite.config.ts 的 define，取值 src-tauri/tauri.conf.json 的 version）。
// 声明在此，供设置页等直接使用；不要改成运行时读取——版本号必须在构建时固化。
declare const __APP_VERSION__: string

// 内置服务地址（vite.config.ts 的 define，取值 APP_DEFAULT_BASE 环境变量）。
// 登录前只能靠它（此时无 token，拿不到绿联通道）；dev 未注入时 client.ts 回落 localhost:8080。
declare const __DEFAULT_BASE__: string
