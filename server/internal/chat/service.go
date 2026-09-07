// Package chat AI 对话（DESIGN 7.3 端到端链路的阶段一）：模型往返工具循环、
// 写工具产出确认卡片（15 分钟 TTL 单次有效）、SSE 推送、审计与历史。
package chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/bookandmusic/love-life/server/internal/auth"
	"github.com/bookandmusic/love-life/server/internal/crypto"
	"github.com/bookandmusic/love-life/server/internal/llm"
	"github.com/bookandmusic/love-life/server/internal/store"
	"github.com/bookandmusic/love-life/server/internal/tools"
)

const (
	historyWindow = 20 // 送入模型的历史消息条数
	maxToolRounds = 3  // 单次对话最大工具往返
	MaxChatMedia   = 18 // 单条聊天消息媒体引用上限（对齐发动态 18 个/条）
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

func (s *Service) systemPrompt(style string) string {
	base := "你是情侣生活记录应用的 AI 助手，帮助这对情侣记录生活、查询回忆。"
	if style != "" {
		base += "输出风格偏好：" + style + "。"
	}
	return base
}

// clientFromConfig 取对话供应商（链头起首个已配 Key 的 openai-chat）并解密 Key。
func (s *Service) clientFromConfig(ctx context.Context) (*llm.Client, error) {
	p, err := s.db.GetChatProvider(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return nil, errNoModel
	}
	if err != nil {
		return nil, err
	}
	apiKey, err := crypto.Decrypt(s.key, p.APIKeyEnc)
	if err != nil {
		return nil, fmt.Errorf("解密模型 Key 失败: %w", err)
	}
	model := p.DefaultModel
	if model == "" && len(p.Models) > 0 {
		model = p.Models[0].ModelID
	}
	if model == "" {
		return nil, errNoModel
	}
	return llm.New(p.BaseURL, apiKey, model), nil
}

var errNoModel = errors.New("未配置模型——请管理员在后台配置模型服务")

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

// chatMedia 客户端随消息携带的媒体引用（D8）：仅本会话展示 + 历史回显，不落媒体资源；
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
	client, err := s.clientFromConfig(r.Context())
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
	msgs := []llm.Message{{Role: "system", Content: s.systemPrompt(userStyle(user))}}
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
		res, err := client.Chat(r.Context(), &llm.ChatRequest{Messages: msgs, Tools: s.toolSpecs()})
		if err != nil {
			s.sseEvent(w, flusher, "error", map[string]string{"message": err.Error()})
			s.sseEvent(w, flusher, "done", nil)
			return
		}
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
			result, card, err := s.runToolCall(r.Context(), claims.UserID, call)
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

func userStyle(u *store.User) string {
	if u == nil {
		return ""
	}
	return u.AIStyle
}

// runToolCall 读工具立即执行；写工具创建确认卡片并返回卡片数据。
func (s *Service) runToolCall(ctx context.Context, userID int64, call llm.ToolCall) (result string, card map[string]any, err error) {
	tool, ok := s.reg[call.Function.Name]
	if !ok {
		return "未知工具: " + call.Function.Name, nil, nil
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return "", nil, fmt.Errorf("参数解析失败: %w", err)
	}
	if !tool.IsWrite {
		out, err := tool.Exec(ctx, s.deps, userID, args)
		if err != nil {
			return "", nil, err
		}
		return out.Content, nil, nil
	}
	// 写工具 → 确认卡片（15 分钟 TTL，单次有效）
	params, _ := json.Marshal(args)
	actionID, err := s.db.CreateConfirmAction(ctx, userID, tool.Name, string(params))
	if err != nil {
		return "", nil, err
	}
	_, _ = s.db.SaveChatMessage(ctx, &store.ChatMessage{UserID: userID, Role: "assistant", Content: tool.Description, CardID: sql.NullInt64{Int64: actionID, Valid: actionID > 0}})
	return "", map[string]any{
		"action_id": actionID,
		"tool":      tool.Name,
		"params":    args,
		"status":    "pending",
		"expires_in": 900,
	}, nil
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
	out, err := tool.Exec(r.Context(), s.deps, claims.UserID, args)
	if err != nil {
		_ = s.db.InsertAuditLog(r.Context(), "ai", claims.UserID, action.ToolName, action.ParamsJSON, "failed: "+err.Error())
		s.writeJSON(w, http.StatusOK, map[string]any{"status": "failed", "error": err.Error()})
		return
	}
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

func (s *Service) toolSpecs() []llm.Tool {
	specs := make([]llm.Tool, 0, len(s.reg))
	for _, t := range tools.Registry() {
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