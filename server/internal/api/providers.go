package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/bookandmusic/love-life/server/internal/crypto"
	"github.com/bookandmusic/love-life/server/internal/llm"
	"github.com/bookandmusic/love-life/server/internal/store"
)

// providersAPI AI 供应商管理（admin 域）：契约对齐 tools/mock「管理后台」段——
// Key 加密存储、只写不读（响应永不回读密钥）；models 参数挂模型级，null = 调用不传。
type providersAPI struct {
	db  *store.DB
	key []byte
}

// modelOut / providerOut 响应形状（对齐 mock）：has_key 布尔，密钥不出网。
type modelOut struct {
	ID          string   `json:"id"`
	Temperature *float64 `json:"temperature"`
	MaxTokens   *int64   `json:"max_tokens"`
}

type providerOut struct {
	ID           int64      `json:"id"`
	APIType      string     `json:"api_type"`
	Name         string     `json:"name"`
	BaseURL      string     `json:"base_url"`
	HasKey       bool       `json:"has_key"`
	Models       []modelOut `json:"models"`
	DefaultModel string     `json:"default_model"`
}

func toProviderOut(p *store.ModelProvider) providerOut {
	models := make([]modelOut, 0, len(p.Models))
	for _, m := range p.Models {
		var temp *float64
		var maxTok *int64
		if m.Temperature.Valid {
			t := m.Temperature.Float64
			temp = &t
		}
		if m.MaxTokens.Valid {
			mt := m.MaxTokens.Int64
			maxTok = &mt
		}
		models = append(models, modelOut{ID: m.ModelID, Temperature: temp, MaxTokens: maxTok})
	}
	return providerOut{
		ID: p.ID, APIType: p.APIType, Name: p.Name, BaseURL: p.BaseURL,
		HasKey: p.APIKeyEnc != "", Models: models, DefaultModel: p.DefaultModel,
	}
}

func sqlNullFloat(f float64) sql.NullFloat64 { return sql.NullFloat64{Float64: f, Valid: true} }
func sqlNullInt(i int64) sql.NullInt64       { return sql.NullInt64{Int64: i, Valid: true} }

func (a *providersAPI) list(w http.ResponseWriter, r *http.Request) {
	list, err := a.db.ListProviders(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	out := make([]providerOut, 0, len(list))
	for _, p := range list {
		out = append(out, toProviderOut(p))
	}
	writeJSON(w, http.StatusOK, out)
}

// modelIn 请求侧模型条目：temperature/max_tokens 可能是 null / 数字 / 数字字符串 / 空串。
type modelIn struct {
	ID          string `json:"id"`
	Temperature any    `json:"temperature"`
	MaxTokens   any    `json:"max_tokens"`
}

// numOrNull 归一：null/”/缺失 → ok=false（存 NULL）；数字或数字字符串 → 值。
func numOrNull(v any) (float64, bool) {
	switch t := v.(type) {
	case nil:
		return 0, false
	case string:
		if strings.TrimSpace(t) == "" {
			return 0, false
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		if err != nil {
			return 0, true // 非数字字符串：视作提供了值（校验阶段报错）
		}
		return f, true
	case float64:
		return t, true
	case json.Number:
		f, _ := t.Float64()
		return f, true
	default:
		return 0, false
	}
}

// normalizeModels 模型条目归一与校验（文案逐字对齐 mock）。
func normalizeModels(raw []modelIn) ([]store.ProviderModel, string) {
	out := make([]store.ProviderModel, 0, len(raw))
	for _, m := range raw {
		id := strings.TrimSpace(m.ID)
		if id == "" {
			return nil, "models 中存在模型名为空的条目"
		}
		entry := store.ProviderModel{ModelID: id}
		if t, provided := numOrNull(m.Temperature); provided {
			if t < 0 || t > 2 {
				return nil, "模型 " + id + " 的 temperature 需在 0~2 之间，或不设置"
			}
			entry.Temperature = sqlNullFloat(t)
		}
		if mt, provided := numOrNull(m.MaxTokens); provided {
			mti := int64(mt)
			if mt != float64(mti) || mti < 256 || mti > 32768 {
				return nil, "模型 " + id + " 的 max_tokens 需为 256~32768 整数，或不设置"
			}
			entry.MaxTokens = sqlNullInt(mti)
		}
		out = append(out, entry)
	}
	return out, ""
}

func (a *providersAPI) create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		APIType      string    `json:"api_type"`
		Name         string    `json:"name"`
		BaseURL      string    `json:"base_url"`
		APIKey       string    `json:"api_key"`
		DefaultModel string    `json:"default_model"`
		Models       []modelIn `json:"models"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if _, err := llm.NewForProtocol("", "", "", req.APIType); err != nil {
		writeError(w, http.StatusBadRequest, "api_type 不支持（openai-chat / openai-responses / anthropic / gemini）")
		return
	}
	if req.BaseURL == "" {
		writeError(w, http.StatusBadRequest, "base_url 必填")
		return
	}
	if req.APIKey == "" {
		writeError(w, http.StatusBadRequest, "api_key 必填")
		return
	}
	models, merr := normalizeModels(req.Models)
	if merr != "" {
		writeError(w, http.StatusBadRequest, merr)
		return
	}
	enc, err := crypto.Encrypt(a.key, req.APIKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	// sort_order = 追加到链尾（当前最大 +1；空链从 0 起）
	list, err := a.db.ListProviders(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	nextOrder := 0
	if len(list) > 0 {
		nextOrder = list[len(list)-1].SortOrder + 1
	}
	p := &store.ModelProvider{
		APIType: req.APIType, Name: strings.TrimSpace(req.Name), BaseURL: req.BaseURL,
		APIKeyEnc: enc, DefaultModel: req.DefaultModel, SortOrder: nextOrder,
	}
	if p.Name == "" {
		p.Name = req.APIType
	}
	id, err := a.db.CreateProvider(r.Context(), p, models)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	fresh, err := a.db.GetProvider(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	writeJSON(w, http.StatusCreated, toProviderOut(fresh))
}

func (a *providersAPI) update(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "供应商不存在")
		return
	}
	p, err := a.db.GetProvider(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "供应商不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	var req struct {
		APIType      *string    `json:"api_type"`
		Name         *string    `json:"name"`
		BaseURL      *string    `json:"base_url"`
		DefaultModel *string    `json:"default_model"`
		Models       *[]modelIn `json:"models"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if req.APIType != nil {
		if _, err := llm.NewForProtocol("", "", "", *req.APIType); err != nil {
			writeError(w, http.StatusBadRequest, "api_type 不支持（openai-chat / openai-responses / anthropic / gemini）")
			return
		}
		p.APIType = *req.APIType
	}
	if req.Name != nil {
		if trimmed := strings.TrimSpace(*req.Name); trimmed != "" {
			p.Name = trimmed
		}
	}
	if req.BaseURL != nil {
		if *req.BaseURL == "" {
			writeError(w, http.StatusBadRequest, "base_url 不能为空")
			return
		}
		p.BaseURL = *req.BaseURL
	}
	if req.DefaultModel != nil {
		p.DefaultModel = *req.DefaultModel
	}
	replaceModels := req.Models != nil
	var models []store.ProviderModel
	if replaceModels {
		norm, merr := normalizeModels(*req.Models)
		if merr != "" {
			writeError(w, http.StatusBadRequest, merr)
			return
		}
		models = norm
	}
	if err := a.db.UpdateProvider(r.Context(), id, p.APIType, p.Name, p.BaseURL, p.DefaultModel, models, replaceModels); err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	fresh, err := a.db.GetProvider(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	writeJSON(w, http.StatusOK, toProviderOut(fresh))
}

func (a *providersAPI) updateKey(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "供应商不存在")
		return
	}
	var req struct {
		APIKey string `json:"api_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.APIKey == "" {
		writeError(w, http.StatusBadRequest, "api_key 必填")
		return
	}
	enc, err := crypto.Encrypt(a.key, req.APIKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	if err := a.db.UpdateProviderKey(r.Context(), id, enc); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "供应商不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	p, err := a.db.GetProvider(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	writeJSON(w, http.StatusOK, toProviderOut(p))
}

func (a *providersAPI) order(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "ids 必填（数组）")
		return
	}
	list, err := a.db.ListProviders(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	// ids 必须恰好包含全部供应商（无未知、无重复）
	if len(req.IDs) != len(list) {
		writeError(w, http.StatusBadRequest, "ids 必须恰好包含全部供应商")
		return
	}
	known := map[int64]bool{}
	for _, p := range list {
		known[p.ID] = true
	}
	seen := map[int64]bool{}
	for _, id := range req.IDs {
		if !known[id] || seen[id] {
			writeError(w, http.StatusBadRequest, "ids 必须恰好包含全部供应商")
			return
		}
		seen[id] = true
	}
	if err := a.db.ReorderProviders(r.Context(), req.IDs); err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	fresh, err := a.db.ListProviders(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	out := make([]providerOut, 0, len(fresh))
	for _, p := range fresh {
		out = append(out, toProviderOut(p))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *providersAPI) remove(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "供应商不存在")
		return
	}
	if err := a.db.DeleteProvider(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "供应商不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *providersAPI) fetchModels(w http.ResponseWriter, r *http.Request) {
	var req struct {
		APIType    string `json:"api_type"`
		BaseURL    string `json:"base_url"`
		APIKey     string `json:"api_key"`
		ProviderID int64  `json:"provider_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if _, err := llm.NewForProtocol("", "", "", req.APIType); err != nil {
		writeError(w, http.StatusBadRequest, "api_type 不支持（openai-chat / openai-responses / anthropic / gemini）")
		return
	}
	if req.BaseURL == "" {
		writeError(w, http.StatusBadRequest, "base_url 必填")
		return
	}
	apiKey := req.APIKey
	if apiKey == "" {
		// 已保存过秘钥的供应商可只传 provider_id（服务端用存储秘钥解密）
		if req.ProviderID == 0 {
			writeError(w, http.StatusBadRequest, "api_key 必填（或使用已保存秘钥的供应商）")
			return
		}
		p, err := a.db.GetProvider(r.Context(), req.ProviderID)
		if err != nil || p.APIKeyEnc == "" {
			writeError(w, http.StatusBadRequest, "api_key 必填（或使用已保存秘钥的供应商）")
			return
		}
		plain, err := crypto.Decrypt(a.key, p.APIKeyEnc)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "模型 Key 解密失败，请管理员检查配置")
			return
		}
		apiKey = plain
	}
	models, err := llm.ListModels(r.Context(), req.APIType, req.BaseURL, apiKey)
	if err != nil {
		writeError(w, http.StatusBadGateway, "获取模型列表失败："+err.Error())
		return
	}
	if models == nil {
		models = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}
