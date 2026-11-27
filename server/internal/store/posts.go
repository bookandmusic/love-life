package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

type Post struct {
	ID           int64
	AuthorID     int64
	Content      string
	Mood         sql.NullString
	LocationName sql.NullString
	PointID      sql.NullInt64
	CreatedAt    string
	DeletedAt    sql.NullString

	AuthorNickname sql.NullString // 联表
	AuthorAvatar   sql.NullString // 联表：users.avatar_path（相对数据目录，空=无头像）
}

// 媒体归属实体的类型（media.owner_type）。新增可挂图的实体时在这里加一个取值，
// 并同步 migrations 里 media 表的 CHECK 约束（SQLite 改 CHECK 同样要重建表）。
const (
	MediaOwnerPost    = "post"
	MediaOwnerComment = "comment"
)

type MediaRow struct {
	ID int64
	// OwnerType / OwnerID 媒体归属：'post' → 动态自己的图，'comment' → 评论里附的图。
	// 两列覆盖任意实体，不必随实体数量加列。
	OwnerType string
	OwnerID   int64
	Kind      string
	FilePath  string
	ThumbPath sql.NullString
	Width     int
	Height    int
	Size      int64
	Duration  sql.NullFloat64
	TakenAt   sql.NullString
	CreatedAt string
	// PostID 媒体经归属链指向的动态（评论图 → 所属评论 → 动态），供「按动态聚合」的读路径用。
	// 不是表列，由查询 JOIN 算出。
	PostID   int64
	AuthorID int64 // 归属实体的作者：动态图取动态作者，评论图取评论作者
}

// SortTime 时间轴排序键：taken_at 优先，回退 created_at。
func (m *MediaRow) SortTime() string {
	if m.TakenAt.Valid && m.TakenAt.String != "" {
		return m.TakenAt.String
	}
	return m.CreatedAt
}

// NullStr 构造可空字符串字段（供 API 与 AI 工具层使用）。
func NullStr(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// mediaSelect 媒体行的统一 SELECT 片段（列序须与 scanMediaRow 一致）。
//
// 归属是 (owner_type, owner_id) 两列；无论归属动态还是评论，都要能解出
// 「最终所属动态」与「作者」，供按动态聚合的读路径与权限判断使用：
//   - post   → 动态就是自己，作者是动态作者
//   - comment → 经 comments.post_id 上溯到动态，作者是评论作者
//
// p 一律指向**最终所属动态**（评论图取评论的动态），所以 p.deleted_at 对两类媒体
// 都表示「所属动态是否已删」——时间轴的「所属实体未删」判断因此可以统一写。
const mediaSelect = `SELECT m.id, m.owner_type, m.owner_id, m.kind, m.file_path, m.thumb_path, m.width, m.height, m.size,
		m.duration, m.taken_at, m.created_at,
		COALESCE(c.post_id, m.owner_id) AS post_id,
		COALESCE(c.author_id, p.author_id) AS author_id
	FROM media m
	LEFT JOIN comments c ON m.owner_type = 'comment' AND c.id = m.owner_id
	LEFT JOIN posts p ON p.id = CASE m.owner_type WHEN 'comment' THEN c.post_id ELSE m.owner_id END`

// mediaOwnedBy 拼「归属某类实体」的媒体条件。
// 不带表别名：`FROM media m` 的查询里 media 也可用裸列名，而 UPDATE/DELETE 语境没有别名。
func mediaOwnedBy(ownerType, ownerExpr string) string {
	return `owner_type = '` + ownerType + `' AND owner_id = ` + ownerExpr
}

// indexPost / unindexPost 转发索引钩子。索引是派生数据：
// 失败只告警，绝不反向阻断动态落库（未配置嵌入模型时钩子为空）。
func (d *DB) indexPost(ctx context.Context, id int64, content, location string) {
	if d.postIndex == nil {
		return
	}
	if err := d.postIndex.IndexPost(ctx, id, content, location); err != nil {
		slog.Warn("index post failed", "post_id", id, "err", err)
	}
}

func (d *DB) unindexPost(ctx context.Context, id int64) {
	if d.postIndex == nil {
		return
	}
	if err := d.postIndex.UnindexPost(ctx, id); err != nil {
		slog.Warn("unindex post failed", "post_id", id, "err", err)
	}
}

// CreatePostWithMedia 单事务创建动态与媒体行（媒体文件已由管线转正，失败由调用方回收）。
func (d *DB) CreatePostWithMedia(ctx context.Context, p *Post, medias []MediaRow) (int64, error) {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx,
		`INSERT INTO posts (author_id, content, mood, location_name, point_id) VALUES (?, ?, ?, ?, ?)`,
		p.AuthorID, p.Content, p.Mood, p.LocationName, p.PointID)
	if err != nil {
		return 0, err
	}
	postID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	for _, m := range medias {
		if err := insertMediaTx(ctx, tx, MediaOwnerPost, postID, m); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	d.indexPost(ctx, postID, p.Content, p.LocationName.String)
	return postID, nil
}

// insertMediaTx 写一行媒体并把归属绑到 (ownerType, ownerID)。
// 动态图与评论图走同一条写入路径，只是归属参数不同。
func insertMediaTx(ctx context.Context, tx *sql.Tx, ownerType string, ownerID int64, m MediaRow) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO media (owner_type, owner_id, kind, file_path, thumb_path, width, height, size, duration, taken_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ownerType, ownerID, m.Kind, m.FilePath, m.ThumbPath, m.Width, m.Height, m.Size, m.Duration, m.TakenAt)
	return err
}

const postColumns = `p.id, p.author_id, p.content, p.mood, p.location_name, p.point_id, p.created_at, p.deleted_at, COALESCE(NULLIF(u.nickname, ''), u.username), u.avatar_path`

func scanPost(row interface{ Scan(...any) error }) (*Post, error) {
	var p Post
	err := row.Scan(&p.ID, &p.AuthorID, &p.Content, &p.Mood, &p.LocationName, &p.PointID, &p.CreatedAt, &p.DeletedAt, &p.AuthorNickname, &p.AuthorAvatar)
	return &p, err
}

// ListPosts 分页倒序；deleted=show 时含已删（后台回收站用，默认过滤）。
// memories=true 时只取「那年今日」——历年在今天同月同日发布的动态（**不含当年**，
// 「同月同日历史回忆」）；过滤发生在分页之前，total 为过滤后总数。
// 月/年判定按部署者本地时区（库内 created_at 为 UTC，见 timeutil.go 时间口径）。
func (d *DB) ListPosts(ctx context.Context, page, pageSize int, memories bool) ([]*Post, int, error) {
	return d.ListPostsSampled(ctx, page, pageSize, memories, false, nil)
}

// ListPostsSampled 同 ListPosts，另有两点可选：
//   - sample=true：在**全量候选**里随机取（而非取最新一页）。首页「回忆精选」用它——
//     从**历史照片 / 动态池**随机抽取，只取最新 50 条会让更早的历史永远抽不到（池子随记录增长而失真）。
//   - hasMedia：只看带媒体 / 不带媒体的动态。首页要用它**分层抽样**：封面卡取「有媒体」那层、
//     文字回忆取「无媒体」那层——否则随机样本可能整批没有照片，封面卡就会时有时无
//     （回忆精选是首页的**常驻兜底卡**）。
//
// 随机在 SQL 侧做，避免把全量拉到内存再洗牌。
func (d *DB) ListPostsSampled(ctx context.Context, page, pageSize int, memories, sample bool, hasMedia *bool) ([]*Post, int, error) {
	// 两个查询共用 WHERE，故一律用 p. 限定列名（users 也有 created_at，不限定会 ambiguous）
	where := ` WHERE p.deleted_at IS NULL`
	if memories {
		where += ` AND strftime('%m-%d', p.created_at, 'localtime') = strftime('%m-%d', 'now', 'localtime')` +
			` AND strftime('%Y', p.created_at, 'localtime') < strftime('%Y', 'now', 'localtime')`
	}
	if hasMedia != nil {
		// 只算动态自己的图：评论里的图不属于「这条动态带媒体」
		sub := `EXISTS (SELECT 1 FROM media m WHERE ` + mediaOwnedBy(MediaOwnerPost, "p.id") + ` AND m.deleted_at IS NULL)`
		// mediaOwnedBy 不带别名，这里外层有 p，须避免 owner_id 落到 p 上——p 没有 owner_id 列，故无歧义；
		// 但 deleted_at 两表都有，故上面显式写 m.deleted_at。
		if !*hasMedia {
			sub = `NOT ` + sub
		}
		where += ` AND ` + sub
	}
	var total int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM posts p`+where).Scan(&total); err != nil {
		return nil, 0, err
	}
	// 排序按**内容时间**而非 id：id 只代表写入顺序，与内容时间不必同序——回填历史时间，
	// 或将来支持补记往事时，按 id 排会把刚导入的内容顶到时间轴最前（动态按时间倒序）。
	// id 作次序键只为同秒内稳定，不参与「谁更新」的判断。
	// 随机抽样：ORDER BY RANDOM() 在候选集上取前 pageSize 条（单空间自用规模，代价可接受）。
	// 抽样路径下 page 无意义（每次都重抽），故固定 offset=0，响应也回 page=1，不假装能翻页。
	order, offset := ` ORDER BY p.created_at DESC, p.id DESC LIMIT ? OFFSET ?`, (page-1)*pageSize
	if sample {
		order, offset = ` ORDER BY RANDOM() LIMIT ? OFFSET ?`, 0
	}
	rows, err := d.QueryContext(ctx, `
		SELECT `+postColumns+` FROM posts p JOIN users u ON u.id = p.author_id`+where+order, pageSize, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var posts []*Post
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, 0, err
		}
		posts = append(posts, p)
	}
	return posts, total, rows.Err()
}

// PostsByIDs 取未删动态，返回顺序与入参一致（供语义检索还原相似度序）；
// 已删或缺失的 id 直接跳过。
func (d *DB) PostsByIDs(ctx context.Context, ids []int64) ([]*Post, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := d.QueryContext(ctx, `
		SELECT `+postColumns+` FROM posts p JOIN users u ON u.id = p.author_id
		WHERE p.deleted_at IS NULL AND p.id IN (`+placeholders(len(ids))+`)`, int64Args(ids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := make(map[int64]*Post, len(ids))
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		byID[p.ID] = p
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	posts := make([]*Post, 0, len(byID))
	for _, id := range ids {
		if p, ok := byID[id]; ok {
			posts = append(posts, p)
		}
	}
	return posts, nil
}

func (d *DB) GetPost(ctx context.Context, id int64) (*Post, error) {
	row := d.QueryRowContext(ctx, `
		SELECT `+postColumns+` FROM posts p JOIN users u ON u.id = p.author_id
		WHERE p.id = ? AND p.deleted_at IS NULL`, id)
	p, err := scanPost(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

// SoftDeletePost 软删除动态并级联其媒体（媒体只能随动态存在， 口径）；
// actorType ∈ ('user','ai')、actorID 为发起人，供回收站「删除者」列。
func (d *DB) SoftDeletePost(ctx context.Context, id int64, actorType string, actorID int64) error {
	return d.softDeleteKindTx(ctx, "post", id, actorType, actorID)
}

// GetMediaByPostIDs 动态**自己的**媒体（九宫格）；评论里附的图不在此列。
func (d *DB) GetMediaByPostIDs(ctx context.Context, postIDs []int64) (map[int64][]*MediaRow, error) {
	out := map[int64][]*MediaRow{}
	if len(postIDs) == 0 {
		return out, nil
	}
	ops := placeholders(len(postIDs))
	args := int64Args(postIDs)
	rows, err := d.QueryContext(ctx, mediaSelect+`
		WHERE `+mediaOwnedBy(MediaOwnerPost, "m.owner_id")+
		` AND m.owner_id IN (`+ops+`) AND m.deleted_at IS NULL ORDER BY m.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		m, err := scanMediaRow(rows)
		if err != nil {
			return nil, err
		}
		out[m.PostID] = append(out[m.PostID], m)
	}
	return out, rows.Err()
}

// ListMediaByCommentIDs 批量取评论附带的图片（评论列表回显用）。
// 只取归属 comment 的媒体；动态自己的图不会混进来。
func (d *DB) ListMediaByCommentIDs(ctx context.Context, commentIDs []int64) (map[int64][]*MediaRow, error) {
	out := map[int64][]*MediaRow{}
	if len(commentIDs) == 0 {
		return out, nil
	}
	ph := placeholders(len(commentIDs))
	rows, err := d.QueryContext(ctx, mediaSelect+`
		WHERE `+mediaOwnedBy(MediaOwnerComment, "m.owner_id")+
		` AND m.owner_id IN (`+ph+`) AND m.deleted_at IS NULL ORDER BY m.id`, int64Args(commentIDs)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		m, err := scanMediaRow(rows)
		if err != nil {
			return nil, err
		}
		out[m.OwnerID] = append(out[m.OwnerID], m)
	}
	return out, rows.Err()
}

// scanMediaRow 统一行扫描（列序见 mediaSelect：id/owner_type/owner_id/kind/file_path/
// thumb_path/width/height/size/duration/taken_at/created_at/post_id/author_id）。
func scanMediaRow(row interface{ Scan(...any) error }) (*MediaRow, error) {
	var m MediaRow
	err := row.Scan(&m.ID, &m.OwnerType, &m.OwnerID, &m.Kind, &m.FilePath, &m.ThumbPath, &m.Width, &m.Height, &m.Size,
		&m.Duration, &m.TakenAt, &m.CreatedAt, &m.PostID, &m.AuthorID)
	return &m, err
}

// GetMediaByIDs 批量取媒体（reurl）：仅未删除的命中项，入参无效 id 自然跳过。
func (d *DB) GetMediaByIDs(ctx context.Context, ids []int64) ([]*MediaRow, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	ph := placeholders(len(ids))
	args := int64Args(ids)
	rows, err := d.QueryContext(ctx, mediaSelect+` WHERE m.id IN (`+ph+`) AND m.deleted_at IS NULL ORDER BY m.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*MediaRow
	for rows.Next() {
		m, err := scanMediaRow(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, m)
	}
	return list, rows.Err()
}

func (d *DB) GetMedia(ctx context.Context, id int64) (*MediaRow, error) {
	row := d.QueryRowContext(ctx, mediaSelect+` WHERE m.id = ? AND m.deleted_at IS NULL`, id)
	m, err := scanMediaRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return m, nil
}

// ListPostsOnMonthDay 那年今日（get_today_in_history）：历年在今天同月同日创建的动态，
// yearsBack 限制回看年数上限，按时间倒序。
// 月/年判定按**部署者本地时区**（库内 created_at 为 UTC，见 timeutil.go 时间口径）。
func (d *DB) ListPostsOnMonthDay(ctx context.Context, yearsBack int, limit int) ([]*Post, int, error) {
	if yearsBack <= 0 {
		yearsBack = 50
	}
	// 同一 WHERE 下推计数：列表卡的 total/more 要用真值，不能拿「命中数 == limit」猜
	// （真实余量 total - len(records)，未知则两者都省略）。
	const where = ` FROM posts p
		WHERE p.deleted_at IS NULL
		  AND strftime('%m-%d', p.created_at, 'localtime') = strftime('%m-%d', 'now', 'localtime')
		  AND strftime('%Y', p.created_at, 'localtime') < strftime('%Y', 'now', 'localtime')
		  AND p.created_at >= datetime('now', ?)`
	since := fmt.Sprintf("-%d years", yearsBack)
	var total int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*)`+where, since).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := d.QueryContext(ctx, `SELECT `+postColumns+`
		FROM posts p JOIN users u ON u.id = p.author_id
		WHERE p.deleted_at IS NULL
		  AND strftime('%m-%d', p.created_at, 'localtime') = strftime('%m-%d', 'now', 'localtime')
		  AND strftime('%Y', p.created_at, 'localtime') < strftime('%Y', 'now', 'localtime')
		  AND p.created_at >= datetime('now', ?)
		ORDER BY p.created_at DESC LIMIT ?`, since, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var list []*Post
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, 0, err
		}
		list = append(list, p)
	}
	return list, total, rows.Err()
}

// ListPostsByPoint 某地点下的动态（get_location_posts）：动态带地点时写入的 point_id 反查。
func (d *DB) ListPostsByPoint(ctx context.Context, pointID int64, limit int) ([]*Post, error) {
	rows, err := d.QueryContext(ctx, `SELECT `+postColumns+`
		FROM posts p JOIN users u ON u.id = p.author_id
		WHERE p.deleted_at IS NULL AND p.point_id = ?
		ORDER BY p.created_at DESC, p.id DESC LIMIT ?`, pointID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*Post
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	return list, rows.Err()
}

// TimelinePostCounts 时间线统计（get_timeline_summary）：无参按年分布；
// year 非空按该年月度分布；month(YYYY-MM) 非空按该月日度分布。
func (d *DB) TimelinePostCounts(ctx context.Context, year, month string) ([]PeriodCount, error) {
	// 已软删动态不进统计（统计概览卡的数字必须准且即时；
	// 与同工具 postTotal 的 deleted_at IS NULL 口径一致，否则同卡自相矛盾）。
	group := "strftime('%Y', created_at)"
	where := " WHERE deleted_at IS NULL"
	args := []any{}
	switch {
	case month != "":
		group = "strftime('%Y-%m-%d', created_at)"
		where = " WHERE deleted_at IS NULL AND strftime('%Y-%m', created_at) = ?"
		args = append(args, month)
	case year != "":
		group = "strftime('%Y-%m', created_at)"
		where = " WHERE deleted_at IS NULL AND strftime('%Y', created_at) = ?"
		args = append(args, year)
	}
	rows, err := d.QueryContext(ctx, `SELECT `+group+` AS period, COUNT(*) FROM posts`+where+`
		GROUP BY period ORDER BY period`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []PeriodCount
	for rows.Next() {
		var pc PeriodCount
		if err := rows.Scan(&pc.Period, &pc.Count); err != nil {
			return nil, err
		}
		list = append(list, pc)
	}
	return list, rows.Err()
}

// PeriodCount 时间段聚合行（get_timeline_summary 输出）。
type PeriodCount struct {
	Period string
	Count  int64
}

// SoftDeleteMedia 单独软删一条媒体（动态自己的图或评论里的图）。
//
// 删完若归属实体失去全部内容，同一事务内把实体一并软删（用户需求：空壳不留）：
//   - 动态：无任何未删媒体且正文为空 → 动态进回收站，postGone=true
//   - 评论：无任何未删图片且正文为空 → 评论进回收站，commentGone=true
//
// 返回值里的 postID 恒为媒体最终所属的动态（评论图会经所属评论上溯），
// 供客户端定位「该刷新哪条动态的评论列表」。
func (d *DB) SoftDeleteMedia(ctx context.Context, mediaID int64, actorType string, actorID int64) (postID int64, postGone bool, commentGone bool, err error) {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, false, err
	}
	defer func() { _ = tx.Rollback() }()

	// 一次取全：媒体归属 + 归属实体（动态或评论）的正文与所属动态。
	// 两条归属链在这里归一，后续分支只差「空壳判定作用在哪张表」。
	var ownerType string
	var ownerID, cPostID int64
	var postContent, commentContent string
	err = tx.QueryRowContext(ctx, `
		SELECT m.owner_type, m.owner_id,
		       COALESCE(c.post_id, 0), COALESCE(p.content, ''), COALESCE(c.content, '')
		FROM media m
		LEFT JOIN comments c ON m.owner_type = 'comment' AND c.id = m.owner_id
		LEFT JOIN posts p ON m.owner_type = 'post' AND p.id = m.owner_id
		WHERE m.id = ? AND m.deleted_at IS NULL
		  AND (CASE m.owner_type
		        WHEN 'comment' THEN c.deleted_at IS NULL
		        ELSE p.deleted_at IS NULL END)`, mediaID).
		Scan(&ownerType, &ownerID, &cPostID, &postContent, &commentContent)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, false, ErrNotFound
	}
	if err != nil {
		return 0, false, false, err
	}
	if ownerType == MediaOwnerComment {
		postID = cPostID
	} else {
		postID = ownerID
	}

	res, err := tx.ExecContext(ctx,
		`UPDATE media SET deleted_at = datetime('now'), deleted_by = ?, deleted_by_type = ? WHERE id = ? AND deleted_at IS NULL`,
		actorID, actorType, mediaID)
	if err != nil {
		return 0, false, false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, false, false, ErrNotFound
	}

	// 空壳判定：只数同一归属实体下剩余的媒体。
	var remain int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM media WHERE `+mediaOwnedBy(ownerType, "?")+` AND deleted_at IS NULL`, ownerID).Scan(&remain); err != nil {
		return 0, false, false, err
	}
	if remain > 0 {
		if err := tx.Commit(); err != nil {
			return 0, false, false, err
		}
		return postID, false, false, nil
	}

	switch ownerType {
	case MediaOwnerComment:
		// 评论既无图又无正文 → 评论进回收站（其图已在此处全部软删，无需再级联）。
		if strings.TrimSpace(commentContent) == "" {
			if _, err := tx.ExecContext(ctx,
				`UPDATE comments SET deleted_at = datetime('now'), deleted_by = ?, deleted_by_type = ? WHERE id = ? AND deleted_at IS NULL`,
				actorID, actorType, ownerID); err != nil {
				return 0, false, false, err
			}
			commentGone = true
		}
	default:
		// 动态失去全部内容 → 动态一并软删。
		if strings.TrimSpace(postContent) == "" {
			if _, err := tx.ExecContext(ctx,
				`UPDATE posts SET deleted_at = datetime('now'), deleted_by = ?, deleted_by_type = ? WHERE id = ? AND deleted_at IS NULL`,
				actorID, actorType, postID); err != nil {
				return 0, false, false, err
			}
			postGone = true
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, false, false, err
	}
	if postGone {
		d.unindexPost(ctx, postID)
	}
	return postID, postGone, commentGone, nil
}
