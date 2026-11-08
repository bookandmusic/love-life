# AGENTS.md — agent 工作规则

> 仓库内 agent 的唯一入口：命令、改动流程、验证口径、文档地图。其余文档按任务需要再读，不要一次全读。

## 1. 项目速览

| 端 | 目录 | 技术栈 | 入口 |
|:--|:--|:--|:--|
| 服务端 | `server/` | Go 1.25 + net/http + SQLite（modernc，纯 Go） | `server/cmd/server/main.go` |
| APP | `app/` | Kotlin + Jetpack Compose（原生） | `app/app/src/main/java/com/bookandmusic/lovelife/appnative/MainActivity.kt` |
| 后台 | `admin/` | Vue 3 + Vite + naive-ui + pnpm | `admin/src/main.ts` |

发布形态：单个静态 Linux 二进制，内嵌后台页面（`go:embed`）；全部状态落在 `DATA_DIR`，部署就是「一个二进制 + 一个数据目录」。

## 2. 命令（一律走 make，不裸调 go / gradle 出产物）

| 场景 | 命令 |
|:--|:--|
| 装依赖 | `make deps` |
| 本地跑后端（日常开发，改代码即生效） | `make serve` |
| 后台 dev server（:5173，代理到 :8080） | `make admin-dev` |
| 清空本地数据 | `make reset`（交互确认） |
| 全量验证 | `make verify`（fail-fast；`make -k verify` 跑完再报） |
| 文档/注释门禁 | `make verify-docs`（文档与注释一致性，脚本校验） |
| 后端验证 | `make verify-server`（build + vet + test + gofmt） |
| 后台验证 | `make verify-admin`（vue-tsc + eslint + 样式守卫） |
| APP 验证 | `make verify-app`（需 JDK 25 + Android SDK；不出包） |
| 竞态检测（不进 CI） | `make test-race` |
| 出二进制 | `make build`（含后台页面） |
| 出镜像并启动 | `make compose-build` → `make up` |
| 出 APK | `APP_DEFAULT_BASE=http://<地址>:8080 make build-apk` |

三条出产物的硬规则：

1. **禁止裸 `go build`**：`server/internal/webui/dist` 只入库 `.gitkeep`，裸构建能通过但没有后台界面，失败是静默的（`Makefile:121-131`）。
2. **APP 出包必须给 `APP_DEFAULT_BASE`**：仓库内不写死任何服务地址（`app/app/build.gradle.kts:35-38`）。
3. **版本号唯一源是 git tag `v*`**：CI 从 tag 传入并回读产物核对；版本号换算与构建期门禁在 `app/app/build.gradle.kts:11-24`。

## 3. 改动流程

1. **先读后写**：改哪端先读该端文档与相关代码；需求有歧义就问，一次只问一个决策点，给推荐项。
2. **先报影响再动手**：说明改哪些文件、影响哪些端、有无不可逆操作（删数据 / 改协议 / 换存储）。
3. **改完就跑验证**：只改一端就跑该端 `verify`；跨端或改数据模型跑 `make -k verify`。验收一律以脚本命令为准（`make verify` / `make verify-app`），「我看过了」不算验收。
4. **文档同轮改完**：行为变了就改对应文档；删概念时全仓搜残留（代码、注释、测试、文案）。
5. **修 bug / 补边界必须留守卫**：同一轮补一条能自动跑的测试，并登记到该端文档的「坑与守卫」表；确实无法自动化的，标「无测试」并写清人工验收步骤。
6. **不主动提交**：`git commit` / `push` 必须由用户明确要求；提交前展示 staged 清单与提交信息。

计划放 `docs/dev/`（不入库），执行完即删；长期结论回填权威文档。

### 3.1 审核结果落文档

审核结论落到 `docs/dev/review.md`（`docs/dev/` 不入库），不靠对话传递。审核只读：不改代码、不提交。

文件头固定三项，缺一项视为没审完：**范围**（审了哪些目录/文件）、**基线**（`git rev-parse --short HEAD`，无提交时写「未提交工作区」）、**未覆盖**（没审到、或只能人工验证的部分）。

条目一条一行，严重度只有三档：

| 严重度 | 位置 | 问题 | 证据 | 建议 |
|:--|:--|:--|:--|:--|
| `[CRITICAL]` | `文件:行` | 一句话说清坏在哪 | 复现命令、测试名或代码片段 | 一句话修法 |
| `[MAJOR]` | 同上 | 同上 | 同上 | 同上 |
| `[MINOR]` | 同上 | 同上 | 同上 | 同上 |

判据：`CRITICAL`/`MAJOR` 阻塞提交；`MINOR` 可不修。怎么改按第 3 节的改动流程走，这里只定审核结果的形态。没有证据的猜测不写进条目，改写进「未覆盖」。

### 3.2 文档与注释只写当前事实

文档、注释、提交信息描述的都是「现在是什么」，不是「以前是什么、为什么改」。**版本演进归 git**：谁改的、什么时候改的、改了几版，用 `git log` / `git blame` 查，不抄进正文。

| 不写 | 改成 |
|:--|:--|
| 历史叙事：「此前」「原先」「曾经」「旧版」「已废弃」「迁移自」「本次改动」 | 直接写当前行为与约束 |
| 日期、修订记录、CHANGELOG、版本历史小节 | 删掉；发布号只由 git tag 决定 |
| 下线功能的墓碑：「已移除 X，改用 Y」 | 相关段落整段删除，正文只留 Y |
| 注释里解释「为什么当初这么写」的来龙去脉 | 只写「现在为什么必须这样」的约束本身 |

例外（属于当前事实，照常写）：依赖与协议版本（如 MCP 协议 `2025-06-18`）、算法常量、外部服务的接口约定。

删概念时全仓搜残留（代码、注释、测试、文案、配置），漏一处就算没删干净。

## 4. 验证口径

| 层 | 命令 | 判据 |
|:--|:--|:--|
| 文档/注释 | `make verify-docs` | 文档门禁全过；注释不含文档引用与历史叙事；源码不被 `.gitignore` 吞掉 |
| 行为 | `make verify-server` / `make verify-admin` / `make verify-app` | 对应端全绿；改到跨端时 `make -k verify` |
| 质量 | 同上（gofmt / vue-tsc / eslint --max-warnings=0 / Android Lint） | 无新增告警 |
| 界面 | admin 用浏览器实测；APP 用真机或模拟器 | 看到具体元素、控制台无报错、截图留证 |
| 竞态 | `make test-race` | 手动跑，结果写进提交说明 |

需求与架构文档没有自动化守卫：`REQUIREMENTS.md` 的验收靠人工评审，`ARCHITECTURE.md` 的结构/引用一致性由 `make verify-docs` 校验；各端文档的「坑与守卫」表才是可执行的验收清单。

界面验证一律填可访问的服务地址（局域网 IP 或域名），不要用 `127.0.0.1`——它未必指向你正在验证的那个服务。

## 5. 文档地图

| 文件 | 什么时候读 |
|:--|:--|
| `docs/product/REQUIREMENTS.md` | 判断要做什么、用户可见规则、上限、验收与待评审决策 |
| `docs/tech/ARCHITECTURE.md` | 改跨端结构、数据模型、部署、CI |
| `docs/tech/SERVER.md` | 改 `server/`：接口、媒体、AI 子系统、MCP |
| `docs/tech/APP.md` | 改 `app/`：页面、数据层、AI 对话渲染、视觉令牌 |
| `docs/tech/ADMIN.md` | 改 `admin/`：路由、组件、令牌、样式守卫 |
| `README.md` | 项目介绍、部署、安装与运维（面向使用者） |
