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
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	MaxTokens *int64 `json:"max_tokens"`
	// Thinking 思考档位（off/low/medium/high）；off = 该协议的最低档（现役模型关不掉思考）。
	Thinking string `json:"thinking"`
	// ThinkingLevels 该模型支持的思考档位（"off,low,high" 之类），admin 下拉的可选项来源。
	ThinkingLevels string `json:"thinking_levels"`
	// ContextWindow 模型上下文窗口（token），null = 未声明、用服务端兜底预算。
	ContextWindow *int64 `json:"context_window"`
}

type providerOut struct {
	ID      int64  `json:"id"`
	APIType string `json:"api_type"`
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	// HeaderTimeoutSec 等待上游响应头（首包）的超时秒数；null = 默认 30s。
	HeaderTimeoutSec *int64     `json:"header_timeout_sec"`
	HasKey           bool       `json:"has_key"`
	Models           []modelOut `json:"models"`
}

func toModelOut(m store.ProviderModel) modelOut {
	var maxTok, ctxWin *int64
	if m.MaxTokens.Valid {
		mt := m.MaxTokens.Int64
		maxTok = &mt
	}
	if m.ContextWindow.Valid {
		cw := m.ContextWindow.Int64
		ctxWin = &cw
	}
	return modelOut{ID: m.ModelID, Kind: m.Kind, MaxTokens: maxTok, Thinking: m.Thinking,
		ThinkingLevels: m.ThinkingLevels, ContextWindow: ctxWin}
}

func toProviderOut(p *store.ModelProvider) providerOut {
	models := make([]modelOut, 0, len(p.Models))
	for _, m := range p.Models {
		models = append(models, toModelOut(m))
	}
	var headerTimeout *int64
	if p.HeaderTimeoutSec.Valid {
		h := p.HeaderTimeoutSec.Int64
		headerTimeout = &h
	}
	return providerOut{
		ID: p.ID, APIType: p.APIType, Name: p.Name, BaseURL: p.BaseURL,
		HeaderTimeoutSec: headerTimeout, HasKey: p.APIKeyEnc != "", Models: models,
	}
}

// normalizeHeaderTimeout 归一响应头超时：nil/0 → NULL（默认 30s）；负数拒绝。
func normalizeHeaderTimeout(sec *int64) (sql.NullInt64, string) {
	if sec == nil || *sec <= 0 {
		return sql.NullInt64{}, ""
	}
	if *sec > 3600 {
		return sql.NullInt64{}, "响应头超时上限 3600 秒"
	}
	return sql.NullInt64{Int64: *sec, Valid: true}, ""
}

// modelRowOut 模型条目行（链视图）：链序 = 数组顺序（chain_order, id）。
type modelRowOut struct {
	ID             int64  `json:"id"`
	ProviderID     int64  `json:"provider_id"`
	ProviderNm     string `json:"provider_name"`
	APIType        string `json:"api_type"`
	Kind           string `json:"kind"`
	ModelID        string `json:"model_id"`
	MaxTokens      *int64 `json:"max_tokens"`
	Thinking       string `json:"thinking"`
	ThinkingLevels string `json:"thinking_levels"`
	ContextWindow  *int64 `json:"context_window"`
}

func toModelRowOut(e store.ChainEntry) modelRowOut {
	o := modelRowOut{
		ID: e.Model.ID, ProviderID: e.Provider.ID, ProviderNm: e.Provider.Name,
		APIType: e.Provider.APIType, Kind: e.Model.Kind, ModelID: e.Model.ModelID,
	}
	m := toModelOut(e.Model)
	o.MaxTokens = m.MaxTokens
	o.Thinking = m.Thinking
	o.ThinkingLevels = m.ThinkingLevels
	o.ContextWindow = m.ContextWindow
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

func sqlNullInt(i int64) sql.NullInt64 { return sql.NullInt64{Int64: i, Valid: true} }

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

// modelIn 请求侧模型条目：max_tokens / context_window 可能是 null / 数字 / 数字字符串 / 空串。
type modelIn struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	MaxTokens any    `json:"max_tokens"`
	// Thinking 思考档位；缺省（空串/缺失）= off。未知取值在校验处拒绝而非静默归 off，
	// 以便配置界面写错时立刻可见。
	Thinking string `json:"thinking"`
	// ThinkingLevels 该模型支持的思考档位（逗号分隔子集）；缺省只有 off。
	// 思考档位必须落在集合内：档位取值是模型属性（Gemini 3 Pro 只接受 low/high），
	// 在配置面声明即可，代码不判模型名（别名会让名称匹配落空）。
	ThinkingLevels string `json:"thinking_levels"`
	// ContextWindow 模型上下文窗口（token），可空。
	ContextWindow any `json:"context_window"`
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

// checkThinkingAllowed 思考档位与协议的一致性（admin 下拉的同口径服务端校验）。
// OpenAI Chat 协议没有统一的思考参数（是否回思考由供应商自定），故该协议下不允许配置档位：
// 只做在下拉上不够——接口可被直接调用，写进去的值最终会被 adapter 忽略，形成「配了不生效」的静默错觉。
func checkThinkingAllowed(apiType string, models []store.ProviderModel) string {
	if apiType != llm.APITypeOpenAIChat {
		return ""
	}
	for _, m := range models {
		// 声明集只允许 off 即可覆盖档位：档位必须落在声明集内（normalizeModels 已校验），
		// 集合只有 off 时档位自然也只剩 off。此处单独判 thinking 只为给出更直白的文案。
		if m.Thinking != store.ThinkingOff || m.ThinkingLevels != store.ThinkingOff {
			return "OpenAI Chat 协议没有统一的思考参数，思考档位只能为关"
		}
	}
	return ""
}

// embeddingOffProtocol 嵌入条目的协议一致性：嵌入只允许挂 openai-chat 供应商。
// 三处入口（建模型、编辑供应商带 models、改协议）共用一处文案，措辞与 mock 逐字一致——
// 两处各写一遍就会漂移，而 mock 存在的意义正是「本地能存的形态与 server 一致」。
func embeddingOffProtocol(apiType string, models []store.ProviderModel) string {
	if apiType == llm.APITypeOpenAIChat {
		return ""
	}
	for _, m := range models {
		if m.Kind == "embedding" {
			return "嵌入模型只能挂 openai-chat 供应商"
		}
	}
	return ""
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
		if mt, provided := numOrNull(m.MaxTokens); provided {
			mti := int64(mt)
			if mt != float64(mti) || mti < 256 || mti > 32768 {
				return nil, "模型 " + id + " 的 max_tokens 需为 256~32768 整数，或不设置"
			}
			entry.MaxTokens = sqlNullInt(mti)
		}
		if cw, provided := numOrNull(m.ContextWindow); provided {
			cwi := int64(cw)
			if cw != float64(cwi) || cwi < 1024 || cwi > 2000000 {
				return nil, "模型 " + id + " 的 context_window 需为 1024~2000000 整数，或不设置"
			}
			entry.ContextWindow = sqlNullInt(cwi)
		}
		// 最大输出不得超过上下文窗口：否则「窗口 − 输出预留」为负，历史预算恒被压到下限。
		if entry.MaxTokens.Valid && entry.ContextWindow.Valid && entry.MaxTokens.Int64 > entry.ContextWindow.Int64 {
			return nil, "模型 " + id + " 的 max_tokens 不得超过 context_window"
		}
		thinking, ok := store.NormalizeThinking(strings.TrimSpace(m.Thinking))
		if !ok {
			return nil, "模型 " + id + " 的 thinking 需为 off / low / medium / high"
		}
		levels, lok := store.NormalizeThinkingLevels(m.ThinkingLevels)
		if !lok {
			return nil, "模型 " + id + " 的 thinking_levels 需为 off / low / medium / high 的逗号分隔子集"
		}
		// 档位必须落在声明的支持集合内：否则选了该模型会向供应商发它不认的档位（轻则报错重则静默降级）。
		if !store.ThinkingLevelAllowed(levels, thinking) {
			return nil, "模型 " + id + " 的 thinking 需在 thinking_levels 声明的档位内"
		}
		entry.Thinking = thinking
		entry.ThinkingLevels = levels
		out = append(out, entry)
	}
	return out, ""
}

func (a *providersAPI) create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		APIType          string    `json:"api_type"`
		Name             string    `json:"name"`
		BaseURL          string    `json:"base_url"`
		HeaderTimeoutSec *int64    `json:"header_timeout_sec"`
		APIKey           string    `json:"api_key"`
		Models           []modelIn `json:"models"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	if _, err := llm.NewForProtocol("", "", "", req.APIType, 0); err != nil {
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
	if terr := checkThinkingAllowed(req.APIType, models); terr != "" {
		writeError(w, http.StatusBadRequest, terr)
		return
	}
	if eerr := embeddingOffProtocol(req.APIType, models); eerr != "" {
		writeError(w, http.StatusBadRequest, eerr)
		return
	}
	enc, err := crypto.Encrypt(a.key, req.APIKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	headerTimeout, terr := normalizeHeaderTimeout(req.HeaderTimeoutSec)
	if terr != "" {
		writeError(w, http.StatusBadRequest, terr)
		return
	}
	p := &store.ModelProvider{
		APIType: req.APIType, Name: strings.TrimSpace(req.Name), BaseURL: req.BaseURL,
		APIKeyEnc: enc, HeaderTimeoutSec: headerTimeout,
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
		APIType          *string    `json:"api_type"`
		Name             *string    `json:"name"`
		BaseURL          *string    `json:"base_url"`
		HeaderTimeoutSec *int64     `json:"header_timeout_sec"`
		Models           *[]modelIn `json:"models"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	if req.APIType != nil {
		if _, err := llm.NewForProtocol("", "", "", *req.APIType, 0); err != nil {
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
	if req.HeaderTimeoutSec != nil {
		headerTimeout, terr := normalizeHeaderTimeout(req.HeaderTimeoutSec)
		if terr != "" {
			writeError(w, http.StatusBadRequest, terr)
			return
		}
		p.HeaderTimeoutSec = headerTimeout
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
	// 落库后的条目形态：带 models 用请求里的，不带则沿用名下旧条目。
	// 档位校验一律对「落库后的形态」做——只改协议、不带 models 的编辑（admin 的供应商表单正是如此）
	// 会把旧条目原样留下，只校验请求里的条目就会漏掉它们，留下该协议根本不认的档位。
	next := p.Models
	var models []store.ProviderModel
	if replaceModels {
		norm, merr := normalizeModels(*req.Models)
		if merr != "" {
			writeError(w, http.StatusBadRequest, merr)
			return
		}
		next, models = norm, norm
	}
	if terr := checkThinkingAllowed(p.APIType, next); terr != "" {
		writeError(w, http.StatusBadRequest, terr)
		return
	}
	if eerr := embeddingOffProtocol(p.APIType, next); eerr != "" {
		writeError(w, http.StatusBadRequest, eerr)
		return
	}
	if err := a.db.UpdateProvider(r.Context(), id, p.APIType, p.Name, p.BaseURL, p.HeaderTimeoutSec, models, replaceModels); err != nil {
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
		Kind           string `json:"kind"`
		ProviderID     int64  `json:"provider_id"`
		ModelID        string `json:"model_id"`
		MaxTokens      any    `json:"max_tokens"`
		Thinking       string `json:"thinking"`
		ThinkingLevels string `json:"thinking_levels"`
		ContextWindow  any    `json:"context_window"`
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
	// 嵌入守卫放在归一之前：与 mock 的判定顺序一致（两处都先拒协议不符，再报字段错误）。
	if eerr := embeddingOffProtocol(p.APIType, []store.ProviderModel{{Kind: req.Kind}}); eerr != "" {
		writeError(w, http.StatusBadRequest, eerr)
		return
	}
	m, merr := normalizeOne(req.Kind, strings.TrimSpace(req.ModelID), req.MaxTokens, req.Thinking, req.ThinkingLevels, req.ContextWindow)
	if merr != "" {
		writeError(w, http.StatusBadRequest, merr)
		return
	}
	if terr := checkThinkingAllowed(p.APIType, []store.ProviderModel{m}); terr != "" {
		writeError(w, http.StatusBadRequest, terr)
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
func normalizeOne(kind, id string, maxTok any, thinking, thinkingLevels string, ctxWindow any) (store.ProviderModel, string) {
	m, err := normalizeModels([]modelIn{{ID: id, Kind: kind, MaxTokens: maxTok,
		Thinking: thinking, ThinkingLevels: thinkingLevels, ContextWindow: ctxWindow}})
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
		ModelID       *string `json:"model_id"`
		MaxTokens     any     `json:"max_tokens"`
		ContextWindow any     `json:"context_window"`
		// Thinking / ThinkingLevels 指针：nil = 未传（保留原值），空串 = 显式置 off
		Thinking       *string `json:"thinking"`
		ThinkingLevels *string `json:"thinking_levels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	// 现值兜底：model_id / thinking / thinking_levels 未传则保留原值。
	// max_tokens / context_window 是「整值替换」——省略或 null 都会落 NULL（清空），
	// 因为这两个字段要支持「清掉已配的上限」；前端表单每次提交都带全量字段。
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
	thinking := cur.Thinking
	if req.Thinking != nil {
		thinking = *req.Thinking
	}
	levels := cur.ThinkingLevels
	if req.ThinkingLevels != nil {
		levels = *req.ThinkingLevels
	}
	m, merr := normalizeOne(cur.Kind, modelID, req.MaxTokens, thinking, levels, req.ContextWindow)
	if merr != "" {
		writeError(w, http.StatusBadRequest, merr)
		return
	}
	// 本条目的供应商协议（链里已经带着，不用再查一次库）
	curAPIType := ""
	for _, e := range chain {
		if e.Model.ID == id {
			curAPIType = e.Provider.APIType
			break
		}
	}
	if terr := checkThinkingAllowed(curAPIType, []store.ProviderModel{m}); terr != "" {
		writeError(w, http.StatusBadRequest, terr)
		return
	}
	if err := a.db.UpdateModel(r.Context(), id, m.ModelID, m.MaxTokens, m.Thinking, m.ThinkingLevels, m.ContextWindow); err != nil {
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
	if _, err := llm.NewForProtocol("", "", "", req.APIType, 0); err != nil {
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
