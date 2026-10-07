package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/bookandmusic/love-life/server/internal/auth"
	"github.com/bookandmusic/love-life/server/internal/lunar"
	"github.com/bookandmusic/love-life/server/internal/notify"
	"github.com/bookandmusic/love-life/server/internal/store"
)

type anniversariesAPI struct {
	db *store.DB
}

func (a *anniversariesAPI) create(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	var req struct {
		Name string `json:"name"`
		Date string `json:"date"`
		Type string `json:"type"`
		// IsLeap 仅 type=lunar 有效（闰月标记）；
		// solar 一律忽略，恒按平月。
		IsLeap *bool `json:"is_leap"`
		// 指针以区分「未传」与「显式 0」：未传 → 默认提前 1 天；
		// 显式 0 = 当天提醒，不得被缺省逻辑改写（契约同 mock routes.mjs）。
		RemindDays *int `json:"remind_days_before"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "名称与日期不能为空")
		return
	}
	// 统一契约（mock routes.mjs）：type 缺省/其它值归一 solar，仅显式 'lunar' 生效；
	// date 一律 YYYY-MM-DD（lunar 时为农历日期，可落在公历不存在的日子，只校验格式）。
	annivType := "solar"
	if req.Type == "lunar" {
		annivType = "lunar"
	}
	// is_leap 仅 lunar 生效（mock routes.mjs 同口径）
	isLeap := false
	if annivType == "lunar" && req.IsLeap != nil {
		isLeap = *req.IsLeap
	}
	if !lunar.ValidDateShape(req.Date) {
		writeError(w, http.StatusBadRequest, "日期格式应为 YYYY-MM-DD")
		return
	}
	if annivType != "lunar" { // solar 需为真实公历日；lunar 仅格式（农历日可为公历不存在日，如 2026-02-30）
		if _, err := time.Parse("2006-01-02", req.Date); err != nil {
			writeError(w, http.StatusBadRequest, "日期格式应为 YYYY-MM-DD")
			return
		}
	}
	remindDays := 1 // 缺省：提前 1 天
	if req.RemindDays != nil {
		remindDays = *req.RemindDays
	}
	if remindDays < 0 || remindDays > 90 {
		remindDays = 1
	}
	id, err := a.db.CreateAnniversary(r.Context(), req.Name, req.Date, annivType, isLeap, remindDays, claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (a *anniversariesAPI) list(w http.ResponseWriter, r *http.Request) {
	list, err := a.db.ListAnniversaries(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	now := time.Now()
	type item struct {
		ID         int64  `json:"id"`
		Name       string `json:"name"`
		Date       string `json:"date"`
		Type       string `json:"type"`
		IsLeap     bool   `json:"is_leap"`
		RemindDays int    `json:"remind_days_before"`
		DaysUntil  int    `json:"days_until"`
		CreatedBy  int64  `json:"created_by"`
	}
	items := make([]item, 0, len(list))
	for _, ann := range list {
		t := ann.Type
		if t != "lunar" {
			t = "solar" // 与 mock 一致：非 lunar 一律输出 solar
		}
		items = append(items, item{ID: ann.ID, Name: ann.Name, Date: ann.Date, Type: t, IsLeap: ann.IsLeap, RemindDays: ann.RemindDaysBefore, DaysUntil: ann.DaysUntil(now), CreatedBy: ann.CreatedBy})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *anniversariesAPI) updateReminder(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidID)
		return
	}
	var req struct {
		Days int `json:"remind_days_before"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Days < 0 || req.Days > 90 {
		writeError(w, http.StatusBadRequest, "提前天数应为 0-90")
		return
	}
	if err := a.db.UpdateAnniversaryReminder(r.Context(), id, req.Days); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "纪念日不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *anniversariesAPI) remove(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidID)
		return
	}
	// 先取行：删除后拿不到名称与创建者（也用于把 404 判定提前）
	ann, err := a.db.GetAnniversary(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "纪念日不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	if err := a.db.SoftDeleteAnniversary(r.Context(), id, store.ActorTypeUser, claims.UserID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "纪念日不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	// 删除对方创建的纪念日 → 站内通知创建者（自删静默；内容删除仅站内）
	_ = a.db.NotifyContentDeleted(r.Context(), store.DeletedNotice{
		CreatorID: ann.CreatedBy, DeleterID: claims.UserID, DeleterName: claims.Username,
		EventType: "anniversary_deleted", TargetType: "anniversary", TargetID: id,
		Extra: map[string]any{"name": ann.Name},
	})
	// ：删除一律 204 No Content
	w.WriteHeader(http.StatusNoContent)
}

// ---- Webhook 渠道（空间级，admin 后台配置） ----

type webhooksAPI struct {
	db *store.DB
	dp *notify.Dispatcher
}

var validEvents = map[string]bool{
	"new_post": true, "like": true, "comment": true,
	"favorite": true, "anniversary": true, "footprint": true, "album": true,
}

func (a *webhooksAPI) decode(r *http.Request) (*store.WebhookChannel, error) {
	var req struct {
		Name         string            `json:"name"`
		Method       string            `json:"method"`
		URL          string            `json:"url"`
		Headers      map[string]string `json:"headers"`
		BodyTemplate string            `json:"body_template"`
		Events       []string          `json:"events"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	if req.Method == "" {
		req.Method = "POST"
	}
	if req.Method != "GET" && req.Method != "POST" {
		return nil, errBadWebhook{"method 仅支持 GET/POST"}
	}
	if req.URL == "" {
		return nil, errBadWebhook{"服务地址不能为空"}
	}
	for _, e := range req.Events {
		if !validEvents[e] {
			return nil, errBadWebhook{"未知事件类型: " + e}
		}
	}
	if req.Headers == nil {
		req.Headers = map[string]string{}
	}
	return &store.WebhookChannel{
		Name: req.Name, Method: req.Method, URL: req.URL,
		Headers: req.Headers, BodyTemplate: req.BodyTemplate, Events: req.Events,
	}, nil
}

type errBadWebhook struct{ msg string }

func (e errBadWebhook) Error() string { return e.msg }

func (a *webhooksAPI) create(w http.ResponseWriter, r *http.Request) {
	ch, err := a.decode(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := a.db.CreateWebhookChannel(r.Context(), ch)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (a *webhooksAPI) list(w http.ResponseWriter, r *http.Request) {
	list, err := a.db.ListWebhookChannels(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	type item struct {
		ID           int64             `json:"id"`
		Name         string            `json:"name"`
		Method       string            `json:"method"`
		URL          string            `json:"url"`
		Headers      map[string]string `json:"headers"`
		BodyTemplate string            `json:"body_template"`
		Events       []string          `json:"events"`
	}
	items := make([]item, 0, len(list))
	for _, c := range list {
		items = append(items, item{ID: c.ID, Name: c.Name, Method: c.Method, URL: c.URL, Headers: c.Headers, BodyTemplate: c.BodyTemplate, Events: c.Events})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *webhooksAPI) update(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidID)
		return
	}
	ch, err := a.decode(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ch.ID = id
	if err := a.db.UpdateWebhookChannel(r.Context(), ch); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "渠道不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *webhooksAPI) remove(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidID)
		return
	}
	if err := a.db.DeleteWebhookChannel(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "渠道不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *webhooksAPI) test(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidID)
		return
	}
	ch, err := a.db.GetWebhookChannel(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "渠道不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	if err := a.dp.PushTest(r.Context(), ch); err != nil {
		writeError(w, http.StatusBadGateway, "推送失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
