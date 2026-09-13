// Package tools MCP 工具注册表（DESIGN 第 6 章 26 个全量注册；§8.3 分档：
// IsWrite=写工具，客户端确认卡片后生效（15 分钟 TTL）；AppOnly=删除类，不对 PAT 开放；
// 读工具立即执行。所有调用均记审计，actor=发起人）。
package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/bookandmusic/love-life/server/internal/lunar"
	"github.com/bookandmusic/love-life/server/internal/media"
	"github.com/bookandmusic/love-life/server/internal/notify"
	"github.com/bookandmusic/love-life/server/internal/store"
	"github.com/bookandmusic/love-life/server/internal/textutil"
)

type ExecResult struct {
	Content string // 回填给模型的结果文本
	Data    any    // 附带给客户端的结构化数据（如确认卡片详情）
}

type Tool struct {
	Name        string
	Description string
	// Category 后台工具表分组（AD4；契约 routes.mjs /admin/mcp-tools 的 category 字段）。
	// 取值：动态 / 相册 / 地图 / 纪念日 / 空间 / 互动。
	Category string
	// Schema 参数名册（后台工具详情展示；契约 /admin/mcp-tools 的 schema 字段，
	// 值形如 "string — 动态正文"）。与 Params 并存：Params 是给模型的 JSON Schema，
	// Schema 是后台人工可读的名册。
	Schema  map[string]string
	Params  map[string]any
	IsWrite bool
	// AppOnly 标记删除类工具：铁律「删除类工具不对任何令牌开放」（AGENTS §5 / DESIGN §8.3），
	// 仅 APP 内部通道可用；外部 PAT 通道列工具表必须经 RegistryForPAT 过滤。
	AppOnly bool
	Exec    func(ctx context.Context, deps *Deps, userID int64, args map[string]any) (*ExecResult, error)
}

type Deps struct {
	DB *store.DB
	MP *media.Pipeline
	DP *notify.Dispatcher
}

func (d *Deps) audit(ctx context.Context, userID int64, tool, digest, result string) {
	_ = d.DB.InsertAuditLog(ctx, store.ActorTypeAI, userID, tool, digest, result)
}

// AuditWithLatency 工具审计带 latency 版本（L5 AI 调用统计需求）。
func (d *Deps) AuditWithLatency(ctx context.Context, userID int64, tool, digest, result string, latencyMs int64) {
	_ = d.DB.InsertAuditLogFull(ctx, store.ActorTypeAI, userID, tool, digest, "", result, &latencyMs)
}

// aiOperatorLabel AI 操作者名称查不到时的回退标签（正常路径用真实用户名，REQ §8.1 外部推送需含操作者）。
const aiOperatorLabel = "AI助手"

// notifyPartners 通用通知分发辅助（内部工具通知另一方+外部推送）。
// snapshot 由调用方序列化（忽略失败按空串处理）；通知失败记 warning 但不阻断主流程（业务写入已完成）。
func (d *Deps) notifyPartners(ctx context.Context, userID int64, eventType, entityType string, entityID int64, snapshot, title, body string) {
	partnerIDs, err := d.DB.ListPartnerIDs(ctx, userID)
	if err != nil {
		slog.Warn("list partner ids failed", "err", err, "user_id", userID, "event", eventType)
		return
	}
	d.DP.Dispatch(ctx, partnerIDs, eventType, entityType, entityID, snapshot, title, body)
}

// aiOperatorName AI 代操作的通知署名：取真实用户名（与 REST/create_comment 同口径）。
// 查名失败不阻断主流程——回退 aiOperatorLabel 并记 warning。
func (d *Deps) aiOperatorName(ctx context.Context, userID int64) string {
	u, err := d.DB.GetUserByID(ctx, userID)
	if err != nil || u.Username == "" {
		slog.Warn("resolve ai operator name failed", "err", err, "user_id", userID)
		return aiOperatorLabel
	}
	return u.Username
}

func str(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

func num(args map[string]any, key string) int64 {
	if v, ok := args[key].(float64); ok {
		return int64(v)
	}
	return 0
}

func flt(args map[string]any, key string) float64 {
	if v, ok := args[key].(float64); ok {
		return v
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

// postHit 动态查询类工具（search_posts / get_today_in_history / get_location_posts /
// list_favorites）的统一输出行；location_name 仅当动态带地点时出现。
type postHit struct {
	ID           int64  `json:"id"`
	Title        string `json:"title,omitempty"`
	Content      string `json:"content"`
	Date         string `json:"date"`
	Author       string `json:"author"`
	LocationName string `json:"location_name,omitempty"`
}

// postHitColumns 与 scanPostHits 配套的 SELECT 列清单（调用方需 JOIN users）。
const postHitColumns = `p.id, COALESCE(p.title,''), p.content, p.created_at,
		COALESCE(u.nickname, u.username), COALESCE(p.location_name,'')`

// scanPostHits 扫描按 postHitColumns 查询的动态行。
func scanPostHits(rows *sql.Rows) ([]postHit, error) {
	var hits []postHit
	for rows.Next() {
		var h postHit
		if err := rows.Scan(&h.ID, &h.Title, &h.Content, &h.Date, &h.Author, &h.LocationName); err != nil {
			return nil, err
		}
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

// pageArgs 分页参数归一（list_albums / list_favorites / list_comments 共用）：
// page 缺省 1，size 缺省 20、上限 50（对齐 D5 分页夹紧口径）。
func pageArgs(args map[string]any) (page, size int) {
	page, size = int(num(args, "page")), int(num(args, "page_size"))
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 50 {
		size = 50
	}
	return page, size
}

// pagedOut 分页输出统一包裹（list_albums / list_favorites / list_comments 共用）。
func pagedOut(items any, total, page, size int) map[string]any {
	return map[string]any{"items": items, "total": total, "page": page, "size": size, "has_more": page*size < total}
}

// toggleWord 可撤销关系操作的动词前缀（toggle_like / toggle_favorite 共用）。
func toggleWord(done bool) string {
	if done {
		return ""
	}
	return "取消"
}

// isDigits 纯数字串判断（timeline year/month 入参校验用）。
func isDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

// promoteMediaTokens 确认执行时把客户端随 confirm 提交的 media token 转正为媒体行；
// 转正中途失败即清理已转正文件；成功返回媒体行与已转正文件列表（调用方在后续落库失败时需要
// 据此清理正式区文件，与 REST 路径 cleanupPromoted 同口径；取消/过期零落库不在此处）。
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

// Registry 阶段一工具集；读写标志与 DESIGN 8.3 分档一致（写工具不对只读档开放）。
func Registry() []*Tool {
	return []*Tool{
		{
			Name:        "search_posts",
			Category:    "动态",
			Description: "按关键词搜索历史动态",
			Schema: map[string]string{
				"query": "string — 语义检索词",
			},
			Params: map[string]any{"type": "object", "properties": map[string]any{
				"query": map[string]any{"type": "string"},
			}, "required": []string{"query"}},
			Exec: func(ctx context.Context, d *Deps, userID int64, args map[string]any) (*ExecResult, error) {
				q := "%" + str(args, "query") + "%"
				rows, err := d.DB.QueryContext(ctx, `
					SELECT `+postHitColumns+`
					FROM posts p JOIN users u ON u.id = p.author_id
					WHERE p.deleted_at IS NULL AND (p.content LIKE ? OR p.title LIKE ?)
					ORDER BY p.id DESC LIMIT 10`, q, q)
				if err != nil {
					return nil, err
				}
				defer rows.Close()
				hits, err := scanPostHits(rows)
				if err != nil {
					return nil, err
				}
				if hits == nil {
					hits = []postHit{}
				}
				d.audit(ctx, userID, "search_posts", str(args, "query"), "ok")
				b, _ := json.Marshal(hits)
				return &ExecResult{Content: string(b), Data: hits}, nil
			},
		},
		{
			Name:        "delete_post",
			Category:    "动态",
			Description: "删除动态（进入回收站，需用户确认后生效）",
			Schema: map[string]string{
				"post_id": "number — 动态 ID",
			},
			Params: map[string]any{"type": "object", "properties": map[string]any{
				"post_id": map[string]any{"type": "integer"},
			}, "required": []string{"post_id"}},
			IsWrite: true,
			AppOnly: true,
			Exec: func(ctx context.Context, d *Deps, userID int64, args map[string]any) (*ExecResult, error) {
				postID := num(args, "post_id")
				p, err := d.DB.GetPost(ctx, postID)
				if errors.Is(err, store.ErrNotFound) {
					return nil, fmt.Errorf("动态不存在")
				}
				if err != nil {
					return nil, err
				}
				if err := d.DB.SoftDeletePost(ctx, postID, store.ActorTypeAI, userID); err != nil {
					return nil, err
				}
				// 通知口径对齐 REST DELETE /posts/{id}：删对方内容 → 站内通知作者（自删静默，无外推）
				_ = d.DB.NotifyContentDeleted(ctx, store.DeletedNotice{
					CreatorID: p.AuthorID, DeleterID: userID, DeleterName: d.aiOperatorName(ctx, userID),
					EventType: "post_deleted", TargetType: "post", TargetID: postID,
					Extra: map[string]any{"title": p.Title.String, "content": textutil.Trunc(p.Content, 50)},
				})
				d.audit(ctx, userID, "delete_post", textutil.Trunc(p.Content, 50), "deleted")
				return &ExecResult{Content: "动态已删除（进回收站）", Data: map[string]any{"post_id": postID}}, nil
			},
		},
		{
			Name:        "get_today_in_history",
			Category:    "动态",
			Description: "获取那年今日：历年在今天同月同日发布的动态",
			Schema:      map[string]string{},
			Params: map[string]any{"type": "object", "properties": map[string]any{
				"years_back": map[string]any{"type": "integer"},
			}},
			Exec: func(ctx context.Context, d *Deps, userID int64, args map[string]any) (*ExecResult, error) {
				yearsBack := int(num(args, "years_back"))
				if yearsBack <= 0 {
					yearsBack = 50
				}
				if yearsBack > 50 {
					yearsBack = 50
				}
				posts, err := d.DB.ListPostsOnMonthDay(ctx, yearsBack, 10)
				if err != nil {
					return nil, err
				}
				hits := make([]postHit, 0, len(posts))
				for _, p := range posts {
					hits = append(hits, postHit{ID: p.ID, Title: p.Title.String, Content: p.Content,
						Date: p.CreatedAt, Author: p.AuthorNickname.String, LocationName: p.LocationName.String})
				}
				d.audit(ctx, userID, "get_today_in_history", fmt.Sprintf("years_back=%d", yearsBack), "ok")
				b, _ := json.Marshal(hits)
				return &ExecResult{Content: string(b), Data: hits}, nil
			},
		},
		{
			Name:        "get_space_summary",
			Category:    "空间",
			Description: "空间概览：在一起天数、动态数、相册数、纪念日数等统计",
			Schema:      map[string]string{},
			Params:      map[string]any{"type": "object", "properties": map[string]any{}},
			Exec: func(ctx context.Context, d *Deps, userID int64, _ map[string]any) (*ExecResult, error) {
				var posts, albums, anniversaries int64
				_ = d.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM posts WHERE deleted_at IS NULL`).Scan(&posts)
				_ = d.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM albums WHERE deleted_at IS NULL AND is_hidden=0`).Scan(&albums)
				_ = d.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM anniversaries WHERE deleted_at IS NULL`).Scan(&anniversaries)
				summary := map[string]int64{"posts": posts, "albums": albums, "anniversaries": anniversaries}
				d.audit(ctx, userID, "get_space_summary", "", "ok")
				b, _ := json.Marshal(summary)
				return &ExecResult{Content: string(b), Data: summary}, nil
			},
		},
		{
			Name:        "get_timeline_summary",
			Category:    "空间",
			Description: "时间线统计：按年/月（给 year）或按日（给 month，YYYY-MM）统计动态数量分布",
			Schema: map[string]string{
				"year":  "string — 年份 YYYY（可选）",
				"month": "string — 月份 YYYY-MM（可选）",
			},
			Params: map[string]any{"type": "object", "properties": map[string]any{
				"year":  map[string]any{"type": "string"},
				"month": map[string]any{"type": "string"},
			}},
			Exec: func(ctx context.Context, d *Deps, userID int64, args map[string]any) (*ExecResult, error) {
				year, month := str(args, "year"), str(args, "month")
				if year != "" && (len(year) != 4 || !isDigits(year)) {
					return nil, fmt.Errorf("year 格式应为 YYYY")
				}
				if month != "" && (len(month) != 7 || month[4] != '-' || !isDigits(month[:4]) || !isDigits(month[5:])) {
					return nil, fmt.Errorf("month 格式应为 YYYY-MM")
				}
				counts, err := d.DB.TimelinePostCounts(ctx, year, month)
				if err != nil {
					return nil, err
				}
				if counts == nil {
					counts = []store.PeriodCount{}
				}
				d.audit(ctx, userID, "get_timeline_summary",
					fmt.Sprintf("year=%s month=%s", year, month), "ok")
				b, _ := json.Marshal(counts)
				return &ExecResult{Content: string(b), Data: counts}, nil
			},
		},
		{
			Name:        "create_post",
			Category:    "动态",
			Description: "发布一条动态（需用户确认后生效）",
			Schema: map[string]string{
				"content": "string — 动态正文",
				"mood":    "string — 心情（可选）",
			},
			Params: map[string]any{"type": "object", "properties": map[string]any{
				"content": map[string]any{"type": "string"},
				"mood":    map[string]any{"type": "string"},
			}, "required": []string{"content"}},
			IsWrite: true,
			Exec: func(ctx context.Context, d *Deps, userID int64, args map[string]any) (*ExecResult, error) {
				content := str(args, "content")
				if content == "" {
					return nil, fmt.Errorf("content 不能为空")
				}
				medias, promoted, err := promoteMediaTokens(ctx, d, tokens(args, "media_tokens"))
				if err != nil {
					return nil, err
				}
				postID, err := d.DB.CreatePostWithMedia(ctx, &store.Post{
					AuthorID: userID, Content: content, Mood: store.NullStr(str(args, "mood")),
				}, medias)
				if err != nil {
					// 落库失败回收已转正的正式区文件，避免孤儿媒体（与 REST cleanupPromoted 同口径）
					for _, pr := range promoted {
						d.MP.Delete(pr.FilePath, pr.ThumbPath)
					}
					return nil, err
				}
				snapshot, _ := json.Marshal(map[string]any{"content": content, "operator": d.aiOperatorName(ctx, userID), "via": "ai"})
				d.notifyPartners(ctx, userID, "post_created", "post", postID, string(snapshot), "新动态", content)
				return &ExecResult{Content: fmt.Sprintf("动态 #%d 已发布", postID), Data: map[string]any{"post_id": postID}}, nil
			},
		},
		{
			Name:        "create_album",
			Category:    "相册",
			Description: "创建相册（需用户确认后生效；媒体从动态媒体池选择）",
			Schema: map[string]string{
				"name":        "string — 相册名",
				"description": "string — 描述（可选）",
			},
			Params: map[string]any{"type": "object", "properties": map[string]any{
				"name":        map[string]any{"type": "string"},
				"description": map[string]any{"type": "string"},
			}, "required": []string{"name"}},
			IsWrite: true,
			Exec: func(ctx context.Context, d *Deps, userID int64, args map[string]any) (*ExecResult, error) {
				name := str(args, "name")
				if name == "" {
					return nil, fmt.Errorf("name 不能为空")
				}
				albumID, err := d.DB.CreateAlbum(ctx, "manual", name, str(args, "description"), "", userID)
				if err != nil {
					return nil, err
				}

				// 相册上新：站内通知另一方 + 按订阅外推（口径对齐 api/albums.go 的手动建相册，
				// operator 记真实发起人、via 标记 AI 代操作）
				operator := d.aiOperatorName(ctx, userID)
				snapshot, _ := json.Marshal(map[string]any{"name": name, "operator": operator, "via": "ai"})
				d.notifyPartners(ctx, userID, "album_created", "album", albumID, string(snapshot),
					"相册上新", operator+"（AI）创建了相册「"+name+"」")

				return &ExecResult{Content: fmt.Sprintf("相册「%s」已创建", name), Data: map[string]any{"album_id": albumID}}, nil
			},
		},
		{
			Name:        "add_photos_to_album",
			Category:    "相册",
			Description: "把动态媒体池中的照片加入相册（media_ids 必须来自既有动态，不接受上传附件）",
			Schema: map[string]string{
				"album_id":  "number — 相册 ID",
				"media_ids": "number[] — 待加入媒体 ID（来自动态媒体池）",
			},
			Params: map[string]any{"type": "object", "properties": map[string]any{
				"album_id":  map[string]any{"type": "integer"},
				"media_ids": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}},
			}, "required": []string{"album_id", "media_ids"}},
			IsWrite: true,
			Exec: func(ctx context.Context, d *Deps, userID int64, args map[string]any) (*ExecResult, error) {
				// 相册只从动态媒体池选入；随 confirm 上传的新附件 token 直接拒绝（DESIGN §12 / mock 契约）
				if tks := tokens(args, "media_tokens"); len(tks) > 0 {
					return nil, fmt.Errorf("新附件不能直接加入相册：媒体只能随动态发布，请在时间轴选择照片后加入")
				}
				albumID := num(args, "album_id")
				if albumID == 0 {
					return nil, fmt.Errorf("album_id 必填")
				}
				ids, err := ints(args, "media_ids")
				if err != nil || len(ids) == 0 {
					return nil, fmt.Errorf("media_ids 必填（来自动态媒体池）")
				}
				if _, err := d.DB.GetAlbum(ctx, albumID); err != nil {
					return nil, fmt.Errorf("相册不存在")
				}
				if err := d.DB.AddAlbumItems(ctx, albumID, userID, ids); err != nil {
					return nil, err
				}
				return &ExecResult{Content: fmt.Sprintf("已把 %d 张照片加入相册", len(ids)), Data: map[string]any{"album_id": albumID, "added": len(ids)}}, nil
			},
		},
		{
			Name:        "remove_photos_from_album",
			Category:    "相册",
			Description: "从相册移除照片（不影响照片本身，需用户确认后生效）",
			Schema: map[string]string{
				"album_id":  "number — 相册 ID",
				"photo_ids": "number[] — 待移除照片 ID",
			},
			Params: map[string]any{"type": "object", "properties": map[string]any{
				"album_id":  map[string]any{"type": "integer"},
				"photo_ids": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}},
			}, "required": []string{"album_id", "photo_ids"}},
			IsWrite: true,
			Exec: func(ctx context.Context, d *Deps, userID int64, args map[string]any) (*ExecResult, error) {
				albumID := num(args, "album_id")
				if albumID == 0 {
					return nil, fmt.Errorf("album_id 必填")
				}
				ids, err := ints(args, "photo_ids")
				if err != nil || len(ids) == 0 {
					return nil, fmt.Errorf("photo_ids 必填")
				}
				if _, err := d.DB.GetAlbum(ctx, albumID); err != nil {
					return nil, fmt.Errorf("相册不存在")
				}
				if err := d.DB.RemoveAlbumItems(ctx, albumID, ids); err != nil {
					return nil, err
				}
				return &ExecResult{Content: fmt.Sprintf("已从相册移除 %d 张照片", len(ids)), Data: map[string]any{"album_id": albumID, "removed": len(ids)}}, nil
			},
		},
		{
			Name:        "suggest_photo_grouping",
			Category:    "相册",
			Description: "推荐照片分组：按日期与地点聚合候选媒体，给出分组建议（纯查询，不生成内容）",
			Schema: map[string]string{
				"date_range": "object — 日期范围（可选，包含 from/to）",
				"location":   "string — 地点筛选（可选）",
			},
			Params: map[string]any{"type": "object", "properties": map[string]any{
				"date_range": map[string]any{"type": "object", "properties": map[string]any{
					"from": map[string]any{"type": "string"},
					"to":   map[string]any{"type": "string"},
				}},
				"location": map[string]any{"type": "string"},
			}},
			Exec: func(ctx context.Context, d *Deps, userID int64, args map[string]any) (*ExecResult, error) {
				from, to, location := "", "", str(args, "location")
				if dr, ok := args["date_range"].(map[string]any); ok {
					from, to = str(dr, "from"), str(dr, "to")
				}
				cands, err := d.DB.ListMediaGroupCandidates(ctx, from, to, location)
				if err != nil {
					return nil, err
				}
				type group struct {
					Key      string  `json:"key"`
					Date     string  `json:"date"`
					Location string  `json:"location,omitempty"`
					MediaIDs []int64 `json:"media_ids"`
					Count    int     `json:"count"`
				}
				groups := []group{}
				index := map[string]int{}
				for _, c := range cands {
					key := c.Date + "|" + c.Location
					i, ok := index[key]
					if !ok {
						if len(groups) >= 10 {
							continue
						}
						groups = append(groups, group{Key: key, Date: c.Date, Location: c.Location, MediaIDs: []int64{}})
						i = len(groups) - 1
						index[key] = i
					}
					groups[i].MediaIDs = append(groups[i].MediaIDs, c.MediaID)
					groups[i].Count++
				}
				d.audit(ctx, userID, "suggest_photo_grouping",
					fmt.Sprintf("from=%s to=%s location=%s", from, to, location), fmt.Sprintf("groups=%d", len(groups)))
				b, _ := json.Marshal(groups)
				return &ExecResult{Content: string(b), Data: groups}, nil
			},
		},
		{
			Name:        "list_albums",
			Category:    "相册",
			Description: "列出所有相册（timeline 含月度相册，collection 仅手动相册；分页）",
			Schema: map[string]string{
				"view_type": "string — timeline（全部）或 collection（手动相册）",
				"page":      "number — 页码（从 1 开始，可选）",
				"page_size": "number — 每页数量（可选）",
			},
			Params: map[string]any{"type": "object", "properties": map[string]any{
				"view_type": map[string]any{"type": "string"},
				"page":      map[string]any{"type": "integer"},
				"page_size": map[string]any{"type": "integer"},
			}},
			Exec: func(ctx context.Context, d *Deps, userID int64, args map[string]any) (*ExecResult, error) {
				viewType := str(args, "view_type")
				if viewType == "" {
					viewType = "timeline"
				}
				if viewType != "timeline" && viewType != "collection" {
					return nil, fmt.Errorf("view_type 仅允许 timeline / collection")
				}
				albums, err := d.DB.ListAlbums(ctx, false)
				if err != nil {
					return nil, err
				}
				items := make([]map[string]any, 0, len(albums))
				for _, al := range albums {
					if viewType == "collection" && al.Type != "manual" {
						continue
					}
					cover := ""
					if al.CoverMediaID.Valid {
						cover = d.MP.SignedURL(al.CoverMediaID.Int64, "thumb")
					} else if medias, err := d.DB.ListAlbumMedia(ctx, al.ID); err == nil && len(medias) > 0 {
						cover = d.MP.SignedURL(medias[0].ID, "thumb")
					}
					items = append(items, map[string]any{
						"id": al.ID, "type": al.Type, "name": al.Name, "description": al.Description.String,
						"month_key": al.MonthKey.String, "is_hidden": al.IsHidden, "item_count": al.ItemCount,
						"cover_url": cover,
					})
				}
				total := len(items)
				page, size := pageArgs(args)
				lo := (page - 1) * size
				if lo > total {
					lo = total
				}
				hi := lo + size
				if hi > total {
					hi = total
				}
				out := pagedOut(items[lo:hi], total, page, size)
				d.audit(ctx, userID, "list_albums", fmt.Sprintf("view=%s page=%d", viewType, page), fmt.Sprintf("total=%d", total))
				b, _ := json.Marshal(out)
				return &ExecResult{Content: string(b), Data: out}, nil
			},
		},
		{
			Name:        "delete_album",
			Category:    "相册",
			Description: "删除手动相册（进入回收站，不影响照片本身；需用户确认后生效）",
			Schema: map[string]string{
				"album_id": "number — 相册 ID",
			},
			Params: map[string]any{"type": "object", "properties": map[string]any{
				"album_id": map[string]any{"type": "integer"},
			}, "required": []string{"album_id"}},
			IsWrite: true,
			AppOnly: true,
			Exec: func(ctx context.Context, d *Deps, userID int64, args map[string]any) (*ExecResult, error) {
				albumID := num(args, "album_id")
				al, err := d.DB.GetAlbum(ctx, albumID)
				if errors.Is(err, store.ErrNotFound) {
					return nil, fmt.Errorf("相册不存在")
				}
				if err != nil {
					return nil, err
				}
				if al.Type == "monthly" {
					return nil, fmt.Errorf("月度相册不可删除，仅可在设置中隐藏")
				}
				if err := d.DB.SoftDeleteAlbum(ctx, albumID, store.ActorTypeAI, userID); err != nil {
					return nil, err
				}
				// AI 代删对方创建的内容 → 站内通知创建者（自删静默；内容删除仅站内，不带 via，与 delete_post 同口径）
				if al.CreatedBy.Valid {
					_ = d.DB.NotifyContentDeleted(ctx, store.DeletedNotice{
						CreatorID: al.CreatedBy.Int64, DeleterID: userID, DeleterName: d.aiOperatorName(ctx, userID),
						EventType: "album_deleted", TargetType: "album", TargetID: albumID,
						Extra: map[string]any{"name": al.Name},
					})
				}
				return &ExecResult{Content: fmt.Sprintf("相册「%s」已删除（进回收站）", al.Name), Data: map[string]any{"album_id": albumID}}, nil
			},
		},
		{
			Name:        "toggle_like",
			Category:    "互动",
			Description: "点赞/取消点赞动态（需用户确认后生效）",
			Schema: map[string]string{
				"post_id": "number — 动态 ID",
			},
			Params: map[string]any{"type": "object", "properties": map[string]any{
				"post_id": map[string]any{"type": "integer"},
			}, "required": []string{"post_id"}},
			IsWrite: true,
			Exec: func(ctx context.Context, d *Deps, userID int64, args map[string]any) (*ExecResult, error) {
				postID := num(args, "post_id")
				liked, err := d.DB.ToggleLike(ctx, postID, userID)
				if err != nil {
					return nil, err
				}
				return &ExecResult{Content: fmt.Sprintf("已%s点赞", toggleWord(liked)), Data: map[string]any{"liked": liked}}, nil
			},
		},
		{
			Name:        "toggle_favorite",
			Category:    "互动",
			Description: "收藏/取消收藏动态（可撤销关系数据，不进回收站；需用户确认后生效）",
			Schema: map[string]string{
				"post_id": "number — 动态 ID",
			},
			Params: map[string]any{"type": "object", "properties": map[string]any{
				"post_id": map[string]any{"type": "integer"},
			}, "required": []string{"post_id"}},
			IsWrite: true,
			Exec: func(ctx context.Context, d *Deps, userID int64, args map[string]any) (*ExecResult, error) {
				postID := num(args, "post_id")
				favorited, err := d.DB.ToggleFavorite(ctx, postID, userID)
				if err != nil {
					return nil, err
				}
				return &ExecResult{Content: fmt.Sprintf("已%s收藏", toggleWord(favorited)), Data: map[string]any{"favorited": favorited}}, nil
			},
		},
		{
			Name:        "list_favorites",
			Category:    "互动",
			Description: "列出我收藏的动态（收藏时间倒序，分页）",
			Schema: map[string]string{
				"page": "number — 页码",
			},
			Params: map[string]any{"type": "object", "properties": map[string]any{
				"page":      map[string]any{"type": "integer"},
				"page_size": map[string]any{"type": "integer"},
			}},
			Exec: func(ctx context.Context, d *Deps, userID int64, args map[string]any) (*ExecResult, error) {
				page, size := pageArgs(args)
				posts, total, err := d.DB.ListFavoritePostsPaged(ctx, userID, page, size)
				if err != nil {
					return nil, err
				}
				hits := make([]postHit, 0, len(posts))
				for _, p := range posts {
					hits = append(hits, postHit{ID: p.ID, Title: p.Title.String, Content: p.Content,
						Date: p.CreatedAt, Author: p.AuthorNickname.String, LocationName: p.LocationName.String})
				}
				out := pagedOut(hits, total, page, size)
				d.audit(ctx, userID, "list_favorites", fmt.Sprintf("page=%d", page), fmt.Sprintf("total=%d", total))
				b, _ := json.Marshal(out)
				return &ExecResult{Content: string(b), Data: out}, nil
			},
		},
		{
			Name:        "create_comment",
			Category:    "互动",
			Description: "发表评论（最多 500 字，需用户确认后生效）",
			Schema: map[string]string{
				"post_id": "number — 动态 ID",
				"content": "string — 评论内容",
			},
			Params: map[string]any{"type": "object", "properties": map[string]any{
				"post_id": map[string]any{"type": "integer"},
				"content": map[string]any{"type": "string"},
			}, "required": []string{"post_id", "content"}},
			IsWrite: true,
			Exec: func(ctx context.Context, d *Deps, userID int64, args map[string]any) (*ExecResult, error) {
				postID := num(args, "post_id")
				content := str(args, "content")
				if content == "" {
					return nil, fmt.Errorf("评论内容不能为空")
				}
				if len([]rune(content)) > 500 {
					return nil, fmt.Errorf("评论最多 500 字")
				}
				if _, err := d.DB.GetPost(ctx, postID); err != nil {
					return nil, fmt.Errorf("动态不存在")
				}
				commentID, err := d.DB.CreateComment(ctx, postID, userID, content)
				if err != nil {
					return nil, err
				}
				// 通知口径对齐 REST POST /posts/{id}/comments：任何一方评论都通知另一方（D6，§4.8）
				operator, err := d.DB.GetUserByID(ctx, userID)
				if err != nil {
					return nil, err
				}
				snapshot, _ := json.Marshal(map[string]any{"content": textutil.Trunc(content, 50), "operator": operator.Username})
				d.notifyPartners(ctx, userID, "post_commented", "post", postID, string(snapshot),
					"收到评论", operator.Username+"："+textutil.Trunc(content, 50))
				d.audit(ctx, userID, "create_comment", textutil.Trunc(content, 50), fmt.Sprintf("comment=%d", commentID))
				return &ExecResult{Content: "评论已发表", Data: map[string]any{"comment_id": commentID}}, nil
			},
		},
		{
			Name:        "delete_comment",
			Category:    "互动",
			Description: "删除评论（进入回收站，需用户确认后生效）",
			Schema: map[string]string{
				"comment_id": "number — 评论 ID",
			},
			Params: map[string]any{"type": "object", "properties": map[string]any{
				"comment_id": map[string]any{"type": "integer"},
			}, "required": []string{"comment_id"}},
			IsWrite: true,
			AppOnly: true,
			Exec: func(ctx context.Context, d *Deps, userID int64, args map[string]any) (*ExecResult, error) {
				id := num(args, "comment_id")
				c, err := d.DB.GetComment(ctx, id)
				if errors.Is(err, store.ErrNotFound) {
					return nil, fmt.Errorf("评论不存在")
				}
				if err != nil {
					return nil, err
				}
				if err := d.DB.SoftDeleteComment(ctx, id, "ai", userID); err != nil {
					return nil, err
				}
				// 通知口径对齐 REST DELETE /comments/{id}：删对方评论 → 通知评论作者（自删静默）
				_ = d.DB.NotifyContentDeleted(ctx, store.DeletedNotice{
					CreatorID: c.AuthorID, DeleterID: userID, DeleterName: d.aiOperatorName(ctx, userID),
					EventType: "comment_deleted", TargetType: "comment", TargetID: id,
					Extra: map[string]any{"content": textutil.Trunc(c.Content, 50)},
				})
				d.audit(ctx, userID, "delete_comment", textutil.Trunc(c.Content, 50), "deleted")
				return &ExecResult{Content: "评论已删除（进回收站）", Data: map[string]any{"comment_id": id}}, nil
			},
		},
		{
			Name:        "list_comments",
			Category:    "互动",
			Description: "列出某条动态下的评论（时间倒序，分页）",
			Schema: map[string]string{
				"post_id": "number — 动态 ID",
			},
			Params: map[string]any{"type": "object", "properties": map[string]any{
				"post_id":   map[string]any{"type": "integer"},
				"page":      map[string]any{"type": "integer"},
				"page_size": map[string]any{"type": "integer"},
			}, "required": []string{"post_id"}},
			Exec: func(ctx context.Context, d *Deps, userID int64, args map[string]any) (*ExecResult, error) {
				postID := num(args, "post_id")
				page, size := pageArgs(args)
				comments, err := d.DB.ListComments(ctx, postID, size, (page-1)*size)
				if err != nil {
					return nil, err
				}
				total, err := d.DB.CountComments(ctx, postID)
				if err != nil {
					return nil, err
				}
				items := make([]map[string]any, 0, len(comments))
				for _, c := range comments {
					items = append(items, map[string]any{
						"id": c.ID, "post_id": c.PostID, "author": c.AuthorNickname.String,
						"content": c.Content, "created_at": c.CreatedAt,
					})
				}
				out := pagedOut(items, total, page, size)
				d.audit(ctx, userID, "list_comments", fmt.Sprintf("post=%d page=%d", postID, page), fmt.Sprintf("total=%d", total))
				b, _ := json.Marshal(out)
				return &ExecResult{Content: string(b), Data: out}, nil
			},
		},
		{
			Name:        "create_map_point",
			Category:    "地图",
			Description: "创建地图点位（足迹）；同名地点复用既有点位",
			Schema: map[string]string{
				"name":        "string — 地点名",
				"latitude":    "number — 纬度",
				"longitude":   "number — 经度",
				"visit_date":  "string — 到访日期 YYYY-MM-DD（可选）",
				"description": "string — 描述（可选）",
			},
			Params: map[string]any{"type": "object", "properties": map[string]any{
				"name":        map[string]any{"type": "string"},
				"latitude":    map[string]any{"type": "number"},
				"longitude":   map[string]any{"type": "number"},
				"visit_date":  map[string]any{"type": "string"},
				"description": map[string]any{"type": "string"},
			}, "required": []string{"name", "latitude", "longitude"}},
			IsWrite: true,
			Exec: func(ctx context.Context, d *Deps, userID int64, args map[string]any) (*ExecResult, error) {
				name := str(args, "name")
				if name == "" {
					return nil, fmt.Errorf("name 不能为空")
				}
				lat, lok := args["latitude"].(float64)
				lng, lokOk := args["longitude"].(float64)
				if !lok || !lokOk {
					return nil, fmt.Errorf("latitude/longitude 必填（数值）")
				}
				p, created, err := d.DB.FindOrCreatePoint(ctx, store.PointInput{
					Name: name, Lat: lat, Lng: lng,
					VisitDate: str(args, "visit_date"), Description: str(args, "description"),
					Source: "ai", UserID: userID,
				})
				if err != nil {
					return nil, err
				}
				content := fmt.Sprintf("已把「%s」记到地图上", name)
				if !created {
					content = fmt.Sprintf("「%s」已在地图上，复用了既有点位", name)
				}

				// 足迹更新：仅新建时通知另一方（复用点位不通知）；手动/AI 报点同口径，
				// 动态带地点自动建点不发（该次发布已有 post_created）
				if created {
					operator := d.aiOperatorName(ctx, userID)
					snapshot, _ := json.Marshal(map[string]any{
						"name": name, "latitude": lat, "longitude": lng, "operator": operator, "via": "ai",
					})
					d.notifyPartners(ctx, userID, "map_point_created", "map_point", p.ID, string(snapshot),
						"足迹更新", operator+"（AI）把「"+name+"」记到了地图上")
				}

				return &ExecResult{Content: content, Data: map[string]any{"point_id": p.ID, "created": created}}, nil
			},
		},
		{
			// 删除类工具仅 APP 内部通道可用，不对 PAT 开放（铁律 AGENTS §5 / DESIGN §8.3 分档）
			Name:        "delete_map_point",
			Category:    "地图",
			Description: "删除地图点位（进入回收站，需用户确认后生效）",
			Schema: map[string]string{
				"point_id": "number — 点位 ID",
			},
			Params: map[string]any{"type": "object", "properties": map[string]any{
				"point_id": map[string]any{"type": "integer"},
			}, "required": []string{"point_id"}},
			IsWrite: true,
			AppOnly: true,
			Exec: func(ctx context.Context, d *Deps, userID int64, args map[string]any) (*ExecResult, error) {
				pointID := num(args, "point_id")
				p, err := d.DB.GetPoint(ctx, pointID)
				if errors.Is(err, store.ErrNotFound) {
					return nil, fmt.Errorf("点位不存在")
				}
				if err != nil {
					return nil, err
				}
				if err := d.DB.SoftDeletePoint(ctx, pointID, store.ActorTypeAI, userID); err != nil {
					return nil, err
				}
				// AI 代删对方创建的内容 → 站内通知创建者（自删静默；内容删除仅站内，不带 via，与 delete_post 同口径）
				_ = d.DB.NotifyContentDeleted(ctx, store.DeletedNotice{
					CreatorID: p.CreatedBy, DeleterID: userID, DeleterName: d.aiOperatorName(ctx, userID),
					EventType: "map_point_deleted", TargetType: "map_point", TargetID: pointID,
					Extra: map[string]any{"name": p.Name},
				})
				return &ExecResult{Content: fmt.Sprintf("已删除地图点位「%s」", p.Name), Data: map[string]any{"point_id": pointID}}, nil
			},
		},
		{
			Name:        "search_locations",
			Category:    "地图",
			Description: "按名称/描述搜索地图足迹点位",
			Schema: map[string]string{
				"query": "string — 地点关键词",
			},
			Params: map[string]any{"type": "object", "properties": map[string]any{
				"query": map[string]any{"type": "string"},
			}, "required": []string{"query"}},
			Exec: func(ctx context.Context, d *Deps, userID int64, args map[string]any) (*ExecResult, error) {
				query := str(args, "query")
				if query == "" {
					return nil, fmt.Errorf("query 不能为空")
				}
				points, err := d.DB.SearchPoints(ctx, query, 20)
				if err != nil {
					return nil, err
				}
				out := make([]map[string]any, 0, len(points))
				for _, p := range points {
					out = append(out, map[string]any{
						"id": p.ID, "name": p.Name, "latitude": p.Lat, "longitude": p.Lng,
						"city": p.City, "visit_date": p.VisitDate.String, "description": p.Description,
					})
				}
				d.audit(ctx, userID, "search_locations", query, fmt.Sprintf("hits=%d", len(out)))
				b, _ := json.Marshal(out)
				return &ExecResult{Content: string(b), Data: out}, nil
			},
		},
		{
			Name:        "get_location_posts",
			Category:    "地图",
			Description: "获取某地点（点位）下的全部动态",
			Schema: map[string]string{
				"location_id": "number — 点位 ID",
			},
			Params: map[string]any{"type": "object", "properties": map[string]any{
				"location_id": map[string]any{"type": "integer"},
			}, "required": []string{"location_id"}},
			Exec: func(ctx context.Context, d *Deps, userID int64, args map[string]any) (*ExecResult, error) {
				pointID := num(args, "location_id")
				point, err := d.DB.GetPoint(ctx, pointID)
				if errors.Is(err, store.ErrNotFound) {
					return nil, fmt.Errorf("点位不存在")
				}
				if err != nil {
					return nil, err
				}
				posts, err := d.DB.ListPostsByPoint(ctx, pointID, 20)
				if err != nil {
					return nil, err
				}
				hits := make([]postHit, 0, len(posts))
				for _, p := range posts {
					hits = append(hits, postHit{ID: p.ID, Title: p.Title.String, Content: p.Content,
						Date: p.CreatedAt, Author: p.AuthorNickname.String, LocationName: p.LocationName.String})
				}
				d.audit(ctx, userID, "get_location_posts", fmt.Sprintf("point=%d name=%s", pointID, point.Name), fmt.Sprintf("hits=%d", len(hits)))
				b, _ := json.Marshal(hits)
				return &ExecResult{Content: string(b), Data: hits}, nil
			},
		},
		{
			// T1（DESIGN §6.4/§12.2）：设计名 get_next_anniversary；输出补 type 与农历换算公历 next_solar_date
			Name:        "get_next_anniversary",
			Category:    "纪念日",
			Description: "获取下一个纪念日信息（含类型与农历换算的公历日期）",
			Schema:      map[string]string{},
			Params:      map[string]any{"type": "object", "properties": map[string]any{}},
			Exec: func(ctx context.Context, d *Deps, userID int64, _ map[string]any) (*ExecResult, error) {
				list, err := d.DB.ListAnniversaries(ctx)
				if err != nil {
					return nil, err
				}
				now := time.Now()
				var best *store.Anniversary
				for _, a := range list {
					if best == nil || a.DaysUntil(now) < best.DaysUntil(now) {
						best = a
					}
				}
				d.audit(ctx, userID, "get_next_anniversary", "", fmt.Sprintf("count=%d", len(list)))
				if best == nil {
					return &ExecResult{Content: "当前还没有纪念日", Data: nil}, nil
				}
				item := map[string]any{
					"name": best.Name, "date": best.Date, "days_until": best.DaysUntil(now),
					"type": best.Type, "next_solar_date": best.NextSolarDate(now),
				}
				b, _ := json.Marshal(item)
				return &ExecResult{Content: string(b), Data: item}, nil
			},
		},
		{
			Name:        "create_anniversary",
			Category:    "纪念日",
			Description: "创建纪念日（date 统一 YYYY-MM-DD；solar=公历，lunar=农历日期；需用户确认后生效）",
			Schema: map[string]string{
				"name":               "string — 名称",
				"date":               "string — YYYY-MM-DD",
				"type":               "solar | lunar",
				"remind_days_before": "number — 提前提醒天数",
			},
			Params: map[string]any{"type": "object", "properties": map[string]any{
				"name":               map[string]any{"type": "string"},
				"date":               map[string]any{"type": "string"},
				"type":               map[string]any{"type": "string"},
				"remind_days_before": map[string]any{"type": "integer"},
			}, "required": []string{"name", "date"}},
			IsWrite: true,
			Exec: func(ctx context.Context, d *Deps, userID int64, args map[string]any) (*ExecResult, error) {
				name, date := str(args, "name"), str(args, "date")
				if name == "" || date == "" {
					return nil, fmt.Errorf("name 与 date 必填")
				}
				// 口径对齐 REST：type 缺省/其它值归一 solar，仅显式 'lunar' 生效（§12 契约）
				annivType := "solar"
				if str(args, "type") == "lunar" {
					annivType = "lunar"
				}
				if !lunar.ValidDateShape(date) {
					return nil, fmt.Errorf("日期格式应为 YYYY-MM-DD")
				}
				if annivType != "lunar" { // solar 需为真实公历日；lunar 仅格式（农历日可落在公历不存在日）
					if _, err := time.Parse("2006-01-02", date); err != nil {
						return nil, fmt.Errorf("日期格式应为 YYYY-MM-DD")
					}
				}
				// 缺省与显式 0 语义不同（契约同 REST/mock）：未传 → 提前 1 天；显式 0 = 当天提醒，不得被改写。
				remind := 1
				if v, ok := args["remind_days_before"].(float64); ok {
					remind = int(v)
				}
				if remind < 0 || remind > 90 {
					remind = 1
				}
				id, err := d.DB.CreateAnniversary(ctx, name, date, annivType, remind, userID)
				if err != nil {
					return nil, err
				}
				return &ExecResult{Content: fmt.Sprintf("纪念日「%s」已创建", name),
					Data: map[string]any{"anniversary_id": id, "type": annivType}}, nil
			},
		},
		{
			Name:        "delete_anniversary",
			Category:    "纪念日",
			Description: "删除纪念日（进入回收站，需用户确认后生效）",
			Schema: map[string]string{
				"anniversary_id": "number — 纪念日 ID",
			},
			Params: map[string]any{"type": "object", "properties": map[string]any{
				"anniversary_id": map[string]any{"type": "integer"},
			}, "required": []string{"anniversary_id"}},
			IsWrite: true,
			AppOnly: true,
			Exec: func(ctx context.Context, d *Deps, userID int64, args map[string]any) (*ExecResult, error) {
				id := num(args, "anniversary_id")
				a, err := d.DB.GetAnniversary(ctx, id)
				if errors.Is(err, store.ErrNotFound) {
					return nil, fmt.Errorf("纪念日不存在")
				}
				if err != nil {
					return nil, err
				}
				if err := d.DB.SoftDeleteAnniversary(ctx, id, store.ActorTypeAI, userID); err != nil {
					return nil, err
				}
				// AI 代删对方创建的内容 → 站内通知创建者（自删静默；内容删除仅站内，不带 via，与 delete_post 同口径）
				_ = d.DB.NotifyContentDeleted(ctx, store.DeletedNotice{
					CreatorID: a.CreatedBy, DeleterID: userID, DeleterName: d.aiOperatorName(ctx, userID),
					EventType: "anniversary_deleted", TargetType: "anniversary", TargetID: id,
					Extra: map[string]any{"name": a.Name},
				})
				d.audit(ctx, userID, "delete_anniversary", a.Name, "deleted")
				return &ExecResult{Content: fmt.Sprintf("纪念日「%s」已删除（进回收站）", a.Name), Data: map[string]any{"anniversary_id": id}}, nil
			},
		},
		{
			Name:        "configure_reminder",
			Category:    "纪念日",
			Description: "配置纪念日提醒的提前天数（0-90，站内+外部同发；需用户确认后生效）",
			Schema: map[string]string{
				"anniversary_id": "number — 纪念日 ID",
				"days_before":    "number — 提前提醒天数（0-90）",
			},
			Params: map[string]any{"type": "object", "properties": map[string]any{
				"anniversary_id": map[string]any{"type": "integer"},
				"days_before":    map[string]any{"type": "integer"},
			}, "required": []string{"anniversary_id", "days_before"}},
			IsWrite: true,
			Exec: func(ctx context.Context, d *Deps, userID int64, args map[string]any) (*ExecResult, error) {
				id, days := num(args, "anniversary_id"), int(num(args, "days_before"))
				if days < 0 || days > 90 {
					return nil, fmt.Errorf("提前天数应为 0-90")
				}
				if _, err := d.DB.GetAnniversary(ctx, id); err != nil {
					return nil, fmt.Errorf("纪念日不存在")
				}
				if err := d.DB.UpdateAnniversaryReminder(ctx, id, days); err != nil {
					return nil, err
				}
				return &ExecResult{Content: fmt.Sprintf("纪念日提醒已设为提前 %d 天", days), Data: map[string]any{"anniversary_id": id, "days_before": days}}, nil
			},
		},
	}
}

// RegistryForPAT 外部 PAT 通道可见的工具子集：过滤 AppOnly（删除类）工具。
// 铁律「删除类工具不对任何令牌开放」的注册表级强制——PAT 接线时必须经此函数列工具。
func RegistryForPAT() []*Tool {
	var list []*Tool
	for _, t := range Registry() {
		if !t.AppOnly {
			list = append(list, t)
		}
	}
	return list
}
