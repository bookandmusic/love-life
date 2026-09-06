/**
 * 统一开发 mock 后端 —— HTTP 服务入口（零依赖，仅 Node 内置模块 + 原生 fetch）。
 *
 * admin 与 app 共用此服务（app 通过登录页 base URL 或 VITE_SERVER_URL 指向 8787 即可切换）。
 * 启动：node tools/mock/server.mjs   （默认监听 8787，可用 MOCK_PORT 覆盖）
 * 热重载：node --watch tools/mock/server.mjs
 */

import http from 'node:http'
import { routes } from './routes.mjs'

const PORT = Number(process.env.MOCK_PORT) || 8787

const json = (res, status, data) => {
  if (status === 204) { res.statusCode = 204; res.end(); return }
  res.setHeader('Content-Type', 'application/json')
  res.statusCode = status
  res.end(JSON.stringify(data))
}

// ---------- 服务器 ----------
const server = http.createServer((req, res) => {
  const startAt = Date.now()
  const pathname = (req.url || '').split('?')[0]

  // 请求日志：响应完成后统一打印（含耗时与状态码）
  res.on('finish', () => {
    console.log(`[mock] ${req.method} ${pathname} ${res.statusCode} ${Date.now() - startAt}ms`)
  })

  res.setHeader('Access-Control-Allow-Origin', '*')
  res.setHeader('Access-Control-Allow-Methods', 'GET,POST,PUT,PATCH,DELETE,OPTIONS')
  res.setHeader('Access-Control-Allow-Headers', 'Content-Type,Authorization')

  if (req.method === 'OPTIONS') {
    res.statusCode = 204
    res.end()
    return
  }

  let body = ''
  req.on('data', (chunk) => (body += chunk))
  req.on('end', () => {
    // JSON 预校验：带 body 的请求必须是合法 JSON（否则 400，不进入 handler）
    // multipart/form-data（媒体上传）跳过预校验，直接交给 handler（A2）
    const contentType = req.headers['content-type'] || ''
    if (req.method !== 'GET' && body.trim() && !contentType.startsWith('multipart/')) {
      try { JSON.parse(body) } catch {
        json(res, 400, { error: 'invalid JSON body' })
        return
      }
    }

    const route = routes.find((r) => r.method === req.method && r.pattern.test(pathname))
    if (!route) {
      json(res, 404, { error: 'no mock for ' + req.method + ' ' + pathname })
      return
    }
    try {
      const result = route.handler(req.url || pathname, body, req.headers, res)
      if (typeof result === 'function') {
        // 非 JSON 响应：handler 直接返回写 response 的函数
        result(res)
      } else if (result && result._s !== undefined) {
        json(res, result._s, result.d)
      } else {
        json(res, 200, result)
      }
    } catch (e) {
      json(res, 500, { error: String(e) })
    }
  })
})

// ---------- 优雅关闭 ----------
function shutdown(signal) {
  console.log(`\n[mock] received ${signal}, shutting down...`)
  server.close(() => {
    console.log('[mock] server closed, bye')
    process.exit(0)
  })
  // 兜底：3 秒内未关闭则强制退出
  setTimeout(() => process.exit(1), 3000).unref()
}
process.on('SIGINT', () => shutdown('SIGINT'))
process.on('SIGTERM', () => shutdown('SIGTERM'))

server.listen(PORT, () => {
  console.log(`[mock] unified mock server listening on http://localhost:${PORT}`)
})