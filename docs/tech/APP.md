# APP（原生 Kotlin + Compose）

结论：两个 Gradle 模块单向依赖（`:app` → `:core:designsystem`）；无 ViewModel、无 Room / DataStore；页面状态放 Compose 快照状态，全局状态放单例 `object`；AI 卡片形态由服务端 `validated` 决定，客户端不推断。源码路径相对仓库根，`…/appnative/` = `app/app/src/main/java/com/bookandmusic/lovelife/appnative/`，`…/designsystem/` = `app/core/designsystem/src/main/java/com/bookandmusic/lovelife/appnative/designsystem/`。

## 1. 模块与职责

| 模块 | 类型 | 职责 | 关键依赖 |
|:--|:--|:--|:--|
| `:app` | android.application | 应用壳、全部业务页面、数据层、AI 渲染、应用内更新、媒体 | `:core:designsystem`（`app/app/build.gradle.kts:108`）、okhttp、security-crypto、kotlinx.serialization、coil、media3 |
| `:core:designsystem` | android.library | 主题令牌（色/字/形/间距/chrome）+ 21 个原子文件、38 个公开 `@Composable` 入口（35 个组件与样式函数 + 3 个 `Modifier` 扩展）；零业务、零字体资源 | Compose BOM / UI / foundation / material3 / icons、coil，均 `api` 透传（`app/core/designsystem/build.gradle.kts:23-31`） |

- 依赖方向单向：designsystem 无任何 `project(...)` 依赖。
- 根工程 `lovelife-native`，只有两个 include（`app/settings.gradle.kts:18-20`）；两模块同工具链 compileSdk 37 / minSdk 26 / JDK 25（`app/app/build.gradle.kts:41-46,77`）。
- 字体属 `:app`（`res/font/lxgw_wenkai_lite.ttf`），经 `AppTheme(titleFont=…)` 注入（`MainActivity.kt:71-75`；`…/designsystem/theme/Type.kt:18`）。
- 服务地址构建期注入：`BuildConfig.DEFAULT_BASE` ← 环境变量 `APP_DEFAULT_BASE` 或 `-PAPP_DEFAULT_BASE`，仓库不写死地址（`app/app/build.gradle.kts:35-38,49`）。
- 出包硬门禁（`gradle.taskGraph.whenReady`）：无 `DEFAULT_BASE` 不许 assemble / bundle / package，release 另需 `keystore.properties`；`versionCode = major*1000000 + minor*1000 + patch`（`app/app/build.gradle.kts:15-24,85-105`）。

## 2. 数据层
### 2.1 网络

结论：统一走 `ApiClient`（OkHttp + 单例 object），base 由 `Channel` 选路决定。

| 契约 | 事实 | 依据 |
|:--|:--|:--|
| 内置地址 | `BuildConfig.DEFAULT_BASE` | `…/appnative/data/ApiClient.kt:40` |
| 选路 | 内置（权重 100）+ 后端 `GET /api/v1/channels` 合并，按 weight 降序探活 `GET /api/v1/health`（5s 超时），第一个通的即 base；全不通保留上次通道 | `Channel.kt:33,59,109,123` |
| 重选时机 | 启动 `init()`、首次请求、回前台（60s 去抖）、前台每 5 分钟；绿联凭据到期前 60s 续期 | `Channel.kt:80,83,184,192,200` |
| 鉴权头 | `Authorization: Bearer` + 绿联 `Cookie`（`applyAuth`） | `ApiClient.kt:195-198` |
| 401 | `Mutex` 单飞刷新 + 单次重放；刷新失败 → 清会话 + 置 `sessionExpired` 事件 → 主界面回登录页 | `ApiClient.kt:112-120,145-170` |
| 其他失败 | 网络异常或非 401 失败 → `markFailed` → 重选 → 重试一次（仅非 auth 路径） | `ApiClient.kt:98-110,122-128` |
| 超时 / 空体 | `callTimeout` 30s；204 返回 null（`Unit`） | `ApiClient.kt:38,136` |
| 编解码 | `ignoreUnknownKeys` + `coerceInputValues`；`ApiError` 透传服务端 `error`，否则调用方 fallback | `ApiClient.kt:35-36,129-134,201-203` |

对话路径例外：**发消息前**才选路/续期，收帧后不再切通道（`ChatRepository.kt:204-205`）。

### 2.2 仓库（全部 `object`，无 DI 容器）

| 对象 | 端点 | 职责 | 依据 |
|:--|:--|:--|:--|
| `AuthRepository` | `/api/v1/auth/*` | 登录、记住登录、启动恢复三分支（Ok / Expired / Network） | `AuthRepository.kt:7-64` |
| `SpaceRepository`、`AnniversariesRepository`、`AlbumsRepository`、`PointsRepository`、`CommentsRepository`、`NotificationsRepository` | `/api/v1/{space,anniversaries,albums,map/points,comments,notifications}` | 六个业务域读写；纪念日列表按 `days_until` 升序 | `Domains.kt:32-295` |
| `PostsRepository` / `MapRepository` | `/api/v1/posts`、`/api/v1/media/reurl` / `/api/v1/map/config`、`/tile` | 动态流、发布、删除、媒体重签 / 瓦片代理与换签（提前 2 分钟，夹 1–25 分钟；首拉失败 8s 重试） | `PostsRepository.kt:132-196`；`MapRepository.kt:34-56` |
| `MediaRepository` / `ChatRepository` | `/api/v1/media/upload`、`/discard` / `/api/v1/chat/*` | 上传分片与本机元数据 / 会话、历史、SSE、提案裁决 | `MediaRepository.kt:56-320`；`ChatRepository.kt:119-167` |
| `AppUpdate` / `DismissedUpdateStore` | `/api/v1/app/update/latest` | 清单解析、版本比较、「稍后」记忆 | `AppUpdate.kt:46-108` |

纯逻辑无网络：`ChatState`（渲染状态机）、`ChatUi`（卡片契约）、`UiFence`（围栏解析）、`Channel`（选路）。

### 2.3 本地存储

结论：只有 SharedPreferences（一处加密、两处明文），**没有 Room / DataStore**（版本目录与源码均无引用）。

| 存储 | 文件 / 位置 | 内容 | 依据 |
|:--|:--|:--|:--|
| EncryptedSharedPreferences（AES256-GCM + AES256-SIV） | `ll_secure` | token、refresh token、记住的用户名密码；安全区不可用时降级为仅内存，不落明文 | `SecureStore.kt:26-34,58-67` |
| SharedPreferences | `lovelife_theme` | `theme_mode`、`accent`、`app_mode`（classic / ai），只存本机不上服务端 | `ThemePreferences.kt:7-12,25-53` |
| SharedPreferences | `lovelife_update` | `dismissed_version_code` | `AppUpdate.kt:97-108` |
| 文件缓存 | `cacheDir` | 上传临时文件、APK 成品与 `.part` | `MediaRepository.kt:133,165`；`UpdateDownloader.kt:76` |

## 3. 导航与页面

结论：单 Activity + 手写 `sealed Route`（无 Navigation Compose），顶层三阶段 Boot / Login / Main。

| 阶段 | 触发与行为 | 依据 |
|:--|:--|:--|
| `Boot` | 默认；恢复会话与取通道并行，总超时 15s。网络失败留页可重试，凭据失效转 Login | `MainActivity.kt:76-80`；`BootScreen.kt:51-75` |
| `Login` | 未登录或退出登录 | `MainActivity.kt:81-84`；`LoginScreen.kt:48` |
| `Main` | `DemoApp` | `MainActivity.kt:85-101` |

底部导航 5 项（`DemoApp.kt:57-63`）：我们 / 时光 / 相册 / 纪念日 / 足迹，图标 `FavoriteBorder / History / PhotoLibrary / Cake / Place`（`DemoApp.kt:508-514`）；选中态 = 主色 12% 胶囊（`AppNavBar.kt:85`）。

| 路由 | 页面 | 顶栏标题 | 底栏 | 依据 |
|:--|:--|:--|:--|:--|
| `Main` | 当前 tab（`MainTabs`） | tab 名 | 显示 | `DemoApp.kt:193-212` |
| `Settings` / `Notifications` | `SettingsScreen` / `NotificationsScreen` | 设置 / 通知 | 隐藏（不属于任何 tab） | `DemoApp.kt:213-229,242-275` |
| `Chat` | `ChatScreen` | 「我们的 AI 助手」，自带顶栏不叠壳顶栏 | 隐藏 | `DemoApp.kt:230-241,334-337` |
| `About` / `Anniversaries` / `AnniversaryForm` | `AboutScreen` / 纪念日列表 / 纪念日表单 | 关于 / 纪念日 / 添加纪念日 | 显示 | `DemoApp.kt:276-301` |
| `AlbumForm` / `PointForm` / `Publish` / `AlbumDetail` | 三个表单页 / 相册详情（顶栏用相册名） | 新建相册 / 添加地点 / 记动态 / 相册名 | 显示 | `DemoApp.kt:302-331` |

- 二级页 = 除 `Main` 外全部（`DemoApp.kt:66-81`）；返回目标 `backTarget`：`AnniversaryForm → Anniversaries`、`About → Settings`，其余 → `Main`，根页（`Main` / `Chat`）返回 null（`DemoApp.kt:87-94`）。系统返回键与顶栏返回共用同一分支（`DemoApp.kt:138-144,431`）。
- 顶栏动作分工：「我们」只给通知 + 设置；其余四个 tab 只给「+」新增入口（`DemoApp.kt:369-389,498-505`）。
- 页面内边距：顶部 = 状态栏 + 44dp 标题行 + 8dp；底部 = 手势区 + 68dp 导航条 + 8 + 12（`DemoApp.kt:166-173`）；AI 页顶部只让状态栏（`DemoApp.kt:182-183`）。
- 设置页返回要提交「界面模式」暂存，顶栏与系统返回都走 `leaveSettings()`，按新模式落位 `Chat` 或 `Main`（`DemoApp.kt:130-134,362-363,431`）。

## 4. 媒体

结论：**选图只读本机元数据，确认执行才上传**——取消 / 过期零残留。

| 阶段 | 行为 | 依据 |
|:--|:--|:--|
| 选择 | 只读 name / mime / size，不联网；上限 9 个（相册选择器与音频文档选择器共用入口） | `ChatScreen.kt:169-196`；`MediaRepository.kt:284-298` |
| 发消息 | 只提交 `AttachmentMeta(name, type, size)`，不带 token；`type` 由 kind 归一成 image / video / audio 前缀 | `ChatScreen.kt:398-405`；`ChatRepository.kt:311` |
| 提案到达 | 只对吃媒体的意图（`propose_post` / `propose_album` / `propose_album_members`）绑定本轮媒体到 `action_id` | `ChatScreen.kt:418-426,861-862` |
| 确认执行 | 逐张 `upload()` 换 `media_token`；任一张失败整卡退回待确认并撤销已传 token，服务端不落任何媒体；裁决没落库时同样撤销已传 token | `ChatScreen.kt:468-501` |
| 取消 / 过期 | 不上传，服务端无孤儿文件 | `ChatScreen.kt:169-176` |

上传管线（`MediaRepository.kt`）：单文件上限 500MB（`:61`，对齐服务端 `MEDIA_MAX_MB` 默认值）；≤ 8MB 走整文件 multipart（`:152-165,193-215`），> 8MB 走分片 `X-Upload-Offset / X-Upload-Id / X-Upload-Final`（`:65,98-134`）。上传复用单例 OkHttpClient，显式 connect 15s / read 30s / write 60s（`:87-89`）。单条 9 个与分片 8MB 仍是客户端硬编码，需与服务端 `limits/media.json` 手工对齐（无构建期同步）；推进位置以服务端回 `received` 为唯一依据，409 用它重同步（`:98-134`）；单片遇网络层 IOException 自动重试 3 次（1s / 2s / 4s 退避，按原 offset 重发、不重发其他片；HTTP 错误不重试，见 `:139-150`），仍失败则整次上传失败、需用户重发；无跨进程续传，临时文件用完即删。

时间轴分页：`AlbumsRepository.timeline` 每页 50（服务端硬夹 50），是否还有下一页只看服务端 `has_more`（`Domains.kt:137-152`）；时光页首屏与翻页都读它（`DemoScreens.kt:115,136`），选图器用 `timelineAll()` 按 `has_more` 翻页拉全（最多 20 页兜底，`DemoScreens.kt:600`）。

| 查看器能力 | 实现 | 依据 |
|:--|:--|:--|
| 翻页与关闭 | `HorizontalPager` 多张；系统返回键关闭，无关闭按钮 | `MediaViewer.kt:84-105` |
| 图片 | `ContentScale.Fit`；双指缩放夹在 1–4x，双击 2.5x | `MediaViewer.kt:524-578` |
| 视频 / 音频 | Media3 `ExoPlayer` 渐进播放（视频带 `PlayerView` 控制器），缓冲与失败提示、点击重试 | `MediaViewer.kt:246-345,410-465` |
| 播放前换签 | 签名 URL 剩余 ≤ 120s 先 `POST /media/reurl`，失败用原地址；请求只加绿联 Cookie 不加 Bearer | `MediaViewer.kt:231-243,400-403` |
| 长按与复用 | `onLongPress` 上抛给页面层弹 `AppActionSheet`（保存 / 删除）；时光、相册（时间轴 / 相册详情）三处共用 | `MediaViewer.kt:110-119`；`TimelineScreen.kt:306`；`DemoScreens.kt:188,654` |

另存到本机：`rememberMediaSaver`（`MediaSave.kt:34-115`）。系统 `CreateDocument("*/*")`，文件名 `lovelife_<毫秒时间戳>.<ext>`，扩展名取 URL 真实后缀（取不到按 video→mp4 / audio→m4a / 其他→jpg）；下载走 OkHttp（连接/读取 15s、整体 60s），绿联通道补 `Cookie`，响应的 `Content-Type` 必须是 image/video/audio 才落盘，失败给具体原因（`：86-110`）。

## 5. 应用内更新

结论：更新只经自己的服务端，APP 不直连 GitHub；下载与业务请求同一 base、同一凭据。

| 环节 | 事实 | 依据 |
|:--|:--|:--|
| 清单 | `GET /api/v1/app/update/latest`；`available:false` = 暂无就绪包，按「无需更新」而非错误 | `AppUpdate.kt:47-63` |
| 版本比较 | 只用 `versionCode`；强更 = 本机 < `min_version_code`，提示级，不做准入拦截 | `AppUpdate.kt:66-73` |
| 状态机与决策 | `Idle / Checking / Prompt / Downloading / Ready / Handoff / Failed`；决策顺序即语义：无更新→Idle；手动→（有成品?Ready:Prompt）；（非强更且该版本已「稍后」）→Idle；有成品→Ready；否则 Prompt | `UpdateController.kt:17,75-90,119-138` |
| 「稍后」 | SharedPreferences 记 `versionCode`，强更不记；有成品包时不清 `release`，设置页保留「待安装」 | `UpdateController.kt:152-157`；`AppUpdate.kt:97-108` |
| 下载 | 前台服务（`foregroundServiceType=dataSync`）保活；同进程直接写 `StateFlow`，无跨进程通道 | `UpdateService.kt:27,76-98`；`AndroidManifest.xml:30-33` |
| 断点续传 | `Range: bytes=N-` 写 `.part`；206 追加 / 200 重下 / 416 删了重来；IO 异常保留 `.part`；进度 ≥ 200ms 或整数百分比才推一次；磁盘不足立即失败 | `UpdateDownloader.kt:129-134,143-190,216-218,238-243` |
| 完整性 | 只校验「最终字节数 == 服务端声明大小」；安装包身份由系统安装器签名一致性把关；只清非当前版本的 `.apk` / `.apk.part` | `UpdateDownloader.kt:250-255,271-278` |
| 安装交接 | FileProvider `content://` + `ACTION_INSTALL_PACKAGE`（退 `ACTION_VIEW`）；缺授权先跳系统页，回来自动重试 | `ApkInstaller.kt:36-84`；`MainActivity.kt:34-41`；`UpdateController.kt:222-228` |
| 弹窗 | 根节点常驻，Boot / Login / Main 三阶段共用；强更不可关、无「稍后」（下载中除外） | `MainActivity.kt:103-104`；`AppUpdateSheet.kt:44-77` |

出站约束：

| 约束 | 事实 | 依据 |
|:--|:--|:--|
| 域名 | 只用 `Channel.base()`（内置地址或后端下发通道） | `UpdateDownloader.kt:149` |
| 下载地址 | 服务端下发相对 `download_path`，拼同一 base（含绿联通道） | `AppUpdate.kt:29-30` |
| 凭据 | 与业务请求同一套 Bearer + 绿联 Cookie；401 刷新一次后按 Range 继续 | `UpdateDownloader.kt:151,158-179` |
| 第三方直连 | 无：取包与缓存都在服务端 | `AppUpdate.kt:11-13` |
| 明文传输 | 允许（自部署，无法按域名白名单收紧） | `AndroidManifest.xml:15`；`res/xml/network_security_config.xml:6-8` |

## 6. AI 对话渲染

结论：客户端**不推断卡片形态**——正文里语言标记为 `ui` 的围栏只是占位引用，画成什么卡只看服务端 `validated: true` + `view`。

### 6.1 流与状态

| 项 | 事实 | 依据 |
|:--|:--|:--|
| 连接与选路 | 专用 OkHttp 长连接 `POST /api/v1/chat/message`：`callTimeout=0`、`readTimeout=120s`、不跟随 302（防登录页 HTML 被当 SSE）；只在发消息前 `ensureResolved / ensureFresh` 一次 | `ChatRepository.kt:193-205` |
| 会话切换门 | 流式回复期间顶栏「会话 / 新建」置灰；切换、新建、删当前会话（含批量删含当前）一律拒绝并 toast，避免在途流的 `session` / `turn.start` 把 `session_id`、`turn_id` 写回旧会话 | `ChatScreen.kt:227-248,250-260,262-276,289-307,639-644,879-925` |
| SSE 事件 | 12 个：`session`、`turn.start`、`thinking.delta`、`thinking.end`、`answer.delta`、`answer.end`、`tool.start`、`tool.end`、`proposal`、`card`、`turn.end`、`error` | `ChatRepository.kt:19-20,111-116,313-343`；`ChatState.kt:100-169` |
| 分段 | `Seg` = `Think`（无壳竖线）/ `Text`（正文）/ `Tool`（灰底容器，可内嵌确认卡）；`tool.end` 按 `call_id` 归位 | `ChatState.kt:31-68,139-147` |
| 流式渲染 | 每个 delta 就地累加到 Compose 快照状态（`mutableStateOf` / `mutableStateListOf`），逐帧重组；不可换回可变容器 | `ChatState.kt:24-26,100-131` |
| SSE 字段解析 | 统一走 kotlinx `Json`（`ApiClient.json`）：缺失 / `null` / 非字符串都按缺省值处理，不抛异常 | `ChatState.kt:271-304`；`ChatStateTest.kt:256` |
| 光标与滚动 | 正文末尾追加 ` ▍`（primary），`done` 后消失；流式中 `scrollToItem`（瞬时），结束后 `animateScrollToItem` | `ChatScreen.kt:542-548,1774` |
| 中断与重生成 | 中断不删消息，标「已中断」+ 编辑重发；一条内容都没收到则表述为「连接失败」。重生成先删整轮再重发，带附件/引用的轮不给按钮 | `ChatScreen.kt:431-465,1110-1129`；`ChatState.kt:198-217` |
| 历史回显 | `parts` → 与流式同形态；`result_card` 只灌 ref→卡映射 | `ChatState.kt:219-269` |

### 6.2 卡片形态

七个展示形态的唯一事实源是 `ChatUi.VIEWS`（`ChatUi.kt:18`），渲染分支无 `else`（`ChatCards.kt:141-149`）。

| 形态 | 主体 | 服务端下发的用途 | 依据 |
|:--|:--|:--|:--|
| `list` | 记录清单，一行一条（缩略图 + 文字 + 日期/角标） | 「有哪些」 | `ChatCards.kt:194` |
| `grid` | 3 列方图，点开看大图 | 「看照片」 | `ChatCards.kt:294` |
| `detail` | 单条全文 + 媒体 + 元信息 | 「看看这条」 | `ChatCards.kt:655` |
| `stats` / `kpi` | 数字 + 分布条形 / 一个大数字 + 一句说明 | 「多少 / 分布」/ 只问一个数字 | `ChatCards.kt:356-358,384-385` |
| `timeline` | 按日期倒序、按月分组的竖轴 | 「什么时候 / 这一年」 | `ChatCards.kt:497` |
| `map` | 带经纬度的地点标记（缺坐标不画） | 只有地点数据能用 | `ChatCards.kt:573-574` |

- 形态声明（含 `desc` / `params`）随消息以 `client_tools` 提交，共 7 条（`ChatRepository.kt:299-307`）。
- 收窄由服务端校验后原样透传：`limit` 只裁 `records`（list / grid / timeline），`only` 只裁 `stats` / `series` / `summaries`（`ChatCards.kt:99-113`）。
- 第 8 种是交互卡「确认卡」（`ChatCards.kt:689`）：8 个意图 → 中文动词（`ChatUi.kt:250-260`），预览用与结果卡同一套 `PreviewView`（`Text` / `Lines` / `Change` / `Media` / `Items`，`ChatUi.kt:121-136`），状态 `pending / executing / executed / cancelled / expired / failed`（`ChatCards.kt:804-813,835-843`），倒计时由本地绝对到期时刻驱动（`ChatState.kt:173-185`）；列表卡仅 `data.selectable` 给勾选引用，引用随下一条消息提交（`ChatScreen.kt:1308-1320`）。

### 6.3 围栏与正文

| 规则 | 事实 | 依据 |
|:--|:--|:--|
| 识别 | 语言标记恰为 `ui` 才是卡；单行与多行两种写法都认 | `UiFence.kt:37-38,62-105` |
| 取卡 | 用 `ref` 查本轮 `CardEvent`；未校验 / 形态不在七种 / 无 data → 一行人话 | `UiFence.kt:108-119` |
| 流式中 | 未闭合围栏按文本渲染（闭合后重拆出卡）；引用不到且本轮未结束 → `Pending`（不占位不报错） | `UiFence.kt:82-87,115` |
| 无围栏 | 不兜底画卡 | `ChatScreen.kt:1098-1099` |

正文 Markdown 子集：标题、有序/无序列表、行内代码、围栏代码块、链接（只留文字 + 下划线）、引用行、粗体；不引第三方库、不做语法高亮（`ChatScreen.kt:1701-1818`）。

### 6.4 附件回显

| 位置 | 表现 | 依据 |
|:--|:--|:--|
| 输入区 | 56dp 缩略图一行，可逐个移除；未发送前无上传占位 | `ChatScreen.kt:1526-1555` |
| 用户气泡与历史 | 「附 N 个媒体」+ 图标，纯附件无文字时显示 `[图片]`；`attachment` part → `mediaCount`，刷新后仍可见；`quote` part → 气泡内最多 3 个胶囊 + 「+N」 | `ChatScreen.kt:990-1048`；`ChatState.kt:261-267` |

## 7. 视觉令牌
### 7.1 颜色

画布：浅 `#F2F2F7` / 深 `#000000`；卡面：浅 `#FFFFFF` / 深 `#1C1C1E`；正文：`#1C1C1E` / `#F2F2F7`（`…/designsystem/theme/Palette.kt:32-36,64-68`）。

| 令牌 | Light | Dark |
|:--|:--|:--|
| `primary` | `#FF8FAB` | `#E86A8C` |
| `primaryContainer` | `#FFE3EA` | `#5C2136` |
| `secondary` / `tertiary` | `#5AA7D4` / `#E8A800` | `#6DB9E8` / `#FFD166` |
| `error` / `outline` / `outlineVariant` | `#FF3B30` / `#C7C7CC` / `#E5E5EA` | `#FF453A` / `#545458` / `#3A3A3C` |
| `surfaceContainerHigh` | `#E9E9EE` | `#2C2C2E` |
| `AppColors.primaryDeep` | `#F06292` | `#FFB4C2` |
| `AppColors.tint` / `tintWash` | `#007AFF` / `#1A007AFF` | `#0A84FF` / `#290A84FF` |
| `AppColors.rule` / `cardEdge` | `#2E3C3C43` / `#143C3C43` | `#A6545458` / `#1FFFFFFF` |
| `AppColors.authorName` / `semBlue/Green/Gold` | `#576B95` / `#5AA7D4` / `#34C759` / `#E8A800` | `#7A8FB8` / `#6DB9E8` / `#30D158` / `#FFD166` |

- 强调色 5 档（`Palette.kt:140-151`）：`pink #FF8FAB`、`rose #E86A8C`、`coral #FF9A8B`、`peach #FFB07C`、`apricot #FFC48C`；只替换 `primary` 家族（`AppTheme.kt:35-42`）。
- 深浅唯一判据是 `LocalAppDark`（`Palette.kt:160`），除 `MainActivity` 外禁止 `isSystemInDarkTheme()`（守卫见 §7.5）。

### 7.2 字号（`…/designsystem/theme/Type.kt:24-38`）

| 档位 | 字号 / 字重 / 行高 | 用途 |
|:--|:--|:--|
| `displaySmall` | 52 / ExtraBold / 56，字距 -0.03 | 头卡天数 |
| `headlineSmall` | 19 / Bold / 26 | 页标题 |
| `titleLarge` | 17 / SemiBold / 23 | 顶栏文字动作 |
| `titleMedium` | 15 / SemiBold / 21 | 弹层标题 |
| `bodyLarge` | 15 / Normal / 23 | 正文 |
| `bodyMedium` | 14 / Normal / 21 | 说明 |
| `bodySmall` / `labelLarge` | 13 / Normal / 19；13 / SemiBold / 18 | 次要说明 / 按钮与动作 |
| `labelMedium` | 11 / Medium / 15 | 微字 / 角标 |

独立样式：`appSectionTitleStyle()` 17/700（`Type.kt:44-48`）、`appTopBarTitleStyle()` 17/600（`Type.kt:52-56`）、`AppCardBodyStyle` 14/22（`Type.kt:59`）。

### 7.3 几何（间距与圆角）

| 常量 / 值 | 内容 | 依据 |
|:--|:--|:--|
| `AppSpacing` md / lg / xxl | 12 / 16 / 32 | `AppTheme.kt:13-17` |
| 顶栏标题行 / 导航条总高 | 44 / `heightIn 58` + 上下 padding 5+5 = 68 | `AppTopBar.kt:41`；`AppNavBar.kt:54-55` |
| 999 | 胶囊：chip / 徽标 / 进度条 / 输入胶囊 | `AppChip.kt:39`；`AppProgressBar.kt:24` |
| 31 / 20 / 18 | 导航条胶囊 / 输入卡 / 弹层顶部 | `AppNavBar.kt:52`；`ChatScreen.kt:1519-1522`；`AppSheet.kt:99` |
| 16 / 14 / 12 | AI 结果卡 `CARD_RADIUS`、`Shapes.medium` / 导航项选中底衬 / 按钮、toast、`AppCard` 默认、`AppFieldRow` | `ChatCards.kt:79`；`Type.kt:64`；`AppNavBar.kt:84`；`AppButton.kt:44`；`AppCard.kt:26` |
| 8 / 2 | 小元素与动作命中区 / 细线细条与小方格（2dp 视觉等同直角） | `AppSectionTitle.kt:51`；`StyleGuardTest.kt:113` |
| `Shapes` 五档 / 品牌图 | 6 / 10 / 16 / 24 / 28；`brandTileShape(size) = size * 0.3f` | `Type.kt:61-67`；`AppBrandTile.kt:13` |

### 7.4 chrome 与动效

| 项 | 值 | 依据 |
|:--|:--|:--|
| chrome 三档 alpha | `Bar 0.97` / `Floating 0.95` / `Overlay 1.0`；前两档带 0.5dp 边线 | `AppChrome.kt:29-48` |
| 会话抽屉 / Boot 呼吸条 | `tween(220)` / `infiniteRepeatable(tween(800), Reverse)`，alpha 0.18 ↔ 0.55 | `ChatScreen.kt:200-211`；`BootScreen.kt:96-102` |
| Toast / 长按 / 弹层进出 | 2500ms 自动消失 / 500ms / `fadeIn·fadeOut` + `slideInVertically·slideOutVertically`（默认时长） | `AppToast.kt:35-39`；`AppActionSheet.kt:128-143`；`AppSheet.kt:74-93` |

### 7.5 StyleGuardTest（16 条，任一失败 = 构建不通过；`app/core/designsystem/src/test/kotlin/…/StyleGuardTest.kt`）

| 规则 | 断言内容 | 行 |
|:--|:--|:--|
| 禁折射玻璃 | app 源码不得含 `import io.github.itshedimisawi.vitro` / `appGlass(` / `AppGlassHost` | 22 |
| 禁 M3 交互组件与 FAB | 不得 import `Button` / `Card` / `TextField` / `Scaffold` / `TopAppBar` / FAB / `ModalBottomSheet` / `Switch` / `Slider` / `Checkbox` / `RadioButton` / `SwipeToDismissBox` 等；不得出现 `FloatingActionButton` | 32,48 |
| 禁写死颜色与字号 | 不得出现 `Color(0x`；不得出现 `N.sp` 字面量 | 42,54 |
| 禁氛围渐变与系统深浅 | 不得出现 `LocalAppAtmosphere`；除 `MainActivity.kt` 外不得用 `isSystemInDarkTheme` | 60,83 |
| 玻璃只属 chrome | 除 `MapScreen.kt` / `DemoApp.kt` 外不得出现 `appGlass(`；`AppChrome.kt` 必须同时含 `0.97f` / `0.95f` / `1f` | 68,179 |
| chrome 吃触摸 | `AppNavBar.kt` 与 `AppTopBar.kt` 必须含 `consumeTouches()` | 193 |
| 圆角档位与品牌图 | `corner = N.dp` 仅 8 / 12 / 20 / 999；`RoundedCornerShape(N.dp)` 仅 2 / 8 / 12 / 20 / 999；Boot / Login / About 引用 `brand_icon` 后 3 行内不得写死圆角 | 97,109,124 |
| 卡片必须带阴影 | 不得出现 `shadow = false` | 143 |
| 滚动让开底栏 | 6 个页面文件含 `LazyColumn` / `verticalScroll` 时必须出现 `contentPadding` 或 `calculateBottomPadding` | 149 |
| 删除只走长按 | 不得出现 `AppSwipeItem` / `SwipeToDismiss` | 171 |

## 8. 坑与守卫

改到相关代码先跑「守卫」里的测试；这些测试是本节契约的机器可执行版本。

| 约束（不许改坏） | 守卫 |
|:--|:--|
| SSE 分帧规则：空行分界、`event:` / `data:` 解析、注释行与心跳忽略、多行 data 拼接 | `SseParserTest.kt:22,37,48` |
| 对话分段状态机：think 累加与折叠、tool.end 按 call_id 归位、提案嵌入最后一个工具段 | `ChatStateTest.kt:17,44,59` |
| 流式渲染与确认卡：增量逐帧、确认卡状态与倒计时、预览段序 | `ChatRenderTest.kt:21,40`、`ChatUiTest.kt:22` |
| 更新决策：冷启动恢复 / 稍后记忆 / 强更 / 缓存成品优先的顺序 | `UpdateDecisionTest.kt:33,38`、`AppUpdateTest.kt:34` |
| 频道选路与 URL 拼接：权重降序探活、相对路径补 base、绝对地址不改写 | `ChannelSelectionTest.kt:24,38`、`ChannelUrlTest.kt:13` |
| 地图投影与签名续期：经纬度投影、到期前重签与上下界夹取 | `MapProjectionTest.kt:16`、`MapSignatureRefreshTest.kt:12` |
| 围栏解析：只认 `ui` 围栏、多卡顺序、未闭合按文本 | `UiFenceTest.kt:48,60` |
| 农历换算：春节/中秋等锚点、闰月序位与月首递增 | `LunarCalendarTest.kt:19`、`LunarMonthPositionTest.kt:18` |
| 视觉宪法：禁折射玻璃、禁 M3 交互组件、禁写死色值/字号、圆角档位、chrome 吃触摸等 16 条 | `StyleGuardTest.kt:22` |
| 坐标/缩放文案与系统语言无关（`ui/` 内不许出现 `"%.Nf".format(`）；另存为只接受媒体 Content-Type 且不设下载总时长上限；会话分组不漏不重（最近两个月份按日期排序，其余月份并入「更早」）；勾选行带复选框语义与档位圆角；时间轴单页 ≤ 服务端硬上限 | `AppFixGuardTest.kt:45,58,69,113,119,134,145` |
| 分片上传：三片按序、409 冲突按服务端 offset 重同步、无进展即失败、空文件拒收 | `MediaChunkUploadTest.kt:20,37,63,77` |
| 单片瞬断重试：IOException 用同一 offset 重发（不重发其他片），持续失败最多 4 次尝试后抛错 | `MediaChunkUploadTest.kt:90,113` |
| 断点续传：206 追加、206 无 partial 回退重下、200 表示忽略 Range 重下、416 重置 partial | `UpdateDownloaderTest.kt:17,22,28,34` |
| 更新失败文案：断流/超时/DNS 分类正确、全部分支无英文 | `UpdateErrorMessageTest.kt:27,34,44,51,59` |
| 界面布局、手势、键盘弹出等视觉/交互行为 | **无自动测试**：真机或模拟器逐屏目检（截图 + 控制台/日志） |
| 当前流式轮 id 不得用文件级单例承载，切会话/新建会话必须清空 | `AppFixGuardTest.kt:205` |
| `RenderMessage.turnId` 必须走快照状态（SSE 线程写、主线程读） | `ChatStateTest.kt:248` |
| 上传复用单例 OkHttpClient，显式 connect 15s / read 30s / write 60s | `AppFixGuardTest.kt:176` |
| `discard` 请求体是 `{"tokens":[…]}` 且单次 ≤100（与服务端契约一致） | `AppFixGuardTest.kt:186` |
| SSE 字段解析统一走 kotlinx `Json`，缺失/null/非字符串的缺省语义不变 | `ChatStateTest.kt:256` |
| 空响应只允许 `Unit`，其余返回类型给可读错误而不是 `ClassCastException` | `AppFixGuardTest.kt:157` |
| 401 且刷新失败必须置 `sessionExpired` 事件，主界面自动回登录页 | `AppFixGuardTest.kt:166` |
| 地图配置首拉失败 5–10s 重试，已加载后按 exp 换签（≥60s） | `AppFixGuardTest.kt:194` |
| 流式期间禁止切换 / 新建 / 删当前会话（旧 SSE 的 session / turn.start 会写回旧会话） | **无自动测试**：模拟器手验——生成中点顶栏、抽屉切换、新建、删当前会话，均应被拒并 toast「正在生成回复，先停止或稍候」 |
| 聊天内地图卡与地图页同一换签口径：首拉失败短退避、已加载按 exp 提前 2 分钟续签 | `ChatCards.kt:594-602`（延迟逻辑由 `AppFixGuardTest.kt:194` 覆盖） |
| `PickedMedia` 字段只读，状态整体 `copy()` 替换 | `AppFixGuardTest.kt:217` |
| 项目主页地址全文件只出现一次（单一常量） | `AppFixGuardTest.kt:226` |
| 壳内返回策略：根页（主壳 / AI 对话页）不拦截系统返回，二级页回上级，设置页离开时提交模式 | `ShellBackTest.kt:15,21` |
| AI 对话页的返回层级：「选择对话」管理页 → 会话抽屉 → 交给系统退出应用；抽屉/管理页打开时按返回不得退出应用 | **无自动测试**：模拟器手验——AI 模式下开抽屉按返回应收抽屉、开「管理对话」按返回应回抽屉、两层都收起后再按返回才退到桌面 |
