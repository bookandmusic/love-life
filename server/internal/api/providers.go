package api

import (
	"context"
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
	Kind        string   `json:"kind"`
	Temperature *float64 `json:"temperature"`
	MaxTokens   *int64   `json:"max_tokens"`
}

type providerOut struct {
	ID      int64      `json:"id"`
	APIType string     `json:"api_type"`
	Name    string     `json:"name"`
	BaseURL string     `json:"base_url"`
	HasKey  bool       `json:"has_key"`
	Models  []modelOut `json:"models"`
}

func toModelOut(m store.ProviderModel) modelOut {
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
	return modelOut{ID: m.ModelID, Kind: m.Kind, Temperature: temp, MaxTokens: maxTok}
}

func toProviderOut(p *store.ModelProvider) providerOut {
	models := make([]modelOut, 0, len(p.Models))
	for _, m := range p.Models {
		models = append(models, toModelOut(m))
	}
	return providerOut{
		ID: p.ID, APIType: p.APIType, Name: p.Name, BaseURL: p.BaseURL,
		HasKey: p.APIKeyEnc != "", Models: models,
	}
}

// modelRowOut 模型条目行（链视图）：链序 = 数组顺序（chain_order, id）。
type modelRowOut struct {
	ID          int64    `json:"id"`
	ProviderID  int64    `json:"provider_id"`
	ProviderNm  string   `json:"provider_name"`
	APIType     string   `json:"api_type"`
	Kind        string   `json:"kind"`
	ModelID     string   `json:"model_id"`
	Temperature *float64 `json:"temperature"`
	MaxTokens   *int64   `json:"max_tokens"`
}

func toModelRowOut(e store.ChainEntry) modelRowOut {
	o := modelRowOut{
		ID: e.Model.ID, ProviderID: e.Provider.ID, ProviderNm: e.Provider.Name,
		APIType: e.Provider.APIType, Kind: e.Model.Kind, ModelID: e.Model.ModelID,
	}
	o.Temperature = toModelOut(e.Model).Temperature
	o.MaxTokens = toModelOut(e.Model).MaxTokens
	return o
}

// modelsOut 两条链一起下发（后台一页配完）。
func (a *providersAPI) modelsOut(ctx context.Context) (map[string][]modelRowOut, error) {
	out := map[string][]modelRowOut{"chat": {}, "embedding": {}}
	for _, kind := range []string{"chat", "embedding"} {
		chain, err := a.db.ListModels(ctx, kind)
		if err != nil {
			return nil, err
		}
		for _, e := range chain {
			out[kind] = append(out[kind], toModelRowOut(e))
		}
	}
	return out, nil
}

func sqlNullFloat(f float64) sql.NullFloat64 { return sql.NullFloat64{Float64: f, Valid: true} }
func sqlNullInt(i int64) sql.NullInt64       { return sql.NullInt64{Int64: i, Valid: true} }

func (a *providersAPI) list(w http.ResponseWriter, r *http.Request) {
	list, err := a.db.ListProviders(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
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
	Kind        string `json:"kind"`
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
		kind := strings.TrimSpace(m.Kind)
		if kind != "chat" && kind != "embedding" {
			return nil, "模型 " + id + " 的 kind 需为 chat 或 embedding"
		}
		entry := store.ProviderModel{Kind: kind, ModelID: id}
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
		APIType string    `json:"api_type"`
		Name    string    `json:"name"`
		BaseURL string    `json:"base_url"`
		APIKey  string    `json:"api_key"`
		Models  []modelIn `json:"models"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
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
	for _, m := range models {
		if m.Kind == "embedding" && req.APIType != llm.APITypeOpenAIChat {
			writeError(w, http.StatusBadRequest, "嵌入模型只能挂 openai-chat 供应商")
			return
		}
	}
	enc, err := crypto.Encrypt(a.key, req.APIKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	p := &store.ModelProvider{
		APIType: req.APIType, Name: strings.TrimSpace(req.Name), BaseURL: req.BaseURL,
		APIKeyEnc: enc,
	}
	if p.Name == "" {
		p.Name = req.APIType
	}
	id, err := a.db.CreateProvider(r.Context(), p, models)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	fresh, err := a.db.GetProvider(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
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
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	var req struct {
		APIType *string    `json:"api_type"`
		Name    *string    `json:"name"`
		BaseURL *string    `json:"base_url"`
		Models  *[]modelIn `json:"models"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
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
	// 协议改离 openai-chat 时，该供应商名下不得还有嵌入条目（嵌入只允许 openai-chat）
	if req.APIType != nil && p.APIType != llm.APITypeOpenAIChat {
		for _, m := range p.Models {
			if m.Kind == "embedding" {
				writeError(w, http.StatusBadRequest, "该供应商名下有嵌入模型，请先移出后再改协议（嵌入只允许 openai-chat）")
				return
			}
		}
	}
	replaceModels := req.Models != nil
	var models []store.ProviderModel
	if replaceModels {
		norm, merr := normalizeModels(*req.Models)
		if merr != "" {
			writeError(w, http.StatusBadRequest, merr)
			return
		}
		for _, m := range norm {
			if m.Kind == "embedding" && p.APIType != llm.APITypeOpenAIChat {
				writeError(w, http.StatusBadRequest, "嵌入模型只能挂 openai-chat 供应商")
				return
			}
		}
		models = norm
	}
	if err := a.db.UpdateProvider(r.Context(), id, p.APIType, p.Name, p.BaseURL, models, replaceModels); err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	fresh, err := a.db.GetProvider(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
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
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	if err := a.db.UpdateProviderKey(r.Context(), id, enc); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "供应商不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	p, err := a.db.GetProvider(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusOK, toProviderOut(p))
}

// ---- 模型条目（两条链共享 CRUD；kind 区分 chat / embedding，ARCHITECTURE「后台管理页面实现」）----

func (a *providersAPI) modelsList(w http.ResponseWriter, r *http.Request) {
	out, err := a.modelsOut(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *providersAPI) modelsCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind        string `json:"kind"`
		ProviderID  int64  `json:"provider_id"`
		ModelID     string `json:"model_id"`
		Temperature any    `json:"temperature"`
		MaxTokens   any    `json:"max_tokens"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	if req.Kind != "chat" && req.Kind != "embedding" {
		writeError(w, http.StatusBadRequest, "kind 需为 chat 或 embedding")
		return
	}
	if req.ProviderID <= 0 {
		writeError(w, http.StatusBadRequest, "provider_id 必填")
		return
	}
	p, err := a.db.GetProvider(r.Context(), req.ProviderID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "供应商不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	if req.Kind == "embedding" && p.APIType != llm.APITypeOpenAIChat {
		writeError(w, http.StatusBadRequest, "嵌入模型只能挂 openai-chat 供应商")
		return
	}
	m, merr := normalizeOne(req.Kind, strings.TrimSpace(req.ModelID), req.Temperature, req.MaxTokens)
	if merr != "" {
		writeError(w, http.StatusBadRequest, merr)
		return
	}
	m.ProviderID = p.ID
	if _, err := a.db.CreateModel(r.Context(), &m); err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	out, err := a.modelsOut(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

// normalizeOne 单条模型条目归一与校验（与 normalizeModels 同参数口径）。
func normalizeOne(kind, id string, temp, maxTok any) (store.ProviderModel, string) {
	m, err := normalizeModels([]modelIn{{ID: id, Kind: kind, Temperature: temp, MaxTokens: maxTok}})
	if err != "" {
		return store.ProviderModel{}, err
	}
	return m[0], ""
}

func (a *providersAPI) modelsUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "模型条目不存在")
		return
	}
	var req struct {
		ModelID     *string `json:"model_id"`
		Temperature any     `json:"temperature"`
		MaxTokens   any     `json:"max_tokens"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	// 现值兜底：部分更新——未传字段保留原值
	chain, err := a.db.ListModels(r.Context(), "chat")
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	chainE, err := a.db.ListModels(r.Context(), "embedding")
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	chain = append(chain, chainE...)
	var cur store.ProviderModel
	found := false
	for _, e := range chain {
		if e.Model.ID == id {
			cur = e.Model
			found = true
			break
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, "模型条目不存在")
		return
	}
	modelID := cur.ModelID
	if req.ModelID != nil {
		if trimmed := strings.TrimSpace(*req.ModelID); trimmed != "" {
			modelID = trimmed
		}
	}
	m, merr := normalizeOne(cur.Kind, modelID, req.Temperature, req.MaxTokens)
	if merr != "" {
		writeError(w, http.StatusBadRequest, merr)
		return
	}
	if err := a.db.UpdateModel(r.Context(), id, m.ModelID, m.Temperature, m.MaxTokens); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "模型条目不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	out, err := a.modelsOut(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *providersAPI) modelsRemove(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "模型条目不存在")
		return
	}
	if err := a.db.DeleteModel(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "模型条目不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// modelsOrder 链内重排：体带 kind + 有序 id 数组（须恰好覆盖该 kind 全部条目）。
func (a *providersAPI) modelsOrder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind string  `json:"kind"`
		IDs  []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "kind 与 ids 必填")
		return
	}
	if req.Kind != "chat" && req.Kind != "embedding" {
		writeError(w, http.StatusBadRequest, "kind 需为 chat 或 embedding")
		return
	}
	chain, err := a.db.ListModels(r.Context(), req.Kind)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	if len(req.IDs) != len(chain) {
		writeError(w, http.StatusBadRequest, "ids 必须恰好包含该类型的全部模型条目")
		return
	}
	known := map[int64]bool{}
	for _, e := range chain {
		known[e.Model.ID] = true
	}
	seen := map[int64]bool{}
	for _, id := range req.IDs {
		if !known[id] || seen[id] {
			writeError(w, http.StatusBadRequest, "ids 必须恰好包含该类型的全部模型条目")
			return
		}
		seen[id] = true
	}
	if err := a.db.ReorderModels(r.Context(), req.Kind, req.IDs); err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	out, err := a.modelsOut(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
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
		writeError(w, http.StatusInternalServerError, errInternal)
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
		writeError(w, http.StatusBadRequest, errBadRequest)
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
