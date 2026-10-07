package tools

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/bookandmusic/love-life/server/internal/notify"
	"github.com/bookandmusic/love-life/server/internal/store"
)

// statByLabel 取概览卡某项；不存在返回 nil。
func statByLabel(ui *UIBlock, label string) *UIStat {
	if ui == nil {
		return nil
	}
	for i := range ui.Stats {
		if ui.Stats[i].Label == label {
			return &ui.Stats[i]
		}
	}
	return nil
}

// P2-6：概览卡补齐缺失维度——在一起天数（首项；未设恋爱开始日则整项省略，不得给「0 天」）、
// 媒体照片/视频/录音分类计数、纪念日「下一个」；ModelText 只补媒体分类，不重复常驻事实四项。
func TestSpaceStatsCardDimensions(t *testing.T) {
	db := newToolDB(t)
	d := &Deps{DB: db, DP: notify.New(db)}
	ctx := t.Context()

	// 未设恋爱开始日：不得出现「在一起」项（不编造 0 天）
	out := exec(t, "get_space_stats", d, 1, map[string]any{})
	if statByLabel(out.UI, "在一起") != nil {
		t.Fatalf("未设恋爱开始日时不得给「在一起」项：%+v", out.UI.Stats)
	}

	// 设开始日 → 首项为在一起天数（首日算第 1 天）
	start := time.Now().AddDate(0, 0, -99).Format("2006-01-02")
	if err := db.UpdateSpace(ctx, &store.Space{LoveStartDate: sql.NullString{String: start, Valid: true}}); err != nil {
		t.Fatal(err)
	}
	// 媒体三类计数（删掉一条不参与计数）
	postID := seedPostAt(t, db, "带媒体的动态", "2026-08-10 10:00:00")
	for _, k := range []string{"photo", "photo", "video", "audio", "photo"} {
		seedMedia(t, db, postID, k, "")
	}
	if _, err := db.Exec(`UPDATE media SET deleted_at = datetime('now') WHERE post_id = ? AND kind = 'photo' AND id =
		(SELECT id FROM media WHERE post_id = ? AND kind = 'photo' ORDER BY id DESC LIMIT 1)`, postID, postID); err != nil {
		t.Fatal(err)
	}
	// 纪念日「下一个」：明天那条最近（含 lunar 之外的常规公历）
	tomorrow := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	if _, err := db.Exec(`INSERT INTO anniversaries (name, date, type, is_leap, remind_days_before, created_by)
		VALUES ('她的生日', ?, 'solar', 0, 3, 1)`, tomorrow); err != nil {
		t.Fatal(err)
	}

	out = exec(t, "get_space_stats", d, 1, map[string]any{})
	if out.UI == nil || len(out.UI.Stats) == 0 {
		t.Fatalf("概览卡应有 stats：%+v", out.UI)
	}
	if got := out.UI.Stats[0]; got.Label != "在一起" || got.Value != "100 天" {
		t.Fatalf("首项应为在一起天数，实得 %+v", got)
	}
	media := statByLabel(out.UI, "媒体")
	if media == nil || !strings.Contains(media.Hint, "照片 2") ||
		!strings.Contains(media.Hint, "视频 1") || !strings.Contains(media.Hint, "录音 1") {
		t.Fatalf("媒体项应含三类计数，实得 %+v", media)
	}
	anniv := statByLabel(out.UI, "纪念日")
	if anniv == nil || !strings.Contains(anniv.Hint, "她的生日") || !strings.Contains(anniv.Hint, "还有 1 天") {
		t.Fatalf("纪念日项应含「下一个」，实得 %+v", anniv)
	}
	// ModelText 只补媒体分类；常驻事实四项（在一起天数 / 今天 / 近 7 天动态数 / 下一个纪念日）不得重复
	if !strings.Contains(out.ModelText, "照片 2 / 视频 1 / 录音 1") {
		t.Fatalf("ModelText 应含媒体分类：%s", out.ModelText)
	}
	for _, banned := range []string{"在一起", "下一个纪念日", "今天", "近 7 天"} {
		if strings.Contains(out.ModelText, banned) {
			t.Fatalf("ModelText 不得出现常驻事实「%s」：%s", banned, out.ModelText)
		}
	}
}

// P2-7：近 12 个月分布窗口跨年——只取当年会让去年那两三个月在查询结果里根本不存在；
// 且窗口起点不能被月末归一化吃掉一个月（31 号直接回退 11 个月会跳到下一个月）。
func TestRecentMonthCountsSpansTwoYears(t *testing.T) {
	cases := []struct {
		name        string
		now         time.Time
		firstMonth  string // 窗口首个自然月（= now 往前第 11 个月）
		beforeMonth string // 窗口外的前一个月
	}{
		{"月中跨年", time.Date(2026, 10, 15, 12, 0, 0, 0, time.Local), "2025-11", "2025-10"},
		{"月末跨年", time.Date(2026, 10, 31, 12, 0, 0, 0, time.Local), "2025-11", "2025-10"},
		{"月末跨年二", time.Date(2026, 1, 31, 12, 0, 0, 0, time.Local), "2025-02", "2025-01"},
		{"年末单年", time.Date(2026, 12, 31, 12, 0, 0, 0, time.Local), "2026-01", "2025-12"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := newToolDB(t)
			d := &Deps{DB: db, DP: notify.New(db)}
			ctx := t.Context()
			seedPostAt(t, db, "窗口首月", tc.firstMonth+"-05 10:00:00")
			seedPostAt(t, db, "窗口外", tc.beforeMonth+"-05 10:00:00")
			seedPostAt(t, db, "本月", tc.now.Format("2006-01")+"-05 10:00:00")

			got := recentMonthCounts(ctx, d, tc.now)
			if !strings.Contains(got, tc.firstMonth+" 1 条") {
				t.Fatalf("窗口首个自然月 %s 应在分布里：%q", tc.firstMonth, got)
			}
			if !strings.Contains(got, tc.now.Format("2006-01")+" 1 条") {
				t.Fatalf("本月应在分布里：%q", got)
			}
			if strings.Contains(got, tc.beforeMonth) {
				t.Fatalf("窗口外（12 个月以前）的月份 %s 不得出现：%q", tc.beforeMonth, got)
			}
			// 跨年时必须查两个年份，输出仍按时间递增（顺序来自年份循环，SQL 只保证年内有序）
			if strings.Index(got, tc.firstMonth) > strings.Index(got, tc.now.Format("2006-01")) {
				t.Fatalf("月份应按时间递增：%q", got)
			}
		})
	}
}
