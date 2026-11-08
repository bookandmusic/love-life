// Package mcp 外部 MCP 服务通道：单实例双通道中的外部通道，
// 用 PAT 认证、按档位过滤工具、Streamable HTTP 传输（唯一传输）。
//
// 与内部通道（APP Agent，走 /api/v1/chat/，JWT + 确认卡片）共用同一套工具注册表
// 与业务逻辑；差异在鉴权与工具可见范围：
// - AppOnly 工具（删除类）不对任何令牌开放（ 铁律 / ）
//   - readonly 档只见读工具；readwrite 档见读 + 写工具
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/bookandmusic/love-life/server/internal/store"
	"github.com/bookandmusic/love-life/server/internal/tools"
)

// 协议版本（MCP Streamable HTTP）。客户端请求的版本不受支持时回落到本常量。
const protocolVersion = "2025-06-18"

// Service 外部 MCP 通道。
type Service struct {
	db   *store.DB
	deps *tools.Deps
}

func NewService(db *store.DB, deps *tools.Deps) *Service {
	return &Service{db: db, deps: deps}
}

// rpcRequest JSON-RPC 2.0 请求。
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// rpcError JSON-RPC 错误对象。
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// rpcResponse JSON-RPC 响应（result 与 error 互斥）。
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// tokenFromHeader 从 Authorization: Bearer <PAT> 取明文令牌。
func tokenFromHeader(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const p = "Bearer "
	if len(h) > len(p) && strings.EqualFold(h[:len(p)], p) {
		return h[len(p):]
	}
	return ""
}

// Handle POST /api/v1/mcp —— Streamable HTTP 单一入口。
// 未携带合法 PAT → 401；协议方法见 handleRPC。
func (s *Service) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeRPCError(w, http.StatusMethodNotAllowed, nil, -32600, "仅支持 POST（Streamable HTTP）")
		return
	}

	plain := tokenFromHeader(r)
	if plain == "" {
		writeRPCError(w, http.StatusUnauthorized, nil, -32001, "缺少 PAT：请携带 Authorization: Bearer <token>")
		return
	}
	pat, err := s.db.AuthenticatePAT(r.Context(), plain)
	if err != nil {
		writeRPCError(w, http.StatusUnauthorized, nil, -32001, "PAT 无效或已吊销")
		return
	}

	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRPCError(w, http.StatusBadRequest, nil, -32700, "JSON 解析失败")
		return
	}

	// 通知类消息（无 id）按 JSON-RPC 约定不返回响应体。
	if len(req.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	switch req.Method {
	case "initialize":
		s.writeResult(w, req.ID, map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "love-life", "version": "1.0.0"},
		})
	case "tools/list":
		list, err := s.toolList(r.Context(), pat)
		if err != nil {
			// fail-closed：读不到启停状态就不下发清单——按旧集合下发等于放行已停用的工具
			writeRPCError(w, http.StatusOK, req.ID, -32603, "工具清单暂不可用，请稍后重试")
			return
		}
		s.writeResult(w, req.ID, map[string]any{"tools": list})
	case "tools/call":
		s.handleCall(w, r, req, pat)
	case "ping":
		s.writeResult(w, req.ID, map[string]any{})
	default:
		writeRPCError(w, http.StatusOK, req.ID, -32601, "不支持的方法: "+req.Method)
	}
}

// allowed 按档位判定工具可见性。
//
// 「某工具是否对 PAT 开放」的唯一事实源是 tools.RegistryForPAT()（删除类 AppOnly 不开放），
// 本包只在其上再收窄一档：readonly 只见读工具。
func allowed(t *tools.Tool, tier string) bool {
	if !patVisible(t.Name) {
		return false
	}
	if tier == "readwrite" {
		return true
	}
	return !t.IsWrite
}

// patVisible 工具是否在 PAT 可见集合内（按名字比对：Registry 每次调用都新建工具值，指针不可比）。
func patVisible(name string) bool {
	for _, t := range tools.RegistryForPAT() {
		if t.Name == name {
			return true
		}
	}
	return false
}

// disabledTools 读后台停用的工具集合；读失败即返回错误，由调用方 fail-closed 处理
// （「读不到」不等于「没有工具被停用」，猜错就是放行已停用的工具）。
func (s *Service) disabledTools(ctx context.Context) (map[string]bool, error) {
	disabled, err := s.db.DisabledTools(ctx)
	if err != nil {
		slog.Error("读取工具启停状态失败", "err", err)
		return nil, err
	}
	return disabled, nil
}

// toolErrorText 工具执行错误的对外文案：只按错误类型回人话分类，绝不拼 err.Error()。
//
// 外部 MCP 客户端是第三方 Agent，其对话可能展示给用户；原始错误里常带 SQL、表名、
// 内部路径与实现细节。口径与内部通道的模型错误分类一致：类型分类给人，原文进日志。
func toolErrorText(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "请求已取消，工具未执行"
	case errors.Is(err, context.DeadlineExceeded):
		return "工具执行超时，请稍后重试"
	default:
		return "工具执行失败，请稍后重试"
	}
}

// toolList 下发工具清单（MCP tools/list 形状：name/description/inputSchema）。
// 后台停用的工具不下发；启停状态读不到时返回错误（fail-closed，见 disabledTools）。
func (s *Service) toolList(ctx context.Context, pat *store.PersonalAccessToken) ([]map[string]any, error) {
	disabled, err := s.disabledTools(ctx)
	if err != nil {
		return nil, err
	}
	reg := tools.Registry()
	out := make([]map[string]any, 0, len(reg))
	for _, t := range reg {
		if !allowed(t, pat.Tier) || disabled[t.Name] {
			continue
		}
		out = append(out, map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": t.Params,
			"annotations": toolAnnotations(t),
		})
	}
	return out, nil
}

// toolAnnotations 派生 MCP 工具注解。
// 只发三列（readOnlyHint / destructiveHint / openWorldHint）：这正是对外集成
// 「工具注解」表要求的全部列。**不发 idempotentHint**——它对所有工具恒为 true，
// 而 propose_post 这类「新建」并不幂等，写死 true 是失实声明；留给客户端按规范默认处理。
// 本服务工具全部操作本地封闭数据集，openWorldHint 固定 false（覆盖规范默认 true）。
func toolAnnotations(t *tools.Tool) map[string]any {
	return map[string]any{
		"readOnlyHint":    !t.IsWrite,
		"destructiveHint": t.IsWrite && t.AppOnly,
		"openWorldHint":   false,
	}
}

// handleCall 执行 tools/call。审计记 actor_type=pat、actor_id=令牌 ID。
func (s *Service) handleCall(w http.ResponseWriter, r *http.Request, req rpcRequest, pat *store.PersonalAccessToken) {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil || params.Name == "" {
		writeRPCError(w, http.StatusOK, req.ID, -32602, "参数错误：需要 name 与 arguments")
		return
	}

	var target *tools.Tool
	for _, t := range tools.Registry() {
		if t.Name == params.Name {
			target = t
			break
		}
	}
	if target == nil {
		writeRPCError(w, http.StatusOK, req.ID, -32602, "未知工具: "+params.Name)
		return
	}
	// 档位与启停校验：不可见即不可调用（不能靠客户端自觉）。
	if !allowed(target, pat.Tier) {
		writeRPCError(w, http.StatusOK, req.ID, -32003,
			fmt.Sprintf("当前令牌档位（%s）不可调用工具 %s", pat.Tier, params.Name))
		return
	}
	// fail-closed：启停状态读失败即按「工具不可用」拒绝，不放行任何一次调用
	disabled, derr := s.disabledTools(r.Context())
	if derr != nil {
		writeRPCError(w, http.StatusOK, req.ID, -32003, "工具暂不可用，请稍后重试")
		return
	}
	if disabled[target.Name] {
		writeRPCError(w, http.StatusOK, req.ID, -32003, "该工具已在后台停用: "+params.Name)
		return
	}

	args := params.Arguments
	if args == nil {
		args = map[string]any{}
	}

	// 审计：params 存人类可读摘要、digest 存原始 JSON，latency_ms 记本次调用耗时
	digest, _ := json.Marshal(args)
	paramsText := tools.FormatParams(args)
	started := nowMillis()
	// 外部通道一律直接执行（提案类不走确认卡）：MCP 规范的 User Interaction Model 由
	// 客户端（外部 Agent）负责二次确认，服务端通过 tools/list 注解声明风险属性。
	out, err := target.Invoke(r.Context(), s.deps, pat.UserID, args, tools.ModeExecute)
	elapsed := nowMillis() - started

	if err != nil {
		// 原文只进日志：外部客户端拿到的是人话分类（见 toolErrorText）
		slog.Error("MCP 工具执行失败", "tool", target.Name, "pat_id", pat.ID, "user_id", pat.UserID, "err", err)
		s.audit(r.Context(), pat, target.Name, string(digest), paramsText, tools.AuditFailure(err), &elapsed)
		s.writeResult(w, req.ID, map[string]any{
			"content": []map[string]any{{"type": "text", "text": toolErrorText(err)}},
			"isError": true,
		})
		return
	}

	s.audit(r.Context(), pat, target.Name, string(digest), paramsText, tools.AuditSuccess, &elapsed)
	// 给外部 Agent 的文本 = Text（含 id 与结论，便于它接力）；Data 载荷不出通道
	s.writeResult(w, req.ID, map[string]any{
		"content": []map[string]any{{"type": "text", "text": out.Text}},
	})
}

// audit 记一次外部通道调用。写失败只告警：审计落库失败不该把调用结果一并吞掉，
// 但也绝不能静默——否则「调用过但查不到记录」在事后无从分辨。
func (s *Service) audit(ctx context.Context, pat *store.PersonalAccessToken, tool, digest, paramsText, result string, latency *int64) {
	if err := s.db.InsertAuditLogFull(ctx, store.ActorTypePAT, pat.ID, tool, digest, paramsText, result, latency); err != nil {
		slog.Warn("MCP 审计写入失败", "tool", tool, "pat_id", pat.ID, "err", err)
	}
}

func (s *Service) writeResult(w http.ResponseWriter, id json.RawMessage, result any) {
	writeJSON(w, http.StatusOK, rpcResponse{JSONRPC: "2.0", ID: id, Result: result})
}

func writeRPCError(w http.ResponseWriter, status int, id json.RawMessage, code int, msg string) {
	writeJSON(w, status, rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// nowMillis 当前毫秒时间戳（审计耗时用）。
func nowMillis() int64 { return time.Now().UnixMilli() }
