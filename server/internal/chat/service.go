// Package chat AI 对话（DESIGN 7.3 端到端链路）：模型往返工具循环、
// 写工具产出确认卡片（15 分钟 TTL 单次有效）、SSE 推送、审计与历史。
package chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/bookandmusic/love-life/server/internal/auth"
	"github.com/bookandmusic/love-life/server/internal/crypto"
	"github.com/bookandmusic/love-life/server/internal/llm"
	"github.com/bookandmusic/love-life/server/internal/store"
	"github.com/bookandmusic/love-life/server/internal/textutil"
	"github.com/bookandmusic/love-life/server/internal/tools"
)

const (
	historyWindow = 20 // 送入模型的历史消息条数
	maxToolRounds = 3  // 单次对话最大工具往返
	MaxChatMedia  = 18 // 单条聊天消息媒体引用上限（对齐发动态 18 个/条）
)

type Service struct {
	db   *store.DB
	key  []byte // API Key 加密密钥（派生自数据目录密钥）
	deps *tools.Deps
	reg  map[string]*tools.Tool
}

func NewService(db *store.DB, seed []byte, deps *tools.Deps) *Service {
	reg := map[string]*tools.Tool{}
	for _, t := range tools.Registry() {
		reg[t.Name] = t
	}
	return &Service{db: db, key: crypto.KeyFrom(seed), deps: deps, reg: reg}
}

// SystemPromptTrunkFallback 主干模板：管理后台未配置 system_prompt 时的默认值
// （与改造前硬编码一致，行为向后兼容）。GET /admin/system-prompt 未配置时同用此值。
const SystemPromptTrunkFallback = "你是情侣生活记录应用的 AI 助手，帮助这对情侣记录生活、查询回忆。"

// systemPrompt 拼装：主干（app_settings system_prompt → fallback）+ 风格片段。
// 用户风格按名字精确匹配风格库；未设置或名字已删 → 全局默认风格；库空 → 无片段。
// 每次调用实时读库——管理后台热切换即生效。
func (s *Service) systemPrompt(ctx context.Context, userStyle string) string {
	trunk := SystemPromptTrunkFallback
	if v, err := s.db.GetSetting(ctx, "system_prompt"); err == nil {
		var saved struct {
			Content string `json:"content"`
		}
		if json.Unmarshal([]byte(v), &saved) == nil && saved.Content != "" {
			trunk = saved.Content
		}
	}

	styles, err := s.db.ListStyles(ctx)
	if err != nil || len(styles) == 0 {
		return trunk
	}
	defID, err := s.db.DefaultStyleID(ctx)
	if err != nil {
		defID = 0
	}
	prompt := ""
	if userStyle != "" {
		for _, st := range styles {
			if st.Name == userStyle {
				prompt = st.Prompt
				break
			}
		}
	}
	if prompt == "" {
		for _, st := range styles {
			if st.ID == defID {
				prompt = st.Prompt
				break
			}
		}
	}
	if prompt == "" {
		return trunk
	}
	return trunk + "输出风格偏好：" + prompt
}

// chatTarget 一次对话可用的供应商与模型（统计落库需要 provider_id 与 model 名称）。
type chatTarget struct {
	Client      *llm.Client
	ProviderID  int64
	Model       string
	Temperature *float64 // 模型级采样参数（provider_models 条目），nil = 调用不传
	MaxTokens   *int64
}

// chatTargets 构建本条消息的供应商链（DESIGN §4.1 有序故障切换链）：已配 Key 的供应商
// 按链序排列；解 Key 失败留痕后跳过（全部因此不可用 → errKeyDecrypt）；无模型条目的跳过；
// 链空 → errNoModel。每次调用实时读库——管理后台热切换即生效。
func (s *Service) chatTargets(ctx context.Context) ([]*chatTarget, error) {
	providers, err := s.db.ListChatProviders(ctx)
	if err != nil {
		return nil, err
	}
	var targets []*chatTarget
	decryptFailed := 0
	for _, p := range providers {
		apiKey, derr := crypto.Decrypt(s.key, p.APIKeyEnc)
		if derr != nil {
			decryptFailed++
			slog.Warn("模型 Key 解密失败，跳过该供应商", "provider_id", p.ID, "err", derr)
			continue
		}
		model := p.DefaultModel
		if model == "" && len(p.Models) > 0 {
			model = p.Models[0].ModelID
		}
		if model == "" {
			continue
		}
		client, cerr := llm.NewForProtocol(p.BaseURL, apiKey, model, p.APIType)
		if cerr != nil {
			// api_type 由 DB CHECK 约束为四协议，正常到不了这里；上抛而非静默跳过
			return nil, fmt.Errorf("不支持的服务协议: %w", cerr)
		}
		t := &chatTarget{Client: client, ProviderID: p.ID, Model: model}
		for _, m := range p.Models {
			if m.ModelID == model {
				if m.Temperature.Valid {
					v := m.Temperature.Float64
					t.Temperature = &v
				}
				if m.MaxTokens.Valid {
					v := m.MaxTokens.Int64
					t.MaxTokens = &v
				}
				break
			}
		}
		targets = append(targets, t)
	}
	if len(targets) == 0 {
		if decryptFailed > 0 && decryptFailed == len(providers) {
			return nil, errKeyDecrypt
		}
		return nil, errNoModel
	}
	return targets, nil
}

var errNoModel = errors.New("未配置模型——请管理员在后台配置模型服务")

// errKeyDecrypt 链上全部供应商的 Key 都解密失败（数据问题，非「未配置」）。
var errKeyDecrypt = errors.New("模型 Key 解密失败，请管理员检查配置")

// chatCall 一次模型调用沿链执行：链头起首个成功者胜出；失败者记 failed 统计后淘汰，
// 成功者记 success 并固定为后续轮次起点（回到链首）。模型级参数随目标供应商注入
// （default_model 可手填兜底名、查不到参数条目按未设置处理）。链耗尽返回最后一个错误
// 与空链；返回的链供同一条消息的后续轮次续用（不跨请求）。
func (s *Service) chatCall(ctx context.Context, chain []*chatTarget, req *llm.ChatRequest) (*llm.ChatResponse, []*chatTarget, error) {
	var lastErr error
	for len(chain) > 0 {
		t := chain[0]
		chain = chain[1:]
		req.Temperature = t.Temperature
		req.MaxTokens = t.MaxTokens
		startedAt := time.Now()
		res, err := t.Client.Chat(ctx, req)
		elapsed := time.Since(startedAt).Milliseconds()
		if err == nil {
			s.recordUsage(ctx, t, res, elapsed, "success")
			return res, append([]*chatTarget{t}, chain...), nil
		}
		s.recordUsage(ctx, t, nil, elapsed, "failed")
		lastErr = err
	}
	return nil, nil, lastErr
}

// recordUsage 落库一次模型调用的统计。失败不阻断对话——统计是旁路，
// 但会尽力写入，避免仪表盘长期缺口。
func (s *Service) recordUsage(ctx context.Context, t *chatTarget, res *llm.ChatResponse, latencyMs int64, status string) {
	if t == nil {
		return
	}
	log := &store.AIUsageLog{
		ProviderID: t.ProviderID,
		Model:      t.Model,
		LatencyMs:  latencyMs,
		Status:     status,
	}
	if res != nil {
		log.PromptTokens = res.Usage.PromptTokens
		log.CompletionTokens = res.Usage.CompletionTokens
		log.TotalTokens = res.Usage.TotalTokens
	}
	_ = s.db.InsertAIUsage(ctx, log)
}

// Status GET /api/v1/chat/status 返回 AI 可用性（需求 §3.2：无模型 Key 时 APP 置灰并提示）。
func (s *Service) Status(w http.ResponseWriter, r *http.Request) {
	available := true
	reason := ""
	p, err := s.db.GetChatProvider(r.Context())
	switch {
	case errors.Is(err, store.ErrNotFound):
		available = false
		reason = "未配置模型——请管理员在后台配置模型服务"
	case err != nil:
		available = false
		reason = "模型配置读取失败"
	default:
		if _, derr := crypto.Decrypt(s.key, p.APIKeyEnc); derr != nil {
			available = false
			reason = "模型 Key 解密失败，请管理员检查配置"
		} else if p.DefaultModel == "" && len(p.Models) == 0 {
			available = false
			reason = "未配置模型——请管理员在后台配置模型服务"
		}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"available": available, "reason": reason})
}

// chatMedia 客户端随消息携带的媒体引用：仅本会话展示 + 历史回显，不落媒体资源；
// 真正落库发生在「确认执行」时客户端先用 /media/upload 换 token 随 confirm 提交。
type chatMedia struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Size    int64  `json:"size"`
	DataURL string `json:"data_url"`
}

// Message POST /api/v1/chat/message {content, media?}，SSE 响应（event: text/card/done/error）。
func (s *Service) Message(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	var req struct {
		Content string      `json:"content"`
		Media   []chatMedia `json:"media"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Content) == "" {
		http.Error(w, `{"error":"内容不能为空"}`, http.StatusBadRequest)
		return
	}
	if len(req.Media) > MaxChatMedia {
		http.Error(w, fmt.Sprintf(`{"error":"单条消息最多 %d 个媒体引用"}`, MaxChatMedia), http.StatusBadRequest)
		return
	}
	for _, m := range req.Media {
		if !strings.HasPrefix(m.Type, "image/") && !strings.HasPrefix(m.Type, "video/") && !strings.HasPrefix(m.Type, "audio/") {
			http.Error(w, `{"error":"媒体类型仅支持图片/视频/音频"}`, http.StatusBadRequest)
			return
		}
	}
	mediaJSON, _ := json.Marshal(req.Media)
	chain, err := s.chatTargets(r.Context())
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errNoModel) {
			status = http.StatusLocked
		}
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), status)
		return
	}

	// 历史 + 用户风格
	user, _ := s.db.GetUserByID(r.Context(), claims.UserID)
	msgs := []llm.Message{{Role: "system", Content: s.systemPrompt(r.Context(), userStyle(user))}}
	history, _ := s.db.ListChatMessages(r.Context(), claims.UserID, historyWindow)
	for _, m := range history {
		if m.Role == "user" || m.Role == "assistant" {
			msgs = append(msgs, llm.Message{Role: m.Role, Content: m.Content})
		}
	}
	modelContent := req.Content
	if len(req.Media) > 0 {
		modelContent += fmt.Sprintf(" [本条消息附带 %d 个媒体附件]", len(req.Media))
	}
	msgs = append(msgs, llm.Message{Role: "user", Content: modelContent})
	if _, err := s.db.SaveChatMessage(r.Context(), &store.ChatMessage{UserID: claims.UserID, Role: "user", Content: req.Content, Media: string(mediaJSON)}); err != nil {
		http.Error(w, `{"error":"服务器错误"}`, http.StatusInternalServerError)
		return
	}

	flusher, ok := s.sseHeader(w)
	if !ok {
		return
	}

	// 工具循环（读工具立即执行回填；写工具止于确认卡片）
	for round := 0; round < maxToolRounds; round++ {
		// 每次模型调用沿链执行：失败自动切换下一供应商重试同轮，链耗尽才报错
		res, rest, err := s.chatCall(r.Context(), chain, &llm.ChatRequest{Messages: msgs, Tools: s.toolSpecs(r.Context())})
		if err != nil {
			s.sseEvent(w, flusher, "error", map[string]string{"message": err.Error()})
			s.sseEvent(w, flusher, "done", nil)
			return
		}
		chain = rest
		choice := res.Choices[0]
		if len(choice.Message.ToolCalls) == 0 {
			text := choice.Message.Content
			_, _ = s.db.SaveChatMessage(r.Context(), &store.ChatMessage{UserID: claims.UserID, Role: "assistant", Content: text})
			s.sseEvent(w, flusher, "text", map[string]string{"content": text})
			break
		}

		msgs = append(msgs, llm.Message{Role: "assistant", Content: choice.Message.Content, ToolCalls: choice.Message.ToolCalls})
		stopForConfirm := false
		for _, call := range choice.Message.ToolCalls {
			result, card, data, err := s.runToolCall(r.Context(), claims.UserID, call)
			if err != nil {
				result = "工具执行失败: " + err.Error()
			}
			if card != nil {
				if len(req.Media) > 0 {
					// 卡片携带本条消息的媒体引用（name/type/size，无 data_url）：供确认界面展示待确认附件（mock 契约）
					if prms, ok := card["params"].(map[string]any); ok {
						refs := make([]map[string]any, 0, len(req.Media))
						for _, m := range req.Media {
							refs = append(refs, map[string]any{"name": m.Name, "type": m.Type, "size": m.Size})
						}
						prms["media_refs"] = refs
					}
				}
				s.sseEvent(w, flusher, "card", card)
				result = "已生成确认卡片，等待用户确认后执行"
				stopForConfirm = true
			} else if ev := s.searchResultFrom(r.Context(), call.Function.Name, call.Function.Arguments, data); ev != nil {
				// 搜索结果卡片（契约 tools/mock/routes.mjs:897-905）：读工具产出转 result 事件内联展示。
				// 空结果同样下发（items=[]），由客户端渲染「没找到」。
				// 仅推卡片 + 一句人话文案；不推「🔎 工具名 → 原始 JSON」（那是内部结构，用户看不懂）。
				s.sseEvent(w, flusher, "text", map[string]string{"content": "🔎 " + ev.Caption})
				s.sseEvent(w, flusher, "result", ev)
			} else {
				s.sseEvent(w, flusher, "text", map[string]string{"content": fmt.Sprintf("🔎 %s → %s", call.Function.Name, result)})
			}
			msgs = append(msgs, llm.Message{Role: "tool", Content: result, ToolCallID: call.ID})
		}
		if stopForConfirm {
			_, _ = s.db.SaveChatMessage(r.Context(), &store.ChatMessage{UserID: claims.UserID, Role: "assistant", Content: "已生成确认卡片，等待确认"})
			break
		}
	}
	s.sseEvent(w, flusher, "done", nil)
}

// searchHitItem 搜索结果卡片条目（契约 routes.mjs:899-903）。
type searchHitItem struct {
	ID       int64  `json:"id"`
	Content  string `json:"content"`
	ThumbURL string `json:"thumb_url"`
}

// searchResultEvent 搜索结果卡片事件体（契约 tools/mock/routes.mjs:897-905）。
// Items 用非 nil 空切片：空结果须序列化为 []，客户端读 items.length，null 会渲染异常。
type searchResultEvent struct {
	Tool  string          `json:"tool"`
	Query string          `json:"query"`
	Items []searchHitItem `json:"items"`
	// Caption 不进事件体（客户端只认 tool/query/items），仅用于日志/文案一致性检查
	Caption string `json:"-"`
}

// searchResultFrom 把 search_posts 的产出转成 result 事件（有命中才有缩略图）。
// 契约：content 截断 60 rune、thumb_url 取该动态首图缩略图（无媒体为空串）。
func (s *Service) searchResultFrom(ctx context.Context, toolName, argsJSON string, data any) *searchResultEvent {
	if toolName != "search_posts" {
		return nil
	}
	hits, ok := data.([]tools.PostHit)
	if !ok {
		return nil
	}
	ids := make([]int64, 0, len(hits))
	for _, h := range hits {
		ids = append(ids, h.ID)
	}
	// 一次批量取媒体；查不到只是没缩略图，不该让整张卡片不出来
	medias, err := s.db.GetMediaByPostIDs(ctx, ids)
	if err != nil {
		slog.Warn("search result media lookup failed", "err", err)
	}
	items := make([]searchHitItem, 0, len(hits))
	for _, h := range hits {
		thumb := ""
		if list := medias[h.ID]; len(list) > 0 && s.deps.MP != nil {
			thumb = s.deps.MP.SignedURL(list[0].ID, "thumb")
		}
		items = append(items, searchHitItem{ID: h.ID, Content: textutil.Trunc(h.Content, 60), ThumbURL: thumb})
	}
	var args map[string]any
	_ = json.Unmarshal([]byte(argsJSON), &args)
	q, _ := args["query"].(string)
	evt := &searchResultEvent{Tool: "search_posts", Query: q, Items: items}
	// 文案照 mock 口径：卡片是主体，配一句人话；空结果由客户端渲染「没找到」
	if len(items) > 0 {
		evt.Caption = fmt.Sprintf("找到 %d 条%s回忆：", len(items), relatedTo(q))
	} else {
		evt.Caption = "没找到" + relatedTo(q) + "回忆。"
	}
	return evt
}

// relatedTo 组装「和「X」相关的」限定语；query 为空时退化为「最近的」。
func relatedTo(q string) string {
	if q == "" {
		return "最近的"
	}
	return "和「" + q + "」相关的"
}

func userStyle(u *store.User) string {
	if u == nil {
		return ""
	}
	return u.AIStyle
}

// runToolCall 读工具立即执行；写工具创建确认卡片并返回卡片数据。
// data 是读工具的结构化产出（ExecResult.Data），供搜索结果卡片等事件取用。
func (s *Service) runToolCall(ctx context.Context, userID int64, call llm.ToolCall) (result string, card map[string]any, data any, err error) {
	tool, ok := s.reg[call.Function.Name]
	if !ok {
		return "未知工具: " + call.Function.Name, nil, nil, nil
	}
	// 后台停用的工具拒绝执行（模型可能凭历史上下文继续调用，
	// 仅从 toolSpecs 摘除不足以拦截）。
	if disabled, derr := s.db.DisabledTools(ctx); derr == nil && disabled[tool.Name] {
		return "该工具已在后台停用: " + tool.Name, nil, nil, nil
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return "", nil, nil, fmt.Errorf("参数解析失败: %w", err)
	}
	if !tool.IsWrite {
		startAt := time.Now()
		out, err := tool.Exec(ctx, s.deps, userID, args)
		latencyMs := time.Since(startAt).Milliseconds()
		if err != nil {
			// 读工具失败同样落审计（failed: 原因），与 confirm / PAT 通道口径一致；
			// 否则「所有工具调用均记审计」在这条路径留空档。
			s.deps.AuditWithLatency(ctx, userID, tool.Name, call.Function.Arguments, tools.AuditFailure(err), latencyMs)
			return "", nil, nil, err
		}
		// result 仅记状态（success/failed，对齐 mock 契约与后台等值判定）；工具输出不回写审计
		s.deps.AuditWithLatency(ctx, userID, tool.Name, call.Function.Arguments, tools.AuditSuccess, latencyMs)
		return out.Content, nil, out.Data, nil
	}
	// 写工具 → 确认卡片（15 分钟 TTL，单次有效）
	params, _ := json.Marshal(args)
	actionID, err := s.db.CreateConfirmAction(ctx, userID, tool.Name, string(params))
	if err != nil {
		return "", nil, nil, err
	}
	_, _ = s.db.SaveChatMessage(ctx, &store.ChatMessage{UserID: userID, Role: "assistant", Content: tool.Description, CardID: sql.NullInt64{Int64: actionID, Valid: actionID > 0}})
	return "", map[string]any{
		"action_id":  actionID,
		"tool":       tool.Name,
		"params":     args,
		"status":     "pending",
		"expires_in": 900,
	}, nil, nil
}

// Confirm POST /api/v1/chat/confirm {action_id, confirmed}。
func (s *Service) Confirm(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	var req struct {
		ActionID    int64    `json:"action_id"`
		Confirmed   bool     `json:"confirmed"`
		MediaTokens []string `json:"media_tokens"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ActionID == 0 {
		http.Error(w, `{"error":"请求格式错误"}`, http.StatusBadRequest)
		return
	}
	action, err := s.db.GetConfirmAction(r.Context(), req.ActionID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && action.UserID != claims.UserID) {
		http.Error(w, `{"error":"确认卡片不存在"}`, http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, `{"error":"服务器错误"}`, http.StatusInternalServerError)
		return
	}

	if !req.Confirmed {
		_ = s.db.ResolveConfirmAction(r.Context(), req.ActionID, "cancelled")
		s.writeJSON(w, http.StatusOK, map[string]any{"status": "cancelled"})
		return
	}

	tool, ok := s.reg[action.ToolName]
	if !ok {
		http.Error(w, `{"error":"未知工具"}`, http.StatusInternalServerError)
		return
	}
	if err := s.db.ResolveConfirmAction(r.Context(), req.ActionID, "confirmed"); err != nil {
		if errors.Is(err, store.ErrConfirmExpired) {
			_ = s.db.ResolveConfirmAction(r.Context(), req.ActionID, "expired")
			s.writeJSON(w, http.StatusGone, map[string]string{"error": "确认卡片已过期（15 分钟），请重新发起"})
			return
		}
		if errors.Is(err, store.ErrConfirmResolved) {
			s.writeJSON(w, http.StatusConflict, map[string]string{"error": "卡片已处理过（单次有效）"})
			return
		}
		http.Error(w, `{"error":"服务器错误"}`, http.StatusInternalServerError)
		return
	}

	var args map[string]any
	_ = json.Unmarshal([]byte(action.ParamsJSON), &args)
	if len(req.MediaTokens) > 0 {
		args["media_tokens"] = req.MediaTokens
	}
	startAt := time.Now()
	out, err := tool.Exec(r.Context(), s.deps, claims.UserID, args)
	latencyMs := time.Since(startAt).Milliseconds()
	if err != nil {
		s.deps.AuditWithLatency(r.Context(), claims.UserID, action.ToolName, action.ParamsJSON,
			tools.AuditFailure(err), latencyMs)
		s.writeJSON(w, http.StatusOK, map[string]any{"status": "failed", "error": err.Error()})
		return
	}
	s.deps.AuditWithLatency(r.Context(), claims.UserID, action.ToolName, action.ParamsJSON, tools.AuditSuccess, latencyMs)
	msg := out.Content
	if msgID, err := s.db.SaveChatMessage(r.Context(), &store.ChatMessage{UserID: claims.UserID, Role: "tool_result", Content: msg, CardID: sql.NullInt64{Int64: req.ActionID, Valid: req.ActionID > 0}}); err == nil {
		_ = msgID
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"status": "executed", "result": msg})
}

// History GET /api/v1/chat/history。
func (s *Service) History(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	list, err := s.db.ListChatMessages(r.Context(), claims.UserID, 100)
	if err != nil {
		http.Error(w, `{"error":"服务器错误"}`, http.StatusInternalServerError)
		return
	}
	type item struct {
		ID        int64           `json:"id"`
		Role      string          `json:"role"`
		Content   string          `json:"content"`
		CardID    int64           `json:"card_id,omitempty"`
		CreatedAt string          `json:"created_at"`
		Media     json.RawMessage `json:"media,omitempty"`
	}
	items := make([]item, 0, len(list))
	for _, m := range list {
		cardID := int64(0)
		if m.CardID.Valid {
			cardID = m.CardID.Int64
		}
		media := json.RawMessage("[]")
		if m.Media != "" {
			media = json.RawMessage(m.Media)
		}
		items = append(items, item{ID: m.ID, Role: m.Role, Content: m.Content, CardID: cardID, CreatedAt: m.CreatedAt, Media: media})
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// toolSpecs 下发给模型的工具清单。后台停用的工具不下发，
// 使「工具启停」真正生效而非仅页面展示。
func (s *Service) toolSpecs(ctx context.Context) []llm.Tool {
	disabled, err := s.db.DisabledTools(ctx)
	if err != nil {
		// 读配置失败按「全部启用」降级，不因配置读取问题阻断对话。
		disabled = nil
	}

	specs := make([]llm.Tool, 0, len(s.reg))
	for _, t := range tools.Registry() {
		if disabled[t.Name] {
			continue
		}
		specs = append(specs, llm.Tool{Type: "function", Function: llm.ToolFunction{
			Name: t.Name, Description: t.Description, Parameters: t.Params,
		}})
	}
	return specs
}

func (s *Service) sseHeader(w http.ResponseWriter) (http.Flusher, bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"error":"streaming unsupported"}`, http.StatusInternalServerError)
		return nil, false
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	return flusher, true
}

func (s *Service) sseEvent(w http.ResponseWriter, f http.Flusher, event string, data any) {
	b, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
	f.Flush()
}

func (s *Service) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
