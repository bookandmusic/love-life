package tools

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/bookandmusic/love-life/server/internal/store"
)

// 工具返回值三分离（治「一个返回字段同时服务两个受众」）：
//   - Text 喂模型：紧凑自然语言摘要 + 必要 id + 结果集句柄；
//   - UserText 给用户：一句人话（空串 = 该工具不单独发声，由卡片或模型回答承载）；
//   - Data 给界面：结构化数据（nil = 没有可展示的数据，不出卡）。
//
// **数据与画法分离**：工具只描述数据（ResultData），画成哪种卡由模型在正文 ```ui 围栏里提案
// （CardSpec），服务端 ValidateSpec 校验通过才下发——「形状由服务端猜」在结构上不存在。
type ToolResult struct {
	// Text 是唯一进模型上下文的工具产出（四条硬规则见 modelTextForList）。
	Text string
	// UserText 一句人话；空串 = 不单独发声。
	UserText string
	// Data 结构化数据；nil = 无（空结果不出卡，答案由模型的话承担）。
	Data *ResultData
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
//   - ModeExecute：校验 → 直接执行 → 返回 Text。
//     用于两处：① 外部 PAT 通道 tools/call（按档位，铁律：删除类不开放）
//     ② 内部通道提案被 approve 后的执行（resolve 事务内）。
type ExecMode string

const (
	ModePropose ExecMode = "propose"
	ModeExecute ExecMode = "execute"
)

// DataKind 数据的形状类别：决定这份数据能画成哪些 view（不是画法本身）。
type DataKind string

const (
	KindRecords DataKind = "records" // 记录列表（动态 / 相册 / 纪念日 / 地点）
	KindMedia   DataKind = "media"   // 媒体（可画网格，也可画列表）
	KindEntity  DataKind = "entity"  // 单条详情
	KindStats   DataKind = "stats"   // 统计概览
)

// ResultData 工具产出的结构化数据。**只带数据，不带画法**：
// 画成哪种卡由模型在正文围栏里提案（CardSpec），服务端校验后下发。
// 可勾选引用：Selectable 时客户端渲染选择框，勾选结果随下一条消息提交。
type ResultData struct {
	Ref        string    `json:"ref"`             // 结果集句柄 r_17：模型围栏用它引用这份数据
	Kind       DataKind  `json:"kind"`            // records | media | entity | stats
	Title      string    `json:"title,omitempty"` // 数据自身标题（如「共同足迹」）
	Total      int       `json:"total,omitempty"` // 命中总数真值（0 = 未知，如语义检索）
	Records    []Record  `json:"records,omitempty"`
	Entity     *Entity   `json:"entity,omitempty"`
	Stats      []Stat    `json:"stats,omitempty"`
	Series     []Series  `json:"series,omitempty"`
	Summaries  []Summary `json:"summaries,omitempty"` // 统计卡正文（一行一条分布摘要）
	Semantic   bool      `json:"semantic,omitempty"`  // 语义检索：无稳定总数，客户端标「按相关性截取」
	Selectable bool      `json:"selectable,omitempty"`
}

// hasMedia 这份数据有没有可画网格的图片/视频。
func (d *ResultData) hasMedia() bool {
	if d == nil {
		return false
	}
	if d.Kind == KindMedia && len(d.Records) > 0 {
		return true
	}
	return d.Entity != nil && len(d.Entity.Media) > 0
}

// hasNumbers 这份数据有没有可统计的数字（数字网格或分布）。
func (d *ResultData) hasNumbers() bool {
	return d != nil && (len(d.Stats) > 0 || len(d.Series) > 0)
}

// hasDates 有没有能排进时间轴的条目（没有日期的条目排不进轴，只有一条有也算排得出）。
func (d *ResultData) hasDates() bool {
	if d == nil {
		return false
	}
	for _, r := range d.Records {
		if r.Date != "" {
			return true
		}
	}
	return false
}

// hasCoords 有没有能落图的点位（经纬度都是 0 视为没有坐标）。
func (d *ResultData) hasCoords() bool {
	if d == nil {
		return false
	}
	for _, r := range d.Records {
		if r.Lat != 0 || r.Lng != 0 {
			return true
		}
	}
	return false
}

// listData 列表类数据的统一出口：没有条目就不给数据。
// 空结果不出卡（让模型用一句话回答），也避免模型对空数据提一个必被拒的卡。
func listData(d *ResultData) *ResultData {
	if d == nil || len(d.Records) == 0 {
		return nil
	}
	return d
}

// Entity 单实体的载荷（详情卡）：一条动态 / 一个相册 / 一个纪念日 / 一个地点 /
// 一条评论 / 一张媒体。列表卡画不了的「全文 + 图 + 元信息」靠它。
type Entity struct {
	Kind  string   `json:"kind"` // post / album / anniversary / place / comment / media
	ID    int64    `json:"id"`
	Title string   `json:"title,omitempty"`
	Text  string   `json:"text,omitempty"`
	Date  string   `json:"date,omitempty"`
	Place string   `json:"place,omitempty"`
	Mood  string   `json:"mood,omitempty"`
	Media []Record `json:"media,omitempty"`
	Meta  []Stat   `json:"meta,omitempty"` // 标签/值（相册张数、倒计时…）
}

// ViewKind 渲染形态（客户端只认这七种，由 ValidateSpec 保证合法）。
type ViewKind string

const (
	ViewList     ViewKind = "list"     // 记录清单
	ViewGrid     ViewKind = "grid"     // 图片网格（需媒体）
	ViewStats    ViewKind = "stats"    // 统计概览（需数字）
	ViewDetail   ViewKind = "detail"   // 单条详情（需实体）
	ViewKpi      ViewKind = "kpi"      // 单值数字（恰好一个数字；回答「多少」）
	ViewTimeline ViewKind = "timeline" // 竖向时间轴（需带日期的记录）
	ViewMap      ViewKind = "map"      // 地图点位（需带经纬度的记录）
)

// Valid view 是否是服务端认识的形态。
func (v ViewKind) Valid() bool {
	switch v {
	case ViewList, ViewGrid, ViewStats, ViewDetail, ViewKpi, ViewTimeline, ViewMap:
		return true
	}
	return false
}

// CardSpec 模型在正文 ```ui 围栏里提的卡片提案：选哪种 view 由模型决定，服务端只校验。
type CardSpec struct {
	Ref   string   `json:"ref"`             // 引用本轮结果集句柄 r_17
	View  ViewKind `json:"view"`            // list | grid | stats | detail | kpi | timeline | map
	Title string   `json:"title,omitempty"` // 覆盖数据自带标题
	Note  string   `json:"note,omitempty"`  // 卡上一句注释
	// Only 只画这些项（按 Stat.Label / Series.Title / Summary.Label 匹配；仅 stats / kpi）。
	// 用户只问一项时用它把大卡裁成小卡，别把整份概览端上去。
	Only []string `json:"only,omitempty"`
	// Limit 条目最多画几条（仅 list / grid / timeline；0 = 数据默认条数）。
	Limit int `json:"limit,omitempty"`
}

// 卡片提案的错误码（回给模型自纠，不静默降级）。
const (
	CardErrUnknownRef     = "unknown_ref"      // ref 不存在
	CardErrGridNeedsMedia = "grid_needs_media" // 无媒体却要网格
	CardErrStatsNeedsNum  = "stats_needs_numbers"
	CardErrDetailNeedsEnt = "detail_needs_entity"
	CardErrListNeedsRecs  = "list_needs_records"
	CardErrDuplicateRef   = "duplicate_ref"
	CardErrUnknownView    = "unknown_view"
	CardErrBadJSON        = "bad_json"
	CardErrOnlyForStats   = "only_for_stats"          // only 只能配 stats / kpi
	CardErrUnknownField   = "unknown_field"           // only 里的项名不存在
	CardErrBadLimit       = "bad_limit"               // limit 越界或配错形态
	CardErrKpiNeedsOne    = "kpi_needs_single_number" // kpi 要求恰好一个数字
	CardErrTimelineNoDate = "timeline_needs_dates"    // 时间轴要求记录带日期
	CardErrMapNoCoord     = "map_needs_coords"        // 地图要求记录带经纬度
)

// CardError 卡片提案校验失败的原因（Code 供模型与审计识别，Message 是给模型看的人话）。
type CardError struct {
	Code          string   `json:"code"`
	Message       string   `json:"message"`
	AvailableRefs []string `json:"available_refs,omitempty"`
}

func (e *CardError) Error() string { return e.Code + ": " + e.Message }

// CardEnv 校验所需的当轮上下文：本轮可引用的数据（ref → 数据）与已画过的 ref。
type CardEnv struct {
	Data map[string]*ResultData
	Used map[string]bool
}

// Refs 本轮可引用的结果集句柄（升序，回给模型）。
func (e CardEnv) Refs() []string {
	refs := make([]string, 0, len(e.Data))
	for ref, d := range e.Data {
		if ref != "" && d != nil {
			refs = append(refs, ref)
		}
	}
	sort.Strings(refs)
	return refs
}

// ValidateSpec 校验卡片提案；合法返回 nil，否则返回带错误码的原因（由调用方回给模型重写）。
// 规则见 AI 卡片体系：unknown_ref → duplicate_ref → 各 view 的数据前提。
func ValidateSpec(spec CardSpec, env CardEnv) *CardError {
	data, ok := env.Data[spec.Ref]
	if spec.Ref == "" || !ok || data == nil {
		refs := env.Refs()
		avail := "无"
		if len(refs) > 0 {
			avail = strings.Join(refs, ", ")
		}
		if spec.Ref == "" {
			return &CardError{Code: CardErrUnknownRef,
				Message: "围栏没写 ref，请填工具回执里的结果集（如 r_3）", AvailableRefs: refs}
		}
		return &CardError{Code: CardErrUnknownRef,
			Message: fmt.Sprintf("结果集 %s 不存在，本轮可用：%s", spec.Ref, avail), AvailableRefs: refs}
	}
	if env.Used[spec.Ref] {
		return &CardError{Code: CardErrDuplicateRef,
			Message: fmt.Sprintf("结果集 %s 已经画过了，同一份数据只画一次", spec.Ref)}
	}
	// only / limit 的形态前提（先于各 view 的数据前提校验：配错形态时给的是「用法」错，不是「数据不够」错）
	if len(spec.Only) > 0 && spec.View != ViewStats && spec.View != ViewKpi {
		return &CardError{Code: CardErrOnlyForStats,
			Message: "only 只能用在 stats / kpi 卡上（list / grid / timeline 要收窄请用 limit）"}
	}
	if spec.Limit != 0 {
		if spec.View != ViewList && spec.View != ViewGrid && spec.View != ViewTimeline {
			return &CardError{Code: CardErrBadLimit,
				Message: "limit 只能用在 list / grid / timeline 卡上"}
		}
		if spec.Limit < 1 || spec.Limit > 50 {
			return &CardError{Code: CardErrBadLimit,
				Message: "limit 取值 1–50"}
		}
	}
	if len(spec.Only) > 0 {
		// 项名必须真的存在：写错名字时列可用项让模型改，而不是静默少画几项
		avail := data.fieldNames()
		for _, want := range spec.Only {
			if !slices.Contains(avail, want) {
				return &CardError{Code: CardErrUnknownField,
					Message: fmt.Sprintf("only 里的 %q 不存在，这份数据可用的项：%s", want, strings.Join(avail, "、"))}
			}
		}
	}
	switch spec.View {
	case ViewList:
		if len(data.Records) == 0 {
			return &CardError{Code: CardErrListNeedsRecs, Message: "这条数据没有可列出的条目"}
		}
	case ViewGrid:
		if !data.hasMedia() {
			return &CardError{Code: CardErrGridNeedsMedia, Message: "这条数据没有图片/视频，不能画网格"}
		}
	case ViewStats:
		if !data.hasNumbers() {
			return &CardError{Code: CardErrStatsNeedsNum, Message: "这条数据没有可统计的数字"}
		}
	case ViewKpi:
		// kpi 是「一个大数字」：数据里恰好一个数字项才成立（多数字请用 stats + only 点名，或直接回答）。
		// only 必须指向 Stats 里的 Label——fieldNames 还含 Series/Summaries，
		// 光看「项名存在」会把分布标题当数字项收下，下发一张空卡。
		labels := make([]string, 0, len(data.Stats))
		for _, s := range data.Stats {
			labels = append(labels, s.Label)
		}
		if len(spec.Only) == 1 {
			if !slices.Contains(labels, spec.Only[0]) {
				avail := strings.Join(labels, "、")
				if avail == "" {
					avail = "无"
				}
				return &CardError{Code: CardErrKpiNeedsOne,
					Message: fmt.Sprintf("kpi 的 only 必须指向数字项，这份数据的数字项：%s", avail)}
			}
		} else if len(data.Stats) != 1 {
			return &CardError{Code: CardErrKpiNeedsOne,
				Message: fmt.Sprintf("kpi 卡只画一个数字，这份数据有 %d 个数字项；请用 only 指定一项，或改用 stats 卡", len(data.Stats))}
		}
	case ViewDetail:
		if data.Entity == nil {
			return &CardError{Code: CardErrDetailNeedsEnt, Message: "详情卡只能画单条记录"}
		}
	case ViewTimeline:
		// 时间轴按日期排序：整份数据都没日期就排不出来，别画一张空轴
		if !data.hasDates() {
			return &CardError{Code: CardErrTimelineNoDate, Message: "这条数据没有带日期的条目，排不出时间轴"}
		}
	case ViewMap:
		// 地图要坐标：没有经纬度的条目落不到图上（媒体、相册都没有坐标）
		if !data.hasCoords() {
			return &CardError{Code: CardErrMapNoCoord, Message: "这条数据没有带经纬度的点位，画不了地图"}
		}
	default:
		return &CardError{Code: CardErrUnknownView,
			Message: fmt.Sprintf("view %q 不存在，只能是 list / grid / stats / detail / kpi / timeline / map", spec.View)}
	}
	return nil
}

// fieldNames 这份数据里可被 only 点名的项（Stat.Label / Series.Title / Summary.Label）。
func (d *ResultData) fieldNames() []string {
	out := make([]string, 0, len(d.Stats)+len(d.Series)+len(d.Summaries))
	for _, s := range d.Stats {
		out = append(out, s.Label)
	}
	for _, s := range d.Series {
		out = append(out, s.Title)
	}
	for _, s := range d.Summaries {
		out = append(out, s.Label)
	}
	return out
}

// Stat / Series / Summary 统计概览卡载荷（get_space_stats 专用）：数字网格 + 分布列表 + 分布摘要。
// value 由服务端拼好单位，客户端不做运算；卡上正文放分布摘要（Summaries，一行一条），
// series 仅用于历史行回显兼容（统计概览卡本身用 grid 维度）。
type Stat struct {
	Label string `json:"label"`
	Value string `json:"value"` // 服务端拼好单位（如「320 条」「12 城」）
	Hint  string `json:"hint,omitempty"`
}

type Series struct {
	Title  string  `json:"title"`
	Points []Point `json:"points"`
}

type Point struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

// Summary 分布摘要的一行（label + text），如 {Label: "按年", Text: "2024–2026 · 3 年共 498 条"}。
type Summary struct {
	Label string `json:"label"`
	Text  string `json:"text"`
}

// Record 统一条目形状。GroupKey 仅 media_groups 用：勾选分组时引用条携带它
// （quoted: {result_id, group}），服务端凭句柄+组键展开具体媒体。
// Lat/Lng 只有地点条目带（地图卡读它；0,0 与「没有坐标」等价——真实点位不会正好落在那里）。
// Remind 只有纪念日条目带（提醒设置 + 提醒日，只进回执给模型用，卡上不画）。
type Record struct {
	ID       int64   `json:"id"`
	Kind     string  `json:"kind"` // post/media/album/anniversary/place/comment
	Text     string  `json:"text"`
	Date     string  `json:"date,omitempty"`
	ThumbURL string  `json:"thumb_url,omitempty"`
	Badge    string  `json:"badge,omitempty"` // 「3 条」「N 张」等角标
	GroupKey string  `json:"group_key,omitempty"`
	Lat      float64 `json:"lat,omitempty"`
	Lng      float64 `json:"lng,omitempty"`
	Remind   string  `json:"remind,omitempty"`
}

// AttachmentRef 附件清单（展示用元数据），与执行参数解耦：
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

// ProposalPreview 按意图给出「目标形态」的预览。Kind 与 8 个提案意图一一对应；
// Body 为该意图的目标形态数据（发布动态的卡片与发布前预览同形，不是 label:value 参数表）。
type ProposalPreview struct {
	Kind string `json:"kind"` // post/comment/album/album_members/anniversary/place/reaction/delete
	Body any    `json:"body"`
}

// ResultSink 结果集句柄写入端口：读工具执行时把结果 id 存入 chat_result_sets，
// 换回 ref（r_N）挂到 Data 与 Text。由 chat 层注入（绑 session_id）；
// MCP 外部通道不注入（外部 Agent 直接消费 id，不需要句柄）。
type ResultSink func(kind string, ids []int64, groups []store.ResultGroup) (string, error)

type resultSinkKey struct{}

// WithResultSink 注入结果集端口（chat 层调用）。
func WithResultSink(ctx context.Context, sink ResultSink) context.Context {
	return context.WithValue(ctx, resultSinkKey{}, sink)
}

// sessionKey 会话 id（结果集句柄的归属校验范围）。
type sessionKey struct{}

// WithSessionID 注入当前会话 id：source 句柄展开时校验归属（仅限当前会话）。
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
// 用途：出提案时校验「作用于既有记录的目标 id 集 ⊆ 勾选并集」——点选优先的硬不变量。
// 未勾选时 Enabled=false，校验跳过。
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
	// stats 卡没有逐条 id（聚合数字直出）也落句柄：模型正文围栏按 ref 引用这份数据，
	// 没句柄它只能编 ref，卡片必然校验不过。空 ids 合法。
	if sink == nil {
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
	// 数据是否为空由工具自己决定（listData 对空列表返回 nil）：不做「先造再筛」，
	// 空结果在源头就没有 Data，模型也就不会对空数据提案。
	return t.Run(ctx, d, userID, args, mode)
}

// modelTextCap ModelText 长度上限（rune）：防大结果集撑爆上下文。
const modelTextCap = 2000

// modelTextForList 组装列表类 ModelText（四条硬规则）：
//  1. header 必带查询条件回显（调用方拼好「按 query=海边 from=… 」）；
//  2. 必带结果集句柄与条数；
//  3. ≤10 条逐条列出（含 id，模型搬单个 id 可靠）；>10 条只列前 5 + 句柄；
//  4. 超长截断并注明完整列表在句柄里。
//
// 翻页时用 modelTextForPage 传区间：模型据此知道自己看的是哪一段（「第 21–40 条」），
// 否则它无法判断要不要继续调 offset 推进。
func modelTextForList(header string, total int, recs []Record, resultID string) string {
	return modelTextForPage(header, total, recs, resultID, 0)
}

// modelTextForPage 带区间回显的列表 ModelText。offset 为 0 且未显式翻页时不写区间
// （首页「共 N 条」已足够；写了反而让模型以为必须翻页）。
func modelTextForPage(header string, total int, recs []Record, resultID string, offset int) string {
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
		// 命中却说「没有找到内容」会让模型据此回答用户「没找到」。
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
		if r.Remind != "" {
			sb.WriteString(" " + r.Remind)
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
