<div align="center">

<img src="assets/images/logo.png" width="140" alt="情侣生活记录 APP Logo" />

# 情侣生活记录 APP

**面向单一情侣的自部署生活记录与情感管理应用**

记录日常、管理回忆、追踪足迹、维护纪念日，并用 AI 助手降低记录门槛。

[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](server/go.mod)
[![Tauri](https://img.shields.io/badge/Tauri-v2-24C8DB?logo=tauri&logoColor=white)](app/src-tauri)
[![Vue](https://img.shields.io/badge/Vue-3-4FC08D?logo=vuedotjs&logoColor=white)](app/package.json)
[![Docker](https://img.shields.io/badge/Docker-ghcr.io-2496ED?logo=docker&logoColor=white)](https://ghcr.io/bookandmusic/love-life)

[快速开始](#-快速开始) · [手机 APP](#-手机-app) · [文档](#-文档)

</div>

---

## 📖 简介

一对情侣的**私密生活档案**：两人各自用手机 APP 记录动态、照片、足迹与纪念日，服务端运行在你自己的服务器（NAS / VPS）上，所有数据自包含于单一数据目录——核心数据不经任何第三方。

| 组件 | 职责 | 技术栈 |
|:--|:--|:--|
| `server/` | 后端单静态二进制：REST API + MCP Server + 模型网关 + 内嵌后台页面 | Go 1.25 · SQLite |
| `admin/` | 后台管理页面（仅 admin 登录）：模型 / 成员 / 通知 / 审计 | Vue 3 · Vite · TS |
| `app/` | 手机 APP：传统模式 + AI 对话双模式界面 | Tauri v2 · Vue 3 · TS |
| `docs/` | 需求 / 设计 / 待办等权威文档 | — |

### 功能一览

- **动态**：图文视频动态、评论、点赞与收藏
- **相册**：照片随动态上传，月度相册每月 1 日自动聚合
- **纪念日**：按年循环，每个纪念日可单独设置提前提醒天数
- **地图**：足迹点位与关联动态，瓦片源由后台配置，逆地理本地离线完成
- **通知**：站内通知中心 + 外部 Webhook（Bark / Server酱 等，空间级配置）
- **回收站**：记录内容只增不删，删除一律软删除进回收站；admin 可恢复，仅系统定时任务可永久清理
- **AI 助手**：自然语言完成记录 / 查询 / 整理，写操作经确认卡片后生效；适配 OpenAI / Anthropic / Gemini 协议及兼容生态（DeepSeek / Kimi / GLM / Ollama 等），模型凭证只在服务端，客户端零 AI 配置

## 🚀 快速开始

官方镜像发布在 GHCR（多架构 amd64 / arm64），推 `v*` tag 时由 [GitHub Actions](.github/workflows/docker-publish.yml) 自动构建发布。仓库根目录已附带 `docker-compose.yml`，直接启动：

```yaml
# docker-compose.yml
services:
  love-life:
    image: ghcr.io/bookandmusic/love-life:latest
    container_name: love-life
    ports:
      - "8080:8080"
    environment:
      TZ: Asia/Shanghai
      # 首次启动用，创建 admin 后可注释掉这两行
      ADMIN_USER: admin
      ADMIN_PASS: change-me-please
      # 可选：单媒体文件软限（默认 500MB）
      # MEDIA_MAX_MB: "500"
    volumes:
      - ./data:/data
    restart: unless-stopped
```

```bash
docker compose up -d    # 启动
docker compose logs -f  # 看日志
docker compose down     # 停止
```

首次使用三步：

1. 首次启动必须提供 `ADMIN_USER` / `ADMIN_PASS`，服务端会自动创建管理员账号
2. 浏览器访问 `http://<服务器>:8080` 登录后台，添加情侣成员（girl / boy）
3. 手机 APP 登录页填入同一服务端地址，用成员账号登录

> [!IMPORTANT]
> 服务端本身只监听 HTTP，TLS 终止交给部署者自选——公网部署请前置 HTTPS 反向代理（Caddy / Nginx / 云 LB）。

> [!NOTE]
> 数据持久化在宿主机 `./data` 目录。容器启动时会自动把数据目录属主修正为内部 `app` 用户（uid 100）并以非 root 运行；若曾用 root 手工拷贝数据导致属主混杂，需自行 `sudo chown -R 100:101 ./data`。

镜像 tag 约定：`latest`；语义化版本 `v1.2.3` / `1.2.3` / `1.2` / `1`（推 `v*` tag 时自动生成，`v0.x` 阶段不生成主版本 tag）；`sha-<短SHA>`（特定提交）。

### 环境变量

| 变量 | 默认值 | 说明 |
|:--|:--|:--|
| `ADMIN_USER` / `ADMIN_PASS` | — | 首次启动创建 admin 账号，创建后可移除 |
| `LISTEN` | `:8080` | 监听地址 |
| `DATA_DIR` | `./data` | 自包含数据目录（SQLite / 向量库 / 媒体） |
| `MEDIA_MAX_MB` | `500` | 单媒体文件软限（MB） |
| `TZ` | — | 建议容器内设置，如 `Asia/Shanghai` |

<details>
<summary>不用 Compose？手动 docker run</summary>

```bash
docker pull ghcr.io/bookandmusic/love-life:latest
docker run -d \
  --name love-life \
  -p 8080:8080 \
  -v "$PWD/data:/data" \
  -e TZ=Asia/Shanghai \
  -e ADMIN_USER=admin -e ADMIN_PASS=<密码> \
  --restart unless-stopped \
  ghcr.io/bookandmusic/love-life:latest
```

</details>

## 📱 手机 APP

- APK 不在本地打包：推 `v*` tag 时由 [GitHub Actions](.github/workflows/android-build.yml) 构建并签名，产物见 [Releases](https://github.com/bookandmusic/love-life/releases)
- 单个通用 APK（含 `arm64-v8a` / `armeabi-v7a` / `x86_64` / `x86` 四种 ABI），另附 Play 上架用 AAB；workflow artifact 保留 90 天副本
- 下载后在 Android 设备开启「未知来源安装」即可旁加载；更新 = 覆盖安装新 APK（须同一签名密钥）
- 登录页填写服务端地址即可使用（见[快速开始](#-快速开始)）；构建与签名流程见 [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md)

## 📚 文档

| 文档 | 内容 |
|:--|:--|
| [docs/REQUIREMENTS.md](docs/REQUIREMENTS.md) | 产品需求：页面 / 交互 / 视觉 / 数据规则 / 决策清单 |
| [docs/DESIGN.md](docs/DESIGN.md) | 技术设计：架构 / 认证 / MCP / 后台 / 部署 / 数据模型 |
| [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) | 开发与构建：本地开发 / 源码构建 / Android APK CI |
| [docs/TODO.md](docs/TODO.md) | 未完成事项台账 |
| [AGENTS.md](AGENTS.md) | AI 协作开发流程与仓库约定 |
