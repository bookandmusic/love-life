# 情侣生活记录 APP

面向单一情侣的自部署生活记录与情感管理应用。

- 产品需求：[docs/REQUIREMENTS.md](docs/REQUIREMENTS.md)
- 技术设计：[docs/DESIGN.md](docs/DESIGN.md)
- 工作约定：[AGENTS.md](AGENTS.md)

## 仓库结构

```
server/   Go 后端（单二进制：REST + MCP + 内嵌后台静态资源）
admin/    后台管理页面（Vue 3 + Vite + TS，仅 admin 登录）
app/      手机 APP（Tauri v2 + Vue 3，双模式界面）
docs/     需求与设计文档
```

## 开发

```bash
# 后端（默认监听 :8080，数据目录 ./data）
cd server && go run ./cmd/server

# 后台管理页（开发代理 /api → :8080）
cd admin && npm install && npm run dev

# APP 前端（Tauri 需要 Rust 工具链；纯 Web 预览可用 vite）
cd app && npm install && npm run dev
cd app && npm run tauri dev        # 需 Rust
cd app && npm run tauri android    # 需 Rust + Android SDK
```

## 构建产物

服务端单静态二进制（内嵌 admin 构建产物：`admin` 构建后复制到 `server/internal/webui/dist/`）+ Android APK + Docker 镜像；运行状态自包含于单一数据目录。
