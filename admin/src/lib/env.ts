/**
 * 运行时环境变量。通过 Vite 构建时注入（.env / .env.local / 部署环境）。
 * 未配置 VITE_API_BASE_URL 时回退到相对路径 /api（开发期由 vite proxy 转发，
 * 生产期与服务端同源部署），从而保证"支持通过环境变量指定服务端 API"。
 */
export const API_BASE_URL: string =
  (import.meta.env.VITE_API_BASE_URL as string | undefined) ?? ''
