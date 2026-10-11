// Package chat AI 对话（ 端到端链路）：模型往返工具循环、
// 提案（确认卡，15 分钟 TTL 单次有效）、SSE 语义事件、审计与历史回显。
//
// 协议口径：SSE 事件为语义事件——turn.start / session /
// thinking.delta / thinking.end / answer.delta / answer.end / tool.start / tool.end /
// proposal / turn.end / error。零带内信令（无 emoji 前缀协议）。
package chat

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bookandmusic/love-life/server/internal/auth"
	"github.com/bookandmusic/love-life/server/internal/crypto"
	"github.com/bookandmusic/love-life/server/internal/limits"
	"github.com/bookandmusic/love-life/server/internal/llm"
	"github.com/bookandmusic/love-life/server/internal/store"
	"github.com/bookandmusic/love-life/server/internal/textutil"
	"github.com/bookandmusic/love-life/server/internal/tools"
)

const (
	// MaxToolRounds 单次对话最大工具往返（预算三维之一；初始值，无实测依据，上线后按观测调整）。
	MaxToolRounds = 8
	maxToolRounds = MaxToolRounds
	// MaxToolCalls 单轮工具调用总数上限（预算三维之一）：一次响应可能回远超额度数量的调用，
	// 故执行前按剩余额度截断，而不是先执行再判断。
	MaxToolCalls   = 16
	maxToolCalls   = MaxToolCalls
	turnWallBudget = 90 * time.Second // 整轮墙钟预算：触顶进入收尾
	// wrapUpBudget 收尾调用自己的短预算（一个连接期超时的量级）。
	// **不复用已耗尽的整轮 deadline**：那会让墙钟触顶时收尾被一并跳过，
	// AI模式设计「收尾义务」要求的「再补一次」永远不会发生，用户只拿到固定文案。
	wrapUpBudget = 30 * time.Second
	// MaxChatMediaBytes 单条消息附件的元数据体积上限：8MB（按客户端上报的 size 累加）。
	// 附件在「确认执行」前不离开设备，服务端只收到元数据——这里防的是「一次带上几十个大文件」
	// 这种明显不合理的批量（PRODUCT「发布动态」的整批把关在确认阶段还有一道真实校验）。
	MaxChatMediaBytes = 8 << 20
	// proposalTTL 提案有效期（秒）：单次有效 + 15 分钟过期。
	proposalTTLSeconds = 900
)

// MaxChatMedia 单条聊天消息媒体引用上限（与发动态同源；var 因值来自运行时解析的 JSON）。
var MaxChatMedia = limits.MaxMediaPerChatMessage

// errTurnBudgetExhausted 进链循环时预算已过 → 一个模型都没试，结果为 nil。
//
// 必须与「试了但都失败」区分开（后者返回真实错误）：若这里也返回 nil error，
// 调用方会把 nil 结果当成功去解引用（收尾路径的 `len(res.Choices)`）。
var errTurnBudgetExhausted = errors.New("整轮预算已耗尽，剩余模型未再尝试")

type Service struct {
	db   *store.DB
	key  []byte // API Key 加密密钥（派生自数据目录密钥）
	deps *tools.Deps
	reg  map[string]*tools.Tool
	// activeTurns 进程内「本轮 turn 的状态」登记（key = userID|turnID）——一份表两种读法：
	// ① 记录存在 = 本轮尚未终结（流式进行中），DELETE 会话据此拒绝（AI模式设计「会话与历史回显」）；
	// ② aborted = 用户已显式中止本轮。用户点「停止」时客户端先 abort 请求（服务端 r.Context()
	//    随之取消），在服务端与「意外断连」表现同一现象，但语义相反：停止 = 整轮回滚
	//    （AbortTurn 按 turn_id 删除全轮行），断连 = 保留已产生内容并标中断。故用同一张表把
	//    「用户主动停止」显式标出，否则断连分支会与 AbortTurn 的 DELETE 并发，给用户明确要求
	//    回滚的那轮留下孤儿 assistant 行。
	// 进程内登记，重启即失效：重启后本轮已无处理 goroutine，不会再往该会话写内容，
	// 故丢登记只是不再拦删（安全侧失败），不需要持久化。
	activeTurns sync.Map
	// turnMu 串行化「已有会话的登记」与「删除会话」：两者若各查各的，登记刚好落在
	// 「查活跃轮 → 删会话」之间，本轮的消息就会写进已删会话（chat_messages.session_id
	// 刻意无外键，落库不会报错，只会留下永不清理的孤儿行）。
	// ponytail: 一把全局锁（单进程自用规模足够）；并发量上来再按会话分锁。
	turnMu sync.Mutex
}

// turnState 单轮 turn 的进程内状态；由登记表按 userID|turnID 持有。
type turnState struct {
	userID    int64
	sessionID int64
	aborted   atomic.Bool // 用户是否已显式中止本轮
}

// turnKey 轮次登记的 key（userID 与 turnID 的组合，turnID 单轮唯一）。
func turnKey(userID int64, turnID string) string {
	return strconv.FormatInt(userID, 10) + "|" + turnID
}

// loadTurn 取本轮登记（写入口只有本文件，故断言安全）。
func (s *Service) loadTurn(userID int64, turnID string) (*turnState, bool) {
	v, ok := s.activeTurns.Load(turnKey(userID, turnID))
	if !ok {
		return nil, false
	}
	st, ok := v.(*turnState)
	return st, ok
}

// markTurnActive 登记本轮开始（记录存在即「未终结」，直到 endTurn）。
// 自身不加锁：已有会话须由持 turnMu 的 beginTurn 调用（登记与「确认会话仍在」要一气呵成）；
// 懒创建的新会话 id 尚未离开本请求，直接调用即可。
func (s *Service) markTurnActive(userID, sessionID int64, turnID string) {
	s.activeTurns.Store(turnKey(userID, turnID), &turnState{userID: userID, sessionID: sessionID})
}

// beginTurn 已有会话的登记：会话存在性确认与本轮登记在同一把锁内完成，
// 使「删会话」无法插在两者之间（见 turnMu）。会话不存在返回 false，调用方回 404。
func (s *Service) beginTurn(ctx context.Context, userID, sessionID int64, turnID string) (*store.ChatSession, bool) {
	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	sess, err := s.db.GetChatSession(ctx, sessionID)
	if err != nil || sess.UserID != userID {
		return nil, false
	}
	s.markTurnActive(userID, sessionID, turnID)
	return sess, true
}

// endTurn 登记本轮终结（正常收尾 / 断连落库 / 用户停止回滚都走同一出口）。
func (s *Service) endTurn(userID int64, turnID string) {
	s.activeTurns.Delete(turnKey(userID, turnID))
}

// beginResolve 登记「本提案正在裁决执行」——回执落库前，该会话与「流式进行中」一样不可删。
// 与删会话同锁（turnMu）：否则删会话能插在「原子认领卡片」与「回执落库」之间，
// 回执就落进已删会话（chat_messages.session_id 刻意无外键，只会留下永不清理的孤儿行）。
// 必须在**认领之前**登记：认领后卡片已不是 pending，CountPendingProposals 便拦不住删除，
// 只有这份进程内登记能拦住。合成 key 用 actionID（卡片单次有效，天然唯一）。
func (s *Service) beginResolve(userID, sessionID, actionID int64) string {
	key := "card:" + strconv.FormatInt(actionID, 10)
	s.turnMu.Lock()
	s.markTurnActive(userID, sessionID, key)
	s.turnMu.Unlock()
	return key
}

// endResolve 裁决执行结束（无论成功失败）后注销，恢复该会话的可删性。
func (s *Service) endResolve(userID int64, key string) {
	s.endTurn(userID, key)
}

// sessionTurnActive 该会话是否有未终结的轮次（流式中）；删会话路径须持 turnMu 调用。
func (s *Service) sessionTurnActive(userID, sessionID int64) bool {
	active := false
	s.activeTurns.Range(func(_, v any) bool {
		if st, ok := v.(*turnState); ok && st.userID == userID && st.sessionID == sessionID {
			active = true
			return false
		}
		return true
	})
	return active
}

// markAbortIntent 登记「用户已显式中止本轮」，供 Abort handler 在整轮回滚前调用。
// 只对本轮尚在登记中（处理中）时生效：读它的只有同一轮的处理 goroutine（persistDisconnectedTurn），
// 轮次终结后登记已删，无人再读。
func (s *Service) markAbortIntent(userID int64, turnID string) {
	if st, ok := s.loadTurn(userID, turnID); ok {
		st.aborted.Store(true)
	}
}

// abortRequested 本轮是否已被用户显式中止。
func (s *Service) abortRequested(userID int64, turnID string) bool {
	st, ok := s.loadTurn(userID, turnID)
	return ok && st.aborted.Load()
}

// clearAbortIntent 撤销中止登记（AbortTurn 自身失败时，整轮并未回滚，
// 保留登记会让后续真实断连不敢落库）。清位而非删记录：登记本身代表「本轮未终结」，
// 由 endTurn 负责删（本轮若已终结，登记已不在，这里自然是空操作）。
func (s *Service) clearAbortIntent(userID int64, turnID string) {
	if st, ok := s.loadTurn(userID, turnID); ok {
		st.aborted.Store(false)
	}
}

func NewService(db *store.DB, seed []byte, deps *tools.Deps) *Service {
	reg := map[string]*tools.Tool{}
	for _, t := range tools.Registry() {
		reg[t.Name] = t
	}
	return &Service{db: db, key: crypto.KeyFrom(seed), deps: deps, reg: reg}
}

// SystemPromptTrunkFallback 主干模板：管理后台未配置 system_prompt 时的默认值。
const SystemPromptTrunkFallback = "你是情侣生活记录应用的 AI 助手，帮助这对情侣记录生活、查询回忆。"

// weekdayCN 中文星期（time.Weekday 只给英文，提示词要给人看的中文）。
var weekdayCN = [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}

// promptTail 固定追加在 system prompt 末尾的「空间事实 + 能力与规矩」。
// 两项各有理由：空间事实是每轮都成立的稳定信息（直接注入比让模型调工具省一个往返）；
// 能力与规矩不被后台自定义 prompt 覆盖（覆盖掉模型就会退化成「文字复述操作」）。
// 每一项单独降级：读不到就跳过，不得让对话因提示词组装失败而中断。
// resolvedSummary 为「已裁决提案超出回放上限」的一行摘要（空 = 无溢出）。
func (s *Service) promptTail(ctx context.Context, userID int64, resolvedSummary string) string {
	facts := make([]string, 0, 5)
	if who := s.factCouples(ctx, userID); who != "" {
		facts = append(facts, who)
	}
	if d := s.factTogether(ctx); d != "" {
		facts = append(facts, d)
	}
	now := time.Now()
	facts = append(facts, "今天 "+now.Format("2006-01-02")+" "+weekdayCN[now.Weekday()])
	if n, err := s.countRecentPosts(ctx); err == nil {
		facts = append(facts, fmt.Sprintf("近 7 天记了 %d 条动态", n))
	}
	if a := s.factNextAnniversary(ctx, now); a != "" {
		facts = append(facts, a)
	}

	var sb strings.Builder
	sb.WriteString("\n\n【空间事实】" + strings.Join(facts, "；") + "\n")
	if resolvedSummary != "" {
		sb.WriteString("【历史操作概要】" + resolvedSummary + "\n")
	}
	sb.WriteString("【能力与规矩】\n")
	sb.WriteString("  - 你能查询与提案操作：动态/相册/纪念日/地点/互动。\n")
	sb.WriteString("  - 所有写操作都会先给用户一张确认卡，用户确认后才生效；" +
		"意图明确就直接出提案，不要反复问「需要我帮你…吗」。\n")
	sb.WriteString("  - 记录内容不可编辑，只能新增或删除（删除进回收站）。不要承诺你能修改已有内容。\n")
	sb.WriteString("  - 「今天/昨天/上周/最近」等相对时间，先换算成 from/to 的 YYYY-MM-DD 再查，query 用描述内容的语义词。\n")
	sb.WriteString("  - 用户消息标注附带媒体附件时，直接调用 propose_post 只填文本参数；" +
		"附件由系统在用户确认时自动转正，不要传 media_tokens、更不要向用户索要。\n")
	sb.WriteString("  - 批量整理媒体时用结果集句柄（source={result_id, group?}）引用，不要搬运 id 列表；" +
		"用户勾选了条目时，操作范围以勾选子集句柄为准。\n")
	sb.WriteString("  - 「我们一共记了多少」「点亮了哪些城市」「哪个月记得最多」这类总量/分布问题，" +
		"用 get_space_stats 一次拿全（统计卡会直接展示），不要逐类去数。\n")
	sb.WriteString("  - 查询结果用户已经能在卡片里看到，不要逐条复述；给结论、给感受、给下一步建议。\n")
	return sb.String()
}

func (s *Service) factCouples(ctx context.Context, userID int64) string {
	users, err := s.db.ListUsers(ctx)
	if err != nil {
		return ""
	}
	var self, partner string
	for _, u := range users {
		if u.Role == "admin" {
			continue
		}
		n := u.Nickname
		if n == "" {
			n = u.Username
		}
		if n == "" {
			continue
		}
		if u.ID == userID {
			self = n
		} else {
			partner = n
		}
	}
	switch {
	case self != "" && partner != "":
		return fmt.Sprintf("正在与你（%s）对话，你的伴侣是 %s", self, partner)
	case partner != "":
		return "你的伴侣是 " + partner
	default:
		return ""
	}
}

// factTogether 在一起天数；未设恋爱开始日时不给数字（与首页同口径：不编造）。
func (s *Service) factTogether(ctx context.Context) string {
	sp, err := s.db.GetSpace(ctx)
	if err != nil {
		return ""
	}
	days, ok := store.DaysTogether(sp.LoveStartDate.String, time.Now())
	if !ok {
		return ""
	}
	return fmt.Sprintf("在一起 %d 天（自 %s）", days, sp.LoveStartDate.String)
}

// countRecentPosts 近 7 天动态数（只给计数，不列内容）。
func (s *Service) countRecentPosts(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM posts WHERE deleted_at IS NULL AND created_at >= datetime('now', '-7 days')`).Scan(&n)
	return n, err
}

// factNextAnniversary 最近一个纪念日（含农历换算后的下次公历日）。
func (s *Service) factNextAnniversary(ctx context.Context, now time.Time) string {
	list, err := s.db.ListAnniversaries(ctx)
	if err != nil {
		return ""
	}
	a, ok := store.PickNextAnniversary(list, now)
	if !ok {
		return ""
	}
	return fmt.Sprintf("下一个纪念日：%s（下次公历 %s，还有 %d 天）", a.Name, a.NextSolarDate(now), a.DaysUntil(now))
}

// systemPrompt 拼装：主干（app_settings system_prompt → fallback）+ 风格片段。
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
	Client     *llm.Client
	ProviderID int64
	Model      string
	MaxTokens  *int64
	// Thinking 模型级思考档位（off/low/medium/high）；adapter 按协议映射到原生参数。
	Thinking string
	// ContextWindow 模型上下文窗口（token），nil = 未声明，用服务端兜底预算。
	ContextWindow *int64
}

// chatTargets 构建 chat 链（模型条目按 chain_order, id 全局链序，跨供应商；
// 同一供应商挂多个条目时各自成链上节点）。失败依次向下切换（AI模式设计「模型网关与配置中心」）。
func (s *Service) chatTargets(ctx context.Context) ([]*chatTarget, error) {
	chain, err := s.db.ListModels(ctx, "chat")
	if err != nil {
		return nil, err
	}
	var targets []*chatTarget
	decryptFailed := 0
	decrypted := map[int64]string{} // provider_id -> 明文 Key（一次解密，多条目复用）
	for _, e := range chain {
		p := e.Provider
		apiKey, ok := decrypted[p.ID]
		if !ok {
			plain, derr := crypto.Decrypt(s.key, p.APIKeyEnc)
			if derr != nil {
				decryptFailed++
				slog.Warn("模型 Key 解密失败，跳过该供应商", "provider_id", p.ID, "err", derr)
				decrypted[p.ID] = ""
				continue
			}
			decrypted[p.ID] = plain
			apiKey = plain
		}
		if apiKey == "" {
			continue
		}
		model := e.Model.ModelID
		client, cerr := llm.NewForProtocol(p.BaseURL, apiKey, model, p.APIType)
		if cerr != nil {
			return nil, fmt.Errorf("不支持的服务协议: %w", cerr)
		}
		t := &chatTarget{Client: client, ProviderID: p.ID, Model: model}
		if e.Model.MaxTokens.Valid {
			v := e.Model.MaxTokens.Int64
			t.MaxTokens = &v
		}
		t.Thinking = e.Model.Thinking
		if e.Model.ContextWindow.Valid {
			v := e.Model.ContextWindow.Int64
			t.ContextWindow = &v
		}
		targets = append(targets, t)
	}
	if len(targets) == 0 {
		if decryptFailed > 0 && decryptFailed == len(chain) {
			return nil, errKeyDecrypt
		}
		return nil, errNoModel
	}
	return targets, nil
}

var errNoModel = errors.New("未配置模型——请管理员在后台配置模型服务")

// errKeyDecrypt 链上全部供应商的 Key 都解密失败（数据问题，非「未配置」）。
var errKeyDecrypt = errors.New("模型 Key 解密失败，请管理员检查配置")

// chatCall 一次模型调用沿链执行：链头起首个成功者胜出；失败者记 failed 统计后淘汰。
// streamEvents 一轮流式调用的归一产物：全部增量 + 重组后的完整响应（供既有工具循环）。
type streamEvents struct {
	Deltas   []llm.Delta
	Response *llm.ChatResponse
}

// chatCallStream 流式版链式调用：链首失败且尚未产出任何增量时向下切换；
// 已开始下发增量后失败即以错误收尾（不换模型续写，AI模式设计「模型网关」）。
// 返回值 rest 之外还带本轮全部增量（调用方据此重组完整响应）。
//
// deadline 是**整轮墙钟预算**的到期点（必传，零值即「早已过期」，故调用方一律给真实时间）：
// 链上每个模型各自可能耗尽连接期超时（30s），
// 逐个重试会线性超预算（4 条链 = 120s，而预算是 90s）。故每试下一个之前先判到期，
// 已到期就直接放弃剩余链——预算的语义是「整轮不超过 N 秒」，不能只管轮与轮之间。
func (s *Service) chatCallStream(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, chain []*chatTarget, req *llm.ChatRequest, onDelta func(llm.Delta), deadline time.Time) (*llm.ChatResponse, []llm.Delta, []*chatTarget, error) {
	var lastErr error
	for len(chain) > 0 {
		if time.Now().After(deadline) {
			// 预算到期：剩余链不再尝试。单独记一条——下面那条 warn 只在单个模型失败时发，
			// 看不出「这轮是被预算叫停的」。
			slog.Warn("整轮预算已耗尽，放弃剩余链", "remaining", len(chain), "last_err", lastErr)
			break
		}
		t := chain[0]
		chain = chain[1:]
		req.MaxTokens = t.MaxTokens
		req.Thinking = t.Thinking
		startedAt := time.Now()
		var deltas []llm.Delta
		var usage llm.Usage
		_, err := t.Client.StreamChat(ctx, req, func(d llm.Delta) {
			if d.Usage != nil {
				usage = *d.Usage
			}
			deltas = append(deltas, d)
			if onDelta != nil {
				onDelta(d)
			}
		})
		elapsed := time.Since(startedAt).Milliseconds()
		if err == nil {
			// 上游未产出任何流式增量（非事件流响应）：adapter 层已降级回放，
			// 正常不会走到这里；deltas 为空时重组块会自动填充整块响应。
			s.recordUsageStream(ctx, t, usage, elapsed, "success")
			return &llm.ChatResponse{Usage: usage}, deltas, append([]*chatTarget{t}, chain...), nil
		}
		s.recordUsageStream(ctx, t, usage, elapsed, "failed")
		lastErr = err
		if len(deltas) > 0 {
			break // 已下发增量：不换模型续写（用户会看到半句话被另一个模型接下去）
		}
		// 链上还有下一个模型时，这次失败会被**静默跳过**（用户只看到最终答上来的那个）。
		// 不记一笔的话，后台「服务日志」与终端都看不到「链首一直失败」——而排障时
		// 第一个失败原因往往才是根因（末尾的 lastErr 只是最后一次）。
		if len(chain) > 0 {
			warnChainFallback(t, err, elapsed)
		}
	}
	if lastErr == nil {
		return nil, nil, nil, errTurnBudgetExhausted // 一个都没试成：见该错误的说明
	}
	return nil, nil, nil, lastErr
}

// recordUsageStream 流式版用量落库（usage 由末帧归一；无 usage 时如实记 0）。
func (s *Service) recordUsageStream(ctx context.Context, t *chatTarget, usage llm.Usage, latencyMs int64, status string) {
	if t == nil {
		return
	}
	_ = s.db.InsertAIUsage(ctx, &store.AIUsageLog{
		ProviderID: t.ProviderID, Model: t.Model, LatencyMs: latencyMs, Status: status,
		PromptTokens: usage.PromptTokens, CompletionTokens: usage.CompletionTokens, TotalTokens: usage.TotalTokens,
	})
}

// warnChainFallback 记一次「链上某模型失败、还有下一个可试」。
//
// 用户只看到最终答上来的那个模型，中途失败不体现在界面上——不记一笔的话，后台
// 「服务日志」与终端都看不到「链首一直在失败」，而排障时第一个失败原因往往才是根因。
// （usage 行只记状态、不带原因，故不能替代这里。）流式与非流式两条链式路径共用，
// 避免两处措辞漂移。
//
// elapsedMs 是这一次尝试实际耗时：单看错误串分不出「耗尽 30s 连接超时」还是
// 「上游秒回错误」，有这个字段才不用再写临时探针。预算叫停剩余链另有一条日志
// （本函数只在「还有下一个可试」时发，看不出是被预算拦住的）。
func warnChainFallback(t *chatTarget, err error, elapsedMs int64) {
	if t == nil {
		return
	}
	slog.Warn("模型调用失败，切换到链上下一个",
		"provider_id", t.ProviderID, "model", t.Model, "err", err, "elapsed_ms", elapsedMs)
}

// shouldWrapUp 预算收尾调用是否该发：未作答必须收尾（不能静默 done），**出提案不收尾**
// （提案 break 留下无回执的 tool_calls，收尾请求必被上游拒——见 AI模式设计「收尾义务」）。
//
// 现有可达路径上 `answered && budgetHit` 只可能是出提案（正文回答那一支不经过预算截断），
// 故本函数等价于 `!answered`；显式留 `budgetHit`/`stopReason` 两参数是把那条规则钉在
// 测试里，防止有人再把 `|| budgetHit` 加回来。
func shouldWrapUp(answered, budgetHit bool, stopReason string) bool {
	return !answered || (budgetHit && stopReason != "proposal")
}

// modelErrorText 把模型调用的原始错误翻成面向用户的文案，并给出「重试是否有意义」。
//
// 分类只认**类型与状态码**，不匹配错误字符串（措辞一改，字符串匹配就静默失效）。
// 原始错误另有去向：调用处写 slog，故这里可以放心只给人话。
func modelErrorText(err error) (text string, retryable bool) {
	// 上游自述原因（含 HTTP 状态）：按状态码分流
	var up *llm.UpstreamError
	if errors.As(err, &up) {
		switch {
		case up.StatusCode == http.StatusUnauthorized || up.StatusCode == http.StatusForbidden:
			return "模型服务拒绝了访问，请检查后台配置的 API Key", false
		case up.StatusCode == http.StatusTooManyRequests:
			return "模型服务当前限流，请稍后重试", true
		case up.StatusCode >= 500:
			return "模型服务暂时不可用，请稍后重试", true
		case up.StatusCode == http.StatusBadRequest:
			// 参数被上游拒（如档位不被该模型接受）——重试无用，得改配置
			return "该模型不接受当前的请求参数，请在后台检查模型配置", false
		case up.StatusCode == http.StatusRequestTimeout:
			return "模型服务响应超时，请稍后重试", true
		case up.StatusCode > 0:
			// 走到这里只剩未单列的 4xx（≥500 与 429 已在上面拦掉）：重试解决不了
			return "模型服务返回错误，请检查后台配置", false
		default:
			// 流式帧内的 error 事件：没有状态码可用
			return "模型服务中断了本次回答，请重试", true
		}
	}
	if errors.Is(err, llm.ErrUnsupportedAPIType) {
		return "该供应商协议暂不支持，请在后台检查配置", false
	}
	// 超时：必须区分「被我们自己的连接期超时砍掉」与「上游回了 504」——
	// 两者都表现为 net/http timeout，但运维动作不同：
	//   前者说明该模型首包就是要 30s 以上（换模型 / 放宽超时），
	//   后者是上游自身故障（等它恢复）。
	// 上游 504 走上面的 UpstreamError 分支（>=500 → 暂时不可用），故这里剩下的是
	// 「连接期被 Transport 砍」与「请求 ctx 到期」两类，都归到响应超时。
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "模型服务响应超时，请稍后重试", true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "模型服务响应超时，请稍后重试", true
	}
	if errors.Is(err, errTurnBudgetExhausted) {
		return "本轮耗时已达上限，请稍后重试", true
	}
	// 连接层失败（域名解析不了 / 端口不通 / TLS 握手失败）都包在 *url.Error 里
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return "无法连接模型服务，请检查后台配置的服务地址", false
	}
	return "AI 暂时不可用，请稍后重试", true
}

func (s *Service) chatCall(ctx context.Context, chain []*chatTarget, req *llm.ChatRequest, deadline time.Time) (*llm.ChatResponse, []*chatTarget, error) {
	var lastErr error
	for len(chain) > 0 {
		if time.Now().After(deadline) {
			slog.Warn("整轮预算已耗尽，放弃剩余链", "remaining", len(chain), "last_err", lastErr)
			break
		}
		t := chain[0]
		chain = chain[1:]
		req.MaxTokens = t.MaxTokens
		req.Thinking = t.Thinking
		startedAt := time.Now()
		res, err := t.Client.Chat(ctx, req)
		elapsed := time.Since(startedAt).Milliseconds()
		if err == nil {
			s.recordUsage(ctx, t, res, elapsed, "success")
			return res, append([]*chatTarget{t}, chain...), nil
		}
		s.recordUsage(ctx, t, nil, elapsed, "failed")
		lastErr = err
		// 同上：链上还有下一个时这次失败会被静默跳过，记一笔否则无从察觉
		if len(chain) > 0 {
			warnChainFallback(t, err, elapsed)
		}
	}
	if lastErr == nil {
		return nil, nil, errTurnBudgetExhausted // 一个都没试成：见该错误的说明
	}
	return nil, nil, lastErr
}

// recordUsage 落库一次模型调用的统计。失败不阻断对话。
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

// aiAvailability AI 可用性判定（chat 链为空即不可用，不设独立功能开关）。
func (s *Service) aiAvailability(ctx context.Context) (bool, string) {
	head, err := s.db.GetChatHead(ctx)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return false, "未配置模型——请管理员在后台配置模型服务"
	case err != nil:
		return false, "模型配置读取失败"
	}
	if _, derr := crypto.Decrypt(s.key, head.Provider.APIKeyEnc); derr != nil {
		return false, "模型 Key 解密失败，请管理员检查配置"
	}
	return true, ""
}

// Status GET /api/v1/chat/status 返回 AI 可用性（锁定态轮询用，文案与 bootstrap 同源）。
func (s *Service) Status(w http.ResponseWriter, r *http.Request) {
	available, reason := s.aiAvailability(r.Context())
	s.writeJSON(w, http.StatusOK, map[string]any{"available": available, "reason": reason})
}

// chatChips 快捷指令（静态文案，点击填入输入框）。
var chatChips = []string{"帮我发动态", "整理最近的照片", "我们记过什么"}

// Bootstrap GET /api/v1/chat/bootstrap：AI 页首屏一次拿全（可用性 + 欢迎语 + chips）。
func (s *Service) Bootstrap(w http.ResponseWriter, r *http.Request) {
	available, reason := s.aiAvailability(r.Context())
	s.writeJSON(w, http.StatusOK, map[string]any{
		"available": available,
		"reason":    reason,
		"welcome":   s.welcomeText(r.Context()),
		"chips":     chatChips,
	})
}

// welcomeText 空会话欢迎语：服务端模板拼装空间事实，不调模型、不落库、不占会话。
func (s *Service) welcomeText(ctx context.Context) string {
	days := 0
	if sp, err := s.db.GetSpace(ctx); err == nil {
		if n, ok := store.DaysTogether(sp.LoveStartDate.String, time.Now()); ok {
			days = n
		}
	}
	var recent int
	_ = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM posts WHERE deleted_at IS NULL AND created_at >= datetime('now', '-7 days')`).Scan(&recent)
	switch {
	case days > 0 && recent > 0:
		return fmt.Sprintf("今天是你们在一起的第 %d 天。这周已经记了 %d 条，想再记点什么？", days, recent)
	case days > 0:
		return fmt.Sprintf("今天是你们在一起的第 %d 天。想记点什么？", days)
	default:
		return "想记点什么？"
	}
}

// newTurnID 生成本轮对话标识——abort 按轮回滚的定位依据。
func newTurnID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "t" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b[:])
}

// chatMedia 客户端随消息携带的媒体引用：仅元数据，不含文件内容。
// 依 AI模式设计「图片处理与存储」：消息体只带 {name, mime, size}，服务端不存文件、不存 base64；
// 文件在「确认执行提案时」才从本机上传（与发动态同一管线）。
type chatMedia struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Size int64  `json:"size"`
}

// quoteRef 用户引用的资源（仅限当前会话）：
//   - 结果集引用：{result_id, group?, ids?}（勾选具体条目时带 ids，缺省 = 整组/整集）；
//   - 单条引用：{kind, id}（id 必须在本会话结果集中出现过）。
type quoteRef struct {
	ResultID string  `json:"result_id,omitempty"`
	Group    string  `json:"group,omitempty"`
	IDs      []int64 `json:"ids,omitempty"`
	Kind     string  `json:"kind,omitempty"`
	ID       int64   `json:"id,omitempty"`
}

// Message POST /api/v1/chat/message {content, media?, session_id?, quoted?}，SSE 语义事件响应。
func (s *Service) Message(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	var req struct {
		Content   string      `json:"content"`
		Media     []chatMedia `json:"media"`
		SessionID int64       `json:"session_id"`
		Quoted    []quoteRef  `json:"quoted"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"请求格式错误"}`, http.StatusBadRequest)
		return
	}
	// 正文可选：可留空只传媒体、或只勾引用（PRODUCT「发布动态」同口径）；三者全空才是空消息。
	if strings.TrimSpace(req.Content) == "" && len(req.Media) == 0 && len(req.Quoted) == 0 {
		http.Error(w, `{"error":"内容、媒体或引用至少一项"}`, http.StatusBadRequest)
		return
	}
	if len(req.Media) > MaxChatMedia {
		http.Error(w, fmt.Sprintf(`{"error":"单条消息最多 %d 个媒体引用"}`, MaxChatMedia), http.StatusBadRequest)
		return
	}
	// 附件总量护栏：按客户端上报的 size 累加（元数据层面，不需要文件内容）。
	// 拦的是「一次带上几十个大文件」这种明显不合理的批量——真实校验在确认执行阶段的上传。
	var mediaBytes int64
	for _, m := range req.Media {
		mediaBytes += m.Size
	}
	if mediaBytes > MaxChatMediaBytes {
		http.Error(w, fmt.Sprintf(`{"error":"附件总体积过大（上限 %d MB），请减少数量"}`, MaxChatMediaBytes>>20), http.StatusBadRequest)
		return
	}
	for _, m := range req.Media {
		if !strings.HasPrefix(m.Type, "image/") && !strings.HasPrefix(m.Type, "video/") && !strings.HasPrefix(m.Type, "audio/") {
			http.Error(w, `{"error":"媒体类型仅支持图片/视频/音频"}`, http.StatusBadRequest)
			return
		}
	}

	// 会话归属：>0 必须是本人会话；否则懒创建（标题=首条消息摘要，无 AI 调用）
	turnID := newTurnID()
	// 本轮登记的解除：defer 兜住全部出口——正常收尾、断连落库、用户停止回滚、中间的错误返回。
	// 未登记时 endTurn 是空操作，故可先挂 defer 再登记。
	defer s.endTurn(claims.UserID, turnID)
	sessionID := req.SessionID
	sessionTitle := ""
	createdSession := false
	if sessionID > 0 {
		sess, ok := s.beginTurn(r.Context(), claims.UserID, sessionID, turnID)
		if !ok {
			http.Error(w, `{"error":"会话不存在"}`, http.StatusNotFound)
			return
		}
		sessionTitle = sess.Title
	} else {
		sessionTitle = sessionTitleFrom(req.Content)
		id, err := s.db.CreateChatSession(r.Context(), claims.UserID, sessionTitle)
		if err != nil {
			http.Error(w, `{"error":"服务器错误"}`, http.StatusInternalServerError)
			return
		}
		sessionID = id
		// 仅懒创建时下发：已有会话的 id 客户端自己带的，不重复发
		createdSession = true
		// 新会话 id 尚未离开本请求（只有本函数知道），不存在与删会话的竞态
		s.markTurnActive(claims.UserID, sessionID, turnID)
	}

	// 引用归属校验：句柄必须属于本会话、单条 id 必须在本会话结果集里出现过。
	quotedContext, quotedJSON, quotedSel, qerr := s.validateQuoted(r.Context(), sessionID, req.Quoted)
	if qerr != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, qerr.Error()), http.StatusBadRequest)
		return
	}

	// 轮级聚合缓存：思考文本 / 工具步骤（parts 契约：一轮助手消息只有一行）。
	// turnParts 按发生顺序累积各轮内容，落库时一次写出（thinking → tool* → text/card）。
	turnParts := []map[string]any{}
	chain, err := s.chatTargets(r.Context())
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errNoModel) {
			status = http.StatusLocked
		}
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), status)
		return
	}

	// 历史 + 用户风格；空间事实与工具约定固定追加在 system 末尾
	user, _ := s.db.GetUserByID(r.Context(), claims.UserID)
	ctxMsgs, resolvedSummary := s.buildModelHistory(r.Context(), claims.UserID, sessionID, contextBudget(chain))
	msgs := []llm.Message{{Role: "system", Content: s.systemPrompt(r.Context(), userStyle(user)) + s.promptTail(r.Context(), claims.UserID, resolvedSummary)}}
	msgs = append(msgs, ctxMsgs...)
	modelContent := req.Content
	if len(req.Media) > 0 {
		// 只给文件名与数量（不存文件、不存 base64）：模型据此知道「这条消息发过哪些文件」，
		// 刷新后回显也仍是同一行元信息（AI模式设计「图片处理与存储」）。
		// 名字可能缺省（客户端未上报）——空名不拼进列表，全空时只留数量，避免出现「附件：」残句。
		names := make([]string, 0, len(req.Media))
		for _, m := range req.Media {
			if n := strings.TrimSpace(m.Name); n != "" {
				names = append(names, n)
			}
		}
		suffix := ""
		if len(names) > 0 {
			suffix = "：" + strings.Join(names, "、")
		}
		modelContent += fmt.Sprintf(" [本条消息附带 %d 个媒体附件%s]", len(req.Media), suffix)
	}
	if quotedContext != "" {
		modelContent += "\n" + quotedContext
	}
	msgs = append(msgs, llm.Message{Role: "user", Content: modelContent})
	// user parts：text + attachment（本机元数据 {name,mime,size}，无 ref 无 base64）
	// + quote（引用条回显；附件在确认执行前只在本机，落库仅元数据）
	// text 为空（只传媒体 / 只勾引用）时不产生空 text part，避免回显出空行。
	// 判据用 TrimSpace 与上面的入参校验同口径，否则「只有空格 + 有媒体」会落一个空白 part。
	userParts := []map[string]any{}
	if strings.TrimSpace(req.Content) != "" {
		userParts = append(userParts, map[string]any{"type": "text", "text": req.Content})
	}
	for _, m := range req.Media {
		userParts = append(userParts, map[string]any{"type": "attachment", "name": m.Name, "mime": m.Type, "size": m.Size})
	}
	for _, q := range req.Quoted {
		userParts = append(userParts, map[string]any{"type": "quote", "refs": q})
	}
	partsJSON, _ := json.Marshal(userParts)
	if _, err := s.db.SaveChatMessage(r.Context(), &store.ChatMessage{
		UserID: claims.UserID, SessionID: sessionID, TurnID: turnID, Role: "user",
		Parts: string(partsJSON), Quoted: quotedJSON,
	}); err != nil {
		http.Error(w, `{"error":"服务器错误"}`, http.StatusInternalServerError)
		return
	}
	if err := s.db.TouchChatSession(r.Context(), sessionID); err != nil {
		slog.Warn("touch chat session failed", "session_id", sessionID, "err", err)
	}
	// 模型上下文：user 轮（dialogue）
	if err := s.db.AppendContext(r.Context(), claims.UserID, sessionID, turnID,
		"user", modelContent, "", "", "", store.ApproxTokens(modelContent), "dialogue", false); err != nil {
		slog.Warn("写入模型上下文失败", "session_id", sessionID, "err", err)
	}

	flusher, ok := s.sseHeader(w)
	if !ok {
		return
	}
	if createdSession {
		s.sseEvent(w, flusher, "session", map[string]any{"session_id": sessionID, "title": sessionTitle})
	}
	s.sseEvent(w, flusher, "turn.start", map[string]any{"turn_id": turnID})

	// 工具循环（读工具立即执行回填；提案工具止于确认卡）
	answered := false
	stopReason := "aborted"
	// 一轮附件只归首个提案（旧实现两行都核会让同轮两张卡绑同一批 token）。
	mediaRefs := mediaRefsFrom(req.Media)
	// 结果集端口 + 会话 id 注入 ctx：读工具落句柄、source 展开校验归属
	toolCtx := tools.WithSessionID(r.Context(), sessionID)
	toolCtx = tools.WithResultSink(toolCtx, func(kind string, ids []int64, groups []store.ResultGroup) (string, error) {
		return s.db.SaveResultSet(toolCtx, sessionID, kind, ids, groups)
	})
	// 点选优先：本轮勾选并集注入工具 ctx——提案展开 source / 直给 id 时校验 ⊆（超出即驳回）
	toolCtx = tools.WithQuotedSelection(toolCtx, quotedSel)
	deadline := time.Now().Add(turnWallBudget)
	toolCallsUsed := 0
	budgetHit := false
	for round := 0; round < maxToolRounds; round++ {
		// 预算三维任一触顶即**跳出主循环**，直接进收尾（AI模式设计「预算与并行」预算三维 +
		// 「收尾义务」）：此处 break 掉的是本轮**带工具**的 chatCallStream，不再多花一次调用；
		// 跳出后循环末尾的不带工具收尾调用照常发（那是文档要求的「必须再补一次」）。
		if time.Now().After(deadline) || toolCallsUsed >= maxToolCalls {
			budgetHit = true
			break
		}
		// 真流式：思考链与正文逐字下发（AI模式设计「时序与事件」）；回答一开始即折叠的
		// 语义由客户端处理，服务端只保证 delta 真增量。
		thinkStart := time.Now()
		thinkOpen := false
		thinkStreamed := false
		var streamedAnswer bool
		// 断连兜底：逐字增量的真正去处是回调 onDelta；chatCallStream 出错时返回的
		// lastDeltas 为 nil（错误路径丢弃 deltas），所以这里同步累积一份，
		// 使「意外断连」时手上已有全部已下发内容（否则落库时无正文可存）。
		var streamReasoning, streamAnswer strings.Builder
		// 本轮已下发的思考秒数之和：下发 thinking.end 时顺手累加，落库直接复用它，使
		// 「流式期间看到的」与「刷新后看到的」构造上同值（AI模式设计「过程收起（客户端渲染口径）」）。
		// 落库不得另算一次——那样算的是整轮墙钟，会把正文生成的耗时也算成思考。
		roundThinkSeconds := 0
		emitThinkEnd := func() {
			secs := int(time.Since(thinkStart).Seconds())
			roundThinkSeconds += secs
			thinkOpen = false
			s.sseEvent(w, flusher, "thinking.end", map[string]any{"seconds": secs})
		}
		res, lastDeltas, rest, err := s.chatCallStream(r.Context(), w, flusher, chain,
			&llm.ChatRequest{Messages: msgs, Tools: s.toolSpecs(r.Context())},
			func(d llm.Delta) {
				switch {
				case d.Reasoning != "":
					streamReasoning.WriteString(d.Reasoning)
					s.sseEvent(w, flusher, "thinking.delta", map[string]any{"text": d.Reasoning})
					thinkOpen = true
					thinkStreamed = true
				case d.Content != "":
					if thinkOpen {
						emitThinkEnd()
					}
					streamAnswer.WriteString(d.Content)
					streamedAnswer = true
					s.sseEvent(w, flusher, "answer.delta", map[string]any{"text": d.Content})
				}
			}, deadline)
		if thinkOpen {
			emitThinkEnd()
		}
		// 断连判定必须在 err 之前：连接断了（网络抖动 / 切后台 / 锁屏，或客户端未及发 abort）
		// 会让上游调用报 ctx 取消错误，先走 err 分支就再也到不了这里。
		if r.Context().Err() != nil {
			// 思考秒数同样复用本轮**已下发**的那个数（同上）：上面的收尾保证思考只要开过就
			// 一定发过 thinking.end 并累加过，故这里直接取累加值，不再另算。
			s.persistDisconnectedTurn(claims.UserID, sessionID, turnID, turnParts,
				streamReasoning.String(), streamAnswer.String(), roundThinkSeconds)
			return
		}
		if err != nil {
			// 原始错误进服务端日志（含上游地址与自述原因），面向用户只给可读分类文案——
			// 此前直出 err.Error() 会把 `Post "http://内网地址/…": net/http: …` 显示给用户。
			text, retryable := modelErrorText(err)
			slog.Error("模型调用失败", "session_id", sessionID, "turn_id", turnID, "err", err, "retryable", retryable)
			s.sseEvent(w, flusher, "error", map[string]any{"code": "model_error", "message": text, "retryable": retryable})
			s.sseEvent(w, flusher, "turn.end", map[string]any{"stop_reason": "error"})
			return
		}
		chain = rest
		// 流式重组：chatCallStream 只带 usage，正文/思考从增量重组。
		//
		// 工具调用**不在这里拼**——各 adapter 已把分片按协议归组拼好，只发完整调用
		// （AI模式设计「预算与并行」· tool_calls 增量累积：「累积在 adapter 内部完成，
		// Agent 层只看到完整的 tool_call」）。这里按增量到达顺序原样收集即可。
		if len(res.Choices) == 0 {
			var reasoning, content strings.Builder
			var toolCalls []llm.ToolCall
			for _, d := range lastDeltas {
				if d.Reasoning != "" {
					reasoning.WriteString(d.Reasoning)
				}
				if d.Content != "" {
					content.WriteString(d.Content)
				}
				if d.ToolCall != nil {
					tc := llm.ToolCall{ID: d.ToolCall.ID, Type: "function", Signature: d.ToolCall.Signature}
					tc.Function.Name = d.ToolCall.Name
					tc.Function.Arguments = d.ToolCall.Arguments
					toolCalls = append(toolCalls, tc)
				}
			}
			cm := llm.ChoiceMessage{Content: content.String(), Reasoning: reasoning.String(), ToolCalls: toolCalls}
			res.Choices = []llm.Choice{{Message: cm, FinishReason: "stop"}}
		}
		choice := res.Choices[0]
		// 思考链：模型下发 reasoning_content 则记录并落库（刷新后挂回随后的助手气泡，默认折叠）。
		// 增量已在上面逐帧下发过——这里**只补未流式下发的情况**，否则用户会看到思考重复一遍。
		if choice.Message.Reasoning != "" {
			if !thinkStreamed {
				s.sseEvent(w, flusher, "thinking.delta", map[string]any{"text": choice.Message.Reasoning})
				emitThinkEnd()
			}
			// 本轮思考进 parts 累积（各轮按发生顺序，落库时一次写出）
			turnParts = append(turnParts, map[string]any{"type": "thinking", "text": choice.Message.Reasoning, "seconds": roundThinkSeconds})
		}
		if len(choice.Message.ToolCalls) == 0 {
			text := choice.Message.Content
			{
				// parts 展示口径（AI模式设计「消息 parts 契约」）：思考 → 工具步骤 → 正文
				// 全在同一行，刷新后仍可展开（与「不进模型上下文」是两回事）。
				parts := append(turnParts, map[string]any{"type": "text", "text": text})
				pj, _ := json.Marshal(parts)
				_, _ = s.db.SaveChatMessage(r.Context(), &store.ChatMessage{UserID: claims.UserID, SessionID: sessionID, TurnID: turnID, Role: "assistant", Parts: string(pj)})
			}
			// 兜底整块下发（流式回调未覆盖到正文时）：文本非空才发 delta，避免空帧
			if text != "" && !streamedAnswer {
				s.sseEvent(w, flusher, "answer.delta", map[string]any{"text": text})
			}
			// 助手正文进模型上下文（kind=dialogue，AI模式设计「上下文组装」· kind 取值：
			// dialogue 含用户消息与助手正文）。此前只写展示面 chat_messages，
			// 模型看不到自己上一轮说过什么，多轮指代（「再说一次」）会失忆。
			// 空文本不写（空串进上下文是无效行）；思考链不进上下文。
			if text != "" {
				_ = s.db.AppendContext(r.Context(), claims.UserID, sessionID, turnID,
					"assistant", text, "", "", "", store.ApproxTokens(text), "dialogue", false)
			}
			s.sseEvent(w, flusher, "answer.end", map[string]any{})
			answered = true
			stopReason = "answer"
			break
		}

		// 预算：工具调用总数上限。一次响应的调用数可能远超剩余额度，故**执行前截断**，
		// 且截断后的 assistant 行与随后的 tool 结果成对进上下文（不留下无回执的 tool_call）。
		calls := choice.Message.ToolCalls
		if remaining := maxToolCalls - toolCallsUsed; len(calls) > remaining {
			if remaining <= 0 {
				budgetHit = true
				break
			}
			calls = calls[:remaining]
			budgetHit = true
		}
		toolCallsUsed += len(calls)

		msgs = append(msgs, llm.Message{Role: "assistant", Content: choice.Message.Content, ToolCalls: calls})
		stopForProposal := false
		// 读工具并行、写工具串行（AI模式设计「预算与并行」）。
		// 只并行「首个写工具之前」那一段：出提案会中止其后所有 call（一轮一卡），
		// 若把提案之后的读工具也并发跑掉，就等于执行了本不该执行的调用。
		readN := len(calls)
		for i, c := range calls {
			if t, ok := s.reg[c.Function.Name]; ok && t.IsWrite {
				readN = i
				break
			}
		}
		serialFrom := 0 // 串行段起点：并行只吃掉了前面的读工具段
		if readN > 1 {  // 单个读工具并发无收益，整批仍走下面的串行路径
			tp, tm := s.runReadToolsParallel(w, flusher, toolCtx, claims.UserID, sessionID, turnID, calls[:readN], mediaRefs)
			turnParts = append(turnParts, tp...)
			msgs = append(msgs, tm...)
			serialFrom = readN
		}
		for _, call := range calls[serialFrom:] {
			// 工具状态条：label 由注册表提供（人话），不含工具名与参数（ 零带内信令）
			label := s.progressLabel(call.Function.Name)
			s.sseEvent(w, flusher, "tool.start", map[string]any{"call_id": call.ID, "label": label})
			out, err := s.invokeTool(toolCtx, claims.UserID, sessionID, turnID, call, mediaRefs)
			if err != nil {
				// 校验前置失败等：把原因回填给模型，本轮继续（模型可修正重试）；用户不见卡
				errText := "工具调用失败: " + err.Error()
				s.sseEvent(w, flusher, "tool.end", map[string]any{"call_id": call.ID, "status": "failed", "text": err.Error()})
				// 失败步骤同样进 parts（展示口径：刷新后能看到这一步失败过）。
				// text 只放「没成功」级信息，不放错误原文——parts 是展示面，错误细节属审计
				// （AI模式设计「时序与事件」：展开看到的是人话状态，不是技术细节）。
				turnParts = append(turnParts, map[string]any{
					"type": "tool", "call_id": call.ID, "label": label, "status": "failed", "text": "没成功",
				})
				msgs = append(msgs, llm.Message{Role: "tool", Content: errText, ToolCallID: call.ID})
				// 失败同样成对进上下文：该 call 在内存 msgs 里已有回执，上下文侧不能缺，
				// 否则两个受众不一致（AI模式设计「上下文组装」· 协议配对）。
				s.appendToolPair(r.Context(), claims.UserID, sessionID, turnID, call, errText)
				continue
			}
			if out.Proposal != nil {
				// 该 call 的步骤也进 parts（展示口径：刷新后与流式中看到的形态一致——
				// 步骤行收口为「待确认」，确认卡跟在同一行）
				turnParts = append(turnParts, map[string]any{
					"type": "tool", "call_id": call.ID, "label": label, "status": "ok", "text": "待确认",
				})
				proposal := s.proposalEvent(r.Context(), out.Proposal, mediaRefs)
				actionID, perr := s.createProposal(r.Context(), claims.UserID, sessionID, turnID, out.Proposal, mediaRefs, turnParts)
				if perr != nil {
					return
				}
				// 提案即该 call 的终态：先补 tool.end 再发 proposal——客户端步骤条
				// 按 call_id 配对收口，缺了它「正在…」会永远挂着（AI模式设计「时序与事件」）。
				// text 不带 out.UserText（「准备新建纪念日…请确认」与标签重复），收口为
				// 「<label> → 待确认」，与提案卡交给用户的状态一致。
				s.sseEvent(w, flusher, "tool.end", map[string]any{"call_id": call.ID, "status": "ok",
					"text": "待确认"})
				proposal["id"] = actionID
				s.sseEvent(w, flusher, "proposal", map[string]any{"proposal": proposal})
				// 提案往返进上下文：模型据此知道「已出卡待确认」，不会重复出卡
				msgs = append(msgs, llm.Message{Role: "tool", Content: out.ModelText, ToolCallID: call.ID})
				mediaRefs = nil // 本轮附件已被这张卡消费（一轮只归一个提案）
				// 一轮只出一张卡：立即停止处理本响应里后续的 tool_calls
				// （AI模式设计「预算与并行」：写工具串行，一旦产出提案即停止本轮）。
				// 用 break 而非 continue——continue 只跳过一个 call，同响应的第二个提案仍会出卡。
				stopForProposal = true
				break
			}
			// 读工具：状态条 + 可选 UI 块；ModelText 进上下文（AI模式设计「工具契约」）
			s.sseEvent(w, flusher, "tool.end", map[string]any{"call_id": call.ID, "status": "ok",
				"text": out.UserText, "ui": out.UI})
			// ui 一并落库：结果卡（含 result_id 句柄）刷新后要能重绘并继续勾选引用
			// （AI模式设计「结果集句柄与点选优先」：句柄随会话存活，翻页结果照常落新句柄）。
			tp := map[string]any{
				"type": "tool", "call_id": call.ID, "label": label, "status": "ok", "text": out.UserText,
			}
			if out.UI != nil {
				tp["ui"] = out.UI
			}
			turnParts = append(turnParts, tp)
			// 模型上下文：读工具往返成对写（assistant 带 tool_calls + tool 回执）——
			// 缺 assistant 行会跨轮留下孤儿 tool 行（AI模式设计「上下文组装」· 协议配对）。
			s.appendToolPair(r.Context(), claims.UserID, sessionID, turnID, call, out.ModelText)
			msgs = append(msgs, llm.Message{Role: "tool", Content: out.ModelText, ToolCallID: call.ID})
		}
		if stopForProposal {
			answered = true
			stopReason = "proposal"
			break
		}
		if budgetHit {
			break
		}
	}
	// 预算耗尽必收尾：轮次/工具数触顶时不能静默 done——再补一次不带工具的调用。
	if shouldWrapUp(answered, budgetHit, stopReason) {
		// 到达这里时 ctx 可能已取消（断连发生在工具执行期间，循环后的断连检查在收尾之前
		// 没有机会跑）。此时收尾调用与后续写库都会失败，必须先按断连落库再返回，
		// 否则 turnParts 全丢且用户消息没有任何中断标记。
		if r.Context().Err() != nil {
			s.persistDisconnectedTurn(claims.UserID, sessionID, turnID, turnParts, "", "", 0)
			return
		}
		msgs = append(msgs, llm.Message{Role: "user",
			Content: "（系统提示）工具调用次数已用尽。请基于以上已获得的信息直接回答用户，并说明哪些部分没能完成，不要再调用工具。"})
		text := "这次没能整理完，请换个说法再试一次。"
		if res, _, cerr := s.chatCall(r.Context(), chain, &llm.ChatRequest{Messages: msgs}, time.Now().Add(wrapUpBudget)); cerr == nil &&
			len(res.Choices) > 0 && strings.TrimSpace(res.Choices[0].Message.Content) != "" {
			text = res.Choices[0].Message.Content
		} else if cerr != nil {
			slog.Warn("预算收尾调用失败", "err", cerr)
		}
		// 收尾调用途中断连：这是循环之外的**第二个**断连出口，必须与循环内同款处理——
		// 把已累积的工具步骤落成 assistant 行并在用户消息上标中断。否则下面的写库会因
		// 请求 ctx 已取消而静默失败（`_, _ =`），turnParts 全丢且没有任何中断标记。
		// 兜底文案不算「已产生内容」，故不并入 parts（只保留真实的工具步骤）。
		if r.Context().Err() != nil {
			s.persistDisconnectedTurn(claims.UserID, sessionID, turnID, turnParts, "", "", 0)
			return
		}
		{
			parts := append(turnParts, map[string]any{"type": "text", "text": text})
			pj, _ := json.Marshal(parts)
			_, _ = s.db.SaveChatMessage(r.Context(), &store.ChatMessage{UserID: claims.UserID, SessionID: sessionID, TurnID: turnID, Role: "assistant", Parts: string(pj)})
		}
		// 收尾回答也是助手正文，同样要进模型上下文（与循环内 :777 的 dialogue 写入同口径），
		// 否则用户下一轮引用「你刚才的总结」时模型看不到它。
		if t := strings.TrimSpace(text); t != "" {
			_ = s.db.AppendContext(r.Context(), claims.UserID, sessionID, turnID,
				"assistant", text, "", "", "", store.ApproxTokens(text), "dialogue", false)
		}
		s.sseEvent(w, flusher, "answer.delta", map[string]any{"text": text})
		s.sseEvent(w, flusher, "answer.end", map[string]any{})
		stopReason = "budget"
	}
	// 服务器主动收尾必须是预算类；正常/提案路径已在循环内置位。
	if stopReason == "aborted" && answered {
		stopReason = "answer"
	}
	s.sseEvent(w, flusher, "turn.end", map[string]any{"stop_reason": stopReason})
}

// toolNameOf 取工具名（call.Function.Name 的取值包装，便于测试与日志统一）。
func toolNameOf(call llm.ToolCall) string { return call.Function.Name }

// appendToolPair 把一次读工具的往返按协议配对写进模型上下文：
// 先 assistant 行（**单元素** tool_calls_json），再 tool 行（同 tool_call_id）。
//
// AI模式设计「上下文组装」· 协议配对：`tool_calls_json` 存 assistant 轮的 name+arguments，
// 回放时原样还原配对。严格要求下（OpenAI / Anthropic）`role=tool` 必须紧跟带同 id
// `tool_calls` 的 assistant 消息，孤儿 tool 行属协议违规。
//
// 三条约束（改动前务必读）：
//  1. **逐 call 写，不得把整响应写成一条 assistant 行**——同一响应里有的 call 失败
//     （不产生回执）、有的 call 因出提案 `break` 而未执行，整响应写就会出现
//     「声明 N 个 tool_calls、只有部分回执」。
//  2. **只在拿到该 call 的终局结果之后写**——执行前写 assistant，出提案的 call 会
//     `break` 且不写 tool 行，同样留下无回执的 tool_calls。
//  3. 出提案的 call 两侧都不写：其往返由 recordDecision 以合成 id `card-<n>` 自成一对
//     （service.go recordDecision），与真实 call.ID 不同名，先写的 assistant 永远配不上。
func (s *Service) appendToolPair(ctx context.Context, userID, sessionID int64, turnID string, call llm.ToolCall, toolContent string) {
	tc, _ := json.Marshal([]llm.ToolCall{call})
	_ = s.db.AppendContext(ctx, userID, sessionID, turnID,
		"assistant", "", string(tc), "", "", store.ApproxTokens(string(tc)), "read_result", false)
	_ = s.db.AppendContext(ctx, userID, sessionID, turnID,
		"tool", toolContent, "", call.ID, toolNameOf(call), store.ApproxTokens(toolContent), "read_result", false)
}

// readToolOutcome 并行读工具的结果槽位（按发起顺序归位，不由完成顺序决定）。
type readToolOutcome struct {
	out *tools.ToolResult
	err error
}

// runReadToolsParallel 并行执行一组**读工具**调用（AI模式设计「预算与并行」）。
//
// 三条不变式（改动前先读）：
//  1. **只并行读工具**：调用方只把「首个写工具之前」那段传进来——出提案会中止其后所有
//     call，把提案之后的读工具也跑掉就是执行了本不该执行的调用。
//  2. **parts / 上下文 / msgs 按发起顺序写**：结果先落到按 index 预分配的槽位，
//     `Wait()` 之后在**主 goroutine** 里按发起顺序消费——顺序由发起决定，不由完成决定。
//  3. **goroutine 内只调 invokeTool**：不发事件、不写 parts、不写上下文。
//     这条是硬约束：`sseEvent` 直接写 http.ResponseWriter（不可并发使用），
//     本函数的所有事件都由主 goroutine 发（start 先按序发完，end 在 Wait 后按序发），
//     所以不需要给 sseEvent 加锁。若将来某个工具自身要发事件，必须先把事件队列化回主
//     goroutine，或改成每轮一个写入器——不要直接在工作 goroutine 里发。
//
// 返回值交给调用方按序 append（保持与串行路径完全一致的顺序语义）。
func (s *Service) runReadToolsParallel(w http.ResponseWriter, flusher http.Flusher, ctx context.Context,
	userID, sessionID int64, turnID string, calls []llm.ToolCall, mediaRefs []any) ([]map[string]any, []llm.Message) {
	outcomes := make([]readToolOutcome, len(calls))
	// 状态条按发起顺序先发（此时还没启动任何工具，顺序确定）。
	for _, call := range calls {
		s.sseEvent(w, flusher, "tool.start", map[string]any{"call_id": call.ID, "label": s.progressLabel(call.Function.Name)})
	}
	var wg sync.WaitGroup
	for i := range calls {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out, err := s.invokeTool(ctx, userID, sessionID, turnID, calls[i], mediaRefs)
			outcomes[i] = readToolOutcome{out: out, err: err}
		}(i)
	}
	wg.Wait()

	toolParts := make([]map[string]any, 0, len(calls))
	toolMsgs := make([]llm.Message, 0, len(calls))
	for i, call := range calls {
		label := s.progressLabel(call.Function.Name)
		oc := outcomes[i]
		if oc.err != nil {
			// 与串行路径同款：把原因回填给模型，本轮继续（模型可修正重试）；用户不见卡。
			errText := "工具调用失败: " + oc.err.Error()
			s.sseEvent(w, flusher, "tool.end", map[string]any{"call_id": call.ID, "status": "failed", "text": oc.err.Error()})
			toolParts = append(toolParts, map[string]any{
				"type": "tool", "call_id": call.ID, "label": label, "status": "failed", "text": "没成功",
			})
			toolMsgs = append(toolMsgs, llm.Message{Role: "tool", Content: errText, ToolCallID: call.ID})
			s.appendToolPair(ctx, userID, sessionID, turnID, call, errText)
			continue
		}
		out := oc.out
		// 只把非写工具送进并行段，理论上读工具不会产出提案；真出现即注册表分类与
		// 工具行为不一致——此时**不能静默当读结果处理**（会丢掉确认卡），
		// 按失败步骤回填给模型并留日志，让不一致显形。
		if out.Proposal != nil {
			slog.Warn("读工具返回了提案：注册表 IsWrite 分类与工具行为不一致",
				"tool", call.Function.Name, "call_id", call.ID)
			errText := "工具调用失败: 该工具误配为读工具却要出确认卡"
			s.sseEvent(w, flusher, "tool.end", map[string]any{"call_id": call.ID, "status": "failed", "text": errText})
			toolParts = append(toolParts, map[string]any{
				"type": "tool", "call_id": call.ID, "label": label, "status": "failed", "text": "没成功",
			})
			toolMsgs = append(toolMsgs, llm.Message{Role: "tool", Content: errText, ToolCallID: call.ID})
			s.appendToolPair(ctx, userID, sessionID, turnID, call, errText)
			continue
		}
		s.sseEvent(w, flusher, "tool.end", map[string]any{"call_id": call.ID, "status": "ok",
			"text": out.UserText, "ui": out.UI})
		tp := map[string]any{
			"type": "tool", "call_id": call.ID, "label": label, "status": "ok", "text": out.UserText,
		}
		if out.UI != nil {
			tp["ui"] = out.UI
		}
		toolParts = append(toolParts, tp)
		s.appendToolPair(ctx, userID, sessionID, turnID, call, out.ModelText)
		toolMsgs = append(toolMsgs, llm.Message{Role: "tool", Content: out.ModelText, ToolCallID: call.ID})
	}
	return toolParts, toolMsgs
}

// progressLabel 工具状态条人话标签（注册表 ProgressLabel）；未知工具回退通用文案。
func (s *Service) progressLabel(toolName string) string {
	if t, ok := s.reg[toolName]; ok && t.ProgressLabel != "" {
		return t.ProgressLabel
	}
	return "处理中"
}

// invokeTool 执行一次模型工具调用。
//   - 读工具：立即执行（ModeExecute），返回 ModelText/UserText/UI；
//   - 提案工具：ModePropose（校验 + 自构造 Preview + 展开 source），返回 Proposal；
//     出卡时记审计（action=工具名, result=pending），裁决时另记一条（见 ResolveProposal）。
func (s *Service) invokeTool(ctx context.Context, userID, sessionID int64, turnID string, call llm.ToolCall, mediaRefs []any) (*tools.ToolResult, error) {
	tool, ok := s.reg[call.Function.Name]
	if !ok {
		return nil, fmt.Errorf("未知工具: %s", call.Function.Name)
	}
	// 后台停用的工具拒绝执行（模型可能凭历史上下文继续调用）
	if disabled, derr := s.db.DisabledTools(ctx); derr == nil && disabled[tool.Name] {
		return nil, fmt.Errorf("该工具已在后台停用: %s", tool.Name)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return nil, fmt.Errorf("参数解析失败: %w", err)
	}
	// 顺序重要：必须先挂 media_refs 再校验，否则「用户只发图不说字」的纯媒体动态会被误拒
	//（媒体在确认时才转正，卡阶段只能以引用形式存在）。
	if tool.IsWrite && len(mediaRefs) > 0 {
		args["media_refs"] = mediaRefs
	}
	mode := tools.ModeExecute
	if tool.IsWrite {
		mode = tools.ModePropose
	}
	startAt := time.Now()
	out, err := tool.Invoke(ctx, s.deps, userID, args, mode)
	latencyMs := time.Since(startAt).Milliseconds()
	if err != nil {
		s.deps.AuditWithLatency(ctx, userID, tool.Name, call.Function.Arguments, tools.AuditFailure(err), latencyMs)
		return nil, err
	}
	if tool.IsWrite {
		// 出卡审计：result=pending（「AI 提议了什么但未被确认」也可追溯）
		// 摘要取自**出卡归一化后**的参数：source 句柄在这里已展开成具体 ids，
		// 审计才能记到「kind + 条数 + 前 3 个 id」；digest 仍是模型原始入参。
		// 提案可能为 nil（写工具异常返回），此时传 nil 由审计退回原始入参摘要，不解引用。
		var proposalParams map[string]any
		if out.Proposal != nil {
			proposalParams = out.Proposal.Params
		}
		s.deps.AuditWithLatencyParams(ctx, userID, tool.Name, call.Function.Arguments, proposalParams, tools.AuditPending, latencyMs)
	} else {
		s.deps.AuditWithLatency(ctx, userID, tool.Name, call.Function.Arguments, tools.AuditSuccess, latencyMs)
	}
	_ = turnID // 卡片载体消息在 createProposal 内落库，需要 turn 归属（回滚）
	return out, nil
}

// createProposal 落库提案（params_json 存展开后的执行参数；preview_json 存自构造预览）
// 与载体消息（card_id 关联，content 为空——卡片本体由 preview 渲染，不需要文字载体）。
func (s *Service) createProposal(ctx context.Context, userID, sessionID int64, turnID string, p *tools.Proposal, mediaRefs []any, turnParts []map[string]any) (int64, error) {
	params, err := json.Marshal(p.Params)
	if err != nil {
		return 0, err
	}
	previewJSON, err := json.Marshal(p)
	if err != nil {
		return 0, err
	}
	actionID, err := s.db.CreateConfirmAction(ctx, userID, p.Intent, string(params), string(previewJSON))
	if err != nil {
		return 0, err
	}
	// parts 展示口径：本轮的思考与工具步骤 + 卡片指针，全在同一行（刷新后可展开）。
	parts := append(append([]map[string]any{}, turnParts...), map[string]any{"type": "card", "action_id": actionID})
	partsJSON, _ := json.Marshal(parts)
	if _, err := s.db.SaveChatMessage(ctx, &store.ChatMessage{
		UserID: userID, SessionID: sessionID, TurnID: turnID, Role: "assistant",
		Parts:  string(partsJSON),
		CardID: sql.NullInt64{Int64: actionID, Valid: true},
	}); err != nil {
		return 0, err
	}
	// 模型上下文：提案在 pending 时不进（未发生的操作不向模型透露，AI模式设计「上下文组装」）；
	// resolve 裁决后一次性写入 pinned 往返（见 recordDecision）。
	return actionID, nil
}

// proposalEvent 组装 proposal 事件载荷（与 preview_json 同源 + expires_in）。
func (s *Service) proposalEvent(ctx context.Context, p *tools.Proposal, mediaRefs []any) map[string]any {
	atts := p.Attachments
	if len(atts) == 0 && len(mediaRefs) > 0 {
		atts = refsToAttachments(mediaRefs)
	}
	return map[string]any{
		"intent":      p.Intent,
		"summary":     p.Summary,
		"preview":     p.Preview,
		"warnings":    p.Warnings,
		"attachments": atts,
		"expires_in":  proposalTTLSeconds,
	}
}

// refsToAttachments 卡阶段引用元数据（media_refs）→ 事件附件清单。
func refsToAttachments(mediaRefs []any) []tools.AttachmentRef {
	out := make([]tools.AttachmentRef, 0, len(mediaRefs))
	for _, r := range mediaRefs {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		ref := tools.AttachmentRef{}
		if s, ok := m["name"].(string); ok {
			ref.Name = s
		}
		if s, ok := m["type"].(string); ok {
			ref.Type = s
		}
		if f, ok := m["size"].(float64); ok {
			ref.Size = int64(f)
		}
		out = append(out, ref)
	}
	return out
}

// ResolveProposal POST /api/v1/chat/proposals/{id}/resolve {decision, media_tokens?}。
// 裁决口径：approve 原子认领（UPDATE … WHERE status='pending' AND 未过期）→ 执行
// （ModeExecute）→ 审计 + 回执落库；cancel 只置状态。
// 客户端据此即时拿到 record（kind+id）做「已发布」反馈，不必等下一轮模型回答。
func (s *Service) ResolveProposal(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, `{"error":"提案不存在"}`, http.StatusNotFound)
		return
	}
	var req struct {
		Decision    string   `json:"decision"`
		MediaTokens []string `json:"media_tokens"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"请求格式错误"}`, http.StatusBadRequest)
		return
	}
	if req.Decision != "approve" && req.Decision != "cancel" {
		http.Error(w, `{"error":"decision 仅支持 approve / cancel"}`, http.StatusBadRequest)
		return
	}
	action, err := s.db.GetConfirmAction(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && action.UserID != claims.UserID) {
		http.Error(w, `{"error":"提案不存在"}`, http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, `{"error":"服务器错误"}`, http.StatusInternalServerError)
		return
	}
	// 会话/轮次归属：回执落在载体消息所在会话与轮次
	sessionID, _ := s.db.SessionIDOfCard(r.Context(), id)
	turnIDs, _ := s.db.TurnIDsOfCard(r.Context(), id)
	carrierTurn := ""
	if len(turnIDs) > 0 {
		carrierTurn = turnIDs[0]
	}

	// 裁决执行期间该会话不可删（见 beginResolve）：覆盖取消与确认两条路径——
	// 两条都会在卡片状态已变更后写回执，都在同一个窗口里。
	resolveKey := s.beginResolve(claims.UserID, sessionID, id)
	defer s.endResolve(claims.UserID, resolveKey)

	if req.Decision == "cancel" {
		if err := s.db.ResolveConfirmAction(r.Context(), id, "cancelled"); err != nil {
			if errors.Is(err, store.ErrConfirmExpired) {
				// 过期前置裁决：置 expired 并回填上下文（口径「已裁决提案归一：过期→未执行」）
				if uerr := s.db.UpdateConfirmStatus(r.Context(), id, "expired"); uerr == nil {
					s.recordDecision(r.Context(), claims.UserID, action, sessionID, carrierTurn, "expired", "确认卡片已过期，未执行")
				}
			}
			s.resolveConflict(w, err)
			return
		}
		// 裁决时点审计（出卡时已记 pending；见 invokeTool）——取消也要可追溯
		s.deps.AuditWithLatency(r.Context(), claims.UserID, action.ToolName, action.ParamsJSON, "cancelled", 0)
		s.recordDecision(r.Context(), claims.UserID, action, sessionID, carrierTurn, "cancelled", "用户取消了该操作，未执行")
		s.writeJSON(w, http.StatusOK, map[string]any{"status": "cancelled"})
		return
	}

	tool, ok := s.reg[action.ToolName]
	if !ok {
		http.Error(w, `{"error":"未知工具"}`, http.StatusInternalServerError)
		return
	}
	// 入参把关**必须在原子认领之前**：认领是消耗性的（pending→confirmed 单次有效），
	// 若先认领再发现入参不合法，卡片就被吃掉却从未执行——历史回显按 confirmed→executed
	// 归一，会显示成「已执行」，属 AI模式设计「提案契约」明确排除的形态
	// （原子认领 → 事务执行 → 回写结果；失败置 failed 并记原因）。
	if len(req.MediaTokens) > MaxChatMedia {
		// 单条动态媒体数上限与发布页同口径（PRODUCT「发布动态」：媒体单条最多 9 个）。
		// 正常链路受 MaxChatMedia 间接约束（发消息时校验元数据数量），但本接口可被直接调用。
		http.Error(w, fmt.Sprintf(`{"error":"单条动态最多 %d 个媒体文件"}`, MaxChatMedia), http.StatusBadRequest)
		return
	}
	// 原子认领：单次有效 + 未过期（并发双确认只有一个能赢）
	if err := s.db.ResolveConfirmAction(r.Context(), id, "confirmed"); err != nil {
		if errors.Is(err, store.ErrConfirmExpired) {
			if uerr := s.db.UpdateConfirmStatus(r.Context(), id, "expired"); uerr == nil {
				s.recordDecision(r.Context(), claims.UserID, action, sessionID, carrierTurn, "expired", "确认卡片已过期，未执行")
			}
		}
		s.resolveConflict(w, err)
		return
	}

	var args map[string]any
	_ = json.Unmarshal([]byte(action.ParamsJSON), &args)
	if args == nil {
		args = map[string]any{}
	}
	if len(req.MediaTokens) > 0 {
		args["media_tokens"] = req.MediaTokens
	}
	toolCtx := tools.WithSessionID(r.Context(), sessionID)
	startAt := time.Now()
	out, err := tool.Invoke(toolCtx, s.deps, claims.UserID, args, tools.ModeExecute)
	latencyMs := time.Since(startAt).Milliseconds()
	if err != nil {
		s.deps.AuditWithLatency(r.Context(), claims.UserID, action.ToolName, action.ParamsJSON, tools.AuditFailure(err), latencyMs)
		if ferr := s.db.ResolveConfirmAction(r.Context(), id, "failed"); ferr != nil {
			slog.Warn("proposal failed 回写失败", "action_id", id, "err", ferr)
		}
		// recordDecision 走审计（内部人能看原文）；回给客户端的 error 会**逐字渲染在卡片上**
		// （APP CardActions 显示 card.result），故只给人话。工具错误原文进服务端日志。
		slog.Error("提案执行失败", "action_id", id, "tool", action.ToolName, "err", err)
		s.recordDecision(r.Context(), claims.UserID, action, sessionID, carrierTurn, "failed", err.Error())
		s.writeJSON(w, http.StatusOK, map[string]any{"status": "failed", "error": "操作没能完成，请稍后重试"})
		return
	}
	s.deps.AuditWithLatency(r.Context(), claims.UserID, action.ToolName, action.ParamsJSON, tools.AuditSuccess, latencyMs)
	s.recordDecision(r.Context(), claims.UserID, action, sessionID, carrierTurn, "executed", out.ModelText)
	resp := map[string]any{"status": "executed", "result": out.ModelText}
	if out.Record != nil {
		resp["record"] = out.Record
	}
	s.writeJSON(w, http.StatusOK, resp)
}

// resolveConflict 认领失败的错误分类：过期 410 / 已裁决 409。
func (s *Service) resolveConflict(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrConfirmExpired):
		s.writeJSON(w, http.StatusGone, map[string]string{"error": "确认卡片已过期（15 分钟），请重新发起"})
	case errors.Is(err, store.ErrConfirmResolved):
		s.writeJSON(w, http.StatusConflict, map[string]string{"error": "卡片已处理过（单次有效）"})
	default:
		http.Error(w, `{"error":"服务器错误"}`, http.StatusInternalServerError)
	}
}

// recordDecision 裁决回执落库：tool_result 行（供历史与模型上下文回放）+ 会话活跃时间推进。
func (s *Service) recordDecision(ctx context.Context, userID int64, action *store.ConfirmAction, sessionID int64, carrierTurn, status, text string) {
	if sessionID > 0 {
		if err := s.db.TouchChatSession(ctx, sessionID); err != nil {
			slog.Warn("touch chat session failed", "session_id", sessionID, "err", err)
		}
	}
	{
		pj, _ := json.Marshal([]map[string]any{
			{"type": "tool", "call_id": "", "label": action.ToolName, "status": status, "text": text},
		})
		if _, err := s.db.SaveChatMessage(ctx, &store.ChatMessage{
			UserID: userID, SessionID: sessionID, TurnID: carrierTurn, Role: "assistant",
			Parts: string(pj), CardID: sql.NullInt64{Int64: action.ID, Valid: true},
		}); err != nil {
			slog.Warn("tool_result 落库失败", "action_id", action.ID, "status", status, "err", err)
		}
		// 模型上下文：已裁决提案往返（proposal 档，pinned——永不淘汰）。
		// status 口径（executed/cancelled/failed/expired）→ 归一结果文本：
		// executed=执行结果原文；failed=「执行失败：原因」；cancelled/expired=固定文案。
		outcome := text
		switch status {
		case "failed":
			if text != "" {
				outcome = "执行失败：" + text
			} else {
				outcome = "执行失败"
			}
		case "cancelled":
			outcome = "用户取消了该操作，未执行"
		case "expired":
			outcome = "确认卡片已过期，未执行"
		}
		tc, _ := json.Marshal([]map[string]any{{"id": fmt.Sprintf("card-%d", action.ID), "type": "function",
			"function": map[string]any{"name": action.ToolName, "arguments": action.ParamsJSON}}})
		_ = s.db.AppendContext(ctx, userID, sessionID, carrierTurn,
			"assistant", "", string(tc), "", "", store.ApproxTokens(string(tc)), "proposal", true)
		_ = s.db.AppendContext(ctx, userID, sessionID, carrierTurn,
			"tool", outcome, "", fmt.Sprintf("card-%d", action.ID), action.ToolName, store.ApproxTokens(outcome), "proposal", true)
	}
}

// persistDisconnectedTurn 意外断连时把本轮已产生内容落库保留（AI模式设计「停止与断连」：
// 已下发的增量与已完成的工具步骤按 parts 落库保留并标 interrupted；
// 「落库时机」节同款「中途断连不产生半截行，已下发的内容按 parts 完整保留」）。
//
// 与「用户主动停止」的区别：停止 = 整轮回滚（AbortTurn 删除全轮行），本函数只服务意外断连。
// 因此写库前先查中止登记；写完再查一次，若期间用户点了停止则删掉自己刚写的行（幂等收敛），
// 避免与 AbortTurn 的 DELETE 并发后留下孤儿 assistant 行。
//
// 请求 context 此时已取消，落库必须用独立短超时 context（与 markTurnAborted 同款做法）。
func (s *Service) persistDisconnectedTurn(userID, sessionID int64, turnID string, turnParts []map[string]any,
	reasoning, answer string, thinkSeconds int) {
	if s.abortRequested(userID, turnID) {
		return // 用户主动停止：整轮回滚，不保留任何内容
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// parts 契约顺序：思考 → 工具* → 正文（AI模式设计「消息 parts 契约」）。
	// 本轮的思考**必然**还没进 turnParts——本轮只在 chatCallStream 正常返回后才 append
	// 思考 part，而断连分支在那之前就 return 了，故这里无需去重判断，直接续在末尾即可
	// （续在末尾 = 按发生顺序：turnParts 里是更早各轮的步骤，本轮思考发生在它们之后）。
	parts := make([]map[string]any, 0, len(turnParts)+2)
	parts = append(parts, turnParts...)
	if reasoning != "" {
		parts = append(parts, map[string]any{"type": "thinking", "text": reasoning, "seconds": thinkSeconds})
	}
	if answer != "" {
		parts = append(parts, map[string]any{"type": "text", "text": answer})
	}
	// 全空（连一个字都没下发、也没跑完任何工具）→ 只标中断，不写空 assistant 行。
	if len(parts) > 0 {
		pj, err := json.Marshal(parts)
		if err != nil {
			slog.Warn("断连落库序列化失败", "session_id", sessionID, "turn_id", turnID, "err", err)
		} else {
			_, err = s.db.SaveChatMessage(ctx, &store.ChatMessage{
				UserID: userID, SessionID: sessionID, TurnID: turnID, Role: "assistant", Parts: string(pj),
			})
			if err != nil {
				slog.Warn("断连落库失败", "session_id", sessionID, "turn_id", turnID, "err", err)
			}
		}
	}
	// 写后再查：期间用户点了停止 → AbortTurn 已删全轮，这里补删自己刚写的行。
	if s.abortRequested(userID, turnID) {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM chat_messages WHERE user_id = ? AND turn_id = ? AND role = 'assistant'`,
			userID, turnID); err != nil {
			slog.Warn("断连回滚失败", "session_id", sessionID, "turn_id", turnID, "err", err)
		}
		return
	}
	s.markTurnAborted(userID, sessionID, turnID)
}

// markTurnAborted 连接断开后给本轮用户消息打标（不删，回显带「已中断」）。
// 标记即以 parts 追加一个 interrupted part（与 parts 契约一致，不加列、不加 role）。
// 用独立 context：调用时请求 context 已取消，沿用它写库必失败。
func (s *Service) markTurnAborted(userID, sessionID int64, turnID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var (
		id    int64
		parts string
	)
	err := s.db.QueryRowContext(ctx, `SELECT id, parts FROM chat_messages
		WHERE user_id = ? AND session_id = ? AND turn_id = ? AND role = 'user'
		ORDER BY id LIMIT 1`, userID, sessionID, turnID).Scan(&id, &parts)
	if err != nil {
		slog.Warn("断连标记读取失败", "session_id", sessionID, "turn_id", turnID, "err", err)
		return
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(parts), &list); err != nil {
		slog.Warn("断连标记解析失败", "session_id", sessionID, "turn_id", turnID, "err", err)
		return
	}
	for _, p := range list {
		if p["type"] == "interrupted" {
			return // 幂等：已有标记不重复追加
		}
	}
	list = append(list, map[string]any{"type": "interrupted"})
	pj, err := json.Marshal(list)
	if err != nil {
		return
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE chat_messages SET parts = ? WHERE id = ?`, string(pj), id); err != nil {
		slog.Warn("断连标记落库失败", "session_id", sessionID, "turn_id", turnID, "err", err)
	}
}

// Abort POST /api/v1/chat/turns/{turn_id}/abort：用户显式中止本轮 → 整轮回滚。
func (s *Service) Abort(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	turnID := r.PathValue("turn_id")
	if turnID == "" {
		http.Error(w, `{"error":"缺少 turn_id"}`, http.StatusBadRequest)
		return
	}
	// 先登记「用户主动停止」，再执行整轮回滚：服务端断连分支据此区分
	// 「停止」与「意外断连」，不会在回滚之后又把内容写回来（见 persistDisconnectedTurn）。
	s.markAbortIntent(claims.UserID, turnID)
	ok, err := s.db.AbortTurn(r.Context(), claims.UserID, turnID)
	if err != nil {
		s.clearAbortIntent(claims.UserID, turnID)
		http.Error(w, `{"error":"服务器错误"}`, http.StatusInternalServerError)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"aborted": ok})
}

// DeleteTurn DELETE /api/v1/chat/turns/{turn_id}：回收「已中断」残局（编辑重发前调用）。
// 与 Abort 同一套整轮回滚（AbortTurn：删本轮消息与模型上下文、pending 卡置 cancelled），
// 区别仅在用途——turn 已结束，无需登记中止意图（登记了反而会让迟到的断连落库分支误判）。
func (s *Service) DeleteTurn(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	turnID := r.PathValue("turn_id")
	if turnID == "" {
		http.Error(w, `{"error":"缺少 turn_id"}`, http.StatusBadRequest)
		return
	}
	ok, err := s.db.AbortTurn(r.Context(), claims.UserID, turnID)
	if err != nil {
		http.Error(w, `{"error":"服务器错误"}`, http.StatusInternalServerError)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"deleted": ok})
}

// sessionTitleFrom 会话自动命名：前 20 字 + 超长省略号；纯媒体空文本回退默认名。
func sessionTitleFrom(content string) string {
	t := strings.TrimSpace(content)
	if t == "" {
		return "新的会话"
	}
	return textutil.TruncMark(t, 20)
}

// ListSessions GET /api/v1/chat/sessions：当前用户的会话列表（updated_at 倒序）。
func (s *Service) ListSessions(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	list, err := s.db.ListChatSessions(r.Context(), claims.UserID)
	if err != nil {
		http.Error(w, `{"error":"服务器错误"}`, http.StatusInternalServerError)
		return
	}
	type sessItem struct {
		ID        int64  `json:"id"`
		Title     string `json:"title"`
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
	}
	items := make([]sessItem, 0, len(list))
	for _, sess := range list {
		items = append(items, sessItem{ID: sess.ID, Title: sess.Title, CreatedAt: sess.CreatedAt, UpdatedAt: sess.UpdatedAt})
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// DeleteSession DELETE /api/v1/chat/sessions/{id}：硬删（会话 + 消息 + 结果集事务同删）。
func (s *Service) DeleteSession(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, `{"error":"会话不存在"}`, http.StatusNotFound)
		return
	}
	// 待确认提案存在时拒绝删除：那是一张即将生效的写操作卡，随会话硬删即无声取消
	// （AI模式设计「会话与历史回显」；数据模型 ConfirmAction）。
	if n, err := s.db.CountPendingProposals(r.Context(), claims.UserID, id); err == nil && n > 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"会话有待确认的操作卡片，请先处理后再删除"}`))
		return
	}
	// 流式进行中拒绝删除：本轮 turn 尚未终结，会话行会在本轮落库前被删掉
	// （AI模式设计「会话与历史回显」：turn_id 未终结一律拒绝）。
	// 「查活跃轮」与「删会话」同锁：否则本轮登记能插在两者之间，本轮的消息会写进已删会话。
	s.turnMu.Lock()
	if s.sessionTurnActive(claims.UserID, id) {
		s.turnMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"会话正在生成回复，请先停止后再删除"}`))
		return
	}
	deleted, err := s.db.DeleteChatSession(r.Context(), claims.UserID, id)
	s.turnMu.Unlock()
	if err != nil {
		http.Error(w, `{"error":"服务器错误"}`, http.StatusInternalServerError)
		return
	}
	if !deleted {
		http.Error(w, `{"error":"会话不存在"}`, http.StatusNotFound)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// histItem 历史回显条目（形状：一条消息一行；后续换 parts）。
type histItem struct {
	ID   int64  `json:"id"`
	Role string `json:"role"`
	// parts 直出：客户端按 part.type 渲染（AI模式设计「消息 parts 契约」）
	Parts     json.RawMessage   `json:"parts"`
	CardID    int64             `json:"card_id,omitempty"`
	Card      *proposalSnapshot `json:"card,omitempty"`
	CreatedAt string            `json:"created_at"`
	Quoted    json.RawMessage   `json:"quoted,omitempty"`
	// 归属轮次：客户端「编辑重发」据它回收中断残局（DeleteTurn）后再回填输入框。
	TurnID string `json:"turn_id,omitempty"`
	// 本轮未答完即中断（意外断线、未发 abort）：消息不删，但客户端要知道这里不会再来下文。
	Interrupted bool `json:"interrupted,omitempty"`
}

// proposalSnapshot 历史回显的提案快照：直接读 preview_json（工具自构造，永不反推）。
type proposalSnapshot struct {
	ActionID    int64                 `json:"action_id"`
	Intent      string                `json:"intent"`
	Summary     string                `json:"summary"`
	Preview     tools.ProposalPreview `json:"preview"`
	Warnings    []string              `json:"warnings,omitempty"`
	Attachments []tools.AttachmentRef `json:"attachments,omitempty"`
	Status      string                `json:"status"` // pending / executed / cancelled / expired / failed
	ExpiresIn   int                   `json:"expires_in"`
	Result      string                `json:"result,omitempty"`
}

// History GET /api/v1/chat/history?session_id=N：按会话回显；缺省/0 → 空列表。
func (s *Service) History(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.UserFromContext(r.Context())
	sessionID, _ := strconv.ParseInt(r.URL.Query().Get("session_id"), 10, 64)
	if sessionID <= 0 {
		s.writeJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		return
	}
	list, err := s.db.ListChatMessages(r.Context(), claims.UserID, sessionID, 100)
	if err != nil {
		http.Error(w, `{"error":"服务器错误"}`, http.StatusInternalServerError)
		return
	}
	cardIDs := make([]int64, 0)
	for _, m := range list {
		if m.CardID.Valid && m.CardID.Int64 > 0 {
			cardIDs = append(cardIDs, m.CardID.Int64)
		}
	}
	cards, _ := s.db.ListConfirmActionsByIDs(r.Context(), cardIDs)
	results, _ := s.db.CardResults(r.Context(), cardIDs)

	items := make([]histItem, 0, len(list))
	for _, m := range list {
		cardID := int64(0)
		if m.CardID.Valid {
			cardID = m.CardID.Int64
		}
		quoted := json.RawMessage("")
		if m.Quoted != "" && m.Quoted != "null" {
			quoted = json.RawMessage(m.Quoted)
		}
		var card *proposalSnapshot
		if cardID > 0 && cards != nil {
			if ca, ok := cards[cardID]; ok {
				// 卡片本体行以卡片形态回显；预览读 preview_json（提案自包含，永不反推）
				var p tools.Proposal
				_ = json.Unmarshal([]byte(ca.PreviewJSON), &p)
				expiresIn := int(time.Until(ca.ExpiresAt).Seconds())
				if expiresIn < 0 {
					expiresIn = 0
				}
				status := ca.Status
				switch {
				case status == "confirmed":
					status = "executed" // 对齐客户端口径
				case status == "pending" && !time.Now().Before(ca.ExpiresAt):
					status = "expired" // 过期前置裁决：回显即只读「已过期」
				}
				card = &proposalSnapshot{
					ActionID: ca.ID, Intent: p.Intent, Summary: p.Summary, Preview: p.Preview,
					Warnings: p.Warnings, Attachments: p.Attachments,
					Status: status, ExpiresIn: expiresIn, Result: results[cardID],
				}
			}
		}
		items = append(items, histItem{ID: m.ID, Role: m.Role, Parts: json.RawMessage(m.Parts),
			CardID: cardID, Card: card, CreatedAt: m.CreatedAt, Quoted: quoted, TurnID: m.TurnID})
	}
	// 中断标记：断连时已在 parts 里追加 interrupted part（markTurnAborted），这里只读取。
	// 逐条独立判定（不只认尾条）：后面又有新消息时，早先被中断的那轮仍要显示「已中断」。
	for i := range items {
		if items[i].Role != "user" {
			continue
		}
		var mp []map[string]any
		if json.Unmarshal(items[i].Parts, &mp) != nil {
			continue
		}
		for _, p := range mp {
			if p["type"] == "interrupted" {
				items[i].Interrupted = true
				break
			}
		}
	}
	// 兜底：尾条是无人回答的 user（断在标记落库之前）：同样视为中断。
	if n := len(items); n > 0 && items[n-1].Role == "user" && !items[n-1].Interrupted {
		items[n-1].Interrupted = true
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// contextBudget 可用历史 token 预算 = 链首模型的上下文窗口 − 输出预留。
//
// 取链首模型：上下文在请求前只构建一次（选链后立即组装），链内后续模型窗口更小也不重建——
// 重建会改变已发出请求的内容，与「流式下切换窗口只到首个增量之前」冲突。这是已知近似。
// 返回 0 表示未声明窗口，由 buildModelHistory 回落兜底预算（system 段与工具定义不计入，亦为近似）。
func contextBudget(chain []*chatTarget) int {
	if len(chain) == 0 || chain[0].ContextWindow == nil {
		return 0
	}
	const defaultReserve = 4096 // 模型未配最大输出时的输出预留
	reserve := defaultReserve
	if chain[0].MaxTokens != nil {
		reserve = int(*chain[0].MaxTokens)
	}
	budget := int(*chain[0].ContextWindow) - reserve
	if budget < 1024 {
		budget = 1024 // 下限：配错（窗口小于输出预留）也不至于把上下文压成空
	}
	return budget
}

// buildModelHistory 组装模型上下文：取自 chat_context（模型事实源），按 token 预算裁剪。
//   - pinned（已裁决提案往返）永不淘汰；read_result 可淘汰，淘汰后留一行残留摘要进 system；
//   - pending 卡片不进（未发生的操作不向模型透露）；thinking/UI 载荷从不进上下文。
func (s *Service) buildModelHistory(ctx context.Context, userID, sessionID int64, tokenBudget int) ([]llm.Message, string) {
	rows, err := s.db.ListContext(ctx, sessionID)
	if err != nil {
		slog.Warn("加载模型上下文失败", "session_id", sessionID, "err", err)
		return nil, ""
	}
	// token 预算裁剪（AI模式设计「上下文组装」）：system 段不算预算；从最新往回收，
	// pinned 永不淘汰，read_result 超预算时先淘汰并计数。
	//
	// 裁剪以「配对单元」为单位（assistant 带 tool_calls 与其后续 tool 行同进同出）：
	// 逐行淘汰会把这一对拆开，重新造出孤儿 tool 行（AI模式设计「上下文组装」· 协议配对）。
	const contextTokenBudget = 24000 // 兜底：模型未声明上下文窗口时用
	if tokenBudget <= 0 {
		tokenBudget = contextTokenBudget
	}
	type ctxUnit struct {
		rows   []store.ContextRow
		tokens int
		pinned bool
		isRead bool
	}
	units := make([]ctxUnit, 0, len(rows))
	for i := 0; i < len(rows); i++ {
		r := rows[i]
		if r.Kind == "system" {
			continue // system 段由 promptTail 组装，不进上下文行
		}
		u := ctxUnit{rows: []store.ContextRow{r}, tokens: r.Tokens, pinned: r.Pinned, isRead: r.Kind == "read_result"}
		if r.Role == "assistant" && r.ToolCalls != "" {
			for i+1 < len(rows) && rows[i+1].Role == "tool" {
				i++
				nxt := rows[i]
				u.rows = append(u.rows, nxt)
				u.tokens += nxt.Tokens
				u.pinned = u.pinned || nxt.Pinned
				u.isRead = u.isRead || nxt.Kind == "read_result"
			}
		}
		units = append(units, u)
	}
	budget := tokenBudget
	keptUnits := make([]ctxUnit, 0, len(units))
	trimmedRead := 0
	for i := len(units) - 1; i >= 0; i-- {
		u := units[i]
		if u.pinned {
			keptUnits = append([]ctxUnit{u}, keptUnits...)
			budget -= u.tokens
			continue
		}
		if budget-u.tokens < 0 {
			if u.isRead {
				trimmedRead++ // 按「一对往返」计数，不按行计
				continue
			}
			// dialogue 超预算：现状行为（丢弃该单元后继续往回收更旧的，结果是中间留洞，
			// 而非注释所称「截断最旧」）。本项不动它——它不制造孤儿 tool 行。
			continue
		}
		budget -= u.tokens
		keptUnits = append([]ctxUnit{u}, keptUnits...)
	}
	kept := make([]store.ContextRow, 0, len(rows))
	for _, u := range keptUnits {
		kept = append(kept, u.rows...)
	}
	msgs := make([]llm.Message, 0, len(kept))
	for _, r := range kept {
		switch r.Role {
		case "user":
			msgs = append(msgs, llm.Message{Role: "user", Content: r.Content})
		case "assistant":
			if r.ToolCalls != "" {
				var calls []llm.ToolCall
				if json.Unmarshal([]byte(r.ToolCalls), &calls) == nil && len(calls) > 0 {
					msgs = append(msgs, llm.Message{Role: "assistant", ToolCalls: calls})
					continue
				}
			}
			msgs = append(msgs, llm.Message{Role: "assistant", Content: r.Content})
		case "tool":
			msgs = append(msgs, llm.Message{Role: "tool", ToolCallID: r.ToolCallID, ToolName: r.ToolName, Content: r.Content})
		}
	}
	resolvedSummary := ""
	if trimmedRead > 0 {
		resolvedSummary = fmt.Sprintf("更早查询过 %d 次，结果已省略", trimmedRead)
	}
	return msgs, resolvedSummary
}

// validateQuoted 引用归属校验并转结构化上下文文本（自然语言 + 句柄，不给原始 id 数组）。
// 校验不通过返回 error（400）；同时返回落库用的 quoted JSON（引用条刷新后回显）。
// 第三个返回值是本轮勾选并集（按 kind 分组 + 子集句柄），注入工具 ctx 供提案做子集校验
// （点选优先的硬不变量，AI模式设计「结果集句柄与点选优先」）。
func (s *Service) validateQuoted(ctx context.Context, sessionID int64, refs []quoteRef) (string, string, tools.QuotedSelection, error) {
	sel := tools.QuotedSelection{IDs: map[string][]int64{}}
	if len(refs) == 0 {
		return "", "", sel, nil
	}
	var parts []string
	for _, ref := range refs {
		switch {
		case ref.ResultID != "":
			rs, err := s.db.GetResultSet(ctx, sessionID, ref.ResultID)
			if err != nil {
				return "", "", sel, fmt.Errorf("引用的结果集不存在或不属于本会话")
			}
			ids := rs.IDs
			desc := fmt.Sprintf("%d 条%s", len(ids), kindLabelCN(rs.Kind))
			if ref.Group != "" {
				found := false
				for _, g := range rs.Groups {
					if g.Key == ref.Group || g.Label == ref.Group {
						ids = g.IDs
						found = true
						break
					}
				}
				if !found {
					return "", "", sel, fmt.Errorf("结果集里没有分组「%s」", ref.Group)
				}
				desc = fmt.Sprintf("%d 条%s（来自你上次查询 %s 的「%s」分组）", len(ids), kindLabelCN(rs.Kind), rs.ResultID, ref.Group)
			}
			if len(ref.IDs) == 0 {
				// 只带 group（或整集引用）不带 ids：展开出的 id 全部计入勾选并集
				// （AI模式设计「结果集句柄与点选优先」不变量一节）。此路径不落子集句柄
				// ——父句柄本身就是范围，回填文案据此指引。
				sel.Enabled = true
				sel.IDs[rs.Kind] = append(sel.IDs[rs.Kind], ids...)
				sel.Count += len(ids)
				if sel.Handle == "" {
					sel.Handle = ref.ResultID
				}
			}
			if len(ref.IDs) > 0 {
				// 用户勾选了具体条目：取交集（防伪造）
				allowed := map[int64]bool{}
				for _, id := range ids {
					allowed[id] = true
				}
				var picked []int64
				for _, id := range ref.IDs {
					if allowed[id] {
						picked = append(picked, id)
					}
				}
				if len(picked) == 0 {
					return "", "", sel, fmt.Errorf("勾选的条目不属于该结果集")
				}
				ids = picked
				// 点选优先（AI模式设计「结果集句柄与点选优先」）：勾选交集落成子集结果集，
				// 换回新句柄 r_M 进上下文——模型拿到的就是「恰好那几条」的句柄，
				// 而非父全集；父句柄保留不动，下一轮仍可引用。
				subHandle, serr := s.db.SaveResultSet(ctx, sessionID, rs.Kind, picked, nil)
				if serr != nil {
					return "", "", sel, fmt.Errorf("勾选处理失败")
				}
				// 勾选并集（按 kind 合并多次引用）：提案目标必须全部落在其中，超出即驳回。
				// 子集句柄 r_M 即「恰好那几条」，回填文案让模型改用 r_M 重试。
				sel.Enabled = true
				sel.IDs[rs.Kind] = append(sel.IDs[rs.Kind], picked...)
				sel.Count += len(picked)
				if sel.Handle == "" {
					sel.Handle = subHandle
				}
				desc = fmt.Sprintf("你勾选的 %d 条%s（来自你上次查询 %s）；本轮操作范围以勾选为准", len(ids), kindLabelCN(rs.Kind), rs.ResultID)
				parts = append(parts, fmt.Sprintf("用户引用了：%s；引用句柄 %s；父集合 %s 仍可引用", desc, subHandle, ref.ResultID))
				continue
			}
			parts = append(parts, fmt.Sprintf("用户引用了：%s；引用句柄 %s", desc, ref.ResultID))
		case ref.Kind != "" && ref.ID > 0:
			allowed, err := s.db.ResultSetIDsByKind(ctx, sessionID, ref.Kind)
			if err != nil {
				return "", "", sel, fmt.Errorf("引用校验失败")
			}
			if !allowed[ref.ID] {
				return "", "", sel, fmt.Errorf("引用的资源不在本会话查询结果中")
			}
			// 单条引用同样构成范围约束（模型不得借它扩大到同 kind 的其它记录）
			sel.Enabled = true
			sel.IDs[ref.Kind] = append(sel.IDs[ref.Kind], ref.ID)
			sel.Count++
			parts = append(parts, fmt.Sprintf("用户引用了：%s #%d", kindLabelCN(ref.Kind), ref.ID))
		default:
			return "", "", sel, fmt.Errorf("引用格式无效")
		}
	}
	quotedJSON, _ := json.Marshal(refs)
	return "【用户引用】" + strings.Join(parts, "；"), string(quotedJSON), sel, nil
}

func kindLabelCN(kind string) string {
	switch kind {
	case "post":
		return "动态"
	case "media":
		return "媒体"
	case "album":
		return "相册"
	case "anniversary":
		return "纪念日"
	case "place":
		return "地点"
	case "comment":
		return "评论"
	}
	return kind
}

// mediaRefsFrom 客户端随消息携带的媒体引用（仅 name/type/size，无 data_url）。
// 返回 []any：args 走 map[string]any，断言口径与 json.Unmarshal 回读一致。
func mediaRefsFrom(media []chatMedia) []any {
	if len(media) == 0 {
		return nil
	}
	refs := make([]any, 0, len(media))
	for _, m := range media {
		refs = append(refs, map[string]any{"name": m.Name, "type": m.Type, "size": m.Size})
	}
	return refs
}

func userStyle(u *store.User) string {
	if u == nil {
		return ""
	}
	return u.AIStyle
}

// toolSpecs 下发给模型的工具清单。后台停用的工具不下发。
func (s *Service) toolSpecs(ctx context.Context) []llm.Tool {
	disabled, err := s.db.DisabledTools(ctx)
	if err != nil {
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
	b, err := json.Marshal(data)
	if err != nil {
		b = []byte(`{"error":"响应序列化失败"}`)
		event = "error"
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
	f.Flush()
}

func (s *Service) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
