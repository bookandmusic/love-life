package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/bookandmusic/love-life/server/internal/auth"
	"github.com/bookandmusic/love-life/server/internal/media"
	"github.com/bookandmusic/love-life/server/internal/store"
)

type profileAPI struct {
	db *store.DB
	mp *media.Pipeline
}

// spaceSummary GET /api/v1/space：空间信息 + 双方昵称 + 在一起天数 + 下一个纪念日倒计时（首页关系栏一次拉全）。
func (a *profileAPI) getSpace(w http.ResponseWriter, r *http.Request) {
	space, err := a.db.GetSpace(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	users, err := a.db.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	type member struct {
		ID       int64  `json:"id"`
		Nickname string `json:"nickname"`
		Role     string `json:"role"`
		Avatar   string `json:"avatar"` // 头像签名 URL；无头像时空串（前端 fallback 首字母）
	}
	members := []member{}
	for _, u := range users {
		if u.Role != "admin" {
			name := u.Nickname
			if name == "" {
				name = u.Username
			}
			avatar := ""
			if u.AvatarPath.Valid && u.AvatarPath.String != "" {
				avatar = a.mp.SignedAvatarURL(u.ID)
			}
			members = append(members, member{ID: u.ID, Nickname: name, Role: u.Role, Avatar: avatar})
		}
	}

	var daysTogether *int
	loveStart := space.LoveStartDate.String // 未设时为 ""（如实回显库里的值）
	// 天数口径与 AI 提示词事实、统计卡同源；未设 / 不可解析 / 开始日在未来时不给数字，不编造 0 天
	now := time.Now()
	if days, ok := store.DaysTogether(loveStart, now); ok {
		daysTogether = &days
	}

	var nextAnniversary struct {
		Name      string `json:"name,omitempty"`
		Date      string `json:"date,omitempty"`
		DaysUntil int    `json:"days_until"`
	}
	// 计数供 APP 首页空态判定：单看 next_anniversary 无法区分「没有纪念日」
	// 与「纪念日存在但日期全部无效」，会把有数据的空间误判成全新部署
	anniversaryCount := 0
	albumCount := 0
	if list, err := a.db.ListAlbums(r.Context()); err == nil {
		albumCount = len(list)
	}
	if list, err := a.db.ListAnniversaries(r.Context()); err == nil {
		anniversaryCount = len(list)
		// 挑选口径与 AI 提示词事实、统计卡同源（含「无法换算的条目不参与」）；
		// 同一 now：date 与 days_until 必须来自同一时点，否则跨零点会给出不自洽的一对
		if ann, ok := store.PickNextAnniversary(list, now); ok {
			nextAnniversary.Name = ann.Name
			// lunar 回显换算后的下一次公历（mock annivNextDate 契约），solar 原样
			nextAnniversary.Date = ann.NextSolarDate(now)
			nextAnniversary.DaysUntil = ann.DaysUntil(now)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"name": space.Name, "signature": space.Signature, "love_start_date": loveStart,
		"members": members, "days_together": daysTogether, "next_anniversary": nextAnniversary,
		"anniversary_count": anniversaryCount, "album_count": albumCount,
	})
}

// updateSpace PUT /api/v1/space（双方均可改，配置信息）。
func (a *profileAPI) updateSpace(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name          string `json:"name"`
		Signature     string `json:"signature"`
		LoveStartDate string `json:"love_start_date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	if req.LoveStartDate != "" {
		start, err := time.ParseInLocation("2006-01-02", req.LoveStartDate, time.Local)
		if err != nil {
			writeError(w, http.StatusBadRequest, "恋爱开始日期格式应为 YYYY-MM-DD")
			return
		}
		// 开始日不能在未来：那会算出「1 天 / 0 天」这类无意义天数（未设开始日的处理见
		// store.DaysTogether——未来日期属需求未定义的第三态，这里从写入侧消掉）。
		today := time.Now().In(time.Local)
		if start.After(time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.Local)) {
			writeError(w, http.StatusBadRequest, "恋爱开始日期不能晚于今天")
			return
		}
	}
	if err := a.db.UpdateSpace(r.Context(), &store.Space{
		Name: req.Name, Signature: req.Signature, LoveStartDate: nullStr(req.LoveStartDate),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// me PATCH /api/v1/me：仅 AI 偏好（输出风格）。昵称 / 生日等资料由后台维护
// （PRODUCT「设置」），APP 侧不发、服务端不收。
func (a *profileAPI) updateMe(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	var req struct {
		AIStyle *string `json:"ai_style"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	if req.AIStyle != nil {
		// ai_style 存风格名；空 = 跟随全局默认。合法性 = 风格库中存在。
		if name := strings.TrimSpace(*req.AIStyle); name != "" {
			styles, err := a.db.ListStyles(r.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, errInternal)
				return
			}
			found := false
			for _, s := range styles {
				if s.Name == name {
					found = true
					break
				}
			}
			if !found {
				writeError(w, http.StatusBadRequest, "输出风格不存在")
				return
			}
			req.AIStyle = &name
		}
	}
	if err := a.db.UpdateUserSelf(r.Context(), claims.UserID, req.AIStyle); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "用户不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
