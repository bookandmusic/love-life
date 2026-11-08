# 服务端（server/）

Go 1.25 + net/http + SQLite（modernc，纯 Go）；单个静态二进制内嵌后台页面，全部状态落在 `DATA_DIR`。

> 本文是服务端的实现契约：接口、认证、媒体、AI 子系统、MCP、数据目录、坑与守卫。改 `server/` 前读它。
> 事实源是当前代码。路径相对 `server/internal/`（如 `api/posts.go`），`cmd/` 等其它位置写仓库根相对路径；`001_init.sql` 指 `store/migrations/001_init.sql`。行号为当前实现。

## 1. 目录与职责

| 位置 | 职责 |
|:--|:--|
| `server/cmd/server` | 入口：配置 → 开库 → 内置 admin → 密钥 → 管线 → 后台任务 → HTTP → 优雅退出（`main.go:106-203`） |
| `internal/api` | 路由表与全部 HTTP handler（`router.go:22`）；CORS、SPA 回退、错误文案 |
| `internal/auth` | 访问令牌、refresh token、登录限速、鉴权中间件 |
| `internal/media` | 两段式上传、缩略图/首帧/波形、签名 URL、临时区清理 |
| `internal/llm` | 四协议模型网关：规范化类型 + adapter + SSE 扫描 |
| `internal/chat` | AI 对话：工具循环、预算、提示词、上下文裁剪、提案、SSE |
| `internal/tools` | 工具注册表（15 个）、工具契约、卡片 spec 校验、审计辅助 |
| `internal/mcp` | 对外 MCP 通道（PAT + Streamable HTTP） |
| `internal/store` | SQLite 访问层与建库、软删/回收站、时间口径、审计、PAT、用量 |
| `internal/search` | 动态向量索引（chromem）+ OpenAI 兼容 embeddings |
| `internal/notify` | 站内通知入库 + 外部 webhook 推送 + 纪念日提醒 |
| `internal/geo` | 离线城市判定（内嵌边界多边形）+ 地名候选表 |
| `internal/uglink` | 绿联通道握手：凭据加密落库、会话缓存、失败退避 |
| `internal/appupdate` | 替 APP 从 GitHub 取版本元数据与 APK 并缓存落盘 |
| `internal/lunar` | 离线农历换算（1900–2100 表驱动，单一 JSON 源） |
| `internal/logbuf` | 进程内日志环形缓冲 + slog handler（后台日志页数据源） |
| `internal/crypto` | API Key / 密码静态加密（AES-256-GCM） |
| `internal/limits` | 服务端上限值（`media.json` 单一数据源，`go:embed`；APP 侧常量需手工对齐） |
| `internal/config` | 仅环境变量配置，无配置文件 |
| `internal/textutil` | 快照截断（API 与工具层共用，勿在调用方复刻） |
| `internal/webui` | `go:embed` 后台构建产物，供 SPA 回退 |

## 2. HTTP 接口

### 2.1 鉴权边界

| 层 | 判定 | 覆盖 |
|:--|:--|:--|
| 公开 | 无 | `GET /api/v1/health`、`POST /api/v1/auth/{login,refresh,logout}`、`GET /api/v1/media/file/{id}`、`GET /api/v1/avatar/{userID}`（后两条以签名代替 Bearer）、`GET /api/v1/map/tile`（exp+sig）、`POST /api/v1/mcp`（PAT，handler 内校验）；其余非 `/api` 路径回 SPA |
| 登录 | Bearer JWT | `GET /api/v1/auth/me`、整棵 `/api/v1/admin/`（再叠 admin）、`GET /api/v1/space`、`/api/v1/app/update/*` |
| couple | 登录 + 角色 ≠ admin | 全部业务路由；admin 不进 APP、不碰业务数据（`router.go:138-140`） |
| admin | 登录 + `role=admin` | `/api/v1/admin/`（`router.go:136`）、`PUT /api/v1/space` |

### 2.2 路由清单

认证与 AI（couple，除标注「登录」）：

| 方法 | 路径 | 用途 |
|:--|:--|:--|
| POST | `/api/v1/auth/login` | 登录，`client=admin\|app` 与角色绑定 |
| POST | `/api/v1/auth/refresh` · `/logout` | 换 access + 轮换 refresh / 吊销 refresh |
| GET | `/api/v1/auth/me` | 当前用户（登录） |
| GET | `/api/v1/chat/status` · `/chat/bootstrap` | AI 可用性 / 首屏（欢迎语 + chips） |
| POST | `/api/v1/chat/message` | 对话入口，SSE 响应 |
| POST | `/api/v1/chat/proposals/{id}/resolve` | 提案裁决 `approve\|cancel` |
| POST · DELETE | `/api/v1/chat/turns/{turn_id}`（`/abort`） | 显式中止本轮（整轮回滚）/ 回收残局 |
| GET · DELETE · POST | `/api/v1/chat/sessions`、`/{id}`、`/{id}/pin`、`/chat/history` | 会话列表、硬删、置顶、历史回显 |

动态、互动、通知（couple）：

| 方法 | 路径 | 用途 |
|:--|:--|:--|
| POST · GET | `/api/v1/posts` | 发动态（`media_tokens` 转正）/ 列表 |
| GET · DELETE | `/api/v1/posts/{id}` | 详情 / 软删进回收站 |
| POST | `/api/v1/posts/{id}/like` | 点赞切换 |
| POST · GET · DELETE | `/api/v1/posts/{id}/comments`、`/comments/{id}` | 发评论、评论列表（`limit` 默认 100 / 上限 200，返回 `has_more`）、软删 |
| GET | `/api/v1/notifications`、`/notifications/unread-count` | 通知列表 / 未读数 |
| DELETE | `/api/v1/notifications`、`/notifications/{id}` | 清空 / 消费单条（点击即删） |

媒体、相册、纪念日、地图、空间、通道（couple）：

| 方法 | 路径 | 用途 |
|:--|:--|:--|
| POST | `/api/v1/media/upload` | 上传（整文件或分片），需 couple |
| POST | `/api/v1/media/reurl` · `/discard` | 批量重签（≤200）/ 撤销临时文件 |
| DELETE · GET | `/api/v1/media/{id}` · `/media/timeline` · `/media/file/{id}` | 软删 / 时间轴 / 签名下发 |
| GET · POST · DELETE | `/api/v1/albums`（`/{id}`、`/{id}/photos`） | 相册：列表 / 详情 / 新建 / 成员增删 / 删除 |
| GET · POST · DELETE | `/api/v1/anniversaries`、`/{id}` | 纪念日：列表 / 新建（提醒天数只在此设定）/ 删除 |
| GET · POST · DELETE | `/api/v1/map/points`、`/{id}` | 地图点位（软删进回收站） |
| GET | `/api/v1/map/config` · `/map/tile` · `/geocode` | 瓦片源+签名（couple）/ 瓦片代理（签名）/ 离线地名 |
| GET · PUT | `/api/v1/space` | 空间信息（登录）/ 空间配置（admin，统一由后台维护） |
| PATCH | `/api/v1/me` | AI 偏好（couple） |
| GET | `/api/v1/styles` | 输出风格列表（后台维护；APP 设置页读取，选择后 PATCH `/me` 保存） |
| GET | `/api/v1/channels` | 统一选路 + 权重（绿联与备用地址都在这一条里下发） |
| GET | `/api/v1/app/update/latest` · `/download` | 本地已就绪的更新元数据 / APK（登录） |

后台 `/api/v1/admin/*`（仅 admin）：users（列表/新建情侣/改资料/重置密码/头像/生日）、webhooks（含 `/{id}/test`）、map-config（含 `/probe`、`/{id}/enable`）、model-providers（含 `/fetch-models`、`/{id}/key`）、models（chat 与 embedding 两链 + `/order` 排序）、styles + `/styles/default` + system-prompt、security、recycle（列表按 `?limit=&offset=` 分页，默认 50、上限 200，返回 `items`/`total`/`has_more`；单条与批量恢复；恢复日志）、mcp-tools（清单 + 单项启停）、tokens（PAT 列表/签发/吊销）、dashboard（stats/activity/ai-usage/alerts）、audit、uglink/config、appupdate（config/check/download/remove）、backup-addresses、logs。

### 2.3 横切

- CORS：仅 `/api/*`，`Allow-Origin: *`，放行 `Authorization` / `Content-Type` / `Range` / `X-Upload-*`，暴露 `Content-Disposition`、`Content-Length`、`Accept-Ranges`、`Content-Range`，`Max-Age 600`（`router.go:292-313`）；无 Cookie 故无 CSRF 面。
- SPA 回退：非 `/api` 路径一律 `index.html`（`router.go:316-332`）；错误体统一 `{"error":"中文文案"}`，5xx 额外把原始错误写日志（`api/errors.go:12-39`）。
- 请求体上限：非上传端点一律 1 MiB（`http.MaxBytesReader`，超限 400），未认证的登录/刷新同样受限；上传端点按路径豁免，文件上限由各自 handler 卡（`router.go:270-290`、`api/admin.go:267-279`）。
- 地图瓦片：鉴权走短时签名——`/map/config` 下发 `exp`+`sig`（30 分钟、空间级，只绑 `exp` 不绑坐标），签名不符或过期 401（`api/mapconfig.go:425-448,516-521`）；模板 URL 只允许 http/https 且主机不得落在回环/内网/链路本地/保留段，创建、启用与探测同校验（`api/mapconfig.go:169-198,216,311,399`）；取数时按解析出的 IP 再判一次（`tileDialContext`），302 跳转目标同样受控；上游 body 上限 8 MiB，声明 `Content-Length` 超限直接 502，chunked 超限按 `LimitReader` 截断（`api/mapconfig.go:474-478,597-603`）。

## 3. 认证

| 项 | 契约 |
|:--|:--|
| access token | HS256 JWT，TTL 1 小时，claims `uid/username/role`（`auth/token.go:16,20-25,51-64`） |
| refresh token | 32 字节随机 hex，TTL 30 天，库内只存 SHA-256（`token.go:17,84-92`）；刷新成功即删原行、发新行（`auth/service.go:239-257`） |
| 签名密钥 | 首启生成 32 字节存 `DATA_DIR/jwt_secret`（0600），重启不失效，用于 JWT 与媒体/瓦片 HMAC（`token.go:35-49`）；静态加密不共用它，另用 `DATA_DIR/crypto_key` |
| 登录限速 | `用户名\|IP` 连续 5 次失败锁 15 分钟，成功即清零；失败计数带 15 分钟窗口，两张状态表各有 4096 条 key 上界（`limiter.go:9-20,28-38`） |
| 刷新限速 | 独立命名空间 `refresh:<ip>`，只把「查不到令牌」计失败（`auth/service.go:220-234`） |
| 客户端 IP | 默认只认 `RemoteAddr`；只有直连来源命中 `TRUSTED_PROXIES`（IP/CIDR，逗号分隔）时才从 `X-Forwarded-For` 右往左取第一个不受信跳。反代终结 TLS 后必须配它，否则限速退化成全局单键（`auth/service.go:74-141`、`config.Config.TrustedProxies`） |
| 入口门禁 | `client` 只认 `admin`/`app`；admin 禁登 APP、情侣禁登后台，其它值 400（`auth/service.go:166-182`） |
| 中间件 | 只认 `Authorization: Bearer `，失败 401「认证已过期」，claims 注入 ctx（`auth/service.go:298-314`） |

后台重置密码即吊销该用户全部 refresh（`api/admin.go:256-258`、`store/tokens.go:50-57`）；但 access token 不查库，改密后最长 1 小时内仍可用（`store/tokens.go:47-49`）。

## 4. 媒体

### 4.1 管线

两段式：`Begin/Append/Finalize` 收进临时区 → 发动态时 `Promote(token, ownerID)` 转正（校验 token 形态、sidecar 归属与 `Finalized`，他人或未收尾的上传一律拒绝）。无分片头 = 整文件上传，走 `SaveTemp` 全流程（`api/posts.go:59-93`）。

| 契约 | 细节 |
|:--|:--|
| 分片协议头 | `X-Upload-Id` / `X-Upload-Offset` / `X-Upload-Final`（`api/posts.go:53-57`） |
| 四种形态 | 首片即末片、首片、中间片、末片；末片重发幂等（`api/posts.go:97-163`、`media.go:257-262`） |
| offset 不符 | 409 + `received` 数字，供客户端重同步（`api/posts.go:175-193`、`media.go:50-55,207-209`） |
| 超限 | 读取阶段即截断，413（`api/posts.go:61-68`）；累计超限删半成品（`media.go:223-228`） |
| 不存在 / 非本人 | 一律 404「上传不存在或已过期」，不泄露存在性（`media.go:46-47,192-198`） |
| 空文件 | 三类统一 400，禁止落库成 0:00 假条目（`media.go:39-42,268-274`） |
| sidecar | `media/tmp/{token}.json` 记 kind/尺寸/时长/EXIF/owner/finalized（`media.go:95-106`） |

### 4.2 缩略图、占位与时长

- 照片：最长边 1080、JPEG q85，小图不放大（`media.go:32,368-386`、`thumb.go:12-27`）。解码前先按文件头校验像素上限（约 5200 万），小文件声明的超大尺寸直接拒绝（`media/decode_limit.go:10-28`、`media.go:388-402`）。
- 视频首帧 / 音频波形：`ffmpeg` 是部署必备运行时（`ffmpeg.go:13-16`，超时 30s）；失败降级占位图，不阻塞上传（`media.go:288-306`）。
- 占位：视频灰底播放三角形、音频蓝底「音频 N」，均 SVG（`media.go:658-667`）；时长零依赖自解析（mp4/mov/m4a 走 `moov/mvhd`，webm 走 EBML，wav/mp3 各自解析，失败留空）（`video.go:14-20`、`audio.go:10-13`）。

### 4.3 存储布局（`DATA_DIR` 下）

| 路径 | 内容 |
|:--|:--|
| `media/{YYYY}/{MM}/` | 转正后的原文件（`media.go:426-463`） |
| `thumbs/{YYYY}/{MM}/` | 缩略图（照片/视频首帧 .jpg，音频波形 .png） |
| `media/tmp/`、`thumbs/tmp/` | 临时区（含 sidecar） |
| `avatars/{YYYY}/{MM}/` | 头像，独立通道，不经临时区（`media.go:613-644`） |
| `love-life.db` | SQLite（WAL + busy_timeout 5000 + foreign_keys，`store/db.go:36-51`） |
| `jwt_secret` | 令牌签名与媒体/瓦片 HMAC 的密钥材料 |
| `crypto_key` | 静态加密密钥材料（AES-256-GCM 的种子），与 `jwt_secret` 分离（`crypto/keyfile.go:19-38`） |
| `vectors/` + `vectors.dim` | 向量库与维度记录（派生数据，`search/search.go:71-84`） |
| `updates/{版本}/` | 已缓存 APK（`appupdate/fetch.go:36-38`） |

### 4.4 鉴权与下发

- 上传/撤销/删除需 couple；下发走签名 URL，TTL 15 分钟，签名域 `media:<id>:<variant>:<exp>`（`media.go:568-581`）。
- `GET /media/file/{id}` 不查 Bearer：签名有效即放行；`v=thumb|raw`，照片缺缩略图回退原图（照片缩略图在上传收尾阶段必生成，缺失仅见于文件被手删），视频/音频缺缩略图回退占位 SVG（`api/posts.go:557-622`）。
- 字节下发用 `http.ServeContent`，自带 Range/206 与 `Last-Modified`（`api/posts.go:626-642`）；`download=1` 走服务端生成文件名（`api/posts.go:613-621,647-678`）。
- 头像签名独立域 `avatar:<userID>`，与媒体签名不通用（`media.go:591-611`）。头像上传上限 5 MiB，超限 413（`api/admin.go:267-279`）。

### 4.5 清理

- janitor 每小时：临时区孤儿（24 小时未发布）、过期 refresh token、纪念日提醒、检索回填（`cmd/server/main.go:28-49`）。
- 主动撤销 `Discard`：幂等；sidecar 缺失视为已被清理 → 放行删除，sidecar 损坏或缺 owner → 拒绝并告警（`media.go:496-541`）。

## 5. AI 子系统

### 5.1 `internal/llm` 模型网关

| 契约 | 细节 |
|:--|:--|
| 协议 | `openai-chat` / `openai-responses` / `anthropic` / `gemini`，adapter 内部做规范化↔native（`adapter.go:14-20,55-62`） |
| 增量 | 统一 `Delta{Reasoning,Content,ToolCall,Usage}`；tool_calls 分片在 adapter 内归组，上层只拿完整调用（`adapter.go:65-86`） |
| SSE | 只交 `data:` 载荷，忽略事件名；非 `text/event-stream` 响应按整块解析并回放，不重发请求（`sse.go:11-13,74-97`） |
| 超时 | 流式不设总超时，只设响应头超时（默认 30s，供应商可配）（`client.go:50-74`） |
| 重试 | 同模型不重试：失败即沿链下移，无退避重试（`chat/service.go:739-787`） |
| 思考档位 | off/low/medium/high；`off` 映射为该协议最低档，不按模型名分支（`thinking.go:5-35`）；请求不传 `temperature`（`llm/modelparams_test.go:9-13`） |
| 错误分类 | `UpstreamError.StatusCode` 供上层按状态码分类，不匹配字符串（`adapter.go:29-51`） |
| 响应体上限 | 上游响应体一律 8 MiB 上限，超出部分丢弃后按解析失败处理，不裸 `io.ReadAll`（`client.go:76-85`、`llm/readlimit_test.go:48`） |

### 5.2 `internal/chat` 调度与提示词

| 项 | 契约 |
|:--|:--|
| 预算三维 | 最多 8 轮工具往返、16 次工具调用、整轮墙钟 90s（`chat/service.go:38-57`） |
| 附件预算 | 单条消息附件 9 个、合计 ≤8MB：消息阶段按客户端上报 size 预检，确认执行阶段按临时区实际文件体积复核（`chat/service.go:52,1758-1774,1855`） |
| 引用上限 | 单条消息引用 ≤3 条，超过返回 400（`chat/service.go:54,2465-2467`） |
| 截断时机 | 调用数执行前按剩余额度截断，assistant 行与 tool 回执成对入上下文（`chat/service.go:1338-1359`） |
| 收尾义务 | 触顶后必再补一次「不带工具」的调用，独立 30s 预算，失败回固定文案（`chat/service.go:47-50,1460-1500`） |
| 模型链 | `chain_order,id` 全局链序、跨供应商；链首失败切下一条（`chat/service.go:670-724`） |
| 切换边界 | 已下发增量即锁定模型，不得换模型续写，避免半句话被接（`chat/service.go:773-775`） |
| 并行 | 只并行「首个写工具之前」的读工具段；写工具串行，出提案即停本轮（`chat/service.go:1361-1377,1429-1433`） |
| 提示词 | 主干（后台 `system_prompt` → fallback）+ 输出风格片段 + 固定尾段（空间事实/能力与规矩）（`chat/service.go:201-249,614-654`） |
| 客户端卡片规则 | 请求带 `client_tools` 才注入；≤8 条，名字 `[a-z_]{1,24}`，文案截 200 字（`chat/service.go:329-363,558-611`） |
| 上下文 | 事实源 `chat_context`；按 token 预算从新到旧裁，`pinned` 永不淘汰，assistant+tool 成对进出（`chat/service.go:2362-2454`）；预算 = 链首 `context_window − max_tokens`，下限 1024，未声明窗口兜底 24000（`chat/service.go:2346-2360,2376`） |
| 卡片校验 | 正文 ```` ```ui ```` 围栏 → `ValidateSpec`；失败回灌错误码重写，至多 2 次仍失败则不出卡并记审计（`chat/service.go:380-529`） |
| 提案 | 15 分钟 TTL 单次有效；approve 原子认领（`UPDATE ... WHERE status='pending'` 影 0 行即按已裁决拒绝，并发只有一个赢家）→ `ModeExecute` → 审计 → 回执落库；过期 410、重复 409（`chat/service.go:56,1780-1902`、`store/ai.go:386-420`） |
| 断连与中止 | 断连保留已产生内容并标「中断」（`chat/service.go:1958-2010`）；主动 abort 整轮回滚删全轮（`chat/service.go:2050-2067`） |
| 会话删除 | 与「本轮登记」同一把锁串行，流式进行中拒绝删（`chat/service.go:83-87,2139-2175`） |
| 用量 | 每次调模型落 `ai_usage_logs`（成功/失败、耗时、token），失败不阻断对话（`chat/service.go:915-932`） |

SSE 事件（语义事件，无带内信令）：`session`（仅懒创建）、`turn.start`、`thinking.delta`、`thinking.end`、`answer.delta`、`answer.end`、`tool.start`、`tool.end`、`proposal`、`card`、`turn.end`、`error`。

### 5.3 `internal/tools` 工具契约

`Text`（喂模型）/ `UserText`（给人）/ `Data`（给界面）三分离；一个 `Run` 只写一次逻辑，`ModePropose` 出预览、`ModeExecute` 直接落库（`toolresult.go:20-52`）。工具内部不写审计，由调用方按通道记（`registry.go:10`）。

| 类别 | 工具 | 说明 |
|:--|:--|:--|
| 读（7） | `find_posts`、`find_albums`、`find_media`、`find_anniversaries`、`find_places`、`get_record`、`get_space_stats` | 立即执行，产出可画数据；列表类落句柄 `r_<n>` |
| 提案（7） | `propose_post`、`propose_comment`、`propose_album`、`propose_album_members`、`propose_anniversary`、`propose_place`、`propose_reaction` | `IsWrite`：内部通道出确认卡，PAT readwrite 直接执行；没有「改」类工具，业务数据只增删 |
| 删除（1） | `propose_delete` | `AppOnly`，不对任何令牌开放（`registry.go:2781-2802,3113-3121`） |

注册表在 `tools.Registry()`（`registry.go:1161`），单测守住 `Schema` 与 `Params` 键一致。卡片 7 种 view（list/grid/stats/detail/kpi/timeline/map）+ 14 个错误码；`only` 只配 stats/kpi（kpi 的 `only` 必须指向数字项，否则 `kpi_needs_single_number`），`limit` 只配 list/grid/timeline 且 1–50（`toolresult.go:145-196,226-313`）。

## 6. 对外 MCP

| 维度 | 内部通道 | 外部通道 |
|:--|:--|:--|
| 入口 | `POST /api/v1/chat/message`（couple） | `POST /api/v1/mcp`（`router.go:201-204`） |
| 鉴权 | JWT | PAT：`Authorization: Bearer llp_...` |
| 写操作 | 提案卡 + `resolve` 裁决 | 直接执行（二次确认由外部客户端负责） |
| 可见工具 | 全部（含 `AppOnly`） | `RegistryForPAT` 过滤 `AppOnly`；`readonly` 再滤 `IsWrite` |

- 传输与方法：仅 Streamable HTTP POST（非 POST 回 405），协议版本 `2025-06-18`（`mcp/service.go:24-25,71-123`）；方法 `initialize`/`tools/list`/`tools/call`/`ping`，无 `id` 的通知回 202（`mcp/service.go:95-99,101-122`）。未实现 GET/SSE 流与 `Mcp-Session-Id` 会话。
- 注解只发三列：`readOnlyHint`、`destructiveHint`（= 写且 AppOnly）、`openWorldHint` 恒 false；不发 `idempotentHint`（`mcp/service.go:198-209`）。
- 后台停用的工具既不下发也不可调用（`mcp/service.go:175-196,233-248`）；档位与停用均在服务端二次校验。启停状态读失败一律 fail-closed：`tools/list` 不下发清单、`tools/call` 直接拒绝，不按空集合放行。
- 审计：`actor_type=pat`、`actor_id=令牌 ID`，params 存人类可读摘要、digest 存原始 JSON、记耗时（`mcp/service.go:255-288`）。

## 7. 其它能力

| 包 | 要点 + 关键约束 |
|:--|:--|
| `search` | chromem 持久库 + 供应商 embeddings；相关性下限 0.5；未配置不报错而是回落关键词（`search.go:28,71-84,221-262`）；集合按「端点+模型」指纹命名，换模型即删原集合，防混维度越界（`search.go:265-297,328-332`） |
| `notify` | 站内全量入库 + 外部按事件订阅推送；6 类事件映射（`dispatcher.go:18-26`）；推送异步、总预算 30s、单渠道 10s（`:28-30,45,81-95`）；纪念日按「当日已发」幂等（`:192-224`） |
| `geo` | 内嵌地级市多边形，点入多边形判定；按名称排序保证重叠时结果确定（`geo.go:15-61`）；地名候选 20 条内置表，包含匹配取前 8（`places.go:14-56`、`api/geocode.go:13-14`） |
| `uglink` | 五步握手拿 `ugreen-proxy-token`（`login.go:1-7`）；会话缓存 TTL 1h，到期后下一次 Session 重新登录（`session.go:10-13`）；保存/删除配置时 `Invalidate` 丢弃缓存会话（`api/uglink.go:148,158`）；后台「测试」走 `Refresh` 强制重登（`api/uglink.go:163-170`、`session.go:68-72`）；并发登录串行化防风控，失败退避 10 分钟 / 20 分钟 / 1 小时（`session.go:114-174`） |
| `appupdate` | 版本号取 tag 并把 `/`、`\` 收成 `_` 后才拼缓存目录（`server/internal/appupdate/appupdate.go:353-364`）；`x*1e6+y*1e3+z` 量纲须与 APK 一致（`server/internal/appupdate/appupdate.go:366-381`）；APK 只由后台手动触发下载并落盘，APP 只下本地已有包（`api/appupdate.go:26-33`）；回源 5 次重试 + `.part` 续传（`server/internal/appupdate/fetch.go:57-192,273`）；代理仅作用于 GitHub 出站 |
| `lunar` | 表驱动 1900–2100，表坏则启动 panic；不变量测试覆盖闰月编码（`lunar.go:16-34`） |
| `logbuf` | 定长 2000 条环形，`Seq` 单调供增量拉取；重启即丢（`logbuf.go:17-36`）；`since` 大于最大序号时按全量重拉（`api/logs.go:24-44`） |
| `crypto` | AES-256-GCM，密钥 = SHA-256(`DATA_DIR/crypto_key`)；用于供应商 Key、绿联密码、代理密码、天地图 Key。轮换 `jwt_secret` 不影响它；`crypto_key` 缺失时首启以当前 `jwt_secret` 落盘一份（升级前后密钥相同，历史密文照旧可解），此后两者独立 |
| `limits` | 上限唯一来源 `media.json`（单条动态/聊天媒体数、分片大小）；解析失败 panic 而非按 0（`limits.go:15-46`） |
| `config` | `LISTEN`(:8080)、`DATA_DIR`(./data)、`ADMIN_USER/PASS`、`MEDIA_MAX_MB`(500)（`config.go:9-30`） |

## 8. 回收站、数据目录与备份

- 软删：`deleted_at + deleted_by + deleted_by_type`；可回收 6 类（动态/评论/照片/相册/纪念日/地图点位，`store/recycle.go:91-99`）。对外 id 是「表序号 × stride + 表内 id」的合成值，恢复时反解（`recycle.go:70,123-140`）。动态级联软删其媒体；相册软删即释放 `album_items`（`store/softdelete.go:21-52`）。
- 保留期：7–365 天，默认 30（`recycle.go:18-25,32-48`）；`PUT /admin/security` 为部分更新，越界 400（`api/security.go:44-76`）。
- 永久删除：每日本地 0 点跑一轮，启动先补跑；库行删 → 删媒体文件 → 写回收站操作日志；保留期越界则不删只报错（`cmd/server/main.go:65-104`），不做向量清理——软删时即从索引摘除。恢复 = 清空 `deleted_at` 并写 `restore_logs`，批量恢复与 purge 同表记录（`recycle.go:206-310,311-317`）。
- 备份：仓库内无备份接口、无备份脚本。`/admin/backup-addresses` 是「备用服务地址」而非数据备份（`api/backupaddr.go:29-45`）；口径是停机后整体复制 `DATA_DIR`——WAL 开启（`store/db.go:38`），进程退出时 `defer db.Close()`（`cmd/server/main.go:128`）由 SQLite 在最后连接关闭时 checkpoint，不必手工搬 `-wal/-shm`。
- `scripts/sql/fix-time-format.sql` 是一次性人工 SQL，不参与自动建库（`go:embed` 只收 `internal/store/migrations/*.sql`，`store/migrate.go:12`）。
- admin：仅当库中无 admin 时用 `ADMIN_USER/ADMIN_PASS` 创建，已存在则忽略环境变量、改密走后台（`main.go:318-341`）。

## 9. 坑与守卫

改到相关代码先跑「守卫」里的测试；这些测试就是本节契约的机器可执行版本。

| 约束（不许改坏） | 守卫 |
|:--|:--|
| 库内时间列一律 UTC `YYYY-MM-DD HH:MM:SS`，读侧另认 RFC3339；混用会让「已过期」被判成未过期（字符串比较 `'T' > ' '`） | `store/timeutil_test.go:71`、`api/chat_p22_expiry_test.go:17,50` |
| 用户可见日期按进程时区（`TZ`）换算；EXIF `taken_at` 是无时区墙钟时间，不参与换算；写入时间列必须用 `FormatDBTime` | `store/timeutil_test.go:9,31,71`、`media/media_test.go:180` |
| 空文件三类同口径拒收：照片不靠缩略图解码失败挡、音视频不靠容器头解析失败挡，统一在收尾拒绝 | `api/upload_empty_test.go:19,69`、`media.go:268-274` |
| 分片 offset 落后必须 409 + 数字长度；末片重发幂等，归属不符按「不存在」处理 | `api/upload_chunk_test.go:110,132,163` |
| 流式进行中删会话必须 409（`turn` 未终结），否则内容写进已删会话 | `api/chat_p25_session_guard_test.go:16` |
| 已下发增量后不得换模型续写；预算耗尽不得静默结束，必须收尾；收尾不复用已耗尽的 deadline | `chat/budget_test.go:59,80,103,125,146`、`chat/wrapup_test.go:10` |
| 模型报错原文不得透给用户（含内网地址与 Go 术语），只给分类人话 + `retryable` | `chat/modelerror_test.go:81` |
| `tool_calls` 与 tool 回执必须成对入上下文，孤儿 tool 行属协议违规 | `api/chat_read_tool_pair_test.go:22,95,124` |
| 登录失败计数与锁定表都必须有 key 上界（公开端点可被任意用户名刷条目），过期项先清、仍超则按最旧淘汰 | `auth/limiter_test.go:147,183,215` |
| 错误响应必须是合法 JSON 且 `Content-Type: application/json`；消息里的引号/换行由 encoder 转义，不能手拼字面量 | `chat/error_response_test.go:12` |
| 仪表盘「今天/昨天」按本地日历日比较，不能用 `time.Since/24`（23 小时前可能是昨天） | `api/dashboard_test.go:232` |
| GET 推送渠道的 URL 可能自带 query，追加参数必须解析后合并，不能直接拼 `?` | `notify/dispatcher_test.go:14` |
| CORS 必须放行 `Authorization` / `Content-Type` / `Range` / `X-Upload-*`，暴露 `Content-Disposition`；非 `/api` 路径不加 CORS 头 | `api/cors_test.go:12` |
| 卡片校验失败不静默降级：回灌错误码让模型重写，连续两次失败才放弃并审计 | `api/chat_card_test.go:56,87` |
| 回收站对 AI 不可见（语义检索、详情、删除预览） | `api/recycle_ai_hidden_test.go:26,68` |
| 服务日志游标跨重启必须重置，否则页面永远不出新日志 | `api/logs_test.go:88` |
| `POST /api/v1/mcp` 只做 POST；PAT 哈希冒充、已吊销、畸形头一律 401 | `mcp/security_test.go:29,53,74` |
| 农历换算的日上限按「该月是否闰」取：闰月用 `leapDays`，平月用 `monthDays`（2017/2036 闰六月有 30 天） | `lunar/lunar_test.go:19,68` |
| 纪念日提醒去重按本地日历日（`date(created_at,'localtime')`），混用 UTC 日会让同一本地日发两次 | `store/anniversaries_test.go:10` |
| 删 AI 会话必须连带清掉提案行：先收集 `card_id` 再删消息，顺序反过来子查询恒空 | `store/ai_test.go:10` |
| 临时媒体转正（`Promote`）校验 token 形态、sidecar 归属与 `Finalized`；他人/半成品上传按「不存在」处理 | `media/promote_test.go:10` |
| 向量集合的读路径只读不删：`Has` 与 `Remove` 同口径（`writeCfg` 优先），否则一次存在性检查会删掉真实集合 | `search/search_test.go:367` |
| kpi 卡的 `only` 必须命中数字项，写错名字不是静默空卡而是报错让模型重写 | `tools/cardspec_test.go:58` |
| 相册加媒体要拒绝已单删的媒体（`media.deleted_at`），只看所属动态会让回收站里的照片混进相册 | `store/albums_test.go:9` |
| 媒体探针只读容器头（mp4 box 步进 / wav chunk 头 / mp3 与 JPEG 头部窗口），不许整文件读入内存 | `media/media_test.go:203`、`media/video_test.go:187` |
| 提案执行失败的回执只落一句人话（同时进模型上下文与 History），工具错误原文只进服务端日志 | `api/chat_context_test.go:202-256` |
| 仪表盘活动流排除已软删动态的赞与评、以及已单删的评论 | `api/dashboard_test.go:261` |
| 恢复日志与评论列表必须有上限并回报 `has_more`，无界返回会随历史增长拖垮响应 | `api/security_recycle_test.go:258`、`api/interactions_test.go:353` |
| 提案裁决靠 `UPDATE ... WHERE status='pending'` 的受影响行数认领：影 0 行必须报「已裁决」，否则并发双 approve 会各执行一次写工具 | `store/confirm_claim_test.go:11` |
| 非上传端点的请求体一律 1 MiB 上限（未认证的登录/刷新也在内），按路径而非 Content-Type 判断，伪造头绕不过 | `api/body_limit_test.go:13,32,53` |
| 图片解码前按文件头校验像素上限（约 5200 万），小文件声明的超大尺寸直接拒绝；头像上传 5 MiB 上限 | `media/decode_guard_test.go:41` |
| 静态加密只认 `crypto_key`：轮换 `jwt_secret` 不得让已存的供应商 Key / 凭据解不开 | `crypto/keyfile_test.go:11,31`、`api/crypto_key_test.go:14` |
| `crypto_key` 内容不成形或无可继承密钥时启动即报错，不许静默换一把新密钥（否则历史密文永久解不开） | `crypto/keyfile_test.go:51,62` |
| 嵌入链的供应商 Key 也必须用 `crypto_key` 解：传 `jwt_secret` 会静默退化为关键词检索 | `cmd/server/main_test.go:257` |
| 限速的客户端 IP 只认 `RemoteAddr`；配了 `TRUSTED_PROXIES` 才采信 `X-Forwarded-For` 的最右不受信跳（防伪造） | `auth/clientip_test.go:20,33,45,57,73` |
| 登录/刷新 5xx 必须落 slog（场景+原文），响应体不得外泄内部错误 | `auth/service_test.go:258` |
| 工具启停状态读失败必须 fail-closed：`tools/list` 不下发、`tools/call` 拒绝；chat 侧执行同样拒绝，只有下发给模型的清单可 fail-open+告警 | `mcp/failure_test.go:23`、`chat/proposal_failure_test.go:31` |
| 外部 MCP 的工具错误只回分类人话，原文只进服务端日志；审计写失败必须告警 | `mcp/failure_test.go:79`、`:124` |
| 提案落库失败不得裸 return：必须记日志并发 `error` / `turn.end` 事件 | `chat/proposal_failure_test.go:91` |
| Registry 注释里的工具数字必须与注册表一致（降级为 15 = 读 7 / 提案 7 / 删 1） | `tools/registry_test.go:797` |
| tools 包的测试必须等后台推送排空（`Dispatcher.Wait`），否则 `-race` 下 TempDir 清理必失败 | `tools/registry_test.go:57`、`notify/dispatcher_test.go:94` |
| LLM 上游响应体一律 8 MiB 上限，不许裸 `io.ReadAll` | `llm/readlimit_test.go:48` |
| 默认推送 body 用 `json.Marshal` 组装，手写转义漏控制字符会产出非法 JSON | `notify/dispatcher_test.go:58` |
| 绿联会话过期只认 TTL，到期必须重登 | `uglink/uglink_test.go:127` |
| appupdate check 的失败原因走可读映射，不透代理/上游原始错误 | `api/appupdate_test.go:12` |
| 回收站列表必须分页：默认 50、上限 200，非法参数回落，返回 `items`/`total`/`has_more` | `api/recycle_pagination_test.go:22`、`store/recycle_pagination_test.go:10` |
| 更新版本名里的路径分隔符进缓存目录前收成单段，清理不会误删刚建的版本目录 | `appupdate/appupdate_test.go:278` |
| 确认执行前按临时区实际文件体积复核附件合计，超限在原子认领之前拒绝 | `chat/media_budget_test.go:47,87` |
| 引用条数上限 3 由服务端收口，改包客户端不能绕过 | `chat/quote_limit_test.go:12` |
| 瓦片上游 body 有上限（声明超限直接 502，chunked 截断） | `api/mapconfig_limit_test.go:11` |
| 瓦片模板 URL 只允许 http/https 且主机不得落在内网/保留段，创建、启用与探测都要校验 | `api/mapconfig_ssrf_test.go:18,52` |
| 瓦片取数拨号前按解析出的 IP 复判内网/保留段（302 跳转目标同样受控） | `api/mapconfig_ssrf_test.go:110,128,138` |
