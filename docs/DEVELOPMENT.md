# 情侣生活记录 APP — 开发与构建指南

## 目录

1. [本地开发（浏览器为主）](#1-本地开发浏览器为主)
2. [本地测试](#2-本地测试)
3. [构建（二进制 / 镜像 / APK）](#3-构建二进制--镜像--apk)
4. [CI 与发布](#4-ci-与发布)
5. [发布前检查清单](#5-发布前检查清单)

---

## 1. 本地开发（浏览器为主）

日常开发 = **浏览器 + Vite dev server**：三个终端（后端 / admin / APP）跑起来后，页面开发、联调、自测全在浏览器里完成，**不需要 Rust / Android SDK**——那两样只有构建 APK 时才用，见「构建（二进制 / 镜像 / APK）」。APP 的手机形态在浏览器里用移动视口验证（见「健康检查（全链路）」）。

后端用 `go run` 直跑，**没有「先构建再运行」这一步**，也就不存在二进制落后于源码的问题；`bin/server` 只是供发布的构建产物，与镜像、APK 并列，日常开发不碰。

### 1.1 环境要求

版本口径来自 `server/go.mod`、`Dockerfile` 与 `android-build.yml`（APK workflow 跑 Node 侧构建，故钉 Node/pnpm；`docker-publish.yml` 无语言版本钉，版本在镜像内定），改动其中任何一处时同步本表：

| 工具 | 版本 | 用在哪 |
|:--|:--|:--|
| Go | ≥ 1.25（`go 1.25.0`，镜像 `golang:1.25-alpine`） | 后端 |
| Node | 22（workflow `node-version: '22'`，镜像 `node:22-alpine`） | admin、mock、APP 前端 |
| pnpm | 钉 `pnpm@11`（两侧 `package.json` 的 `packageManager` 字段 + corepack） | admin、mock、APP 前端（三处只用 pnpm） |
| Rust + Android SDK | 仅构建 APK 需要 | `make android-apk`；浏览器开发全程不需要 |

> 本机没有 Rust 工具链不影响日常开发；工具链装在容器内的，容器重建即丢失（见「发布前检查清单」）。

### 1.2 装依赖

`make deps`：server `go mod download` + admin `pnpm install` + app `pnpm install`。

> **依赖的构建脚本要显式放行**：pnpm 11 默认不跑依赖的 `postinstall`，且**把「有脚本被忽略」直接判为安装失败**（`ERR_PNPM_IGNORED_BUILDS`）。放行写在各自的 `pnpm-workspace.yaml`（如 `allowBuilds: esbuild: true`）——pnpm 11 起不再读 `package.json` 的 `pnpm` 字段，写在那里会被忽略并只留一条 WARN。新增带构建脚本的依赖时，先跑一次 `pnpm approve-builds` 确认它确实需要跑，再落进该文件。

### 1.3 命令速查

项目相关命令一律走 `make`（浏览器层例外见 AGENTS「浏览器验证」），Makefile 内按同名分组组织。变量可命令行覆盖，如 `make app-dev API_PROXY=http://127.0.0.1:8787`、`make up VERSION=0.2.0`。

| 组 | 目标 | 干什么 | 细节在 |
|:--|:--|:--|:--|
| 本地开发 | `make deps` | 三端装依赖 | 「装依赖」 |
| | `make mock` | 起 mock（:8787） | 本节注记 |
| | `make reset` | 清空数据目录（交互确认） | 「数据目录与重置」 |
| | `make serve` | 后端 `go run` 直跑（无需构建） | 「启动命令（三端都连真实后端）」 |
| | `make admin-dev` / `make app-dev` | 两端 dev server | 同上 |
| 本地测试 | `make verify` / `verify-server` / `verify-app` / `verify-admin` | 三端测试、类型检查与 lint（`verify-app` 含 APP 侧 `node --test`；`-k` 跑完全部再报） | AGENTS「验证命令」 |
| | `make test-race` | 后端 `-race` 竞态检测（不进 `verify` / CI） | 「自动化验证」 |
| 构建 | `make build` / `make build-server` / `make clean` | 二进制 | 「二进制」 |
| | `make compose-build` / `make up` / `make logs` / `make down` | 镜像 | 「Docker 镜像」 |
| | `make android-keystore` / `make android-sign-env` / `make android-apk` / `make app-icon` | APK | 「APK」 |

> **`tools/mock` 的定位：离线开发辅助**——没有后端（或不想起后端）时调界面用，`make mock` 可起零依赖 mock 服务（默认 `:8787`，`MOCK_PORT` 可改）。
>
> 它**不再是契约基准**：「mock 先行」阶段规则已随后端完成废止。契约以 `server/` 实现与 PRODUCT、`docs/design/` 的设计文档为准；需要 mock 时，由它向真实后端对齐，而不是反过来。

开发流程、编码约定与验证口径见 AGENTS「验证命令」；浏览器验证的固定做法见 AGENTS「浏览器验证」（一律用局域网地址，不用 `127.0.0.1`）。改了样式页面不变（dev server 返回旧 CSS）是 Vite 缓存问题，`touch` 该文件即可，详见客户端设计「坑与验收口径（改浮层前先看这里）」。

### 1.4 启动命令（三端都连真实后端）

三个终端各起一个（都是前台进程）：

- `make serve` — 后端：`go run` 直跑（`DATA_DIR` 默认指向仓库内 `server/data`，换目录就覆盖它：`make serve DATA_DIR=$PWD/server/data-prod`；`ADMIN_USER` / `ADMIN_PASS` 内置默认值；**改了 `server/` 保存即生效，不用先构建**）
- `make admin-dev` — 后台管理页（`:5173`）
- `make app-dev` — APP（`:1420`）；连 mock 时 `make app-dev API_PROXY=http://127.0.0.1:8787`

访问地址口径：

- 服务端地址一律填**本机局域网地址**（`hostname -I | awk '{print $1}'`；macOS 用 `ipconfig getifaddr en0`），形如 `192.168.1.23:8080`：服务常跑在容器或另一层命名空间里，`127.0.0.1` 指向的是浏览器那一侧，会连不上或连到别的东西。
- 浏览器验证页面同理——访问局域网地址而不是 `127.0.0.1`（理由与前置检查见 AGENTS「浏览器验证」）。

### 1.5 端口分配

| 服务 | 端口 | 依据 |
|:--|:--|:--|
| 后端 | 8080 | `LISTEN` 默认 `:8080`，并内嵌后台页面 |
| admin | 5173 | `admin/vite.config.ts` 默认 |
| APP | 1420 | `app/vite.config.ts`（`strictPort`），Tauri `devUrl` 固定指向 |

三者不冲突；不要再给 APP 手动指定 5173，会顶掉 admin。

### 1.6 数据目录与重置

开发默认用 `server/data`（已被 `.gitignore` 忽略，清空不影响版本库）；`make serve` 的 `DATA_DIR` 默认就是它，需要另开一份数据（如跑一份真实数据、与开发数据并存）时覆盖即可：`make serve DATA_DIR=$PWD/server/data-prod`——`server/data-prod` 同样已在忽略名单内。

`DATA_DIR` 无论走哪条路都要给成**绝对路径**：它的默认值是相对 `./data`、随进程 cwd 解析——不经 `make serve` 直接跑（`go run ./cmd/server` 或跑 `bin/server`）都会在仓库根另建一份空库，看起来像「数据全丢了」。

**重置** = 清空该目录：`make reset`（先停后端——服务运行中持有 SQLite；目标带交互确认，输 `n` 不动数据）。清空即重置一切：admin 重建、所有 token 失效、成员 / AI 供应商 / 地图源 / Webhook 全部丢失。

### 1.7 开发账号

本机开发三账号的默认凭据（**弱口令，仅限本机开发**，禁止用于任何可被外部访问的实例）：

| 账号 | 密码 | 用途 |
|:--|:--|:--|
| `admin` | `admin123456` | 登录 admin 后台（`:5173`）。Makefile `serve` 自动带上；只在库中**无** admin 时生效，已存在则忽略（改密走后台）；想换就 `make serve ADMIN_PASS=<密码>` 临时覆盖 |
| `girl` | `girl123456` | 情侣成员，登录 APP |
| `boy` | `boy123456` | 情侣成员，登录 APP |

girl / boy 由 admin 在后台「成员」页创建，本地开发按上表设密码。

### 1.8 APP 登录用成员账号，不是 admin

登录按 `client` 分流——admin 只能登录后台、APP 只接受情侣角色（口径见 PRODUCT「角色与账号」）；admin 账号走 APP 入口（`client:"app"`）登录会被 403 拒绝。所以清空数据后 APP 无法直接用 admin 登录：

1. 先用 `admin / admin123456` 登录后台 `:5173`
2. 在「成员」里创建情侣成员（girl / boy，密码见「开发账号」）
3. 用 girl / boy 登录 APP

## 2. 本地测试

### 2.1 自动化验证

`make verify` 依次跑三端（`verify-server` / `verify-app` / `verify-admin`），默认 fail-fast；`make -k verify` 跑完全部再一起报。口径（什么必须过、失败怎么办）见 AGENTS「验证命令」。

**两端前端验证强度对齐**：`verify-app` 与 `verify-admin` 都走「类型检查 + ESLint（`--max-warnings=0`）」，规则同一套（各自的 `eslint.config.js`，Vue 3 + TS 扁平配置）；差别只在 APP 侧多一层 `node --test`。admin 没有单测——它的逻辑集中在配置表单与接口调用，类型检查 + lint 已覆盖主要风险面。

**后端多一道 gofmt**：`verify-server` 在编译与测试之外跑 `gofmt -l`，列出的文件即未通过（先 `gofmt -w` 再提交）。编辑器保存时自动格式化的可以忽略这道，它是给「没配自动格式化」的环境兜底的，也是 CI 的同一道门。

**竞态检测单独跑**：`make test-race` 跑后端全部包的 `-race`（`-timeout 30m`——api 包在 `-race` 下远超 Go 默认 10 分钟超时）。它不进 `verify` 也不进 CI：每次 PR 都跑会让验证时间翻十倍，而收益只在真改了并发代码时才体现；改了并发代码就手动跑一次。

**APP 侧测试用 Node 内置 runner**（`node --test`，零新依赖，无需 vitest / jest）：用例文件与源码同目录、后缀 `.test.mjs`（纯 JS 用例，`node --test` 直接执行、零编译步骤，也不进 `vue-tsc` 与 vite 构建）；入口是 `app/scripts/run-tests.mjs`，`make verify-app` 里以 `pnpm test` 调用。该入口除了跑用例，还要求**实际执行的用例数不低于一个下限**——`node --test <glob>` 在 glob 一个文件都没匹配到时返回 exit 0，不设下限的话改名/换后缀会让整套 APP 测试静默消失、verify 依旧全绿。新增用例请同步抬高 `run-tests.mjs` 里的 `MIN_TESTS`。

**跨语言共享常量放单一数据源**：同一份值同时被服务端与 APP 使用时（如农历周期表、单条动态媒体数上限），不各写一份，而是存成 JSON 由 Go `go:embed` + APP `import` 各取同一份——`go:embed` 不允许 `..`，故 JSON 放在 `server/` 侧、APP 反向 import。这样两侧不可能漂移，也就不需要「两份表逐项比对」这类读源码的测试。

### 2.2 健康检查（全链路）

「后端起了」不等于「链路通」：页面能不能渲染、接口在页面里生不生效，只有浏览器说了算。分两层，任一层不过，先修它再往下查。

**① 浏览器层（按 AGENTS「浏览器验证」，唯一算数的链路证据）**

固定用 `mcp__chrome-devtools__*` 工具，**访问局域网地址**（IP 取法见「启动命令（三端都连真实后端）」，形如 `http://192.168.x.x:5173`）——不用 `127.0.0.1`：服务常跑在容器里，浏览器那一侧的 `127.0.0.1` 连不到它。

**APP 一律按手机页面验**：先用 `chrome-devtools-emulate` 把视口设成手机档（`390x844x3,mobile,touch`），再按页面走查——这是 APP 的基准形态，桌面 / 平板档只在改响应式或浮层时补验（客户端设计「坑与验收口径（改浮层前先看这里）」的六档）。手机视口下能验的都在这里验完，不需要另找设备。

两端各走一遍 登录 → 首屏：

| 端 | 打开 | 期望看到 |
|:--|:--|:--|
| admin | `http://<IP>:5173` | 登录页 → admin 登录 → 仪表盘，七个导航项（仪表盘 / 空间管理 / AI 配置 / 外部集成 / 回收站 / 审计日志 / 服务日志，PRODUCT「后台管理页面功能范围（admin）」） |
| APP | `http://<IP>:1420`（手机视口） | 登录页 → 成员账号登录（见「APP 登录用成员账号，不是 admin」）→ 传统模式底部五个 tab + 右下角 FAB；AI 模式未配模型 Key 时切换项置灰是预期（PRODUCT「双模式界面」） |

每端三件事：`take_snapshot` 确认上表元素在；`list_console_messages` 无 error；`list_network_requests` 里 `/api/*` 无 5xx、无代理失败。各留一张 `take_screenshot` 存档。

**② 功能走查（手机视口，改到哪验到哪）**

- 传统模式五 tab + 发布 + 通知 + 设置各走一遍主路径：列表 → 详情/弹层 → 提交，覆盖「空态 / 有数据 / 长按删除」三种状态。
- AI 模式：空会话欢迎语与 chips、发一条消息（流式正文）、出确认卡 → 确认或取消、结果卡勾选 → 引用条、停止生成、会话新建与删除。
- 交互手势在触屏模拟下验：长按（`pointerdown` 500ms）走 `ConfirmSheet`；弹层贴屏幕底、不溢出（手机档 32px 溢出是历史坑，见客户端设计「坑与验收口径」）。
- 结论落到具体观测（页面元素、console、网络响应），不写「看起来正常」（AGENTS「浏览器验证」）。
- 白屏 / console 报错 → 前端问题：查 vite 与 console，不回后端。
- 页面在但接口 4xx / 5xx → 前后端契约或登录态问题：按响应码查。

## 3. 构建（二进制 / 镜像 / APK）

构建 = 三个发布产物，各自独立、按需产出；日常开发不碰它们（后端 `go run`、前端 Vite，见「本地开发（浏览器为主）」）。三个产物本地与 CI 走同一条 make 构建链：二进制与镜像由 `Dockerfile` 调同一批目标（见「Docker 镜像」），APK 的 CI 也执行 `make android-apk`（见「CI 与发布」）。

### 3.1 二进制

| 目标 | 干什么 |
|:--|:--|
| `make build` | 全量构建：admin 构建 → 拷入 `server/internal/webui/dist/` → `go build`，产物 `bin/server` |
| `make build-server` | 只重建二进制（发布前用）；**依赖 `copy-admin`**（再依赖 `ensure-admin-dist`）：`admin/dist` 过期会先重建，再拷进内嵌目录 |
| `make clean` | 清理构建产物 |

> [!NOTE]
> `server/internal/webui/dist/` 不入版本库，内容由 `make build` / `make build-server` / `make serve`（经 `copy-admin`）填充。
> **保新规则**：`copy-admin` 依赖 `ensure-admin-dist`——`admin/dist` 不存在、或比 `admin/src`、`admin/index.html`、`admin/package.json` 旧时先跑 `make build-admin` 重建，否则跳过。因此 `make serve`（`:8080` 内嵌页）与 `make build-server`（二进制）拿到的都是当前源码构建的页面；全新克隆第一次 `make serve` 也会自动补齐（代价是那次要多等一次 admin 构建，需本机有 Node/pnpm）。
> 只有**裸 `go build` / 裸 `go run`**（不经 make）才用该目录上一次的内容——它们绕过了保新规则，改完后端页面请走 `make`。

- **别为日常开发构建它**：`make serve` 是 `go run` 直跑，改了后端代码即生效（前端页面按上面的保新规则自动重建）；二进制只在要分发 / 部署裸机时才构建。
- go 的 `GOPROXY` / `GOMODCACHE` / `GOCACHE` **不设默认值，由 go env 与用户环境决定**——需要时命令行临时覆盖（`make build-server GOCACHE=$PWD/.gocache`）；受限环境（默认 `~/.cache/go-build` 不可写）一次性 `go env -w GOCACHE=<可写路径>` 更省事，make 与裸 go 命令都受益，别把环境特例固化成仓库默认。网络能直连时同理换 `GOPROXY=https://proxy.golang.org,direct`。

### 3.2 Docker 镜像

`make compose-build`：经 compose 构建本地镜像——`Dockerfile` 内部调的正是「二进制」那批目标（先建 admin 内嵌、再 `make build-server`），构建链与本地一致；产物与 `make up` 同名同版本，构建后直接 `make up` 即跑本地产物、不拉远端。

- 跑本地镜像的**数据卷 / 首启凭据 / 端口 / TLS** 见 README「快速开始」：`DATA_DIR` 要指卷内路径（备份口径见 ARCHITECTURE「备份与迁移」），`ADMIN_USER` / `ADMIN_PASS` 只在数据目录为空时生效（同「开发账号」）。
- `make up` / `make logs` / `make down`：compose 三连，部署面权威在 README；版本用 `make up VERSION=<x.y.z>` 覆盖。

### 3.3 APK

本地签名包（自测用）三步——**需要 Rust + Android SDK**：

```text
make android-keystore    一次性：生成密钥并写 .env
make android-sign-env    从 .env 生成本地签名配置 keystore.properties（复用已有密钥，不覆盖）
make android-apk         构建 release 包，产物在 app/src-tauri/gen/android/app/build/outputs/
```

- `keystore.properties` 由 CI 构建时写入，本地即由第二步生成；缺失时 release 用未签名/默认配置，debug 不受影响。
- 本地与 CI 都经 `make android-apk` 调用（底层即 `pnpm tauri android build`）。用 `node_modules/.bin/tauri` 直调会让 CLI 把生成的 Gradle 任务写成 `executable="node"`，Gradle 阶段报 `Cannot find module '<路径>/tauri'`（CI 的 init 会自愈回 `pnpm`，本地需注意）。
- 换图标：`make app-icon`——源图是 `app/src-tauri/icons/icon.png`（512×512 主图，在仓；README 顶部那张 `assets/images/logo.png` 是同款设计的 1254×1254 大图，但 tauri 命令只吃前者）。换图标 = 替换该文件后重跑，`src-tauri/icons/` 与 Android `res/` 整套重新生成，diff 较大属正常。
- **浏览器标签页图标（favicon）**：两个前端各有 `public/favicon.png`（`admin/` 与 `app/` 一份），内容为同一张图，取自上面那次生成出的 `app/src-tauri/icons/128x128.png`。两处是各自构建根，Vite 无法共享同一份静态文件，故保留两份副本——**换 logo 时这三处要一起换**（`icons/icon.png` + 两个 `favicon.png`）。
- **发布产物仍以 CI 为准**（见「CI 与发布」）；本地包只供自测。
- **发布与应用内更新**：发布 = 推 `v<version>` tag（与 `tauri.conf.json` version 一致性由 workflow 校验）→ CI 构建并发布正式 Release；APP 从 GitHub Releases 检查 / 下载 / 安装（链路与清单推导见客户端设计「应用内更新」）。**需要强制更新时**，发布后编辑 release body 加一行 `min-version-code: <最低可用 versionCode>`（versionCode 换算见客户端设计「应用内更新」）；**回滚** = 删除最新 release，latest 自动回退上一版。

## 4. CI 与发布

三个 workflow：`ci.yml` 在推 main / 提 PR / 打 `v*` tag 时跑全量验证；`android-build.yml` 与 `docker-publish.yml` 由推 `v*` tag 触发，各产各的物。

### 4.1 CI 检查（push / PR 自动验证）

`.github/workflows/ci.yml` 只做一件事：把验证命令原样搬到 CI（口径见 AGENTS「验证命令」）。依赖安装与验证全走 make（`make deps` + `make -k verify`），本地与 CI 同一口径，不在 workflow 里另写一套命令。

| 触发 | 时机 |
|:--|:--|
| `push: branches: [main]` | 推 main |
| `pull_request` | 提 PR |
| `push: tags: ['v*']` | 打 tag（发布前先验一遍） |
| `workflow_dispatch` | 手动跑 |

- **版本口径与「环境要求」同源**：Go 跟 `server/go.mod`（`go-version-file`，不钉死数字）；Node 钉 22、pnpm 钉 11（corepack）。
- **`-k` 跑完全部再报**：三端各自出结论，不会后端一挂就看不到前端结果。
- **权限只读**（`contents: read`）：不碰仓库内容、不传制品。
- 想在本机复现 CI 行为，就是 `make deps && make -k verify`。

### 4.2 一次性配置：签名密钥与 secrets（仅 APK 需要）

运行 `make android-keystore`。脚本会交互式询问密码，然后：把密钥库生成到 `~/.love-life-keys/upload-keystore.jks`（放在仓库之外避免误提交，可用 `KEYSTORE_DIR` 覆盖）；把三项 secret 写入仓库根目录 `.env`（已被 gitignore），并打印 `gh` 一键录入命令。在仓库 Settings → Secrets and variables → Actions 录入，名称须完全一致：

| Secret 名 | 取值 |
|:--|:--|
| `ANDROID_KEYSTORE_BASE64` | 密钥库文件的 base64（脚本已生成） |
| `ANDROID_KEY_ALIAS` | key 别名（脚本默认 `upload`） |
| `ANDROID_KEY_PASSWORD` | keystore 与 key 的密码（PKCS12 两者强制相同） |

> [!CAUTION]
> **换了密钥，已安装的旧 APK 无法覆盖安装 / 更新。**密钥库请离线备份；未录入 secrets 时 workflow 会在签名步骤直接失败。

### 4.3 触发与产物

tag 名 = `v<version>`，version 必须与 `app/src-tauri/tauri.conf.json` 的 `version` 一致（推送前先核对，不一致 workflow 直接失败）：

```text
git tag v0.1.0
git push origin v0.1.0
```

推送即触发三个 workflow：CI 再验一遍，Docker 与 APK 各产各的物。

- **Docker**（`docker-publish.yml`）：推 `ghcr.io/bookandmusic/love-life`，多架构 `linux/amd64` + `linux/arm64`；tag 集合 = `v0.1.0` / `0.1.0` / `0.1`（`1` 仅非 v0.x，`v0.x` 阶段不产裸主版本）/ `sha-<短SHA>`，另由 `docker/metadata-action` 的 `flavor.latest=auto` 默认产出 **`latest`**（跟随最新 `v*` tag 移动；自部署仍应钉版本号，见 README「快速开始」）。workflow 本身只调 `docker build`，构建链在 `Dockerfile` 内（即 Makefile 的目标）。
- **APK**（`android-build.yml`）：构建步骤为 `make android-apk`，与本地同链（`keystore.properties` 由 workflow 前序步骤从 secrets 写入；本地由 `android-sign-env` 从 `.env` 生成，这是两条路径的唯一差异）。产出单个通用 APK（含 `arm64-v8a` / `armeabi-v7a` / `x86_64` / `x86` 四种 ABI）+ Play 上架用 AAB，**直接发布为正式 Release**（非草稿，`draft: false`）；workflow artifact **未设 `retention-days`**，保留期按仓库设置（默认 90 天）。产物形态与安装方式见 README「手机 APP」。

### 4.4 维护事项

- **〔两端〕**`app/src-tauri/gen/android/` 纳入版本管理（Tauri 官方约定，工程自带 `.gitignore`，构建产物与 `keystore.properties` 不会入库）。签名配置与 release 明文开关写在 `app/src-tauri/gen/android/app/build.gradle.kts`。

## 5. 发布前检查清单

首次发布前逐项确认——**下面任何一项未完成，都是发布阻断**：

- [ ] **仓库已配置 git remote 并推送**：workflow 跑在 GitHub Actions 上，没有 remote（或没推 tag）不会触发；仓库地址由仓库所有者决定。
- [ ] **待发布提交的 CI 全绿**：main 上 `ci.yml` 通过（后端编译测试 + gofmt / APP 与 admin 类型检查 lint / APP 单测）；本机复现 = `make -k verify`。tag 推送会再触发一次，别等它先失败。
- [ ] **三项签名 secrets 已录入**：见「CI 与发布」，名称须完全一致；未录入时 workflow 会在签名步骤直接失败。
- [ ] **密钥库已离线备份**：丢失后无法为已安装的 App 发布更新。
- [ ] **CI 产出的 release APK 安装并启动一次**：确认签名与打包链路产物可用；功能验收以浏览器手机视口实测为准（见「健康检查（全链路）」）。
- [ ] **确认 `.env` 的 keystore 备份仍在**：密钥不可再生，丢失 = 应用内更新全线失效，所有存量用户只能卸载重装（见客户端设计「应用内更新 · 发布约定」）。
