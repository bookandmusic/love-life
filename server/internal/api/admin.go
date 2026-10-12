package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bookandmusic/love-life/server/internal/auth"
	"github.com/bookandmusic/love-life/server/internal/media"
	"github.com/bookandmusic/love-life/server/internal/store"
)

// adminAPI 后台成员管理（本空间信息与成员，含 girl/boy 密码重置）。
type adminAPI struct {
	db *store.DB
	mp *media.Pipeline
}

func (a *adminAPI) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := a.db.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	type item struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
		Role     string `json:"role"`
		Nickname string `json:"nickname"`
		Birthday string `json:"birthday"` // 仅后台展示（关系卡未列）
		Avatar   string `json:"avatar"`   // 头像签名 URL；无则空串
	}
	out := make([]item, 0, len(users))
	for _, u := range users {
		avatar := ""
		if u.AvatarPath.Valid && u.AvatarPath.String != "" {
			avatar = a.mp.SignedAvatarURL(u.ID)
		}
		out = append(out, item{
			ID: u.ID, Username: u.Username, Role: u.Role, Nickname: u.Nickname,
			Birthday: nullStrText(u.Birthday), Avatar: avatar,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// createCouple 情侣账户原子创建（；契约与校验顺序对齐 mock routes.mjs / mock-check C1-C4）：
// 必填 → 双方同名 → 格式 → 情侣已存在 → 用户名冲突，以上均 400（C4 明确情侣已存在不得返 409）。
func (a *adminAPI) createCouple(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Girl struct {
			Username string `json:"username"`
			Nickname string `json:"nickname"`
			Password string `json:"password"`
		} `json:"girl"`
		Boy struct {
			Username string `json:"username"`
			Nickname string `json:"nickname"`
			Password string `json:"password"`
		} `json:"boy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	if req.Girl.Username == "" || req.Girl.Password == "" || req.Boy.Username == "" || req.Boy.Password == "" {
		writeError(w, http.StatusBadRequest, "双方 username/password 均必填")
		return
	}
	if req.Girl.Username == req.Boy.Username {
		writeError(w, http.StatusBadRequest, "双方用户名不能相同")
		return
	}
	// 格式校验先于存在性判断：情侣已存在时格式错误的入参也应得到格式错误
	for _, p := range []struct{ username, password string }{
		{req.Girl.Username, req.Girl.Password}, {req.Boy.Username, req.Boy.Password},
	} {
		if n := len([]rune(p.username)); n < 2 || n > 32 {
			writeError(w, http.StatusBadRequest, "用户名长度 2-32 个字符")
			return
		}
		if len(p.password) < 6 {
			writeError(w, http.StatusBadRequest, "密码强度不足：至少 6 位")
			return
		}
	}
	users, err := a.db.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	for _, u := range users {
		if u.Role == "girl" || u.Role == "boy" {
			writeError(w, http.StatusBadRequest, "情侣账户已存在，仅可修改昵称或重置密码")
			return
		}
	}
	for _, u := range users {
		if u.Username == req.Girl.Username || u.Username == req.Boy.Username {
			writeError(w, http.StatusBadRequest, "用户名已存在")
			return
		}
	}
	girlHash, err := auth.HashPassword(req.Girl.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	boyHash, err := auth.HashPassword(req.Boy.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	girlID, boyID, err := a.db.CreateCouple(r.Context(),
		&store.User{Username: req.Girl.Username, PasswordHash: girlHash, Role: "girl", Nickname: req.Girl.Nickname},
		&store.User{Username: req.Boy.Username, PasswordHash: boyHash, Role: "boy", Nickname: req.Boy.Nickname})
	if err != nil {
		if errors.Is(err, store.ErrDuplicateUsername) {
			writeError(w, http.StatusBadRequest, "用户名已存在")
			return
		}
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"girl": map[string]any{"id": girlID, "username": req.Girl.Username, "role": "girl", "nickname": req.Girl.Nickname},
		"boy":  map[string]any{"id": boyID, "username": req.Boy.Username, "role": "boy", "nickname": req.Boy.Nickname},
	})
}

// updateUser PUT /api/v1/admin/users/{id}：更新昵称与生日（用户名/角色为登录身份与数据归属，不可修改）。
// 仅情侣角色可改——admin 账号只读展示、由部署环境管理（「后台管理页面实现」空间管理）。
// birthday 省略（nil）= 不改；给空串 = 清除。
func (a *adminAPI) updateUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的用户 ID")
		return
	}
	// 先取用户：既做存在性判断，也把「admin 不可改」在 handler 层挡住（store 层 SQL 另有同款条件）
	u, err := a.db.GetUserByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "用户不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	if u.Role == "admin" {
		writeError(w, http.StatusForbidden, "管理员账号只读，由部署环境管理")
		return
	}
	var req struct {
		Nickname string  `json:"nickname"`
		Birthday *string `json:"birthday"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	if strings.TrimSpace(req.Nickname) == "" {
		writeError(w, http.StatusBadRequest, "nickname 必填")
		return
	}
	// 全校验通过后才落库：若先写昵称再验生日，「生日格式错」返回 400 时昵称已被改掉
	bd, hasBD := "", false
	if req.Birthday != nil {
		bd, hasBD = strings.TrimSpace(*req.Birthday), true
		if bd != "" {
			if _, err := time.Parse("2006-01-02", bd); err != nil {
				writeError(w, http.StatusBadRequest, "生日格式应为 YYYY-MM-DD")
				return
			}
		}
	}
	if err := a.db.UpdateUserNickname(r.Context(), id, strings.TrimSpace(req.Nickname)); err != nil {
		a.writeUserUpdateErr(w, err)
		return
	}
	if hasBD {
		if err := a.db.UpdateUserBirthday(r.Context(), id, bd); err != nil {
			a.writeUserUpdateErr(w, err)
			return
		}
	}
	got, err := a.db.GetUserByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": got.ID, "username": got.Username, "role": got.Role,
		"nickname": got.Nickname, "birthday": got.Birthday.String,
	})
}

// writeUserUpdateErr 用户更新写库失败的统一回写。
// 走到 ErrNotFound 只有两种可能：目标行在本次请求期间被删（并发），或 store 的 role 条件
// 拦下了 admin 行（正常情况下 handler 已先返回 403，这里是纵深防御）。
// 两种都用同一条「只读 / 不可改」文案：对本接口的可达路径而言，提示「不存在」会把
// 管理员只读误导成数据缺失；真正的并发删除在自部署单空间场景下几乎不可能出现。
func (a *adminAPI) writeUserUpdateErr(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusForbidden, "管理员账号只读，由部署环境管理")
		return
	}
	writeError(w, http.StatusInternalServerError, errInternal)
}

func (a *adminAPI) resetPassword(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的用户 ID")
		return
	}
	// admin 账号只读：先挡角色，再校验与写库（store 层 SQL 另有同款条件）
	target, err := a.db.GetUserByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "用户不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	if target.Role == "admin" {
		writeError(w, http.StatusForbidden, "管理员账号只读，由部署环境管理")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	if len(req.Password) < 6 {
		writeError(w, http.StatusBadRequest, "密码至少 6 位")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	if err := a.db.UpdateUserPassword(r.Context(), id, hash); err != nil {
		a.writeUserUpdateErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// uploadAvatar POST /api/v1/admin/users/{id}/avatar：admin 上传头像（独立通道）。
// multipart/form-data 字段 avatar；仅接受照片格式。替换时删旧文件。
func (a *adminAPI) uploadAvatar(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的用户 ID")
		return
	}
	// 5 MiB 是**内存阈值**（超出部分溢写系统临时目录），不是文件大小上限；
	// 头像通道无大小限制，传多大都接受。
	if err := r.ParseMultipartForm(5 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "上传解析失败")
		return
	}
	file, header, err := r.FormFile("avatar")
	if err != nil {
		writeError(w, http.StatusBadRequest, "缺少 avatar 文件")
		return
	}
	defer file.Close()
	// 预检用户存在性 + 取旧头像路径（替换后清理）
	u, err := a.db.GetUserByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "用户不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	if u.Role == "admin" {
		writeError(w, http.StatusForbidden, "管理员账号只读，由部署环境管理")
		return
	}
	rel, err := a.mp.SaveAvatar(file, header.Filename)
	if err != nil {
		if errors.Is(err, media.ErrUnsupportedFormat) {
			writeError(w, http.StatusBadRequest, "头像仅支持 jpg/png/webp")
			return
		}
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	if err := a.db.UpdateUserAvatar(r.Context(), id, rel); err != nil {
		a.mp.Delete(rel) // DB 写失败 → 回滚已落盘的头像文件，避免孤儿
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "用户不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	if u.AvatarPath.Valid && u.AvatarPath.String != "" && u.AvatarPath.String != rel {
		a.mp.Delete(u.AvatarPath.String) // 替换头像 → 删旧文件
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
		"avatar": a.mp.SignedAvatarURL(id),
	})
}

// updateBirthday PUT /api/v1/admin/users/{id}/birthday：仅后台维护，不下发 APP。
func (a *adminAPI) updateBirthday(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的用户 ID")
		return
	}
	var req struct {
		Birthday string `json:"birthday"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	if req.Birthday != "" {
		if _, err := time.Parse("2006-01-02", req.Birthday); err != nil {
			writeError(w, http.StatusBadRequest, "生日格式应为 YYYY-MM-DD")
			return
		}
	}
	// 与主端点同口径：admin 行匹配不到时给「只读」而非「不存在」
	if err := a.db.UpdateUserBirthday(r.Context(), id, req.Birthday); err != nil {
		a.writeUserUpdateErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// serveAvatar GET /api/v1/avatar/{userID}：公开（签名即凭证， 同媒体文件通道）。
// 不走 Bearer：APP 内 <img> 无法附加 header；签名 HMAC 命名空间与媒体隔离（avatar:<id>）。
func (a *adminAPI) serveAvatar(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("userID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的用户 ID")
		return
	}
	if !a.mp.VerifyAvatarSig(id, r.URL.Query().Get("exp"), r.URL.Query().Get("sig")) {
		writeError(w, http.StatusUnauthorized, "签名无效或已过期")
		return
	}
	u, err := a.db.GetUserByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "用户不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	if !u.AvatarPath.Valid || u.AvatarPath.String == "" {
		writeError(w, http.StatusNotFound, "未设置头像")
		return
	}
	f, err := a.mp.Open(u.AvatarPath.String)
	if err != nil {
		writeError(w, http.StatusNotFound, "头像文件丢失")
		return
	}
	defer f.Close()
	// 探测 Content-Type（避免依赖扩展名）
	header := make([]byte, 512)
	n, _ := io.ReadFull(f, header)
	ct := http.DetectContentType(header[:n])
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=900")
	_, _ = io.Copy(w, f)
}

// requireAdmin 在认证之上叠加角色校验：后台路由仅 admin。
func requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := auth.UserFromContext(r.Context())
		if !ok || claims.Role != "admin" {
			writeError(w, http.StatusForbidden, "仅 admin 可访问")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
