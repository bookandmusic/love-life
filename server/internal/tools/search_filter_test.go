package tools

import (
	"testing"

	"github.com/bookandmusic/love-life/server/internal/store"
)

// seedPostAt 造一条指定创建时间（库内 UTC 形如 2006-01-02 15:04:05）的动态。
func seedPostAt(t *testing.T, db *store.DB, content, createdAt string) int64 {
	t.Helper()
	id, err := db.CreatePostWithMedia(t.Context(), &store.Post{AuthorID: 1, Content: content}, nil)
	if err != nil {
		t.Fatalf("seed post: %v", err)
	}
	if _, err := db.Exec("UPDATE posts SET created_at = ? WHERE id = ?", createdAt, id); err != nil {
		t.Fatalf("set created_at: %v", err)
	}
	return id
}

// seedPostMediaKind 给动态挂一条指定类型的媒体。
func seedPostMediaKind(t *testing.T, db *store.DB, postID int64, kind string) {
	t.Helper()
	if _, err := db.Exec("INSERT INTO media (post_id, kind, file_path) VALUES (?, ?, ?)", postID, kind, "media/x."+kind); err != nil {
		t.Fatalf("seed media: %v", err)
	}
}

// idsOf 提取命中 id（断言用）。
func idsOf(hits []PostHit) []int64 {
	ids := make([]int64, 0, len(hits))
	for _, h := range hits {
		ids = append(ids, h.ID)
	}
	return ids
}

// parseDateRange 口径：闭区间、日期形如 YYYY-MM-DD；空串表示不限，单边缺省表示该侧不限。
func TestParseDateRange(t *testing.T) {
	cases := []struct {
		name             string
		in               string
		wantOK           bool
		wantFrom, wantTo string
	}{
		{"空串 = 不限", "", true, "", ""},
		{"双边", "2026-01-01~2026-03-31", true, "2026-01-01", "2026-03-31"},
		{"单边起始", "2026-01-01~", true, "2026-01-01", ""},
		{"单边结束", "~2026-03-31", true, "", "2026-03-31"},
		{"格式错误", "2026/01/01~2026-03-31", false, "", ""},
		{"起始晚于结束", "2026-03-31~2026-01-01", false, "", ""},
		{"三段", "2026-01-01~2026-02-01~2026-03-01", false, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			from, to, err := parseDateRange(c.in)
			if c.wantOK != (err == nil) {
				t.Fatalf("parseDateRange(%q) err = %v, wantOK %v", c.in, err, c.wantOK)
			}
			if !c.wantOK {
				return
			}
			if gotFrom, gotTo := dateOnly(from), dateOnly(to); gotFrom != c.wantFrom || gotTo != c.wantTo {
				t.Fatalf("got %s~%s, want %s~%s", gotFrom, gotTo, c.wantFrom, c.wantTo)
			}
		})
	}
}

// 关键词路径：日期区间（闭区间，含端点）与媒体类型都要生效；非法参数报错而非静默忽略。
func TestSearchPostsKeywordFilters(t *testing.T) {
	db := newToolDB(t)
	ctx := t.Context()
	in1 := seedPostAt(t, db, "海边散步", "2026-01-15 10:00:00")
	in2 := seedPostAt(t, db, "海边看日出", "2026-02-15 10:00:00")
	seedPostAt(t, db, "海边野餐", "2026-03-15 10:00:00")
	seedPostMediaKind(t, db, in1, "photo")
	seedPostMediaKind(t, db, in2, "video")

	tool := findTool(t, "search_posts")

	res, err := tool.Exec(ctx, &Deps{DB: db}, 1, map[string]any{"query": "海边", "date_range": "2026-01-15~2026-02-15"})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if got := idsOf(res.Data.([]PostHit)); len(got) != 2 {
		t.Fatalf("闭区间应命中两端: %v", got)
	}

	res, err = tool.Exec(ctx, &Deps{DB: db}, 1, map[string]any{"query": "海边", "date_range": "2026-02-01~"})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if got := idsOf(res.Data.([]PostHit)); len(got) != 2 {
		t.Fatalf("单边起始应命中 2/3 月: %v", got)
	}

	res, err = tool.Exec(ctx, &Deps{DB: db}, 1, map[string]any{"query": "海边", "media_type": "photo"})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	hits := res.Data.([]PostHit)
	if len(hits) != 1 || hits[0].ID != in1 {
		t.Fatalf("photo 过滤应只命中 in1: %+v", hits)
	}

	res, err = tool.Exec(ctx, &Deps{DB: db}, 1, map[string]any{"query": "海边", "date_range": "2026-02-01~2026-02-28", "media_type": "photo"})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if got := res.Data.([]PostHit); len(got) != 0 {
		t.Fatalf("组合条件应无命中: %+v", got)
	}

	if _, err := tool.Exec(ctx, &Deps{DB: db}, 1, map[string]any{"query": "海边", "date_range": "2026/01/01~2026/02/01"}); err == nil {
		t.Fatal("非法 date_range 应报错")
	}
	if _, err := tool.Exec(ctx, &Deps{DB: db}, 1, map[string]any{"query": "海边", "media_type": "doc"}); err == nil {
		t.Fatal("非法 media_type 应报错")
	}
}

// 语义路径：按相似度序回库后过滤；召回不足时扩大取数以尽量补足。
func TestSearchPostsSemanticFilters(t *testing.T) {
	db := newToolDB(t)
	ctx := t.Context()
	in1 := seedPostAt(t, db, "海边散步", "2026-01-15 10:00:00")
	in2 := seedPostAt(t, db, "海边看日出", "2026-02-15 10:00:00")
	out := seedPostAt(t, db, "海边野餐", "2026-03-15 10:00:00")
	seedPostMediaKind(t, db, in1, "photo")
	seedPostMediaKind(t, db, out, "video")

	// 相似度序：out 最前，过滤器须剔除它并保留 in1/in2
	fake := &fakeSearch{enabled: true, ids: []int64{out, in1, in2}}
	res, err := findTool(t, "search_posts").Exec(ctx, &Deps{DB: db, Search: fake}, 1, map[string]any{"query": "海边", "media_type": "photo"})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	hits := res.Data.([]PostHit)
	if len(hits) != 1 || hits[0].ID != in1 {
		t.Fatalf("语义路径 photo 过滤应只留 in1: %+v", hits)
	}
	if fake.gotK <= searchPostLimit {
		t.Fatalf("带过滤时语义召回应扩大（gotK=%d <= %d）", fake.gotK, searchPostLimit)
	}
}

// 语义路径的日期下界过滤必须生效（关键词路径走 SQL，覆盖不到 Go 侧 matchDate）。
// 构造：向量命中含 1 月与 3 月两条；条件「起始 2 月」应滤掉 1 月。
// 若下界过滤失效，1 月那条会留下（结果条数 2），从而暴露。
func TestSearchPostsSemanticDateFromBound(t *testing.T) {
	db := newToolDB(t)
	jan := seedPostAt(t, db, "海边散步", "2026-01-15 10:00:00")
	mar := seedPostAt(t, db, "海边野餐", "2026-03-15 10:00:00")

	// 相似度序把 jan 放前面，确保它若未被滤掉就会出现在结果里
	fake := &fakeSearch{enabled: true, ids: []int64{jan, mar}}
	res, err := findTool(t, "search_posts").Exec(t.Context(), &Deps{DB: db, Search: fake}, 1, map[string]any{"query": "海边", "date_range": "2026-02-01~"})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	hits := res.Data.([]PostHit)
	if len(hits) != 1 || hits[0].ID != mar {
		t.Fatalf("起始日期过滤应只留 mar(%d)，若下界失效会多出 jan(%d): %+v", mar, jan, hits)
	}
}

// 语义路径过滤后为空是合法结果，不得回落关键词。
// 构造：向量只命中 3 月动态，而关键词能查到 1 月动态（同含「海边」）。
// 过滤条件（截止 2 月）把向量命中全部滤掉 → 正确行为是返回空；
// 若误回落关键词，会带回 1 月那条（关键词路径的日期过滤同样生效，能查到）。
func TestSearchPostsSemanticEmptyAfterFilter(t *testing.T) {
	db := newToolDB(t)
	jan := seedPostAt(t, db, "海边散步", "2026-01-15 10:00:00")
	mar := seedPostAt(t, db, "海边野餐", "2026-03-15 10:00:00")

	fake := &fakeSearch{enabled: true, ids: []int64{mar}}
	res, err := findTool(t, "search_posts").Exec(t.Context(), &Deps{DB: db, Search: fake}, 1, map[string]any{"query": "海边", "date_range": "~2026-02-28"})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	hits := res.Data.([]PostHit)
	if len(hits) != 0 {
		t.Fatalf("过滤后应为空（若回落关键词会带回 jan=%d）: %+v", jan, hits)
	}
	if fake.gotK <= searchPostLimit {
		t.Fatalf("带过滤应扩大召回，gotK=%d", fake.gotK)
	}
}
