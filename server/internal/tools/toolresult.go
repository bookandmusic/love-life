package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/bookandmusic/love-life/server/internal/store"
)

// 工具返回值三分离（治「一个返回字段同时服务两个受众」）：
//   - ModelText 喂模型：紧凑自然语言摘要 + 必要 id + 结果集句柄；
//   - UserText 给用户：一句人话（空串 = 该工具不单独发声，由 UI 块或模型回答承载）；
//   - UI 给界面：结构化 UI 块（nil = 无）。
//
// 模型永远拿不到 UI 载荷（无 URL、无缩略图），用户永远拿不到 ModelText——
// 「原始 JSON 泄给用户」在结构上不可能，而非靠人记得别泄。
type ToolResult struct {
	// ModelText 是唯一进模型上下文的工具产出（四条硬规则见 modelTextForList）。
	ModelText string
	// UserText 一句人话；空串 = 不单独发声。
	UserText string
	// UI 结构化 UI 块，客户端按 Kind 渲染；nil = 无。
	UI *UIBlock
	// Proposal 提案类工具专用：非 nil 时内部通道本轮以「待确认」收尾。
	Proposal *Proposal
	// Record Execute 模式下新建/变更的记录（kind+id），resolve 响应据此给客户端即时反馈。
	Record *RecordRef
}

// RecordRef 提案执行后新建记录的引用（resolve 响应 record 字段）。
type RecordRef struct {
	Kind string `json:"kind"`
	ID   int64  `json:"id"`
}

// ExecMode 一个工具 Run 只写一次业务逻辑，模式差异收敛在一个开关：
//   - ModePropose：校验 → 构造 Preview → 返回 Proposal，不落业务数据。
//     仅内部通道的工具调用阶段（APP 内 AI 模式）。
//   - ModeExecute：校验 → 直接执行 → 返回 ModelText。
//     用于两处：① 外部 PAT 通道 tools/call（按档位，铁律：删除类不开放）
//     ② 内部通道提案被 approve 后的执行（resolve 事务内）。
type ExecMode string

const (
	ModePropose ExecMode = "propose"
	ModeExecute ExecMode = "execute"
)

// UIBlock 判别联合。新增渲染形态 = 新增一个 Kind，不改动既有工具。
// 可勾选引用：Selectable 时客户端渲染选择框，勾选结果随下一条消息提交；
// SinglePick 单选语义（地点候选「选哪一个」），缺省多选。
type UIBlock struct {
	Kind       string      `json:"kind"` // record_list | media_grid | place_candidates | media_groups | record_detail | stats
	Title      string      `json:"title"`
	Records    []UIRecord  `json:"records"`
	More       int         `json:"more,omitempty"` // 未展示条数（「还有 7 条」）
	ResultID   string      `json:"result_id,omitempty"`
	Selectable bool        `json:"selectable,omitempty"`
	SinglePick bool        `json:"single_pick,omitempty"`
	Total      int         `json:"total,omitempty"`     // 命中总数真值（0/未知省略）
	Semantic   bool        `json:"semantic,omitempty"`  // 语义检索结果：无稳定总数，客户端标注「按相关性截取，非全部命中」
	Stats      []UIStat    `json:"stats,omitempty"`     // 统计概览卡：数字网格
	Series     []UISeries  `json:"series,omitempty"`    // 统计概览卡：分布列表（最多两组）
	Summaries  []UISummary `json:"summaries,omitempty"` // 统计概览卡：分布摘要（一行一条）
}

// UIStat / UISeries / UISummary 统计概览卡载荷（get_space_stats 专用）：数字网格 + 分布列表 + 分布摘要。
// value 由服务端拼好单位，客户端不做运算；卡上正文放分布摘要（Summaries，一行一条），
// series 仅作旧数据回显兼容（PRODUCT「卡片体系」统计概览卡）。
type UIStat struct {
	Label string `json:"label"`
	Value string `json:"value"` // 服务端拼好单位（如「320 条」「12 城」）
	Hint  string `json:"hint,omitempty"`
}

type UISeries struct {
	Title  string          `json:"title"`
	Points []UISeriesPoint `json:"points"`
}

type UISeriesPoint struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

// UISummary 分布摘要的一行（label + text），如 {Label: "按年", Text: "2024–2026 · 3 年共 498 条"}。
type UISummary struct {
	Label string `json:"label"`
	Text  string `json:"text"`
}

// UIRecord 统一条目形状。GroupKey 仅 media_groups 用：勾选分组时引用条携带它
// （quoted: {result_id, group}），服务端凭句柄+组键展开具体媒体。
type UIRecord struct {
	ID       int64  `json:"id"`
	Kind     string `json:"kind"` // post/media/album/anniversary/place/comment
	Text     string `json:"text"`
	Date     string `json:"date,omitempty"`
	ThumbURL string `json:"thumb_url,omitempty"`
	Badge    string `json:"badge,omitempty"` // 「3 条」「N 张」等角标
	GroupKey string `json:"group_key,omitempty"`
}

// AttachmentRef 附件清单（展示用元数据），与执行参数解耦（AI模式设计「工具契约」）：
// 提案只存 name/type/size；base64 留在消息上，media_token 在 resolve 时由客户端单独提交。
type AttachmentRef struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Size int64  `json:"size"`
}

// Proposal 一等公民提案：预览即目标形态，由工具在 Run 内自构造——
// 「预览漏参数」在结构上不可能（工具不写就没有卡片）。
type Proposal struct {
	Intent      string          `json:"intent"`  // propose_post / propose_album / …（与工具名一致）
	Summary     string          `json:"summary"` // 一句话：「发布一条动态」
	Preview     ProposalPreview `json:"preview"`
	Warnings    []string        `json:"warnings,omitempty"`
	Attachments []AttachmentRef `json:"attachments,omitempty"`
	// Params 出提案时已展开的执行参数（source 句柄 → 具体 id 列表），落 confirm_actions.params_json——
	// 提案自包含，resolve 不依赖结果集仍存活（重启/过期不影响已出的卡）。
	Params map[string]any `json:"-"`
}

// ProposalPreview 按意图给出「目标形态」的预览。Kind 与 9 个提案意图一一对应；
// Body 为该意图的目标形态数据（发布动态的卡片与发布前预览同形，不是 label:value 参数表）。
type ProposalPreview struct {
	Kind string `json:"kind"` // post/comment/album/album_members/anniversary/reminder/place/reaction/delete
	Body any    `json:"body"`
}

// ResultSink 结果集句柄写入端口：读工具执行时把结果 id 存入 chat_result_sets，
// 换回 result_id（r_N）挂到 UI 与 ModelText。由 chat 层注入（绑 session_id）；
// MCP 外部通道不注入（外部 Agent 直接消费 id，不需要句柄）。
type ResultSink func(kind string, ids []int64, groups []store.ResultGroup) (string, error)

type resultSinkKey struct{}

// WithResultSink 注入结果集端口（chat 层调用）。
func WithResultSink(ctx context.Context, sink ResultSink) context.Context {
	return context.WithValue(ctx, resultSinkKey{}, sink)
}

// sessionKey 会话 id（结果集句柄的归属校验范围）。
type sessionKey struct{}

// WithSessionID 注入当前会话 id：source 句柄展开时校验归属（仅限当前会话，AI模式设计「结果集句柄与点选优先」）。
func WithSessionID(ctx context.Context, sessionID int64) context.Context {
	return context.WithValue(ctx, sessionKey{}, sessionID)
}

// sessionIDFromCtx 取当前会话 id；未注入返回 0（MCP 通道无会话，句柄展开会用不到 source）。
func sessionIDFromCtx(ctx context.Context) int64 {
	id, _ := ctx.Value(sessionKey{}).(int64)
	return id
}

// sinkFrom 取当前结果集端口；未注入返回 nil。
func sinkFrom(ctx context.Context) ResultSink {
	sink, _ := ctx.Value(resultSinkKey{}).(ResultSink)
	return sink
}

// QuotedSelection 本轮用户勾选的并集（按 kind 分组），以及落成的子集句柄。
// 用途：出提案时校验「作用于既有记录的目标 id 集 ⊆ 勾选并集」——点选优先的硬不变量
// （AI模式设计「结果集句柄与点选优先」）。未勾选时 Enabled=false，校验跳过。
type QuotedSelection struct {
	Enabled bool
	IDs     map[string][]int64 // kind → 勾选并集
	Handle  string             // 子集句柄（r_M），用于回填文案
	Count   int                // 勾选总条数（回填文案）
}

type quotedSelectionKey struct{}

// WithQuotedSelection 注入本轮勾选并集（chat 层在解析 quoted 后调用）。
func WithQuotedSelection(ctx context.Context, sel QuotedSelection) context.Context {
	return context.WithValue(ctx, quotedSelectionKey{}, sel)
}

// quotedSelectionFrom 取本轮勾选并集；未注入或未勾选返回 Enabled=false。
func quotedSelectionFrom(ctx context.Context) QuotedSelection {
	sel, _ := ctx.Value(quotedSelectionKey{}).(QuotedSelection)
	return sel
}

// checkQuotedSubset 校验提案目标 id 集是否落在勾选并集内；超出返回可直接回填给模型的错误。
// 未勾选（Enabled=false）时放行——无勾选即无范围约束。
func checkQuotedSubset(ctx context.Context, kind string, ids []int64) error {
	sel := quotedSelectionFrom(ctx)
	if !sel.Enabled {
		return nil
	}
	allowed := make(map[int64]bool, len(sel.IDs[kind]))
	for _, id := range sel.IDs[kind] {
		allowed[id] = true
	}
	var outside []int64
	for _, id := range ids {
		if !allowed[id] {
			outside = append(outside, id)
		}
	}
	if len(outside) == 0 {
		return nil
	}
	// 勾选里没有该 kind 的条目：不是「超额」而是「类型不对」，文案要能引导改道
	if len(sel.IDs[kind]) == 0 {
		return fmt.Errorf("本轮勾选里没有%s——勾选的类型决定能做什么，请先查询并勾选%s，或用其它工具",
			kindLabel(kind), kindLabel(kind))
	}
	return fmt.Errorf("本次操作范围超出用户勾选：用户勾选了 %d 条%s（句柄 %s），请引用 %s",
		len(sel.IDs[kind]), kindLabel(kind), sel.Handle, sel.Handle)
}

// kindLabel kind 的中文名（回填文案用）。
func kindLabel(kind string) string {
	switch kind {
	case "post":
		return "动态"
	case "media":
		return "照片"
	case "album":
		return "相册"
	case "anniversary":
		return "纪念日"
	case "place":
		return "地图点位"
	case "comment":
		return "评论"
	}
	return kind
}

// saveResultSet 有注入时落结果集并返回句柄；未注入返回空串（MCP 通道）。
func saveResultSet(ctx context.Context, kind string, ids []int64, groups []store.ResultGroup) string {
	sink := sinkFrom(ctx)
	if sink == nil || len(ids) == 0 {
		return ""
	}
	id, err := sink(kind, ids, groups)
	if err != nil {
		return "" // 句柄失败不阻断查询：退化为无句柄模式（小结果集 id 已直接列出）
	}
	return id
}

// Invoke 统一入口：通用必填校验 + 工具自有跨字段校验 + 业务执行。
// 校验前置：不合格直接返 error，模型当轮可见原因并可重试，用户不见卡。
func (t *Tool) Invoke(ctx context.Context, d *Deps, userID int64, args map[string]any, mode ExecMode) (*ToolResult, error) {
	if err := t.ValidateRequired(args); err != nil {
		return nil, err
	}
	if t.Validate != nil {
		if err := t.Validate(args); err != nil {
			return nil, err
		}
	}
	return t.Run(ctx, d, userID, args, mode)
}

// modelTextCap ModelText 长度上限（rune）：防大结果集撑爆上下文（AI模式设计「工具契约」）。
const modelTextCap = 2000

// modelTextForList 组装列表类 ModelText（ 四条硬规则）：
//  1. header 必带查询条件回显（调用方拼好「按 query=海边 from=… 」）；
//  2. 必带结果集句柄与条数；
//  3. ≤10 条逐条列出（含 id，模型搬单个 id 可靠）；>10 条只列前 5 + 句柄；
//  4. 超长截断并注明完整列表在句柄里。
//
// 翻页时用 modelTextForPage 传区间：模型据此知道自己看的是哪一段（「第 21–40 条」），
// 否则它无法判断要不要继续调 offset 推进（AI模式设计「计数与翻页」）。
func modelTextForList(header string, total int, recs []UIRecord, resultID string) string {
	return modelTextForPage(header, total, recs, resultID, 0)
}

// modelTextForPage 带区间回显的列表 ModelText。offset 为 0 且未显式翻页时不写区间
// （首页「共 N 条」已足够；写了反而让模型以为必须翻页）。
func modelTextForPage(header string, total int, recs []UIRecord, resultID string, offset int) string {
	var sb strings.Builder
	sb.WriteString(header)
	if total > 0 {
		if offset > 0 {
			fmt.Fprintf(&sb, "第 %d–%d 条，共 %d 条", offset+1, offset+len(recs), total)
		} else {
			fmt.Fprintf(&sb, "共 %d 条", total)
		}
		if resultID != "" {
			fmt.Fprintf(&sb, "（结果集 %s）", resultID)
		}
		sb.WriteString("：")
	} else if total < 0 {
		// 语义检索路径（total 未知）：不给假数，但必须如实说「有命中」——
		// 命中却说「没有找到内容」会让模型据此回答用户「没找到」（AI模式设计「工具契约」）。
		fmt.Fprintf(&sb, "语义匹配到 %d 条（按相关性截取，非全部命中）", len(recs))
		if resultID != "" {
			fmt.Fprintf(&sb, "（结果集 %s）", resultID)
		}
		sb.WriteString("：")
	} else {
		sb.WriteString("没有找到内容")
		if resultID != "" {
			fmt.Fprintf(&sb, "（结果集 %s 为空）", resultID)
		}
		return sb.String()
	}
	list := recs
	more := 0
	if len(list) > 10 {
		more = len(list) - 5
		list = list[:5]
	}
	for i, r := range list {
		if i > 0 {
			sb.WriteString("；")
		}
		fmt.Fprintf(&sb, "#%d", r.ID)
		if r.Text != "" {
			fmt.Fprintf(&sb, "「%s」", truncateRunes(r.Text, 40))
		}
		if r.Date != "" {
			sb.WriteString(" " + r.Date)
		}
		if r.Badge != "" {
			sb.WriteString(" " + r.Badge)
		}
	}
	if more > 0 {
		fmt.Fprintf(&sb, "；另有 %d 条未列出", more)
	}
	if resultID != "" && (more > 0 || total > 10) {
		fmt.Fprintf(&sb, "，批量操作引用结果集 %s", resultID)
	}
	return truncateRunes(sb.String(), modelTextCap)
}

// truncateRunes 按 rune 截断；mark 非空时截断处追加标记。
func truncateRunes(s string, cap int) string {
	rs := []rune(s)
	if len(rs) <= cap {
		return s
	}
	return string(rs[:cap]) + "…"
}
