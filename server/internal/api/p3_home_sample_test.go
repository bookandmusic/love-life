package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/bookandmusic/love-life/server/internal/store"
)

// P3-10：首页「回忆精选」必须在**全量历史池**里随机取，而不是「最新 50 条」。
// 守卫两层：① sample=1 时同一空间多次调用返回的集合不完全相同（确有随机）；
// ② 更早的历史（超出最新 50 条窗口）能被抽到——只洗牌最新 50 条时这一条永远不成立。
func TestPostsSampleDrawsFromWholeHistory(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	ctx := t.Context()

	// 造 60 条：id 越大越新。第 1 条是最旧的，落在「最新 50 条」窗口之外。
	const total = 60
	for i := 0; i < total; i++ {
		if _, err := ts.db.ExecContext(ctx,
			`INSERT INTO posts (author_id, content, created_at) VALUES (1, ?, datetime('now', ?))`,
			fmt.Sprintf("回忆 %02d", i), fmt.Sprintf("-%d days", total-i)); err != nil {
			t.Fatal(err)
		}
	}

	fetch := func(size int) []int64 {
		res := doJSON(t, "GET", ts.URL+fmt.Sprintf("/api/v1/posts?sample=1&page=1&page_size=%d", size), adminToken, nil)
		if res.StatusCode != http.StatusOK {
			res.Body.Close()
			t.Fatalf("sample 列表 status=%d", res.StatusCode)
		}
		var out struct {
			Items []struct {
				ID int64 `json:"id"`
			} `json:"items"`
		}
		_ = json.NewDecoder(res.Body).Decode(&out)
		res.Body.Close()
		ids := make([]int64, 0, len(out.Items))
		for _, it := range out.Items {
			ids = append(ids, it.ID)
		}
		return ids
	}

	// ① 随机生效：两次抽样不应完全相同（同一顺序重复出现的概率可忽略）
	first := fetch(10)
	sawDifferent := false
	for i := 0; i < 3 && !sawDifferent; i++ {
		if fmt.Sprint(fetch(10)) != fmt.Sprint(first) {
			sawDifferent = true
		}
	}
	if !sawDifferent {
		t.Fatalf("sample=1 连续多次返回了完全相同的集合 %v（未随机）", first)
	}

	// ② 抽样范围是**全量历史池**，而非「最新一页」。
	// 断言「抽到的 50 条里至少有 1 条落在『最新 50 条』窗口之外」：
	// 若实现只在窗口内取，抽出的必然**恰好**是最新 50 条（1 种组合），
	// 而全池 60 取 50 共 C(60,50) ≈ 7.5e10 种组合——误报概率约 1.3e-11，
	// 远低于「断言某一条必被抽中」（那条只有 50/60，会 17% 概率随机红）。
	latest := fetch(50) // 随机抽一批
	if len(latest) != 50 {
		t.Fatalf("应返回 50 条，实得 %d", len(latest))
	}
	// 默认（非抽样）列表就是「最新 50 条」窗口，用它作为对照集
	res := doJSON(t, "GET", ts.URL+"/api/v1/posts?page=1&page_size=50", adminToken, nil)
	var def struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	_ = json.NewDecoder(res.Body).Decode(&def)
	res.Body.Close()
	window := make(map[int64]bool, len(def.Items))
	for _, it := range def.Items {
		window[it.ID] = true
	}
	if len(window) != 50 {
		t.Fatalf("对照窗口应 50 条，实得 %d", len(window))
	}
	outside := 0
	for _, id := range latest {
		if !window[id] {
			outside++
		}
	}
	if outside == 0 {
		t.Fatal("抽到的全部落在「最新 50 条」窗口内——抽样被限制在最新一页")
	}

	// 不给 sample 时仍是最新优先（默认行为不被改坏）：上面那次非抽样请求即为对照
	for i := 1; i < len(def.Items); i++ {
		if def.Items[i].ID > def.Items[i-1].ID {
			t.Fatalf("默认列表应按 id 倒序，实得 %+v", def.Items)
		}
	}
}

// P3-10 分层抽样：封面卡取「有媒体」那层、文字回忆取「无媒体」那层。
// 不分层时，随机样本可能整批没有照片 → 封面卡时有时无，而文档要求它是**常驻兜底卡**。
// 守卫：两层各自只返回本层内容；即使有媒体的动态远少于无媒体的，media=1 也必然命中。
func TestPostsSampleCanStratifyByMedia(t *testing.T) {
	ts, adminToken, _ := newPostTestEnv(t)
	ctx := t.Context()

	// 1 条带媒体 + 40 条纯文字：有媒体占 1/41，不分层时随机 20 条命中它的概率很低
	postID, err := ts.db.CreatePostWithMedia(ctx, &store.Post{AuthorID: 1, Content: "有图的一条"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ts.db.ExecContext(ctx,
		`INSERT INTO media (post_id, kind, file_path, thumb_path) VALUES (?, 'photo', 'media/a.jpg', 'media/a_th.jpg')`, postID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		if _, err := ts.db.ExecContext(ctx,
			`INSERT INTO posts (author_id, content) VALUES (1, ?)`, fmt.Sprintf("纯文字 %02d", i)); err != nil {
			t.Fatal(err)
		}
	}

	fetch := func(query string) []struct {
		ID    int64 `json:"id"`
		Media []struct {
			ID int64 `json:"id"`
		} `json:"media"`
	} {
		res := doJSON(t, "GET", ts.URL+"/api/v1/posts?"+query, adminToken, nil)
		if res.StatusCode != http.StatusOK {
			res.Body.Close()
			t.Fatalf("%s status=%d", query, res.StatusCode)
		}
		var out struct {
			Items []struct {
				ID    int64 `json:"id"`
				Media []struct {
					ID int64 `json:"id"`
				} `json:"media"`
			} `json:"items"`
		}
		_ = json.NewDecoder(res.Body).Decode(&out)
		res.Body.Close()
		return out.Items
	}

	// media=1：必然返回那条带媒体的，且每条都真的带媒体
	withMedia := fetch("sample=1&media=1&page=1&page_size=20")
	if len(withMedia) != 1 || withMedia[0].ID != postID {
		t.Fatalf("media=1 应恰好返回那条带媒体的动态，实得 %+v", withMedia)
	}
	for _, p := range withMedia {
		if len(p.Media) == 0 {
			t.Fatalf("media=1 返回了不带媒体的动态：%+v", p)
		}
	}

	// media=0：一条都不该带媒体
	for _, p := range fetch("sample=1&media=0&page=1&page_size=20") {
		if len(p.Media) != 0 {
			t.Fatalf("media=0 返回了带媒体的动态：%+v", p)
		}
	}
}
