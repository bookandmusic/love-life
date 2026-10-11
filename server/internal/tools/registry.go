// Package tools MCP 工具注册表：16 个工具（读 7 / 提案 8 / 删除 1），按意图命名。
//
// 三通道/模式口径：
//   - 读工具立即执行（两通道均开放），产出 ModelText/UserText/UI 三分离载荷；
//   - 提案类工具（IsWrite）：内部通道 ModePropose 出确认卡（15 分钟 TTL 单次有效）；
//     外部 PAT readwrite 档与内部 resolve 审批走 ModeExecute 直接执行；
//
// - 删除类（AppOnly）不对任何令牌开放（铁律 ）。
//
// 所有调用均由调用方（chat AI 通道 / mcp PAT 通道 / resolve）记审计，Exec 内部不写审计。
package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/bookandmusic/love-life/server/internal/geo"
	"github.com/bookandmusic/love-life/server/internal/lunar"
	"github.com/bookandmusic/love-life/server/internal/media"
	"github.com/bookandmusic/love-life/server/internal/notify"
	"github.com/bookandmusic/love-life/server/internal/store"
	"github.com/bookandmusic/love-life/server/internal/textutil"
)

type Tool struct {
	Name        string
	Description string
	// ProgressLabel 工具状态条的人话标签（SSE tool.start），不含工具名与参数。
	ProgressLabel string
	// Category 后台工具表分组。取值：动态 / 相册 / 地图 / 纪念日 / 互动。
	Category string
	// Schema 参数名册（后台展示）；Params 是给模型的 JSON Schema。两份必须键一致（守卫测试）。
	Schema map[string]string
	Params map[string]any
	// IsWrite 提案类工具：内部通道出确认卡，外部通道 readwrite 档直接执行。
	IsWrite bool
	// AppOnly 删除类：铁律「删除类工具不对任何令牌开放」，RegistryForPAT 过滤。
	AppOnly bool
	// Validate 参数与业务前置校验：Propose/Execute 两模式共跑（校验前置）。
	Validate func(args map[string]any) error
	// Run 业务逻辑；mode 语义见 ExecMode。只写一次，模式差异收敛在开关上。
	Run func(ctx context.Context, d *Deps, userID int64, args map[string]any, mode ExecMode) (*ToolResult, error)
}

type Deps struct {
	DB *store.DB
	MP *media.Pipeline
	DP *notify.Dispatcher
	// Search 语义检索端口；未配置嵌入模型时为 nil，find_posts 回落关键词匹配。
	Search SearchIndex
}

// SearchIndex 语义检索端口。接口定义在此以避免 tools 依赖具体实现，
// 由 search.Index 满足（Go 结构化接口，无需显式声明实现）。
type SearchIndex interface {
	Enabled(ctx context.Context) bool
	Query(ctx context.Context, text string, k int) ([]int64, error)
}

// 审计 result 口径（对齐 mock 契约 fixtures.mjs audit：success / failed 两态）。
// 提案另有 pending（出卡时）与 cancelled（裁决取消），由 chat 通道记账。
const (
	AuditSuccess = "success"
	AuditFailed  = "failed"
	AuditPending = "pending"
)

// AuditFailure 组装失败态 result（failed: 原因），供各通道统一使用。
func AuditFailure(err error) string {
	return AuditFailed + ": " + err.Error()
}

// auditParamValueMax 审计 params 单个值的展示上限（rune）。
const auditParamValueMax = 60

// FormatParams 把工具入参渲染成后台展示用的人类可读摘要（键序取字母序确定化）。
func FormatParams(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	var sb strings.Builder
	first := true
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !first {
			sb.WriteString(", ")
		}
		first = false
		sb.WriteString(k + ": " + formatParamValue(args[k]))
	}
	return sb.String()
}

// FormatParamsFor 按工具定制审计的人类可读摘要；未定制的工具走通用 FormatParams。
//
// 定制的原因：通用格式化按字母序拼 `key: value` 并对超长单值截断，
// 批量删除会落成 `ids: [1,2,3…`（看不出删了几条）。审计口径要求
// 「kind + 条数 + 前 3 个 id」（AI模式设计「批量删除契约」· 通道与审计）。
func formatParamsFor(tool string, args map[string]any) string {
	if tool == "propose_delete" {
		return formatDeleteParams(args)
	}
	return FormatParams(args)
}

// formatDeleteParams 批量删除的审计摘要：kind + 条数 + 前 3 个 id。
//
// 三种入参形态：`{kind,id}` 单条 / `{kind,ids[]}` 显式批量 / `{kind,source}` 句柄。
// 句柄在出卡时展开为具体 ids 并写进 `params_json`——**resolve 时的审计用的正是它**，
// 故那一条能给出条数与前 3 个 id；出卡前的 pending 审计只有句柄，如实记 kind + source。
func formatDeleteParams(args map[string]any) string {
	kind := str(args, "kind")
	ids := deleteParamIDs(args)
	switch {
	case len(ids) > 0:
		head := ids
		if len(head) > 3 {
			head = head[:3]
		}
		parts := make([]string, 0, len(head))
		for _, id := range head {
			parts = append(parts, fmt.Sprintf("%v", id))
		}
		tail := ""
		if len(ids) > len(head) {
			tail = "…"
		}
		return fmt.Sprintf("kind: %s, 条数: %d, ids: [%s%s]", kind, len(ids), strings.Join(parts, ", "), tail)
	case args["source"] != nil:
		if src := parseSource(args); src != nil {
			return fmt.Sprintf("kind: %s, source: %s", kind, src.ResultID)
		}
	}
	return FormatParams(args)
}

// deleteParamIDs 从入参取删除目标 id（单条 id 或批量 ids），两者都没有时返回空。
func deleteParamIDs(args map[string]any) []int64 {
	if list, ok := args["ids"].([]any); ok && len(list) > 0 {
		ids := make([]int64, 0, len(list))
		for i := range list {
			// 非正数一律丢弃：入参非法时（如 id 传了字符串）宁可退回通用摘要，
			// 也不能在审计里写成「条数: 1, ids: [0]」这种失实摘要。
			if n := asInt64(list[i]); n > 0 {
				ids = append(ids, n)
			}
		}
		return ids
	}
	if n := asInt64(args["id"]); n > 0 {
		return []int64{n}
	}
	return nil
}

// asInt64 单值容错取整：模型 JSON 解码给 float64、提案回读给 int64、内部直传给 int。
func asInt64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	}
	return 0
}

func formatParamValue(v any) string {
	if str, ok := v.(string); ok {
		b, _ := json.Marshal(textutil.TruncMark(str, auditParamValueMax))
		return string(b)
	}
	b, _ := json.Marshal(v)
	return textutil.TruncMark(string(b), auditParamValueMax)
}

// AuditWithLatency 工具审计带 latency 版本：argsJSON 原样存 params_digest，
// 人类可读摘要存 params（由 argsJSON 派生）。latencyMs 仅 ai/pat 调用有值。
func (d *Deps) AuditWithLatency(ctx context.Context, userID int64, tool, argsJSON, result string, latencyMs int64) {
	d.auditWithLatency(ctx, userID, tool, argsJSON, nil, result, latencyMs)
}

// AuditWithLatencyParams 同 AuditWithLatency，但**人类可读摘要改由 params 派生**
// （`params_digest` 仍原样存 argsJSON）。
//
// 用于提案类工具：读工具/提案的出卡会把入参归一化——最典型是 `source` 句柄在此展开成
// 具体 ids。审计口径要求摘要记「kind + 条数 + 前 3 个 id」，只看模型原始入参只能记到句柄
// 名，故这条 pending 审计必须用归一化后的参数。resolve 那条本就用落库的 params_json。
func (d *Deps) AuditWithLatencyParams(ctx context.Context, userID int64, tool, argsJSON string, params map[string]any,
	result string, latencyMs int64) {
	d.auditWithLatency(ctx, userID, tool, argsJSON, params, result, latencyMs)
}

func (d *Deps) auditWithLatency(ctx context.Context, userID int64, tool, argsJSON string, summaryFrom map[string]any,
	result string, latencyMs int64) {
	var args map[string]any
	_ = json.Unmarshal([]byte(argsJSON), &args)
	src := args
	if summaryFrom != nil {
		src = summaryFrom
	}
	_ = d.DB.InsertAuditLogFull(ctx, store.ActorTypeAI, userID, tool, argsJSON, formatParamsFor(tool, src), result, &latencyMs)
}

// aiOperatorLabel AI 操作者名称查不到时的回退标签。
const aiOperatorLabel = "AI助手"

// notifyPartners 通用通知分发辅助（站内通知另一方 + 外部推送）。
func (d *Deps) notifyPartners(ctx context.Context, userID int64, eventType, entityType string, entityID int64, snapshot, title, body string) {
	partnerIDs, err := d.DB.ListPartnerIDs(ctx, userID)
	if err != nil {
		slog.Warn("list partner ids failed", "err", err, "user_id", userID, "event", eventType)
		return
	}
	d.DP.Dispatch(ctx, partnerIDs, eventType, entityType, entityID, snapshot, title, body)
}

// aiOperatorName AI 代操作的通知署名：取真实用户名（与 REST 同口径）。
func (d *Deps) aiOperatorName(ctx context.Context, userID int64) string {
	u, err := d.DB.GetUserByID(ctx, userID)
	if err != nil || u.Username == "" {
		slog.Warn("resolve ai operator name failed", "err", err, "user_id", userID)
		return aiOperatorLabel
	}
	return u.Username
}

// ---- 参数取值小工具 ----

func str(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// num 容错取整：参数来源有三处（模型 JSON 解码=float64、提案 params 回读=float64、
// 服务端内部直传=int64/int），三者都要认，否则「参数看着在、执行时却丢了」。
func num(args map[string]any, key string) int64 { return asInt64(args[key]) }

func flt(args map[string]any, key string) float64 {
	switch v := args[key].(type) {
	case float64:
		return v
	case int64:
		return float64(v)
	case int:
		return float64(v)
	}
	return 0
}

// ints 从参数取整形数组（如 media_ids），容错 []any（JSON number）/[]float64。
func ints(args map[string]any, key string) ([]int64, error) {
	var out []int64
	v, ok := args[key]
	if !ok {
		return nil, nil
	}
	switch items := v.(type) {
	case []any:
		for _, it := range items {
			if n, ok := it.(float64); ok {
				out = append(out, int64(n))
			}
		}
	case []float64:
		for _, n := range items {
			out = append(out, int64(n))
		}
	case []int64:
		out = items
	default:
		return nil, fmt.Errorf("%s 必须是整数数组", key)
	}
	return out, nil
}

// tokens 从 {/确认参数} 读取字符串数组（如 media_tokens），容错 []any/[]string。
func tokens(args map[string]any, key string) []string {
	var out []string
	switch v := args[key].(type) {
	case []any:
		for _, it := range v {
			if s, ok := it.(string); ok {
				out = append(out, s)
			}
		}
	case []string:
		out = v
	}
	return out
}

// optBool 宽容取布尔参数：模型可能给 true/false，也可能给 "true"/"1"/1。
func optBool(args map[string]any, key string) (bool, bool) {
	switch v := args[key].(type) {
	case bool:
		return v, true
	case string:
		if v == "true" || v == "1" {
			return true, true
		}
		if v == "false" || v == "0" {
			return false, true
		}
	case float64:
		return v != 0, true
	case int64:
		return v != 0, true
	}
	return false, false
}

// boolArg 布尔参数（缺省 false）。
func boolArg(args map[string]any, key string) bool {
	v, _ := optBool(args, key)
	return v
}

// ValidateRequired 按工具 Params 里的 `required` 声明校验必填参数给到位。
// 单一实现覆盖所有工具：`required` 本是给模型的契约，同一份契约再当一次服务端校验。
func (t *Tool) ValidateRequired(args map[string]any) error {
	req, _ := t.Params["required"].([]string)
	for _, key := range req {
		switch v := args[key].(type) {
		case nil:
			return fmt.Errorf("缺少必填参数 %s", key)
		case string:
			if strings.TrimSpace(v) == "" {
				return fmt.Errorf("必填参数 %s 不能为空", key)
			}
		case []any:
			if len(v) == 0 {
				return fmt.Errorf("必填参数 %s 不能为空数组", key)
			}
		case float64:
			if v == 0 && strings.HasSuffix(key, "_id") {
				return fmt.Errorf("必填参数 %s 无效", key)
			}
		}
	}
	return nil
}

// ---- 结果集句柄引用 ----

// SourceRef 提案工具的批量资源来源：source={result_id, group?} 与 media_ids 二选一。
type SourceRef struct {
	ResultID string `json:"result_id"`
	Group    string `json:"group,omitempty"`
}

// parseSource 解析 source 参数；未提供返回 nil。
func parseSource(args map[string]any) *SourceRef {
	raw, ok := args["source"].(map[string]any)
	if !ok {
		return nil
	}
	s := &SourceRef{}
	if v, ok := raw["result_id"].(string); ok {
		s.ResultID = v
	}
	if v, ok := raw["group"].(string); ok {
		s.Group = v
	}
	if s.ResultID == "" {
		return nil
	}
	return s
}

// expandSource 把结果集句柄展开为具体 id 列表（出提案时即展开，提案自此自包含）。
// sessionID 用于归属校验：句柄必须属于本会话（防伪造引用摸到未授权数据）。
// wantKind 非空时校验结果集类型：media id 与 post id 是两个自增空间，不校验 kind
// 时传入 media id 只要 posts 表恰好有同号就会被误删——校验前置，不出注定失败的卡
// （AI模式设计「批量删除契约」）。
//
// 勾选存在时另做**子集不变量**校验：展开出的 id 必须全部落在本轮勾选并集内。父句柄按
// 设计保留不动（用户可回头引用更早的结果集），所以模型随时可能拿父全集覆盖勾选范围——
// 这道校验是点选优先的硬约束，超出即报错回填、不出卡（AI模式设计「结果集句柄与点选优先」）。
func expandSource(ctx context.Context, d *Deps, userID, sessionID int64, s *SourceRef, wantKind string) ([]int64, error) {
	rs, err := d.DB.GetResultSet(ctx, sessionID, s.ResultID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, fmt.Errorf("结果集 %s 不存在或已过期（仅限当前会话）", s.ResultID)
		}
		return nil, err
	}
	if wantKind != "" && rs.Kind != wantKind {
		return nil, fmt.Errorf("结果集类型不匹配：引用的是 %s 集合，本操作需要 %s 集合", rs.Kind, wantKind)
	}
	var ids []int64
	if s.Group != "" {
		found := false
		for _, g := range rs.Groups {
			if g.Key == s.Group || g.Label == s.Group {
				ids, found = g.IDs, true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("结果集 %s 中没有分组「%s」", s.ResultID, s.Group)
		}
	} else {
		ids = rs.IDs
	}
	if err := checkQuotedSubset(ctx, rs.Kind, ids); err != nil {
		return nil, err
	}
	return ids, nil
}

// ---- 动态查询路径（find_posts） ----

// PostHit 动态查询结果的统一输出行；location_name 仅当动态带地点时出现。
type PostHit struct {
	ID           int64  `json:"id"`
	Content      string `json:"content"`
	Date         string `json:"date"`
	Author       string `json:"author"`
	LocationName string `json:"location_name,omitempty"`
}

const postHitColumns = `p.id, p.content, p.created_at,
		COALESCE(u.nickname, u.username), COALESCE(p.location_name,'')`

func scanPostHits(rows *sql.Rows) ([]PostHit, error) {
	var hits []PostHit
	for rows.Next() {
		var h PostHit
		if err := rows.Scan(&h.ID, &h.Content, &h.Date, &h.Author, &h.LocationName); err != nil {
			return nil, err
		}
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

const findPostsDefaultLimit = 10

// searchMediaTypes 动态可用的媒体类型（与 media.kind 值域一致）。
var searchMediaTypes = map[string]bool{"photo": true, "video": true, "audio": true}

// postsFilter 查询过滤条件（零值 = 不限）；全部下推 SQL（禁「取 N 条再 Go 侧过滤」）。
type postsFilter struct {
	from, to    string // YYYY-MM-DD（本地日期口径，闭区间）
	mediaType   string // photo / video / audio
	mood        string
	place       string
	authorIDs   []int64 // 空 = 不限；me/partner 由调用方解析后注入
	favoritedBy int64   // >0 = 只看该用户收藏的
}

// where 关键词/浏览路径的附加谓词与参数（列名限定 p. 前缀）。
// 日期按 localtime 比对（用户可见日期按部署者本地时区，与 store 既有口径一致）。
func (f postsFilter) where(viewerID int64) (string, []any) {
	var conds []string
	var args []any
	if f.from != "" {
		conds = append(conds, "strftime('%Y-%m-%d', p.created_at, 'localtime') >= ?")
		args = append(args, f.from)
	}
	if f.to != "" {
		conds = append(conds, "strftime('%Y-%m-%d', p.created_at, 'localtime') <= ?")
		args = append(args, f.to)
	}
	if f.mediaType != "" {
		conds = append(conds, "EXISTS(SELECT 1 FROM media m WHERE m.post_id = p.id AND m.kind = ? AND m.deleted_at IS NULL)")
		args = append(args, f.mediaType)
	}
	if f.mood != "" {
		conds = append(conds, "p.mood LIKE ?")
		args = append(args, "%"+f.mood+"%")
	}
	if f.place != "" {
		conds = append(conds, "p.location_name LIKE ?")
		args = append(args, "%"+f.place+"%")
	}
	if len(f.authorIDs) > 0 {
		ph := strings.TrimSuffix(strings.Repeat("?,", len(f.authorIDs)), ",")
		conds = append(conds, "p.author_id IN ("+ph+")")
		for _, id := range f.authorIDs {
			args = append(args, id)
		}
	}
	if f.favoritedBy > 0 {
		conds = append(conds, "EXISTS(SELECT 1 FROM favorites fav WHERE fav.post_id = p.id AND fav.user_id = ?)")
		args = append(args, f.favoritedBy)
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " AND " + strings.Join(conds, " AND "), args
}

// heavy 语义路径无法下推的重过滤（mood/place/author/favorited）：出现时跳过语义检索，
// 走关键词/浏览的 SQL 路径（全量谓词下推），避免向量召回后 N+1 过滤。
func (f postsFilter) heavy() bool {
	return f.mood != "" || f.place != "" || len(f.authorIDs) > 0 || f.favoritedBy > 0
}

// utcToLocalDate 把库内 UTC 时间串转部署者本地日期 YYYY-MM-DD。
func utcToLocalDate(s string) string {
	layouts := []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05Z", "2006-01-02"}
	for _, lay := range layouts {
		if t, err := time.Parse(lay, s); err == nil {
			return t.Local().Format("2006-01-02")
		}
	}
	if i := strings.IndexByte(s, ' '); i > 0 {
		return s[:i]
	}
	return s
}

// parsePostsFilter 解析 find_posts 可选入参；格式非法时报错（不静默忽略）。
func parsePostsFilter(args map[string]any) (postsFilter, error) {
	var f postsFilter
	if mt := str(args, "media_type"); mt != "" {
		if !searchMediaTypes[mt] {
			return f, fmt.Errorf("media_type 仅支持 photo / video / audio")
		}
		f.mediaType = mt
	}
	if v, ok := args["from"].(string); ok && v != "" {
		if _, err := time.Parse("2006-01-02", v); err != nil {
			return f, fmt.Errorf("from 日期无效: %s", v)
		}
		f.from = v
	}
	if v, ok := args["to"].(string); ok && v != "" {
		if _, err := time.Parse("2006-01-02", v); err != nil {
			return f, fmt.Errorf("to 日期无效: %s", v)
		}
		f.to = v
	}
	if f.from != "" && f.to != "" && f.from > f.to {
		return f, fmt.Errorf("from 晚于 to")
	}
	f.mood = str(args, "mood")
	f.place = str(args, "place")
	return f, nil
}

// postsTotal 与列表同 WHERE 的命中总数（find_posts 真实 total，COUNT 下推）。
func (d *Deps) postsTotal(ctx context.Context, f postsFilter, viewerID int64, extraWhere string, extraArg any) (int, error) {
	where, args := f.where(viewerID)
	if extraArg != nil {
		args = append([]any{extraArg}, args...)
	}
	var n int
	err := d.DB.QueryRowContext(ctx, fmt.Sprintf(`
			SELECT COUNT(*) FROM posts p JOIN users u ON u.id = p.author_id
			WHERE p.deleted_at IS NULL%s%s`, extraWhere, where), args...).Scan(&n)
	return n, err
}

// browsePosts 浏览路径（query 留空）：过滤下推 SQL，按时间倒序；offset 翻页。
// 返回值带命中总数真值（COUNT 下推，与列表共用同一 WHERE）。
func (d *Deps) browsePosts(ctx context.Context, f postsFilter, viewerID int64, limit, offset int) ([]PostHit, int, error) {
	where, args := f.where(viewerID)
	total, err := d.postsTotal(ctx, f, viewerID, "", nil)
	if err != nil {
		return nil, 0, err
	}
	rows, err := d.DB.QueryContext(ctx, fmt.Sprintf(`
			SELECT %s FROM posts p JOIN users u ON u.id = p.author_id
			WHERE p.deleted_at IS NULL%s
			ORDER BY p.id DESC LIMIT %d OFFSET %d`, postHitColumns, where, limit, offset), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	hits, err := scanPostHits(rows)
	return hits, total, err
}

// keywordPosts 关键词路径：SQL 侧直接套用全部过滤条件；offset 翻页 + 真实总数。
func (d *Deps) keywordPosts(ctx context.Context, query string, f postsFilter, viewerID int64, limit, offset int) ([]PostHit, int, error) {
	like := "%" + query + "%"
	where, args := f.where(viewerID)
	args = append([]any{like}, args...)
	total, err := d.postsTotal(ctx, f, viewerID, " AND p.content LIKE ?", like)
	if err != nil {
		return nil, 0, err
	}
	rows, err := d.DB.QueryContext(ctx, fmt.Sprintf(`
			SELECT %s FROM posts p JOIN users u ON u.id = p.author_id
			WHERE p.deleted_at IS NULL AND p.content LIKE ?%s
			ORDER BY p.id DESC LIMIT %d OFFSET %d`, postHitColumns, where, limit, offset), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	hits, err := scanPostHits(rows)
	return hits, total, err
}

// semanticPosts 语义检索动态：未配置 / 上游失败 / 无命中一律返回 ok=false，
// 由调用方回落关键词匹配——语义检索是增强能力，不得让搜索整体失败。
// 仅轻过滤（日期/媒体类型）时启用；重过滤走 SQL 路径（heavy()）。
func (d *Deps) semanticPosts(ctx context.Context, query string, f postsFilter, viewerID int64, limit int) ([]PostHit, bool) {
	if d.Search == nil || !d.Search.Enabled(ctx) || f.heavy() {
		return nil, false
	}
	k := limit
	if f.from != "" || f.to != "" || f.mediaType != "" {
		k = limit * 3
	}
	ids, err := d.Search.Query(ctx, query, k)
	if err != nil {
		slog.Warn("semantic search failed, falling back to keyword match", "err", err)
		return nil, false
	}
	if len(ids) == 0 {
		return nil, false
	}
	hits, err := d.postsByIDs(ctx, ids)
	if err != nil {
		slog.Warn("semantic search lookup failed, falling back to keyword match", "err", err)
		return nil, false
	}
	if len(hits) == 0 {
		return nil, false
	}
	// 语义召回后的轻过滤（本地日期口径与媒体类型；有过滤时过滤后为空是合法结果）
	kept := make([]PostHit, 0, len(hits))
	for _, h := range hits {
		d := utcToLocalDate(h.Date)
		if f.from != "" && d < f.from {
			continue
		}
		if f.to != "" && d > f.to {
			continue
		}
		kept = append(kept, h)
	}
	if f.mediaType != "" && len(kept) > 0 {
		kinds, err := d.mediaKinds(ctx, kept, f.mediaType)
		if err != nil {
			slog.Warn("semantic search media filter failed, falling back to keyword match", "err", err)
			return nil, false
		}
		filtered := kept[:0]
		for _, h := range kept {
			if kinds[h.ID] {
				filtered = append(filtered, h)
			}
		}
		kept = filtered
	}
	if len(kept) > limit {
		kept = kept[:limit]
	}
	return kept, true
}

// mediaKinds 返回命中动态中「含指定类型未删媒体」的 id 集合。
func (d *Deps) mediaKinds(ctx context.Context, hits []PostHit, kind string) (map[int64]bool, error) {
	ph := strings.TrimSuffix(strings.Repeat("?,", len(hits)), ",")
	args := make([]any, 0, len(hits)+1)
	args = append(args, kind)
	for _, h := range hits {
		args = append(args, h.ID)
	}
	rows, err := d.DB.QueryContext(ctx, fmt.Sprintf(
		`SELECT DISTINCT post_id FROM media WHERE kind = ? AND deleted_at IS NULL AND post_id IN (%s)`, ph), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	set := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		set[id] = true
	}
	return set, rows.Err()
}

// postsByIDs 按向量命中顺序取回未删动态。
func (d *Deps) postsByIDs(ctx context.Context, ids []int64) ([]PostHit, error) {
	posts, err := d.DB.PostsByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	hits := make([]PostHit, 0, len(posts))
	for _, p := range posts {
		hits = append(hits, PostHit{
			ID: p.ID, Content: p.Content, Date: p.CreatedAt,
			Author: p.AuthorNickname.String, LocationName: p.LocationName.String,
		})
	}
	return hits, nil
}

// promoteMediaTokens 把 media token 转正为媒体行；中途失败清理已转正文件。
func promoteMediaTokens(ctx context.Context, d *Deps, toks []string) ([]store.MediaRow, []*media.Promoted, error) {
	medias := make([]store.MediaRow, 0, len(toks))
	var promoted []*media.Promoted
	cleanup := func() {
		for _, pr := range promoted {
			d.MP.Delete(pr.FilePath, pr.ThumbPath)
		}
	}
	for _, token := range toks {
		p, err := d.MP.Promote(token)
		if err != nil {
			cleanup()
			return nil, nil, err
		}
		promoted = append(promoted, p)
		medias = append(medias, store.MediaRow{
			Kind: p.Kind, FilePath: p.FilePath, ThumbPath: store.NullStr(p.ThumbPath),
			Width: p.Width, Height: p.Height, Size: p.Size,
			Duration: sql.NullFloat64{Float64: p.Duration, Valid: p.Duration > 0},
			TakenAt:  store.NullStr(p.TakenAt),
		})
	}
	return medias, promoted, nil
}

// validatePostArgs 发布动态的成立条件（对齐 REST 与 ）：
// 正文 / 媒体 / 心情 / 地点至少一项非空，纯媒体动态合法。
// 卡阶段媒体尚未上传，以 media_refs（引用元数据）判定；执行阶段才是 media_tokens。
func validatePostArgs(args map[string]any) error {
	if str(args, "content") == "" && str(args, "mood") == "" && str(args, "place") == "" &&
		len(tokens(args, "media_tokens")) == 0 {
		if refs, ok := args["media_refs"].([]any); !ok || len(refs) == 0 {
			return fmt.Errorf("正文、媒体、心情、地点至少填一项")
		}
	}
	return nil
}

// postPreviewBody 发布动态的目标形态预览（与发布前预览同形）。
type postPreviewBody struct {
	Content     string          `json:"content,omitempty"`
	Mood        string          `json:"mood,omitempty"`
	Place       string          `json:"place,omitempty"`
	PlaceCity   string          `json:"place_city,omitempty"`
	Attachments []AttachmentRef `json:"attachments,omitempty"`
}

// albumPreviewBody 新建相册的目标形态预览。
type albumPreviewBody struct {
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Count       int        `json:"count"`
	Media       []UIRecord `json:"media,omitempty"` // 前 6 张缩略
}

// albumMembersPreviewBody 相册增删成员的目标形态预览。
type albumMembersPreviewBody struct {
	Album       UIRecord   `json:"album"`
	AddCount    int        `json:"add_count"`
	RemoveCount int        `json:"remove_count"`
	Add         []UIRecord `json:"add,omitempty"`
	Remove      []UIRecord `json:"remove,omitempty"`
}

// anniversaryPreviewBody 新建纪念日目标形态。
type anniversaryPreviewBody struct {
	Name             string `json:"name"`
	Date             string `json:"date"`
	Calendar         string `json:"calendar"` // solar | lunar
	IsLeap           bool   `json:"is_leap,omitempty"`
	NextSolarDate    string `json:"next_solar_date,omitempty"`
	RemindDaysBefore int    `json:"remind_days_before,omitempty"`
}

// reminderPreviewBody 提醒调整的状态变化预览：「提前 7 天 → 提前 3 天」。
type reminderPreviewBody struct {
	Anniversary UIRecord `json:"anniversary"`
	From        int      `json:"from"`
	To          int      `json:"to"`
}

// placePreviewBody 记录地点目标形态。
type placePreviewBody struct {
	Name        string  `json:"name"`
	Lat         float64 `json:"lat,omitempty"`
	Lng         float64 `json:"lng,omitempty"`
	VisitDate   string  `json:"visit_date,omitempty"`
	Description string  `json:"description,omitempty"`
	Resolved    bool    `json:"resolved"` // place_query 是否解析到坐标
}

// reactionPreviewBody 点赞/收藏的状态变化预览（幂等「设为」）。
type reactionPreviewBody struct {
	Post     UIRecord `json:"post"`
	Like     *fromTo  `json:"like,omitempty"`
	Favorite *fromTo  `json:"favorite,omitempty"`
}

type fromTo struct {
	From bool `json:"from"`
	To   bool `json:"to"`
}

// deletePreviewBody 删除（移入回收站）目标形态。
// 预览只增不改：单条走既有 text/date/thumb；批量走 items+count（AI模式设计「批量删除契约」）。
type deletePreviewBody struct {
	Kind  string              `json:"kind"`
	Text  string              `json:"text,omitempty"`
	Date  string              `json:"date,omitempty"`
	Thumb string              `json:"thumb,omitempty"`
	Items []deletePreviewItem `json:"items,omitempty"`
	Count int                 `json:"count,omitempty"`
}

// deletePreviewItem 批量删除预览行。
type deletePreviewItem struct {
	ID    int64  `json:"id"`
	Text  string `json:"text"`
	Date  string `json:"date,omitempty"`
	Thumb string `json:"thumb,omitempty"`
}

// mediaUIRecord 媒体行 → UI 条目（thumb 优先，音频/视频带时长角标）。
func mediaUIRecord(d *Deps, m *store.MediaRow) UIRecord {
	r := UIRecord{ID: m.ID, Kind: "media", Text: mediaKindLabel(m.Kind)}
	if m.ThumbPath.Valid && m.ThumbPath.String != "" && d.MP != nil {
		r.ThumbURL = d.MP.SignedURL(m.ID, "thumb")
	}
	r.Date = utcToLocalDate(m.SortTime())
	if m.Kind == "video" && m.Duration.Valid && m.Duration.Float64 > 0 {
		r.Badge = fmt.Sprintf("%d″", int(m.Duration.Float64))
	}
	return r
}

func mediaKindLabel(kind string) string {
	switch kind {
	case "photo":
		return "照片"
	case "video":
		return "视频"
	case "audio":
		return "录音"
	}
	return kind
}

// mediaThumbsByPostIDs 动态首图缩略图（查不到只是没图，不阻断）。
func (d *Deps) mediaThumbsByPostIDs(ctx context.Context, postIDs []int64) map[int64]string {
	out := map[int64]string{}
	medias, err := d.DB.GetMediaByPostIDs(ctx, postIDs)
	if err != nil {
		slog.Warn("post thumbs lookup failed", "err", err)
		return out
	}
	if d.MP == nil {
		return out
	}
	for pid, list := range medias {
		for _, m := range list {
			if m.ThumbPath.Valid && m.ThumbPath.String != "" {
				out[pid] = d.MP.SignedURL(m.ID, "thumb")
				break
			}
		}
	}
	return out
}

// mediaThumbsByIDs 媒体 id 集合的缩略图（失效 id 跳过）。
func (d *Deps) mediaThumbsByIDs(ctx context.Context, ids []int64) map[int64]string {
	out := map[int64]string{}
	if d.MP == nil || len(ids) == 0 {
		return out
	}
	rows, err := d.DB.GetMediaByIDs(ctx, ids)
	if err != nil {
		slog.Warn("media thumbs lookup failed", "err", err)
		return out
	}
	for _, m := range rows {
		if m.ThumbPath.Valid && m.ThumbPath.String != "" {
			out[m.ID] = d.MP.SignedURL(m.ID, "thumb")
		}
	}
	return out
}

// postRecords 动态命中 → UI 条目（带首图缩略）。
func (d *Deps) postRecords(ctx context.Context, hits []PostHit) []UIRecord {
	thumbs := d.mediaThumbsByPostIDs(ctx, hitIDs(hits))
	recs := make([]UIRecord, 0, len(hits))
	for _, h := range hits {
		r := UIRecord{
			ID:   h.ID,
			Kind: "post",
			Text: truncateRunes(strings.TrimSpace(h.Content), 60),
			Date: utcToLocalDate(h.Date),
		}
		if t, ok := thumbs[h.ID]; ok {
			r.ThumbURL = t
			r.Badge = "有图"
		}
		if h.LocationName != "" {
			r.Badge = strings.TrimSpace(r.Badge + " · " + h.LocationName)
		}
		recs = append(recs, r)
	}
	return recs
}

func hitIDs(hits []PostHit) []int64 {
	ids := make([]int64, 0, len(hits))
	for _, h := range hits {
		ids = append(ids, h.ID)
	}
	return ids
}

// recordIDs 取当前页展示条目的 id，用于落结果集句柄——句柄代表「这一卡上看到的这些」，
// 与 find_posts 同口径（翻页照常落新句柄）。
//
// 过滤 ID == 0：离线地名候选是「建议你去建这个点」而非已存在的记录（find_places 里
// 以 ID: 0 表示），把它放进句柄会让展开/批量删除带出一个不存在的 id：
// 批量删除会在第 0 条上就失败（整批事务化后更会让整批回滚）。
func recordIDs(recs []UIRecord) []int64 {
	ids := make([]int64, 0, len(recs))
	for _, r := range recs {
		if r.ID > 0 {
			ids = append(ids, r.ID)
		}
	}
	return ids
}

// attachmentsFromRefs 卡阶段引用元数据 → 提案附件清单。
func attachmentsFromRefs(args map[string]any) []AttachmentRef {
	refs, ok := args["media_refs"].([]any)
	if !ok {
		return nil
	}
	var out []AttachmentRef
	for _, r := range refs {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		ref := AttachmentRef{}
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

// ---- 对象摘要（提案预览引用既有记录时使用） ----

// postSummaryRecord 动态摘要条目；不存在（已删/越权 id）返回 ok=false。
func (d *Deps) postSummaryRecord(ctx context.Context, id int64) (UIRecord, bool) {
	p, err := d.DB.GetPost(ctx, id)
	if err != nil || p == nil {
		return UIRecord{ID: id, Kind: "post", Text: fmt.Sprintf("已删除的动态 #%d", id)}, false
	}
	r := UIRecord{ID: id, Kind: "post", Text: truncateRunes(strings.TrimSpace(p.Content), 40), Date: utcToLocalDate(p.CreatedAt)}
	if thumbs := d.mediaThumbsByPostIDs(ctx, []int64{id}); thumbs[id] != "" {
		r.ThumbURL = thumbs[id]
	}
	return r, true
}

// commentSummaryRecord 评论摘要条目。
func (d *Deps) commentSummaryRecord(ctx context.Context, id int64) (UIRecord, bool) {
	c, err := d.DB.GetComment(ctx, id)
	if err != nil || c == nil {
		return UIRecord{ID: id, Kind: "comment", Text: fmt.Sprintf("已删除的评论 #%d", id)}, false
	}
	return UIRecord{ID: id, Kind: "comment", Text: truncateRunes(strings.TrimSpace(c.Content), 40), Date: utcToLocalDate(c.CreatedAt)}, true
}

// albumSummaryRecord 相册摘要条目。
func (d *Deps) albumSummaryRecord(ctx context.Context, id int64) (UIRecord, bool) {
	a, err := d.DB.GetAlbum(ctx, id)
	if err != nil || a == nil {
		return UIRecord{ID: id, Kind: "album", Text: fmt.Sprintf("未知相册 #%d", id)}, false
	}
	r := UIRecord{ID: id, Kind: "album", Text: a.Name, Badge: fmt.Sprintf("%d 张", a.ItemCount)}
	if a.CoverMediaID.Valid && d.MP != nil {
		r.ThumbURL = d.MP.SignedURL(a.CoverMediaID.Int64, "thumb")
	}
	return r, true
}

// anniversarySummaryRecord 纪念日摘要条目。
func (d *Deps) anniversarySummaryRecord(ctx context.Context, id int64) (UIRecord, bool) {
	a, err := d.DB.GetAnniversary(ctx, id)
	if err != nil || a == nil {
		return UIRecord{ID: id, Kind: "anniversary", Text: fmt.Sprintf("未知纪念日 #%d", id)}, false
	}
	now := time.Now()
	return UIRecord{ID: id, Kind: "anniversary", Text: a.Name, Date: a.Date,
		Badge: fmt.Sprintf("还有 %d 天", a.DaysUntil(now))}, true
}

// placeSummaryRecord 地点摘要条目（未删点位：删除预览不该显示回收站里的点位）。
func (d *Deps) placeSummaryRecord(ctx context.Context, id int64) (UIRecord, bool) {
	p, err := d.DB.GetPointActive(ctx, id)
	if err != nil || p == nil {
		return UIRecord{ID: id, Kind: "place", Text: fmt.Sprintf("未知地点 #%d", id)}, false
	}
	return UIRecord{ID: id, Kind: "place", Text: p.Name, Badge: p.City, Date: p.VisitDate.String}, true
}

// mediaSummaryRecords 批量媒体摘要条目（失效 id 保留占位，让预览数与执行数一致）。
func (d *Deps) mediaSummaryRecords(ctx context.Context, ids []int64) []UIRecord {
	thumbs := d.mediaThumbsByIDs(ctx, ids)
	byID := map[int64]*store.MediaRow{}
	if rows, err := d.DB.GetMediaByIDs(ctx, ids); err == nil {
		for _, m := range rows {
			byID[m.ID] = m
		}
	}
	recs := make([]UIRecord, 0, len(ids))
	for _, id := range ids {
		if m, ok := byID[id]; ok {
			r := UIRecord{ID: id, Kind: "media", Text: mediaKindLabel(m.Kind), Date: utcToLocalDate(m.SortTime())}
			r.ThumbURL = thumbs[id]
			recs = append(recs, r)
		} else {
			recs = append(recs, UIRecord{ID: id, Kind: "media", Text: fmt.Sprintf("已删除的媒体 #%d", id)})
		}
	}
	return recs
}

// ---- 注册表 ----

// Registry 16 工具全量注册（读 7 / 提案 8 / 删除 1）。
func Registry() []*Tool {
	return []*Tool{
		{
			Name:          "find_posts",
			Category:      "动态",
			ProgressLabel: "搜索动态",
			Description: "查动态：query 语义/关键词检索，留空 = 按时间倒序浏览；" +
				"支持日期区间/媒体类型/心情/地点/作者/收藏过滤与那年今日。查询结果用户已在卡片里看到，不要逐条复述",
			Schema: map[string]string{
				"query":       "string — 语义检索词；留空 = 按时间倒序浏览",
				"from":        "string — 起始日期 YYYY-MM-DD（可选，含当天）",
				"to":          "string — 结束日期 YYYY-MM-DD（可选）",
				"media_type":  "string — 可选 photo / video / audio",
				"mood":        "string — 可选，心情筛选",
				"place":       "string — 可选，地点名筛选",
				"author":      "string — 可选 me / partner / both（缺省 both）",
				"favorited":   "boolean — 可选，true = 只看我收藏的",
				"on_this_day": "boolean — 可选，true = 那年今日（其余过滤忽略）",
				"sort":        "string — 可选 recent / relevant（带 query 时缺省 relevant）",
				"limit":       "number — 可选，条数上限（缺省 10，最大 50）",
				"offset":      "number — 可选，翻页偏移（缺省 0；语义检索路径不支持翻页）",
			},
			Params: map[string]any{"type": "object", "properties": map[string]any{
				"query":       map[string]any{"type": "string"},
				"from":        map[string]any{"type": "string"},
				"to":          map[string]any{"type": "string"},
				"media_type":  map[string]any{"type": "string", "enum": []string{"photo", "video", "audio"}},
				"mood":        map[string]any{"type": "string"},
				"place":       map[string]any{"type": "string"},
				"author":      map[string]any{"type": "string", "enum": []string{"me", "partner", "both"}},
				"favorited":   map[string]any{"type": "boolean"},
				"on_this_day": map[string]any{"type": "boolean"},
				"sort":        map[string]any{"type": "string", "enum": []string{"recent", "relevant"}},
				"limit":       map[string]any{"type": "integer"},
				"offset":      map[string]any{"type": "integer"},
			}},
			Run: func(ctx context.Context, d *Deps, userID int64, args map[string]any, mode ExecMode) (*ToolResult, error) {
				limit := int(num(args, "limit"))
				if limit <= 0 {
					limit = findPostsDefaultLimit
				}
				if limit > 50 {
					limit = 50
				}
				filter, err := parsePostsFilter(args)
				if err != nil {
					return nil, err
				}
				// author 语义：me/partner 按当前用户与其伴侣解析；both/未传 = 不限
				switch str(args, "author") {
				case "me":
					filter.authorIDs = []int64{userID}
				case "partner":
					ids, err := d.DB.ListPartnerIDs(ctx, userID)
					if err != nil {
						return nil, err
					}
					filter.authorIDs = ids
				}
				if boolArg(args, "favorited") {
					filter.favoritedBy = userID
				}
				header := postsEchoHeader(args)

				// 那年今日：独立口径（其余过滤忽略），SQL 下推（ListPostsOnMonthDay）
				if boolArg(args, "on_this_day") {
					posts, onTodayTotal, err := d.DB.ListPostsOnMonthDay(ctx, 50, limit)
					if err != nil {
						return nil, err
					}
					hits := make([]PostHit, 0, len(posts))
					for _, p := range posts {
						hits = append(hits, PostHit{ID: p.ID, Content: p.Content,
							Date: p.CreatedAt, Author: p.AuthorNickname.String, LocationName: p.LocationName.String})
					}
					recs := d.postRecords(ctx, hits)
					resultID := saveResultSet(ctx, "post", hitIDs(hits), nil)
					// 计数用真值（与列表查询同 WHERE 下推），不拿「命中数 == limit」猜余量
					ui := &UIBlock{Kind: "record_list", Title: "那年今日", Records: recs, ResultID: resultID, Selectable: true,
						Total: onTodayTotal, More: maxInt(0, onTodayTotal-len(recs))}
					return &ToolResult{ModelText: modelTextForList("那年今日（历年同月同日）", onTodayTotal, recs, resultID),
						UserText: userTextFor(len(hits), "条那年今日的回忆"), UI: ui}, nil
				}

				q := str(args, "query")
				offset := int(num(args, "offset"))
				if offset < 0 {
					offset = 0
				}
				var hits []PostHit
				var total = -1 // 语义路径无稳定总数
				if q == "" {
					hits, total, err = d.browsePosts(ctx, filter, userID, limit, offset)
				} else if h, ok := d.semanticPosts(ctx, q, filter, userID, limit); ok {
					hits = h
					// sort=recent：语义序（相关性）改回时间倒序
					if str(args, "sort") == "recent" {
						sort.SliceStable(hits, func(i, j int) bool { return hits[i].Date > hits[j].Date })
					}
					header += "（语义检索不支持翻页，可去掉 query 或指定 sort=recent）"
				} else {
					hits, total, err = d.keywordPosts(ctx, q, filter, userID, limit, offset)
				}
				if err != nil {
					return nil, err
				}
				recs := d.postRecords(ctx, hits)
				resultID := saveResultSet(ctx, "post", hitIDs(hits), nil)
				ui := &UIBlock{Kind: "record_list", Title: uiTitleForPosts(args), Records: recs, ResultID: resultID, Selectable: true}
				ui.Semantic = total < 0 // 语义路径：客户端据此标注「按相关性截取，非全部命中」
				if total >= 0 {
					ui.Total = total
					ui.More = maxInt(0, total-len(recs))
				}
				return &ToolResult{ModelText: modelTextForPage(header, total, recs, resultID, offset),
					UserText: userTextFor(len(hits), "条动态"), UI: ui}, nil
			},
		},
		{
			Name:          "find_albums",
			Category:      "相册",
			ProgressLabel: "查找相册",
			Description:   "查相册（组织视图，不含媒体本体）：名称/描述关键词与创建日期过滤。相册内媒体走 find_media 或 get_record(album)",
			Schema: map[string]string{
				"query":  "string — 名称/描述关键词（可选）",
				"from":   "string — 创建起始日期 YYYY-MM-DD（可选）",
				"to":     "string — 创建结束日期 YYYY-MM-DD（可选）",
				"limit":  "number — 可选，条数上限（缺省 10，最大 50）",
				"offset": "number — 可选，翻页偏移（缺省 0）",
			},
			Params: map[string]any{"type": "object", "properties": map[string]any{
				"query":  map[string]any{"type": "string"},
				"from":   map[string]any{"type": "string"},
				"to":     map[string]any{"type": "string"},
				"limit":  map[string]any{"type": "integer"},
				"offset": map[string]any{"type": "integer"},
			}},
			Run: func(ctx context.Context, d *Deps, userID int64, args map[string]any, mode ExecMode) (*ToolResult, error) {
				limit := int(num(args, "limit"))
				if limit <= 0 {
					limit = findPostsDefaultLimit
				}
				if limit > 50 {
					limit = 50
				}
				off := int(num(args, "offset"))
				if off < 0 {
					off = 0
				}
				from, to := "", ""
				for k, dst := range map[string]*string{"from": &from, "to": &to} {
					if v := str(args, k); v != "" {
						if _, err := time.Parse("2006-01-02", v); err != nil {
							return nil, fmt.Errorf("%s 日期无效: %s", k, v)
						}
						*dst = v
					}
				}
				albums, err := d.DB.ListAlbums(ctx)
				if err != nil {
					return nil, err
				}
				q := str(args, "query")
				recs := make([]UIRecord, 0, len(albums))
				total := 0
				for _, al := range albums {
					if q != "" && !strings.Contains(al.Name, q) && !strings.Contains(al.Description.String, q) {
						continue
					}
					if from != "" && al.CreatedAt[:10] < from {
						continue
					}
					if to != "" && al.CreatedAt[:10] > to {
						continue
					}
					total++
					if total <= off || len(recs) >= limit {
						continue
					}
					r := UIRecord{ID: al.ID, Kind: "album", Text: al.Name, Badge: fmt.Sprintf("%d 张", al.ItemCount)}
					if al.CoverMediaID.Valid && d.MP != nil {
						r.ThumbURL = d.MP.SignedURL(al.CoverMediaID.Int64, "thumb")
					}
					recs = append(recs, r)
				}
				header := "按条件" + postsEchoKV(args)
				resultID := saveResultSet(ctx, "album", recordIDs(recs), nil)
				ui := &UIBlock{Kind: "record_list", Title: "相册", Records: recs, ResultID: resultID, Selectable: true,
					Total: total, More: maxInt(0, total-len(recs))}
				return &ToolResult{ModelText: modelTextForPage(header, total, recs, "", off),
					UserText: userTextFor(total, "个相册"), UI: ui}, nil
			},
		},
		{
			Name:          "find_media",
			Category:      "相册",
			ProgressLabel: "查找媒体",
			Description: "查媒体本体（照片/视频/录音，时间轴=未归档）：日期/地点/类型过滤；" +
				"group_by 按日期或地点聚合未归档媒体（批量整理入口）。用户可在结果卡片里勾选后引用",
			Schema: map[string]string{
				"unfiled":    "boolean — 可选，true = 仅未归档（缺省 true，时间轴口径）",
				"album_id":   "number — 可选，查指定相册内媒体（已归档也能查；整理某相册用它）",
				"from":       "string — 起始日期 YYYY-MM-DD（可选）",
				"to":         "string — 结束日期 YYYY-MM-DD（可选）",
				"place":      "string — 可选，所属动态地点筛选",
				"media_type": "string — 可选 photo / video / audio",
				"group_by":   "string — 可选 date / place：返回分桶结果（批量建册入口）",
				"limit":      "number — 可选，条数上限（缺省 24，最大 100）",
				"offset":     "number — 可选，翻页偏移（缺省 0）",
			},
			Params: map[string]any{"type": "object", "properties": map[string]any{
				"unfiled":    map[string]any{"type": "boolean"},
				"album_id":   map[string]any{"type": "integer"},
				"from":       map[string]any{"type": "string"},
				"to":         map[string]any{"type": "string"},
				"place":      map[string]any{"type": "string"},
				"media_type": map[string]any{"type": "string", "enum": []string{"photo", "video", "audio"}},
				"group_by":   map[string]any{"type": "string", "enum": []string{"date", "place"}},
				"limit":      map[string]any{"type": "integer"},
				"offset":     map[string]any{"type": "integer"},
			}},
			Run: func(ctx context.Context, d *Deps, userID int64, args map[string]any, mode ExecMode) (*ToolResult, error) {
				limit := int(num(args, "limit"))
				if limit <= 0 {
					limit = 24
				}
				if limit > 100 {
					limit = 100
				}
				unfiled := true
				if v, ok := optBool(args, "unfiled"); ok {
					unfiled = v
				}
				offset := int(num(args, "offset"))
				if offset < 0 {
					offset = 0
				}
				f := store.MediaFilter{Unfiled: unfiled, Place: str(args, "place")}
				if aid := num(args, "album_id"); aid > 0 {
					// 指定相册：已归档媒体也能查（补 propose_album_members 的 remove 路径）
					if _, err := d.DB.GetAlbum(ctx, aid); err != nil {
						return nil, fmt.Errorf("相册不存在")
					}
					f.AlbumID = aid
					f.Unfiled = false
				}
				if mt := str(args, "media_type"); mt != "" {
					if !searchMediaTypes[mt] {
						return nil, fmt.Errorf("media_type 仅支持 photo / video / audio")
					}
					f.MediaType = mt
				}
				for k, dst := range map[string]*string{"from": &f.From, "to": &f.To} {
					if v := str(args, k); v != "" {
						if _, err := time.Parse("2006-01-02", v); err != nil {
							return nil, fmt.Errorf("%s 日期无效: %s", k, v)
						}
						*dst = v
					}
				}
				header := postsEchoKV(args)

				// 分桶：批量整理入口（吸收 suggest_photo_grouping）
				if gb := str(args, "group_by"); gb == "date" || gb == "place" {
					cands, err := d.DB.ListMediaGroupCandidates(ctx, f.From, f.To, f.Place, f.MediaType)
					if err != nil {
						return nil, err
					}
					groups := buildMediaGroups(cands, gb)
					allIDs := make([]int64, 0, len(cands))
					for _, c := range cands {
						allIDs = append(allIDs, c.MediaID)
					}
					resultID := saveResultSet(ctx, "media", allIDs, groupToStore(groups))
					recs := make([]UIRecord, 0, len(groups))
					thumbs := d.mediaThumbsByIDs(ctx, allIDs)
					for _, g := range groups {
						r := UIRecord{ID: 0, Kind: "media", GroupKey: g.Key, Text: g.Label, Badge: fmt.Sprintf("%d 张", len(g.IDs))}
						if t, ok := thumbs[g.IDs[0]]; ok {
							r.ThumbURL = t
						}
						recs = append(recs, r)
					}
					ui := &UIBlock{Kind: "media_groups", Title: "媒体分组", Records: recs, ResultID: resultID, Selectable: true}
					mt := modelTextForList(header+"，按 "+gb+" 分组，", len(allIDs), recs, resultID)
					return &ToolResult{ModelText: mt, UserText: userTextFor(len(allIDs), "张未归档媒体"), UI: ui}, nil
				}

				total, err := d.DB.CountMediaFiltered(ctx, f)
				if err != nil {
					return nil, err
				}
				medias, err := d.DB.ListMediaFiltered(ctx, f, limit, offset)
				if err != nil {
					return nil, err
				}
				recs := make([]UIRecord, 0, len(medias))
				ids := make([]int64, 0, len(medias))
				for _, m := range medias {
					recs = append(recs, mediaUIRecord(d, m))
					ids = append(ids, m.ID)
				}
				resultID := saveResultSet(ctx, "media", ids, nil)
				ui := &UIBlock{Kind: "media_grid", Title: "媒体", Records: recs, ResultID: resultID,
					Selectable: true, Total: total, More: maxInt(0, total-len(recs))}
				return &ToolResult{ModelText: modelTextForPage(header, total, recs, resultID, offset),
					UserText: userTextFor(total, "条媒体"), UI: ui}, nil
			},
		},
		{
			Name:          "find_anniversaries",
			Category:      "纪念日",
			ProgressLabel: "查找纪念日",
			Description:   "查纪念日列表（含农历换算后的下次公历日与剩余天数）",
			Schema: map[string]string{
				"upcoming_only": "boolean — 可选，true = 只看未过的",
				"limit":         "number — 可选，条数上限（缺省 10）",
				"offset":        "number — 可选，翻页偏移（缺省 0）",
			},
			Params: map[string]any{"type": "object", "properties": map[string]any{
				"upcoming_only": map[string]any{"type": "boolean"},
				"limit":         map[string]any{"type": "integer"},
				"offset":        map[string]any{"type": "integer"},
			}},
			Run: func(ctx context.Context, d *Deps, userID int64, args map[string]any, mode ExecMode) (*ToolResult, error) {
				limit := int(num(args, "limit"))
				if limit <= 0 {
					limit = findPostsDefaultLimit
				}
				if limit > 50 { // 上限与其余 find_* 一致（media 100、其余 50）
					limit = 50
				}
				offset := int(num(args, "offset"))
				if offset < 0 {
					offset = 0
				}
				list, err := d.DB.ListAnniversaries(ctx)
				if err != nil {
					return nil, err
				}
				now := time.Now()
				upcoming := boolArg(args, "upcoming_only")
				recs := make([]UIRecord, 0, len(list))
				total := 0
				for _, a := range list {
					days := a.DaysUntil(now)
					if upcoming && days < 0 {
						continue
					}
					total++
					if len(recs) >= limit || total <= offset {
						continue
					}
					text := a.Name
					if a.Type == "lunar" {
						text += "（农历"
						if a.IsLeap {
							text += "·闰月"
						}
						text += "）"
					}
					recs = append(recs, UIRecord{ID: a.ID, Kind: "anniversary", Text: text,
						Date: a.NextSolarDate(now), Badge: fmt.Sprintf("还有 %d 天", days)})
				}
				resultID := saveResultSet(ctx, "anniversary", recordIDs(recs), nil)
				ui := &UIBlock{Kind: "record_list", Title: "纪念日", Records: recs, ResultID: resultID, Selectable: true,
					Total: total, More: maxInt(0, total-len(recs))}
				return &ToolResult{ModelText: modelTextForPage("纪念日列表，", total, recs, "", offset),
					UserText: userTextFor(total, "个纪念日"), UI: ui}, nil
			},
		},
		{
			Name:          "find_places",
			Category:      "地图",
			ProgressLabel: "查找地点",
			Description:   "查地点：已记录的足迹点位 + 离线地名候选（geocode）。候选用于 propose_place 的 place_query",
			Schema: map[string]string{
				"query":        "string — 地点关键词（可选）",
				"visited_only": "boolean — 可选，true = 只看已记录点位",
				"limit":        "number — 可选，条数上限（缺省 10）",
				"offset":       "number — 可选，翻页偏移（缺省 0，只对已记录点位生效）",
			},
			Params: map[string]any{"type": "object", "properties": map[string]any{
				"query":        map[string]any{"type": "string"},
				"visited_only": map[string]any{"type": "boolean"},
				"limit":        map[string]any{"type": "integer"},
				"offset":       map[string]any{"type": "integer"},
			}},
			Run: func(ctx context.Context, d *Deps, userID int64, args map[string]any, mode ExecMode) (*ToolResult, error) {
				limit := int(num(args, "limit"))
				if limit <= 0 {
					limit = findPostsDefaultLimit
				}
				if limit > 50 { // 上限与其余 find_* 一致（media 100、其余 50）
					limit = 50
				}
				offset := int(num(args, "offset"))
				if offset < 0 {
					offset = 0
				}
				q := str(args, "query")
				visitedOnly := boolArg(args, "visited_only")
				recs := make([]UIRecord, 0, 16)
				var pts []*store.MapPoint
				var err error
				// 取全量再切片：SQL 层先 LIMIT 会让 offset 超过 limit 的页永远为空
				// （AI模式设计「计数与翻页」：offset 推进 + total 用真值）。
				if q != "" {
					pts, err = d.DB.SearchPoints(ctx, q, 0)
				} else {
					pts, err = d.DB.ListPoints(ctx)
				}
				if err != nil {
					return nil, err
				}
				visitedTotal := len(pts)
				visited := 0
				for _, p := range pts {
					visited++
					if visited <= offset {
						continue
					}
					r := UIRecord{ID: p.ID, Kind: "place", Text: p.Name, Badge: p.City, Date: p.VisitDate.String}
					if p.Description != "" {
						r.Text += " — " + truncateRunes(p.Description, 30)
					}
					recs = append(recs, r)
					if len(recs) >= limit {
						break
					}
				}
				// 余量按「总数 - 已跳过 - 已展示」算：用 visited 会把跳过的 offset 条也算进余量
				// （翻到第 2 页时余量偏大 offset 条），与 UIBlock.more = total - len(records) 口径不符。
				more := maxInt(0, visitedTotal-offset-len(recs))
				if !visitedOnly {
					// 离线地名候选：已有点位之外的补全（模型凭它调 propose_place）
					cands := geo.SearchPlaces(q, maxInt(0, limit-len(recs)))
					for _, c := range cands {
						dup := false
						for _, p := range pts {
							if p.Name == c.Name {
								dup = true
								break
							}
						}
						if !dup {
							recs = append(recs, UIRecord{ID: 0, Kind: "place", Text: c.Display, Badge: "候选"})
						}
					}
				}
				resultID := saveResultSet(ctx, "place", recordIDs(recs), nil)
				ui := &UIBlock{Kind: "place_candidates", Title: "地点", Records: recs, ResultID: resultID, Selectable: true,
					More: more, SinglePick: true, Total: maxInt(visitedTotal, len(recs))}
				mt := modelTextForList(postsEchoKV(args), visited+maxInt(0, len(recs)-visited), recs, "")
				return &ToolResult{ModelText: mt, UserText: userTextFor(visited, "个足迹"), UI: ui}, nil
			},
		},
		{
			Name:          "get_record",
			Category:      "动态",
			ProgressLabel: "查看详情",
			Description:   "取单条详情：动态（含评论与媒体清单）/ 相册（含成员）/ 纪念日 / 地点（含关联动态）/ 评论 / 媒体",
			Schema: map[string]string{
				"kind": "string — post / album / anniversary / place / comment / media",
				"id":   "number — 记录 ID",
			},
			Params: map[string]any{"type": "object", "properties": map[string]any{
				"kind": map[string]any{"type": "string", "enum": []string{"post", "album", "anniversary", "place", "comment", "media"}},
				"id":   map[string]any{"type": "integer"},
			}, "required": []string{"kind", "id"}},
			Run: func(ctx context.Context, d *Deps, userID int64, args map[string]any, mode ExecMode) (*ToolResult, error) {
				kind, id := str(args, "kind"), num(args, "id")
				if id <= 0 {
					return nil, fmt.Errorf("id 必填")
				}
				switch kind {
				case "post":
					p, err := d.DB.GetPost(ctx, id)
					if err != nil {
						return nil, fmt.Errorf("动态不存在")
					}
					var mt strings.Builder
					fmt.Fprintf(&mt, "动态 #%d（%s", p.ID, utcToLocalDate(p.CreatedAt))
					if p.AuthorNickname.Valid && p.AuthorNickname.String != "" {
						fmt.Fprintf(&mt, "，%s", p.AuthorNickname.String)
					}
					mt.WriteString("）")
					if p.Mood.Valid && p.Mood.String != "" {
						fmt.Fprintf(&mt, " 心情:%s", p.Mood.String)
					}
					if p.LocationName.Valid && p.LocationName.String != "" {
						fmt.Fprintf(&mt, " 地点:%s", p.LocationName.String)
					}
					mt.WriteString("：" + truncateRunes(p.Content, 500))
					medias, _ := d.DB.GetMediaByPostIDs(ctx, []int64{id})
					recs := []UIRecord{}
					if list := medias[id]; len(list) > 0 {
						fmt.Fprintf(&mt, "（含 %d 条媒体", len(list))
						// 详情清单截断：最多 50 条（口径与相册详情一致，AI模式设计「统计与计数口径」）。
						// 截断时按该文档要求注明「已列前 N 条」与完整清单的取法——动态没有 album_id 可传，
						// 故提示按时间范围查（不能照抄相册那句 find_media(album_id=…)）。
						const detailLimitMedia = 50
						if len(list) > detailLimitMedia {
							// 提示要照着**可用参数**写：find_media 缺省只看未归档（unfiled=true），
							// 而本动态的媒体可能已归入相册，故必须显式带 unfiled=false，否则补查会漏。
							fmt.Fprintf(&mt, "，已列前 %d 条，完整清单用 find_media(unfiled=false) 按时间范围查", detailLimitMedia)
							list = list[:detailLimitMedia]
						}
						for _, m := range list {
							recs = append(recs, mediaUIRecord(d, m))
						}
						mt.WriteString("）")
					}
					comments, _ := d.DB.ListComments(ctx, id, 10, 0)
					if len(comments) > 0 {
						mt.WriteString("；评论：")
						for i, c := range comments {
							if i > 0 {
								mt.WriteString("；")
							}
							fmt.Fprintf(&mt, "#%d %s: %s", c.ID, c.AuthorNickname.String, truncateRunes(c.Content, 40))
						}
					}
					r := UIRecord{ID: p.ID, Kind: "post", Text: truncateRunes(strings.TrimSpace(p.Content), 60), Date: utcToLocalDate(p.CreatedAt)}
					if thumbs := d.mediaThumbsByPostIDs(ctx, []int64{id}); thumbs[id] != "" {
						r.ThumbURL = thumbs[id]
					}
					ui := &UIBlock{Kind: "record_detail", Title: "动态详情", Records: append([]UIRecord{r}, recs...)}
					return &ToolResult{ModelText: mt.String(), UI: ui}, nil
				case "album":
					a, err := d.DB.GetAlbum(ctx, id)
					if err != nil {
						return nil, fmt.Errorf("相册不存在")
					}
					medias, _ := d.DB.ListAlbumMedia(ctx, id)
					// 详情清单截断：最多下发 50 条（AI模式设计「统计与计数口径」）；
					// 详情块不可勾选，大相册浏览与多选走 find_media(album_id=…)
					const detailLimit = 50
					total := len(medias)
					if total > detailLimit {
						medias = medias[:detailLimit]
					}
					recs := make([]UIRecord, 0, len(medias))
					var mt strings.Builder
					fmt.Fprintf(&mt, "相册「%s」（%d 条媒体）", a.Name, total)
					if total > detailLimit {
						fmt.Fprintf(&mt, "，已列前 %d 条，完整清单用 find_media(album_id=%d)", detailLimit, id)
					}
					if a.Description.Valid && a.Description.String != "" {
						fmt.Fprintf(&mt, " %s", a.Description.String)
					}
					for _, m := range medias {
						recs = append(recs, mediaUIRecord(d, m))
					}
					r := UIRecord{ID: a.ID, Kind: "album", Text: a.Name, Badge: fmt.Sprintf("%d 张", total)}
					ui := &UIBlock{Kind: "record_detail", Title: "相册详情", Records: append([]UIRecord{r}, recs...)}
					return &ToolResult{ModelText: mt.String(), UI: ui}, nil
				case "anniversary":
					a, err := d.DB.GetAnniversary(ctx, id)
					if err != nil {
						return nil, fmt.Errorf("纪念日不存在")
					}
					now := time.Now()
					cal := "公历"
					if a.Type == "lunar" {
						cal = "农历"
						if a.IsLeap {
							cal += "（闰月）"
						}
					}
					mt := fmt.Sprintf("纪念日「%s」%s（%s），下次公历 %s，还有 %d 天，提前 %d 天提醒",
						a.Name, a.Date, cal, a.NextSolarDate(now), a.DaysUntil(now), a.RemindDaysBefore)
					ui := &UIBlock{Kind: "record_detail", Title: "纪念日详情", Records: []UIRecord{
						{ID: a.ID, Kind: "anniversary", Text: a.Name, Date: a.Date, Badge: cal}}}
					return &ToolResult{ModelText: mt, UI: ui}, nil
				case "place":
					// 未删点位：回收站数据对 AI 不可见（ARCHITECTURE「删除与恢复流程」）。
					p, err := d.DB.GetPointActive(ctx, id)
					if err != nil {
						return nil, fmt.Errorf("地点不存在")
					}
					posts, _ := d.DB.ListPostsByPoint(ctx, id, 5)
					mt := fmt.Sprintf("地点「%s」（%s）坐标 %.5f, %.5f", p.Name, p.City, p.Lat, p.Lng)
					if p.VisitDate.Valid && p.VisitDate.String != "" {
						mt += "，到访 " + p.VisitDate.String
					}
					if p.Description != "" {
						mt += "：" + truncateRunes(p.Description, 100)
					}
					if len(posts) > 0 {
						mt += "；关联动态：" + truncateRunes(posts[0].Content, 40)
						if len(posts) > 1 {
							mt += fmt.Sprintf(" 等 %d 条", len(posts))
						}
					}
					ui := &UIBlock{Kind: "record_detail", Title: "地点详情", Records: []UIRecord{
						{ID: p.ID, Kind: "place", Text: p.Name, Badge: p.City, Date: p.VisitDate.String}}}
					return &ToolResult{ModelText: mt, UI: ui}, nil
				case "comment":
					c, err := d.DB.GetComment(ctx, id)
					if err != nil {
						return nil, fmt.Errorf("评论不存在")
					}
					mt := fmt.Sprintf("评论 #%d（动态 #%d 下，%s）：%s", c.ID, c.PostID, c.AuthorNickname.String, truncateRunes(c.Content, 200))
					ui := &UIBlock{Kind: "record_detail", Title: "评论详情", Records: []UIRecord{
						{ID: c.ID, Kind: "comment", Text: truncateRunes(c.Content, 60), Date: utcToLocalDate(c.CreatedAt)}}}
					return &ToolResult{ModelText: mt, UI: ui}, nil
				case "media":
					m, err := d.DB.GetMedia(ctx, id)
					if err != nil {
						return nil, fmt.Errorf("媒体不存在")
					}
					mt := fmt.Sprintf("媒体 #%d（%s，%s）", m.ID, mediaKindLabel(m.Kind), utcToLocalDate(m.SortTime()))
					if m.PostID > 0 {
						if p, perr := d.DB.GetPost(ctx, m.PostID); perr == nil && p != nil {
							mt += "，来自动态 #" + fmt.Sprint(m.PostID) + "「" + truncateRunes(p.Content, 40) + "」"
						}
					}
					ui := &UIBlock{Kind: "record_detail", Title: "媒体详情", Records: []UIRecord{mediaUIRecord(d, m)}}
					return &ToolResult{ModelText: mt, UI: ui}, nil
				default:
					return nil, fmt.Errorf("kind 仅支持 post / album / anniversary / place / comment / media")
				}
			},
		},
		getSpaceStatsTool(),
		proposePostTool(),
		proposeCommentTool(),
		proposeAlbumTool(),
		proposeAlbumMembersTool(),
		proposeAnniversaryTool(),
		proposeReminderTool(),
		proposePlaceTool(),
		proposeReactionTool(),
		proposeDeleteTool(),
	}
}

// ---- 回显与文案辅助（ModelText 规则 1：必带查询条件回显） ----

func postsEchoKV(args map[string]any) string {
	var conds []string
	for _, k := range []string{"query", "from", "to", "media_type", "mood", "place", "author", "favorited", "on_this_day", "group_by"} {
		if v, ok := args[k]; ok && v != nil && fmt.Sprint(v) != "" && fmt.Sprint(v) != "false" {
			conds = append(conds, k+"="+fmt.Sprint(v))
		}
	}
	if len(conds) == 0 {
		return "浏览全部"
	}
	return "按 " + strings.Join(conds, " ") + " 查询"
}

func postsEchoHeader(args map[string]any) string {
	return postsEchoKV(args) + "动态，"
}

func uiTitleForPosts(args map[string]any) string {
	if boolArg(args, "on_this_day") {
		return "那年今日"
	}
	if q := str(args, "query"); q != "" {
		return "搜索：" + q
	}
	return "动态"
}

// userTextFor 读工具的一句人话；空结果给「没找到」口径。
func userTextFor(n int, unit string) string {
	if n == 0 {
		return "没找到相关内容"
	}
	return fmt.Sprintf("找到 %d %s", n, unit)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// buildMediaGroups 分桶候选 → 分组（group_by=date|place）。
type mediaGroup struct {
	Key   string
	Label string
	IDs   []int64
}

func buildMediaGroups(cands []store.GroupCandidate, by string) []mediaGroup {
	var groups []mediaGroup
	index := map[string]int{}
	for _, c := range cands {
		key, label := c.Date, c.Date
		if by == "place" {
			key, label = c.Location, c.Location
			if label == "" {
				key, label = "未记录地点", "未记录地点"
			}
		}
		i, ok := index[key]
		if !ok {
			groups = append(groups, mediaGroup{Key: key, Label: label})
			i = len(groups) - 1
			index[key] = i
		}
		groups[i].IDs = append(groups[i].IDs, c.MediaID)
	}
	return groups
}

func groupToStore(groups []mediaGroup) []store.ResultGroup {
	out := make([]store.ResultGroup, 0, len(groups))
	for _, g := range groups {
		out = append(out, store.ResultGroup{Key: g.Key, Label: g.Label, IDs: g.IDs})
	}
	return out
}

// ---- 提案类工具（8 个）与删除类（1 个） ----

// resolvePlace 地名 → 离线地理编码坐标（未收录返回 ok=false，调用方降级为纯文本地名）。
func resolvePlace(name string) (geo.Place, bool) {
	if strings.TrimSpace(name) == "" {
		return geo.Place{}, false
	}
	return geo.LocatePlace(name)
}

// proposalResult 提案类工具的 Propose 模式统一返回。
func proposalResult(p *Proposal, userText string) *ToolResult {
	return &ToolResult{
		ModelText: "已生成确认卡片（" + p.Summary + "），等待用户确认后执行。",
		UserText:  userText,
		Proposal:  p,
	}
}

// proposePostTool 发布动态（提案）。
func proposePostTool() *Tool {
	return &Tool{
		Name:          "propose_post",
		Category:      "动态",
		ProgressLabel: "发布动态",
		Description: "提案发布动态（需用户确认后生效）：正文/媒体/心情/地点至少一项；" +
			"用户消息附带媒体附件时，只填文本参数，附件由系统自动挂上",
		Schema: map[string]string{
			"content": "string — 动态正文（可选，纯媒体可留空）",
			"mood":    "string — 心情（可选）",
			"place":   "string — 地点名（可选，服务端解析坐标）",
		},
		Params: map[string]any{"type": "object", "properties": map[string]any{
			"content": map[string]any{"type": "string"},
			"mood":    map[string]any{"type": "string"},
			"place":   map[string]any{"type": "string"},
		}},
		IsWrite:  true,
		Validate: validatePostArgs,
		Run: func(ctx context.Context, d *Deps, userID int64, args map[string]any, mode ExecMode) (*ToolResult, error) {
			// 模型调用用 place；落库 params 用 location_name（执行参数口径）。两者都认。
			place := str(args, "place")
			if place == "" {
				place = str(args, "location_name")
			}
			content, mood := str(args, "content"), str(args, "mood")
			atts := attachmentsFromRefs(args)
			if mode == ModeExecute {
				if err := validatePostArgs(args); err != nil {
					return nil, err
				}
				toks := tokens(args, "media_tokens")
				medias, promoted, err := promoteMediaTokens(ctx, d, toks)
				if err != nil {
					return nil, err
				}
				post := &store.Post{AuthorID: userID, Content: content, Mood: store.NullStr(mood)}
				if place != "" {
					post.LocationName = store.NullStr(place)
					if p, ok := resolvePlace(place); ok {
						// 动态带地点自动生成的点位不另发通知：该次发布已有 post_created
						// （ARCHITECTURE「通知分发器」事件源：足迹事件仅手动建点 / AI 口语报点产生）
						pt, _, perr := d.DB.FindOrCreatePoint(ctx, store.PointInput{
							Name: p.Display, Lat: p.Lat, Lng: p.Lng, Source: "ai", UserID: userID,
						})
						if perr == nil && pt != nil {
							post.PointID = sql.NullInt64{Int64: pt.ID, Valid: true}
						}
					}
				}
				postID, err := d.DB.CreatePostWithMedia(ctx, post, medias)
				if err != nil {
					for _, pr := range promoted {
						d.MP.Delete(pr.FilePath, pr.ThumbPath)
					}
					return nil, err
				}
				op := d.aiOperatorName(ctx, userID)
				snapshot, _ := json.Marshal(map[string]any{"content": content, "operator": op, "via": "ai"})
				// 推送正文带操作者与「（AI）」标记，接收端据此判别来源
				body := op + "（AI）发布了新动态"
				if t := textutil.TruncMark(content, 100); t != "" {
					body += "：" + t
				}
				d.notifyPartners(ctx, userID, "post_created", "post", postID, string(snapshot), "新动态", body)
				return &ToolResult{
					ModelText: fmt.Sprintf("动态 #%d 已发布。", postID),
					UserText:  fmt.Sprintf("动态 #%d 已发布", postID),
					Record:    &RecordRef{Kind: "post", ID: postID},
				}, nil
			}
			// Propose：校验已由 Invoke 前置完成；构造目标形态预览 + 展开后的执行参数
			body := postPreviewBody{Content: content, Mood: mood, Place: place, Attachments: atts}
			summary := "发布一条动态"
			if content == "" {
				if len(atts) > 0 {
					summary = fmt.Sprintf("发布一条含 %d 张媒体的动态", len(atts))
				} else {
					summary = "发布一条动态"
				}
			} else {
				summary = "发布动态：「" + truncateRunes(content, 20) + "」"
			}
			params := map[string]any{}
			if content != "" {
				params["content"] = content
			}
			if mood != "" {
				params["mood"] = mood
			}
			if place != "" {
				params["location_name"] = place
				if p, ok := resolvePlace(place); ok {
					params["latitude"] = p.Lat
					params["longitude"] = p.Lng
					body.PlaceCity = p.City
				}
			}
			return proposalResult(&Proposal{
				Intent: "propose_post", Summary: summary,
				Preview:     ProposalPreview{Kind: "post", Body: body},
				Attachments: atts, Params: params,
			}, "准备发布动态，请确认"), nil
		},
	}
}

// proposeCommentTool 评论动态（提案）。
func proposeCommentTool() *Tool {
	return &Tool{
		Name: "propose_comment", Category: "互动", ProgressLabel: "评论动态",
		Description: "提案评论某条动态（需用户确认后生效，最多 500 字）",
		Schema: map[string]string{
			"post_id": "number — 动态 ID",
			"content": "string — 评论内容",
		},
		Params: map[string]any{"type": "object", "properties": map[string]any{
			"post_id": map[string]any{"type": "integer"},
			"content": map[string]any{"type": "string"},
		}, "required": []string{"post_id", "content"}},
		IsWrite: true,
		Validate: func(args map[string]any) error {
			if len([]rune(str(args, "content"))) > 500 {
				return fmt.Errorf("评论最多 500 字")
			}
			return nil
		},
		Run: func(ctx context.Context, d *Deps, userID int64, args map[string]any, mode ExecMode) (*ToolResult, error) {
			postID := num(args, "post_id")
			content := str(args, "content")
			// 点选优先：勾选存在时评论只能落在勾选的那条动态上（不能评论别的）
			if err := checkQuotedSubset(ctx, "post", []int64{postID}); err != nil {
				return nil, err
			}
			if mode == ModeExecute {
				if _, err := d.DB.GetPost(ctx, postID); err != nil {
					return nil, fmt.Errorf("动态不存在")
				}
				commentID, err := d.DB.CreateComment(ctx, postID, userID, content)
				if err != nil {
					return nil, err
				}
				operator, err := d.DB.GetUserByID(ctx, userID)
				if err != nil {
					return nil, err
				}
				snapshot, _ := json.Marshal(map[string]any{"content": textutil.Trunc(content, 50), "operator": operator.Username, "via": "ai"})
				d.notifyPartners(ctx, userID, "post_commented", "post", postID, string(snapshot),
					"收到评论", operator.Username+"（AI）："+textutil.Trunc(content, 50))
				return &ToolResult{ModelText: fmt.Sprintf("评论 #%d 已发表。", commentID),
					UserText: "评论已发表", Record: &RecordRef{Kind: "comment", ID: commentID}}, nil
			}
			post, ok := d.postSummaryRecord(ctx, postID)
			if !ok {
				return nil, fmt.Errorf("动态不存在")
			}
			return proposalResult(&Proposal{
				Intent: "propose_comment", Summary: "发表评论",
				Preview: ProposalPreview{Kind: "comment", Body: map[string]any{"post": post, "content": content}},
				Params:  map[string]any{"post_id": postID, "content": content},
			}, "准备发表评论，请确认"), nil
		},
	}
}

// proposeAlbumTool 新建相册（可带首批媒体 = 一次确认）。
func proposeAlbumTool() *Tool {
	return &Tool{
		Name: "propose_album", Category: "相册", ProgressLabel: "新建相册",
		Description: "提案新建相册（需用户确认后生效）。首批媒体用 source={result_id, group?} 引用查询结果集，" +
			"或 media_ids 少量逐个指定；模型不要搬运长 id 列表",
		Schema: map[string]string{
			"name":        "string — 相册名",
			"description": "string — 描述（可选）",
			"source":      "object — {result_id, group?}：引用结果集（或其中一组）作为首批媒体",
			"media_ids":   "number[] — 媒体 ID（少量指定时用；与 source 二选一）",
		},
		Params: map[string]any{"type": "object", "properties": map[string]any{
			"name":        map[string]any{"type": "string"},
			"description": map[string]any{"type": "string"},
			"source": map[string]any{"type": "object", "properties": map[string]any{
				"result_id": map[string]any{"type": "string"},
				"group":     map[string]any{"type": "string"},
			}},
			"media_ids": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}},
		}, "required": []string{"name"}},
		IsWrite: true,
		Run: func(ctx context.Context, d *Deps, userID int64, args map[string]any, mode ExecMode) (*ToolResult, error) {
			name := str(args, "name")
			if name == "" {
				return nil, fmt.Errorf("name 不能为空")
			}
			mediaIDs, err := expandMediaArgs(ctx, d, userID, args)
			if err != nil {
				return nil, err
			}
			if mode == ModeExecute {
				// 建册与首批媒体同事务：加媒体失败则相册也不落，不留「已建册但媒体没进」的半成品
				albumID, err := d.DB.CreateAlbumWithItems(ctx, name, str(args, "description"), userID, mediaIDs)
				if err != nil {
					return nil, err
				}
				operator := d.aiOperatorName(ctx, userID)
				snapshot, _ := json.Marshal(map[string]any{"name": name, "operator": operator, "via": "ai"})
				d.notifyPartners(ctx, userID, "album_created", "album", albumID, string(snapshot),
					"相册上新", operator+"（AI）创建了相册「"+name+"」")
				return &ToolResult{ModelText: fmt.Sprintf("相册「%s」已创建（#%d，%d 条媒体）。", name, albumID, len(mediaIDs)),
					UserText: fmt.Sprintf("相册「%s」已创建", name), Record: &RecordRef{Kind: "album", ID: albumID}}, nil
			}
			mediaIDs, warnings := filterUnowned(ctx, d, mediaIDs)
			recs := d.mediaSummaryRecords(ctx, mediaIDs)
			if len(recs) > 6 {
				recs = recs[:6]
			}
			params := map[string]any{"name": name}
			if desc := str(args, "description"); desc != "" {
				params["description"] = desc
			}
			if len(mediaIDs) > 0 {
				params["media_ids"] = int64sToAny(mediaIDs)
			}
			return proposalResult(&Proposal{
				Intent: "propose_album", Summary: "新建相册「" + name + "」（" + fmt.Sprint(len(mediaIDs)) + " 张）",
				Preview: ProposalPreview{Kind: "album", Body: albumPreviewBody{
					Name: name, Description: str(args, "description"), Count: len(mediaIDs), Media: recs}},
				Warnings: warnings, Params: params,
			}, fmt.Sprintf("准备新建相册「%s」，请确认", name)), nil
		},
	}
}

// proposeAlbumMembersTool 相册增删成员（合并 add/remove，一次确认）。
func proposeAlbumMembersTool() *Tool {
	return &Tool{
		Name: "propose_album_members", Category: "相册", ProgressLabel: "整理相册",
		Description: "提案相册增删媒体（需用户确认后生效，一次确认完成增删）。" +
			"批量用 source={result_id, group?} 引用结果集，少量用 *_media_ids 逐个指定",
		Schema: map[string]string{
			"album_id":         "number — 相册 ID",
			"add_source":       "object — {result_id, group?}：加入结果集（或其中一组）",
			"add_media_ids":    "number[] — 加入的媒体 ID（与 add_source 二选一）",
			"remove_source":    "object — {result_id, group?}：移出结果集（或其中一组）",
			"remove_media_ids": "number[] — 移出的媒体 ID（与 remove_source 二选一）",
		},
		Params: map[string]any{"type": "object", "properties": map[string]any{
			"album_id":         map[string]any{"type": "integer"},
			"add_source":       map[string]any{"type": "object"},
			"add_media_ids":    map[string]any{"type": "array", "items": map[string]any{"type": "integer"}},
			"remove_source":    map[string]any{"type": "object"},
			"remove_media_ids": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}},
		}, "required": []string{"album_id"}},
		IsWrite: true,
		Validate: func(args map[string]any) error {
			// 相册只从动态媒体池选入：随 resolve 上传的新附件 token 直接拒绝（媒体只能随动态发布）
			if len(tokens(args, "media_tokens")) > 0 {
				return fmt.Errorf("新附件不能直接加入相册：媒体只能随动态发布，请先发布动态再选入相册")
			}
			return nil
		},
		Run: func(ctx context.Context, d *Deps, userID int64, args map[string]any, mode ExecMode) (*ToolResult, error) {
			albumID := num(args, "album_id")
			addIDs, err := expandNamedMediaArgs(ctx, d, userID, args, "add")
			if err != nil {
				return nil, err
			}
			removeIDs, err := expandNamedMediaArgs(ctx, d, userID, args, "remove")
			if err != nil {
				return nil, err
			}
			if len(addIDs) == 0 && len(removeIDs) == 0 {
				return nil, fmt.Errorf("add_source/add_media_ids 与 remove_source/remove_media_ids 至少给一项")
			}
			album, ok := d.albumSummaryRecord(ctx, albumID)
			if !ok {
				return nil, fmt.Errorf("相册不存在")
			}
			if mode == ModeExecute {
				// 增与删同事务：加入的预检失败时移出也不落（一次确认 = 一个原子步）
				if err := d.DB.UpdateAlbumMembers(ctx, albumID, userID, addIDs, removeIDs); err != nil {
					return nil, err
				}
				return &ToolResult{ModelText: fmt.Sprintf("相册「%s」已更新：加入 %d 条、移出 %d 条。", album.Text, len(addIDs), len(removeIDs)),
					UserText: "相册已更新", Record: &RecordRef{Kind: "album", ID: albumID}}, nil
			}
			addIDs, warnings := filterUnowned(ctx, d, addIDs)
			addRecs := d.mediaSummaryRecords(ctx, addIDs)
			removeRecs := d.mediaSummaryRecords(ctx, removeIDs)
			if len(addRecs) > 6 {
				addRecs = addRecs[:6]
			}
			if len(removeRecs) > 6 {
				removeRecs = removeRecs[:6]
			}
			params := map[string]any{"album_id": albumID}
			if len(addIDs) > 0 {
				params["add_media_ids"] = int64sToAny(addIDs)
			}
			if len(removeIDs) > 0 {
				params["remove_media_ids"] = int64sToAny(removeIDs)
			}
			return proposalResult(&Proposal{
				Intent: "propose_album_members", Summary: fmt.Sprintf("整理相册「%s」", album.Text),
				Preview: ProposalPreview{Kind: "album_members", Body: albumMembersPreviewBody{
					Album: album, AddCount: len(addIDs), RemoveCount: len(removeIDs), Add: addRecs, Remove: removeRecs}},
				Warnings: warnings, Params: params,
			}, "准备整理相册，请确认"), nil
		},
	}
}

// proposeAnniversaryTool 新建纪念日（提案）。
func proposeAnniversaryTool() *Tool {
	return &Tool{
		Name: "propose_anniversary", Category: "纪念日", ProgressLabel: "新建纪念日",
		Description: "提案新建纪念日（需用户确认后生效）。date 统一 YYYY-MM-DD；calendar=solar 公历 / lunar 农历（可 is_leap 闰月）",
		Schema: map[string]string{
			"name":               "string — 名称",
			"date":               "string — YYYY-MM-DD",
			"calendar":           "string — solar | lunar（缺省 solar）",
			"is_leap":            "boolean — 仅 lunar 有效，true 表示闰月",
			"remind_days_before": "number — 提前提醒天数（0-90，缺省 1）",
		},
		Params: map[string]any{"type": "object", "properties": map[string]any{
			"name":               map[string]any{"type": "string"},
			"date":               map[string]any{"type": "string"},
			"calendar":           map[string]any{"type": "string", "enum": []string{"solar", "lunar"}},
			"is_leap":            map[string]any{"type": "boolean"},
			"remind_days_before": map[string]any{"type": "integer"},
		}, "required": []string{"name", "date"}},
		IsWrite:  true,
		Validate: validateAnniversaryArgs,
		Run: func(ctx context.Context, d *Deps, userID int64, args map[string]any, mode ExecMode) (*ToolResult, error) {
			name, date := str(args, "name"), str(args, "date")
			annivType := "solar"
			if str(args, "calendar") == "lunar" {
				annivType = "lunar"
			}
			isLeap := false
			if annivType == "lunar" {
				isLeap = boolArg(args, "is_leap")
			}
			remind := 1
			if _, ok := args["remind_days_before"]; ok {
				remind = int(num(args, "remind_days_before"))
			}
			if remind < 0 || remind > store.MaxRemindDaysBefore {
				remind = 1
			}
			if mode == ModeExecute {
				id, err := d.DB.CreateAnniversary(ctx, name, date, annivType, isLeap, remind, userID)
				if err != nil {
					return nil, err
				}
				return &ToolResult{ModelText: fmt.Sprintf("纪念日「%s」已创建（#%d）。", name, id),
					UserText: fmt.Sprintf("纪念日「%s」已创建", name), Record: &RecordRef{Kind: "anniversary", ID: id}}, nil
			}
			preview := anniversaryPreviewBody{Name: name, Date: date, Calendar: annivType, IsLeap: isLeap, RemindDaysBefore: remind}
			// 下次公历日：用同构造的临时对象换算（不落库）
			tmp := &store.Anniversary{Name: name, Date: date, Type: annivType, IsLeap: isLeap}
			preview.NextSolarDate = tmp.NextSolarDate(time.Now())
			return proposalResult(&Proposal{
				Intent: "propose_anniversary", Summary: "新建纪念日「" + name + "」",
				Preview: ProposalPreview{Kind: "anniversary", Body: preview},
				Params: map[string]any{"name": name, "date": date, "calendar": annivType,
					"is_leap": isLeap, "remind_days_before": remind},
			}, fmt.Sprintf("准备新建纪念日「%s」，请确认", name)), nil
		},
	}
}

// validateAnniversaryArgs 纪念日参数校验（date 形状 + solar 需真实公历日）。
func validateAnniversaryArgs(args map[string]any) error {
	name, date := str(args, "name"), str(args, "date")
	if name == "" || date == "" {
		return fmt.Errorf("name 与 date 必填")
	}
	if str(args, "calendar") == "lunar" {
		if !lunar.ValidDateShape(date) {
			return fmt.Errorf("日期格式应为 YYYY-MM-DD")
		}
		return nil
	}
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return fmt.Errorf("日期格式应为 YYYY-MM-DD")
	}
	return nil
}

// proposeReminderTool 调整纪念日提醒（提案，预览状态变化）。
func proposeReminderTool() *Tool {
	return &Tool{
		Name: "propose_reminder", Category: "纪念日", ProgressLabel: "设置提醒",
		Description: "提案调整纪念日提醒提前天数（需用户确认后生效）",
		Schema: map[string]string{
			"anniversary_id": "number — 纪念日 ID",
			"days_before":    "number — 提前提醒天数（0-90）",
		},
		Params: map[string]any{"type": "object", "properties": map[string]any{
			"anniversary_id": map[string]any{"type": "integer"},
			"days_before":    map[string]any{"type": "integer"},
		}, "required": []string{"anniversary_id", "days_before"}},
		IsWrite: true,
		Validate: func(args map[string]any) error {
			days := int(num(args, "days_before"))
			if days < 0 || days > store.MaxRemindDaysBefore {
				return fmt.Errorf("提前天数应为 0-%d", store.MaxRemindDaysBefore)
			}
			return nil
		},
		Run: func(ctx context.Context, d *Deps, userID int64, args map[string]any, mode ExecMode) (*ToolResult, error) {
			id := num(args, "anniversary_id")
			days := int(num(args, "days_before"))
			if mode == ModeExecute {
				if _, err := d.DB.GetAnniversary(ctx, id); err != nil {
					return nil, fmt.Errorf("纪念日不存在")
				}
				if err := d.DB.UpdateAnniversaryReminder(ctx, id, days); err != nil {
					return nil, err
				}
				return &ToolResult{ModelText: fmt.Sprintf("纪念日提醒已设为提前 %d 天。", days),
					UserText: fmt.Sprintf("提醒已改为提前 %d 天", days), Record: &RecordRef{Kind: "anniversary", ID: id}}, nil
			}
			a, err := d.DB.GetAnniversary(ctx, id)
			if err != nil {
				return nil, fmt.Errorf("纪念日不存在")
			}
			rec := UIRecord{ID: a.ID, Kind: "anniversary", Text: a.Name, Date: a.Date}
			return proposalResult(&Proposal{
				Intent: "propose_reminder", Summary: fmt.Sprintf("调整「%s」的提醒", a.Name),
				Preview: ProposalPreview{Kind: "reminder", Body: reminderPreviewBody{Anniversary: rec, From: a.RemindDaysBefore, To: days}},
				Params:  map[string]any{"anniversary_id": id, "days_before": days},
			}, "准备调整提醒，请确认"), nil
		},
	}
}

// proposePlaceTool 记录地点（坐标与地名二选一，地名走内置离线地理编码）。
func proposePlaceTool() *Tool {
	return &Tool{
		Name: "propose_place", Category: "地图", ProgressLabel: "记录地点",
		Description: "提案记录一个地点（需用户确认后生效）。给 latitude/longitude 或给 place_query 由服务端解析坐标（二选一）",
		Schema: map[string]string{
			"name":        "string — 地点名",
			"latitude":    "number — 纬度（与 place_query 二选一）",
			"longitude":   "number — 经度",
			"place_query": "string — 地名（服务端离线解析坐标；与经纬度二选一）",
			"visit_date":  "string — 到访日期 YYYY-MM-DD（可选）",
			"description": "string — 描述（可选）",
		},
		Params: map[string]any{"type": "object", "properties": map[string]any{
			"name":        map[string]any{"type": "string"},
			"latitude":    map[string]any{"type": "number"},
			"longitude":   map[string]any{"type": "number"},
			"place_query": map[string]any{"type": "string"},
			"visit_date":  map[string]any{"type": "string"},
			"description": map[string]any{"type": "string"},
		}, "required": []string{"name"}},
		IsWrite: true,
		Validate: func(args map[string]any) error {
			_, hasLat := args["latitude"]
			_, hasLng := args["longitude"]
			if (!hasLat || !hasLng) && str(args, "place_query") == "" {
				return fmt.Errorf("latitude/longitude 与 place_query 至少给一组")
			}
			if v := str(args, "visit_date"); v != "" {
				if _, err := time.Parse("2006-01-02", v); err != nil {
					return fmt.Errorf("visit_date 格式应为 YYYY-MM-DD")
				}
			}
			return nil
		},
		Run: func(ctx context.Context, d *Deps, userID int64, args map[string]any, mode ExecMode) (*ToolResult, error) {
			name := str(args, "name")
			lat, lng := flt(args, "latitude"), flt(args, "longitude")
			resolved := false
			if _, hasLat := args["latitude"]; !hasLat || str(args, "place_query") != "" {
				if p, ok := resolvePlace(str(args, "place_query")); ok {
					lat, lng, resolved = p.Lat, p.Lng, true
					if name == "" {
						name = p.Display
					}
				} else if str(args, "place_query") != "" {
					return nil, fmt.Errorf("内置地名库中没有「%s」，请改用经纬度或更常见的地名", str(args, "place_query"))
				}
			} else {
				resolved = true
			}
			visitDate, desc := str(args, "visit_date"), str(args, "description")
			if mode == ModeExecute {
				p, created, err := d.DB.FindOrCreatePoint(ctx, store.PointInput{
					Name: name, Lat: lat, Lng: lng, VisitDate: visitDate, Description: desc, Source: "ai", UserID: userID,
				})
				if err != nil {
					return nil, err
				}
				content := fmt.Sprintf("已把「%s」记到地图上（#%d）。", name, p.ID)
				if !created {
					content = fmt.Sprintf("「%s」已在地图上（#%d），复用了既有点位。", name, p.ID)
				}
				if created {
					operator := d.aiOperatorName(ctx, userID)
					snapshot, _ := json.Marshal(map[string]any{"name": name, "latitude": lat, "longitude": lng, "operator": operator, "via": "ai"})
					d.notifyPartners(ctx, userID, "map_point_created", "map_point", p.ID, string(snapshot),
						"足迹更新", operator+"（AI）把「"+name+"」记到了地图上")
				}
				return &ToolResult{ModelText: content, UserText: fmt.Sprintf("已记录地点「%s」", name),
					Record: &RecordRef{Kind: "map_point", ID: p.ID}}, nil
			}
			return proposalResult(&Proposal{
				Intent: "propose_place", Summary: "记录地点「" + name + "」",
				Preview: ProposalPreview{Kind: "place", Body: placePreviewBody{
					Name: name, Lat: lat, Lng: lng, VisitDate: visitDate, Description: desc, Resolved: resolved}},
				Params: map[string]any{"name": name, "latitude": lat, "longitude": lng,
					"visit_date": visitDate, "description": desc},
			}, fmt.Sprintf("准备记录地点「%s」，请确认", name)), nil
		},
	}
}

// recentMonthCounts 近 12 个月动态分布（只进 ModelText，不上概览卡）。
// 返回形如「2026-09 3 条、2026-08 5 条」；无数据返回空串。
// 窗口跨年，故按涉及的年份各查一次：TimelinePostCounts 按「年份相等」过滤，
// 只查当年时去年那几个月的行不会被查出来（行本身在库里，是查询把它们挡在外面）。
func recentMonthCounts(ctx context.Context, d *Deps, now time.Time) string {
	// 起点落在本月 1 号再回退 11 个月：直接从 29~31 号回退会被 AddDate 归一化成下个月（窗口少一个月）
	cutoff := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).AddDate(0, -11, 0).Format("2006-01")
	years := []string{now.Format("2006")}
	if prev := cutoff[:4]; prev != years[0] {
		years = []string{prev, years[0]}
	}
	var segs []string
	for _, y := range years {
		byMonth, err := d.DB.TimelinePostCounts(ctx, y, "")
		if err != nil {
			return ""
		}
		for _, pc := range byMonth {
			if pc.Period >= cutoff && pc.Period <= now.Format("2006-01") && pc.Count > 0 {
				segs = append(segs, fmt.Sprintf("%s %d 条", pc.Period, pc.Count))
			}
		}
	}
	return strings.Join(segs, "、")
}

// getSpaceStatsTool 空间聚合概览：无参一次全给，实时聚合不落库（AI模式设计「统计与计数口径」）。
// 不落结果集句柄、Selectable=false：统计不指向具体记录，不可被勾选引用；
// 模型要展开某一维度，自己调对应 find_*。
func getSpaceStatsTool() *Tool {
	return &Tool{
		Name:          "get_space_stats",
		Category:      "空间",
		ProgressLabel: "统计空间数据",
		Description:   "空间聚合概览：动态/媒体/相册/足迹/纪念日/互动总量与分布，一次全给。数字直出实时聚合，不是模型转述",
		Schema:        map[string]string{},
		Params:        map[string]any{"type": "object", "properties": map[string]any{}},
		IsWrite:       false,
		Run: func(ctx context.Context, d *Deps, userID int64, args map[string]any, mode ExecMode) (*ToolResult, error) {
			var postTotal, thisYear, thisMonth int64
			now := time.Now()
			year, month := now.Format("2006"), now.Format("2006-01")
			if err := d.DB.QueryRowContext(ctx,
				`SELECT COUNT(*),
					COALESCE(SUM(CASE WHEN strftime('%Y', created_at) = ? THEN 1 ELSE 0 END), 0),
					COALESCE(SUM(CASE WHEN strftime('%Y-%m', created_at) = ? THEN 1 ELSE 0 END), 0)
				 FROM posts WHERE deleted_at IS NULL`, year, month).Scan(&postTotal, &thisYear, &thisMonth); err != nil {
				return nil, err
			}
			byYear, err := d.DB.TimelinePostCounts(ctx, "", "")
			if err != nil {
				return nil, err
			}
			var mediaTotal, photoN, videoN, audioN, unfiled int64
			if err := d.DB.QueryRowContext(ctx, `SELECT COUNT(*),
					COUNT(CASE WHEN m.kind = 'photo' THEN 1 END),
					COUNT(CASE WHEN m.kind = 'video' THEN 1 END),
					COUNT(CASE WHEN m.kind = 'audio' THEN 1 END)
				 FROM media m JOIN posts p ON p.id = m.post_id
				 WHERE m.deleted_at IS NULL AND p.deleted_at IS NULL`).Scan(&mediaTotal, &photoN, &videoN, &audioN); err != nil {
				return nil, err
			}
			if err := d.DB.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM media m JOIN posts p ON p.id = m.post_id
				 WHERE m.deleted_at IS NULL AND p.deleted_at IS NULL
				   AND NOT EXISTS (SELECT 1 FROM album_items i JOIN albums a ON a.id = i.album_id
				     WHERE i.media_id = m.id AND a.deleted_at IS NULL)`).Scan(&unfiled); err != nil {
				return nil, err
			}
			var albumCount int64
			if err := d.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM albums WHERE deleted_at IS NULL`).Scan(&albumCount); err != nil {
				return nil, err
			}
			var archived int64
			if err := d.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM album_items i JOIN media m ON m.id = i.media_id
				 JOIN albums a ON a.id = i.album_id WHERE m.deleted_at IS NULL AND a.deleted_at IS NULL`).Scan(&archived); err != nil {
				return nil, err
			}
			var pointTotal int64
			if err := d.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM map_points WHERE deleted_at IS NULL`).Scan(&pointTotal); err != nil {
				return nil, err
			}
			cities, err := d.DB.PointCities(ctx)
			if err != nil {
				return nil, err
			}
			// 纪念日：总数与「下一个」共用一次列表读取，避免同卡两次读同一张表
			annivList, err := d.DB.ListAnniversaries(ctx)
			if err != nil {
				return nil, err
			}
			// 互动三计数只算「所属动态未软删」的行：likes/favorites 自身无 deleted_at，
			// 必须 join posts 判；comments 的 deleted_at 只管评论自身，同样要 join。
			// 与上面 postTotal 的 deleted_at IS NULL 口径对齐，避免同卡「动态 3 条 / 互动 3 次」自相矛盾，
			// 也满足 AI模式设计「安全层」的已删除内容过滤（回收站数据对 AI 不可见）。
			var likeN, favN, cmtN int64
			if err := d.DB.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM likes l JOIN posts p ON p.id = l.post_id WHERE p.deleted_at IS NULL`).Scan(&likeN); err != nil {
				return nil, err
			}
			if err := d.DB.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM favorites f JOIN posts p ON p.id = f.post_id WHERE p.deleted_at IS NULL`).Scan(&favN); err != nil {
				return nil, err
			}
			if err := d.DB.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM comments c JOIN posts p ON p.id = c.post_id
				 WHERE c.deleted_at IS NULL AND p.deleted_at IS NULL`).Scan(&cmtN); err != nil {
				return nil, err
			}

			// 概览卡维度：在一起天数（PRODUCT「卡片体系」数字网格首项；未设恋爱开始日则整项省略，
			// 不给「0 天」）、媒体分类计数、纪念日「下一个」（AI模式设计「统计与计数口径」维度行）。
			var stats []UIStat
			if sp, err := d.DB.GetSpace(ctx); err == nil {
				if days, ok := store.DaysTogether(sp.LoveStartDate.String, now); ok {
					stats = append(stats, UIStat{Label: "在一起", Value: fmt.Sprintf("%d 天", days), Hint: "自 " + sp.LoveStartDate.String})
				}
			}
			annivHint := ""
			if a, ok := store.PickNextAnniversary(annivList, now); ok {
				annivHint = fmt.Sprintf("%s · 还有 %d 天", a.Name, a.DaysUntil(now))
			}
			stats = append(stats,
				UIStat{Label: "动态", Value: fmt.Sprintf("%d 条", postTotal), Hint: fmt.Sprintf("今年 %d · 本月 %d", thisYear, thisMonth)},
				UIStat{Label: "媒体", Value: fmt.Sprintf("%d 个", mediaTotal),
					Hint: fmt.Sprintf("照片 %d · 视频 %d · 录音 %d · 未归档 %d", photoN, videoN, audioN, unfiled)},
				UIStat{Label: "相册", Value: fmt.Sprintf("%d 册", albumCount), Hint: fmt.Sprintf("已归档 %d 张", archived)},
				UIStat{Label: "足迹", Value: fmt.Sprintf("%d 点", pointTotal), Hint: fmt.Sprintf("%d 城", len(cities))},
				UIStat{Label: "纪念日", Value: fmt.Sprintf("%d 个", len(annivList)), Hint: annivHint},
				UIStat{Label: "互动", Value: fmt.Sprintf("%d 次", likeN+favN+cmtN), Hint: fmt.Sprintf("赞 %d · 藏 %d · 评 %d", likeN, favN, cmtN)},
			)

			var series []UISeries
			yearPoints := make([]UISeriesPoint, 0, len(byYear))
			for _, pc := range byYear {
				if len(pc.Period) == 4 {
					yearPoints = append(yearPoints, UISeriesPoint{Label: pc.Period, Count: int(pc.Count)})
				}
			}
			if len(yearPoints) > 5 { // 按年窗口固定近 5 年
				yearPoints = yearPoints[len(yearPoints)-5:]
			}
			series = append(series, UISeries{Title: "按年", Points: yearPoints})
			cityPoints := make([]UISeriesPoint, 0, len(cities))
			for _, c := range cities {
				if len(cityPoints) >= 10 {
					break
				}
				cityPoints = append(cityPoints, UISeriesPoint{Label: c.City, Count: int(c.Count)})
			}
			series = append(series, UISeries{Title: "城市 Top", Points: cityPoints})

			// 分布摘要（一行一条，label + text）。数据源与 series 相同（byYear 4 位年份全集、cities Top 10）；
			// 0 条时不下发该条。
			var summaries []UISummary
			fullYears := make([]UISeriesPoint, 0, len(byYear))
			for _, pc := range byYear {
				if len(pc.Period) == 4 {
					fullYears = append(fullYears, UISeriesPoint{Label: pc.Period, Count: int(pc.Count)})
				}
			}
			if len(fullYears) > 0 {
				total := 0
				for _, p := range fullYears {
					total += p.Count
				}
				first := fullYears[0].Label
				last := fullYears[len(fullYears)-1].Label
				var text string
				if len(fullYears) == 1 {
					text = fmt.Sprintf("%s · 共 %d 条", first, total)
				} else {
					text = fmt.Sprintf("%s–%s · %d 年共 %d 条", first, last, len(fullYears), total)
				}
				summaries = append(summaries, UISummary{Label: "按年", Text: text})
			}
			if len(cities) > 0 {
				top := cities[0]
				var text string
				if len(cities) == 1 {
					text = fmt.Sprintf("1 城 · %s（%d）", top.City, top.Count)
				} else {
					text = fmt.Sprintf("走过 %d 城 · %s最多（%d）", len(cities), top.City, top.Count)
				}
				summaries = append(summaries, UISummary{Label: "城市", Text: text})
			}

			ui := &UIBlock{Kind: "stats", Title: "我们的空间", Stats: stats, Series: series, Summaries: summaries}
			// ModelText 只给增量数字（常驻事实四项不重复）；近 12 个月分布只进这里
			// （卡上 series 只放按年 + 城市 Top 10，三组列表全上卡会顶满对话流）。
			mt := fmt.Sprintf("空间概览：动态 %d 条（今年 %d，本月 %d）；媒体 %d 个（照片 %d / 视频 %d / 录音 %d，未归档 %d）；手动相册 %d 册（已归档 %d 张）；"+
				"足迹 %d 点、%d 城；纪念日 %d 个；互动 %d 次（赞 %d / 藏 %d / 评 %d）。",
				postTotal, thisYear, thisMonth, mediaTotal, photoN, videoN, audioN, unfiled, albumCount, archived,
				pointTotal, len(cities), len(annivList), likeN+favN+cmtN, likeN, favN, cmtN)
			if recent := recentMonthCounts(ctx, d, now); recent != "" {
				mt += "近 12 个月动态分布：" + recent + "。"
			}
			return &ToolResult{ModelText: mt, UserText: "统计完成", UI: ui}, nil
		},
	}
}

// proposeReactionTool 点赞/收藏（幂等「设为」语义，非 toggle）。
func proposeReactionTool() *Tool {
	return &Tool{
		Name: "propose_reaction", Category: "互动", ProgressLabel: "点赞 / 收藏",
		Description: "提案点赞/收藏动态（需用户确认后生效）。语义是「设为」不是切换：" +
			"like=true 表示要点赞，false 表示要取消；状态本来就一致时不会出卡片",
		Schema: map[string]string{
			"post_id":  "number — 动态 ID",
			"like":     "boolean — true=点赞，false=取消点赞（可选）",
			"favorite": "boolean — true=收藏，false=取消收藏（可选）",
		},
		Params: map[string]any{"type": "object", "properties": map[string]any{
			"post_id":  map[string]any{"type": "integer"},
			"like":     map[string]any{"type": "boolean"},
			"favorite": map[string]any{"type": "boolean"},
		}, "required": []string{"post_id"}},
		IsWrite: true,
		Validate: func(args map[string]any) error {
			_, hasLike := optBool(args, "like")
			_, hasFav := optBool(args, "favorite")
			if !hasLike && !hasFav {
				return fmt.Errorf("like 与 favorite 至少给一项（true=设置，false=取消）")
			}
			return nil
		},
		Run: func(ctx context.Context, d *Deps, userID int64, args map[string]any, mode ExecMode) (*ToolResult, error) {
			postID := num(args, "post_id")
			wantLike, hasLike := optBool(args, "like")
			wantFav, hasFav := optBool(args, "favorite")
			// 点选优先：勾选存在时点赞/收藏只能作用于勾选的那条动态
			if err := checkQuotedSubset(ctx, "post", []int64{postID}); err != nil {
				return nil, err
			}
			post, ok := d.postSummaryRecord(ctx, postID)
			if !ok {
				return nil, fmt.Errorf("动态不存在")
			}
			if mode == ModeExecute {
				var changes []string
				if hasLike {
					changed, err := d.DB.SetLike(ctx, postID, userID, wantLike)
					if err != nil {
						return nil, err
					}
					if changed {
						if wantLike {
							changes = append(changes, "已点赞")
							// 通知另一方（PRODUCT「通知中心」：任何一方动作都通知另一方；
							// ARCHITECTURE「通知分发器」事件源含点赞）。与 REST 同口径：
							// 只在状态真的变为「已赞」时发，取消赞不发。
							op := d.aiOperatorName(ctx, userID)
							snapshot, _ := json.Marshal(map[string]any{"operator": op, "via": "ai"})
							d.notifyPartners(ctx, userID, "post_liked", "post", postID, string(snapshot),
								"收到点赞", op+"（AI）赞了你的动态")
						} else {
							changes = append(changes, "已取消点赞")
						}
					}
				}
				if hasFav {
					changed, err := d.DB.SetFavorite(ctx, postID, userID, wantFav)
					if err != nil {
						return nil, err
					}
					if changed {
						if wantFav {
							changes = append(changes, "已收藏")
							// 同点赞：收藏成立才通知另一方，取消收藏不发。
							op := d.aiOperatorName(ctx, userID)
							snapshot, _ := json.Marshal(map[string]any{"operator": op, "via": "ai"})
							d.notifyPartners(ctx, userID, "post_favorited", "post", postID, string(snapshot),
								"收到收藏", op+"（AI）收藏了你的动态")
						} else {
							changes = append(changes, "已取消收藏")
						}
					}
				}
				if len(changes) == 0 {
					return &ToolResult{ModelText: "互动状态已符合要求，无需改动。", UserText: "状态无需改动"}, nil
				}
				return &ToolResult{ModelText: strings.Join(changes, "、") + "。",
					UserText: strings.Join(changes, "、"), Record: &RecordRef{Kind: "post", ID: postID}}, nil
			}
			// Propose：读当前状态，与目标一致则不出卡（幂等减少无效确认）
			inter, err := d.DB.GetInteractions(ctx, []int64{postID}, userID)
			if err != nil {
				return nil, err
			}
			cur := inter[postID]
			body := reactionPreviewBody{Post: post}
			var summaryParts []string
			if hasLike && cur != nil && cur.Liked != wantLike {
				body.Like = &fromTo{From: cur.Liked, To: wantLike}
				if wantLike {
					summaryParts = append(summaryParts, "点赞")
				} else {
					summaryParts = append(summaryParts, "取消点赞")
				}
			}
			if hasFav && cur != nil && cur.Favorited != wantFav {
				body.Favorite = &fromTo{From: cur.Favorited, To: wantFav}
				if wantFav {
					summaryParts = append(summaryParts, "收藏")
				} else {
					summaryParts = append(summaryParts, "取消收藏")
				}
			}
			if body.Like == nil && body.Favorite == nil {
				return &ToolResult{ModelText: "该动态的互动状态已经是你要的样子，无需变更。", UserText: "状态无需改动"}, nil
			}
			params := map[string]any{"post_id": postID}
			if hasLike {
				params["like"] = wantLike
			}
			if hasFav {
				params["favorite"] = wantFav
			}
			return proposalResult(&Proposal{
				Intent: "propose_reaction", Summary: strings.Join(summaryParts, " + "),
				Preview: ProposalPreview{Kind: "reaction", Body: body}, Params: params,
			}, "准备互动，请确认"), nil
		},
	}
}

// proposeDeleteTool 移入回收站（软删除，AppOnly：外部通道任何档位不开放）。
func proposeDeleteTool() *Tool {
	return &Tool{
		Name: "propose_delete", Category: "动态", ProgressLabel: "删除内容",
		Description: "提案删除内容（移入回收站，可在后台恢复；需用户确认后生效）。" +
			"kind: post / comment / album / anniversary / place；单条传 id，批量传 ids[] 或 source 三者互斥，批量一张卡一次确认",
		Schema: map[string]string{
			"kind":   "string — post / comment / album / anniversary / place",
			"id":     "number — 单条删除：记录 ID",
			"ids":    "array — 批量删除：记录 ID 数组（与 id / source 三选一）",
			"source": "object — 批量删除：结果集句柄 {result_id, group?}（与 id / ids 三选一）",
		},
		Params: map[string]any{"type": "object", "properties": map[string]any{
			"kind": map[string]any{"type": "string", "enum": []string{"post", "comment", "album", "anniversary", "place"}},
			"id":   map[string]any{"type": "integer"},
			"ids":  map[string]any{"type": "array", "items": map[string]any{"type": "integer"}},
			"source": map[string]any{"type": "object", "properties": map[string]any{
				"result_id": map[string]any{"type": "string"}, "group": map[string]any{"type": "string"},
			}},
		}, "required": []string{"kind"}},
		IsWrite: true,
		AppOnly: true,
		Validate: func(args map[string]any) error {
			switch str(args, "kind") {
			case "post", "comment", "album", "anniversary", "place":
				return nil
			}
			return fmt.Errorf("kind 仅支持 post / comment / album / anniversary / place")
		},
		Run: func(ctx context.Context, d *Deps, userID int64, args map[string]any, mode ExecMode) (*ToolResult, error) {
			kind := str(args, "kind")
			hasID := num(args, "id") > 0
			rawIDs, hasIDs := args["ids"].([]any)
			src := parseSource(args)
			given := 0
			for _, v := range []bool{hasID, hasIDs, src != nil} {
				if v {
					given++
				}
			}
			if given != 1 {
				return nil, fmt.Errorf("id / ids / source 三者必须恰好提供一个")
			}
			var ids []int64
			if hasID {
				ids = []int64{num(args, "id")}
			} else if hasIDs {
				for _, v := range rawIDs {
					if f, ok := v.(float64); ok && f > 0 {
						ids = append(ids, int64(f))
					}
				}
				seen := map[int64]bool{}
				dedup := ids[:0]
				for _, id := range ids {
					if !seen[id] {
						seen[id] = true
						dedup = append(dedup, id)
					}
				}
				ids = dedup
				if len(ids) == 0 {
					return nil, fmt.Errorf("ids 不能为空")
				}
				if len(ids) > deleteBatchMax {
					return nil, fmt.Errorf("一次最多删除 %d 条，请分批操作", deleteBatchMax)
				}
			} else {
				var err error
				ids, err = expandSource(ctx, d, userID, sessionIDFromCtx(ctx), src, deleteResultSetKind(kind))
				if err != nil {
					return nil, err
				}
				if len(ids) == 0 {
					return nil, fmt.Errorf("结果集为空，无可删除条目")
				}
				if len(ids) > deleteBatchMax {
					return nil, fmt.Errorf("一次最多删除 %d 条（当前 %d 条），请分批操作", deleteBatchMax, len(ids))
				}
			}

			// 点选优先：直给 id 也必须落在勾选并集内（勾选存在时）——与 source 路径同一道护栏，
			// 否则模型绕开句柄直接列 id 即可越界。delete kind 与结果集 kind 同名（见
			// deleteResultSetKind），故直接用 kind 比对。
			if err := checkQuotedSubset(ctx, deleteResultSetKind(kind), ids); err != nil {
				return nil, err
			}

			// 校验前置：全部条目必须存在（不存在即拒绝，不出注定失败的卡）
			recs := make([]UIRecord, 0, len(ids))
			for _, id := range ids {
				var rec UIRecord
				var exists bool
				switch kind {
				case "post":
					rec, exists = d.postSummaryRecord(ctx, id)
				case "comment":
					rec, exists = d.commentSummaryRecord(ctx, id)
				case "album":
					rec, exists = d.albumSummaryRecord(ctx, id)
				case "anniversary":
					rec, exists = d.anniversarySummaryRecord(ctx, id)
				case "place":
					rec, exists = d.placeSummaryRecord(ctx, id)
				}
				if !exists {
					return nil, fmt.Errorf("要删除的%s #%d 不存在", deleteKindLabel(kind), id)
				}
				recs = append(recs, rec)
			}

			if mode == ModeExecute {
				// 站内通知要删除前的快照（Get* 一律只取未软删记录），故先取快照再整批事务删除
				notices := make([]store.DeletedNotice, 0, len(ids))
				for _, id := range ids {
					if n, ok := d.deletedNotice(ctx, userID, kind, id); ok {
						notices = append(notices, n)
					}
				}
				// 一个事务内逐条软删：任一条失败整批回滚，不留半删状态
				if err := d.DB.SoftDeleteBatch(ctx, kind, ids, store.ActorTypeAI, userID); err != nil {
					return nil, fmt.Errorf("删除%s失败：%w", deleteKindLabel(kind), err)
				}
				for _, n := range notices {
					// 这里的删除全部是 AI 代操作（REST 删除走各 api 自己的 store 调用，不经本函数），
					// 故快照统一带 via:"ai"——接收端据此判别来源（ARCHITECTURE「通知分发器」快照字段读法）
					n.ViaAI = true
					_ = d.DB.NotifyContentDeleted(ctx, n)
				}
				return &ToolResult{ModelText: fmt.Sprintf("%d 条%s已删除（进回收站）。", len(ids), deleteKindLabel(kind)),
					UserText: fmt.Sprintf("%d 条%s已移入回收站", len(ids), deleteKindLabel(kind))}, nil
			}

			var warnings []string
			if len(ids) > 1 {
				warnings = append(warnings, fmt.Sprintf("将删除 %d 条%s，移入回收站，可在后台恢复", len(ids), deleteKindLabel(kind)))
				if kind == "post" {
					warnings = append(warnings, "动态的媒体会一并删除")
				}
			}
			summary := "删除" + deleteKindLabel(kind)
			if len(ids) > 1 {
				summary = fmt.Sprintf("删除 %d 条%s", len(ids), deleteKindLabel(kind))
			}
			body := deletePreviewBody{Kind: kind, Count: len(ids)}
			if len(ids) == 1 {
				body.Text, body.Date, body.Thumb = recs[0].Text, recs[0].Date, recs[0].ThumbURL
			} else {
				items := make([]deletePreviewItem, 0, len(recs))
				for _, r := range recs {
					items = append(items, deletePreviewItem{ID: r.ID, Text: r.Text, Date: r.Date, Thumb: r.ThumbURL})
				}
				body.Items = items
			}
			params := map[string]any{"kind": kind, "ids": int64sToAny(ids)}
			if len(ids) == 1 {
				params = map[string]any{"kind": kind, "id": ids[0]}
			}
			return proposalResult(&Proposal{
				Intent: "propose_delete", Summary: summary,
				Preview: ProposalPreview{Kind: "delete", Body: body}, Warnings: warnings, Params: params,
			}, "准备删除，请确认"), nil
		},
	}
}

// deleteBatchMax 批量删除上限（PRODUCT「引用与操作范围」：单次批量上限 50 条）。
const deleteBatchMax = 50

// deleteResultSetKind 删除类工具期望的结果集类型（kind → 集合 kind）。
func deleteResultSetKind(kind string) string {
	return kind // post/comment/album/anniversary/place 集合 kind 与删除 kind 同名
}

func deleteKindLabel(kind string) string {
	switch kind {
	case "post":
		return "动态"
	case "comment":
		return "评论"
	case "album":
		return "相册"
	case "anniversary":
		return "纪念日"
	case "place":
		return "地点"
	}
	return kind
}

// deletedNotice 取「删除后要发给对方」的站内通知（口径对齐 REST：删对方内容 → 通知作者/创建者）。
// 读的是删除前的记录（Get* 只返回未软删行），故须在软删之前调用；读不到即无通知，不阻断删除。
func (d *Deps) deletedNotice(ctx context.Context, userID int64, kind string, id int64) (store.DeletedNotice, bool) {
	switch kind {
	case "post":
		p, err := d.DB.GetPost(ctx, id)
		if err != nil {
			return store.DeletedNotice{}, false
		}
		return store.DeletedNotice{
			CreatorID: p.AuthorID, DeleterID: userID, DeleterName: d.aiOperatorName(ctx, userID),
			EventType: "post_deleted", TargetType: "post", TargetID: id,
			Extra: map[string]any{"content": textutil.Trunc(p.Content, 50)},
		}, true
	case "comment":
		c, err := d.DB.GetComment(ctx, id)
		if err != nil {
			return store.DeletedNotice{}, false
		}
		return store.DeletedNotice{
			CreatorID: c.AuthorID, DeleterID: userID, DeleterName: d.aiOperatorName(ctx, userID),
			EventType: "comment_deleted", TargetType: "comment", TargetID: id,
			Extra: map[string]any{"content": textutil.Trunc(c.Content, 50)},
		}, true
	case "album":
		al, err := d.DB.GetAlbum(ctx, id)
		if err != nil || !al.CreatedBy.Valid {
			return store.DeletedNotice{}, false
		}
		return store.DeletedNotice{
			CreatorID: al.CreatedBy.Int64, DeleterID: userID, DeleterName: d.aiOperatorName(ctx, userID),
			EventType: "album_deleted", TargetType: "album", TargetID: id,
			Extra: map[string]any{"name": al.Name},
		}, true
	case "anniversary":
		a, err := d.DB.GetAnniversary(ctx, id)
		if err != nil {
			return store.DeletedNotice{}, false
		}
		return store.DeletedNotice{
			CreatorID: a.CreatedBy, DeleterID: userID, DeleterName: d.aiOperatorName(ctx, userID),
			EventType: "anniversary_deleted", TargetType: "anniversary", TargetID: id,
			Extra: map[string]any{"name": a.Name},
		}, true
	case "place":
		p, err := d.DB.GetPoint(ctx, id)
		if err != nil {
			return store.DeletedNotice{}, false
		}
		return store.DeletedNotice{
			CreatorID: p.CreatedBy, DeleterID: userID, DeleterName: d.aiOperatorName(ctx, userID),
			EventType: "map_point_deleted", TargetType: "map_point", TargetID: id,
			Extra: map[string]any{"name": p.Name},
		}, true
	}
	return store.DeletedNotice{}, false
}

// expandMediaArgs 展开 propose_album 的 source / media_ids（二选一，展开后自包含）。
func expandMediaArgs(ctx context.Context, d *Deps, userID int64, args map[string]any) ([]int64, error) {
	if src := parseSource(args); src != nil {
		ids, err := expandSource(ctx, d, userID, sessionIDFromCtx(ctx), src, "media")
		if err != nil {
			return nil, err
		}
		return ids, nil
	}
	ids, err := ints(args, "media_ids")
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	if err := checkQuotedSubset(ctx, "media", ids); err != nil {
		return nil, err
	}
	return ids, nil
}

// expandNamedMediaArgs 展开 add_/remove_ 前缀的 source / ids（相册增删成员）。
func expandNamedMediaArgs(ctx context.Context, d *Deps, userID int64, args map[string]any, prefix string) ([]int64, error) {
	sub := map[string]any{}
	if v, ok := args[prefix+"_source"]; ok {
		sub["source"] = v
	}
	if v, ok := args[prefix+"_media_ids"]; ok {
		sub[prefix+"_media_ids"] = v
	}
	if src := parseSource(sub); src != nil {
		ids, err := expandSource(ctx, d, userID, sessionIDFromCtx(ctx), src, "media")
		if err != nil {
			return nil, err
		}
		return ids, nil
	}
	ids, err := ints(args, prefix+"_media_ids")
	if err != nil {
		return nil, err
	}
	if err := checkQuotedSubset(ctx, "media", ids); err != nil {
		return nil, err
	}
	return ids, nil
}

// filterUnowned 加入相册前的归属过滤（一文件一相册，UNIQUE(media_id)）：
// 已归属其它相册的媒体从执行参数里剔除并写进 Warnings——预览说「将被跳过」，
// 执行就真的跳过（否则 params 里留着必然撞 409 的 id，执行期才炸）。
func filterUnowned(ctx context.Context, d *Deps, mediaIDs []int64) ([]int64, []string) {
	if len(mediaIDs) == 0 {
		return mediaIDs, nil
	}
	owned, err := d.DB.AlbumOwnership(ctx, mediaIDs)
	if err != nil || len(owned) == 0 {
		return mediaIDs, nil
	}
	byAlbum := map[string]int{}
	keep := make([]int64, 0, len(mediaIDs))
	for _, id := range mediaIDs {
		if ref, ok := owned[id]; ok {
			byAlbum[ref.Name]++
			continue
		}
		keep = append(keep, id)
	}
	var out []string
	for name, n := range byAlbum {
		out = append(out, fmt.Sprintf("这 %d 张已属于相册《%s》，将被跳过", n, name))
	}
	sort.Strings(out)
	return keep, out
}

func int64sToAny(ids []int64) []any {
	out := make([]any, 0, len(ids))
	for _, id := range ids {
		out = append(out, float64(id))
	}
	return out
}

// RegistryForPAT 外部 PAT 通道可见的工具子集：过滤 AppOnly（删除类）工具。
func RegistryForPAT() []*Tool {
	var list []*Tool
	for _, t := range Registry() {
		if !t.AppOnly {
			list = append(list, t)
		}
	}
	return list
}
