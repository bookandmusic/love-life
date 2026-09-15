package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/bookandmusic/love-life/server/internal/auth"
	"github.com/bookandmusic/love-life/server/internal/store"
)

type profileAPI struct {
	db *store.DB
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
	}
	members := []member{}
	for _, u := range users {
		if u.Role != "admin" {
			name := u.Nickname
			if name == "" {
				name = u.Username
			}
			members = append(members, member{ID: u.ID, Nickname: name, Role: u.Role})
		}
	}

	var daysTogether *int
	var loveStart string
	if space.LoveStartDate.Valid {
		loveStart = space.LoveStartDate.String
		if start, err := time.ParseInLocation("2006-01-02", loveStart, time.Local); err == nil {
			d := int(time.Since(start).Hours()/24) + 1
			daysTogether = &d
		}
	}

	var nextAnniversary struct {
		Name      string `json:"name,omitempty"`
		Date      string `json:"date,omitempty"`
		DaysUntil int    `json:"days_until"`
	}
	// 计数供 APP 首页空态判定（S5）：单看 next_anniversary 无法区分「没有纪念日」
	// 与「纪念日存在但日期全部无效」，会把有数据的空间误判成全新部署
	anniversaryCount := 0
	albumCount := 0
	if list, err := a.db.ListAlbums(r.Context(), true); err == nil {
		albumCount = len(list)
	}
	if list, err := a.db.ListAnniversaries(r.Context()); err == nil {
		anniversaryCount = len(list)
		now := time.Now()
		best := -1
		for i, ann := range list {
			d := ann.DaysUntil(now)
			if d < 0 { // 无法换算（如 lunar 超表尾）的条目不参与选择，对齐 mock annivDaysUntil 的 days>=0 过滤
				continue
			}
			if best == -1 || d < list[best].DaysUntil(now) {
				best = i
			}
		}
		if best >= 0 {
			nextAnniversary.Name = list[best].Name
			// lunar 回显换算后的下一次公历（mock annivNextDate 契约），solar 原样
			nextAnniversary.Date = list[best].NextSolarDate(now)
			nextAnniversary.DaysUntil = list[best].DaysUntil(now)
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
		if _, err := time.Parse("2006-01-02", req.LoveStartDate); err != nil {
			writeError(w, http.StatusBadRequest, "恋爱开始日期格式应为 YYYY-MM-DD")
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

// me PATCH /api/v1/me：昵称/生日/输出风格/对话保留时长（个人资料与 AI 偏好，需求 §4.7）。
// 字段缺省 = 不修改（对齐 mock 契约：APP 设置页只发偏好字段时不得清空昵称）。
func (a *profileAPI) updateMe(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	var req struct {
		Nickname      *string `json:"nickname"`
		Birthday      *string `json:"birthday"`
		AIStyle       *string `json:"ai_style"`
		RetentionDays *int    `json:"chat_retention_days"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	if req.AIStyle != nil {
		// ai_style 存风格名；空 = 跟随全局默认（AD2）。合法性 = 风格库中存在。
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
	if req.RetentionDays != nil {
		switch *req.RetentionDays {
		case 7, 30, 90, -1: // -1 = 永久
		default:
			writeError(w, http.StatusBadRequest, "保留时长仅支持 7/30/90 天或永久(-1)")
			return
		}
	}
	if err := a.db.UpdateUserSelf(r.Context(), claims.UserID, req.Nickname, req.Birthday, req.AIStyle, req.RetentionDays); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "用户不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
