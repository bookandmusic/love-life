package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bookandmusic/love-life/server/internal/chat"
	"github.com/bookandmusic/love-life/server/internal/store"
)

// stylesAPI 输出风格库（admin 域 + APP 端列表）+ System Prompt 模板。
// 契约对齐 tools/mock「输出风格库 / system-prompt」段：风格 = 名称 + 提示词片段
// （拼在主干之后，切风格不动主干）；内置风格固化；默认风格 = APP 未设置时兜底。
type stylesAPI struct {
	db *store.DB
}

type styleOut struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Prompt  string `json:"prompt"`
	Builtin bool   `json:"builtin"`
}

// stylesData GET 列表与设默认的统一返回形状。
type stylesData struct {
	Items          []styleOut `json:"items"`
	DefaultStyleID int64      `json:"default_style_id"`
}

func (a *stylesAPI) listData(r *http.Request) (stylesData, error) {
	list, err := a.db.ListStyles(r.Context())
	if err != nil {
		return stylesData{}, err
	}
	def, err := a.db.DefaultStyleID(r.Context())
	if err != nil {
		return stylesData{}, err
	}
	items := make([]styleOut, 0, len(list))
	for _, s := range list {
		items = append(items, styleOut{ID: s.ID, Name: s.Name, Prompt: s.Prompt, Builtin: s.Builtin})
	}
	return stylesData{Items: items, DefaultStyleID: def}, nil
}

func (a *stylesAPI) list(w http.ResponseWriter, r *http.Request) {
	data, err := a.listData(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	writeJSON(w, http.StatusOK, data)
}

func (a *stylesAPI) create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string `json:"name"`
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name 必填")
		return
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		writeError(w, http.StatusBadRequest, "prompt 必填")
		return
	}
	id, err := a.db.CreateStyle(r.Context(), name, prompt)
	if errors.Is(err, store.ErrStyleNameTaken) {
		writeError(w, http.StatusBadRequest, "风格名称已存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	s, err := a.db.GetStyle(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	writeJSON(w, http.StatusCreated, styleOut{ID: s.ID, Name: s.Name, Prompt: s.Prompt, Builtin: s.Builtin})
}

func (a *stylesAPI) setDefault(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if req.ID == 0 {
		writeError(w, http.StatusNotFound, "风格不存在")
		return
	}
	if err := a.db.SetDefaultStyle(r.Context(), req.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "风格不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	data, err := a.listData(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	writeJSON(w, http.StatusOK, data)
}

func (a *stylesAPI) update(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "风格不存在")
		return
	}
	var req struct {
		Name   *string `json:"name"`
		Prompt *string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// mock 语义：传了才更新，trim 后非空才合法
	var name, prompt *string
	if req.Name != nil {
		v := strings.TrimSpace(*req.Name)
		if v == "" {
			writeError(w, http.StatusBadRequest, "name 不能为空")
			return
		}
		name = &v
	}
	if req.Prompt != nil {
		v := strings.TrimSpace(*req.Prompt)
		if v == "" {
			writeError(w, http.StatusBadRequest, "prompt 不能为空")
			return
		}
		prompt = &v
	}
	if err := a.db.UpdateStyle(r.Context(), id, name, prompt); err != nil {
		a.writeStyleErr(w, err)
		return
	}
	s, err := a.db.GetStyle(r.Context(), id)
	if err != nil {
		a.writeStyleErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, styleOut{ID: s.ID, Name: s.Name, Prompt: s.Prompt, Builtin: s.Builtin})
}

func (a *stylesAPI) remove(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "风格不存在")
		return
	}
	// 检查顺序对齐 mock：builtin 先于默认（内置「温暖」同时是默认时文案为「内置风格不可删除」）
	s, err := a.db.GetStyle(r.Context(), id)
	if err != nil {
		a.writeStyleErr(w, err)
		return
	}
	if s.Builtin {
		writeError(w, http.StatusBadRequest, "内置风格不可删除")
		return
	}
	def, err := a.db.DefaultStyleID(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	if def == id {
		writeError(w, http.StatusBadRequest, "默认风格不可删除，请先将其他风格设为默认")
		return
	}
	if err := a.db.DeleteStyle(r.Context(), id); err != nil {
		a.writeStyleErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeStyleErr store 层风格错误的统一映射。
func (a *stylesAPI) writeStyleErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "风格不存在")
	case errors.Is(err, store.ErrBuiltinStyle):
		writeError(w, http.StatusBadRequest, "内置风格不可修改")
	case errors.Is(err, store.ErrStyleNameTaken):
		writeError(w, http.StatusBadRequest, "风格名称已存在")
	default:
		writeError(w, http.StatusInternalServerError, "服务器错误")
	}
}

// ---- System Prompt 模板（app_settings key=system_prompt，JSON {content, updated_at}）----
// 未配置时的主干取 chat.SystemPromptTrunkFallback（与 chat 拼装保持同一来源）。

type systemPromptOut struct {
	Content   string `json:"content"`
	UpdatedAt string `json:"updatedAt"`
}

func (a *stylesAPI) getPrompt(w http.ResponseWriter, r *http.Request) {
	out := systemPromptOut{Content: chat.SystemPromptTrunkFallback}
	if v, err := a.db.GetSetting(r.Context(), "system_prompt"); err == nil {
		var saved struct {
			Content   string `json:"content"`
			UpdatedAt string `json:"updated_at"`
		}
		if json.Unmarshal([]byte(v), &saved) == nil && saved.Content != "" {
			out = systemPromptOut{Content: saved.Content, UpdatedAt: saved.UpdatedAt}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *stylesAPI) setPrompt(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Content) == "" {
		writeError(w, http.StatusBadRequest, "content 必填")
		return
	}
	// updatedAt 格式对齐 mock nowText()：UTC YYYY-MM-DD HH:MM:SS
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	saved, _ := json.Marshal(map[string]string{"content": req.Content, "updated_at": now})
	if err := a.db.SetSetting(r.Context(), "system_prompt", string(saved)); err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	writeJSON(w, http.StatusOK, systemPromptOut{Content: req.Content, UpdatedAt: now})
}

// appList 已并入 list（APP 端与 admin 同形状）
