package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
		// 显式 0 = 当天提醒，不得被缺省逻辑改写。
		RemindDays *int `json:"remind_days_before"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "名称与日期不能为空")
		return
	}
	// 统一契约：type 缺省/其它值归一 solar，仅显式 'lunar' 生效；
	// date 一律 YYYY-MM-DD（lunar 时为农历日期，可落在公历不存在的日子，只校验格式）。
	annivType := "solar"
	if req.Type == "lunar" {
		annivType = "lunar"
	}
	// is_leap 仅 lunar 生效
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
	if remindDays < 0 || remindDays > store.MaxRemindDaysBefore {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("提前天数应为 0-%d", store.MaxRemindDaysBefore))
		return
	}
	id, err := a.db.CreateAnniversary(r.Context(), req.Name, req.Date, annivType, isLeap, remindDays, claims.UserID)
	if err != nil {
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (a *anniversariesAPI) list(w http.ResponseWriter, r *http.Request) {
	list, err := a.db.ListAnniversaries(r.Context())
	if err != nil {
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
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
			t = "solar" // 非 lunar 一律输出 solar
		}
		items = append(items, item{ID: ann.ID, Name: ann.Name, Date: ann.Date, Type: t, IsLeap: ann.IsLeap, RemindDays: ann.RemindDaysBefore, DaysUntil: ann.DaysUntil(now), CreatedBy: ann.CreatedBy})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
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
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
		return
	}
	if err := a.db.SoftDeleteAnniversary(r.Context(), id, store.ActorTypeUser, claims.UserID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "纪念日不存在")
			return
		}
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
		return
	}
	// 删除对方创建的纪念日 → 站内通知创建者（自删静默；内容删除仅站内）
	operator := displayName(r.Context(), a.db, claims.UserID, claims.Username)
	_ = a.db.NotifyContentDeleted(r.Context(), store.DeletedNotice{
		CreatorID: ann.CreatedBy, DeleterID: claims.UserID, DeleterName: operator,
		EventType: "anniversary_deleted", TargetType: "anniversary", TargetID: id,
		Text:  notify.Deleted(operator, "纪念日", ann.Name),
		Extra: map[string]any{"name": ann.Name},
	})
	// ：删除一律 204 No Content
	w.WriteHeader(http.StatusNoContent)
}

// ---- Webhook 渠道（admin 后台配置） ----

type webhooksAPI struct {
	db *store.DB
	dp *notify.Dispatcher
}

// validEvents webhook 可订阅的事件（站内事件 → 外部订阅键见 notify.externalEventKey）。
// 不含 favorite：favorites 表仍保留（回收站永久删除会清理其行），但当前没有任何写入点，
// 订阅它也永远不会触发。
var validEvents = map[string]bool{
	"new_post": true, "like": true, "comment": true,
	"anniversary": true, "footprint": true, "album": true,
}

func (a *webhooksAPI) decode(r *http.Request) (*store.WebhookChannel, error) {
	var req struct {
		Name         string            `json:"name"`
		Method       string            `json:"method"`
		URL          string            `json:"url"`
		Headers      map[string]string `json:"headers"`
		BodyTemplate string            `json:"body_template"`
		Events       []string          `json:"events"`
		OwnerID      *int64            `json:"owner_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, errBadWebhook{"请求格式错误"}
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
	ch := &store.WebhookChannel{
		Name: req.Name, Method: req.Method, URL: req.URL,
		Headers: req.Headers, BodyTemplate: req.BodyTemplate, Events: req.Events,
	}
	// owner_id 省略 / null = 空间级渠道（群 / 共享）；给出则必须是真实用户，
	// 否则该个人渠道的归属永远不在收件人里，等于静默丢通知。
	// 管理员同样拒绝：两个收件人查询都排除 admin，配上去也是永久静默。
	if req.OwnerID != nil {
		u, err := a.db.GetUserByID(r.Context(), *req.OwnerID)
		if errors.Is(err, store.ErrNotFound) {
			return nil, errBadWebhook{"通知对象不存在"}
		}
		if err != nil {
			return nil, err
		}
		if u.Role == "admin" {
			return nil, errBadWebhook{"管理员不在情侣里，不作为通知对象"}
		}
		ch.OwnerID = sql.NullInt64{Int64: *req.OwnerID, Valid: true}
	}
	return ch, nil
}

type errBadWebhook struct{ msg string }

func (e errBadWebhook) Error() string { return e.msg }

// writeDecodeErr 渠道入参解码失败的统一回写：业务校验错误是 400 并回原话，
// 查询用户等内部故障走 500 并记日志——把库故障说成「通知对象不存在」会误导排障。
// 用 errors.As 而非类型断言：返回点写成指针形态（&errBadWebhook{}）时断言会静默漏掉。
func writeDecodeErr(w http.ResponseWriter, r *http.Request, err error) {
	var bad errBadWebhook
	if errors.As(err, &bad) {
		writeError(w, http.StatusBadRequest, bad.Error())
		return
	}
	writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
}

func (a *webhooksAPI) create(w http.ResponseWriter, r *http.Request) {
	ch, err := a.decode(r)
	if err != nil {
		writeDecodeErr(w, r, err)
		return
	}
	id, err := a.db.CreateWebhookChannel(r.Context(), ch)
	if err != nil {
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (a *webhooksAPI) list(w http.ResponseWriter, r *http.Request) {
	list, err := a.db.ListWebhookChannels(r.Context())
	if err != nil {
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
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
		OwnerID      *int64            `json:"owner_id"`
	}
	items := make([]item, 0, len(list))
	for _, c := range list {
		items = append(items, item{ID: c.ID, Name: c.Name, Method: c.Method, URL: c.URL, Headers: c.Headers, BodyTemplate: c.BodyTemplate, Events: c.Events, OwnerID: ownerIDPtr(c.OwnerID)})
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
		writeDecodeErr(w, r, err)
		return
	}
	ch.ID = id
	if err := a.db.UpdateWebhookChannel(r.Context(), ch); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "渠道不存在")
			return
		}
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
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
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
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
		writeErrorf(w, r, http.StatusInternalServerError, errInternal, err)
		return
	}
	if err := a.dp.PushTest(r.Context(), ch); err != nil {
		writeError(w, http.StatusBadGateway, "推送失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ownerIDPtr 渠道归属的 JSON 形态：NULL 输出 null（空间级渠道），否则输出用户 id。
func ownerIDPtr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	id := v.Int64
	return &id
}
