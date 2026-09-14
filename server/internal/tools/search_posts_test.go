package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/store"
)

// fakeSearch 语义检索桩：分别控制「是否配置」与「查询结果」，并记录透传参数。
type fakeSearch struct {
	enabled bool
	ids     []int64
	err     error
	gotQ    string
	gotK    int
}

func (f *fakeSearch) Enabled(context.Context) bool { return f.enabled }

func (f *fakeSearch) Query(_ context.Context, text string, k int) ([]int64, error) {
	f.gotQ, f.gotK = text, k
	return f.ids, f.err
}

func seedPost(t *testing.T, db *store.DB, content string) int64 {
	t.Helper()
	id, err := db.CreatePostWithMedia(t.Context(), &store.Post{AuthorID: 1, Content: content}, nil)
	if err != nil {
		t.Fatalf("seed post: %v", err)
	}
	return id
}

// 已配置嵌入模型：走向量检索，结果顺序尊重相似度序（而非 id 倒序），并过滤已删动态。
func TestSearchPostsSemantic(t *testing.T) {
	db := newToolDB(t)
	ctx := t.Context()
	p1 := seedPost(t, db, "在家烤了蛋糕")
	p2 := seedPost(t, db, "海边散步")
	p3 := seedPost(t, db, "去海边看日落")

	// 相似度序：p3 最像，其次 p1（同一条查询在向量库里的排序与 id 无关）。
	d := &Deps{DB: db, Search: &fakeSearch{enabled: true, ids: []int64{p3, p1}}}
	res, err := findTool(t, "search_posts").Exec(ctx, d, 1, map[string]any{"query": "海"})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	fake := d.Search.(*fakeSearch)
	if fake.gotQ != "海" || fake.gotK != 10 {
		t.Fatalf("query params = %q k=%d, want 海 / 10", fake.gotQ, fake.gotK)
	}
	hits, ok := res.Data.([]PostHit)
	if !ok {
		t.Fatalf("data type = %T, want []PostHit", res.Data)
	}
	if len(hits) != 2 || hits[0].ID != p3 || hits[1].ID != p1 {
		t.Fatalf("hits = %+v, want [%d %d] in similarity order", hits, p3, p1)
	}

	// 软删一条命中的动态后，回库取数须把它过滤掉（向量库可能还没同步）。
	if err := db.SoftDeletePost(ctx, p3, store.ActorTypeUser, 1); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	res, err = findTool(t, "search_posts").Exec(ctx, d, 1, map[string]any{"query": "海"})
	if err != nil {
		t.Fatalf("exec after delete: %v", err)
	}
	hits = res.Data.([]PostHit)
	if len(hits) != 1 || hits[0].ID != p1 {
		t.Fatalf("hits after delete = %+v, want only %d", hits, p1)
	}

	// p2 只在关键词匹配里（正文含「海边」），不在向量命中里，不应出现
	for _, h := range hits {
		if h.ID == p2 {
			t.Fatalf("p2 不在向量命中里，不应出现: %+v", hits)
		}
	}
}

// 未配置 / 上游失败 / 无命中：回落关键词匹配，工具不得因此报错。
func TestSearchPostsFallback(t *testing.T) {
	db := newToolDB(t)
	ctx := t.Context()
	p1 := seedPost(t, db, "海边散步")
	seedPost(t, db, "在家烤了蛋糕")

	cases := []struct {
		name   string
		search SearchIndex
	}{
		{"未配置嵌入模型", nil},
		{"未启用", &fakeSearch{enabled: false}},
		{"上游失败", &fakeSearch{enabled: true, err: errors.New("upstream down")}},
		{"无命中", &fakeSearch{enabled: true, ids: nil}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := &Deps{DB: db, Search: c.search}
			res, err := findTool(t, "search_posts").Exec(ctx, d, 1, map[string]any{"query": "海边"})
			if err != nil {
				t.Fatalf("exec: %v", err)
			}
			hits, ok := res.Data.([]PostHit)
			if !ok {
				t.Fatalf("data type = %T, want []PostHit", res.Data)
			}
			if len(hits) != 1 || hits[0].ID != p1 {
				t.Fatalf("hits = %+v, want keyword fallback hitting %d", hits, p1)
			}
		})
	}
}
