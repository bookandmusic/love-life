package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/bookandmusic/love-life/server/internal/crypto"
	"github.com/bookandmusic/love-life/server/internal/store"
)

type modelConfigAPI struct {
	db  *store.DB
	key []byte
}

func (a *modelConfigAPI) list(w http.ResponseWriter, r *http.Request) {
	list, err := a.db.ListModelConfigs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	type item struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		BaseURL   string `json:"base_url"`
		Model     string `json:"model"`
		IsDefault bool   `json:"is_default"`
		IsBackup  bool   `json:"is_backup"`
	}
	items := make([]item, 0, len(list))
	for _, c := range list {
		items = append(items, item{ID: c.ID, Name: c.Name, BaseURL: c.BaseURL, Model: c.Model, IsDefault: c.IsDefault, IsBackup: c.IsBackup})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type modelConfigRequest struct {
	Name      string `json:"name"`
	BaseURL   string `json:"base_url"`
	APIKey    string `json:"api_key"`
	Model     string `json:"model"`
	IsDefault bool   `json:"is_default"`
	IsBackup  bool   `json:"is_backup"`
}

func (a *modelConfigAPI) save(w http.ResponseWriter, r *http.Request) {
	var req modelConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.BaseURL == "" || req.Model == "" {
		writeError(w, http.StatusBadRequest, "base_url 与 model 不能为空")
		return
	}
	enc, err := crypto.Encrypt(a.key, req.APIKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "加密失败")
		return
	}
	cfg := &store.ModelConfig{
		Name: req.Name, BaseURL: req.BaseURL, APIKeyEnc: enc,
		Model: req.Model, IsDefault: req.IsDefault, IsBackup: req.IsBackup,
	}
	if req.IsDefault {
		// 新默认启用地即切换（热切换，DESIGN 4.1）
		if _, err := a.db.ExecContext(r.Context(), `UPDATE model_provider_configs SET is_default = 0`); err != nil {
			writeError(w, http.StatusInternalServerError, "服务器错误")
			return
		}
	}
	id, err := a.db.SaveModelConfig(r.Context(), cfg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (a *modelConfigAPI) remove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == 0 {
		writeError(w, http.StatusBadRequest, "无效 ID")
		return
	}
	if err := a.db.DeleteModelConfig(r.Context(), req.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "配置不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
