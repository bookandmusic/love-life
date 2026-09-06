// Package tools MCP 工具注册表（DESIGN 第 6 章规划 26 个；当前已注册 9 个：
// search_posts / get_space_summary / create_post / create_album / add_photos_to_album /
// toggle_like / create_map_point / delete_map_point / get_anniversaries，其余按同一模式待接线）。
// 读工具立即执行；写工具产出确认卡片（15 分钟 TTL）。
package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bookandmusic/love-life/server/internal/media"
	"github.com/bookandmusic/love-life/server/internal/notify"
	"github.com/bookandmusic/love-life/server/internal/store"
)

type ExecResult struct {
	Content string // 回填给模型的结果文本
	Data    any    // 附带给客户端的结构化数据（如确认卡片详情）
}

type Tool struct {
	Name        string
	Description string
	Params      map[string]any
	IsWrite     bool
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
	_ = d.DB.InsertAuditLog(ctx, "ai", userID, tool, digest, result)
}

// notifyPartnerNewPost AI 代发动态走与手动发布一致的通知口径（新动态通知另一方+外部推送，admin 不在情侣里）。
func (d *Deps) notifyPartnerNewPost(ctx context.Context, authorID, postID int64, content string) {
	recipients, err := d.DB.ListPartnerIDs(ctx, authorID)
	if err != nil {
		return
	}
	snapshot, _ := json.Marshal(map[string]any{"content": content, "via": "ai"})
	d.DP.Dispatch(ctx, recipients, "post_created", "post", postID, string(snapshot), "新动态", content)
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
			Description: "按关键词搜索历史动态",
			Params: map[string]any{"type": "object", "properties": map[string]any{
				"query": map[string]any{"type": "string"},
			}, "required": []string{"query"}},
			Exec: func(ctx context.Context, d *Deps, _ int64, args map[string]any) (*ExecResult, error) {
				q := "%" + str(args, "query") + "%"
				rows, err := d.DB.QueryContext(ctx, `
					SELECT p.id, COALESCE(p.title,''), p.content, p.created_at, COALESCE(u.nickname, u.username)
					FROM posts p JOIN users u ON u.id = p.author_id
					WHERE p.deleted_at IS NULL AND (p.content LIKE ? OR p.title LIKE ?)
					ORDER BY p.id DESC LIMIT 10`, q, q)
				if err != nil {
					return nil, err
				}
				defer rows.Close()
				type hit struct {
					ID      int64  `json:"id"`
					Title   string `json:"title,omitempty"`
					Content string `json:"content"`
					Date    string `json:"date"`
					Author  string `json:"author"`
				}
				hits := []hit{}
				for rows.Next() {
					var h hit
					if err := rows.Scan(&h.ID, &h.Title, &h.Content, &h.Date, &h.Author); err != nil {
						return nil, err
					}
					hits = append(hits, h)
				}
				d.audit(ctx, 0, "search_posts", str(args, "query"), "ok")
				b, _ := json.Marshal(hits)
				return &ExecResult{Content: string(b), Data: hits}, nil
			},
		},
		{
			Name:        "get_space_summary",
			Description: "空间概览：在一起天数、动态数、相册数、纪念日数等统计",
			Params:      map[string]any{"type": "object", "properties": map[string]any{}},
			Exec: func(ctx context.Context, d *Deps, _ int64, _ map[string]any) (*ExecResult, error) {
				var posts, albums, anniversaries int64
				_ = d.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM posts WHERE deleted_at IS NULL`).Scan(&posts)
				_ = d.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM albums WHERE deleted_at IS NULL AND is_hidden=0`).Scan(&albums)
				_ = d.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM anniversaries WHERE deleted_at IS NULL`).Scan(&anniversaries)
				summary := map[string]int64{"posts": posts, "albums": albums, "anniversaries": anniversaries}
				b, _ := json.Marshal(summary)
				return &ExecResult{Content: string(b), Data: summary}, nil
			},
		},
		{
			Name:        "create_post",
			Description: "发布一条动态（需用户确认后生效）",
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
				d.notifyPartnerNewPost(ctx, userID, postID, content)
				d.audit(ctx, userID, "create_post", content, "created")
				return &ExecResult{Content: fmt.Sprintf("动态 #%d 已发布", postID), Data: map[string]any{"post_id": postID}}, nil
			},
		},
		{
			Name:        "create_album",
			Description: "创建相册（需用户确认后生效；媒体从动态媒体池选择）",
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
				d.audit(ctx, userID, "create_album", name, "created")
				return &ExecResult{Content: fmt.Sprintf("相册「%s」已创建", name), Data: map[string]any{"album_id": albumID}}, nil
			},
		},
		{
			Name:        "add_photos_to_album",
			Description: "把动态媒体池中的照片加入相册（media_ids 必须来自既有动态，不接受上传附件）",
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
				d.audit(ctx, userID, "add_photos_to_album", fmt.Sprintf("album=%d ids=%v", albumID, ids), "added")
				return &ExecResult{Content: fmt.Sprintf("已把 %d 张照片加入相册", len(ids)), Data: map[string]any{"album_id": albumID, "added": len(ids)}}, nil
			},
		},
		{
			Name:        "toggle_like",
			Description: "点赞/取消点赞动态（需用户确认后生效）",
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
				d.audit(ctx, userID, "toggle_like", fmt.Sprint(postID), map[bool]string{true: "liked", false: "unliked"}[liked])
				return &ExecResult{Content: fmt.Sprintf("已%s点赞", map[bool]string{true: "", false: "取消"}[liked]), Data: map[string]any{"liked": liked}}, nil
			},
		},
		{
			Name:        "create_map_point",
			Description: "创建地图点位（足迹）；同名地点复用既有点位",
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
				d.audit(ctx, userID, "create_map_point", name, map[bool]string{true: "created", false: "reused"}[created])
				content := fmt.Sprintf("已把「%s」记到地图上", name)
				if !created {
					content = fmt.Sprintf("「%s」已在地图上，复用了既有点位", name)
				}
				return &ExecResult{Content: content, Data: map[string]any{"point_id": p.ID, "created": created}}, nil
			},
		},
		{
			// 删除类工具仅 APP 内部通道可用，不对 PAT 开放（铁律 AGENTS §5 / DESIGN §8.3 分档）
			Name:        "delete_map_point",
			Description: "删除地图点位（进入回收站，需用户确认后生效）",
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
				if err := d.DB.SoftDeletePoint(ctx, pointID); err != nil {
					return nil, err
				}
				d.audit(ctx, userID, "delete_map_point", p.Name, "deleted")
				return &ExecResult{Content: fmt.Sprintf("已删除地图点位「%s」", p.Name), Data: map[string]any{"point_id": pointID}}, nil
			},
		},
		{
			Name:        "get_anniversaries",
			Description: "列出全部纪念日与倒计时",
			Params:      map[string]any{"type": "object", "properties": map[string]any{}},
			Exec: func(ctx context.Context, d *Deps, _ int64, _ map[string]any) (*ExecResult, error) {
				list, err := d.DB.ListAnniversaries(ctx)
				if err != nil {
					return nil, err
				}
				type item struct {
					Name      string `json:"name"`
					Date      string `json:"date"`
					DaysUntil int    `json:"days_until"`
				}
				items := []item{}
				for _, a := range list {
					items = append(items, item{Name: a.Name, Date: a.Date, DaysUntil: a.DaysUntil(time.Now())})
				}
				b, _ := json.Marshal(items)
				return &ExecResult{Content: string(b), Data: items}, nil
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