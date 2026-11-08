package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// 近 12 个月分布只进 ModelText、不上概览卡（卡上只放两组 series：按年 + 城市 Top 10）。
func TestSpaceStatsRecentMonthsInModelText(t *testing.T) {
	db := newToolDB(t)
	ctx := context.Background()
	d := &Deps{DB: db}
	for _, c := range []string{"海边的回忆 0", "海边的回忆 1", "海边的回忆 2"} {
		if _, err := db.ExecContext(ctx, `INSERT INTO posts (author_id, content) VALUES (1, ?)`, c); err != nil {
			t.Fatal(err)
		}
	}
	res, err := getSpaceStatsTool().Run(ctx, d, 1, map[string]any{}, ModeExecute)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "近 12 个月动态分布") {
		t.Fatalf("ModelText 缺近 12 个月分布：%s", res.Text)
	}
	if !strings.Contains(res.Text, "2026-") {
		t.Fatalf("分布应含月份键：%s", res.Text)
	}
	// 卡上只两组 series，且不含月度
	if res.Data == nil || len(res.Data.Series) != 2 {
		t.Fatalf("概览卡 series 应为 2 组：%+v", res.Data)
	}
	for _, s := range res.Data.Series {
		if strings.Contains(s.Title, "月") && s.Title != "按年" {
			t.Fatalf("月度分布不得上卡：%+v", s)
		}
	}
}

// 翻页时 ModelText 回显区间（「第 N–M 条，共 T 条」），让模型知道自己看的是哪一段。
func TestFindPostsModelTextEchoesPageRange(t *testing.T) {
	db := newToolDB(t)
	d := &Deps{DB: db}
	for i := 1; i <= 5; i++ {
		seedPostAt(t, db, "海边回忆", fmt.Sprintf("2026-01-%02d 10:00:00", i))
	}
	// 第一页：不写区间（「共 N 条」已足够）
	first := exec(t, "find_posts", d, 1, map[string]any{"query": "海边", "limit": 2})
	if strings.Contains(first.Text, "第 1–2 条") {
		t.Fatalf("首页不该写区间：%s", first.Text)
	}
	// 第二页：写区间
	second := exec(t, "find_posts", d, 1, map[string]any{"query": "海边", "limit": 2, "offset": 2})
	if !strings.Contains(second.Text, "第 3–4 条") {
		t.Fatalf("翻页应回显区间：%s", second.Text)
	}
	if !strings.Contains(second.Text, "共 5 条") {
		t.Fatalf("翻页应回显总数：%s", second.Text)
	}
}
