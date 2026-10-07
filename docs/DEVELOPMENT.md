# 情侣生活记录 APP — 开发与构建指南

## 目录

1. [本地开发（浏览器为主）](#1-本地开发浏览器为主)
2. [本地测试](#2-本地测试)
3. [构建（二进制 / 镜像 / APK）](#3-构建二进制--镜像--apk)
4. [发布（CI，tag 触发）](#4-发布citag-触发)
5. [发布前检查清单](#5-发布前检查清单)

---

## 1. 本地开发（浏览器为主）

日常开发 = **浏览器 + Vite dev server**：三个终端（后端 / admin / APP）跑起来后，页面开发、联调、自测全在浏览器里完成，**不需要 Rust / Android SDK**——那两样只有构建 APK 与真机验证时才用，见「构建（二进制 / 镜像 / APK）」。

后端用 `go run` 直跑，**没有「先构建再运行」这一步**，也就不存在二进制落后于源码的问题；`bin/server` 只是供发布的构建产物，与镜像、APK 并列，日常开发不碰。

### 1.1 环境要求

版本口径来自 `server/go.mod`、`Dockerfile` 与 `android-build.yml`（APK workflow 跑 Node 侧构建，故钉 Node/pnpm；`docker-publish.yml` 无语言版本钉，版本在镜像内定），改动其中任何一处时同步本表：

| 工具 | 版本 | 用在哪 |
|:--|:--|:--|
| Go | ≥ 1.25（`go 1.25.0`，镜像 `golang:1.25-alpine`） | 后端 |
| Node | 22（workflow `node-version: '22'`，镜像 `node:22-alpine`） | admin、mock、APP 前端 |
| pnpm | 随 Node 22（`app/pnpm-lock.yaml`）；CI 钉 `pnpm@11`（`corepack prepare`） | APP（`app/` 只用 pnpm） |
| npm | 随 Node 22（`admin/package-lock.json`） | admin 与 mock（`make deps` / `make mock`） |
| Rust + Android SDK | 仅构建 APK 与真机验证需要 | `make android-apk`；浏览器开发全程不需要 |

> 本机没有 Rust 工具链不影响日常开发；工具链装在容器内的，容器重建即丢失（见「发布前检查清单」）。

### 1.2 装依赖

`make deps`：server `go mod download` + admin `npm install` + app `pnpm install`。

### 1.3 命令速查

项目相关命令一律走 `make`（浏览器层例外见 AGENTS「浏览器验证」），Makefile 内按同名分组组织。变量可命令行覆盖，如 `make app-dev API_PROXY=http://127.0.0.1:8787`、`make up VERSION=0.2.0`。

| 组 | 目标 | 干什么 | 细节在 |
|:--|:--|:--|:--|
| 本地开发 | `make deps` | 三端装依赖 | 「装依赖」 |
| | `make mock` | 起 mock（:8787） | 本节注记 |
| | `make reset` | 清空数据目录（交互确认） | 「数据目录与重置」 |
| | `make serve` | 后端 `go run` 直跑（无需构建） | 「启动命令（三端都连真实后端）」 |
| | `make admin-dev` / `make app-dev` | 两端 dev server | 同上 |
| 本地测试 | `make verify` / `verify-server` / `verify-app` / `verify-admin` | 三端测试与类型检查（`-k` 跑完全部再报） | AGENTS「验证命令」 |
| 构建 | `make build` / `make build-server` / `make clean` | 二进制 | 「二进制」 |
| | `make compose-build` / `make up` / `make logs` / `make down` | 镜像 | 「Docker 镜像」 |
| | `make android-keystore` / `make android-sign-env` / `make android-apk` / `make app-icon` | APK | 「APK」 |

> **`tools/mock` 的定位：离线开发辅助**——没有后端（或不想起后端）时调界面用，`make mock` 可起零依赖 mock 服务（默认 `:8787`，`MOCK_PORT` 可改）。
>
> 它**不再是契约基准**：「mock 先行」阶段规则已随后端完成废止。契约以 `server/` 实现与 PRODUCT、`docs/design/` 的设计文档为准；需要 mock 时，由它向真实后端对齐，而不是反过来。

开发流程、编码约定与验证口径见 AGENTS「验证命令」；浏览器验证的固定做法见 AGENTS「浏览器验证」（一律用局域网地址，不用 `127.0.0.1`）。改了样式页面不变（dev server 返回旧 CSS）是 Vite 缓存问题，`touch` 该文件即可，详见客户端设计「坑与验收口径（改浮层前先看这里）」。

### 1.4 启动命令（三端都连真实后端）

三个终端各起一个（都是前台进程）：

- `make serve` — 后端：`go run` 直跑（`DATA_DIR` 固定指向仓库内 `server/data`——Makefile 传绝对路径，不受 `cd server` 影响；`ADMIN_USER` / `ADMIN_PASS` 内置默认值；**改了 `server/` 保存即生效，不用先构建**）
- `make admin-dev` — 后台管理页（`:5173`）
- `make app-dev` — APP（`:1420`）；连 mock 时 `make app-dev API_PROXY=http://127.0.0.1:8787`

访问地址口径：

- Android 真机访问不到 `127.0.0.1`：APP 登录页的服务端地址要填**本机局域网地址**（`hostname -I | awk '{print $1}'`；macOS 用 `ipconfig getifaddr en0`），形如 `192.168.1.23:8080`。
- 用浏览器验证页面时同理——访问局域网地址而不是 `127.0.0.1`（理由与前置检查见 AGENTS「浏览器验证」）。

### 1.5 端口分配

| 服务 | 端口 | 依据 |
|:--|:--|:--|
| 后端 | 8080 | `LISTEN` 默认 `:8080`，并内嵌后台页面 |
| admin | 5173 | `admin/vite.config.ts` 默认 |
| APP | 1420 | `app/vite.config.ts`（`strictPort`），Tauri `devUrl` 固定指向 |

三者不冲突；不要再给 APP 手动指定 5173，会顶掉 admin。

### 1.6 数据目录与重置

固定使用 `server/data`（已被 `.gitignore` 忽略，清空不影响版本库）。

`DATA_DIR` 要显式给成 `server/data`：它的默认值是相对 `./data`、随进程 cwd 解析——不给 `DATA_DIR` 直接跑（`go run ./cmd/server` 或跑 `bin/server`）都会在仓库根另建一份空库，看起来像「数据全丢了」。

**重置** = 清空该目录：`make reset`（先停后端——服务运行中持有 SQLite；目标带交互确认，输 `n` 不动数据）。清空即重置一切：admin 重建、所有 token 失效、成员 / AI 供应商 / 地图源 / Webhook 全部丢失。

### 1.7 admin 账号

本机默认凭据 `admin / admin123456` 已内置为 Makefile 变量（`serve` 自动带上），想换就 `make serve ADMIN_PASS=<密码>` 临时覆盖。

- 只在库中**无** admin 时生效；已存在则忽略（改密走后台）。
- **弱口令，仅限本机开发**，禁止用于任何可被外部访问的实例。

### 1.8 APP 登录用成员账号，不是 admin

登录按 `client` 分流——admin 只能登录后台、APP 只接受情侣角色（口径见 PRODUCT「角色与账号」）；admin 账号走 APP 入口（`client:"app"`）登录会被 403 拒绝。所以清空数据后 APP 无法直接用 admin 登录：

1. 先用 `admin / admin123456` 登录后台 `:5173`
2. 在「成员」里创建情侣成员（girl / boy）
3. 用成员账号登录 APP

## 2. 本地测试

### 2.1 自动化验证

`make verify` 依次跑三端（`verify-server` / `verify-app` / `verify-admin`），默认 fail-fast；`make -k verify` 跑完全部再一起报。口径（什么必须过、失败怎么办）见 AGENTS「验证命令」。

### 2.2 健康检查（全链路）

「后端起了」不等于「链路通」：页面能不能渲染、接口在页面里生不生效，只有浏览器说了算。分两层，任一层不过，先修它再往下查。

**① 浏览器层（按 AGENTS「浏览器验证」，唯一算数的链路证据）**

固定用 `mcp__chrome-devtools__*` 工具，**访问局域网地址**（IP 取法见「启动命令（三端都连真实后端）」，形如 `http://192.168.x.x:5173`）——不用 `127.0.0.1`：服务常跑在容器里，浏览器那一侧的 `127.0.0.1` 连不到它。

两端各走一遍 登录 → 首屏：

| 端 | 打开 | 期望看到 |
|:--|:--|:--|
| admin | `http://<IP>:5173` | 登录页 → admin 登录 → 仪表盘，六个导航项（仪表盘 / 空间管理 / AI 配置 / 外部集成 / 回收站 / 审计日志，PRODUCT「后台管理页面功能范围（admin）」） |
| APP | `http://<IP>:1420` | 登录页 → 成员账号登录（见「APP 登录用成员账号，不是 admin」）→ 传统模式底部五个 tab + 右下角 FAB；AI 模式未配模型 Key 时切换项置灰是预期（PRODUCT「双模式界面」） |

每端三件事：`take_snapshot` 确认上表元素在；`list_console_messages` 无 error；`list_network_requests` 里 `/api/*` 无 5xx、无代理失败。各留一张 `take_screenshot` 存档。

**② 判定口径**

- 结论落到具体观测（页面元素、console、网络响应），不写「看起来正常」（AGENTS「浏览器验证」）。
- 白屏 / console 报错 → 前端问题：查 vite 与 console，不回后端。
- 页面在但接口 4xx / 5xx → 前后端契约或登录态问题：按响应码查。

## 3. 构建（二进制 / 镜像 / APK）

构建 = 三个发布产物，各自独立、按需产出；日常开发不碰它们（后端 `go run`、前端 Vite，见「本地开发（浏览器为主）」）。三个产物本地与 CI 走同一条 make 构建链：二进制与镜像由 `Dockerfile` 调同一批目标（见「Docker 镜像」），APK 的 CI 也执行 `make android-apk`（见「发布（CI，tag 触发）」）。

### 3.1 二进制

| 目标 | 干什么 |
|:--|:--|
| `make build` | 全量构建：admin 构建 → 拷入 `server/internal/webui/dist/` → `go build`，产物 `bin/server` |
| `make build-server` | 只重建二进制（发布前用）；**依赖 `copy-admin`**，会先把当前 `admin/dist` 拷进内嵌目录 |
| `make clean` | 清理构建产物 |

> [!NOTE]
> `server/internal/webui/dist/` 不入版本库，内容由 `make build` / `make build-server`（经 `copy-admin`）填充；裸 `go build` 与 `make serve` 用的是该目录**上一次**的内容，后台页面改完不重建就会跑旧页面。

- **别为日常开发构建它**：`make serve` 是 `go run` 直跑，改了代码即生效；二进制只在要分发 / 部署裸机时才构建。
- go 的 `GOPROXY` / `GOMODCACHE` / `GOCACHE` **不设默认值，由 go env 与用户环境决定**——需要时命令行临时覆盖（`make build-server GOCACHE=$PWD/.gocache`）；受限环境（默认 `~/.cache/go-build` 不可写）一次性 `go env -w GOCACHE=<可写路径>` 更省事，make 与裸 go 命令都受益，别把环境特例固化成仓库默认。网络能直连时同理换 `GOPROXY=https://proxy.golang.org,direct`。

### 3.2 Docker 镜像

`make compose-build`：经 compose 构建本地镜像——`Dockerfile` 内部调的正是「二进制」那批目标（先建 admin 内嵌、再 `make build-server`），构建链与本地一致；产物与 `make up` 同名同版本，构建后直接 `make up` 即跑本地产物、不拉远端。

- 跑本地镜像的**数据卷 / 首启凭据 / 端口 / TLS** 见 README「快速开始」：`DATA_DIR` 要指卷内路径（备份口径见 ARCHITECTURE「备份与迁移」），`ADMIN_USER` / `ADMIN_PASS` 只在数据目录为空时生效（同「admin 账号」）。
- `make up` / `make logs` / `make down`：compose 三连，部署面权威在 README；版本用 `make up VERSION=<x.y.z>` 覆盖（无 `latest` tag）。

### 3.3 APK

本地签名包（冒烟用）三步——**需要 Rust + Android SDK**：

```text
make android-keystore    一次性：生成密钥并写 .env
make android-sign-env    从 .env 生成本地签名配置 keystore.properties（复用已有密钥，不覆盖）
make android-apk         构建 release 包，产物在 app/src-tauri/gen/android/app/build/outputs/
```

- `keystore.properties` 由 CI 构建时写入，本地即由第二步生成；缺失时 release 用未签名/默认配置，debug 不受影响。
- 本地与 CI 都经 `make android-apk` 调用（底层即 `pnpm tauri android build`）。用 `node_modules/.bin/tauri` 直调会让 CLI 把生成的 Gradle 任务写成 `executable="node"`，Gradle 阶段报 `Cannot find module '<路径>/tauri'`（CI 的 init 会自愈回 `pnpm`，本地需注意）。
- 换图标：`make app-icon`——源图是 `app/src-tauri/icons/icon.png`（512×512 主图，在仓；README 顶部那张 `assets/images/logo.png` 是同款设计的 1254×1254 大图，但 tauri 命令只吃前者）。换图标 = 替换该文件后重跑，`src-tauri/icons/` 与 Android `res/` 整套重新生成，diff 较大属正常。
- **发布产物仍以 CI 为准**（见「发布（CI，tag 触发）」）；本地包只供真机冒烟。

## 4. 发布（CI，tag 触发）

两个 workflow 都由推 `v*` tag 触发，各产各的物。

### 4.1 一次性配置：签名密钥与 secrets（仅 APK 需要）

运行 `make android-keystore`。脚本会交互式询问密码，然后：把密钥库生成到 `~/.love-life-keys/upload-keystore.jks`（放在仓库之外避免误提交，可用 `KEYSTORE_DIR` 覆盖）；把三项 secret 写入仓库根目录 `.env`（已被 gitignore），并打印 `gh` 一键录入命令。在仓库 Settings → Secrets and variables → Actions 录入，名称须完全一致：

| Secret 名 | 取值 |
|:--|:--|
| `ANDROID_KEYSTORE_BASE64` | 密钥库文件的 base64（脚本已生成） |
| `ANDROID_KEY_ALIAS` | key 别名（脚本默认 `upload`） |
| `ANDROID_KEY_PASSWORD` | keystore 与 key 的密码（PKCS12 两者强制相同） |

> [!CAUTION]
> **换了密钥，已安装的旧 APK 无法覆盖安装 / 更新。**密钥库请离线备份；未录入 secrets 时 workflow 会在签名步骤直接失败。

### 4.2 触发与产物

tag 名 = `v<version>`，version 必须与 `app/src-tauri/tauri.conf.json` 的 `version` 一致（推送前先核对，不一致 workflow 直接失败）：

```text
git tag v0.1.0
git push origin v0.1.0
```

推送即触发 Docker 与 APK 两个 workflow。

- **Docker**（`docker-publish.yml`）：推 `ghcr.io/bookandmusic/love-life`，多架构 `linux/amd64` + `linux/arm64`；tag 集合 = `v0.1.0` / `0.1.0` / `0.1`（`1` 仅非 v0.x，`v0.x` 阶段不产裸主版本）/ `sha-<短SHA>`，**不产 `latest`**——compose 与 README 用版本变量，见 README「快速开始」。workflow 本身只调 `docker build`，构建链在 `Dockerfile` 内（即 Makefile 的目标）。
- **APK**（`android-build.yml`）：构建步骤为 `make android-apk`，与本地同链（`keystore.properties` 由 workflow 前序步骤从 secrets 写入；本地由 `android-sign-env` 从 `.env` 生成，这是两条路径的唯一差异）。产出单个通用 APK（含 `arm64-v8a` / `armeabi-v7a` / `x86_64` / `x86` 四种 ABI）+ Play 上架用 AAB，传一份 Release 草稿；workflow artifact **未设 `retention-days`**，保留期按仓库设置（默认 90 天）。产物形态与安装方式见 README「手机 APP」。

### 4.3 维护事项

- **〔两端〕**`app/src-tauri/gen/android/` 纳入版本管理（Tauri 官方约定，工程自带 `.gitignore`，构建产物与 `keystore.properties` 不会入库）。签名配置与 release 明文开关写在 `app/build.gradle.kts`。

## 5. 发布前检查清单

首次发布前逐项确认——**下面任何一项未完成，都是发布阻断**：

- [ ] **仓库已配置 git remote 并推送**：workflow 跑在 GitHub Actions 上，没有 remote（或没推 tag）不会触发；仓库地址由仓库所有者决定。
- [ ] **三项签名 secrets 已录入**：见「发布（CI，tag 触发）」，名称须完全一致；未录入时 workflow 会在签名步骤直接失败。
- [ ] **密钥库已离线备份**：丢失后无法为已安装的 App 发布更新。
- [ ] **CI 产出的 release APK 已在真机安装并启动一次**：本地可先用 `make android-apk` 打签名包冒烟，发布前仍以 CI 产物为准。
- [ ] **功能侧真机待验项已过**：本仓库开发环境默认没有 Rust 工具链与 Android SDK（工具链装在容器内，容器重建即丢失），以下真机侧行为一律未在设备上跑过：
  - [ ] `dialog.save()` 在 Android 上返回的路径形态，以及 `fs` 能否直接写 `content://` URI（**首要待验项**：若不支持，改为先写应用目录再用系统「打开 / 分享」交给用户）。
  - [ ] SAF 选择器里是否出现相册 / 下载目录；`fs:scope` 现给 `**`，真机若拒绝则按需收窄。
  - [ ] 大文件（>100MB 视频）流式写入的内存与耗时；保存过程中切到后台的行为。
  - [ ] 未登录 / 离线 / 服务端不可达时的提示；弱网下签名重签的表现。
  - [ ] iOS（若后续打包）：`save()` 走「文件」App，`fs` 写入沙盒内路径。
