package api

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"

	"github.com/bookandmusic/love-life/server/internal/auth"
	"github.com/bookandmusic/love-life/server/internal/chat"
	"github.com/bookandmusic/love-life/server/internal/config"
	"github.com/bookandmusic/love-life/server/internal/crypto"
	"github.com/bookandmusic/love-life/server/internal/media"
	"github.com/bookandmusic/love-life/server/internal/notify"
	"github.com/bookandmusic/love-life/server/internal/store"
	"github.com/bookandmusic/love-life/server/internal/tools"
)

func NewRouter(cfg config.Config, db *store.DB, tm *auth.TokenManager, mp *media.Pipeline, dp *notify.Dispatcher, webuiDist fs.FS) http.Handler {
	authSvc := auth.NewService(db, tm)

	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("POST /api/v1/auth/login", authSvc.Login)
	mux.HandleFunc("POST /api/v1/auth/refresh", authSvc.Refresh)
	mux.HandleFunc("POST /api/v1/auth/logout", authSvc.Logout)
	mux.Handle("GET /api/v1/auth/me", authSvc.RequireAuth(http.HandlerFunc(authSvc.Me)))

	// 后台成员管理（仅 admin）
	adminAPI := &adminAPI{db: db}
	adminMux := http.NewServeMux()
	adminMux.HandleFunc("GET /api/v1/admin/users", adminAPI.listUsers)
	adminMux.HandleFunc("POST /api/v1/admin/users/couple", adminAPI.createCouple)
	adminMux.HandleFunc("PUT /api/v1/admin/users/{id}", adminAPI.updateNickname)
	adminMux.HandleFunc("POST /api/v1/admin/users/{id}/reset-password", adminAPI.resetPassword)
	hooksAPI := &webhooksAPI{db: db, dp: dp}
	adminMux.HandleFunc("GET /api/v1/admin/webhooks", hooksAPI.list)
	adminMux.HandleFunc("POST /api/v1/admin/webhooks", hooksAPI.create)
	adminMux.HandleFunc("PUT /api/v1/admin/webhooks/{id}", hooksAPI.update)
	adminMux.HandleFunc("DELETE /api/v1/admin/webhooks/{id}", hooksAPI.remove)
	adminMux.HandleFunc("POST /api/v1/admin/webhooks/{id}/test", hooksAPI.test)
	mapAPI := &mapConfigAPI{db: db, key: crypto.KeyFrom(tm.Secret())}
	adminMux.HandleFunc("GET /api/v1/admin/map-config", mapAPI.list)
	adminMux.HandleFunc("POST /api/v1/admin/map-config", mapAPI.create)
	adminMux.HandleFunc("DELETE /api/v1/admin/map-config/{id}", mapAPI.remove)
	adminMux.HandleFunc("PUT /api/v1/admin/map-config/{id}/enable", mapAPI.enable)
	providersAPI := &providersAPI{db: db, key: crypto.KeyFrom(tm.Secret())}
	adminMux.HandleFunc("GET /api/v1/admin/model-providers", providersAPI.list)
	adminMux.HandleFunc("POST /api/v1/admin/model-providers", providersAPI.create)
	adminMux.HandleFunc("POST /api/v1/admin/model-providers/fetch-models", providersAPI.fetchModels)
	adminMux.HandleFunc("PUT /api/v1/admin/model-providers/order", providersAPI.order)
	adminMux.HandleFunc("PUT /api/v1/admin/model-providers/{id}/key", providersAPI.updateKey)
	adminMux.HandleFunc("PUT /api/v1/admin/model-providers/{id}", providersAPI.update)
	adminMux.HandleFunc("DELETE /api/v1/admin/model-providers/{id}", providersAPI.remove)
	stylesAPI := &stylesAPI{db: db}
	adminMux.HandleFunc("GET /api/v1/admin/styles", stylesAPI.list)
	adminMux.HandleFunc("POST /api/v1/admin/styles", stylesAPI.create)
	adminMux.HandleFunc("PUT /api/v1/admin/styles/default", stylesAPI.setDefault)
	adminMux.HandleFunc("PUT /api/v1/admin/styles/{id}", stylesAPI.update)
	adminMux.HandleFunc("DELETE /api/v1/admin/styles/{id}", stylesAPI.remove)
	adminMux.HandleFunc("GET /api/v1/admin/system-prompt", stylesAPI.getPrompt)
	adminMux.HandleFunc("PUT /api/v1/admin/system-prompt", stylesAPI.setPrompt)
	mux.Handle("/api/v1/admin/", authSvc.RequireAuth(requireAdmin(adminMux)))

	// 输出风格列表（登录用户，形状同 admin 列表；AI 偏好属个人体验项，需求 §7.1）
	mux.Handle("GET /api/v1/styles", authSvc.RequireAuth(http.HandlerFunc(stylesAPI.list)))

	// 地图（登录用户；瓦片走服务端代理，密钥不下发）
	// tile 接口允许 token query 认证（img 标签无法带 header），config 走 RequireAuth
	mapMux := http.NewServeMux()
	mapMux.HandleFunc("GET /api/v1/map/config", mapAPI.config)
	mux.Handle("/api/v1/map/config", authSvc.RequireAuth(mapMux))
	mapMux.HandleFunc("GET /api/v1/map/tile", mapAPI.tile)
	mux.Handle("/api/v1/map/tile", authSvc.AllowTokenQuery(mapMux))

	// 地图点位（登录用户；双来源同池、同名复用，软删进回收站体系——DESIGN §4.7）
	pointsAPI := &mapPointsAPI{db: db}
	pointsMux := http.NewServeMux()
	pointsMux.HandleFunc("GET /api/v1/map/points", pointsAPI.list)
	pointsMux.HandleFunc("POST /api/v1/map/points", pointsAPI.create)
	pointsMux.HandleFunc("DELETE /api/v1/map/points/{id}", pointsAPI.remove)
	pointsMux.HandleFunc("GET /api/v1/map/points/cities", pointsAPI.cities)
	mux.Handle("/api/v1/map/points", authSvc.RequireAuth(pointsMux))
	mux.Handle("/api/v1/map/points/", authSvc.RequireAuth(pointsMux))

	// 地理编码（内置离线表，无第三方 POI 依赖）
	mux.Handle("GET /api/v1/geocode", authSvc.RequireAuth(http.HandlerFunc(geocode)))

	// AI 对话（登录用户；未配置模型时登录仍可用）
	chatSvc := chat.NewService(db, tm.Secret(), &tools.Deps{DB: db, MP: mp, DP: dp})
	chatMux := http.NewServeMux()
	chatMux.HandleFunc("GET /api/v1/chat/status", chatSvc.Status)
	chatMux.HandleFunc("POST /api/v1/chat/message", chatSvc.Message)
	chatMux.HandleFunc("POST /api/v1/chat/confirm", chatSvc.Confirm)
	chatMux.HandleFunc("GET /api/v1/chat/history", chatSvc.History)
	mux.Handle("/api/v1/chat/", authSvc.RequireAuth(chatMux))

	// 空间信息与个人资料（登录用户；配置信息可修改，需求 §7.1）
	profileAPI := &profileAPI{db: db}
	profileMux := http.NewServeMux()
	profileMux.HandleFunc("GET /api/v1/space", profileAPI.getSpace)
	profileMux.HandleFunc("PUT /api/v1/space", profileAPI.updateSpace)
	profileMux.HandleFunc("PATCH /api/v1/me", profileAPI.updateMe)
	mux.Handle("/api/v1/space", authSvc.RequireAuth(profileMux))
	mux.Handle("/api/v1/space/", authSvc.RequireAuth(profileMux))
	mux.Handle("/api/v1/me", authSvc.RequireAuth(profileMux))

	// 动态与媒体（登录用户）
	postsAPI := &postsAPI{db: db, mp: mp, dp: dp}
	interAPI := &interactionsAPI{db: db, dp: dp}
	// 独立媒体接口已停用（需求 §4.5/166：媒体仅随动态；mock 契约 410）
	mux.HandleFunc("POST /api/v1/media", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusGone)
		_, _ = w.Write([]byte(`{"error":"INDEPENDENT_MEDIA_DISABLED","message":"媒体仅随动态发布，不再支持独立上传"}`))
	})
	mux.Handle("DELETE /api/v1/media/{id}", authSvc.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusGone)
		_, _ = w.Write([]byte(`{"error":"INDEPENDENT_MEDIA_DISABLED","message":"媒体仅随动态发布，不再支持独立删除（删除媒体=删除整条动态）"}`))
	})))
	// reurl 批量重签（D1）：客户端可见媒体签名 URL 临期刷新（主 mux 直挂，避免与 {id} 通配冲突）
	mux.Handle("POST /api/v1/media/reurl", authSvc.RequireAuth(http.HandlerFunc(postsAPI.reurlMedia)))
	mux.Handle("POST /api/v1/media/upload", authSvc.RequireAuth(http.HandlerFunc(postsAPI.uploadMedia)))
	postsMux := http.NewServeMux()
	postsMux.HandleFunc("POST /api/v1/posts", postsAPI.createPost)
	postsMux.HandleFunc("GET /api/v1/posts", postsAPI.listPosts)
	postsMux.HandleFunc("GET /api/v1/posts/{id}", postsAPI.getPost)
	postsMux.HandleFunc("DELETE /api/v1/posts/{id}", postsAPI.deletePost)
	postsMux.HandleFunc("POST /api/v1/posts/{id}/like", interAPI.toggleLike)
	postsMux.HandleFunc("POST /api/v1/posts/{id}/favorite", interAPI.toggleFavorite)
	postsMux.HandleFunc("POST /api/v1/posts/{id}/comments", interAPI.createComment)
	postsMux.HandleFunc("GET /api/v1/posts/{id}/comments", interAPI.listComments)
	postsMux.HandleFunc("DELETE /api/v1/comments/{id}", interAPI.deleteComment)
	postsMux.HandleFunc("GET /api/v1/notifications", interAPI.listNotifications)
	postsMux.HandleFunc("GET /api/v1/notifications/unread-count", interAPI.unreadCount)
	postsMux.HandleFunc("POST /api/v1/notifications/{id}/read", interAPI.markRead)
	postsMux.HandleFunc("POST /api/v1/notifications/read-all", interAPI.markAllRead)
	mux.Handle("/api/v1/posts", authSvc.RequireAuth(postsMux))
	mux.Handle("/api/v1/posts/", authSvc.RequireAuth(postsMux))
	mux.Handle("/api/v1/comments/", authSvc.RequireAuth(postsMux))
	mux.Handle("/api/v1/notifications", authSvc.RequireAuth(postsMux))
	mux.Handle("/api/v1/notifications/", authSvc.RequireAuth(postsMux))

	// 纪念日与相册（登录用户）
	annivAPI := &anniversariesAPI{db: db}
	annivMux := http.NewServeMux()
	annivMux.HandleFunc("POST /api/v1/anniversaries", annivAPI.create)
	annivMux.HandleFunc("GET /api/v1/anniversaries", annivAPI.list)
	annivMux.HandleFunc("PATCH /api/v1/anniversaries/{id}/reminder", annivAPI.updateReminder)
	annivMux.HandleFunc("DELETE /api/v1/anniversaries/{id}", annivAPI.remove)
	albumsAPI := &albumsAPI{db: db, mp: mp, dp: dp}
	annivMux.HandleFunc("GET /api/v1/albums", albumsAPI.list)
	annivMux.HandleFunc("POST /api/v1/albums", albumsAPI.create)
	annivMux.HandleFunc("GET /api/v1/albums/{id}", albumsAPI.get)
	annivMux.HandleFunc("DELETE /api/v1/albums/{id}", albumsAPI.remove)
	annivMux.HandleFunc("PATCH /api/v1/albums/{id}/hidden", albumsAPI.setHidden)
	annivMux.HandleFunc("POST /api/v1/albums/{id}/photos", albumsAPI.addPhotos)
	annivMux.HandleFunc("DELETE /api/v1/albums/{id}/photos", albumsAPI.removePhotos)
	mux.Handle("GET /api/v1/media/timeline", authSvc.RequireAuth(http.HandlerFunc(albumsAPI.timeline)))
	mux.Handle("/api/v1/anniversaries", authSvc.RequireAuth(annivMux))
	mux.Handle("/api/v1/anniversaries/", authSvc.RequireAuth(annivMux))
	mux.Handle("/api/v1/albums", authSvc.RequireAuth(annivMux))
	mux.Handle("/api/v1/albums/", authSvc.RequireAuth(annivMux))
	// 媒体文件：签名即凭证（DESIGN 4.5 短期签名 URL），不走 Bearer
	mux.HandleFunc("GET /api/v1/media/file/{id}", postsAPI.serveMediaFile)

	// 后台管理页面静态资源（构建产物由 webui.Dist 内嵌）
	mux.Handle("/", spaHandler(webuiDist))

	return cors(mux)
}

// cors API 跨域支持：APP/Tauri WebView 的 origin 与服务端不同域（如 http://tauri.localhost），
// 浏览器直连场景必需。认证走 Authorization 头（无 Cookie），Allow-Origin 放开无 CSRF 面。
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
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
