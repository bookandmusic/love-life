package api

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"

	"github.com/bookandmusic/love-life/server/internal/appupdate"
	"github.com/bookandmusic/love-life/server/internal/auth"
	"github.com/bookandmusic/love-life/server/internal/chat"
	"github.com/bookandmusic/love-life/server/internal/config"
	"github.com/bookandmusic/love-life/server/internal/crypto"
	"github.com/bookandmusic/love-life/server/internal/logbuf"
	"github.com/bookandmusic/love-life/server/internal/mcp"
	"github.com/bookandmusic/love-life/server/internal/media"
	"github.com/bookandmusic/love-life/server/internal/notify"
	"github.com/bookandmusic/love-life/server/internal/store"
	"github.com/bookandmusic/love-life/server/internal/tools"
)

func NewRouter(cfg config.Config, db *store.DB, tm *auth.TokenManager, encSeed []byte, mp *media.Pipeline, dp *notify.Dispatcher, webuiDist fs.FS, searchIdx tools.SearchIndex, logs *logbuf.Buffer) http.Handler {
	authSvc := auth.NewService(db, tm, cfg.TrustedProxies...)

	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("POST /api/v1/auth/login", authSvc.Login)
	mux.HandleFunc("POST /api/v1/auth/refresh", authSvc.Refresh)
	mux.HandleFunc("POST /api/v1/auth/logout", authSvc.Logout)
	mux.Handle("GET /api/v1/auth/me", authSvc.RequireAuth(http.HandlerFunc(authSvc.Me)))

	// 后台成员管理（仅 admin）
	adminAPI := &adminAPI{db: db, mp: mp}
	adminMux := http.NewServeMux()
	adminMux.HandleFunc("GET /api/v1/admin/users", adminAPI.listUsers)
	adminMux.HandleFunc("POST /api/v1/admin/users/couple", adminAPI.createCouple)
	adminMux.HandleFunc("PUT /api/v1/admin/users/{id}", adminAPI.updateUser)
	adminMux.HandleFunc("POST /api/v1/admin/users/{id}/reset-password", adminAPI.resetPassword)
	// 头像/生日（独立通道）：admin 上传头像、设置生日
	adminMux.HandleFunc("POST /api/v1/admin/users/{id}/avatar", adminAPI.uploadAvatar)
	adminMux.HandleFunc("PUT /api/v1/admin/users/{id}/birthday", adminAPI.updateBirthday)
	hooksAPI := &webhooksAPI{db: db, dp: dp}
	adminMux.HandleFunc("GET /api/v1/admin/webhooks", hooksAPI.list)
	adminMux.HandleFunc("POST /api/v1/admin/webhooks", hooksAPI.create)
	adminMux.HandleFunc("PUT /api/v1/admin/webhooks/{id}", hooksAPI.update)
	adminMux.HandleFunc("DELETE /api/v1/admin/webhooks/{id}", hooksAPI.remove)
	adminMux.HandleFunc("POST /api/v1/admin/webhooks/{id}/test", hooksAPI.test)
	mapAPI := &mapConfigAPI{db: db, key: crypto.KeyFrom(encSeed), sigSecret: tm.Secret()}
	adminMux.HandleFunc("GET /api/v1/admin/map-config", mapAPI.list)
	adminMux.HandleFunc("POST /api/v1/admin/map-config", mapAPI.create)
	adminMux.HandleFunc("POST /api/v1/admin/map-config/probe", mapAPI.probe)
	adminMux.HandleFunc("DELETE /api/v1/admin/map-config/{id}", mapAPI.remove)
	adminMux.HandleFunc("PUT /api/v1/admin/map-config/{id}/enable", mapAPI.enable)
	providersAPI := &providersAPI{db: db, key: crypto.KeyFrom(encSeed)}
	adminMux.HandleFunc("GET /api/v1/admin/model-providers", providersAPI.list)
	adminMux.HandleFunc("POST /api/v1/admin/model-providers", providersAPI.create)
	adminMux.HandleFunc("POST /api/v1/admin/model-providers/fetch-models", providersAPI.fetchModels)
	adminMux.HandleFunc("PUT /api/v1/admin/model-providers/{id}/key", providersAPI.updateKey)
	adminMux.HandleFunc("PUT /api/v1/admin/model-providers/{id}", providersAPI.update)
	adminMux.HandleFunc("DELETE /api/v1/admin/model-providers/{id}", providersAPI.remove)
	// 模型条目与链序（kind 区分 chat / embedding，两条链共享 CRUD）
	adminMux.HandleFunc("GET /api/v1/admin/models", providersAPI.modelsList)
	adminMux.HandleFunc("POST /api/v1/admin/models", providersAPI.modelsCreate)
	adminMux.HandleFunc("PUT /api/v1/admin/models/order", providersAPI.modelsOrder)
	adminMux.HandleFunc("PUT /api/v1/admin/models/{id}", providersAPI.modelsUpdate)
	adminMux.HandleFunc("DELETE /api/v1/admin/models/{id}", providersAPI.modelsRemove)
	stylesAPI := &stylesAPI{db: db}
	adminMux.HandleFunc("GET /api/v1/admin/styles", stylesAPI.list)
	adminMux.HandleFunc("POST /api/v1/admin/styles", stylesAPI.create)
	adminMux.HandleFunc("PUT /api/v1/admin/styles/default", stylesAPI.setDefault)
	adminMux.HandleFunc("PUT /api/v1/admin/styles/{id}", stylesAPI.update)
	adminMux.HandleFunc("DELETE /api/v1/admin/styles/{id}", stylesAPI.remove)
	adminMux.HandleFunc("GET /api/v1/admin/system-prompt", stylesAPI.getPrompt)
	adminMux.HandleFunc("PUT /api/v1/admin/system-prompt", stylesAPI.setPrompt)
	// 数据安全设置：PUT 为部分更新，按字段合并
	securityAPI := &securityAPI{db: db}
	adminMux.HandleFunc("GET /api/v1/admin/security", securityAPI.get)
	adminMux.HandleFunc("PUT /api/v1/admin/security", securityAPI.put)
	// 回收站管理：列表 / 单条恢复 / 批量恢复 / 回收站操作日志
	recycleAPI := &recycleAPI{db: db}
	adminMux.HandleFunc("GET /api/v1/admin/recycle", recycleAPI.list)
	adminMux.HandleFunc("POST /api/v1/admin/recycle/restore", recycleAPI.restoreMany)
	adminMux.HandleFunc("GET /api/v1/admin/recycle/restore-log", recycleAPI.restoreLog)
	adminMux.HandleFunc("POST /api/v1/admin/recycle/{id}/restore", recycleAPI.restoreOne)
	// MCP 工具注册表与启停：清单 + 单项启停
	mcpToolsAPI := &mcpToolsAPI{db: db}
	adminMux.HandleFunc("GET /api/v1/admin/mcp-tools", mcpToolsAPI.list)
	adminMux.HandleFunc("PATCH /api/v1/admin/mcp-tools/{name}", mcpToolsAPI.patch)
	// 个人访问令牌：列表 / 签发（明文仅一次）/ 吊销
	patAPI := &patAPI{db: db}
	adminMux.HandleFunc("GET /api/v1/admin/tokens", patAPI.list)
	adminMux.HandleFunc("POST /api/v1/admin/tokens", patAPI.create)
	adminMux.HandleFunc("POST /api/v1/admin/tokens/{id}/revoke", patAPI.revoke)
	// 仪表盘：统计 / 近期活动 / AI 用量时序（数据源）/ 异常告警
	dashAPI := &dashboardAPI{db: db}
	adminMux.HandleFunc("GET /api/v1/admin/dashboard/stats", dashAPI.stats)
	adminMux.HandleFunc("GET /api/v1/admin/dashboard/activity", dashAPI.activity)
	adminMux.HandleFunc("GET /api/v1/admin/dashboard/ai-usage", dashAPI.aiUsage)
	adminMux.HandleFunc("GET /api/v1/admin/dashboard/alerts", dashAPI.alerts)
	// 审计日志查询
	auditAPI := &auditAPI{db: db}
	adminMux.HandleFunc("GET /api/v1/admin/audit", auditAPI.list)
	// 绿联通道：APP 取通道信息（登录态）+ 后台配置（admin）
	// 备用服务地址（服务端不代理，只存取与探活；APP 按权重依次探活）
	// 先建是因为统一选路接口要用它枚举通道
	backupAPI := &backupAddrAPI{db: db}
	uglinkAPI := newUglinkAPI(db, encSeed)
	uglinkAPI.backup = backupAPI
	adminMux.HandleFunc("GET /api/v1/admin/uglink/config", uglinkAPI.getConfig)
	adminMux.HandleFunc("PUT /api/v1/admin/uglink/config", uglinkAPI.putConfig)
	adminMux.HandleFunc("DELETE /api/v1/admin/uglink/config", uglinkAPI.deleteConfig)
	adminMux.HandleFunc("POST /api/v1/admin/uglink/test", uglinkAPI.test)

	// 应用内更新中转：服务端从 GitHub 取回并缓存，APP 只连本服务
	appUpdateSvc := appupdate.New(db, crypto.KeyFrom(encSeed), cfg.DataDir)
	appUpdateAPI := &appupdateAPI{svc: appUpdateSvc}
	adminMux.HandleFunc("GET /api/v1/admin/appupdate/config", appUpdateAPI.getConfig)
	adminMux.HandleFunc("PUT /api/v1/admin/appupdate/config", appUpdateAPI.putConfig)
	adminMux.HandleFunc("POST /api/v1/admin/appupdate/check", appUpdateAPI.check)
	adminMux.HandleFunc("POST /api/v1/admin/appupdate/download", appUpdateAPI.startDownload)
	adminMux.HandleFunc("POST /api/v1/admin/appupdate/remove", appUpdateAPI.remove)

	adminMux.HandleFunc("GET /api/v1/admin/backup-addresses", backupAPI.listAdmin)
	adminMux.HandleFunc("POST /api/v1/admin/backup-addresses", backupAPI.create)
	adminMux.HandleFunc("PUT /api/v1/admin/backup-addresses/{id}", backupAPI.update)
	adminMux.HandleFunc("DELETE /api/v1/admin/backup-addresses/{id}", backupAPI.remove)
	adminMux.HandleFunc("POST /api/v1/admin/backup-addresses/test", backupAPI.test)

	// 服务日志（进程内环形缓冲，只读；见 logsAPI 注释）
	logsAPI := &logsAPI{buf: logs}
	adminMux.HandleFunc("GET /api/v1/admin/logs", logsAPI.list)
	mux.Handle("/api/v1/admin/", authSvc.RequireAuth(requireAdmin(adminMux)))

	// couple = 登录 + 情侣角色（requireCouple）：业务数据路由统一口径——
	// admin 只允许管理后台，不登录 APP、不操作业务数据（登录入口的角色门禁之外，路由层再拦一道）
	couple := func(next http.Handler) http.Handler { return authSvc.RequireAuth(requireCouple(next)) }

	// 输出风格列表（APP 设置页「AI 偏好」数据源；形状同 admin 列表）
	mux.Handle("GET /api/v1/styles", couple(http.HandlerFunc(stylesAPI.list)))

	// 绿联通道信息：未配置/失败都返回 available:false，APP 据此回落。
	// 保留给「只看绿联这一条」的场景；选路统一走下面的 /channels。

	// 统一选路：全部后端侧通道 + 权重，APP 按权重降序逐个探活取第一个通的。
	// 备用地址单独一条：形状与 /channels 里的备用项一致。
	mux.Handle("GET /api/v1/channels", couple(http.HandlerFunc(uglinkAPI.channels)))

	// 应用内更新（登录用户）：检查与下载都走当前 base，与业务请求同域
	appUpdateMux := http.NewServeMux()
	appUpdateMux.HandleFunc("GET /api/v1/app/update/latest", appUpdateAPI.latest)
	appUpdateMux.HandleFunc("GET /api/v1/app/update/download", appUpdateAPI.download)
	mux.Handle("/api/v1/app/update/", authSvc.RequireAuth(appUpdateMux))

	// 地图（瓦片走服务端代理，密钥不下发）
	// config 走 couple；tile 用 config 下发的短时签名（exp+sig）鉴权——img 无法带 header
	mapMux := http.NewServeMux()
	mapMux.HandleFunc("GET /api/v1/map/config", mapAPI.config)
	mux.Handle("/api/v1/map/config", couple(mapMux))
	mapMux.HandleFunc("GET /api/v1/map/tile", mapAPI.tile)
	mux.Handle("/api/v1/map/tile", mapMux)

	// 地图点位（登录用户；双来源同池、同名复用，软删进回收站体系——）
	pointsAPI := &mapPointsAPI{db: db, dp: dp}
	pointsMux := http.NewServeMux()
	pointsMux.HandleFunc("GET /api/v1/map/points", pointsAPI.list)
	pointsMux.HandleFunc("POST /api/v1/map/points", pointsAPI.create)
	pointsMux.HandleFunc("DELETE /api/v1/map/points/{id}", pointsAPI.remove)
	mux.Handle("/api/v1/map/points", couple(pointsMux))
	mux.Handle("/api/v1/map/points/", couple(pointsMux))

	// 地理编码（内置离线表，无第三方 POI 依赖）
	mux.Handle("GET /api/v1/geocode", couple(http.HandlerFunc(geocode)))

	// AI 对话（登录用户；未配置模型时登录仍可用）
	// AI 与 MCP 共用同一依赖集：工具实现只有一份，两通道行为一致（含语义检索端口）。
	deps := &tools.Deps{DB: db, MP: mp, DP: dp, Search: searchIdx}
	chatSvc := chat.NewService(db, encSeed, deps)
	chatMux := http.NewServeMux()
	chatMux.HandleFunc("GET /api/v1/chat/status", chatSvc.Status)
	chatMux.HandleFunc("GET /api/v1/chat/bootstrap", chatSvc.Bootstrap)
	chatMux.HandleFunc("POST /api/v1/chat/message", chatSvc.Message)
	// 提案裁决：approve 执行、cancel 取消。
	chatMux.HandleFunc("POST /api/v1/chat/proposals/{id}/resolve", chatSvc.ResolveProposal)
	// 显式中止本轮（整轮回滚）：撤回只由用户意图触发，不由连接状态触发。
	chatMux.HandleFunc("POST /api/v1/chat/turns/{turn_id}/abort", chatSvc.Abort)
	// 回收已中断残局（编辑重发前删旧轮；与 abort 同一套整轮回滚，但 turn 已结束、不登记中止意图）
	chatMux.HandleFunc("DELETE /api/v1/chat/turns/{turn_id}", chatSvc.DeleteTurn)
	chatMux.HandleFunc("GET /api/v1/chat/sessions", chatSvc.ListSessions)
	chatMux.HandleFunc("DELETE /api/v1/chat/sessions/{id}", chatSvc.DeleteSession)
	chatMux.HandleFunc("POST /api/v1/chat/sessions/{id}/pin", chatSvc.ToggleSessionPin)
	chatMux.HandleFunc("GET /api/v1/chat/history", chatSvc.History)
	mux.Handle("/api/v1/chat/", couple(chatMux))

	// 外部 MCP 通道：PAT 认证（非 JWT），Streamable HTTP 唯一传输。
	// 与内部通道共用工具注册表；AppOnly 删除类工具经 allowed() 过滤，不对任何令牌开放。
	mcpSvc := mcp.NewService(db, deps)
	mux.HandleFunc("POST /api/v1/mcp", mcpSvc.Handle)

	// 空间信息与个人资料：GET /space 对登录用户开放（admin 后台「空间管理」页读，空间配置仅 admin 可改）；
	// PATCH /me 是业务操作（couple）
	profileAPI := &profileAPI{db: db, mp: mp}
	mux.Handle("GET /api/v1/space", authSvc.RequireAuth(http.HandlerFunc(profileAPI.getSpace)))
	mux.Handle("PUT /api/v1/space", authSvc.RequireAuth(requireAdmin(http.HandlerFunc(profileAPI.updateSpace))))
	mux.Handle("PATCH /api/v1/me", couple(http.HandlerFunc(profileAPI.updateMe)))

	// 动态与媒体（登录用户）
	postsAPI := &postsAPI{db: db, mp: mp, dp: dp}
	interAPI := &interactionsAPI{db: db, dp: dp}
	// reurl 批量重签：客户端可见媒体签名 URL 临期刷新（主 mux 直挂，避免与 {id} 通配冲突）
	mux.Handle("POST /api/v1/media/reurl", couple(http.HandlerFunc(postsAPI.reurlMedia)))
	mux.Handle("POST /api/v1/media/upload", couple(http.HandlerFunc(postsAPI.uploadMedia)))
	// 主动撤销：放弃 / 取消 / 把关作废时清临时文件（幂等，归属校验）
	mux.Handle("POST /api/v1/media/discard", couple(http.HandlerFunc(postsAPI.discardMedia)))
	postsMux := http.NewServeMux()
	postsMux.HandleFunc("POST /api/v1/posts", postsAPI.createPost)
	postsMux.HandleFunc("GET /api/v1/posts", postsAPI.listPosts)
	postsMux.HandleFunc("GET /api/v1/posts/{id}", postsAPI.getPost)
	postsMux.HandleFunc("DELETE /api/v1/posts/{id}", postsAPI.deletePost)
	postsMux.HandleFunc("DELETE /api/v1/media/{id}", postsAPI.deleteMedia)
	postsMux.HandleFunc("POST /api/v1/posts/{id}/like", interAPI.toggleLike)
	postsMux.HandleFunc("POST /api/v1/posts/{id}/comments", interAPI.createComment)
	postsMux.HandleFunc("GET /api/v1/posts/{id}/comments", interAPI.listComments)
	postsMux.HandleFunc("DELETE /api/v1/comments/{id}", interAPI.deleteComment)
	postsMux.HandleFunc("GET /api/v1/notifications", interAPI.listNotifications)
	postsMux.HandleFunc("GET /api/v1/notifications/unread-count", interAPI.unreadCount)
	postsMux.HandleFunc("DELETE /api/v1/notifications/{id}", interAPI.consumeNotification)
	postsMux.HandleFunc("DELETE /api/v1/notifications", interAPI.clearNotifications)
	mux.Handle("/api/v1/posts", couple(postsMux))
	mux.Handle("/api/v1/posts/", couple(postsMux))
	// DELETE /api/v1/media/{id} 挂在 postsMux 上：必须把 /api/v1/media/ 子树也路由过去，
	// 否则根 mux 只有 upload/reurl/discard 三条精确 POST，DELETE 直接 404
	mux.Handle("/api/v1/media/", couple(postsMux))
	mux.Handle("/api/v1/comments/", couple(postsMux))
	mux.Handle("/api/v1/notifications", couple(postsMux))
	mux.Handle("/api/v1/notifications/", couple(postsMux))

	// 纪念日与相册（登录用户）
	annivAPI := &anniversariesAPI{db: db}
	annivMux := http.NewServeMux()
	annivMux.HandleFunc("POST /api/v1/anniversaries", annivAPI.create)
	annivMux.HandleFunc("GET /api/v1/anniversaries", annivAPI.list)
	annivMux.HandleFunc("DELETE /api/v1/anniversaries/{id}", annivAPI.remove)
	albumsAPI := &albumsAPI{db: db, mp: mp, dp: dp}
	annivMux.HandleFunc("GET /api/v1/albums", albumsAPI.list)
	annivMux.HandleFunc("POST /api/v1/albums", albumsAPI.create)
	annivMux.HandleFunc("GET /api/v1/albums/{id}", albumsAPI.get)
	annivMux.HandleFunc("DELETE /api/v1/albums/{id}", albumsAPI.remove)
	annivMux.HandleFunc("POST /api/v1/albums/{id}/photos", albumsAPI.addPhotos)
	annivMux.HandleFunc("DELETE /api/v1/albums/{id}/photos", albumsAPI.removePhotos)
	mux.Handle("GET /api/v1/media/timeline", couple(http.HandlerFunc(albumsAPI.timeline)))
	mux.Handle("/api/v1/anniversaries", couple(annivMux))
	mux.Handle("/api/v1/anniversaries/", couple(annivMux))
	mux.Handle("/api/v1/albums", couple(annivMux))
	mux.Handle("/api/v1/albums/", couple(annivMux))
	// 媒体文件：签名即凭证（短期签名 URL），不走 Bearer
	mux.HandleFunc("GET /api/v1/media/file/{id}", postsAPI.serveMediaFile)
	// 头像：签名即凭证（独立命名空间），不走 Bearer
	mux.HandleFunc("GET /api/v1/avatar/{userID}", adminAPI.serveAvatar)

	// 后台管理页面静态资源（构建产物由 webui.Dist 内嵌）
	mux.Handle("/", spaHandler(webuiDist))

	return cors(limitBody(mux))
}

// maxJSONBody 非文件上传请求体的上限：所有 JSON 端点共用，含未认证的登录与刷新。
const maxJSONBody = 1 << 20

// limitBody 给请求体套大小上限。上传端点传的是文件，上限由各自 handler 设，不能套 JSON 上限。
// 按路径而不是 Content-Type 判断：Content-Type 由客户端给，伪造一个 multipart 就能绕过。
func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && !uploadPath(r.URL.Path) {
			r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
		}
		next.ServeHTTP(w, r)
	})
}

// uploadPath 请求体是文件的端点。
func uploadPath(p string) bool {
	if p == "/api/v1/media/upload" {
		return true
	}
	return strings.HasPrefix(p, "/api/v1/admin/users/") && strings.HasSuffix(p, "/avatar")
}

// cors API 跨域支持：APP/Tauri WebView 的 origin 与服务端不同域（如 http://tauri.localhost），
// 浏览器直连场景必需。认证走 Authorization 头（无 Cookie），Allow-Origin 放开无 CSRF 面。
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			// X-Upload-* 是分片上传协议头（常量定义在 posts.go）：自定义头必须在此放行，
			// 否则浏览器预检直接拒绝（curl 测试不受影响，极易漏——典型「测试过了线上不通」）。
			w.Header().Set("Access-Control-Allow-Headers",
				"Authorization, Content-Type, Range, "+hdrUploadID+", "+hdrUploadOffset+", "+hdrUploadFinal)
			// 另存到本机：APP 需读取服务端生成的文件名（Content-Disposition 不在 CORS 安全头里）
			w.Header().Set("Access-Control-Expose-Headers", "Content-Disposition, Content-Length, Accept-Ranges, Content-Range")
			w.Header().Set("Access-Control-Max-Age", "600")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// spaHandler 前端路由回退：非 /api 路径一律回 index.html
func spaHandler(dist fs.FS) http.Handler {
	fileServer := http.FileServerFS(dist)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(dist, path); err != nil {
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
