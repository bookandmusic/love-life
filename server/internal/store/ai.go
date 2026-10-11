package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Confirm 卡片流转 sentinel：Confirm 侧按 errors.Is 判断过期/重复，不匹配错误文案。
var (
	ErrConfirmExpired  = errors.New("confirm action expired")
	ErrConfirmResolved = errors.New("confirm action already resolved")
)

// ---- 对话历史 ----

type ChatMessage struct {
	ID        int64
	UserID    int64
	SessionID int64  // 归属会话（多会话）；0 仅存在于 schema 兜底，正常写入恒为真实会话
	TurnID    string // 归属对话轮次；按轮回滚的依据，同一轮所有行（含确认回执）共享
	Role      string // 收窄为 user / assistant；工具步骤、思考链、卡片全在 parts 里
	Parts     string // 有序 JSON 数组：[{type:text|thinking|tool|card|attachment|quote|interrupted,...}]
	CardID    sql.NullInt64
	CreatedAt string
	Quoted    string // JSON 用户引用的资源：引用条随消息回显，仅限当前会话
}

// SaveChatMessage 保存一条聊天消息（m.CardID 为 NULL 时落 NULL；m.Parts 为有序 JSON 数组）。
func (d *DB) SaveChatMessage(ctx context.Context, m *ChatMessage) (int64, error) {
	var card any
	if m.CardID.Valid && m.CardID.Int64 > 0 {
		card = m.CardID.Int64
	}
	res, err := d.ExecContext(ctx,
		`INSERT INTO chat_messages (user_id, session_id, turn_id, role, parts, card_id, quoted) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		m.UserID, m.SessionID, m.TurnID, m.Role, m.Parts, card, m.Quoted)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) ListChatMessages(ctx context.Context, userID, sessionID, limit int64) ([]*ChatMessage, error) {
	rows, err := d.QueryContext(ctx, `SELECT id, user_id, role, parts, card_id, created_at, quoted, turn_id
		FROM chat_messages WHERE user_id = ? AND session_id = ? ORDER BY id DESC LIMIT ?`, userID, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*ChatMessage
	for rows.Next() {
		var m ChatMessage
		if err := rows.Scan(&m.ID, &m.UserID, &m.Role, &m.Parts, &m.CardID, &m.CreatedAt, &m.Quoted, &m.TurnID); err != nil {
			return nil, err
		}
		list = append(list, &m)
	}
	// 倒序取出后翻回时间正序
	for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
		list[i], list[j] = list[j], list[i]
	}
	return list, nil
}

// AbortTurn 整轮回滚（替代 withdraw-last）：删除该轮已产生的全部消息，并把该轮
// 尚未裁决的确认卡置 cancelled（卡片动作行保留为 cancelled 供审计追溯「AI 提议过但被中止」）。
// 只按 (user_id, turn_id) 定位：turn_id 每轮由服务端生成，精确到轮，不会误删下一轮——
// 而旧的「删最后一条 user 消息」在多轮交错与并发下发下都会定位错行。
// 返回是否真的删到了东西（false = turn_id 为空、不存在或不属于该用户）。
func (d *DB) AbortTurn(ctx context.Context, userID int64, turnID string) (bool, error) {
	if turnID == "" {
		return false, nil
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE confirm_actions SET status = 'cancelled', resolved_at = datetime('now')
		WHERE status = 'pending' AND id IN (
			SELECT card_id FROM chat_messages WHERE user_id = ? AND turn_id = ? AND card_id IS NOT NULL)`,
		userID, turnID); err != nil {
		_ = tx.Rollback()
		return false, err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM chat_messages WHERE user_id = ? AND turn_id = ?`, userID, turnID)
	if err != nil {
		_ = tx.Rollback()
		return false, err
	}
	// 模型上下文同轮回滚：该轮 user/read_result 行一并删除（proposal 已裁决行为属历史，
	// 由 AbortTurn 的 pending 卡取消语义处理；未写 context 的 pending 不存在——resolve 才写入）
	if _, err := tx.ExecContext(ctx, `DELETE FROM chat_context WHERE user_id = ? AND turn_id = ?`, userID, turnID); err != nil {
		_ = tx.Rollback()
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		_ = tx.Rollback()
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return n > 0, nil
}

// TurnIDsOfCard 某张确认卡的载体消息所属轮次（可能多行同卡，取全部）。
// 确认回执需落到同一 turn_id，才能让「后来的 abort」连回执一起回滚，不致留下孤儿 tool_result。
func (d *DB) TurnIDsOfCard(ctx context.Context, cardID int64) ([]string, error) {
	rows, err := d.QueryContext(ctx, `SELECT DISTINCT turn_id FROM chat_messages WHERE card_id = ? AND turn_id <> ''`, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ---- 会话（多会话） ----

type ChatSession struct {
	ID        int64
	UserID    int64
	Title     string
	CreatedAt string
	UpdatedAt string
}

func (d *DB) ListChatSessions(ctx context.Context, userID int64) ([]*ChatSession, error) {
	rows, err := d.QueryContext(ctx, `SELECT id, user_id, title, created_at, updated_at
		FROM chat_sessions WHERE user_id = ? ORDER BY updated_at DESC, id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*ChatSession
	for rows.Next() {
		var s ChatSession
		if err := rows.Scan(&s.ID, &s.UserID, &s.Title, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		list = append(list, &s)
	}
	return list, rows.Err()
}

func (d *DB) CreateChatSession(ctx context.Context, userID int64, title string) (int64, error) {
	res, err := d.ExecContext(ctx, `INSERT INTO chat_sessions (user_id, title) VALUES (?, ?)`, userID, title)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) GetChatSession(ctx context.Context, id int64) (*ChatSession, error) {
	row := d.QueryRowContext(ctx, `SELECT id, user_id, title, created_at, updated_at FROM chat_sessions WHERE id = ?`, id)
	var s ChatSession
	if err := row.Scan(&s.ID, &s.UserID, &s.Title, &s.CreatedAt, &s.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &s, nil
}

// TouchChatSession 推进会话活跃时间（每条消息落库后调用，抽屉按 updated_at 倒序）。
func (d *DB) TouchChatSession(ctx context.Context, id int64) error {
	_, err := d.ExecContext(ctx, `UPDATE chat_sessions SET updated_at = datetime('now') WHERE id = ?`, id)
	return err
}

// DeleteChatSession 硬删（已定：聊天为过程性数据，不进回收站）：事务内先删消息与结果集，再删会话行。
// 返回是否真的删了（false = 不存在或非本人会话）。
// DeleteChatSession 硬删：会话 + 消息 + 上下文 + 结果集事务同删（聊天为过程性数据，
// 不进回收站；提案行无 session_id，经 chat_messages.card_id 关联随消息同删不留孤儿）。
func (d *DB) DeleteChatSession(ctx context.Context, userID, sessionID int64) (bool, error) {
	tx, err := d.Begin()
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(`DELETE FROM chat_messages WHERE user_id = ? AND session_id = ?`, userID, sessionID); err != nil {
		_ = tx.Rollback()
		return false, err
	}
	// 模型上下文绑会话：随会话硬删同删（ARCHITECTURE「数据模型」关系要点）
	if _, err := tx.Exec(`DELETE FROM chat_context WHERE session_id = ?`, sessionID); err != nil {
		_ = tx.Rollback()
		return false, err
	}
	// 结果集句柄绑会话：随会话硬删同删，不留孤儿
	if _, err := tx.Exec(`DELETE FROM chat_result_sets WHERE session_id = ?`, sessionID); err != nil {
		_ = tx.Rollback()
		return false, err
	}
	// 确认卡：无 session_id，经消息载体关联同删（含 pending——会话没了卡即无从裁决）
	if _, err := tx.Exec(`DELETE FROM confirm_actions WHERE user_id = ? AND id IN
		(SELECT card_id FROM chat_messages WHERE user_id = ? AND session_id = ? AND card_id IS NOT NULL)`, userID, userID, sessionID); err != nil {
		_ = tx.Rollback()
		return false, err
	}
	res, err := tx.Exec(`DELETE FROM chat_sessions WHERE id = ? AND user_id = ?`, sessionID, userID)
	if err != nil {
		_ = tx.Rollback()
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		_ = tx.Rollback()
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return n > 0, nil
}

// CountPendingProposals 会话内未过期待确认提案数（删会话守卫用）。
func (d *DB) CountPendingProposals(ctx context.Context, userID, sessionID int64) (int64, error) {
	var n int64
	err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM confirm_actions ca
		JOIN chat_messages m ON m.card_id = ca.id
		WHERE ca.user_id = ? AND m.session_id = ? AND ca.status = 'pending' AND datetime(ca.expires_at) > datetime('now')`,
		userID, sessionID).Scan(&n)
	return n, err
}

// SessionIDOfCard 卡片载体消息所在的会话（Confirm 回执对齐落库用）；无载体返回 0。
func (d *DB) SessionIDOfCard(ctx context.Context, cardID int64) (int64, error) {
	var id int64
	err := d.QueryRowContext(ctx, `SELECT session_id FROM chat_messages WHERE card_id = ? ORDER BY id DESC LIMIT 1`, cardID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return id, nil
}

// ---- 确认卡片 ----

type ConfirmAction struct {
	ID          int64
	UserID      int64
	ToolName    string
	ParamsJSON  string
	PreviewJSON string // 提案自构造预览（工具 Exec 内生成并落库）；历史回显直接读，不反推
	Status      string
	ExpiresAt   time.Time
	ResolvedAt  sql.NullString
	CreatedAt   string
}

func (d *DB) CreateConfirmAction(ctx context.Context, userID int64, toolName, paramsJSON, previewJSON string) (int64, error) {
	res, err := d.ExecContext(ctx,
		`INSERT INTO confirm_actions (user_id, tool_name, params_json, preview_json, expires_at) VALUES (?, ?, ?, ?, ?)`,
		userID, toolName, paramsJSON, previewJSON, FormatDBTime(time.Now().Add(15*time.Minute)))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) GetConfirmAction(ctx context.Context, id int64) (*ConfirmAction, error) {
	row := d.QueryRowContext(ctx, `SELECT id, user_id, tool_name, params_json, preview_json, status, expires_at, resolved_at, created_at
		FROM confirm_actions WHERE id = ?`, id)
	var a ConfirmAction
	var expires string
	err := row.Scan(&a.ID, &a.UserID, &a.ToolName, &a.ParamsJSON, &a.PreviewJSON, &a.Status, &expires, &a.ResolvedAt, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if a.ExpiresAt, err = ParseDBTime(expires); err != nil {
		return nil, err
	}
	return &a, nil
}

// ListConfirmActionsByIDs 批量按 ID 查确认卡片（chat history 补 card 快照用）。
// 不存在的 ID 跳过；返回 map[id]*ConfirmAction。
func (d *DB) ListConfirmActionsByIDs(ctx context.Context, ids []int64) (map[int64]*ConfirmAction, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	query := "SELECT id, user_id, tool_name, params_json, preview_json, status, expires_at, resolved_at, created_at FROM confirm_actions WHERE id IN (" +
		strings.Join(placeholders, ",") + ")"
	rows, err := d.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[int64]*ConfirmAction, len(ids))
	for rows.Next() {
		var a ConfirmAction
		var expires string
		if err := rows.Scan(&a.ID, &a.UserID, &a.ToolName, &a.ParamsJSON, &a.PreviewJSON, &a.Status, &expires, &a.ResolvedAt, &a.CreatedAt); err != nil {
			return nil, err
		}
		if a.ExpiresAt, err = ParseDBTime(expires); err != nil {
			return nil, err
		}
		result[a.ID] = &a
	}
	return result, rows.Err()
}

// ResolveConfirmAction 单次有效：仅 pending 且未过期可流转；过期置 expired。
// failed 用于「已确认但工具执行失败」的回写，避免失败卡片在历史里被当作已执行回显。
// UpdateConfirmStatus 直接置状态（过期前置裁决用；不改 resolved_at 语义外的时间戳）。
func (d *DB) UpdateConfirmStatus(ctx context.Context, id int64, to string) error {
	_, err := d.ExecContext(ctx, `UPDATE confirm_actions SET status = ? WHERE id = ?`, to, id)
	return err
}

func (d *DB) ResolveConfirmAction(ctx context.Context, id int64, to string) error {
	allowed := map[string]bool{"confirmed": true, "cancelled": true, "expired": true, "failed": true}
	if !allowed[to] {
		return errors.New("invalid status")
	}
	a, err := d.GetConfirmAction(ctx, id)
	if err != nil {
		return err
	}
	if a.Status != "pending" {
		// confirmed→failed 回写（2026-09-21）：确认已消耗，仅纠正终态——执行失败的历史回显不能是「已执行」
		if a.Status == "confirmed" && to == "failed" {
			_, err = d.ExecContext(ctx, `UPDATE confirm_actions SET status='failed', resolved_at=? WHERE id=? AND status='confirmed'`,
				FormatDBTime(time.Now()), id)
			return err
		}
		return ErrConfirmResolved
	}
	if time.Now().After(a.ExpiresAt) && to != "expired" {
		_, _ = d.ExecContext(ctx, `UPDATE confirm_actions SET status='expired', resolved_at=? WHERE id=?`,
			FormatDBTime(time.Now()), id)
		return ErrConfirmExpired
	}
	_, err = d.ExecContext(ctx, `UPDATE confirm_actions SET status=?, resolved_at=? WHERE id=? AND status='pending'`,
		to, FormatDBTime(time.Now()), id)
	return err
}

// ---- 结果集句柄 ----

// ResultSet 读工具产出的结果集：ids 为完整 id 列表；groups 为分组结果（find_media group_by）。
// 提案工具以 source={result_id, group?} 引用，出提案时即展开为具体 id，提案自此自包含。
type ResultSet struct {
	ResultID string
	Kind     string // post / media
	IDs      []int64
	Groups   []ResultGroup
}

type ResultGroup struct {
	Key   string  `json:"key"`
	Label string  `json:"label"`
	IDs   []int64 `json:"ids"`
}

// SaveResultSet 存一组结果并返回 result_id（r_<行id>）。绑定 session_id：引用校验与随会话清理都靠它。
func (d *DB) SaveResultSet(ctx context.Context, sessionID int64, kind string, ids []int64, groups []ResultGroup) (string, error) {
	idsJSON, err := json.Marshal(ids)
	if err != nil {
		return "", err
	}
	groupsJSON := ""
	if len(groups) > 0 {
		if b, gerr := json.Marshal(groups); gerr == nil {
			groupsJSON = string(b)
		}
	}
	res, err := d.ExecContext(ctx,
		`INSERT INTO chat_result_sets (session_id, result_id, kind, ids_json, groups_json) VALUES (?, '', ?, ?, ?)`,
		sessionID, kind, string(idsJSON), groupsJSON)
	if err != nil {
		return "", err
	}
	rowID, err := res.LastInsertId()
	if err != nil {
		return "", err
	}
	resultID := fmt.Sprintf("r_%d", rowID)
	_, err = d.ExecContext(ctx, `UPDATE chat_result_sets SET result_id = ? WHERE id = ?`, resultID, rowID)
	if err != nil {
		return "", err
	}
	return resultID, nil
}

// GetResultSet 按 result_id 取结果集（引用归属校验由调用方比对 session_id）。
func (d *DB) GetResultSet(ctx context.Context, sessionID int64, resultID string) (*ResultSet, error) {
	row := d.QueryRowContext(ctx,
		`SELECT result_id, kind, ids_json, groups_json FROM chat_result_sets WHERE session_id = ? AND result_id = ?`,
		sessionID, resultID)
	var rs ResultSet
	var idsJSON, groupsJSON string
	if err := row.Scan(&rs.ResultID, &rs.Kind, &idsJSON, &groupsJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := json.Unmarshal([]byte(idsJSON), &rs.IDs); err != nil {
		return nil, err
	}
	if groupsJSON != "" {
		_ = json.Unmarshal([]byte(groupsJSON), &rs.Groups)
	}
	return &rs, nil
}

// ---- 上下文组装与会话范围查询（模型上下文组装用） ----

// CardCarrier 卡片载体消息行（会话内所有带 card_id 的消息，按 id 升序）。
type CardCarrier struct {
	RowID  int64
	CardID int64
}

// CardCarriers 会话内全部卡片载体行（不受条数窗口限制）：已裁决提案的回放要跨窗口稳定存在。
func (d *DB) CardCarriers(ctx context.Context, userID, sessionID int64) ([]CardCarrier, error) {
	// 载体行 = 带 card_id 的 assistant 行；同一 card_id 可能有多行（提案载体 + 裁决回执），
	// 按 card_id 去重取最早一行——否则同一张卡会在上下文里回放两次。
	rows, err := d.QueryContext(ctx, `SELECT MIN(id), card_id FROM chat_messages
		WHERE user_id = ? AND session_id = ? AND card_id IS NOT NULL AND role = 'assistant'
		GROUP BY card_id ORDER BY MIN(id) ASC`, userID, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CardCarrier
	for rows.Next() {
		var c CardCarrier
		if err := rows.Scan(&c.RowID, &c.CardID); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CardResults 各卡片最新一条执行回执（tool_result 行内容）；无回执不在返回中。
func (d *DB) CardResults(ctx context.Context, cardIDs []int64) (map[int64]string, error) {
	out := map[int64]string{}
	if len(cardIDs) == 0 {
		return out, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(cardIDs)), ",")
	args := make([]any, 0, len(cardIDs))
	for _, id := range cardIDs {
		args = append(args, id)
	}
	rows, err := d.QueryContext(ctx, `SELECT id, card_id, parts FROM chat_messages
		WHERE card_id IN (`+ph+`) ORDER BY id ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, cardID int64
		var parts string
		if err := rows.Scan(&id, &cardID, &parts); err != nil {
			return nil, err
		}
		// 裁决回执 = 行内 parts 的 tool part 的 text（升序遍历，后者覆盖前者）
		var mp []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal([]byte(parts), &mp) == nil {
			for _, part := range mp {
				if part.Type == "tool" && part.Text != "" {
					out[cardID] = part.Text
				}
			}
		}
	}
	return out, rows.Err()
}

// ResultSetIDsByKind 本会话某类资源的全部结果集 id 并集（quoted 引用归属校验用）。
func (d *DB) ResultSetIDsByKind(ctx context.Context, sessionID int64, kind string) (map[int64]bool, error) {
	rows, err := d.QueryContext(ctx, `SELECT ids_json FROM chat_result_sets WHERE session_id = ? AND kind = ?`, sessionID, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var ids []int64
		if json.Unmarshal([]byte(raw), &ids) == nil {
			for _, id := range ids {
				out[id] = true
			}
		}
	}
	return out, rows.Err()
}

// ---- 审计 ----

// InsertAuditLogFull 完整审计：params 存人类可读摘要（单值超 60 字截断）供后台展示，
// latencyMs 记调用耗时（仅 ai/pat 调用有值；传 nil 则记 NULL）。
func (d *DB) InsertAuditLogFull(ctx context.Context, actorType string, actorID int64, action, digest, params, result string, latencyMs *int64) error {
	_, err := d.ExecContext(ctx,
		`INSERT INTO audit_logs (actor_type, actor_id, action, params_digest, params, result, latency_ms) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		actorType, actorID, action, digest, params, result, latencyMs)
	return err
}

// ---- chat_context：模型事实源（追加写；组装按 seq 取、按 tokens 裁剪） ----

// ContextRow 模型上下文一行。
type ContextRow struct {
	ID         int64
	Seq        int64
	Role       string
	Content    string
	ToolCalls  string // JSON（assistant 轮的 tool_calls 原样存，回放还原协议配对）
	ToolCallID string
	ToolName   string
	Tokens     int
	Kind       string
	Pinned     bool
}

// AppendContext 追加一行上下文（seq 取当前最大+1）。
//
// seq 用**单条 INSERT … SELECT** 就地算：SELECT 再 INSERT 是两条语句，
// 并发写入会各自读到同一个 MAX(seq) 从而写入**重复 seq**（索引非 UNIQUE，不报错），
// 而回放按 `ORDER BY seq` —— seq 相同则顺序不定，会打乱「assistant 配对行先于 tool 行」。
// 单语句在 SQLite 内天然原子。
func (d *DB) AppendContext(ctx context.Context, userID, sessionID int64, turnID, role, content, toolCalls, toolCallID, toolName string, tokens int, kind string, pinned bool) error {
	var pinnedInt int
	if pinned {
		pinnedInt = 1
	}
	_, err := d.ExecContext(ctx,
		`INSERT INTO chat_context (user_id, session_id, turn_id, seq, role, content, tool_calls_json, tool_call_id, tool_name, tokens, kind, pinned)
		 SELECT ?, ?, ?, COALESCE(MAX(seq), 0) + 1, ?, ?, ?, ?, ?, ?, ?, ?
		 FROM chat_context WHERE session_id = ?`,
		userID, sessionID, turnID, role, content, toolCalls, toolCallID, toolName, tokens, kind, pinnedInt, sessionID)
	return err
}

// ListContext 按 seq 升序取全量（pinned 永不淘汰；裁剪在服务层按 tokens 预算做）。
func (d *DB) ListContext(ctx context.Context, sessionID int64) ([]ContextRow, error) {
	rows, err := d.QueryContext(ctx, `SELECT id, seq, role, content, COALESCE(tool_calls_json,''), COALESCE(tool_call_id,''), COALESCE(tool_name,''), tokens, kind, pinned
		FROM chat_context WHERE session_id = ? ORDER BY seq ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []ContextRow
	for rows.Next() {
		var r ContextRow
		var pinned int
		if err := rows.Scan(&r.ID, &r.Seq, &r.Role, &r.Content, &r.ToolCalls, &r.ToolCallID, &r.ToolName, &r.Tokens, &r.Kind, &pinned); err != nil {
			return nil, err
		}
		r.Pinned = pinned == 1
		list = append(list, r)
	}
	return list, rows.Err()
}

// ApproxTokens token 估算：中文 ≈ 1 rune/token 量级偏保守（×1.1），英文按 4 字符/token。
// 不追求精确——预算裁剪只需数量级正确。
func ApproxTokens(s string) int {
	rn := 0
	for range s {
		rn++
	}
	ascii := len(s) - rn*3
	if ascii < 0 {
		ascii = 0
	}
	return rn + ascii/4
}
